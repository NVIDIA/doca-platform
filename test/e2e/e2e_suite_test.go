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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/api/kamaji/api/v1alpha1"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	flag.StringVar(&testKubeconfig, "e2e.testKubeconfig", "", "path to the testKubeconfig file")
	flag.StringVar(&configPath, "e2e.config", "", "path to the configuration file")
	flag.StringVar(&externalTest, "e2e.externalTestScript", "", "path to the external test file, script will be called in between BeforeSuite setup and AfterSuite cleanup")

	getEnvVariables()
}

// These variables can be set from the environment when running the DPF tests.
var (
	configPath string
	// testKubeconfig path to be used for this test.
	testKubeconfig string

	// skipCleanup indicates whether to skip the cleanup of resources created during the e2e test run.
	// When set to true, resources will not be removed after the test completes.
	skipCleanup = false
	// collectResources indicates whether to collect logs an objects after an e2e test run.
	collectResources = true
	// externalTest path used to run external tests scripts
	externalTest string
)

func getEnvVariables() {
	if v, found := os.LookupEnv("E2E_SKIP_CLEANUP"); found {
		var err error
		skipCleanup, err = strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Errorf("string must be a bool: %v", err))
		}
	}
	if url, found := os.LookupEnv("BFB_IMAGE_URL"); found {
		var err error
		bfbImageURL, err = utils.ResolveBFBImageURL(url)
		if err != nil {
			panic(err)
		}
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

	if t, found := os.LookupEnv("TAG"); found {
		tag = t
	} else {
		panic("TAG env variable must be set")
	}
}

var (
	testClient client.Client
	restConfig *rest.Config
	clientset  *kubernetes.Clientset
	ctx        = ctrl.SetupSignalHandler()
	conf       *config
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	g := NewWithT(t)
	defer GinkgoRecover()
	var err error
	_, err = fmt.Fprintf(GinkgoWriter, "Starting dpf-operator suite\n")
	Expect(err).ToNot(HaveOccurred())
	ctrl.SetLogger(klog.Background())

	Expect(dpuservicev1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(operatorv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(argov1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(provisioningv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(nvipamv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(kamajiv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(vpcv1.AddToScheme(scheme.Scheme)).To(Succeed())
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

	// Extend configs to restConfig for testRESTClient
	restConfig.GroupVersion = &SchemeGroupVersion
	restConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	testRESTClient, err = rest.RESTClientFor(restConfig)
	g.Expect(err).NotTo(HaveOccurred())
	metricsURI = metrics.GetMetricsURI("kube-state-metrics", dpfOperatorSystemNamespace, 8080, "/metrics")
	g.Expect(metricsURI).NotTo(BeEmpty())

	RunSpecs(t, "e2e suite")
}

func skipProvisioning() bool {
	labelFilter := GinkgoLabelFilter()
	hasDpfSystemLabel := strings.Contains(labelFilter, dpfSystemLabel)
	hasNotDpfSystemLabel := strings.Contains(labelFilter, "!"+dpfSystemLabel)
	hasNotProvisioningLabel := strings.Contains(labelFilter, "!"+provisioningLabel)
	isScaleSelected := Label(scaleLabel).MatchesLabelFilter(labelFilter)
	isExternalSelected := Label(externalTestLabel).MatchesLabelFilter(labelFilter)

	return (!hasDpfSystemLabel || hasNotDpfSystemLabel || hasNotProvisioningLabel) && !isScaleSelected && !isExternalSelected
}

var _ = BeforeSuite(func() {
	By("set input")
	SetInput()

	By("cleaning up objects created during recent tests")
	if Label(dpfUpgradeValidationTestLabel).MatchesLabelFilter(GinkgoLabelFilter()) {
		return
	}
	if !skipCleanup {
		Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterEachCleanupLabels), resourcesToDelete...)).To(Succeed())
		Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterAllCleanupLabels), resourcesToDelete...)).To(Succeed())
	}

	// Label filter examples supported:
	//(dpfSystemLabel) -> all test with dpfSystemLabel running. SDN, SNAP included
	//(scaleLabel) -> Only scaleLabel tests running
	//(dpfSystemLabel && !sdnLabel) -> test with dpfSystemLabel, excluding sdnLabel running
	//(dpfSystemLabel && sdnLabel) -> only SDN tests running
	//(externalTestLabel) -> only prepares DPF environment and involves tests with externalTestLabel
	By(fmt.Sprintf("Run BeforeSuite based on label selector: %v ", GinkgoLabelFilter()))
	if !skipProvisioning() {
		SystemSetupBeforeSuite()
		By("Pre provisioning DPU cluster setup")
		ProvisionDPUCluster(ctx, getProvisionDPUClustersInput())
		ProvisionDPUSet(ctx, getProvisionDPUClustersInput())
	}
	// Apply the ProvisioningBeforeSuite setup if directly specified provisioningLabel
	if Label(provisioningLabel).MatchesLabelFilter(GinkgoLabelFilter()) && !strings.Contains(GinkgoLabelFilter(), "!"+provisioningLabel) {
		ProvisioningBeforeSuite()
	}
	// Apply the OVNKHBNBeforeSuite setup if directly specified OVNKPrimaryLabel
	if Label(ovnkPrimaryLabel).MatchesLabelFilter(GinkgoLabelFilter()) {
		OVNKHBNBeforeSuite()
	}
	// Apply the SDNBeforeSuite setup if not directly specified !SDN
	if !strings.Contains(GinkgoLabelFilter(), "!"+sdnLabel) {
		SDNBeforeSuite()
	}
	// Apply the SNAPBeforeSuite setup if not directly specified !SNAP
	if !strings.Contains(GinkgoLabelFilter(), "!"+snapLabel) {
		SNAPBeforeSuite()
	}

	// Apply the VPCOVNBeforeSuite BeforeSuite setup
	if !strings.Contains(GinkgoLabelFilter(), "!"+dpfVPCTestLabel) {
		VPCOVNBeforeSuite()
	}
})

