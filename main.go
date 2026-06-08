package main

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cloudflarev1 "github.com/example/cf-tunnel-operator/api/v1"
	"github.com/example/cf-tunnel-operator/controllers"
	"github.com/example/cf-tunnel-operator/pkg/cloudflare"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cloudflarev1.AddToScheme(scheme))
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Choose Cloudflare client: real API or no-op for local dev
	var cfClient cloudflare.Client
	token := os.Getenv("CF_API_TOKEN")
	if token != "" {
		setupLog.Info("CF_API_TOKEN is set, using real Cloudflare client")
		cfClient = cloudflare.NewRealClientFromEnv()
	} else {
		setupLog.Info("CF_API_TOKEN is empty, using no-op Cloudflare client")
		setupLog.Info("To use real tunnels, create a .env file with CF_API_TOKEN and run: make load-secrets")
		cfClient = &cloudflare.NoOpClient{}
	}

	// Tunnel controller: watches CloudflareTunnel, manages CF resources + cloudflared pods
	if err = (&controllers.CloudflareTunnelReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloudflaretunnel-controller"),
		CF:       cfClient,
		ZoneID:   os.Getenv("CF_ZONE_ID"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudflareTunnel")
		os.Exit(1)
	}

	// Ingress adapter: watches Ingress with class=cf-tunnel, generates CloudflareTunnel
	if err = (&controllers.IngressAdapterReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("ingress-adapter-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IngressAdapter")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
