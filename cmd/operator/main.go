/*
Copyright 2024 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"os"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/release"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(operatorv1.AddToScheme(scheme))

	utilruntime.Must(dpuservicev1.AddToScheme(scheme))

	utilruntime.Must(argov1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

var (
	metricsAddr              string
	enableLeaderElection     bool
	probeAddr                string
	insecureMetrics          bool
	enableHTTP2              bool
	configSingletonNamespace string
	configSingletonName      string
	syncPeriod               time.Duration
	logOptions               = logs.NewOptions()
)

func initFlags(fs *pflag.FlagSet) {
	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.StringVar(&configSingletonNamespace, "config-namespace",
		operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, "The namespace of the DPFOperatorConfig the operator will reconcile")
	fs.StringVar(&configSingletonName, "config-name",
		operatorcontroller.DefaultDPFOperatorConfigSingletonName, "The name of the DPFOperatorConfig the operator will reconcile")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	logsv1.AddFlags(logOptions, fs)

}

// Add RBAC for the metrics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

func main() {
	initFlags(pflag.CommandLine)

	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())
	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})
	metricsOpts := metricsserver.Options{
		BindAddress:    metricsAddr,
		SecureServing:  true,
		FilterProvider: filters.WithAuthenticationAndAuthorization,
	}
	if insecureMetrics {
		metricsOpts.SecureServing = false
		metricsOpts.FilterProvider = nil
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOpts,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "8a3114c5.dpu.nvidia.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// ParseAll manifest yamls to ensure Kubernetes objects are available before starting.
	inventory := inventory.New()
	if err := inventory.ParseAll(); err != nil {
		setupLog.Error(err, "unable to parse inventory")
		os.Exit(1)
	}
	defaults := release.NewDefaults()
	if err = defaults.Parse(); err != nil {
		setupLog.Error(err, "unable to parse defaults")
		os.Exit(1)
	}
	if err = (&operatorcontroller.DPFOperatorConfigReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Settings:  getSettings(),
		Inventory: inventory,
		Defaults:  defaults,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPFOperatorConfig")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getSettings() *operatorcontroller.DPFOperatorConfigReconcilerSettings {
	configSingletonNamespaceName := &types.NamespacedName{
		Namespace: configSingletonNamespace,
		Name:      configSingletonName,
	}
	if configSingletonNamespace == "" && configSingletonName == "" {
		configSingletonNamespaceName = nil
	}
	return &operatorcontroller.DPFOperatorConfigReconcilerSettings{
		ConfigSingletonNamespaceName: configSingletonNamespaceName,
	}
}
