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

//nolint:goconst
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl"
	operatorutils "github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils/tunnel"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/collector"
	dpuflavorutils "github.com/nvidia/doca-platform/test/utils/dpuflavor"
	"github.com/nvidia/doca-platform/test/utils/refreshableclient"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProvisionDPUClustersInput struct {
	numberOfDPUNodes            int
	numberOfDPUsPerNode         int
	dpuClusterPrerequisites     []client.Object
	dpuClusters                 []*provisioningv1.DPUCluster
	dpuFlavor                   *provisioningv1.DPUFlavor
	bfb                         *provisioningv1.BFB
	blueFieldSoftware           *provisioningv1.BlueFieldSoftware
	dpuSet                      *provisioningv1.DPUSet
	client                      client.Client
	bfbImageURL                 string
	bfsOsIsoURL                 string
	bfsPldmFwBundles            map[string]string
	bfsNicFwURL                 string
	restConfig                  *rest.Config
	NodeRebootConfigMap         string
	DPUNodeBMCs                 map[string]string
	expectedKubernetesVersion   string
	selectDPUDevicesDynamically bool
	operatorConfig              *operatorv1.DPFOperatorConfig
}

func isPreUpgradeFromLastReleasedGA(ctx context.Context, kclient client.Client, objectKey client.ObjectKey) (bool, error) {
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
	if err := kclient.Get(ctx, objectKey, dpfOperatorConfig); err != nil {
		return false, err
	}
	if dpfOperatorConfig.Status.Version == nil {
		return false, fmt.Errorf("DPFOperatorConfig %s status.version must be set before comparing", objectKey)
	}
	return operatorutils.IsUpgradeFromLastReleasedGA(*dpfOperatorConfig.Status.Version), nil
}

// systemTestInput represents the fully loaded and processed test environment.
// This struct contains actual Kubernetes API objects and runtime configuration
// that are ready for use in end-to-end tests.
//
// - Contains parsed and loaded Kubernetes manifests from YAML files
//   - Populated by applyConfig() and applySDNConfig() from config struct
//
// - Passed to individual test functions as the primary test context
// - Provides all necessary objects and configuration for test execution
// - Depends on `config` struct for file paths and basic configuration
//
// Fields are grouped to mirror the config struct; suite-specific fields stay
// nil unless the selected suites load them.
type systemTestInput struct {
	// Loaded from the config fields required by every suite.
	dpuClusters             []*provisioningv1.DPUCluster
	dpuDeployment           *dpuservicev1.DPUDeployment
	dpuServiceConfiguration *dpuservicev1.DPUServiceConfiguration
	dpuServiceTemplate      *dpuservicev1.DPUServiceTemplate
	ipPoolDPUServiceIPAM    *dpuservicev1.DPUServiceIPAM

	// Loaded from the config fields required by every suite except upgrade
	// phases; nil when the config omits them.
	cidrDPUServiceIPAM          *dpuservicev1.DPUServiceIPAM
	dpuService                  *dpuservicev1.DPUService
	dpuServiceChain             *dpuservicev1.DPUServiceChain
	dpuServiceCredentialRequest *dpuservicev1.DPUServiceCredentialRequest
	dpuServiceInterface         *dpuservicev1.DPUServiceInterface
	dpuSet                      *provisioningv1.DPUSet

	// SDN suite only; set by applySDNConfig, nil otherwise.
	dpuServiceChainTemplate     *dpuservicev1.DPUServiceChain
	dpuServiceHBN               *dpuservicev1.DPUService
	dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface
	dpuServiceIPAMTemplate      *dpuservicev1.DPUServiceIPAM
	dpuServiceNAD               *dpuservicev1.DPUServiceNAD

	// OVN Kubernetes/HBN performance scenario; nil (or empty) unless the
	// config sets the corresponding paths.
	dpuServiceConfigurationHBN *dpuservicev1.DPUServiceConfiguration
	dpuServiceConfigurationOVN *dpuservicev1.DPUServiceConfiguration
	dpuServiceInterfaceOVN     *dpuservicev1.DPUServiceInterface
	dpuServiceInterfacesHBN    []*dpuservicev1.DPUServiceInterface
	dpuServiceTemplateHBN      *dpuservicev1.DPUServiceTemplate
	dpuServiceTemplateOVN      *dpuservicev1.DPUServiceTemplate
	ovnCredentialRequest       *dpuservicev1.DPUServiceCredentialRequest

	// Upgrade suite: the extra DPUServiceTemplate/DPUServiceConfiguration
	// revision the upgrade phases roll the DPUDeployment to.
	additionalDPUServiceConfiguration *dpuservicev1.DPUServiceConfiguration
	additionalDPUServiceTemplate      *dpuservicev1.DPUServiceTemplate

	// Provisioning objects and environment settings.
	bfb                                 *provisioningv1.BFB
	blueFieldSoftware                   *provisioningv1.BlueFieldSoftware
	dpuClusterPrerequisites             []client.Object
	dpuDiscovery                        *provisioningv1.DPUDiscovery
	dpuFlavor                           *provisioningv1.DPUFlavor
	dpuFlavorTemplate                   *provisioningv1.DPUFlavorTemplate
	nodeRebootConfigMap                 string
	nodeRebootConfigMapPath             string
	numberOfDPUNodes                    int
	numberOfDPUsPerNode                 int
	numberOfCXsToConfigureViaBF4PerNode int
	pvc                                 *corev1.PersistentVolumeClaim
	rshimConsoleCollector               *appsv1.DaemonSet
	selectDPUDevicesDynamically         bool
	useExternalNodeReboot               bool

	// Runtime state assembled by SetInput and the suite, not read from the
	// config file.
	bfbImageURL      string
	bfsOsIsoURL      string
	bfsPldmFwBundles map[string]string
	bfsNicFwURL      string
	cleanupFlags     *cleanup.CleanupFlags
	client           client.Client
	config           *operatorv1.DPFOperatorConfig
	dpuNodeBMCs      map[string]string
	namespace        string
	pullSecretNames  []string
	restConfig       *rest.Config
}

// unstructuredFromFile loads the manifest at path as an unstructured object
// and stamps it with the suite cleanup labels.
func unstructuredFromFile(path string) *unstructured.Unstructured {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	obj.SetLabels(CleanupScope.Suite)
	return obj
}

// objectFromFile decodes the manifest at path into a new T. It goes through
// unstructuredFromFile so the suite cleanup labels are applied.
func objectFromFile[T any](path string) *T {
	obj := new(T)
	u := unstructuredFromFile(path)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj)).To(Succeed())
	return obj
}

// optionalObjectFromFile returns nil when the e2e config field is unset.
func optionalObjectFromFile[T any](path *string) *T {
	if path == nil {
		return nil
	}
	return objectFromFile[T](*path)
}

// requiredObjectFromFile fails with the e2e config field name when a field
// the selected suite needs is unset, then loads it.
func requiredObjectFromFile[T any](suite, fieldName string, path *string) *T {
	return objectFromFile[T](requireConfigField(suite, fieldName, path))
}

// requiredObjectFromFilePath is like requiredObjectFromFile for non-pointer config paths.
func requiredObjectFromFilePath[T any](suite, fieldName, path string) *T {
	return objectFromFile[T](requireConfigFieldPath(suite, fieldName, path))
}

// requireConfigField fails when the given e2e config field is unset and
// returns the configured path otherwise. Suite-specific loaders use it so a
// config missing a field the selected suite needs fails fast with the field
// name instead of an unreadable file error. The suite name tells the operator
// which label to exclude if the suite was selected unintentionally (e.g. !SDN).
func requireConfigField(suite, fieldName string, path *string) string {
	Expect(path).ToNot(BeNil(),
		"e2e config file must set `%s` to run %s tests (set it or exclude the tests with the label filter !%s)", fieldName, suite, suite)
	return *path
}

// requireConfigFieldPath is like requireConfigField for non-pointer config paths.
func requireConfigFieldPath(suite, fieldName, path string) string {
	Expect(path).ToNot(BeEmpty(),
		"e2e config file must set `%s` to run %s tests (set it or exclude the tests with the label filter !%s)", fieldName, suite, suite)
	return path
}

func (t *systemTestInput) applySDNConfig(conf config) {
	t.dpuServiceInterfaceTemplate = requiredObjectFromFile[dpuservicev1.DPUServiceInterface](Domain.SDN, "dpuServiceInterfaceTemplate", conf.DPUServiceInterfaceTemplatePath)
	t.dpuServiceIPAMTemplate = requiredObjectFromFile[dpuservicev1.DPUServiceIPAM](Domain.SDN, "dpuServiceIPAMTemplate", conf.DPUServiceIPAMTemplatePath)
	t.dpuServiceNAD = requiredObjectFromFile[dpuservicev1.DPUServiceNAD](Domain.SDN, "dpuServiceNAD", conf.DPUServiceNADPath)
	t.dpuServiceChainTemplate = requiredObjectFromFile[dpuservicev1.DPUServiceChain](Domain.SDN, "dpuServiceChainTemplate", conf.DPUServiceChainTemplatePath)

	// The HBN DPUService takes its overrides on the unstructured object, so it
	// cannot go through the typed loaders.
	svcHBN := unstructuredFromFile(requireConfigField(Domain.SDN, "dpuServiceHBN", conf.DPUServiceHBNPath))

	// Override HBN image if HBN_IMAGE_URL is set
	if hbnImageURL != "" {
		parts := strings.SplitN(hbnImageURL, ":", 2)
		setNestedField(svcHBN, parts[0], "spec", "helmChart", "values", "image", "repository")
		setNestedField(svcHBN, parts[1], "spec", "helmChart", "values", "image", "tag")
	}
	if repoURL, found := os.LookupEnv("HBN_CHART_REPO"); found {
		setNestedField(svcHBN, repoURL, "spec", "helmChart", "source", "repoURL")
	}
	if chartVersion, found := os.LookupEnv("HBN_CHART_VERSION"); found {
		setNestedField(svcHBN, chartVersion, "spec", "helmChart", "source", "version")
	}

	if ngcAPIKey != "" {
		updateImagePullSecret(svcHBN, ngcPullSecretName)
	}

	dpuServiceHBN := &dpuservicev1.DPUService{}
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcHBN.Object, dpuServiceHBN)).To(Succeed())
	t.dpuServiceHBN = dpuServiceHBN
}

// setNestedField sets a nested string field on an unstructured object,
// failing the test on error.
func setNestedField(obj *unstructured.Unstructured, value string, fields ...string) {
	Expect(unstructured.SetNestedField(obj.Object, value, fields...)).To(Succeed())
}

