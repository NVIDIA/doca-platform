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

package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These variables can be set from the environment when running the DPF tests.
var (
	configPath string
	// testKubeconfig path to be used for this test.
	testKubeconfig string
	// artifactsDir is the path where test artifacts will be stored.
	artifactsDir string

	// collectResources indicates whether to collect logs an objects after an e2e test run.
	collectResources = true
	// externalTest path used to run external tests scripts
	externalTest string
	// enableSOSReports to enable collecting SOS reports after an e2e test run failure.
	enableSOSReports = false
)

var (
	// cleanupFlags holds all flags to control skip cleanup behavior
	cleanupFlags   *cleanup.CleanupFlags
	cleanupTracker *cleanup.Tracker
	testClient     client.Client
	restConfig     *rest.Config
	clientset      *kubernetes.Clientset
	ctx            = ctrl.SetupSignalHandler()
	conf           *config
)

func init() {
	testing.Init() // Initialize Go test flags (required for Go 1.24+)
	flag.StringVar(&testKubeconfig, "e2e.testKubeconfig", "", "path to the testKubeconfig file")
	flag.StringVar(&configPath, "e2e.config", "", "path to the configuration file")
	flag.StringVar(&externalTest, "e2e.externalTestScript", "", "path to the external test file, script will be called in between BeforeSuite setup and AfterSuite cleanup")

	// Register cleanup flags and get handle for it
	cleanupFlags = cleanup.NewCleanupFlagsFromCLI()

	getEnvVariables()
}

func getEnvVariables() {
	if url, found := os.LookupEnv("BFB_IMAGE_URL"); found {
		var err error
		bfbImageURL, err = utils.ResolveBFBImageURL(url)
		if err != nil {
			panic(err)
		}
	}
	if url, found := os.LookupEnv("BFS_OS_ISO_URL"); found {
		bfsOsIsoURL = url
	}
	if url, found := os.LookupEnv("BFS_PLDM_FW_BUNDLE_URL"); found {
		bfsPldmFwBundleURL = url
	}
	if url, found := os.LookupEnv("HBN_IMAGE_URL"); found {
		var err error
		hbnImageURL, err = utils.ResolveHBNImageURL(url)
		if err != nil {
			panic(err)
		}
	}
	if key, found := os.LookupEnv("NGC_API_KEY"); found {
		ngcAPIKey = key
	}
	if v, found := os.LookupEnv("DPF_E2E_COLLECT_RESOURCES"); found {
		var err error
		collectResources, err = strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Errorf("string must be a bool: %v", err))
		}
	}

	if reg, found := os.LookupEnv("HELM_REGISTRY"); found {
		helmRegistry = reg
	} else {
		panic("HELM_REGISTRY env variable must be set")
	}
	if reg, found := os.LookupEnv("DOCKER_IO_REGISTRY"); found {
		dockerIORegistry = reg
	} else {
		panic("DOCKER_IO_REGISTRY env variable must be set")
	}

	if t, found := os.LookupEnv("TAG"); found {
		tag = t
	} else {
		panic("TAG env variable must be set")
	}
	if img, found := os.LookupEnv("NETUTILS_IMAGE"); found {
		netutilsImage = img
	} else {
		panic("NETUTILS_IMAGE env variable must be set")
	}

	// ZeroTrust-only env vars; required-ness enforced in validateFlags() once
	// the ginkgo label filter is known. Reading them here keeps all env-var
	// loading in one place.
	bmcUsername = os.Getenv("E2E_ZT_BMC_USERNAME")
	bmcPassword = os.Getenv("E2E_ZT_BMC_PASSWORD")
	bmcInventoryPath = os.Getenv("E2E_ZT_BMC_INVENTORY_PATH")

	if name, found := os.LookupEnv("DPU_CLUSTER_NAME"); found {
		dpuClusterName = name
	}
	if ns, found := os.LookupEnv("DPU_CLUSTER_NAMESPACE"); found {
		dpuClusterNamespace = ns
	}
	if v, found := os.LookupEnv("ENABLE_SOS_REPORTS"); found {
		var err error
		enableSOSReports, err = strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Errorf("ENABLE_SOS_REPORTS must be a bool: %v", err))
		}
	}
	if path, found := os.LookupEnv("ARTIFACTS_DIR"); found {
		artifactsDir = path
	} else {
		// Default to ../../artifacts relative to the current file.
		_, basePath, _, _ := runtime.Caller(0)
		artifactsDir = filepath.Join(filepath.Dir(basePath), "../../artifacts")
	}

	if interfaceName, found := os.LookupEnv("DPUCLUSTER_INTERFACE"); found {
		dpuClusterInterface = interfaceName
	}
	if host, found := os.LookupEnv("TARGETCLUSTER_API_SERVER_HOST"); found {
		targetClusterAPIServerHost = host
	}

	if ns, found := os.LookupEnv("PREREQS_NAMESPACE"); found {
		// Only set the override if it differs from the default namespace.
		if ns != dpfOperatorSystemNamespace {
			prereqsNamespace = ns
		}
	}
}

