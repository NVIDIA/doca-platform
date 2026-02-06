/*
Copyright 2026 NVIDIA

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
	"context"
	"os"
	"strings"
	"time"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	nodesriovdeviceplugincontrollers "github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/controllers"
	nodesriovdevicepluginwebhooks "github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/webhooks"
	"github.com/nvidia/doca-platform/pkg/health"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(noderesourcesv1.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))

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
	var syncPeriod time.Duration
	var concurrency int
	var namespace string
	var ownerConfigMapName string
	var disableWebhook bool

	// Device plugin configuration flags.
	var devicePluginImage string
	var devicePluginInitImage string
	var defaultResourcePrefix string
	var imagePullSecrets string

	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	fs.StringVar(&pprofBindAddr, "pprof-bind-address", "",
		"The address the pprof endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled.")
	fs.IntVar(&concurrency, "concurrency", 10,
		"Number of objects to process simultaneously by each controller.")
	fs.StringVar(&namespace, "namespace", "dpf-operator-system",
		"The namespace where the controller watches DPU/DPUNode objects and creates managed Pods.")
	fs.StringVar(&ownerConfigMapName, "owner-configmap-name", "",
		"The name of the ConfigMap (in the specified --namespace) to be set as an owner reference for Pods created by this controller,"+
			"enabling proper garbage collection of managed Pods by external controllers.")
	fs.BoolVar(&disableWebhook, "disable-webhook", false,
		"Disable registering webhooks with the manager.")

	// Device plugin configuration flags.
	fs.StringVar(&devicePluginImage, "device-plugin-image", "",
		"The container image for the SRIOV device plugin.")
	fs.StringVar(&devicePluginInitImage, "device-plugin-init-image", "",
		"The container image for the init container that generates device plugin configuration.")
	fs.StringVar(&defaultResourcePrefix, "default-resource-prefix",
		nodesriovdeviceplugincontrollers.DefaultResourcePrefix,
		"The default resource prefix for the SRIOV device plugin resources.")
	fs.StringVar(&imagePullSecrets, "image-pull-secrets", "",
		"Comma-separated list of image pull secrets for the device plugin pod.")

	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())

	// Validate required flags.
	if devicePluginImage == "" {
		setupLog.Error(nil, "--device-plugin-image flag is required")
		os.Exit(1)
	}
	if devicePluginInitImage == "" {
		setupLog.Error(nil, "--device-plugin-init-image flag is required")
		os.Exit(1)
	}

	if ownerConfigMapName == "" {
		setupLog.Info("owner ConfigMap name is not specified, will not set owner reference for Pods created by this controller")
	}

	// Build device plugin config from flags.
	devicePluginConfig := nodesriovdeviceplugincontrollers.DevicePluginConfig{
		Image:                 devicePluginImage,
		InitImage:             devicePluginInitImage,
		DefaultResourcePrefix: defaultResourcePrefix,
	}
	if imagePullSecrets != "" {
		devicePluginConfig.ImagePullSecrets = strings.Split(imagePullSecrets, ",")
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
		Client:                 nodesriovdeviceplugincontrollers.GetClientOptions(),
		Cache:                  nodesriovdeviceplugincontrollers.GetCacheOptions(namespace, syncPeriod, ownerConfigMapName),
		Controller: config.Controller{
			MaxConcurrentReconciles: concurrency,
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "nodesriovdeviceplugin.dpu.nvidia.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	// Setup field indexers.
	if err := nodesriovdeviceplugincontrollers.SetupIndexers(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to setup indexers")
		os.Exit(1)
	}

	// Create backoff for failed pods to avoid hot-looping.
	failedPodsBackoff := flowcontrol.NewBackOff(
		nodesriovdeviceplugincontrollers.FailedPodBackoffBaseDelay,
		nodesriovdeviceplugincontrollers.FailedPodBackoffMaxDelay,
	)

	// Add backoff GC runnable.
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		wait.Until(failedPodsBackoff.GC, nodesriovdeviceplugincontrollers.BackoffGCInterval, ctx.Done())
		return nil
	})); err != nil {
		setupLog.Error(err, "unable to add backoff GC runnable")
		os.Exit(1)
	}

	// Setup node controller.
	if err := (&nodesriovdeviceplugincontrollers.NodeReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		Namespace:          namespace,
		OwnerConfigMapName: ownerConfigMapName,
		DevicePluginConfig: devicePluginConfig,
		FailedPodsBackoff:  failedPodsBackoff,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}

	if disableWebhook {
		setupLog.Info("webhooks are disabled")
	} else {
		if err := (&nodesriovdevicepluginwebhooks.NodeSRIOVDevicePluginConfigValidator{
			DefaultResourcePrefix: defaultResourcePrefix,
		}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "NodeSRIOVDevicePluginConfig")
			os.Exit(1)
		}
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