func updateImagePullSecret(svc *unstructured.Unstructured, secretName string) {
	pullSecrets, found, err := unstructured.NestedSlice(svc.Object, "spec", "helmChart", "values", "imagePullSecrets")
	Expect(err).ToNot(HaveOccurred())
	if !found {
		pullSecrets = make([]interface{}, 0)
	}

	pullSecrets = append(pullSecrets, map[string]interface{}{"name": secretName})

	err = unstructured.SetNestedSlice(svc.Object, pullSecrets, "spec", "helmChart", "values", "imagePullSecrets")
	Expect(err).ToNot(HaveOccurred())
}

func (t *systemTestInput) applyConfig(conf config) {
	t.bfb = optionalObjectFromFile[provisioningv1.BFB](conf.BFBPath)
	t.blueFieldSoftware = optionalObjectFromFile[provisioningv1.BlueFieldSoftware](conf.BlueFieldSoftwarePath)
	t.dpuSet = optionalObjectFromFile[provisioningv1.DPUSet](conf.DPUSetPath)
	t.pvc = optionalObjectFromFile[corev1.PersistentVolumeClaim](conf.ProvisioningControllerPVCPath)
	t.rshimConsoleCollector = optionalObjectFromFile[appsv1.DaemonSet](conf.RshimConsoleCollectorPath)

	// Load all DPU clusters
	t.dpuClusters = make([]*provisioningv1.DPUCluster, 0, len(conf.DPUClusterPaths))
	for _, dpuClusterPath := range conf.DPUClusterPaths {
		dpuCluster := objectFromFile[provisioningv1.DPUCluster](dpuClusterPath)
		// Override interface if DPUCLUSTER_INTERFACE environment variable is set
		if dpuClusterInterface != "" && dpuCluster.Spec.ClusterEndpoint != nil && dpuCluster.Spec.ClusterEndpoint.Keepalived != nil {
			By(fmt.Sprintf("Overriding DPUCluster interface with DPUCLUSTER_INTERFACE=%s", dpuClusterInterface))
			dpuCluster.Spec.ClusterEndpoint.Keepalived.Interface = dpuClusterInterface
		}
		t.dpuClusters = append(t.dpuClusters, dpuCluster)
	}

	t.dpuDiscovery = optionalObjectFromFile[provisioningv1.DPUDiscovery](conf.DPUDiscoveryPath)
	t.dpuServiceInterface = optionalObjectFromFile[dpuservicev1.DPUServiceInterface](conf.DPUServiceInterfacePath)
	t.dpuService = optionalObjectFromFile[dpuservicev1.DPUService](conf.DPUServicePath)

	dpuClusterPrerequisiteObjects := []client.Object{}
	for _, path := range conf.DPUClusterPrerequisiteObjectPaths {
		dpuClusterPrerequisiteObjects = append(dpuClusterPrerequisiteObjects, unstructuredFromFile(path))
	}
	t.dpuClusterPrerequisites = dpuClusterPrerequisiteObjects

	t.dpuServiceTemplate = objectFromFile[dpuservicev1.DPUServiceTemplate](conf.DPUServiceTemplatePath)
	t.dpuServiceConfiguration = objectFromFile[dpuservicev1.DPUServiceConfiguration](conf.DPUServiceConfiguration)
	t.additionalDPUServiceTemplate = optionalObjectFromFile[dpuservicev1.DPUServiceTemplate](conf.AdditionalDPUServiceTemplatePath)
	t.additionalDPUServiceConfiguration = optionalObjectFromFile[dpuservicev1.DPUServiceConfiguration](conf.AdditionalDPUServiceConfigurationPath)

	t.dpuServiceInterfacesHBN = make([]*dpuservicev1.DPUServiceInterface, 0, len(conf.DPUServiceInterfacesHBNPaths))
	for _, path := range conf.DPUServiceInterfacesHBNPaths {
		t.dpuServiceInterfacesHBN = append(t.dpuServiceInterfacesHBN, objectFromFile[dpuservicev1.DPUServiceInterface](path))
	}

	t.dpuServiceInterfaceOVN = optionalObjectFromFile[dpuservicev1.DPUServiceInterface](conf.DPUServiceInterfaceOVNPath)

	t.dpuServiceTemplateOVN = optionalObjectFromFile[dpuservicev1.DPUServiceTemplate](conf.DPUServiceTemplateOVNPath)
	if t.dpuServiceTemplateOVN != nil {
		if repoURL, found := os.LookupEnv("OVN_KUBERNETES_REPO_URL"); found {
			t.dpuServiceTemplateOVN.Spec.HelmChart.Source.RepoURL = repoURL
		}
		if chartTag, found := os.LookupEnv("OVN_KUBERNETES_CHART_TAG"); found {
			t.dpuServiceTemplateOVN.Spec.HelmChart.Source.Version = chartTag
		}
	}

	t.dpuServiceTemplateHBN = optionalObjectFromFile[dpuservicev1.DPUServiceTemplate](conf.DPUServiceTemplateHBNPath)
	if t.dpuServiceTemplateHBN != nil {
		if repoURL, found := os.LookupEnv("HBN_CHART_REPO"); found {
			t.dpuServiceTemplateHBN.Spec.HelmChart.Source.RepoURL = repoURL
		}
		if chartVersion, found := os.LookupEnv("HBN_CHART_VERSION"); found {
			t.dpuServiceTemplateHBN.Spec.HelmChart.Source.Version = chartVersion
		}
	}

	t.dpuServiceConfigurationOVN = optionalObjectFromFile[dpuservicev1.DPUServiceConfiguration](conf.DPUServiceConfigurationOVNPath)
	t.dpuServiceConfigurationHBN = optionalObjectFromFile[dpuservicev1.DPUServiceConfiguration](conf.DPUServiceConfigurationHBNPath)

	t.dpuDeployment = objectFromFile[dpuservicev1.DPUDeployment](conf.DPUDeploymentPath)

	Expect(conf.DPUFlavorPath == nil || conf.DPUFlavorTemplatePath == nil).To(BeTrue(),
		"e2e config must set at most one of `dpuFlavor` and `dpuFlavorTemplate`")
	if conf.DPUFlavorPath != nil {
		t.dpuFlavor = objectFromFile[provisioningv1.DPUFlavor](*conf.DPUFlavorPath)
		overrideDPUFlavorConsole(t.dpuFlavor)
		t.dpuDeployment.Spec.DPUs.Flavor = &t.dpuFlavor.Name
		t.dpuDeployment.Spec.DPUs.FlavorTemplate = nil
		if t.dpuSet != nil {
			t.dpuSet.Spec.DPUTemplate.Spec.DPUFlavor = &t.dpuFlavor.Name
			t.dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate = nil
		}
	}
	if conf.DPUFlavorTemplatePath != nil {
		t.dpuFlavorTemplate = objectFromFile[provisioningv1.DPUFlavorTemplate](*conf.DPUFlavorTemplatePath)
		t.dpuDeployment.Spec.DPUs.Flavor = nil
		t.dpuDeployment.Spec.DPUs.FlavorTemplate = &t.dpuFlavorTemplate.Name
		if t.dpuSet != nil {
			t.dpuSet.Spec.DPUTemplate.Spec.DPUFlavor = nil
			t.dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate = &t.dpuFlavorTemplate.Name
		}
	}

	if conf.IPPoolDPUServiceIPAMPath != "" {
		t.ipPoolDPUServiceIPAM = objectFromFile[dpuservicev1.DPUServiceIPAM](conf.IPPoolDPUServiceIPAMPath)
	}
	t.cidrDPUServiceIPAM = optionalObjectFromFile[dpuservicev1.DPUServiceIPAM](conf.CIDRPoolDPUServiceIPAMPath)
	t.dpuServiceChain = optionalObjectFromFile[dpuservicev1.DPUServiceChain](conf.DPUServiceChainPath)
	t.dpuServiceCredentialRequest = optionalObjectFromFile[dpuservicev1.DPUServiceCredentialRequest](conf.DPUServiceCredentialRequestPath)
	t.ovnCredentialRequest = optionalObjectFromFile[dpuservicev1.DPUServiceCredentialRequest](conf.OVNCredentialRequestPath)

	t.numberOfDPUNodes = conf.NumberOfDPUNodes
	t.numberOfDPUsPerNode = conf.NumberOfDPUsPerNode
	t.numberOfCXsToConfigureViaBF4PerNode = conf.NumberOfCXsToConfigureViaBF4PerNode
	t.nodeRebootConfigMap = conf.NodeRebootConfigMap
	t.nodeRebootConfigMapPath = conf.NodeRebootConfigMapPath
	t.selectDPUDevicesDynamically = conf.SelectDPUDevicesDynamically
	t.useExternalNodeReboot = conf.UseExternalNodeReboot
}

// overrideDPUFlavorConsole replaces all console= entries loaded from the
// DPUFlavor file with exactly one console: ttyAMA0 for Zero Trust tests or
// hvc0 for Host Trusted tests. All non-console kernel parameters are preserved.
func overrideDPUFlavorConsole(flavor *provisioningv1.DPUFlavor) {
	console := "hvc0"
	trustMode := "Host Trust"
	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		console = "ttyAMA0"
		trustMode = "Zero Trust"
	}

	flavor.Spec.Grub.KernelParameters = dpuflavorutils.WithConsoleKernelParameter(
		flavor.Spec.Grub.KernelParameters,
		console,
	)
	By(fmt.Sprintf("Configured DPUFlavor %s with console=%s for %s", flavor.Name, console, trustMode))
}

func (t *systemTestInput) hasDpuNodes() bool {
	return t.numberOfDPUNodes > 0
}

// totalDPUs returns the total number of DPUs (nodes * DPUs per node)
func (t *systemTestInput) totalDPUs() int {
	return t.numberOfDPUNodes * t.numberOfDPUsPerNode
}

type DeployDPFSystemComponentsInput struct {
	operatorConfig            *operatorv1.DPFOperatorConfig
	systemNamespace           string
	ProvisioningControllerPVC *corev1.PersistentVolumeClaim
	ImagePullSecrets          []string
	dpuDiscovery              *provisioningv1.DPUDiscovery
	client                    client.Client
	numberOfDPUNodes          int
	// skipSystemComponentValidation skips the post-deploy system-component checks
	// (the current-shape DPUService assertion and the DPFOperatorConfig ready
	// wait). Set for previous-release installs (e.g. BFB LTS v25.10) whose
	// component shape differs and whose servicechainset-controller stays
	// not-ready under the current CRD schema.
	skipSystemComponentValidation bool
}

