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
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/csr"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/discovery"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpucluster"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpudevice"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunodemaintenance"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	provisioningwebhooks "github.com/nvidia/doca-platform/internal/provisioning/webhooks"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/health"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientset "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))

	utilruntime.Must(maintenancev1alpha1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// Add RBAC for the metrics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

// deleteDMSPods deletes all DMS pods at controller startup for upgrade from 25.7 to 25.10
func deleteDMSPods(ctx context.Context, k8sClient client.Client) error {
	setupLog.Info("Deleting all DMS pods at startup")

	// Get the DPF operator config to determine the namespace
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, k8sClient)
	if err != nil {
		setupLog.Error(err, "Failed to get DPFOperatorConfig for DMS pod deletion")
		return err
	}

	namespace := dpfOperatorConfig.Namespace

	// List DMS pods using label selector
	podList := &corev1.PodList{}
	if err := k8sClient.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			cutil.ProvisioningComponentLabelKey: "dms",
		},
	); err != nil {
		setupLog.Error(err, "Failed to list DMS pods", "namespace", namespace)
		return err
	}

	// Delete all DMS pods found
	deletedCount := 0
	for _, pod := range podList.Items {
		setupLog.Info("Deleting DMS pod", "pod", pod.Name, "namespace", pod.Namespace)
		if err := k8sClient.Delete(ctx, &pod); err != nil {
			setupLog.Error(err, "Failed to delete DMS pod", "pod", pod.Name, "namespace", pod.Namespace)
			continue
		}
		deletedCount++
	}

	setupLog.Info("DMS pod deletion completed", "deletedCount", deletedCount, "namespace", namespace)
	return nil
}

