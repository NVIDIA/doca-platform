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
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/csr"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/discovery"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpucluster"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpudevice"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunodemaintenance"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	httputils "github.com/nvidia/doca-platform/internal/provisioning/utils/http"
	provisioningwebhooks "github.com/nvidia/doca-platform/internal/provisioning/webhooks"
	"github.com/nvidia/doca-platform/pkg/health"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientset "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// defaultBFBRegistryAddress is the in-cluster service address when --bfb-registry is not set.
// bfb-registry is HTTPS-only and the Service exposes the HTTPS port (see bfbregistry.HTTPSContainerPort).
const defaultBFBRegistryAddress = "bfb-registry:8443"

// DefaultOSInstallTimeout is the provisioning-controller default for OS installation.
// Documented on DPFOperatorConfig.spec.provisioningController.osInstallTimeout when unset.
const DefaultOSInstallTimeout = 60 * time.Minute

// DefaultNodeEffectRemovalTimeout is the provisioning-controller default for Node Effect Removal.
// Documented on DPFOperatorConfig.spec.provisioningController.nodeEffectRemovalTimeout when unset.
const DefaultNodeEffectRemovalTimeout = 0

// DefaultFirmwareUpdateTimeout is the provisioning-controller default for BF4 firmware update.
// Documented on DPFOperatorConfig.spec.provisioningController.firmwareUpdateTimeout when unset.
const DefaultFirmwareUpdateTimeout = 45 * time.Minute

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))

	utilruntime.Must(maintenancev1alpha1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// Add RBAC for the metrics endpoint and for BFBRegistry services.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups="",resources=services,verbs=create;delete;deletecollection;get;list;patch;update;watch

type cliFlags struct {
	metricsAddr                        string
	pprofBindAddr                      string
	enableLeaderElection               bool
	insecureMetrics                    bool
	enableHTTP2                        bool
	probeAddr                          string
	dmsImage                           string
	imagePullSecrets                   string
	bfbPVC                             string
	dmsTimeout                         int
	dmsPodTimeout                      time.Duration
	syncPeriod                         time.Duration
	dpuInstallInterface                string
	bfCFGTemplateFile                  string
	bfbRegistryLoadBalancerAddress     string
	customCASecretName                 string
	dmsPodEnvs                         []string
	maxDPUParallelInstallations        int32
	enableDpuDiscovery                 bool
	multiDPUOperationsSyncWaitTime     time.Duration
	maxUnavailableDPUNodes             int32
	osInstallTimeout                   time.Duration
	osInstallRetries                   int32
	firmwareUpdateTimeout              time.Duration
	preInstallAgentRegistrationTimeout time.Duration
	nodeEffectRemovalTimeout           time.Duration
	hostAgentDNSPolicy                 string
	deploymentMode                     string
	redfishClientCertDir               string
	caTrustBundleConfigMapName         string
}

func parseFlags() *cliFlags {
	flags := &cliFlags{}

	fs.StringVar(&flags.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&flags.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.StringVar(&flags.pprofBindAddr, "pprof-bind-address", "", "The address the pprof endpoint binds to.")
	// --leader-elect defaults to false so `make run` / tests skip the Lease entirely
	// (otherwise startup blocks for ~LeaseDuration waiting for stale leases).
	// Production deployment manifests pass --leader-elect to opt into HA.
	fs.BoolVar(&flags.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&flags.insecureMetrics, "insecure-metrics", false,
		"If set the metrics endpoint is served insecure without AuthN/AuthZ.")
	fs.BoolVar(&flags.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.StringVar(&flags.dmsImage, "dms-image", "", "The image for DMS pod.")
	fs.StringVar(&flags.imagePullSecrets, "image-pull-secrets", "", "The image pull secrets for pods deployed by this controller.")
	fs.StringVar(&flags.bfbPVC, "bfb-pvc", "", "The pvc to storage bfb.")
	fs.IntVar(&flags.dmsTimeout, "dms-timeout", 900, "The max timeout execution in seconds of a command if not responding, 0 is unlimited.")
	fs.DurationVar(&flags.dmsPodTimeout, "dms-pod-timeout", 5*time.Minute, "Timeout for DMS pods")
	fs.DurationVar(&flags.syncPeriod, "sync-period", 10*time.Minute, "The minimum interval at which watched resources are reconciled.")
	fs.StringVar(&flags.dpuInstallInterface, "dpu-install-interface", string(provisioningv1.InstallViaHostAgent), "the interface used to provision DPUs")
	fs.StringVar(&flags.bfCFGTemplateFile, "bf-cfg-template-file", "", "A custom bf.cfg template used as part of DPU provisioning.")
	fs.StringVar(&flags.bfbRegistryLoadBalancerAddress, "bfb-registry-load-balancer-address", "", "load balancer address for the BFB registry from which BFBs are downloaded")
	fs.StringVar(&flags.customCASecretName, "custom-CA-secret", "", "the secret object which containing the custom CA certificate")
	fs.StringSliceVar(&flags.dmsPodEnvs, "dms-pod-envs", []string{}, "environment variables to set in the DMS pod")
	fs.Int32Var(&flags.maxDPUParallelInstallations, "max-dpu-parallel-installations", 50, "The maximum number of DPUs that can be in provisioning at once")
	fs.BoolVar(&flags.enableDpuDiscovery, "enable-dpu-discovery", true, "Enable autmated DPU discovery")
	fs.DurationVar(&flags.multiDPUOperationsSyncWaitTime, "multi-dpu-operations-sync-wait-time", 30*time.Second, "The wait time between DPUs sync operations on the same node")
	fs.Int32Var(&flags.maxUnavailableDPUNodes, "max-unavailable-dpu-nodes", 50, "The maximum number of DPUNodes that are unavailable during the node effect period")
	fs.DurationVar(&flags.osInstallTimeout, "os-install-timeout", DefaultOSInstallTimeout, "Maximum time allowed for OS installation in zero-trust mode")
	fs.Int32Var(&flags.osInstallRetries, "os-install-retries", dutil.DefaultOSInstallRetries, "Maximum number of retryable OS installation attempts in zero-trust mode before transitioning to Error. Defaults to 2 when unset")
	fs.DurationVar(&flags.firmwareUpdateTimeout, "firmware-update-timeout", DefaultFirmwareUpdateTimeout, "Maximum time allowed for BF4 firmware update in zero-trust mode")
	fs.DurationVar(&flags.preInstallAgentRegistrationTimeout, "pre-install-agent-registration-timeout", 30*time.Second, "How long Initializing waits for preInstall.agentReported during reprovisioning")
	fs.DurationVar(&flags.nodeEffectRemovalTimeout, "node-effect-removal-timeout", DefaultNodeEffectRemovalTimeout, "Maximum time allowed for the Node Effect Removal phase before transitioning to error. 0 means no timeout.")
	fs.StringVar(&flags.hostAgentDNSPolicy, "hostagent-dns-policy", string(corev1.DNSClusterFirstWithHostNet), "DNS policy for the hostagent pod")
	fs.StringVar(&flags.deploymentMode, "deployment-mode", "", "required: cluster deployment mode from DPFOperatorConfig (zero-trust or host-trusted)")
	fs.StringVar(&flags.redfishClientCertDir, "redfish-client-cert-dir", rfclient.DefaultClientCertDir, "Directory holding the mounted Redfish client key pair (tls.crt/tls.key). The BF4 key pair is read from '<dir>-bf4'.")
	fs.StringVar(&flags.caTrustBundleConfigMapName, "ca-trust-bundle-configmap-name", operatorv1.DefaultCATrustBundleConfigMapName, "Name of the ConfigMap holding the dpf CA trust bundle mounted into the HostAgent to validate the bfb-registry server certificate.")

	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())

	return flags
}

func createManager(flags *cliFlags) (ctrl.Manager, *rest.Config) {
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
	if !flags.enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	metricsOpts := metricsserver.Options{
		BindAddress:    flags.metricsAddr,
		SecureServing:  true,
		FilterProvider: filters.WithAuthenticationAndAuthorization,
	}
	if flags.insecureMetrics {
		metricsOpts.SecureServing = false
		metricsOpts.FilterProvider = nil
	}

	clientConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOpts,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: flags.probeAddr,
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
			},
		},
		Cache: cache.Options{
			SyncPeriod: &flags.syncPeriod,
		},
		PprofBindAddress: flags.pprofBindAddr,
		LeaderElection:   flags.enableLeaderElection,
		LeaderElectionID: "provisioning.dpu.nvidia.com",
		// Release the lease on shutdown so a standby replica can take over within
		// seconds. Safe because main() exits immediately after mgr.Start returns.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	return mgr, clientConfig
}