// DeployDPFSystemComponents creates the operatorConfig and some dependencies and checks that the system components
// are deployed from the operator.
// 1) Ensures the DPF Operator is running and ready
// 2) Creates a PersistentVolumeClaim for the Provisioning controller
// 3) Creates ImagePullSecrets which are tested as part of the e2e flow (note these are fake and could possibly be replaced by real ones)
// 4) Creates the operatorConfig for the test
// 5) Ensures the DPF System components - including DPUServices - have been deployed.
func DeployDPFSystemComponents(ctx context.Context, input DeployDPFSystemComponentsInput) {
	testClient := input.client
	By("Ensure the DPF Operator is running and ready")
	Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.systemNamespace,
			Name:      "dpf-operator-controller-manager"},
			deployment)).To(Succeed())
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Create the PersistentVolumeClaim for the DPF Provisioning controller")
	if input.ProvisioningControllerPVC == nil {
		By("No PVC provided for the provisioning controller, skipping PVC creation")
	} else {
		pvc := input.ProvisioningControllerPVC.DeepCopy()
		if n := input.operatorConfig.Spec.ProvisioningController.BFBPersistentVolumeClaimName; n != nil {
			pvc.SetName(*n)
		}
		pvc.SetNamespace(input.systemNamespace)
		pvc.SetLabels(CleanupScope.Suite)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, pvc))).NotTo(HaveOccurred())
	}

	By("Creates the imagePullSecrets for the DPFOperatorConfig")
	for _, secretName := range input.ImagePullSecrets {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: input.systemNamespace,
				Labels:    CleanupScope.Suite,
			},
		}
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, secret))).ToNot(HaveOccurred())
	}

	By("Create the DPFOperatorConfig for the system")
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, input.operatorConfig))).NotTo(HaveOccurred())

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Deploy DPUDiscovery for ZeroTrust")
		CreateDPUDiscovery(ctx, input)
	}

	By("Ensure the DPF controllers are running and ready")
	Eventually(func(g Gomega) {
		// Check the DPUService controller manager is up and ready.
		dpuServiceDeployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.systemNamespace,
			Name:      "dpuservice-controller-manager"},
			dpuServiceDeployment)).To(Succeed())
		g.Expect(dpuServiceDeployment.Status.ReadyReplicas).To(Equal(*dpuServiceDeployment.Spec.Replicas))

		// Check the DPF provisioning controller manager is up and ready.
		dpfProvisioningDeployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.systemNamespace,
			Name:      "dpf-provisioning-controller-manager"},
			dpfProvisioningDeployment)).To(Succeed())
		g.Expect(dpfProvisioningDeployment.Status.ReadyReplicas).To(Equal(*dpfProvisioningDeployment.Spec.Replicas))

		// Check the NodeSRIOV Device Plugin controller deployment only when it is explicitly enabled.
		if input.operatorConfig.Spec.NodeSRIOVDevicePluginController != nil &&
			input.operatorConfig.Spec.NodeSRIOVDevicePluginController.Disable != nil &&
			!*input.operatorConfig.Spec.NodeSRIOVDevicePluginController.Disable {
			nodesriovDevicePluginDeployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: input.systemNamespace,
				Name:      "dpf-nodesriovdeviceplugin-controller"},
				nodesriovDevicePluginDeployment)).To(Succeed())
			g.Expect(nodesriovDevicePluginDeployment.Status.ReadyReplicas).To(Equal(*nodesriovDevicePluginDeployment.Spec.Replicas))
		}

	}).WithTimeout(300 * time.Second).Should(Succeed())

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Verify bfb-registry Service and pods (created by provisioning controller leader)")
		Eventually(func(g Gomega) {
			svc := &corev1.Service{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: input.systemNamespace,
				Name:      "bfb-registry",
			}, svc)).To(Succeed(), "bfb-registry Service should be created by provisioning controller leader")
			g.Expect(svc.Spec.Ports).ToNot(BeEmpty())
			pods := &corev1.PodList{}
			g.Expect(testClient.List(ctx, pods,
				client.InNamespace(input.systemNamespace),
				client.MatchingLabels(map[string]string{
					"app.kubernetes.io/part-of": "bfb-registry",
					"dpu.nvidia.com/component":  "bfb-registry",
				}))).To(Succeed())
			g.Expect(pods.Items).ToNot(BeEmpty(), "bfb-registry pods should exist")
			hasReadyPod := false
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
							hasReadyPod = true
							break
						}
					}
				}
			}
			g.Expect(hasReadyPod).To(BeTrue(), "At least one bfb-registry pod should be running and ready")
		}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
	}

	if input.skipSystemComponentValidation {
		By("Skipping system component validation")
		return
	}

	By("Ensure the system DPUServices are created")
	Eventually(func(g Gomega) {
		// TODO: Remove as soon as we have version aware upgrade logic for the pre-upgrade validation
		isCurrentVersionLastReleasedGA, err := isPreUpgradeFromLastReleasedGA(ctx, testClient, client.ObjectKeyFromObject(input.operatorConfig))
		g.Expect(err).NotTo(HaveOccurred())

		// Each DPUService the operator creates is labeled with the component it came from, the ones
		// a DPUDeployment creates are not. Their number is no invariant: kube-state-metrics,
		// nvidia-k8s-ipam and servicechainset-controller each add one DPUService per DPUCluster,
		// named with a digest of the cluster, next to a cluster-independent one for CRDs, RBAC or a
		// node agent. All of them carry their component's label, so the set of components is the
		// invariant this asserts.
		dpuServices := &dpuservicev1.DPUServiceList{}
		g.Expect(testClient.List(ctx, dpuServices, client.HasLabels{operatorv1.DPFComponentLabelKey})).To(Succeed())

		components := sets.New[string]()
		for i := range dpuServices.Items {
			components.Insert(dpuServices.Items[i].Labels[operatorv1.DPFComponentLabelKey])
		}

		// The components the last released GA ships as well. spire-agent-rbac is in neither list: it
		// follows the SPIFFE gate and no e2e DPFOperatorConfig configures SPIFFE. Add it for a lane
		// that does.
		expectedComponentsLastReleasedGA := []string{
			operatorv1.CNIInstallerName.String(),
			operatorv1.FlannelName.String(),
			operatorv1.KubeStateMetricsName.String(),
			operatorv1.MultusName.String(),
			operatorv1.NodeProblemDetectorName.String(),
			operatorv1.NVIPAMControllerName.String(),
			operatorv1.ServiceSetControllerName.String(),
			operatorv1.SFCControllerName.String(),
			operatorv1.SRIOVDevicePluginName.String(),
		}

		// What the current branch adds on top of that release.
		expectedComponents := append(slices.Clone(expectedComponentsLastReleasedGA),
			operatorv1.DPUMonitoringName.String(),
			operatorv1.KataContainersName.String(),
			operatorv1.OpenTelemetryCollectorName.String(),
			operatorv1.CoreDNSName.String(),
		)

		// The initial phase of the upgrade test runs the last released GA, which ships the component
		// set of its own release, so require only the long-standing ones there. Every other run is
		// the current branch and has to match it exactly, so that a system component added or
		// dropped without updating this list surfaces here.
		if isCurrentVersionLastReleasedGA {
			g.Expect(components.UnsortedList()).To(ContainElements(expectedComponentsLastReleasedGA))
		} else {
			g.Expect(components.UnsortedList()).To(ConsistOf(expectedComponents))
		}
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Ensure the DPFOperatorConfig is ready")
	VerifyDPFOperatorConfigReady(ctx, testClient, 15*time.Minute)
}

// WaitForExistingDPUClustersReady waits for each DPUCluster referenced by the
// e2e config to already exist and report Ready, without creating or mutating
// them. Used by OCP reuse mode where the DPUClusters are provisioned out of
// band and must not be recreated.
func WaitForExistingDPUClustersReady(ctx context.Context, input ProvisionDPUClustersInput) {
	By(fmt.Sprintf("Waiting for %d existing DPUCluster(s) to be ready", len(input.dpuClusters)))
	Eventually(func(g Gomega) {
		for _, dpuCluster := range input.dpuClusters {
			g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuCluster), dpuCluster)).To(Succeed())
			g.Expect(dpuCluster.Status.Phase).Should(Equal(provisioningv1.PhaseReady))
		}
	}).WithTimeout(300 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
}

// ProvisionDPUClusters provisions DPUClusters.
func ProvisionDPUClusters(ctx context.Context, input ProvisionDPUClustersInput) {
	By("Create prerequisites objects for DPUClusters")
	for _, obj := range input.dpuClusterPrerequisites {
		obj.SetLabels(CleanupScope.Suite)
		// We need to check if object already exists before creating. client.IgnoreAlreadyExists does not work in this case as the error will be "port is already allocated"
		existing := obj.DeepCopyObject().(client.Object)
		err := input.client.Get(ctx, types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}, existing)
		if apierrors.IsNotFound(err) {
			By(fmt.Sprintf("Creating prerequisite object %s %s/%s", obj.GetObjectKind().GroupVersionKind().String(), obj.GetNamespace(), obj.GetName()))
			Expect(input.client.Create(ctx, obj)).To(Succeed())
		} else {
			By(fmt.Sprintf("Skipping creation of existing object %s %s/%s",
				obj.GetObjectKind().GroupVersionKind().String(),
				obj.GetNamespace(), obj.GetName()))
		}
	}

	By("Create DPUClusters")
	for _, dpuCluster := range input.dpuClusters {
		dpuClusterLabels := map[string]string{
			"svc.dpu.nvidia.com/cluster": dpuCluster.Name,
		}
		maps.Copy(dpuClusterLabels, CleanupScope.Suite)
		dpuCluster.SetLabels(dpuClusterLabels)
		By(fmt.Sprintf("Creating DPU Cluster %s/%s", dpuCluster.GetNamespace(), dpuCluster.GetName()))
		Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuCluster))).NotTo(HaveOccurred())
	}

	By(fmt.Sprintf("Waiting for %d DPUCluster(s) to be ready", len(input.dpuClusters)))
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(len(input.dpuClusters)))
		for _, dpuCluster := range input.dpuClusters {
			g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuCluster), dpuCluster)).To(Succeed())
			g.Expect(dpuCluster.Status.Phase).Should(Equal(provisioningv1.PhaseReady))
			if input.expectedKubernetesVersion != "" {
				g.Expect(dpuCluster.Status.Version).Should(Equal(input.expectedKubernetesVersion))
			} else {
				g.Expect(dpuCluster.Status.Version).Should(Equal(util.KubernetesVersion))
			}
		}
	}).WithTimeout(300 * time.Second).Should(Succeed())

	if etcdEncryptionAtRestConfiguration(input.operatorConfig) != nil {
		for _, dpuCluster := range input.dpuClusters {
			if dpuCluster.Spec.Type != string(provisioningv1.KamajiCluster) {
				continue
			}
			expectDPUClusterEncryptionAtRest(ctx, input.client, dpuCluster, input.operatorConfig, time.Minute)
		}
		updateStaticKeySecretIfConfigured(ctx, input.client, input.operatorConfig)
	}

	By("Creating a client for the DPUCluster")
	getDPUClusterClients(ctx, input)
}

// ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor creates the BFB or BlueFieldSoftware and optionally the DPUFlavor resources.
func ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx context.Context, input ProvisionDPUClustersInput) {
	Expect(input.bfb == nil && input.blueFieldSoftware == nil).To(BeFalse(),
		"one of bfb or blueFieldSoftware must be set")
	Expect(input.bfb != nil && input.blueFieldSoftware != nil).To(BeFalse(),
		"bfb and blueFieldSoftware cannot both be set")
	if input.bfb != nil {
		ProvisionBFB(ctx, input)
	}
	if input.blueFieldSoftware != nil {
		ProvisionBlueFieldSoftware(ctx, input)
	}
	if input.dpuFlavor != nil {
		ProvisionDPUFlavor(ctx, input)
	}
}

// ProvisionBFB creates the BFB resource, waits for it to reach Ready phase, and verifies
// the BFB file is reachable via the bfb-registry service (ZeroTrust only).
func ProvisionBFB(ctx context.Context, input ProvisionDPUClustersInput) {
	// TODO: Pass this in as config instead of as a global.
	if input.bfbImageURL != "" {
		By(fmt.Sprintf("Override BFB URL with env variable BFB_IMAGE_URL=%s", input.bfbImageURL))
		input.bfb.Spec.URL = input.bfbImageURL
	}
	By("Create the BFB")
	Eventually(func(g Gomega) {
		bfb := input.bfb.DeepCopy()
		bfb.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, bfb))).NotTo(HaveOccurred())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Checking that BFB is ready")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)).To(Succeed())
		g.Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady))
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Verifying BFB file is reachable")
		bfb := &provisioningv1.BFB{}
		Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)).To(Succeed())
		Expect(bfb.Status.FileName).ToNot(BeEmpty(), "BFB status should have a FileName after reaching Ready")

		svc := &corev1.Service{}
		Expect(input.client.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      "bfb-registry",
		}, svc)).To(Succeed())
		Expect(svc.Spec.Ports).ToNot(BeEmpty(), "bfb-registry Service should have ports")
		nodePort := svc.Spec.Ports[0].NodePort
		Expect(nodePort).ToNot(BeZero(), "bfb-registry Service should have a NodePort")

		// bfb-registry is HTTPS-only. The server certificate SAN only covers the IP of the node the
		// bfb-registry Pod runs on (its NODE_IP). In a multi control-plane-node setup the Pod may run on
		// any control-plane node, so we must reach it via that Pod's HostIP rather than guessing a
		// control-plane IP. Validate the endpoint against the DPF CA trust bundle rather than skipping
		// verification. The Pod and the CA trust bundle ConfigMap are created by the operator in the DPF
		// operator-system namespace, not the BFB's namespace.
		registryIP := getBFBRegistryHostIP(ctx, input.client, dpfOperatorSystemNamespace)
		caPool := getDPFCATrustBundlePool(ctx, input.client, dpfOperatorSystemNamespace)

		bfbURL := fmt.Sprintf("https://%s:%d/bfb/%s", registryIP, nodePort, bfb.Status.FileName)
		By(fmt.Sprintf("Checking BFB is reachable at %s", bfbURL))
		Eventually(func(g Gomega) {
			httpClient := &http.Client{
				Timeout: 10 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{RootCAs: caPool},
				},
			}
			resp, err := httpClient.Head(bfbURL)
			g.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close() //nolint:errcheck
			g.Expect(resp.StatusCode).To(Equal(http.StatusOK),
				fmt.Sprintf("BFB file should be reachable at %s, got status %d", bfbURL, resp.StatusCode))
		}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())
	}
}

// getBFBRegistryHostIP returns the HostIP of the running bfb-registry Pod. This IP is the one written
// into the server certificate SAN, so it is the only address that passes TLS verification regardless
// of which control-plane node the Pod is scheduled on.
func getBFBRegistryHostIP(ctx context.Context, testClient client.Client, namespace string) string {
	var hostIP string
	Eventually(func(g Gomega) {
		hostIP = ""
		pods := &corev1.PodList{}
		g.Expect(testClient.List(ctx, pods,
			client.InNamespace(namespace),
			client.MatchingLabels(map[string]string{
				"app.kubernetes.io/part-of": "bfb-registry",
				"dpu.nvidia.com/component":  "bfb-registry",
			}),
		)).To(Succeed())
		for i := range pods.Items {
			pod := &pods.Items[i]
			if pod.Status.Phase == corev1.PodRunning && pod.Status.HostIP != "" {
				hostIP = pod.Status.HostIP
				break
			}
		}
		g.Expect(hostIP).ToNot(BeEmpty(), "no running bfb-registry Pod with a HostIP found in namespace %s", namespace)
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
	return hostIP
}

// getDPFCATrustBundlePool builds an x509 cert pool from the DPF CA trust bundle ConfigMap so HTTPS
// connections to the bfb-registry can be validated without skipping verification.
func getDPFCATrustBundlePool(ctx context.Context, testClient client.Client, namespace string) *x509.CertPool {
	var pool *x509.CertPool
	// The CA trust bundle ConfigMap is created and populated by a controller, so it may not be
	// present or fully populated immediately. Retry rather than failing on a single transient miss.
	Eventually(func(g Gomega) {
		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      operatorv1.DefaultCATrustBundleConfigMapName,
		}, cm)).To(Succeed(), "DPF CA trust bundle ConfigMap should exist")
		pem := cm.Data[operatorv1.CATrustBundleKey]
		g.Expect(pem).ToNot(BeEmpty(), "DPF CA trust bundle ConfigMap should contain %q", operatorv1.CATrustBundleKey)
		p := x509.NewCertPool()
		g.Expect(p.AppendCertsFromPEM([]byte(pem))).To(BeTrue(), "DPF CA trust bundle should be valid PEM")
		pool = p
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
	return pool
}

// ProvisionBlueFieldSoftware creates the BlueFieldSoftware resource and waits for it to reach Ready phase.
func ProvisionBlueFieldSoftware(ctx context.Context, input ProvisionDPUClustersInput) {
	if input.bfsOsIsoURL != "" {
		By(fmt.Sprintf("Override BlueFieldSoftware OS ISO URL with env variable BFS_OS_ISO_URL=%s", input.bfsOsIsoURL))
		input.blueFieldSoftware.Spec.OsIso = input.bfsOsIsoURL
	}
	if len(input.bfsPldmFwBundles) > 0 {
		By(fmt.Sprintf("Override BlueFieldSoftware PLDM FW bundles from BFS_PLDM_FW_BUNDLE_URL_<PSID>=%v", input.bfsPldmFwBundles))
		input.blueFieldSoftware.Spec.PldmFwBundle = maps.Clone(input.bfsPldmFwBundles)
	}
	if input.bfsNicFwURL != "" {
		By(fmt.Sprintf("Override BlueFieldSoftware NIC FW URL with env variable BFS_NIC_FW_URL=%s", input.bfsNicFwURL))
		input.blueFieldSoftware.Spec.NicFw = &input.bfsNicFwURL
	}
	By("Create the BlueFieldSoftware")
	Eventually(func(g Gomega) {
		bfs := input.blueFieldSoftware.DeepCopy()
		bfs.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, bfs))).NotTo(HaveOccurred())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Checking that BlueFieldSoftware is ready")
	Eventually(func(g Gomega) {
		bfs := &provisioningv1.BlueFieldSoftware{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      input.blueFieldSoftware.Name,
			Namespace: input.blueFieldSoftware.Namespace,
		}, bfs)).To(Succeed())
		g.Expect(bfs.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
		g.Expect(bfs.Status.Versions).NotTo(BeNil(), "BlueFieldSoftware versions should be set when Ready")
		g.Expect(bfs.Status.Versions.DOCA).NotTo(BeEmpty(), "BlueFieldSoftware should have status.versions.doca when Ready")
		g.Expect(bfs.Status.Versions.OSISOVersion).NotTo(BeEmpty(), "BlueFieldSoftware should have status.versions.osISOVersion when Ready")
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// ProvisionDPUFlavor creates the DPUFlavor resource.
func ProvisionDPUFlavor(ctx context.Context, input ProvisionDPUClustersInput) {
	By("Creating the DPUFlavor")
	Eventually(func(g Gomega) {
		dpuFlavor := input.dpuFlavor.DeepCopy()
		dpuFlavor.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuFlavor))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// ProvisionDPUSet DPUSet that will provision DPUs in the background if the environment has such DPUs.
// It doesn't check whether the DPUs become ready intentionally to allow for subsequent tests to be executed in the meantime.
func ProvisionDPUSet(ctx context.Context, input ProvisionDPUClustersInput) {
	dpuset := input.dpuSet.DeepCopy()
	if input.selectDPUDevicesDynamically {
		resolveDPUSetDPUDevicePCISelector(
			ctx,
			input.client,
			dpuset,
			input.numberOfDPUNodes,
			input.numberOfDPUsPerNode,
		)
	}
	// TODO: Test the cleanup of the node related to the DPU.
	dpuset.SetLabels(CleanupScope.Suite)

	Eventually(func(g Gomega) {
		By("Creating the DPUSet")
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuset.DeepCopy()))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Checking the DPUServices have been mirrored to the target cluster")
	for _, componentName := range []operatorv1.ComponentName{
		operatorv1.ServiceSetControllerName,
		operatorv1.NVIPAMControllerName,
	} {
		deploymentName := fmt.Sprintf("in-cluster-%s", getPerClusterDPUServiceName(componentName, input.dpuClusters[0].Name, input.dpuClusters[0].Namespace))
		Eventually(func(g Gomega) {
			deployment := &appsv1.Deployment{}
			g.Expect(input.client.Get(ctx, client.ObjectKey{
				Namespace: dpfOperatorSystemNamespace,
				Name:      deploymentName},
				deployment)).To(Succeed())
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
		}).WithTimeout(600 * time.Second).Should(Succeed())
	}

	By("Checking that DPUService objects have been mirrored to the DPUClusters")
	Eventually(func(g Gomega) {
		deployments := &appsv1.DeploymentList{}
		g.Expect(dpuClusterClient[0].List(ctx, deployments)).To(Succeed())
		found := map[string]bool{}
		for i := range deployments.Items {
			if _, hasAnnotation := deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]; hasAnnotation {
				g.Expect(deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]).NotTo(Equal(""))
				found[deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]] = true
			}
		}
		daemonsets := appsv1.DaemonSetList{}
		g.Expect(dpuClusterClient[0].List(ctx, &daemonsets, client.InNamespace(input.dpuClusters[0].GetNamespace()))).To(Succeed())
		for i := range daemonsets.Items {
			if _, hasAnnotation := daemonsets.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]; hasAnnotation {
				g.Expect(daemonsets.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]).NotTo(Equal(""))
				found[daemonsets.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]] = true
			}
		}

		// Expect each of the following to have been created by the operator.
		// These are labels on the appv1 type - e.g. DaemonSet or Deployment on the DPU cluster.
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.MultusName.String())))
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.FlannelName.String())))
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SRIOVDevicePluginName.String())))
		// Note: The NVIPAM DPUService contains both a Daemonset and a Deployment - but this is overwritten in the map.
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.NVIPAMContainerNode.String())))
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SFCControllerName.String())))
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OpenTelemetryCollectorName.String())))
	}).WithTimeout(600 * time.Second).Should(Succeed())
}