func main() {
	var metricsAddr string
	var pprofBindAddr string
	var enableLeaderElection bool
	var insecureMetrics bool
	var enableHTTP2 bool
	var probeAddr string
	var dmsImage string
	var imagePullSecrets string
	var bfbPVC string
	var dmsTimeout int
	var dmsPodTimeout time.Duration
	var syncPeriod time.Duration
	var dpuInstallInterface string
	var bfCFGTemplateFile string
	var bfbRegistry string
	var concurrency int
	var customCASecretName string
	var dmsPodEnvs []string
	var maxDPUParallelInstallations int32
	var enableDpuDiscovery bool
	var multiDPUOperationsSyncWaitTime time.Duration
	var maxUnavailableDPUNodes int32

	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.StringVar(&pprofBindAddr, "pprof-bind-address", "", "The address the pprof endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.StringVar(&dmsImage, "dms-image", "", "The image for DMS pod.")
	fs.StringVar(&imagePullSecrets, "image-pull-secrets", "", "The image pull secrets for pods deployed by this controller.")
	fs.StringVar(&bfbPVC, "bfb-pvc", "", "The pvc to storage bfb.")
	fs.IntVar(&dmsTimeout, "dms-timeout", 900, "The max timeout execution in seconds of a command if not responding, 0 is unlimited.")
	fs.DurationVar(&dmsPodTimeout, "dms-pod-timeout", 5*time.Minute, "Timeout for DMS pods")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute, "The minimum interval at which watched resources are reconciled.")
	fs.IntVar(&concurrency, "concurrency", 1, "Number of objects to process simultaneously by each controller.")
	fs.StringVar(&dpuInstallInterface, "dpu-install-interface", string(provisioningv1.InstallViaHostAgent), "the interface used to provision DPUs")
	fs.StringVar(&bfCFGTemplateFile, "bf-cfg-template-file", "", "A custom bf.cfg template used as part of DPU provisioning.")
	fs.StringVar(&bfbRegistry, "bfb-registry", "", "hostname of the BFB registry from which BFBs are downloaded")
	fs.StringVar(&customCASecretName, "custom-CA-secret", "", "the secret object which containing the custom CA certificate")
	fs.StringSliceVar(&dmsPodEnvs, "dms-pod-envs", []string{}, "environment variables to set in the DMS pod")
	fs.Int32Var(&maxDPUParallelInstallations, "max-dpu-parallel-installations", 50, "The maximum number of DPUs that can be in provisioning at once")
	fs.BoolVar(&enableDpuDiscovery, "enable-dpu-discovery", true, "Enable autmated DPU discovery")
	fs.DurationVar(&multiDPUOperationsSyncWaitTime, "multi-dpu-operations-sync-wait-time", 30*time.Second, "The wait time between DPUs sync operations on the same node")
	fs.Int32Var(&maxUnavailableDPUNodes, "max-unavailable-dpu-nodes", 50, "The maximum number of DPUNodes that are unavailable during the node effect period")

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

	clientConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOpts,
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
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
			},
		},
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		PprofBindAddress: pprofBindAddr,
		Controller: config.Controller{
			MaxConcurrentReconciles: concurrency,
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "19f9f38b.nvidia.com",
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

	// imagePullSecrets should be a comma-joined list of the names of imagePullSecrets.
	imagePullSecretsReferences := []corev1.LocalObjectReference{}
	if imagePullSecrets != "" {
		secretList := strings.Split(imagePullSecrets, ",")
		for _, secret := range secretList {
			imagePullSecretsReferences = append(imagePullSecretsReferences, corev1.LocalObjectReference{Name: secret})
		}
	}

	alloc := allocator.NewAllocator(mgr.GetClient())
	dpuOptions := dutil.DPUOptions{
		ImagePullSecrets:            imagePullSecretsReferences,
		DPUInstallInterface:         dpuInstallInterface,
		BFCFGTemplateFile:           bfCFGTemplateFile,
		BFBRegistry:                 bfbRegistry,
		CustomCASecretName:          customCASecretName,
		MaxDPUParallelInstallations: maxDPUParallelInstallations,
	}

	setupLog.Info("DPU", "options", dpuOptions)

	dpuMap := dutil.NewDPUInProvisioningMap(maxDPUParallelInstallations)

	if err = (dpu.NewDPUReconciler(
		mgr,
		alloc,
		&dutil.KubeadmBootstrapTokenGenerator{Client: mgr.GetClient()},
		&reboot.DMSPodExecUptimeChecker{},
		dpuOptions,
		dpuMap,
	)).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPU")
		os.Exit(1)
	}
	if err = (&dpuset.DPUSetReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(dpuset.DPUSetControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUSet")
		os.Exit(1)
	}
	if err = (&bfb.BFBReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(bfb.BFBControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BFB")
		os.Exit(1)
	}
	if err = (&dpucluster.DPUClusterReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor(dpucluster.DPUClusterControllerName),
		Allocator: alloc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUCluster")
		os.Exit(1)
	}
	if err = (&dpudevice.DPUDeviceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUDevice")
		os.Exit(1)
	}

	dmsPodOptions := dnutil.HostAgentPodOptions{
		HostAgentImageWithTag: dmsImage,
		ImagePullSecrets:      imagePullSecretsReferences,
		DMSTimeout:            dmsTimeout,
		DMSPodTimeout:         dmsPodTimeout,
		DMSPodEnvs:            dmsPodEnvs,
		BFBRegistryAddress:    bfbRegistry,
	}
	setupLog.Info("DPUNode", "options", dmsPodOptions)

	if err = (&dpunode.NodeReconciler{
		Client:  mgr.GetClient(),
		Options: dmsPodOptions,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}
	if err = (&dpunode.DPUNodeReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor(dpunode.DPUNodeControllerName),
		Options:             dmsPodOptions,
		DPUInstallInterface: &dpuInstallInterface,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUNode")
		os.Exit(1)
	}
	if dpuInstallInterface == string(provisioningv1.InstallViaRedFish) && enableDpuDiscovery {
		if err = (&discovery.DPUDiscoveryReconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DPUDiscovery")
			os.Exit(1)
		}
	}

	dpunodemaintenanceOptions := dpunodemaintenance.DPUNodeMaintenanceOptions{
		MultiDPUOperationsSyncWaitTime: multiDPUOperationsSyncWaitTime,
		MaxUnavailableDPUNodes:         maxUnavailableDPUNodes,
	}

	setupLog.Info("DPUNodeMaintenance", "options", dpunodemaintenanceOptions)

	if err = (&dpunodemaintenance.DPUNodeMaintenanceReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor(dpunodemaintenance.DPUNodeMaintenanceControllerName),
		DPUInstallInterface: &dpuInstallInterface,
		Options:             dpunodemaintenanceOptions,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUNodeMaintenance")
		os.Exit(1)
	}
	if err = (&provisioningwebhooks.BFB{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "BFB")
		os.Exit(1)
	}
	if err = (&provisioningwebhooks.DPUSet{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUSet")
		os.Exit(1)
	}
	if err = (&provisioningwebhooks.DPUFlavor{
		DPUInstallInterface: &dpuInstallInterface,
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUFlavor")
		os.Exit(1)
	}
	if err = (&provisioningwebhooks.DPUDevice{
		Client: mgr.GetClient(),
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUDevice")
		os.Exit(1)
	}
	if err = (&provisioningwebhooks.DPUNode{
		Client: mgr.GetClient(),
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUNode")
		os.Exit(1)
	}

	k8sClient := clientset.NewForConfigOrDie(clientConfig)
	if err = (&csr.CSRReconciler{
		ClientSet:     k8sClient,
		RuntimeClient: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CSR")
		os.Exit(1)
	}

	// Get the context from the signal handler
	ctx := ctrl.SetupSignalHandler()

	if err := mgr.AddHealthzCheck("healthz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Initialize after the cache is running and respect manager shutdown
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		if err := dpuMap.Initialize(ctx, mgr.GetClient()); err != nil {
			return fmt.Errorf("initializing DPUInProvisioningMap: %w", err)
		}

		// Delete all DMS pods at startup for upgrade from 25.7 to 25.10
		if err := deleteDMSPods(ctx, mgr.GetClient()); err != nil {
			setupLog.Error(err, "failed to delete DMS pods at startup")
			// Continue with initialization even if DMS pod deletion fails
		}
		<-ctx.Done() // ensure graceful shutdown
		return nil
	})); err != nil {
		setupLog.Error(err, "unable to register DPUInProvisioningMap init runnable")
		os.Exit(1)
	}

	// Start the manager
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
