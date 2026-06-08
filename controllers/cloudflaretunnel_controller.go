package controllers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cloudflarev1 "github.com/example/cf-tunnel-operator/api/v1"
	"github.com/example/cf-tunnel-operator/pkg/cloudflare"
)

const (
	cloudflareTunnelFinalizer = "cloudflaretunnel.example.com/finalizer"
)

// CloudflareTunnelReconciler reconciles a CloudflareTunnel object
type CloudflareTunnelReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	CF       cloudflare.Client
	ZoneID   string
}

// +kubebuilder:rbac:groups=cloudflare.example.com,resources=cloudflaretunnels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.example.com,resources=cloudflaretunnels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *CloudflareTunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fail fast if ZoneID is not configured
	if r.ZoneID == "" {
		return ctrl.Result{}, fmt.Errorf("ZoneID is not configured: set CF_ZONE_ID environment variable")
	}

	var tunnel cloudflarev1.CloudflareTunnel
	if err := r.Get(ctx, req.NamespacedName, &tunnel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deletion: run cleanup then remove finalizer
	if !tunnel.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &tunnel)
	}

	// Ensure finalizer is present so K8s waits for us on delete
	if !controllerutil.ContainsFinalizer(&tunnel, cloudflareTunnelFinalizer) {
		controllerutil.AddFinalizer(&tunnel, cloudflareTunnelFinalizer)
		if err := r.Update(ctx, &tunnel); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 1. Ensure Cloudflare Tunnel exists
	tunnelID := tunnel.Status.TunnelID
	if tunnelID == "" {
		name := fmt.Sprintf("k8s-%s-%s", tunnel.Namespace, tunnel.Name)
		if n := os.Getenv("TUNNEL_NAME_PREFIX"); n != "" {
			name = n + "-" + tunnel.Namespace + "-" + tunnel.Name
		}
		log.Info("creating cloudflare tunnel", "name", name)
		t, err := r.CF.CreateTunnel(ctx, name)
		if err != nil {
			r.Recorder.Eventf(&tunnel, corev1.EventTypeWarning, "TunnelCreateFailed", "Failed to create tunnel: %v", err)
			return ctrl.Result{}, err
		}
		tunnelID = t.ID
		tunnel.Status.TunnelID = tunnelID
		if err := r.Status().Update(ctx, &tunnel); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(&tunnel, corev1.EventTypeNormal, "TunnelCreated", "Created tunnel %s", tunnelID)
	}

	// 2. Ensure Secret with tunnel credentials
	token, err := r.CF.GetTunnelToken(ctx, tunnelID)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSecret(ctx, &tunnel, tunnelID, token); err != nil {
		return ctrl.Result{}, err
	}

	// 3. Ensure ConfigMap with cloudflared config
	if err := r.ensureConfigMap(ctx, &tunnel, tunnelID); err != nil {
		return ctrl.Result{}, err
	}

	// 4. Ensure Deployment running cloudflared
	if err := r.ensureDeployment(ctx, &tunnel); err != nil {
		return ctrl.Result{}, err
	}

	// 5. Ensure DNS CNAME record
	if err := r.ensureDNS(ctx, &tunnel, tunnelID); err != nil {
		return ctrl.Result{}, err
	}

	// 6. Mark ready
	if !tunnel.Status.Ready {
		tunnel.Status.Ready = true
		if err := r.Status().Update(ctx, &tunnel); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(&tunnel, corev1.EventTypeNormal, "Ready", "Tunnel %s is ready", tunnelID)
	}

	return ctrl.Result{}, nil
}