// VerifyDPUClusterWithNodes waits and verifies if the DPUCluster has nodes meaning that there were DPUs provisioned. In
// addition verifies that the DPUs become ready.
// Note: Each DPU joins the DPU cluster as a separate K8s node, so the number of nodes in the DPU cluster equals totalDPUs.
func VerifyDPUClusterWithNodes(ctx context.Context, input ProvisionDPUClustersInput) {
	expectedDPUs := input.numberOfDPUNodes * input.numberOfDPUsPerNode
	tracker := NewByTracker()

	if err := verifyExpectedDPUsToBeReady(ctx, nil, input, expectedDPUs); err == nil {
		By("All DPUs are already ready")
		return
	}

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		ProcessDPUNodeMaintenanceHold(ctx, input)
		WaitForDPUReboot(ctx, input)
	}

	// Verify nodes are present in DPUCluster,
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).ToNot(HaveOccurred())
		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), expectedDPUs)
		tracker.By(nodeKey, "Checking that the number of nodes %d is equal to %d", len(nodes.Items), expectedDPUs)
		g.Expect(nodes.Items).To(HaveLen(expectedDPUs))
	}).WithTimeout(provisioningTimeout).WithPolling(1 * time.Second).Should(Succeed())

	// Verify DPUs are ready
	Eventually(func(g Gomega) {
		g.Expect(verifyExpectedDPUsToBeReady(ctx, tracker, input, expectedDPUs)).To(Succeed())
	}).WithTimeout(20 * time.Minute).Should(Succeed())

}

func verifyExpectedDPUsToBeReady(ctx context.Context, tracker *ByTracker, input ProvisionDPUClustersInput, expectedDPUs int) error {
	dpus := &provisioningv1.DPUList{}
	if err := input.client.List(ctx, dpus); err != nil {
		return err
	}
	if len(dpus.Items) != expectedDPUs {
		return fmt.Errorf("expected %d DPUs, got %d", expectedDPUs, len(dpus.Items))
	}
	for _, dpu := range dpus.Items {
		dpuStatusKey := fmt.Sprintf("%s/%v", dpu.Name, dpu.Status.Phase)
		if tracker != nil {
			tracker.By(dpuStatusKey, "DPU %s dpu.Status.Phase=%v", dpu.Name, dpu.Status.Phase)
		}
		if dpu.Status.Phase != provisioningv1.DPUReady {
			return fmt.Errorf("DPU %s is not ready. dpu.Status.Phase=%v", dpu.Name, dpu.Status.Phase)
		}
	}
	return nil
}

// isDPUNodeMaintenanceOnHold returns true if the DPUNodeMaintenance waits for hold to be released
func isDPUNodeMaintenanceOnHold(dpuNodeMaintenance *provisioningv1.DPUNodeMaintenance) bool {
	val, exists := dpuNodeMaintenance.Annotations["provisioning.dpu.nvidia.com/wait-for-external-nodeeffect"]
	return exists && val == "true"
}

// releaseDPUNodeMaintenanceHold releases the hold for the given DPUNodeMaintenance
func releaseDPUNodeMaintenanceHold(ctx context.Context, c client.Client, dpuNodeMaintenance *provisioningv1.DPUNodeMaintenance) error {
	current := &provisioningv1.DPUNodeMaintenance{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), current); err != nil {
		return err
	}
	patch := client.MergeFrom(current.DeepCopy())
	if current.Annotations == nil {
		current.Annotations = make(map[string]string)
	}
	current.Annotations["provisioning.dpu.nvidia.com/wait-for-external-nodeeffect"] = "false"

	if err := c.Patch(ctx, current, patch); err != nil {
		return err
	}
	return nil
}

// ProcessDPUNodeMaintenanceHold waits for DPUNodeMaintenance CRs to have the hold annotation set to "true"
// and then patches them to "false" to allow DPU provisioning to continue.
// This simulates an external system completing the node effect in a non-K8s environment.
func ProcessDPUNodeMaintenanceHold(ctx context.Context, input ProvisionDPUClustersInput) {
	By("Processing DPUNodeMaintenance with Node Effect Hold")
	tracker := NewByTracker()

	expectedDPUs := input.numberOfDPUNodes * input.numberOfDPUsPerNode

	// Wait for DPUNodeMaintenance CRs to exist with hold annotation set to "true"
	var dpuNodeMaintenanceList *provisioningv1.DPUNodeMaintenanceList
	Eventually(func(g Gomega) {
		dpuNodeMaintenanceList = &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(input.client.List(ctx, dpuNodeMaintenanceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Count how many have the hold annotation set to "true"
		holdCount := 0
		for i := range dpuNodeMaintenanceList.Items {
			if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[i]) {
				holdCount++
			}
		}

		holdKey := fmt.Sprintf("%d/%d", holdCount, expectedDPUs)
		tracker.By(holdKey, "Found %d/%d DPUNodeMaintenance CRs with hold annotation set to true", holdCount, expectedDPUs)
		g.Expect(holdCount).To(Equal(expectedDPUs), "All DPUs should have DPUNodeMaintenance with hold annotation set to true")
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())

	// Patch all DPUNodeMaintenance CRs to set hold annotation to "false"
	By("Setting hold annotation to false on all DPUNodeMaintenance CRs to allow provisioning to continue")
	for i := range dpuNodeMaintenanceList.Items {
		if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[i]) {
			Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.client, &dpuNodeMaintenanceList.Items[i]).WithTimeout(30 * time.Second).Should(Succeed())
			By(fmt.Sprintf("Released hold on DPUNodeMaintenance %s", dpuNodeMaintenanceList.Items[i].Name))
		}
	}
}

// WaitForDPUReboot waits for all DPUs to reach the DPURebooting phase, then
// polls each DPU's `Status.RebootStatus.Phase` until it reports `Succeeded`.
// The actual reboot is driven in-cluster by the DPUNode controller, which
// spawns a Job from the ConfigMap named in
// ProvisionDPUClustersInput.NodeRebootConfigMap (e.g. `dpunode-reboot-redfish`)
// and updates `RebootStatus` as the Job progresses; the test does not look
// at the Job directly because the controller may garbage-collect it after
// success. Fails fast if any DPU's `RebootStatus.Phase` becomes `Failed`.
// Applies to ZeroTrust only.
func WaitForDPUReboot(ctx context.Context, input ProvisionDPUClustersInput) {
	tracker := NewByTracker()
	dpus := &provisioningv1.DPUList{}

	By("Wait for DPUs to reach DPURebooting state in ZeroTrust")
	Eventually(func(g Gomega) {
		g.Expect(input.client.List(ctx, dpus)).ToNot(HaveOccurred())
		g.Expect(dpus.Items).To(HaveLen(input.numberOfDPUNodes * input.numberOfDPUsPerNode))

		for _, dpu := range dpus.Items {
			dpuStatusKey := fmt.Sprintf("%s/%v", dpu.Name, dpu.Status.Phase)
			tracker.By(dpuStatusKey, "DPU %s dpu.Status.Phase=%v", dpu.Name, dpu.Status.Phase)

			if dpu.Status.Phase != provisioningv1.DPUReady {
				dpuKey := client.ObjectKey{Name: dpu.Name, Namespace: dpu.Namespace}
				current := &provisioningv1.DPU{}
				g.Expect(input.client.Get(ctx, dpuKey, current)).To(Succeed())
				// TODO: update this behavior when retry during provisioning is introduced
				// Failing test instantly when facing Error during provisioning
				Expect(current.Status.Phase).NotTo(Equal(provisioningv1.DPUError))
				g.Expect(current.Status.Phase).To(Equal(provisioningv1.DPURebooting))
			}
		}
	}).WithTimeout(provisioningTimeout).Should(Succeed())

	By("Reboot driven by in-cluster script Job (nodeRebootMethod.script); waiting for completion")
	waitForScriptRebootCompletion(ctx, input.client,
		input.numberOfDPUNodes*input.numberOfDPUsPerNode)
}