var _ = AfterSuite(func() {
	if skipCleanup {
		return
	}
	By("JustAfterSuite: Tests finished, cleaning up DPF operator config")
	DeleteDPFOperatorConfig(ctx, testClient)
})

var _ = ReportAfterEach(func(spec SpecReport) {
	// Check if the test failed
	if spec.Failed() {
		// Collect and print logs (you can also write to a file or another sink)
		By(fmt.Sprintf("ReportAfterEach: Test %q failed. Collecting resources and logs for the clusters", spec.FullText()))
		collectInput := collectResourcesInput{
			collectResources: collectResources,
			testClient:       testClient,
			clientset:        clientset,
			restConfig:       restConfig,
		}
		err := collectResourcesAndLogs(ctx, collectInput, "failed_tests/"+spec.LeafNodeText)
		if err != nil {
			// Don't fail the test if the log collector fails - just print the errors.
			GinkgoLogr.Error(err, "failed to collect resources and logs for the clusters")
		}
	}
	if skipCleanup {
		return
	}

	// Cleanup objects for a spec that ran. Ignore Skipped or Pending specs.
	if !spec.State.Is(types.SpecStateSkipped) && !spec.State.Is(types.SpecStatePending) {
		By("cleaning up objects created during the test")
		Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterEachCleanupLabels), resourcesToDelete...)).To(Succeed())
	}
})

var _ = ReportAfterSuite("My Suite", func(report Report) {
	// Collect and print logs (you can also write to a file or another sink)
	By("collecting resources and logs for the clusters after suite")
	collectInput := collectResourcesInput{
		collectResources: collectResources,
		testClient:       testClient,
		clientset:        clientset,
		restConfig:       restConfig,
	}
	err := collectResourcesAndLogs(ctx, collectInput, "final")
	if err != nil {
		// Don't fail the test if the log collector fails - just print the errors.
		GinkgoLogr.Error(err, "failed to collect resources and logs for the clusters")
	}

	if skipCleanup {
		return
	}
	By("cleaning up objects created during the test suite execution")
	Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterEachCleanupLabels), resourcesToDelete...)).To(Succeed())
	Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterAllCleanupLabels), resourcesToDelete...)).To(Succeed())
})