func resolveBFBRegistry(flags *cliFlags) (string, error) {
	// If the load balancer address is set, use it to build the bfb-registry address
	registryAddress := ""
	if flags.bfbRegistryLoadBalancerAddress == "" {
		if flags.dpuInstallInterface == string(provisioningv1.InstallViaRedFish) {
			nodeIP := os.Getenv("NODE_IP")
			if nodeIP == "" {
				return "", fmt.Errorf("NODE_IP is empty, can not build the bfb-registry address")
			}
			registryAddress = "https://" + nodeIP
		} else {
			registryAddress = defaultBFBRegistryAddress
		}
	} else {
		registryAddress = flags.bfbRegistryLoadBalancerAddress
	}
	registryAddress = httputils.EnsureHTTPSScheme(registryAddress)
	setupLog.Info("bfb-registry address for downloading BFB files", "bfbRegistry", registryAddress)
	return registryAddress, nil
}

func setupControllers(mgr ctrl.Manager, flags *cliFlags, bfbRegistry string, imagePullSecretsReferences []corev1.LocalObjectReference) *dutil.DPUInProvisioningMap {
	alloc := allocator.NewAllocator(mgr.GetClient())
	dpuOptions := dutil.DPUOptions{
		ImagePullSecrets:                   imagePullSecretsReferences,
		DPUInstallInterface:                flags.dpuInstallInterface,
		DeploymentMode:                     flags.deploymentMode,
		BFCFGTemplateFile:                  flags.bfCFGTemplateFile,
		BFBRegistry:                        bfbRegistry,
		BFBPVC:                             flags.bfbPVC,
		BFBRegistryLoadBalancer:            flags.bfbRegistryLoadBalancerAddress,
		CustomCASecretName:                 flags.customCASecretName,
		KubernetesAPIServerVIP:             bfbregistry.APIServerVIPFromDMSPodEnvs(flags.dmsPodEnvs),
		MaxDPUParallelInstallations:        flags.maxDPUParallelInstallations,
		OSInstallTimeout:                   flags.osInstallTimeout,
		OSInstallRetries:                   flags.osInstallRetries,
		FirmwareUpdateTimeout:              flags.firmwareUpdateTimeout,
		PreInstallAgentRegistrationTimeout: flags.preInstallAgentRegistrationTimeout,
		NodeEffectRemovalTimeout:           flags.nodeEffectRemovalTimeout,
	}

	setupLog.Info("DPU", "options", dpuOptions)

	dpuMap := dutil.NewDPUInProvisioningMap(flags.maxDPUParallelInstallations)

	if err := dpu.NewDPUReconciler(
		mgr,
		alloc,
		&dutil.KubeadmBootstrapTokenGenerator{Client: mgr.GetClient()},
		&state.DefaultDPUArtifactGenerator{},
		&reboot.DMSPodExecUptimeChecker{},
		dpuOptions,
		dpuMap,
	).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPU")
		os.Exit(1)
	}
	if err := (&dpuset.DPUSetReconciler{
		Client: mgr.GetClient(),
		Options: dpuset.DPUSetOptions{
			DPUInstallInterface: flags.dpuInstallInterface,
		},
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(dpuset.DPUSetControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUSet")
		os.Exit(1)
	}
	if err := (&bfb.BFBReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(bfb.BFBControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BFB")
		os.Exit(1)
	}
	if err := (&bluefieldsoftware.BlueFieldSoftwareReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(bluefieldsoftware.BlueFieldSoftwareControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BlueFieldSoftware")
		os.Exit(1)
	}
	if err := (&dpucluster.DPUClusterReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor(dpucluster.DPUClusterControllerName),
		Allocator: alloc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUCluster")
		os.Exit(1)
	}
	// When a client cert directory is configured the verified mTLS client reads its client key pair
	// from the mounted volume instead of the Kubernetes API (from a secret in the same namespace).
	rfclient.SetClientCertDir(flags.redfishClientCertDir)
	if err := (&dpudevice.DPUDeviceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor(dpudevice.DPUDeviceControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUDevice")
		os.Exit(1)
	}

	// Step 8: pass bfb-registry-address to DMS options (already defaulted above when empty).
	dmsPodOptions := dnutil.HostAgentPodOptions{
		HostAgentImageWithTag:      flags.dmsImage,
		ImagePullSecrets:           imagePullSecretsReferences,
		DMSTimeout:                 flags.dmsTimeout,
		DMSPodTimeout:              flags.dmsPodTimeout,
		DMSPodEnvs:                 flags.dmsPodEnvs,
		BFBRegistryAddress:         bfbRegistry,
		HostAgentDNSPolicy:         corev1.DNSPolicy(flags.hostAgentDNSPolicy),
		CATrustBundleConfigMapName: flags.caTrustBundleConfigMapName,
	}
	setupLog.Info("DPUNode", "options", dmsPodOptions)

	if err := (&dpunode.NodeReconciler{
		Client:  mgr.GetClient(),
		Options: dmsPodOptions,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}
	if err := (&dpunode.DPUNodeReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor(dpunode.DPUNodeControllerName),
		Options:             dmsPodOptions,
		DPUInstallInterface: &flags.dpuInstallInterface,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUNode")
		os.Exit(1)
	}
	if flags.dpuInstallInterface == string(provisioningv1.InstallViaRedFish) && flags.enableDpuDiscovery {
		if err := (&discovery.DPUDiscoveryReconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DPUDiscovery")
			os.Exit(1)
		}
	}

	dpunodemaintenanceOptions := dpunodemaintenance.DPUNodeMaintenanceOptions{
		MultiDPUOperationsSyncWaitTime: flags.multiDPUOperationsSyncWaitTime,
		MaxUnavailableDPUNodes:         flags.maxUnavailableDPUNodes,
	}

	setupLog.Info("DPUNodeMaintenance", "options", dpunodemaintenanceOptions)

	if err := (&dpunodemaintenance.DPUNodeMaintenanceReconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor(dpunodemaintenance.DPUNodeMaintenanceControllerName),
		DPUInstallInterface: &flags.dpuInstallInterface,
		Options:             dpunodemaintenanceOptions,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DPUNodeMaintenance")
		os.Exit(1)
	}

	return dpuMap
}

func setupWebhooks(mgr ctrl.Manager, deploymentMode string) {
	if err := (&provisioningwebhooks.BFB{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "BFB")
		os.Exit(1)
	}
	if err := (&provisioningwebhooks.DPUSet{
		DeploymentMode: deploymentMode,
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUSet")
		os.Exit(1)
	}
	if err := (&provisioningwebhooks.DPUFlavor{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUFlavor")
		os.Exit(1)
	}
	if err := (&provisioningwebhooks.DPUDevice{
		Client: mgr.GetClient(),
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUDevice")
		os.Exit(1)
	}
	if err := (&provisioningwebhooks.DPUNode{
		Client: mgr.GetClient(),
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUNode")
		os.Exit(1)
	}
	if err := (&provisioningwebhooks.DPUDiscoveryValidator{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DPUDiscovery")
		os.Exit(1)
	}
}

func setupCSRController(mgr ctrl.Manager, clientConfig *rest.Config) {
	k8sClient := clientset.NewForConfigOrDie(clientConfig)
	if err := (&csr.CSRReconciler{
		ClientSet:     k8sClient,
		RuntimeClient: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CSR")
		os.Exit(1)
	}
}

func setupHealthChecks(mgr ctrl.Manager, ctx context.Context) {
	if err := mgr.AddHealthzCheck("healthz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", health.APIConnectionCheck(ctx, mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
}

func setupInitRunnable(mgr ctrl.Manager, dpuMap *dutil.DPUInProvisioningMap) {
	// Initialize after the cache is running and respect manager shutdown
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		if err := dpuMap.Initialize(ctx, mgr.GetClient()); err != nil {
			return fmt.Errorf("initializing DPUInProvisioningMap: %w", err)
		}
		<-ctx.Done() // ensure graceful shutdown
		return nil
	})); err != nil {
		setupLog.Error(err, "unable to register DPUInProvisioningMap init runnable")
		os.Exit(1)
	}
}

func main() {
	flags := parseFlags()

	if strings.TrimSpace(flags.dpuInstallInterface) == "" {
		setupLog.Error(nil, "--dpu-install-interface must not be empty")
		os.Exit(1)
	}
	if strings.TrimSpace(flags.deploymentMode) == "" {
		setupLog.Error(nil, "--deployment-mode is required (zero-trust or host-trusted)")
		os.Exit(1)
	}

	mgr, clientConfig := createManager(flags)

	// imagePullSecrets should be a comma-joined list of the names of imagePullSecrets.
	imagePullSecretsReferences := []corev1.LocalObjectReference{}
	if flags.imagePullSecrets != "" {
		secretList := strings.Split(flags.imagePullSecrets, ",")
		for _, secret := range secretList {
			imagePullSecretsReferences = append(imagePullSecretsReferences, corev1.LocalObjectReference{Name: secret})
		}
	}

	bfbRegistryAddress, err := resolveBFBRegistry(flags)
	if err != nil {
		setupLog.Error(err, "unable to resolve bfb-registry address", "bfbRegistry", bfbRegistryAddress)
		os.Exit(1)
	}

	dpuMap := setupControllers(mgr, flags, bfbRegistryAddress, imagePullSecretsReferences)
	setupWebhooks(mgr, flags.deploymentMode)
	setupCSRController(mgr, clientConfig)

	// Get the context from the signal handler
	ctx := ctrl.SetupSignalHandler()

	setupHealthChecks(mgr, ctx)
	setupInitRunnable(mgr, dpuMap)

	if err := mgr.Add(&bfbregistry.BFBRegistryRunnable{
		Client:                 mgr.GetClient(),
		BFBPVC:                 flags.bfbPVC,
		ImagePullSecrets:       imagePullSecretsReferences,
		KubernetesAPIServerVIP: bfbregistry.APIServerVIPFromDMSPodEnvs(flags.dmsPodEnvs),
	}); err != nil {
		setupLog.Error(err, "unable to register bfb-registry runnable")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