// Waits for all DPU host reboots to finish in script-reboot mode by checking DPU.Status.RebootStatus,
// Succeeds when all DPUs report RebootStatus.Succeeded, fails-fast if any hit RebootStatus.Failed.
func waitForScriptRebootCompletion(ctx context.Context, c client.Client, expectedDPUs int) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(c.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(expectedDPUs))

		for i := range dpus.Items {
			dpu := &dpus.Items[i]
			rs := dpu.Status.RebootStatus
			phase := provisioningv1.RebootStatusPhase("")
			reason, message := "", ""
			if rs != nil {
				phase = rs.Phase
				reason = rs.Reason
				message = rs.Message
			}
			tracker.By(dpu.Name, "DPU %s RebootStatus.Phase=%q reason=%q",
				dpu.Name, phase, reason)

			// We use Expect here (not g.Expect) to fail fast the test if a DPU
			// enters the RebootStatusFailed state.
			Expect(phase).NotTo(Equal(provisioningv1.RebootStatusFailed),
				fmt.Sprintf("DPU %s RebootStatus=Failed (reason=%q, message=%q); "+
					"the script-reboot Job hit its backoffLimit and recovery "+
					"requires manual Job deletion",
					dpu.Name, reason, message))

			g.Expect(phase).To(Equal(provisioningv1.RebootStatusSucceeded),
				fmt.Sprintf("DPU %s RebootStatus.Phase=%q (waiting for Succeeded)",
					dpu.Name, phase))
		}
	}).WithTimeout(30 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// VerifyClusterPods waits until, for each name substring in podSubstrToVerify, at least one pod in the
// cluster whose name contains that substring exists and its containers are ready.
func VerifyClusterPods(ctx context.Context, client client.Client, podSubstrToVerify []string) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(client.List(ctx, pods)).To(Succeed())

		foundPods := make(map[string]bool)
		for _, podSubstr := range podSubstrToVerify {
			foundPods[podSubstr] = false
		}

		for _, pod := range pods.Items {
			var matchedSubstr string
			for _, podSubstr := range podSubstrToVerify {
				if !strings.Contains(pod.Name, podSubstr) {
					continue
				}
				matchedSubstr = podSubstr
				break
			}
			if matchedSubstr == "" {
				continue
			}

			foundPods[matchedSubstr] = true
			podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			tracker.By(podKey, "Verifying pod %s (Phase: %s)", podKey, pod.Status.Phase)

			g.Expect(pod.Status.ContainerStatuses).ToNot(BeEmpty())

			for _, containerStatus := range pod.Status.ContainerStatuses {
				containerKey := fmt.Sprintf("%s/%s", podKey, containerStatus.Name)
				tracker.By(containerKey, "Verifying container %s with image %s (Ready: %v)", containerKey, containerStatus.Image, containerStatus.Ready)
				g.Expect(containerStatus.ImageID).ToNot(BeEmpty())
			}
			g.Expect(pod.DeletionTimestamp).To(BeNil())
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
			g.Expect(pod.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", Equal(corev1.PodReady)),
					HaveField("Status", Equal(corev1.ConditionTrue)),
				),
			))
		}

		for podSubstr, found := range foundPods {
			tracker.By(podSubstr, "Verifying pod %s was scheduled", podSubstr)
			g.Expect(found).To(BeTrue())
		}
	}).WithTimeout(20 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

// VerifyDPFOperatorConfigReady waits and verifies if the DPFOperatorConfig is ready.
func VerifyDPFOperatorConfigReady(ctx context.Context, kclient client.Client, timeout time.Duration) {
	Eventually(func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(kclient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, dpfOperatorConfig)).To(Succeed())
		g.Expect(conditions.IsTrue(dpfOperatorConfig, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(timeout).WithPolling(1 * time.Second).Should(Succeed())
}

// VerifyProvisioningControllerPodsArg waits and verifies that all provisioning controller pods have the given argument
// in their manager container args.
func VerifyProvisioningControllerPodsArg(ctx context.Context, kclient client.Client, arg string, timeout time.Duration) {
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(kclient.List(ctx, pods,
			client.InNamespace(dpfOperatorSystemNamespace),
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"},
		)).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		for _, pod := range pods.Items {
			argFound := false
			for _, c := range pod.Spec.Containers {
				if c.Name == "manager" && slices.Contains(c.Args, arg) {
					argFound = true
					break
				}
			}
			g.Expect(argFound).To(BeTrue(), "pod %s manager container does not have arg %q", pod.Name, arg)
		}
	}).WithTimeout(timeout).WithPolling(1 * time.Second).Should(Succeed())
}

// CreateDPUDiscovery verifies no worker nodes and no DPUDevices are in the host cluster.
// Creates DPUDiscovery and verifies DPUDevices were found and added.
func CreateDPUDiscovery(ctx context.Context, input DeployDPFSystemComponentsInput) {
	By("Verify worker nodes are not present")
	workerNodes := &corev1.NodeList{}
	Eventually(func(g Gomega) int {
		err := input.client.List(ctx, workerNodes, client.InNamespace(dpfOperatorSystemNamespace), client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
		g.Expect(err).NotTo(HaveOccurred())
		return len(workerNodes.Items)
	}, time.Second*30, time.Millisecond*250).Should(Equal(0))

	By("Verify DPU devices are not present")
	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) int {
		err := input.client.List(ctx, dpuDeviceList, client.InNamespace(input.systemNamespace))
		g.Expect(err).NotTo(HaveOccurred())
		return len(dpuDeviceList.Items)
	}, time.Second*30, time.Millisecond*250).Should(Equal(0))

	By("Creating DpuDiscovery")
	Expect(input.dpuDiscovery).NotTo(BeNil(), "dpuDiscovery config is required for ZeroTrust")
	discovery := input.dpuDiscovery.DeepCopy()
	discovery.SetNamespace(input.systemNamespace)
	discovery.SetLabels(CleanupScope.Suite)

	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, discovery))).NotTo(HaveOccurred())

	By("Waiting for DPU discovery to complete and create DPU devices")
	dpuDeviceList = &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) int {
		err := input.client.List(ctx, dpuDeviceList, client.InNamespace(input.systemNamespace))
		g.Expect(err).NotTo(HaveOccurred())
		return len(dpuDeviceList.Items)
	}, time.Minute*5, time.Millisecond*250).Should(Equal(input.numberOfDPUNodes))
}

// ValidateDPUAgentStatus verifies that the DPU agent has reported its status correctly
// on every ready DPU. Each DPU is validated against the supplied expected AgentStatus.
func ValidateDPUAgentStatus(ctx context.Context, input *systemTestInput, expected provisioningv1.AgentStatus) {
	if !input.hasDpuNodes() {
		Skip("Skip DPU Agent validation as there are no DPU nodes")
	}

	expectedDPUs := input.totalDPUs()

	By("Listing all DPUs and validating agent status")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(expectedDPUs), "expected %d DPUs", expectedDPUs)

		for i := range dpus.Items {
			validateSingleDPUAgentStatus(g, &dpus.Items[i], expected)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// validateSingleDPUAgentStatus checks one DPU's agent-reported status against the expected AgentStatus.
// Pointer fields that are nil in expected are skipped (not validated).
// Non-nil pointer fields are validated for equality with the actual value.
// Every expected condition must be reported with the expected status, and every
// other reported condition must be True.
func validateSingleDPUAgentStatus(g Gomega, dpu *provisioningv1.DPU, expectedAgentStatus provisioningv1.AgentStatus) {
	g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady),
		"DPU %s should be Ready, got %v", dpu.Name, dpu.Status.Phase)

	By(fmt.Sprintf("Validating agent status on DPU %s", dpu.Name))

	g.Expect(dpu.Status.AgentStatus).NotTo(BeNil(),
		"DPU %s should have AgentStatus populated by the DPU agent", dpu.Name)

	actualAgentStatus := dpu.Status.AgentStatus

	g.Expect(actualAgentStatus.LastStartupTime).NotTo(BeNil(),
		"DPU %s AgentStatus.LastStartupTime should be set", dpu.Name)

	// The controller sets AgentLastStartupTime during the DPUConfig phase.
	// If the agent restarts after the DPU is already Ready, LastStartupTime advances
	// but AgentLastStartupTime stays at the value from the last provisioning cycle.
	g.Expect(dpu.Status.AgentLastStartupTime).NotTo(BeNil(),
		"DPU %s AgentLastStartupTime should be set by the provisioning controller", dpu.Name)
	g.Expect(dpu.Status.AgentLastStartupTime.Time).To(
		BeTemporally("<=", actualAgentStatus.LastStartupTime.Time),
		"DPU %s AgentLastStartupTime (%v) should not be after AgentStatus.LastStartupTime (%v)",
		dpu.Name, dpu.Status.AgentLastStartupTime, actualAgentStatus.LastStartupTime)

	expectedConditions := make(map[string]metav1.Condition, len(expectedAgentStatus.Conditions))
	for _, cond := range expectedAgentStatus.Conditions {
		expectedConditions[cond.Type] = cond
	}

	// The agent reports the kubelet version from the operation that configures the
	// kubelet. On OpenShift the platform owns the kubelet, so the agent skips that
	// operation and reports no version. Require one only where the caller expects
	// the condition of that operation.
	if _, ok := expectedConditions["KubeletConfigured"]; ok {
		g.Expect(actualAgentStatus.KubeletVersion).NotTo(BeNil(),
			"DPU %s AgentStatus.KubeletVersion should be reported", dpu.Name)
		g.Expect(*actualAgentStatus.KubeletVersion).NotTo(BeEmpty(),
			"DPU %s AgentStatus.KubeletVersion should not be empty", dpu.Name)
	}

	if expectedAgentStatus.RebootMethod != nil {
		g.Expect(actualAgentStatus.RebootMethod).NotTo(BeNil(),
			"DPU %s AgentStatus.RebootMethod should be set", dpu.Name)
		g.Expect(*actualAgentStatus.RebootMethod).To(Equal(*expectedAgentStatus.RebootMethod),
			"DPU %s AgentStatus.RebootMethod should be %s, got %s",
			dpu.Name, *expectedAgentStatus.RebootMethod, *actualAgentStatus.RebootMethod)
	}

	if expectedAgentStatus.InitialBootID != nil {
		g.Expect(actualAgentStatus.InitialBootID).NotTo(BeNil(),
			"DPU %s AgentStatus.InitialBootID should be set", dpu.Name)
		g.Expect(*actualAgentStatus.InitialBootID).To(Equal(*expectedAgentStatus.InitialBootID),
			"DPU %s AgentStatus.InitialBootID should be %s, got %s",
			dpu.Name, *expectedAgentStatus.InitialBootID, *actualAgentStatus.InitialBootID)
	}

	if expectedAgentStatus.RebootSequenceCount != nil {
		g.Expect(actualAgentStatus.RebootSequenceCount).NotTo(BeNil(),
			"DPU %s AgentStatus.RebootSequenceCount should be set", dpu.Name)
		g.Expect(*actualAgentStatus.RebootSequenceCount).To(Equal(*expectedAgentStatus.RebootSequenceCount),
			"DPU %s AgentStatus.RebootSequenceCount should be %d, got %d",
			dpu.Name, *expectedAgentStatus.RebootSequenceCount, *actualAgentStatus.RebootSequenceCount)
	}

	conditionMap := make(map[string]metav1.Condition, len(actualAgentStatus.Conditions))
	for _, cond := range actualAgentStatus.Conditions {
		conditionMap[cond.Type] = cond
	}
	for _, expCond := range expectedAgentStatus.Conditions {
		cond, found := conditionMap[expCond.Type]
		g.Expect(found).To(BeTrue(),
			"DPU %s is missing expected agent condition %q", dpu.Name, expCond.Type)
		g.Expect(cond.Status).To(Equal(expCond.Status),
			"DPU %s agent condition %q should have Status %v, got %v (reason: %s, message: %s)",
			dpu.Name, cond.Type, expCond.Status, cond.Status, cond.Reason, cond.Message)
		g.Expect(cond.Reason).NotTo(BeEmpty(),
			"DPU %s agent condition %q should have a non-empty Reason", dpu.Name, cond.Type)
		g.Expect(cond.LastTransitionTime).NotTo(BeZero(),
			"DPU %s agent condition %q should have LastTransitionTime set", dpu.Name, cond.Type)
	}

	for _, cond := range actualAgentStatus.Conditions {
		// True is healthy for every condition the agent reports on its own work. A
		// condition the caller expects to be False is healthy in that state instead,
		// such as a link-down condition on a link that is up.
		wantStatus := metav1.ConditionTrue
		if expCond, ok := expectedConditions[cond.Type]; ok {
			wantStatus = expCond.Status
		}
		g.Expect(cond.Status).To(Equal(wantStatus),
			"DPU %s agent condition %q should be %v, got %v (reason: %s, message: %s)",
			dpu.Name, cond.Type, wantStatus, cond.Status, cond.Reason, cond.Message)
		g.Expect(cond.Reason).NotTo(BeEmpty(),
			"DPU %s agent condition %q should have a non-empty Reason", dpu.Name, cond.Type)
		g.Expect(cond.LastTransitionTime).NotTo(BeZero(),
			"DPU %s agent condition %q should have LastTransitionTime set", dpu.Name, cond.Type)
	}
}