// filterBenignPortForwardErrors wraps the global apimachinery error handlers so
// that expected SPDY port-forward failures do not clutter the default output. The
// DPUCluster tunnel (see getDPUClusterClient) is torn down and rebuilt whenever
// its health check fails, which is routine during upgrades as the Kamaji
// control-plane pod rolls and DPU network namespaces churn. Each teardown makes
// client-go call runtime.HandleError, printing "Unhandled Error" lines about
// forwarding, closed listeners, and closed network namespaces.
//
// These are noise, not test failures. Rather than drop them outright they are
// downgraded to GinkgoWriter, which is only surfaced on spec failure or in
// verbose mode, so they stay retrievable when debugging a genuine tunnel problem.
// Every other error is passed through to the default handlers unchanged.
func filterBenignPortForwardErrors() {
	benignSubstrings := []string{
		"an error occurred forwarding",
		"error closing listener",
		"network namespace for sandbox",
	}
	wrapped := utilruntime.ErrorHandlers
	utilruntime.ErrorHandlers = []utilruntime.ErrorHandler{
		func(ctx context.Context, err error, msg string, keysAndValues ...interface{}) {
			if err != nil {
				for _, s := range benignSubstrings {
					if strings.Contains(err.Error(), s) {
						GinkgoWriter.Printf("benign port-forward error (downgraded): %v\n", err)
						return
					}
				}
			}
			for _, fn := range wrapped {
				fn(ctx, err, msg, keysAndValues...)
			}
		},
	}
}

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	g := NewWithT(t)
	defer GinkgoRecover()
	var err error
	GinkgoWriter.Printf("E2E Tests Suite starting...\n\n")
	ctrl.SetLogger(klog.Background())
	filterBenignPortForwardErrors()

	Expect(dpuservicev1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(noderesourcesv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(operatorv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(argov1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(provisioningv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(nvipamv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(kamajiv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(vpcv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(netattdefv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(maintenancev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	s := scheme.Scheme

	// SchemeGroupVersion is group version used to register these objects
	var SchemeGroupVersion = schema.GroupVersion{Group: "", Version: "v1"}

	conf, err = readConfig(configPath)
	g.Expect(err).NotTo(HaveOccurred())

	// If testKubeconfig is not set default it to $HOME/.kube/config
	home, exists := os.LookupEnv("HOME")
	g.Expect(exists).To(BeTrue())
	if testKubeconfig == "" {
		testKubeconfig = filepath.Join(home, ".kube/config")
	}

	// Effective configuration
	GinkgoWriter.Printf("E2E Test Configuration:\n")
	GinkgoWriter.Printf("  configPath: %s\n", configPath)
	GinkgoWriter.Printf("  testKubeconfig: %s\n", testKubeconfig)
	GinkgoWriter.Printf("  numberOfDPUNodes: %d\n", conf.NumberOfDPUNodes)
	GinkgoWriter.Printf("  numberOfDPUsPerNode: %d\n", conf.NumberOfDPUsPerNode)
	GinkgoWriter.Printf("  selectDPUDevicesDynamically: %t\n", conf.SelectDPUDevicesDynamically)
	GinkgoWriter.Printf("  nodeRebootConfigMap: %q\n", conf.NodeRebootConfigMap)
	GinkgoWriter.Printf("  nodeRebootConfigMapPath: %q\n", conf.NodeRebootConfigMapPath)

	// Create a client to use throughout the test.
	restConfig, err = clientcmd.BuildConfigFromFlags("", testKubeconfig)
	g.Expect(err).NotTo(HaveOccurred())
	clientset, err = kubernetes.NewForConfig(restConfig)
	g.Expect(err).NotTo(HaveOccurred())
	testClient, err = client.New(restConfig, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	// Set the path to /api for handling core resources (pods, services, etc)
	// for handling custom resources (deployments, etc) would need to set the API path to /apis
	restConfig.APIPath = "/api"

	// Extend configs to restConfig for hostClusterRESTClient
	restConfig.GroupVersion = &SchemeGroupVersion
	restConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	hostClusterRESTClient, err = rest.RESTClientFor(restConfig)
	g.Expect(err).NotTo(HaveOccurred())
	metricsURI = metrics.GetMetricsURI("kube-state-metrics", dpfOperatorSystemNamespace, kubeStateMetricsPort, "/metrics")
	g.Expect(metricsURI).NotTo(BeEmpty())

	// Auto-enable fail-fast when skip-cleanup-on-failure flag is set
	suiteConfig, _ := GinkgoConfiguration()
	if cleanupFlags.SkipCleanupOnFailure {
		suiteConfig.FailFast = true
		GinkgoWriter.Printf("Auto-enabled fail-fast mode (skip-cleanup-on-failure flag detected)\n")
	}

	RunSpecs(t, "e2e suite", suiteConfig)
}

func skipProvisioning() bool {
	labelFilter := GinkgoLabelFilter()
	hasDpfSystemLabel := strings.Contains(labelFilter, Domain.DPFSystem)
	hasNotDpfSystemLabel := strings.Contains(labelFilter, "!"+Domain.DPFSystem)
	hasNotProvisioningLabel := strings.Contains(labelFilter, "!"+Domain.Provisioning)
	isScaleSelected := Label(Domain.Scale).MatchesLabelFilter(labelFilter)
	isExternalSelected := Label(Domain.ExternalTest).MatchesLabelFilter(labelFilter)

	return (!hasDpfSystemLabel || hasNotDpfSystemLabel || hasNotProvisioningLabel) && !isScaleSelected && !isExternalSelected
}

var _ = BeforeSuite(func() {
	By("Set input")
	SetInput()

	// Initialize cleanup flags here as Ginkgo has parsed CLI arguments before BeforeSuite runs
	cleanupFlags.Init()

	cleanupTracker = cleanup.NewTracker(utils.CleanupWithLabelAndWait, cleanupFlags, ctx, testClient, resourcesToDelete)

	// Upgrade validation tests skip cleanup to preserve resources from previous test run.
	// isUpgradeValidationPhase matches the active label filter against every label
	// registered by validationPhase, so no per-phase update is needed here when a new
	// phase is added.
	if isUpgradeValidationPhase() {
		return
	}

	By("Checking for resources from previous test runs")
	cleanupTracker.WarnIfStaleResources()

	By("Performing before suite cleanup")
	cleanupTracker.HandleScopeLifecycle(nil, cleanup.GinkgoHook.BeforeSuite)

	// Upgrade install phases (validation phases returned above) provision and
	// create everything from their own phase steps; none of the domain-specific
	// setup below applies to them, so the upgrade configs may omit those
	// domains' config fields.
	if isUpgradeInstallPhase() {
		By("Skipping domain-specific BeforeSuite hooks for the upgrade install phase")
		return
	}

	// Label filter examples supported:
	// (Domain.DPFSystem)                  -> all tests with Domain.DPFSystem running. SDN, SNAP included
	// (Domain.Scale)                      -> only Domain.Scale tests running
	// (Domain.DPFSystem && !Domain.SDN)   -> tests with Domain.DPFSystem, excluding Domain.SDN running
	// (Domain.DPFSystem && Domain.SDN)    -> only SDN tests running
	// (Domain.ExternalTest)               -> only prepares DPF environment and involves tests with Domain.ExternalTest
	By(fmt.Sprintf("Running BeforeSuite based on label selector: %v ", GinkgoLabelFilter()))

	if !skipProvisioning() {
		SystemSetupBeforeSuite(false)
		By("Pre-provisioning DPU cluster setup")
		// TODO: can be replaced with functions calls from provisioning_test.go
		// BeforeProvisioning(ctx, input)
		// CreateProvisioningDPUCluster(ctx, input)
		// CreateProvisioningDPUSet(ctx, input)
		provInput := getProvisionDPUClustersInput()
		ProvisionDPUClusters(ctx, provInput)
		ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx, provInput)
		ProvisionDPUSet(ctx, provInput)
	}

	// Apply the ProvisioningBeforeSuite setup if directly specified Provisioning label
	// !skipProvisioning() branch should not be executed in provisioning-only tests
	if isGinkgoLabelApplied(Domain.Provisioning) {
		// SystemSetupBeforeSuite must run first to deploy the DPF operator and system components
		// Provisioning tests need the operator running but will provision DPUs from scratch (no pre-provisioning)
		SystemSetupBeforeSuite(false)
		ProvisioningBeforeSuite()
	}

	// Apply the SDNBeforeSuite setup if not directly specified !SDN
	if !strings.Contains(GinkgoLabelFilter(), "!"+Domain.SDN) {
		SDNBeforeSuite()
	}
	// Apply the SNAPBeforeSuite setup if not directly specified !SNAP
	if !strings.Contains(GinkgoLabelFilter(), "!"+Domain.SNAP) {
		SNAPBeforeSuite()
	}

	// Apply the VPCOVNBeforeSuite BeforeSuite setup
	if !strings.Contains(GinkgoLabelFilter(), "!"+Domain.DPFVPCOVN) {
		VPCOVNBeforeSuite()
	}

	// Apply the WeaveBeforeSuite setup
	if !strings.Contains(GinkgoLabelFilter(), "!"+Domain.Weave) {
		WeaveBeforeSuite(*conf)
	}

	// For Performance + OVNKHBN (physical HBN-OVN performance) scenario, deploy the full
	// HBN-OVN application layer: physical DPUServiceInterfaces (p0, p1, ovn), IPAM pools,
	// HBN DPUServiceTemplate, DPUServiceConfiguration, and ovn-hbn DPUDeployment.
	// On physical environments provisioning runs above so we must also wait for DPUs to be ready.
	// IgnoreAlreadyExists handles objects already present (e.g. on re-runs).
	// Per RDG, service object creation precedes the DPU provisioning wait.
	if isGinkgoLabelApplied(Domain.Performance) && isGinkgoLabelApplied(Domain.OVNKHBN) {
		SystemSetupBeforeSuite(false)
		By("Maximizing maintenance operator parallelism for performance provisioning")
		restoreMaintenanceConfig := SetMaintenanceOperatorMaxParallelOperations(ctx, testClient, 50)
		defer restoreMaintenanceConfig()
		By("Pre-provisioning DPU cluster setup")
		provInput := getProvisionDPUClustersInput()
		ProvisionDPUClusters(ctx, provInput)
		ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx, provInput)
		By("Installing OVN-K resource injector webhook")
		InstallOVNKResourceInjector(ctx, testClient)
		By("Deploying HBN-OVN scenario objects")
		DeployOVNKHBNScenario(ctx, input)
		By("Waiting for DPUs to be provisioned")
		VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
	}
})

var _ = ReportBeforeEach(func(spec SpecReport) {
	// Detect entering scopes and perform "before" cleanup
	cleanupTracker.HandleScopeLifecycle(&spec, cleanup.GinkgoHook.BeforeEach)
})

// reportAfterEach collects diagnostics when a test fails
// This is called directly by some tests (e.g., VPC tests) for explicit failure reporting
func reportAfterEach(spec SpecReport) {
	if spec.Failed() {
		By(fmt.Sprintf("ReportAfterEach: Test %q failed. Collecting resources and logs for the clusters", spec.FullText()))
		collectInput := collectResourcesInput{
			collectResources: collectResources,
			testClient:       testClient,
			clientset:        clientset,
			restConfig:       restConfig,
			artifactsDir:     artifactsDir,
		}
		err := collectKubernetesResources(ctx, collectInput, "failed_tests/"+spec.LeafNodeText)
		if err != nil {
			GinkgoLogr.Error(err, "failed to collect resources and logs for the clusters")
		}

		if isGinkgoLabelApplied(Domain.ZeroTrust) {
			By(fmt.Sprintf("ReportAfterEach: Test %q failed. Collecting BMC logs", spec.FullText()))
			if err = collectBMCLogsZT(ctx, spec.LeafNodeText, artifactsDir, testClient, input); err != nil {
				GinkgoLogr.Error(err, "failed to collect BMC logs")
			}
		}

		// Collect SOS reports if enabled (runs at most once per suite via sync.Once).
		if enableSOSReports {
			if err = collectSOSReports(ctx, artifactsDir); err != nil {
				GinkgoLogr.Error(err, "SOS report collection failed")
			}
		}
	}
}

var _ = ReportAfterEach(func(spec SpecReport) {
	// Collect diagnostics on failure (resources, logs, SOS reports)
	reportAfterEach(spec)

	// Handle scope lifecycle and cleanup
	cleanupTracker.HandleScopeLifecycle(&spec, cleanup.GinkgoHook.AfterEach)
})

var _ = AfterSuite(func() {
	collectInput := collectResourcesInput{
		collectResources: collectResources,
		testClient:       testClient,
		clientset:        clientset,
		restConfig:       restConfig,
		artifactsDir:     artifactsDir,
	}

	By("Collecting resources for the clusters after suite (pre-DPF operator config cleanup)")
	if err := collectKubernetesResources(ctx, collectInput, "pre-dpf-operator-config-cleanup"); err != nil {
		GinkgoLogr.Error(err, "failed to collect resources for the clusters (pre-DPF operator config cleanup)")
	}

	if !cleanupFlags.SkipSuiteCleanupAfter {
		deletionCompleted := false
		defer func() {
			if !deletionCompleted {
				By("Collecting resources for the clusters after suite (DPF operator config deletion stuck)")
				if err := collectKubernetesResources(ctx, collectInput, "dpf-operator-config-deletion-stuck"); err != nil {
					GinkgoLogr.Error(err, "failed to collect resources for the clusters (DPF operator config deletion stuck)")
				}
			}
		}()

		DeleteDPFOperatorConfig(ctx, testClient)
		deletionCompleted = true

		By("Collecting resources for the clusters after suite (post-DPF operator config cleanup)")
		if err := collectKubernetesResources(ctx, collectInput, "post-dpf-operator-config-cleanup"); err != nil {
			GinkgoLogr.Error(err, "failed to collect resources for the clusters (post-DPF operator config cleanup)")
		}

	} else {
		By("Skipping AfterSuite cleanup (DPF operator config)")
	}

	By("Performing final suite cleanup")
	cleanupTracker.HandleScopeLifecycle(nil, cleanup.GinkgoHook.AfterSuite)
})

// validateRequiredConfigFields fails fast when the e2e config file omits a
// field the selected suites load unconditionally. Upgrade phases (install and
// validation) create only the objects they declare and skip the
// domain-specific BeforeSuite hooks, so their configs may omit everything the
// other suites need. Fields loaded only by such a hook (SDN, VPC OVN, Weave)
// are validated where they are loaded instead (see requireConfigField).
func validateRequiredConfigFields() {
	type requiredField struct {
		name  string
		isSet bool
	}
	required := []requiredField{
		{"dpuClusters", len(conf.DPUClusterPaths) > 0},
		{"dpuDeployment", conf.DPUDeploymentPath != ""},
		{"dpuServiceTemplate", conf.DPUServiceTemplatePath != ""},
		{"dpuServiceConfiguration", conf.DPUServiceConfiguration != ""},
		{"ipPoolDPUServiceIPAM", conf.IPPoolDPUServiceIPAMPath != ""},
	}
	if !isUpgradePhase() {
		required = append(required,
			requiredField{"dpuSet", conf.DPUSetPath != nil},
			requiredField{"dpuService", conf.DPUServicePath != nil},
			requiredField{"dpuServiceInterface", conf.DPUServiceInterfacePath != nil},
			requiredField{"dpuServiceChain", conf.DPUServiceChainPath != nil},
			requiredField{"dpuServiceCredentialRequest", conf.DPUServiceCredentialRequestPath != nil},
			requiredField{"cidrPoolDPUServiceIPAM", conf.CIDRPoolDPUServiceIPAMPath != nil},
		)
	}

	missing := []string{}
	for _, field := range required {
		if !field.isSet {
			missing = append(missing, "`"+field.name+"`")
		}
	}
	if len(missing) > 0 {
		panic(fmt.Sprintf("e2e config file must set: %s", strings.Join(missing, ", ")))
	}
}

func validateFlags() {
	if !isGinkgoLabelApplied(Domain.ZeroTrust) {
		return
	}

	if conf.NodeRebootConfigMap == "" {
		panic("ZeroTrust requires `nodeRebootConfigMap` to be set in the e2e config file")
	}
	if conf.NodeRebootConfigMapPath == "" {
		panic("ZeroTrust requires `nodeRebootConfigMapPath` to be set in the e2e config file")
	}
	if bmcUsername == "" {
		panic("ZeroTrust requires E2E_ZT_BMC_USERNAME env var (BMC username used by the in-cluster reboot script)")
	}
	if bmcPassword == "" {
		panic("ZeroTrust requires E2E_ZT_BMC_PASSWORD env var (BMC password used by the in-cluster reboot script)")
	}
	if bmcInventoryPath == "" {
		panic("ZeroTrust requires E2E_ZT_BMC_INVENTORY_PATH env var (path to the lab DPU-serial -> BMC IP inventory YAML)")
	}

	if isGinkgoLabelApplied(Domain.ExternalTest) {
		if len(externalTest) == 0 {
			panic("This script must be provided when External label is present")
		}
	}
}
