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
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	dpuservicecontroller "github.com/nvidia/doca-platform/internal/dpuservice/controllers"
	"github.com/nvidia/doca-platform/internal/dpuservice/utils"
	dpuservicechaincontroller "github.com/nvidia/doca-platform/internal/dpuservicechain/controllers"
	dpuservicechainwebhooks "github.com/nvidia/doca-platform/internal/dpuservicechain/webhooks"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/health"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	setupLog   = ctrl.Log.WithName("setup")
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(clientgoscheme.Scheme))

	utilruntime.Must(argov1.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(dpuservicev1.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(nvipamv1.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(operatorv1.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(provisioningv1.AddToScheme(clientgoscheme.Scheme))
	utilruntime.Must(vpcv1.AddToScheme(clientgoscheme.Scheme))
	// +kubebuilder:scaffold:scheme
}

// Add RBAC for the metrics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

func main() {
	var metricsAddr string
	var pprofBindAddr string
	var enableLeaderElection bool
	var probeAddr string
	var insecureMetrics bool
	var enableHTTP2 bool
	var disableDPUReadyTaints bool
	var syncPeriod time.Duration
	var concurrency int

	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.StringVar(&pprofBindAddr, "pprof-bind-address", "", "The address the pprof endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers.")
	fs.BoolVar(&disableDPUReadyTaints, "disable-dpu-ready-taints", false,
		"If set, the DPUReady controller will not add/remove taints when DPUs are not ready. Other controller functionality remains enabled.")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	fs.IntVar(&concurrency, "concurrency", 1,
		"Number of objects to process simultaneously by each controller.")

	logsv1.AddFlags(logOptions, fs)

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
		Scheme:                 clientgoscheme.Scheme,
		Metrics:                metricsOpts,
		PprofBindAddress:       pprofBindAddr,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		Client: client.Options{
			Cache: &client.CacheOptions{
				// Don't cache Secrets and ConfigMaps. In general, the
				// controller-runtime client does a LIST and WATCH to cache
				// kinds you request (see https://github.com/kubernetes-sigs/controller-runtime/pull/1249),
				// and this can mean caching all secrets and configmaps; when
				// all that's required is the few that are referenced for
				// objects of interest to the controllers.
				DisableFor:   []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
				Unstructured: true,
			},
		},
		Controller: config.Controller{
			MaxConcurrentReconciles: concurrency,
		},
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "e361afcf.nvidia.com",
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

	ctx := ctrl.SetupSignalHandler()

	// Setup field indexers
	if err := dpuservicecontroller.SetupIndexers(ctx, mgr); err != nil {
		setupLog.Error(err, "failed to setup field indexers")
		os.Exit(1)
	}

	podsOwnedByDPUServiceLabelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      dpuservicev1.DPFServiceIDLabelKey,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "could not create label selector")
		os.Exit(1)
	}

	if disableDPUReadyTaints {
		setupLog.Info("DPUReady taint management is disabled")
	}
	dpuReadyReconciler := &dpuservicecontroller.DPUReadyReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		DisableDPUReadyTaints: disableDPUReadyTaints,
	}
	if err = dpuReadyReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUReady")
		os.Exit(1)
	}

	// new remote cache
	rc, err := dpucluster.SetupRemoteCacheWithManager(ctx, mgr,
		dpucluster.OptionHostClient{Client: mgr.GetClient()},
		dpucluster.OptionScheme{Scheme: mgr.GetScheme()},
		dpucluster.OptionUserAgent{UserAgent: "dpuservice-controller"},
		dpucluster.OptionSyncPeriod{SyncPeriod: syncPeriod},
		dpucluster.OptionGetWatcherCallbacks{
			GetWatcherCallbacks: []dpucluster.GetWatcherCallback{
				dpuReadyReconciler.WatchServicePods,
			},
		},
		dpucluster.OptionDisableFor{DisableFor: []client.Object{
			&corev1.ConfigMap{},
			&corev1.Secret{},
		}},
		dpucluster.OptionByObject{ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				// watch only pods with the service id label
				Label: podsOwnedByDPUServiceLabelSelector,
			},
		}},
	)

	if err != nil {
		setupLog.Error(err, "unable to create remote cache")
		os.Exit(1)
	}

	if err = (&dpuservicecontroller.DPUServiceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUService")
		os.Exit(1)
	}

	if err = (&dpuservicechaincontroller.DPUServiceInterfaceReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		RemoteCache: rc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUServiceInterface")
		os.Exit(1)
	}

	if err = (&dpuservicechaincontroller.DPUServiceChainReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		RemoteCache: rc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUServiceChain")
		os.Exit(1)
	}

	if err = (&dpuservicechaincontroller.DPUServiceIPAMReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		RemoteCache: rc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUServiceIPAM")
		os.Exit(1)
	}

	if err = (&dpuservicecontroller.DPUDeploymentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUDeployment")
		os.Exit(1)
	}

	if err = (&dpuservicecontroller.DPUDeploymentNodeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUDeploymentNode")
		os.Exit(1)
	}

	if err = (&dpuservicechainwebhooks.DPUServiceIPAMValidator{
		Client: mgr.GetClient(),
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUServiceIPAM")
		os.Exit(1)
	}

	if err = (&dpuservicecontroller.DPUServiceCredentialRequestReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUServiceCredentialRequest")
		os.Exit(1)
	}

	if err = (&dpuservicecontroller.DPUServiceTemplateReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ChartHelper: utils.NewChartHelper(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUServiceTemplate")
		os.Exit(1)
	}

	if err = (&dpuservicechaincontroller.DPUServiceNADReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		RemoteCache: rc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUServiceNAD")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