// getDPUClusterClient retrieves the DPUCluster client for the cluster at the given index. This function is internal and should not be called directly.
// Instead, use getDPUClusterClients to retrieve all clients for all clusters.
func getDPUClusterClient(ctx context.Context, input ProvisionDPUClustersInput, clusterIndex int) {
	var tun *tunnel.Tunnel

	Eventually(func(g Gomega) {
		refreshable, ok := dpuClusterClient[clusterIndex].(*refreshableclient.Client)
		g.Expect(ok).To(BeTrue(), "DPUCluster client %d should be a refreshable client", clusterIndex)

		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(input.dpuClusters[clusterIndex]), input.dpuClusters[clusterIndex])).To(Succeed())
		g.Expect(input.dpuClusters[clusterIndex].Spec.Kubeconfig).ToNot(BeEmpty(), "DPUCluster kubeconfig should be populated")

		var err error
		var restCfg *rest.Config
		restCfg, tun, err = tunnel.NewTunneledRestConfig(ctx, input.client, input.restConfig, input.dpuClusters[clusterIndex])
		g.Expect(err).NotTo(HaveOccurred(), "Should create tunneled REST config")

		dpuClient, err := client.New(restCfg, client.Options{})
		g.Expect(err).NotTo(HaveOccurred(), "Should create tunneled client")
		refreshable.Set(dpuClient)

		// Setup the dpuClusterRestClient
		restCfg.APIPath = "/api"
		restCfg.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
		restCfg.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
		dpuClusterRestConfig[clusterIndex] = restCfg
		dpuClusterRestClient[clusterIndex], err = rest.RESTClientFor(restCfg)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(3 * time.Minute).WithPolling(time.Second).Should(Succeed())

	// Start a go routine that monitors the health of the tunnel and recreates the client and rest config
	// if the health check fails. When no port-forward tunnel is used (e.g. direct kubeconfig on OCP),
	// tun is nil and there is nothing to monitor.
	if tun == nil {
		return
	}
	go func() {
		defer GinkgoRecover()
		// By making this tick faster, we risk opening more ports until we have a functional port that can forward our
		// requests to the DPUCluster. All these connections are closed in the end of the test run. This can happen if
		// the DPUCluster is completely unavailable for long time, which is very unlikely since we run DPUCluster HA.
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !tun.IsHealthy() {
					By("Tunnel health check failed, recreating client and rest config")
					tun.Close()
					getDPUClusterClient(ctx, input, clusterIndex)
					return
				}
			}
		}
	}()
}

// getDPUClusterClients retrieves the DPUCluster clients for all clusters in the input.
// This function must only be called once per test suite as it initializes stable global client wrappers.
func getDPUClusterClients(ctx context.Context, input ProvisionDPUClustersInput) {
	if dpuClusterClientsInitialized {
		warningMsg := "WARNING: getDPUClusterClients called multiple times - " +
			"skipping reinitialization (this may indicate a test structure issue)"
		GinkgoWriter.Println(warningMsg)
		AddReportEntry("Multiple getDPUClusterClients calls", warningMsg, ReportEntryVisibilityFailureOrVerbose)
		return
	}
	dpuClusterClientsInitialized = true

	// Pre-initialize the global slices with the correct size
	numClusters := len(input.dpuClusters)
	dpuClusterClient = make([]client.Client, numClusters)
	dpuClusterRestConfig = make([]*rest.Config, numClusters)
	dpuClusterRestClient = make([]*rest.RESTClient, numClusters)
	for i := range input.dpuClusters {
		dpuClusterClient[i] = refreshableclient.New()
	}

	for i := range input.dpuClusters {
		getDPUClusterClient(ctx, input, i)
	}
}

const bmcIPLabelKey = "host-bmc-ip"

// PatchDPUDevicesForZeroTrust waits for expectedDPUs DPUDevices, then optionally
// patches NICDeviceCount and per-serial Spec.Values from setupInfo.
func PatchDPUDevicesForZeroTrust(ctx context.Context, c client.Client, expectedDPUs int,
	expectedNicDeviceCount int, setupInfo *ciSetupInfo) {
	if expectedDPUs == 0 {
		By("Skipping DPUDevice ZeroTrust patches: expectedDPUs is 0")
		return
	}

	By(fmt.Sprintf("Waiting for %d DPUDevices before ZeroTrust patches (nicDeviceCount=%d)",
		expectedDPUs, expectedNicDeviceCount))
	var observed []provisioningv1.DPUDevice
	Eventually(func(g Gomega) {
		devices := &provisioningv1.DPUDeviceList{}
		g.Expect(c.List(ctx, devices, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(devices.Items).To(HaveLen(expectedDPUs))
		observed = devices.Items
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	if expectedNicDeviceCount == 0 {
		By("Skipping NIC device count patch: expectedNicDeviceCount is 0 (no E/W NICs to configure)")
	}

	nicPatched, valuesPatched := 0, 0
	for i := range observed {
		deviceValues := setupInfo.GetDPUDeviceValuesForDPUDevice(&observed[i])

		current := &provisioningv1.DPUDevice{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(&observed[i]), current)).To(Succeed())
		patch := client.MergeFrom(current.DeepCopy())

		if expectedNicDeviceCount > 0 {
			current.Spec.NICDeviceCount = &expectedNicDeviceCount
			nicPatched++
		}

		// dpu-device-values are only needed by suites that render a DPUFlavorTemplate.
		if len(deviceValues) > 0 {
			raw, err := json.Marshal(deviceValues)
			Expect(err).NotTo(HaveOccurred(),
				"marshaling dpu-device-values for DPUDevice %s", observed[i].Name)
			current.Spec.Values = &machineryruntime.RawExtension{Raw: raw}
			valuesPatched++
		}

		Expect(c.Patch(ctx, current, patch)).To(Succeed())
	}
	By(fmt.Sprintf("Patched NICDeviceCount on %d and setup-info values on %d of %d DPUDevices",
		nicPatched, valuesPatched, len(observed)))
}

// GetDPUNodeToBMCIPs ensures the expected DPUNodes exist and maps each DPUNode name to its host BMC IP
// from the pre-loaded setupInfo map. In Zero-Trust mode, this validates each DPUNode against the
// setup info, failing clearly if not found.
func GetDPUNodeToBMCIPs(ctx context.Context, c client.Client, expectedDPUNodes int,
	setupInfo *ciSetupInfo) map[string]string {
	By(fmt.Sprintf("Resolving DPUNode -> host BMC IP for %d DPUNodes via %s",
		expectedDPUNodes, setupInfo.path))
	var observed []provisioningv1.DPUNode
	Eventually(func(g Gomega) {
		nodes := &provisioningv1.DPUNodeList{}
		g.Expect(c.List(ctx, nodes, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(expectedDPUNodes))
		observed = nodes.Items
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())

	out := make(map[string]string, len(observed))
	for _, node := range observed {
		out[node.Name] = setupInfo.GetHostBMCIPForDPUNode(ctx, c, &node)
	}
	return out
}

// Name of the Secret referenced by the reboot ConfigMap pod-template to provide BMC credentials.
// Must match between Go code and the YAML fixture for script-based node reboot.
const (
	bmcCredentialsSecretName = "dpunode-reboot-bmc-credentials"
	bmcUsernameSecretKey     = "BMC_USERNAME"
	bmcPasswordSecretKey     = "BMC_PASSWORD"
)

// ApplyNodeRebootConfigMap creates the BMC credentials Secret (sourced from
// $E2E_ZT_BMC_USERNAME / $E2E_ZT_BMC_PASSWORD) and applies the reboot ConfigMap fixture as-is.
func ApplyNodeRebootConfigMap(ctx context.Context, c client.Client, configMapPath string) {

	applyBMCCredentialsSecret(ctx, c)

	By(fmt.Sprintf("Applying node reboot ConfigMap fixture %s", configMapPath))
	data, err := os.ReadFile(configMapPath)
	Expect(err).ToNot(HaveOccurred(), "reading node reboot ConfigMap fixture")

	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	if obj.GetNamespace() == "" {
		obj.SetNamespace(dpfOperatorSystemNamespace)
	}
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	maps.Copy(labels, CleanupScope.Suite)
	obj.SetLabels(labels)
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, obj))).To(Succeed())
}

// applyBMCCredentialsSecret creates a suite-scoped Secret for the reboot pod with BMC credentials, ensuring they are not exposed in the ConfigMap.
// bmcUsername / bmcPassword are sourced from $E2E_ZT_BMC_USERNAME / $E2E_ZT_BMC_PASSWORD by getEnvVariables()
// and required-ness is enforced by validateFlags() for ZT runs.
func applyBMCCredentialsSecret(ctx context.Context, c client.Client) {
	By(fmt.Sprintf("Creating BMC credentials Secret %s/%s",
		dpfOperatorSystemNamespace, bmcCredentialsSecretName))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmcCredentialsSecretName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    maps.Clone(CleanupScope.Suite),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			bmcUsernameSecretKey: []byte(bmcUsername),
			bmcPasswordSecretKey: []byte(bmcPassword),
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, secret))).To(Succeed())
}

