package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cloudflarev1 "github.com/example/cf-tunnel-operator/api/v1"
)

const ingressClassName = "cf-tunnel"

// IngressAdapterReconciler watches standard Ingress objects and materializes CloudflareTunnel CRDs.
type IngressAdapterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare.example.com,resources=cloudflaretunnels,verbs=get;list;watch;create;update;patch;delete

func (r *IngressAdapterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var ing networking.Ingress
	if err := r.Get(ctx, req.NamespacedName, &ing); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Filter: only handle our class
	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != ingressClassName {
		return ctrl.Result{}, nil
	}

	// Build desired CloudflareTunnel from Ingress spec
	desired := r.toCloudflareTunnel(&ing)
	found := &cloudflarev1.CloudflareTunnel{}
	key := client.ObjectKey{Namespace: ing.Namespace, Name: ing.Name}

	if err := r.Get(ctx, key, found); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// Create
		log.Info("creating CloudflareTunnel from Ingress", "ingress", req.NamespacedName)
		if err := controllerutil.SetControllerReference(&ing, desired, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, desired); err != nil {
			if errors.IsNotFound(err) {
				log.Info("CloudflareTunnel CRD not found, skipping Ingress (CRD may not be installed)")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(&ing, corev1.EventTypeNormal, "TunnelCreated", "Created CloudflareTunnel %s", desired.Name)
		return ctrl.Result{}, nil
	}

	// Update existing
	found.Spec = desired.Spec
	if err := r.Update(ctx, found); err != nil {
		return ctrl.Result{}, err
	}

	// Mirror status from CloudflareTunnel back to Ingress
	if found.Status.Ready {
		ing.Status.LoadBalancer.Ingress = []networking.IngressLoadBalancerIngress{
			{Hostname: found.Spec.Hostname},
		}
		if err := r.Status().Update(ctx, &ing); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *IngressAdapterReconciler) toCloudflareTunnel(ing *networking.Ingress) *cloudflarev1.CloudflareTunnel {
	// Derive hostname from first rule
	hostname := ""
	var rules []cloudflarev1.TunnelRule

	for _, rule := range ing.Spec.Rules {
		if hostname == "" {
			hostname = rule.Host
		}
		for _, path := range rule.HTTP.Paths {
			rules = append(rules, cloudflarev1.TunnelRule{
				Path: path.Path,
				Backend: cloudflarev1.ServiceBackend{
					ServiceName: path.Backend.Service.Name,
					Port:        path.Backend.Service.Port.Number,
				},
			})
		}
	}

	return &cloudflarev1.CloudflareTunnel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ing.Name,
			Namespace: ing.Namespace,
		},
		Spec: cloudflarev1.CloudflareTunnelSpec{
			Hostname: hostname,
			Rules:    rules,
		},
	}
}

func (r *IngressAdapterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networking.Ingress{}).
		Complete(r)
}