func (r *CloudflareTunnelReconciler) cleanup(ctx context.Context, tunnel *cloudflarev1.CloudflareTunnel) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(tunnel, cloudflareTunnelFinalizer) {
		return ctrl.Result{}, nil
	}

	// 1. Stop the cloudflared Deployment so pods drop their tunnel connections
	depName := fmt.Sprintf("cft-tun-%s", tunnel.Name)
	dep := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: tunnel.Namespace, Name: depName}
	if err := r.Get(ctx, key, dep); err == nil {
		if dep.DeletionTimestamp.IsZero() {
			log.Info("deleting cloudflared deployment to stop tunnel connections", "deployment", depName)
			if err := r.Delete(ctx, dep); err != nil {
				return ctrl.Result{}, err
			}
		}
		log.Info("waiting for cloudflared deployment to terminate before deleting tunnel")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// 2. Delete the Cloudflare tunnel (pods are gone, connections should be closed)
	if tunnel.Status.TunnelID != "" {
		log.Info("deleting cloudflare tunnel", "tunnelID", tunnel.Status.TunnelID)
		if err := r.CF.DeleteTunnel(ctx, tunnel.Status.TunnelID); err != nil {
			if strings.Contains(err.Error(), "active connections") {
				log.Info("tunnel still has active connections, waiting before retry", "tunnelID", tunnel.Status.TunnelID)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			log.Error(err, "failed to delete tunnel, retrying")
			return ctrl.Result{}, err
		}

		// 3. Clean up DNS records that point to this tunnel
		expectedContent := fmt.Sprintf("%s.cfargotunnel.com", tunnel.Status.TunnelID)
		records, err := r.CF.ListDNSRecords(ctx, r.ZoneID, tunnel.Spec.Hostname)
		if err != nil {
			log.Error(err, "failed to list DNS records for cleanup")
		} else {
			for _, rec := range records {
				if rec.Content == expectedContent {
					log.Info("deleting DNS record", "recordID", rec.ID, "hostname", rec.Name)
					if err := r.CF.DeleteDNSRecord(ctx, r.ZoneID, rec.ID); err != nil {
						log.Error(err, "failed to delete DNS record", "recordID", rec.ID)
					}
				} else {
					log.Info("skipping DNS record cleanup, content does not match tunnel", "recordID", rec.ID, "content", rec.Content, "expected", expectedContent)
				}
			}
		}
	}

	// 4. Remove finalizer so Kubernetes can garbage collect child resources
	patch := client.MergeFrom(tunnel.DeepCopy())
	controllerutil.RemoveFinalizer(tunnel, cloudflareTunnelFinalizer)
	if err := r.Patch(ctx, tunnel, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CloudflareTunnelReconciler) ensureSecret(ctx context.Context, tunnel *cloudflarev1.CloudflareTunnel, tunnelID, token string) error {
	secretName := fmt.Sprintf("cft-creds-%s", tunnel.Name)
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: tunnel.Namespace, Name: secretName}

	if err := r.Get(ctx, key, secret); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// Create
		decoded, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			return fmt.Errorf("failed to decode tunnel token: %w", err)
		}

		// The decoded token is compact JSON: {"a":"account","t":"tunnel-id","s":"secret"}
		var compact struct {
			A string `json:"a"`
			T string `json:"t"`
			S string `json:"s"`
		}
		if err := json.Unmarshal(decoded, &compact); err != nil {
			return fmt.Errorf("failed to parse tunnel token: %w", err)
		}

		// Reconstruct with the long-form keys that cloudflared expects
		creds := struct {
			AccountTag   string `json:"AccountTag"`
			TunnelID     string `json:"TunnelID"`
			TunnelSecret string `json:"TunnelSecret"`
		}{
			AccountTag:   compact.A,
			TunnelID:     compact.T,
			TunnelSecret: compact.S,
		}
		credsJSON, err := json.Marshal(creds)
		if err != nil {
			return fmt.Errorf("failed to marshal credentials: %w", err)
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: tunnel.Namespace,
			},
			StringData: map[string]string{
				"creds.json": string(credsJSON),
			},
		}
		if err := controllerutil.SetControllerReference(tunnel, secret, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, secret)
	}
	return nil
}

func (r *CloudflareTunnelReconciler) ensureConfigMap(ctx context.Context, tunnel *cloudflarev1.CloudflareTunnel, tunnelID string) error {
	cmName := fmt.Sprintf("cft-cfg-%s", tunnel.Name)
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: tunnel.Namespace, Name: cmName}

	if err := r.Get(ctx, key, cm); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// Build cloudflared config from spec.rules
		cfg := r.renderCloudflaredConfig(tunnel, tunnelID)
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: tunnel.Namespace,
			},
			Data: map[string]string{
				"config.yaml": cfg,
			},
		}
		if err := controllerutil.SetControllerReference(tunnel, cm, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, cm)
	}

	// Update if config changed
	newCfg := r.renderCloudflaredConfig(tunnel, tunnelID)
	if cm.Data["config.yaml"] != newCfg {
		cm.Data["config.yaml"] = newCfg
		return r.Update(ctx, cm)
	}
	return nil
}