// PatchDPUNodesForScriptReboot updates each DPUNode to use the specified script-based reboot method,
// assigning the correct BMC IP label from the externally provided bmcIPs map. This pattern is necessary
// because we use dpudiscovery (and do not create the DPUNode objects ourselves), so we must associate
// host identity (BMC IPs) externally. This is especially important in Zero-Trust setups, where host
// identity cannot be reliably derived from cluster state.
func PatchDPUNodesForScriptReboot(ctx context.Context, c client.Client,
	expectedDPUNodes int, configMapName string, bmcIPs map[string]string) {

	Expect(bmcIPs).NotTo(BeEmpty(),
		"DPUNodeBMCs must be set when NodeRebootConfigMap is in use")
	Expect(bmcIPs).To(HaveLen(expectedDPUNodes),
		"DPUNodeBMCs must have one entry per DPUNode (got %d, expected %d)",
		len(bmcIPs), expectedDPUNodes)

	By(fmt.Sprintf("Waiting for %d DPUNodes to exist before switching reboot method to script", expectedDPUNodes))
	var observed []provisioningv1.DPUNode
	Eventually(func(g Gomega) {
		nodes := &provisioningv1.DPUNodeList{}
		g.Expect(c.List(ctx, nodes, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(expectedDPUNodes))
		observed = nodes.Items
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())

	for i := range observed {
		dpuNode := &provisioningv1.DPUNode{}
		Expect(c.Get(ctx, client.ObjectKeyFromObject(&observed[i]), dpuNode)).To(Succeed())

		bmcIP, ok := bmcIPs[dpuNode.Name]
		Expect(ok).To(BeTrue(),
			"DPUNode %s has no entry in DPUNodeBMCs: %v", dpuNode.Name, bmcIPs)
		Expect(bmcIP).NotTo(BeEmpty(),
			"DPUNodeBMCs[%s] is empty", dpuNode.Name)

		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
			Script: &provisioningv1.Script{Name: configMapName},
		}
		if dpuNode.Labels == nil {
			dpuNode.Labels = map[string]string{}
		}
		dpuNode.Labels[bmcIPLabelKey] = bmcIP
		By(fmt.Sprintf("Patching DPUNode %s -> script reboot via ConfigMap %s (BMC %s)",
			dpuNode.Name, configMapName, bmcIP))
		Expect(c.Patch(ctx, dpuNode, patch)).To(Succeed())
	}
}

// objectsFromManifest reads a (potentially multi-document) YAML manifest into unstructured objects (e.g.
// the csi-hostpath RBAC/plugin bundles). Cleanup labels are not set.
func objectsFromManifest(path string) []*unstructured.Unstructured {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred(), "reading %s", path)

	// Decode each '---'-separated document in turn until EOF, skipping empty ones.
	// manifestDecoderBufferBytes is the decoder read-buffer size in bytes (common k8s default).
	const decoderBufferBytes = 4096
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), decoderBufferBytes)
	var objs []*unstructured.Unstructured
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			break
		}
		Expect(err).ToNot(HaveOccurred(), "decoding %s", path)
		if len(obj.Object) == 0 {
			continue // empty document, e.g. a trailing '---'
		}
		objs = append(objs, obj)
	}
	return objs
}

// manifestObjectMutator mutates an object decoded from a manifest before it is created — e.g. stamping
// cleanup labels (withCleanupLabels), rewriting chart/image placeholders, or overriding the namespace.
type manifestObjectMutator func(*unstructured.Unstructured)

// withCleanupLabels returns a mutator that stamps the given cleanup-scope labels onto an object,
// keeping the labels its manifest already declares.
func withCleanupLabels(labels map[string]string) manifestObjectMutator {
	return func(obj *unstructured.Unstructured) {
		obj.SetLabels(cleanup.MergeMaps(obj.GetLabels(), labels))
	}
}

// withNamespace returns a mutator that sets the object's namespace (no-op semantics for cluster-scoped
// kinds are the caller's responsibility — only apply it to namespaced objects).
func withNamespace(namespace string) manifestObjectMutator {
	return func(obj *unstructured.Unstructured) { obj.SetNamespace(namespace) }
}

// withPodNodeSelector returns a mutator that merges entries into a workload's pod nodeSelector, keeping the
// ones its manifest already declares. Only apply it to kinds with a pod template under spec.template.
func withPodNodeSelector(nodeSelector map[string]string) manifestObjectMutator {
	return func(obj *unstructured.Unstructured) {
		declared, _, err := unstructured.NestedStringMap(obj.Object, "spec", "template", "spec", "nodeSelector")
		Expect(err).ToNot(HaveOccurred())
		Expect(unstructured.SetNestedStringMap(obj.Object, cleanup.MergeMaps(declared, nodeSelector),
			"spec", "template", "spec", "nodeSelector")).To(Succeed())
	}
}

// withContainerImage returns a mutator that sets the image of the named container in a workload's pod
// template.
//
// Objects without a pod template are skipped: a manifest bundle mixes workloads with RBAC, CSIDriver and
// the like, and those have no image to set. A workload that does have a pod template but no container of
// that name is a failure instead, because the image would silently keep the manifest's placeholder value
// and the run would only break later, on an ImagePullBackOff.
func withContainerImage(containerName, image string) manifestObjectMutator {
	return func(obj *unstructured.Unstructured) {
		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		Expect(err).ToNot(HaveOccurred())
		if !found {
			return
		}
		patched := false
		for _, entry := range containers {
			container, ok := entry.(map[string]interface{})
			if !ok || container["name"] != containerName {
				continue
			}
			container["image"] = image
			patched = true
		}
		Expect(patched).To(BeTrue(), "%s %s has no container %s", obj.GetKind(), obj.GetName(), containerName)
		Expect(unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")).To(Succeed())
	}
}

// withDPUSetPinnedToDPU returns a mutator that narrows a DPUDeployment's single dpuSet to the DPUDevice
// matching pinLabels, as returned by pinDPUDeviceOnNode, so provisioning creates one DPU instead of every
// DPU the dpuSet would otherwise match. Requires the DPUDeployment to declare exactly one dpuSet.
func withDPUSetPinnedToDPU(pinLabels map[string]string) manifestObjectMutator {
	return func(obj *unstructured.Unstructured) {
		// Validate the input and the DPUSet shape before mutating.
		Expect(pinLabels).ToNot(BeEmpty(), "the labels selecting the pinned DPUDevice must be set")
		dpuSets, _, err := unstructured.NestedSlice(obj.Object, "spec", "dpus", "dpuSets")
		Expect(err).ToNot(HaveOccurred())
		Expect(dpuSets).To(HaveLen(1), "DPUDeployment %s must have exactly one dpuSet", obj.GetName())
		dpuSet, ok := dpuSets[0].(map[string]interface{})
		Expect(ok).To(BeTrue(), "dpuSet in DPUDeployment %s is not a map", obj.GetName())

		// Set the device selector on the dpuSet via label selectors, then write the slice back.
		matchLabels := make(map[string]interface{}, len(pinLabels))
		for key, value := range pinLabels {
			matchLabels[key] = value
		}
		dpuSet["dpuDeviceSelector"] = map[string]interface{}{"matchLabels": matchLabels}
		Expect(unstructured.SetNestedSlice(obj.Object, dpuSets, "spec", "dpus", "dpuSets")).To(Succeed())
	}
}

// applyObjectsFromManifests loads every object from each manifest path, runs the mutators on it in
// order, and creates it on cl (ignoring already-exists errors). Pass no mutators to apply verbatim.
func applyObjectsFromManifests(ctx context.Context, cl client.Client, paths []string, mutators ...manifestObjectMutator) {
	for _, path := range paths {
		for _, obj := range objectsFromManifest(path) {
			for _, mutate := range mutators {
				mutate(obj)
			}
			Expect(client.IgnoreAlreadyExists(cl.Create(ctx, obj))).To(Succeed(),
				"creating %s %s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
		}
	}
}

type collectResourcesInput struct {
	collectResources bool
	testClient       client.Client
	clientset        *kubernetes.Clientset
	restConfig       *rest.Config
	artifactsDir     string
}

func collectKubernetesResources(ctx context.Context, input collectResourcesInput, testName string) error {
	if !input.collectResources {
		return nil
	}
	// Run dpfctl describe to get information about the resources on a failed state.
	opts := dpfctl.ObjectTreeOptions{
		ShowOtherConditions: "failed",
		ExpandResources:     "failed",
		Output:              "table",
		Colors:              true,
	}
	t, err := dpfctl.Discover(ctx, input.testClient, opts, "all")
	// Only print if at least a operatorConfig is found.
	if !apierrors.IsNotFound(err) {
		if err != nil {
			return err
		}
		if err := dpfctl.PrintObjectTree(t); err != nil {
			return err
		}
	}

	// Get the path to place artifacts in
	artifactsPath := filepath.Join(input.artifactsDir, testName)

	cc := collector.ClusterCollector{
		Client:     input.testClient,
		ClientSet:  input.clientset,
		RestConfig: input.restConfig,
	}

	// Create a resourceCollector to dump logs and resources for test debugging.
	clusters, err := collector.GetClusterCollectors(ctx, cc, artifactsPath)
	Expect(err).NotTo(HaveOccurred())
	c := collector.New(clusters)
	defer c.Close()
	return c.Run(ctx)
}

// PatchNFDWorkerForVIP patches the node-feature-discovery-worker DaemonSet with the
// KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT env vars so that NFD workers can
// reach the kube-apiserver via VIP on nodes where the ClusterIP is not yet reachable
// (e.g. before OVN-K is running on physical performance setups).
func PatchNFDWorkerForVIP(ctx context.Context, c client.Client, namespace, vip string, port int) {
	By(fmt.Sprintf("Patching node-feature-discovery-worker DaemonSet with VIP %s:%d", vip, port))
	ds := &appsv1.DaemonSet{}
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, types.NamespacedName{
			Name:      "node-feature-discovery-worker",
			Namespace: namespace,
		}, ds)).To(Succeed())
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	patch, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name": "worker",
							"env": []corev1.EnvVar{
								{Name: "KUBERNETES_SERVICE_HOST", Value: vip},
								{Name: "KUBERNETES_SERVICE_PORT", Value: fmt.Sprintf("%d", port)},
							},
						},
					},
				},
			},
		},
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(c.Patch(ctx, ds, client.RawPatch(types.StrategicMergePatchType, patch))).To(Succeed())
}
