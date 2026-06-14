/*
Copyright 2025 NVIDIA

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
	"fmt"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	hostcontroller "github.com/nvidia/doca-platform/internal/storage/snap/host-controller"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/health"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	eventv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
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
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(storagev1.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

var (
	metricsAddr          string
	pprofBindAddr        string
	enableLeaderElection bool
	probeAddr            string
	insecureMetrics      bool
	syncPeriod           time.Duration
	logOptions           = logs.NewOptions()
	concurrency          int
	namespace            string
	targetNamespace      string
)

func initFlags(fs *pflag.FlagSet) {
	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.StringVar(&pprofBindAddr, "pprof-bind-address", "", "The address the pprof endpoint binds to.")
	// --leader-elect defaults to false so `make run` / tests skip the Lease entirely
	// (otherwise startup blocks for ~LeaseDuration waiting for stale leases).
	// Production deployment manifests pass --leader-elect to opt into HA.
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.IntVar(&concurrency, "concurrency", 15,
		"Number of objects to process simultaneously by each controller.")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	fs.StringVar(&namespace, "namespace", "", "namespace in the management cluster in which controller will reconcile resources, required")
	fs.StringVar(&targetNamespace, "target-namespace", "", "namespace in the DPUCluster where storage objects are managed, required")
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

	if err := validateFlags(); err != nil {
		setupLog.Error(err, "invalid argument")
		pflag.Usage()
		os.Exit(1)
	}

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
		PprofBindAddress:       pprofBindAddr,
		HealthProbeBindAddress: probeAddr,
		Client: client.Options{
			Cache: &client.CacheOptions{
				// Don't cache Secrets and ConfigMaps. In general, the
				// controller-runtime client does a LIST and WATCH to cache
				// kinds you request (see https://github.com/kubernetes-sigs/controller-runtime/pull/1249),
				// and this can mean caching all secrets and configmaps; when
				// all that's required is the few that are referenced for
				// objects of interest to the controllers.
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
			},
		},
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
			ByObject: map[client.Object]cache.ByObject{
				// watch DPUVolume and DPUVolumeAttachment only in namespace where the controller runs
				&storagev1.DPUVolume{}:           {Namespaces: map[string]cache.Config{namespace: {}}},
				&storagev1.DPUVolumeAttachment{}: {Namespaces: map[string]cache.Config{namespace: {}}},
				&storagev1.DPUStorageVendor{}:    {Namespaces: map[string]cache.Config{namespace: {}}},
				&storagev1.DPUStoragePolicy{}:    {Namespaces: map[string]cache.Config{namespace: {}}},
			},
		},
		Controller: config.Controller{
			MaxConcurrentReconciles: concurrency,
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "snap-host-controller.dpu.nvidia.com",
		// Release the lease on shutdown so a standby replica can take over within
		// seconds. Safe because main() exits immediately after mgr.Start returns.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	// Setup field indexers
	if err := indexers.SetupIndexers(ctx, mgr); err != nil {
		setupLog.Error(err, "failed to setup field indexers")
		os.Exit(1)
	}

	reconcileOptions := hostcontroller.Options{
		Namespace:       namespace,
		TargetNamespace: targetNamespace,
	}

	dpuVolumeReconciler := &hostcontroller.DPUVolumeReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Options: reconcileOptions,
	}

	dpuVolumeAttachmentReconciler := &hostcontroller.DPUVolumeAttachmentReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Options: reconcileOptions,
	}

	dpuStorageVendorReconciler := &hostcontroller.DPUStorageVendorReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Options: reconcileOptions,
	}

	dpuStoragePolicyReconciler := &hostcontroller.DPUStoragePolicyReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Options: reconcileOptions,
	}

	eventReconciler := &hostcontroller.EventReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("eventstoragecontroller"),
		Options:  reconcileOptions,
	}

	// new remote cache
	rc, err := dpucluster.SetupRemoteCacheWithManager(ctx, mgr,
		dpucluster.OptionTimeout{Timeout: time.Second * 30},
		dpucluster.OptionHostClient{Client: mgr.GetClient()},
		dpucluster.OptionScheme{Scheme: mgr.GetScheme()},
		dpucluster.OptionUserAgent{UserAgent: "snap-host-controller"},
		dpucluster.OptionSyncPeriod{SyncPeriod: syncPeriod},
		dpucluster.OptionDisableFor{DisableFor: []client.Object{
			&corev1.ConfigMap{},
			&corev1.Secret{},
		}},
		dpucluster.OptionDefaultNamespaces{DefaultNamespaces: map[string]cache.Config{targetNamespace: {}}},
		dpucluster.OptionByObject{
			ByObject: map[client.Object]cache.ByObject{
				&eventv1.Event{}: {
					Field: fields.AndSelectors(
						fields.OneTermEqualSelector("regarding.apiVersion", "v1"),
						fields.OneTermEqualSelector("regarding.kind", "PersistentVolumeClaim")),
				},
			},
		},
		dpucluster.OptionGetWatcherCallbacks{
			GetWatcherCallbacks: []dpucluster.GetWatcherCallback{
				dpuStorageVendorReconciler.WatchDPUClusterStorageClass,
				dpuStorageVendorReconciler.WatchDPUClusterCSIDriver,
				dpuVolumeReconciler.WatchDPUClusterPV,
				dpuVolumeReconciler.WatchDPUClusterPVC,
				dpuVolumeReconciler.WatchDPUClusterVolume,
				dpuVolumeAttachmentReconciler.WatchDPUClusterVolumeAttachment,
				dpuVolumeAttachmentReconciler.WatchDPUClusterSVVolumeAttachment,
				eventReconciler.WatchDPUClusterEvent,
			},
		},
	)

	if err != nil {
		setupLog.Error(err, "unable to create remote cache")
		os.Exit(1)
	}

	dpuVolumeReconciler.RemoteCache = rc
	if err = dpuVolumeReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUVolumeReconciler")
		os.Exit(1)
	}
	dpuVolumeAttachmentReconciler.RemoteCache = rc
	if err = dpuVolumeAttachmentReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUVolumeAttachmentReconciler")
		os.Exit(1)
	}
	dpuStorageVendorReconciler.RemoteCache = rc
	if err = dpuStorageVendorReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUStorageVendorReconciler")
		os.Exit(1)
	}

	if err = dpuStoragePolicyReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUStoragePolicyReconciler")
		os.Exit(1)
	}

	eventReconciler.RemoteCache = rc
	if err = eventReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EventReconciler")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

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

func validateFlags() error {
	if targetNamespace == "" {
		return fmt.Errorf("target-namespace arg is required")
	}
	return nil
}