func (r *CloudflareTunnelReconciler) ensureDeployment(ctx context.Context, tunnel *cloudflarev1.CloudflareTunnel) error {
	depName := fmt.Sprintf("cft-tun-%s", tunnel.Name)
	secretName := fmt.Sprintf("cft-creds-%s", tunnel.Name)
	cmName := fmt.Sprintf("cft-cfg-%s", tunnel.Name)
	replicas := int32(2)
	if tunnel.Spec.Replicas != nil {
		replicas = *tunnel.Spec.Replicas
	}

	dep := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: tunnel.Namespace, Name: depName}
	if err := r.Get(ctx, key, dep); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// Create
		dep = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      depName,
				Namespace: tunnel.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"cf-tunnel": tunnel.Name},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"cf-tunnel": tunnel.Name},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "cloudflared",
								Image: "cloudflare/cloudflared:latest",
								Args: []string{
									"tunnel",
									"--config", "/etc/cloudflared/config.yaml",
									"run",
								},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "creds", MountPath: "/etc/cloudflared/creds.json", SubPath: "creds.json", ReadOnly: true},
									{Name: "cfg", MountPath: "/etc/cloudflared/config.yaml", SubPath: "config.yaml", ReadOnly: true},
								},
								Ports: []corev1.ContainerPort{
									{ContainerPort: 2000, Name: "metrics"},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("32Mi"),
										corev1.ResourceCPU:    resource.MustParse("50m"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("128Mi"),
										corev1.ResourceCPU:    resource.MustParse("200m"),
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "creds",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: secretName,
										Items: []corev1.KeyToPath{
											{Key: "creds.json", Path: "creds.json"},
										},
									},
								},
							},
							{
								Name: "cfg",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
										Items: []corev1.KeyToPath{
											{Key: "config.yaml", Path: "config.yaml"},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		if err := controllerutil.SetControllerReference(tunnel, dep, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, dep)
	}
	return nil
}

func (r *CloudflareTunnelReconciler) ensureDNS(ctx context.Context, tunnel *cloudflarev1.CloudflareTunnel, tunnelID string) error {
	recordID := tunnel.Annotations["cloudflaretunnel.example.com/dns-record-id"]
	if recordID != "" {
		return nil // already created
	}
	log := log.FromContext(ctx)

	// Check if any DNS record already exists for this hostname
	records, err := r.CF.ListDNSRecords(ctx, r.ZoneID, tunnel.Spec.Hostname)
	if err != nil {
		return fmt.Errorf("failed to list DNS records for %s: %w", tunnel.Spec.Hostname, err)
	}
	if len(records) > 0 {
		return fmt.Errorf("DNS record already exists for hostname %s (found %d record(s)); delete them before creating a tunnel", tunnel.Spec.Hostname, len(records))
	}

	log.Info("creating DNS record", "hostname", tunnel.Spec.Hostname)
	id, err := r.CF.CreateDNSRecord(ctx, r.ZoneID, tunnel.Spec.Hostname, tunnelID)
	if err != nil {
		return err
	}
	patch := client.MergeFrom(tunnel.DeepCopy())
	if tunnel.Annotations == nil {
		tunnel.Annotations = map[string]string{}
	}
	tunnel.Annotations["cloudflaretunnel.example.com/dns-record-id"] = id
	return r.Patch(ctx, tunnel, patch)
}

func (r *CloudflareTunnelReconciler) renderCloudflaredConfig(tunnel *cloudflarev1.CloudflareTunnel, tunnelID string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "tunnel: %s\n", tunnelID)
	fmt.Fprintln(&b, "credentials-file: /etc/cloudflared/creds.json")
	fmt.Fprintln(&b, "metrics: 0.0.0.0:2000")
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "ingress:")
	for _, rule := range tunnel.Spec.Rules {
		fmt.Fprintf(&b, "  - hostname: %s\n", tunnel.Spec.Hostname)
		fmt.Fprintf(&b, "    path: %s\n", rule.Path)
		fmt.Fprintf(&b, "    service: http://%s.%s.svc.cluster.local:%d\n",
			rule.Backend.ServiceName, tunnel.Namespace, rule.Backend.Port)
	}
	fmt.Fprintln(&b, "  - service: http_status:404")
	return b.String()
}

func (r *CloudflareTunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cloudflarev1.CloudflareTunnel{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
