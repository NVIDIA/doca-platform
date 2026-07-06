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
	"context"
	"encoding/json"
	"fmt"
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
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProvisionDPUClustersInput struct {
	NumberOfDPUNodes          int
	NumberOfDPUsPerNode       int
	DPUClusterPrerequisites   []client.Object
	DPUClusters               []*provisioningv1.DPUCluster
	DPUFlavor                 *provisioningv1.DPUFlavor
	BFB                       *provisioningv1.BFB
	BlueFieldSoftware         *provisioningv1.BlueFieldSoftware
	DPUSet                    *provisioningv1.DPUSet
	Client                    client.Client
	BFBImageURL               string
	BFSOsIsoURL               string
	BFSPldmFwBundleURL        string
	RestConfig                *rest.Config
	NodeRebootConfigMap       string
	DPUNodeBMCs               map[string]string
	ExpectedKubernetesVersion string
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

// SystemTestInput represents the fully loaded and processed test environment.
// This struct contains actual Kubernetes API objects and runtime configuration
// that are ready for use in end-to-end tests.
//
// - Contains parsed and loaded Kubernetes manifests from YAML files
//   - Populated by applyConfig() and applySDNConfig() from config struct
//
// - Passed to individual test functions as the primary test context
// - Provides all necessary objects and configuration for test execution
// - Depends on `config` struct for file paths and basic configuration
type SystemTestInput struct {
	Namespace                         string
	Config                            *operatorv1.DPFOperatorConfig
	PVC                               *corev1.PersistentVolumeClaim
	DPUClusterPrerequisites           []client.Object
	DPUClusters                       []*provisioningv1.DPUCluster
	DPUFlavor                         *provisioningv1.DPUFlavor
	DPUDiscovery                      *provisioningv1.DPUDiscovery
	DPUService                        *dpuservicev1.DPUService
	DPUServiceHBN                     *dpuservicev1.DPUService
	DPUServiceInterface               *dpuservicev1.DPUServiceInterface
	DPUServiceInterfaceTemplate       *dpuservicev1.DPUServiceInterface
	DPUServiceChain                   *dpuservicev1.DPUServiceChain
	DPUServiceChainTemplate           *dpuservicev1.DPUServiceChain
	BFB                               *provisioningv1.BFB
	BlueFieldSoftware                 *provisioningv1.BlueFieldSoftware
	DPUSet                            *provisioningv1.DPUSet
	BFSOsIsoURL                       string
	BFSPldmFwBundleURL                string
	DPUDeployment                     *dpuservicev1.DPUDeployment
	DPUServiceConfiguration           *dpuservicev1.DPUServiceConfiguration
	DPUServiceInterfacesHBN           []*dpuservicev1.DPUServiceInterface
	DPUServiceInterfaceOVN            *dpuservicev1.DPUServiceInterface
	DPUServiceTemplate                *dpuservicev1.DPUServiceTemplate
	AdditionalDPUServiceTemplate      *dpuservicev1.DPUServiceTemplate
	DPUServiceTemplateOVN             *dpuservicev1.DPUServiceTemplate
	DPUServiceTemplateHBN             *dpuservicev1.DPUServiceTemplate
	DPUServiceConfigurationOVN        *dpuservicev1.DPUServiceConfiguration
	DPUServiceConfigurationHBN        *dpuservicev1.DPUServiceConfiguration
	AdditionalDPUServiceConfiguration *dpuservicev1.DPUServiceConfiguration
	DPUServiceIPAMTemplate            *dpuservicev1.DPUServiceIPAM
	DPUServiceNAD                     *dpuservicev1.DPUServiceNAD
	CIDRDPUServiceIPAM                *dpuservicev1.DPUServiceIPAM
	IPPoolDPUServiceIPAM              *dpuservicev1.DPUServiceIPAM
	DPUServiceCredentialRequest       *dpuservicev1.DPUServiceCredentialRequest
	OVNCredentialRequest              *dpuservicev1.DPUServiceCredentialRequest
	NumberOfDPUNodes                  int
	NumberOfDPUsPerNode               int
	PullSecretNames                   []string
	Client                            client.Client
	CleanupFlags                      *cleanup.CleanupFlags
	BFBImageURL                       string
	RestConfig                        *rest.Config
	NodeRebootConfigMap               string
	NodeRebootConfigMapPath           string
	UseExternalNodeReboot             bool
	DPUNodeBMCs                       map[string]string
}

func (t *SystemTestInput) ApplySDNConfig(conf Config) {
	dpuServiceInterfaceTemplate := &dpuservicev1.DPUServiceInterface{}
	dsiTemplate := unstructuredFromFile(conf.DPUServiceInterfaceTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dsiTemplate.Object, dpuServiceInterfaceTemplate)).To(Succeed())
	t.DPUServiceInterfaceTemplate = dpuServiceInterfaceTemplate

	dpuServiceHBN := &dpuservicev1.DPUService{}
	svcHBN := unstructuredFromFile(conf.DPUServiceHBNPath)

	// Override HBN image if HBN_IMAGE_URL is set
	if hbnImageURL != "" {
		parts := strings.SplitN(hbnImageURL, ":", 2)
		repository := parts[0]
		tag := parts[1]
		updateHBNImage(svcHBN, repository, tag)
	}
	if repoURL, found := os.LookupEnv("HBN_CHART_REPO"); found {
		updateHBNChartRepo(svcHBN, repoURL)
	}
	if chartVersion, found := os.LookupEnv("HBN_CHART_VERSION"); found {
		updateHBNChartVersion(svcHBN, chartVersion)
	}

	if ngcAPIKey != "" {
		updateImagePullSecret(svcHBN, NGCPullSecretName)
	}

	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcHBN.Object, dpuServiceHBN)).To(Succeed())
	t.DPUServiceHBN = dpuServiceHBN

	dpuServiceIPAMTemplate := &dpuservicev1.DPUServiceIPAM{}
	ipam := unstructuredFromFile(conf.DPUServiceIPAMTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ipam.Object, dpuServiceIPAMTemplate)).To(Succeed())
	t.DPUServiceIPAMTemplate = dpuServiceIPAMTemplate

	dpuServiceNAD := &dpuservicev1.DPUServiceNAD{}
	nad := unstructuredFromFile(conf.DPUServiceNADPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(nad.Object, dpuServiceNAD)).To(Succeed())
	t.DPUServiceNAD = dpuServiceNAD

	dpuServiceChainTemplate := &dpuservicev1.DPUServiceChain{}
	chainTemplate := unstructuredFromFile(conf.DPUServiceChainTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(chainTemplate.Object, dpuServiceChainTemplate)).To(Succeed())
	t.DPUServiceChainTemplate = dpuServiceChainTemplate
}

func updateHBNImage(svcHBN *unstructured.Unstructured, repository, tag string) {
	err := unstructured.SetNestedField(svcHBN.Object, repository,
		"spec", "helmChart", "values", "image", "repository")
	Expect(err).ToNot(HaveOccurred())

	err = unstructured.SetNestedField(svcHBN.Object, tag,
		"spec", "helmChart", "values", "image", "tag")
	Expect(err).ToNot(HaveOccurred())
}

func updateHBNChartRepo(svcHBN *unstructured.Unstructured, repoURL string) {
	err := unstructured.SetNestedField(svcHBN.Object, repoURL,
		"spec", "helmChart", "source", "repoURL")
	Expect(err).ToNot(HaveOccurred())
}

func updateHBNChartVersion(svcHBN *unstructured.Unstructured, chartVersion string) {
	err := unstructured.SetNestedField(svcHBN.Object, chartVersion,
		"spec", "helmChart", "source", "version")
	Expect(err).ToNot(HaveOccurred())
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

func (t *SystemTestInput) ApplyConfig(conf Config) {
	if conf.BFBPath != nil {
		bfb := &provisioningv1.BFB{}
		bfbUnstructured := unstructuredFromFile(*conf.BFBPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(bfbUnstructured.Object, bfb)).To(Succeed())
		t.BFB = bfb
	}

	if conf.BlueFieldSoftwarePath != nil {
		blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
		bfsUnstructured := unstructuredFromFile(*conf.BlueFieldSoftwarePath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(bfsUnstructured.Object, blueFieldSoftware)).To(Succeed())
		t.BlueFieldSoftware = blueFieldSoftware
	}

	dpuSet := &provisioningv1.DPUSet{}
	dpuSetUnstructured := unstructuredFromFile(conf.DPUSetPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuSetUnstructured.Object, dpuSet)).To(Succeed())
	t.DPUSet = dpuSet

	pvc := &corev1.PersistentVolumeClaim{}
	if conf.ProvisioningControllerPVCPath != nil {
		pvcUnstructured := unstructuredFromFile(*conf.ProvisioningControllerPVCPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(pvcUnstructured.Object, pvc)).To(Succeed())
		t.PVC = pvc
	}

	// Load all DPU clusters
	t.DPUClusters = make([]*provisioningv1.DPUCluster, 0, len(conf.DPUClusterPaths))
	for _, dpuClusterPath := range conf.DPUClusterPaths {
		dpuCluster := &provisioningv1.DPUCluster{}
		dpuClusterUnstructured := unstructuredFromFile(dpuClusterPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuClusterUnstructured.Object, dpuCluster)).To(Succeed())
		// Override interface if DPUCLUSTER_INTERFACE environment variable is set
		if dpuClusterInterface != "" && dpuCluster.Spec.ClusterEndpoint != nil && dpuCluster.Spec.ClusterEndpoint.Keepalived != nil {
			By(fmt.Sprintf("Overriding DPUCluster interface with DPUCLUSTER_INTERFACE=%s", dpuClusterInterface))
			dpuCluster.Spec.ClusterEndpoint.Keepalived.Interface = dpuClusterInterface
		}
		t.DPUClusters = append(t.DPUClusters, dpuCluster)
	}

	if conf.DPUDiscoveryPath != nil {
		dpuDiscovery := &provisioningv1.DPUDiscovery{}
		dpuDiscoveryUnstructured := unstructuredFromFile(*conf.DPUDiscoveryPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuDiscoveryUnstructured.Object, dpuDiscovery)).To(Succeed())
		t.DPUDiscovery = dpuDiscovery
	}

	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
	dsi := unstructuredFromFile(conf.DPUServiceInterfacePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dsi.Object, dpuServiceInterface)).To(Succeed())
	t.DPUServiceInterface = dpuServiceInterface

	dpuService := &dpuservicev1.DPUService{}
	svc := unstructuredFromFile(conf.DPUServicePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svc.Object, dpuService)).To(Succeed())
	t.DPUService = dpuService

	dpuClusterPrerequisiteObjects := []client.Object{}
	for _, path := range conf.DPUClusterPrerequisiteObjectPaths {
		dpuClusterPrerequisiteObjects = append(dpuClusterPrerequisiteObjects, unstructuredFromFile(path))
	}
	t.DPUClusterPrerequisites = dpuClusterPrerequisiteObjects

	dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
	tmp := unstructuredFromFile(conf.DPUServiceTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(tmp.Object, dpuServiceTemplate)).To(Succeed())
	t.DPUServiceTemplate = dpuServiceTemplate

	dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
	svcConfig := unstructuredFromFile(conf.DPUServiceConfiguration)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcConfig.Object, dpuServiceConfiguration)).To(Succeed())
	t.DPUServiceConfiguration = dpuServiceConfiguration

	if conf.AdditionalDPUServiceTemplatePath != nil {
		additionalTemplate := &dpuservicev1.DPUServiceTemplate{}
		tmp := unstructuredFromFile(*conf.AdditionalDPUServiceTemplatePath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(tmp.Object, additionalTemplate)).To(Succeed())
		t.AdditionalDPUServiceTemplate = additionalTemplate
	}

	if conf.AdditionalDPUServiceConfigurationPath != nil {
		additionalConfiguration := &dpuservicev1.DPUServiceConfiguration{}
		svcConfig := unstructuredFromFile(*conf.AdditionalDPUServiceConfigurationPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcConfig.Object, additionalConfiguration)).To(Succeed())
		t.AdditionalDPUServiceConfiguration = additionalConfiguration
	}

	t.DPUServiceInterfacesHBN = make([]*dpuservicev1.DPUServiceInterface, 0, len(conf.DPUServiceInterfacesHBNPaths))
	for _, path := range conf.DPUServiceInterfacesHBNPaths {
		iface := &dpuservicev1.DPUServiceInterface{}
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredFromFile(path).Object, iface)).To(Succeed())
		t.DPUServiceInterfacesHBN = append(t.DPUServiceInterfacesHBN, iface)
	}

	if conf.DPUServiceInterfaceOVNPath != nil {
		ovnIface := &dpuservicev1.DPUServiceInterface{}
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredFromFile(*conf.DPUServiceInterfaceOVNPath).Object, ovnIface)).To(Succeed())
		t.DPUServiceInterfaceOVN = ovnIface
	}

	if conf.DPUServiceTemplateOVNPath != nil {
		dpuServiceTemplateOVN := &dpuservicev1.DPUServiceTemplate{}
		ovnTmp := unstructuredFromFile(*conf.DPUServiceTemplateOVNPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ovnTmp.Object, dpuServiceTemplateOVN)).To(Succeed())
		if repoURL, found := os.LookupEnv("OVN_KUBERNETES_REPO_URL"); found {
			dpuServiceTemplateOVN.Spec.HelmChart.Source.RepoURL = repoURL
		}
		if chartTag, found := os.LookupEnv("OVN_KUBERNETES_CHART_TAG"); found {
			dpuServiceTemplateOVN.Spec.HelmChart.Source.Version = chartTag
		}
		t.DPUServiceTemplateOVN = dpuServiceTemplateOVN
	}

	if conf.DPUServiceTemplateHBNPath != nil {
		dpuServiceTemplateHBN := &dpuservicev1.DPUServiceTemplate{}
		hbnTmp := unstructuredFromFile(*conf.DPUServiceTemplateHBNPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(hbnTmp.Object, dpuServiceTemplateHBN)).To(Succeed())
		if repoURL, found := os.LookupEnv("HBN_CHART_REPO"); found {
			dpuServiceTemplateHBN.Spec.HelmChart.Source.RepoURL = repoURL
		}
		if chartVersion, found := os.LookupEnv("HBN_CHART_VERSION"); found {
			dpuServiceTemplateHBN.Spec.HelmChart.Source.Version = chartVersion
		}
		t.DPUServiceTemplateHBN = dpuServiceTemplateHBN
	}

	if conf.DPUServiceConfigurationOVNPath != nil {
		dpuServiceConfigurationOVN := &dpuservicev1.DPUServiceConfiguration{}
		ovnCfg := unstructuredFromFile(*conf.DPUServiceConfigurationOVNPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ovnCfg.Object, dpuServiceConfigurationOVN)).To(Succeed())
		t.DPUServiceConfigurationOVN = dpuServiceConfigurationOVN
	}

	if conf.DPUServiceConfigurationHBNPath != nil {
		dpuServiceConfigurationHBN := &dpuservicev1.DPUServiceConfiguration{}
		hbnCfg := unstructuredFromFile(*conf.DPUServiceConfigurationHBNPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(hbnCfg.Object, dpuServiceConfigurationHBN)).To(Succeed())
		t.DPUServiceConfigurationHBN = dpuServiceConfigurationHBN
	}

	dpuDeployment := &dpuservicev1.DPUDeployment{}
	deployment := unstructuredFromFile(conf.DPUDeploymentPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(deployment.Object, dpuDeployment)).To(Succeed())
	t.DPUDeployment = dpuDeployment

	dpuFlavor := &provisioningv1.DPUFlavor{}
	if conf.DPUFlavorPath != nil {
		dpuFlavorUnstructured := unstructuredFromFile(*conf.DPUFlavorPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuFlavorUnstructured.Object, dpuFlavor)).To(Succeed())
		t.DPUFlavor = dpuFlavor
		t.DPUDeployment.Spec.DPUs.Flavor = dpuFlavor.Name
		t.DPUSet.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor.Name
	}

	ipPoolDPUServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
	subnetIPAM := unstructuredFromFile(conf.IPPoolDPUServiceIPAMPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(subnetIPAM.Object, ipPoolDPUServiceIPAM)).To(Succeed())
	t.IPPoolDPUServiceIPAM = ipPoolDPUServiceIPAM

	cidrDPUServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
	cidrIPAM := unstructuredFromFile(conf.CIDRPoolDPUServiceIPAMPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(cidrIPAM.Object, cidrDPUServiceIPAM)).To(Succeed())
	t.CIDRDPUServiceIPAM = cidrDPUServiceIPAM

	dpuServiceChain := &dpuservicev1.DPUServiceChain{}
	chain := unstructuredFromFile(conf.DPUServiceChainPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(chain.Object, dpuServiceChain)).To(Succeed())
	t.DPUServiceChain = dpuServiceChain

	dpuServiceCredentialRequest := &dpuservicev1.DPUServiceCredentialRequest{}
	request := unstructuredFromFile(conf.DPUServiceCredentialRequestPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(request.Object, dpuServiceCredentialRequest)).To(Succeed())
	t.DPUServiceCredentialRequest = dpuServiceCredentialRequest

	if conf.OVNCredentialRequestPath != nil {
		ovnCredentialRequest := &dpuservicev1.DPUServiceCredentialRequest{}
		ovnCR := unstructuredFromFile(*conf.OVNCredentialRequestPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ovnCR.Object, ovnCredentialRequest)).To(Succeed())
		t.OVNCredentialRequest = ovnCredentialRequest
	}

	t.NumberOfDPUNodes = conf.NumberOfDPUNodes
	t.NumberOfDPUsPerNode = conf.NumberOfDPUsPerNode
	t.NodeRebootConfigMap = conf.NodeRebootConfigMap
	t.NodeRebootConfigMapPath = conf.NodeRebootConfigMapPath
	t.UseExternalNodeReboot = conf.UseExternalNodeReboot
}

func (t *SystemTestInput) HasDpuNodes() bool {
	return t.NumberOfDPUNodes > 0
}

// TotalDPUs returns the total number of DPUs (nodes * DPUs per node)
func (t *SystemTestInput) TotalDPUs() int {
	return t.NumberOfDPUNodes * t.NumberOfDPUsPerNode
}

type DeployDPFSystemComponentsInput struct {
	OperatorConfig            *operatorv1.DPFOperatorConfig
	SystemNamespace           string
	ProvisioningControllerPVC *corev1.PersistentVolumeClaim
	ImagePullSecrets          []string
	DPUDiscovery              *provisioningv1.DPUDiscovery
	Client                    client.Client
	NumberOfDPUNodes          int
	// SkipSystemComponentValidation skips the post-deploy system-component checks
	// (the current-shape DPUService assertion and the DPFOperatorConfig ready
	// wait). Set for previous-release installs (e.g. BFB LTS v25.10) whose
	// component shape differs and whose servicechainset-controller stays
	// not-ready under the current CRD schema.
	SkipSystemComponentValidation bool
}

// DeployDPFSystemComponents creates the operatorConfig and some dependencies and checks that the system components
// are deployed from the operator.
// 1) Ensures the DPF Operator is running and ready
// 2) Creates a PersistentVolumeClaim for the Provisioning controller
// 3) Creates ImagePullSecrets which are tested as part of the e2e flow (note these are fake and could possibly be replaced by real ones)
// 4) Creates the operatorConfig for the test
// 5) Ensures the DPF System components - including DPUServices - have been deployed.
func DeployDPFSystemComponents(ctx context.Context, input DeployDPFSystemComponentsInput) {
	testClient := input.Client
	By("Ensure the DPF Operator is running and ready")
	Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.SystemNamespace,
			Name:      "dpf-operator-controller-manager"},
			deployment)).To(Succeed())
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Create the PersistentVolumeClaim for the DPF Provisioning controller")
	if input.ProvisioningControllerPVC == nil {
		By("No PVC provided for the provisioning controller, skipping PVC creation")
	} else {
		pvc := input.ProvisioningControllerPVC.DeepCopy()
		if n := input.OperatorConfig.Spec.ProvisioningController.BFBPersistentVolumeClaimName; n != nil {
			pvc.SetName(*n)
		}
		pvc.SetNamespace(input.SystemNamespace)
		pvc.SetLabels(CleanupScope.Suite)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, pvc))).NotTo(HaveOccurred())
	}

	By("Creates the imagePullSecrets for the DPFOperatorConfig")
	for _, secretName := range input.ImagePullSecrets {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: input.SystemNamespace,
				Labels:    CleanupScope.Suite,
			},
		}
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, secret))).ToNot(HaveOccurred())
	}

	By("Create the DPFOperatorConfig for the system")
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, input.OperatorConfig))).NotTo(HaveOccurred())

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Deploy DPUDiscovery for ZeroTrust")
		CreateDPUDiscovery(ctx, input)
	}

	By("Ensure the DPF controllers are running and ready")
	Eventually(func(g Gomega) {
		// Check the DPUService controller manager is up and ready.
		dpuServiceDeployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.SystemNamespace,
			Name:      "dpuservice-controller-manager"},
			dpuServiceDeployment)).To(Succeed())
		g.Expect(dpuServiceDeployment.Status.ReadyReplicas).To(Equal(*dpuServiceDeployment.Spec.Replicas))

		// Check the DPF provisioning controller manager is up and ready.
		dpfProvisioningDeployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.SystemNamespace,
			Name:      "dpf-provisioning-controller-manager"},
			dpfProvisioningDeployment)).To(Succeed())
		g.Expect(dpfProvisioningDeployment.Status.ReadyReplicas).To(Equal(*dpfProvisioningDeployment.Spec.Replicas))

		// Check the NodeSRIOV Device Plugin controller deployment only when it is explicitly enabled.
		if input.OperatorConfig.Spec.NodeSRIOVDevicePluginController != nil &&
			input.OperatorConfig.Spec.NodeSRIOVDevicePluginController.Disable != nil &&
			!*input.OperatorConfig.Spec.NodeSRIOVDevicePluginController.Disable {
			nodesriovDevicePluginDeployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: input.SystemNamespace,
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
				Namespace: input.SystemNamespace,
				Name:      "bfb-registry",
			}, svc)).To(Succeed(), "bfb-registry Service should be created by provisioning controller leader")
			g.Expect(svc.Spec.Ports).ToNot(BeEmpty())
			pods := &corev1.PodList{}
			g.Expect(testClient.List(ctx, pods,
				client.InNamespace(input.SystemNamespace),
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
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}

	if input.SkipSystemComponentValidation {
		By("Skipping system component validation")
		return
	}

	By("Ensure the system DPUServices are created")
	var isCurrentVersionLastReleasedGA bool
	Eventually(func(g Gomega) {
		// TODO: Remove as soon as we have version aware upgrade logic for the pre-upgrade validation
		var err error
		isCurrentVersionLastReleasedGA, err = isPreUpgradeFromLastReleasedGA(ctx, testClient, client.ObjectKeyFromObject(input.OperatorConfig))
		g.Expect(err).NotTo(HaveOccurred())

		dpuServices := &dpuservicev1.DPUServiceList{}
		g.Expect(testClient.List(ctx, dpuServices)).To(Succeed())

		itemNames := []string{}
		for _, item := range dpuServices.Items {
			itemNames = append(itemNames, item.Name)
		}

		// Validate the expected number of DPUServices.
		g.Expect(dpuServices.Items).To(HaveLen(11), "Expected 11 DPUServices, got %d: [%s]", len(dpuServices.Items), strings.Join(itemNames, ", "))

		found := map[string]bool{}
		for i := range dpuServices.Items {
			found[dpuServices.Items[i].Name] = true
		}

		// Validate the expected DPUServices by installation phase.
		// If: standard e2e run, or post-upgrade phase of the upgrade test (current branch state).
		// Else: initial phase of the upgrade test (deployed from the last GA release).
		if !isCurrentVersionLastReleasedGA {
			g.Expect(found).To(HaveKey(operatorv1.KataContainersName.String()))
		}

		// Expect each of the following to have been created by the operator.
		g.Expect(found).To(HaveKey(operatorv1.ServiceChainSetCRDsName.String()))
		g.Expect(found).To(HaveKey(operatorv1.NVIPAMNodeName.String()))
		g.Expect(found).To(HaveKey(operatorv1.KubeStateMetricsRBACName.String()))
		g.Expect(found).To(HaveKey(operatorv1.NodeProblemDetectorName.String()))
		g.Expect(found).To(HaveKey(operatorv1.MultusName.String()))
		g.Expect(found).To(HaveKey(operatorv1.SRIOVDevicePluginName.String()))
		g.Expect(found).To(HaveKey(operatorv1.FlannelName.String()))
		g.Expect(found).To(HaveKey(operatorv1.SFCControllerName.String()))
		g.Expect(found).To(HaveKey(operatorv1.CNIInstallerName.String()))
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Ensure the DPFOperatorConfig is ready")
	VerifyDPFOperatorConfigReady(ctx, testClient, 15*time.Minute)
}

// ProvisionDPUClusters provisions DPUClusters.
func ProvisionDPUClusters(ctx context.Context, input ProvisionDPUClustersInput) {
	By("Create prerequisites objects for DPUClusters")
	for _, obj := range input.DPUClusterPrerequisites {
		obj.SetLabels(CleanupScope.Suite)
		// We need to check if object already exists before creating. client.IgnoreAlreadyExists does not work in this case as the error will be "port is already allocated"
		existing := obj.DeepCopyObject().(client.Object)
		err := input.Client.Get(ctx, types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}, existing)
		if apierrors.IsNotFound(err) {
			By(fmt.Sprintf("Creating prerequisite object %s %s/%s", obj.GetObjectKind().GroupVersionKind().String(), obj.GetNamespace(), obj.GetName()))
			Expect(input.Client.Create(ctx, obj)).To(Succeed())
		} else {
			By(fmt.Sprintf("Skipping creation of existing object %s %s/%s",
				obj.GetObjectKind().GroupVersionKind().String(),
				obj.GetNamespace(), obj.GetName()))
		}
	}

	By("Create DPUClusters")
	for _, dpuCluster := range input.DPUClusters {
		dpuClusterLabels := map[string]string{
			"svc.dpu.nvidia.com/cluster": dpuCluster.Name,
		}
		maps.Copy(dpuClusterLabels, CleanupScope.Suite)
		dpuCluster.SetLabels(dpuClusterLabels)
		By(fmt.Sprintf("Creating DPU Cluster %s/%s", dpuCluster.GetNamespace(), dpuCluster.GetName()))
		Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, dpuCluster))).NotTo(HaveOccurred())
	}

	By(fmt.Sprintf("Waiting for %d DPUCluster(s) to be ready", len(input.DPUClusters)))
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.Client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(len(input.DPUClusters)))
		for _, dpuCluster := range input.DPUClusters {
			g.Expect(input.Client.Get(ctx, client.ObjectKeyFromObject(dpuCluster), dpuCluster)).To(Succeed())
			g.Expect(dpuCluster.Status.Phase).Should(Equal(provisioningv1.PhaseReady))
			if input.ExpectedKubernetesVersion != "" {
				g.Expect(dpuCluster.Status.Version).Should(Equal(input.ExpectedKubernetesVersion))
			} else {
				g.Expect(dpuCluster.Status.Version).Should(Equal(util.KubernetesVersion))
			}
		}
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("Creating a client for the DPUCluster")
	getDPUClusterClients(ctx, input)
}

// ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor creates the BFB or BlueFieldSoftware and optionally the DPUFlavor resources.
func ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx context.Context, input ProvisionDPUClustersInput) {
	Expect(input.BFB == nil && input.BlueFieldSoftware == nil).To(BeFalse(),
		"one of bfb or blueFieldSoftware must be set")
	Expect(input.BFB != nil && input.BlueFieldSoftware != nil).To(BeFalse(),
		"bfb and blueFieldSoftware cannot both be set")
	if input.BFB != nil {
		ProvisionBFB(ctx, input)
	}
	if input.BlueFieldSoftware != nil {
		ProvisionBlueFieldSoftware(ctx, input)
	}
	if input.DPUFlavor != nil {
		ProvisionDPUFlavor(ctx, input)
	}
}

// ProvisionBFB creates the BFB resource, waits for it to reach Ready phase, and verifies
// the BFB file is reachable via the bfb-registry service (ZeroTrust only).
func ProvisionBFB(ctx context.Context, input ProvisionDPUClustersInput) {
	// TODO: Pass this in as config instead of as a global.
	if input.BFBImageURL != "" {
		By(fmt.Sprintf("Override BFB URL with env variable BFB_IMAGE_URL=%s", input.BFBImageURL))
		input.BFB.Spec.URL = input.BFBImageURL
	}
	By("Create the BFB")
	Eventually(func(g Gomega) {
		bfb := input.BFB.DeepCopy()
		bfb.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, bfb))).NotTo(HaveOccurred())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Checking that BFB is ready")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.Client.Get(ctx, client.ObjectKey{
			Name:      input.BFB.Name,
			Namespace: input.BFB.Namespace,
		}, bfb)).To(Succeed())
		g.Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady))
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Verifying BFB file is reachable")
		bfb := &provisioningv1.BFB{}
		Expect(input.Client.Get(ctx, client.ObjectKey{
			Name:      input.BFB.Name,
			Namespace: input.BFB.Namespace,
		}, bfb)).To(Succeed())
		Expect(bfb.Status.FileName).ToNot(BeEmpty(), "BFB status should have a FileName after reaching Ready")

		controlPlaneIP := getClusterControlPlaneIP(ctx, input.Client)
		svc := &corev1.Service{}
		Expect(input.Client.Get(ctx, client.ObjectKey{
			Namespace: input.BFB.Namespace,
			Name:      "bfb-registry",
		}, svc)).To(Succeed())
		Expect(svc.Spec.Ports).ToNot(BeEmpty(), "bfb-registry Service should have ports")
		nodePort := svc.Spec.Ports[0].NodePort
		Expect(nodePort).ToNot(BeZero(), "bfb-registry Service should have a NodePort")

		bfbURL := fmt.Sprintf("http://%s:%d/bfb/%s", controlPlaneIP, nodePort, bfb.Status.FileName)
		By(fmt.Sprintf("Checking BFB is reachable at %s", bfbURL))
		Eventually(func(g Gomega) {
			httpClient := &http.Client{Timeout: 10 * time.Second}
			resp, err := httpClient.Head(bfbURL)
			g.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close() //nolint:errcheck
			g.Expect(resp.StatusCode).To(Equal(http.StatusOK),
				fmt.Sprintf("BFB file should be reachable at %s, got status %d", bfbURL, resp.StatusCode))
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}
}

// ProvisionBlueFieldSoftware creates the BlueFieldSoftware resource and waits for it to reach Ready phase.
func ProvisionBlueFieldSoftware(ctx context.Context, input ProvisionDPUClustersInput) {
	if input.BFSOsIsoURL != "" {
		By(fmt.Sprintf("Override BlueFieldSoftware OS ISO URL with env variable BFS_OS_ISO_URL=%s", input.BFSOsIsoURL))
		input.BlueFieldSoftware.Spec.OsIso = input.BFSOsIsoURL
	}
	if input.BFSPldmFwBundleURL != "" {
		By(fmt.Sprintf("Override BlueFieldSoftware PLDM FW bundle URL with env variable BFS_PLDM_FW_BUNDLE_URL=%s", input.BFSPldmFwBundleURL))
		input.BlueFieldSoftware.Spec.PldmFwBundle = &input.BFSPldmFwBundleURL
	}
	By("Create the BlueFieldSoftware")
	Eventually(func(g Gomega) {
		bfs := input.BlueFieldSoftware.DeepCopy()
		bfs.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, bfs))).NotTo(HaveOccurred())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Checking that BlueFieldSoftware is ready")
	Eventually(func(g Gomega) {
		bfs := &provisioningv1.BlueFieldSoftware{}
		g.Expect(input.Client.Get(ctx, client.ObjectKey{
			Name:      input.BlueFieldSoftware.Name,
			Namespace: input.BlueFieldSoftware.Namespace,
		}, bfs)).To(Succeed())
		g.Expect(bfs.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// ProvisionDPUFlavor creates the DPUFlavor resource.
func ProvisionDPUFlavor(ctx context.Context, input ProvisionDPUClustersInput) {
	By("Creating the DPUFlavor")
	Eventually(func(g Gomega) {
		dpuFlavor := input.DPUFlavor.DeepCopy()
		dpuFlavor.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, dpuFlavor))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// ProvisionDPUSet DPUSet that will provision DPUs in the background if the environment has such DPUs.
// It doesn't check whether the DPUs become ready intentionally to allow for subsequent tests to be executed in the meantime.
func ProvisionDPUSet(ctx context.Context, input ProvisionDPUClustersInput) {
	Eventually(func(g Gomega) {
		By("Creating the DPUSet")
		dpuset := input.DPUSet.DeepCopy()
		// TODO: Test the cleanup of the node related to the DPU.
		dpuset.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, dpuset))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Checking the DPUServices have been mirrored to the target cluster")
	for _, componentName := range []operatorv1.ComponentName{
		operatorv1.ServiceSetControllerName,
		operatorv1.NVIPAMControllerName,
	} {
		deploymentName := fmt.Sprintf("in-cluster-%s", getPerClusterDPUServiceName(componentName, input.DPUClusters[0].Name, input.DPUClusters[0].Namespace))
		Eventually(func(g Gomega) {
			deployment := &appsv1.Deployment{}
			g.Expect(input.Client.Get(ctx, client.ObjectKey{
				Namespace: DPFOperatorSystemNamespace,
				Name:      deploymentName},
				deployment)).To(Succeed())
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
		}).WithTimeout(600 * time.Second).Should(Succeed())
	}

	By("Checking that DPUService objects have been mirrored to the DPUClusters")
	Eventually(func(g Gomega) {
		deployments := &appsv1.DeploymentList{}
		g.Expect(DPUClusterClient[0].List(ctx, deployments)).To(Succeed())
		found := map[string]bool{}
		for i := range deployments.Items {
			if _, hasAnnotation := deployments.Items[i].GetAnnotations()[ArgoCDTrackingIDAnnotation]; hasAnnotation {
				g.Expect(deployments.Items[i].GetAnnotations()[ArgoCDTrackingIDAnnotation]).NotTo(Equal(""))
				found[deployments.Items[i].GetAnnotations()[ArgoCDTrackingIDAnnotation]] = true
			}
		}
		daemonsets := appsv1.DaemonSetList{}
		g.Expect(DPUClusterClient[0].List(ctx, &daemonsets, client.InNamespace(input.DPUClusters[0].GetNamespace()))).To(Succeed())
		for i := range daemonsets.Items {
			if _, hasAnnotation := daemonsets.Items[i].GetAnnotations()[ArgoCDTrackingIDAnnotation]; hasAnnotation {
				g.Expect(daemonsets.Items[i].GetAnnotations()[ArgoCDTrackingIDAnnotation]).NotTo(Equal(""))
				found[daemonsets.Items[i].GetAnnotations()[ArgoCDTrackingIDAnnotation]] = true
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
	expectedDPUs := input.NumberOfDPUNodes * input.NumberOfDPUsPerNode
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
		g.Expect(DPUClusterClient[0].List(ctx, nodes)).ToNot(HaveOccurred())
		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), expectedDPUs)
		tracker.By(nodeKey, "Checking that the number of nodes %d is equal to %d", len(nodes.Items), expectedDPUs)
		g.Expect(nodes.Items).To(HaveLen(expectedDPUs))
	}).WithTimeout(ProvisioningTimeout).WithPolling(1 * time.Second).Should(Succeed())

	// Verify DPUs are ready
	Eventually(func(g Gomega) {
		g.Expect(verifyExpectedDPUsToBeReady(ctx, tracker, input, expectedDPUs)).To(Succeed())
	}).WithTimeout(20 * time.Minute).Should(Succeed())

}

func verifyExpectedDPUsToBeReady(ctx context.Context, tracker *ByTracker, input ProvisionDPUClustersInput, expectedDPUs int) error {
	dpus := &provisioningv1.DPUList{}
	if err := input.Client.List(ctx, dpus); err != nil {
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

	expectedDPUs := input.NumberOfDPUNodes * input.NumberOfDPUsPerNode

	// Wait for DPUNodeMaintenance CRs to exist with hold annotation set to "true"
	var dpuNodeMaintenanceList *provisioningv1.DPUNodeMaintenanceList
	Eventually(func(g Gomega) {
		dpuNodeMaintenanceList = &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(input.Client.List(ctx, dpuNodeMaintenanceList, client.InNamespace(DPFOperatorSystemNamespace))).To(Succeed())

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
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	// Patch all DPUNodeMaintenance CRs to set hold annotation to "false"
	By("Setting hold annotation to false on all DPUNodeMaintenance CRs to allow provisioning to continue")
	for i := range dpuNodeMaintenanceList.Items {
		if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[i]) {
			Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.Client, &dpuNodeMaintenanceList.Items[i]).WithTimeout(30 * time.Second).Should(Succeed())
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
		g.Expect(input.Client.List(ctx, dpus)).ToNot(HaveOccurred())
		g.Expect(dpus.Items).To(HaveLen(input.NumberOfDPUNodes * input.NumberOfDPUsPerNode))

		for _, dpu := range dpus.Items {
			dpuStatusKey := fmt.Sprintf("%s/%v", dpu.Name, dpu.Status.Phase)
			tracker.By(dpuStatusKey, "DPU %s dpu.Status.Phase=%v", dpu.Name, dpu.Status.Phase)

			if dpu.Status.Phase != provisioningv1.DPUReady {
				dpuKey := client.ObjectKey{Name: dpu.Name, Namespace: dpu.Namespace}
				current := &provisioningv1.DPU{}
				g.Expect(input.Client.Get(ctx, dpuKey, current)).To(Succeed())
				// TODO: update this behavior when retry during provisioning is introduced
				// Failing test instantly when facing Error during provisioning
				Expect(current.Status.Phase).NotTo(Equal(provisioningv1.DPUError))
				g.Expect(current.Status.Phase).To(Equal(provisioningv1.DPURebooting))
			}
		}
	}).WithTimeout(ProvisioningTimeout).Should(Succeed())

	By("Reboot driven by in-cluster script Job (nodeRebootMethod.script); waiting for completion")
	waitForScriptRebootCompletion(ctx, input.Client,
		input.NumberOfDPUNodes*input.NumberOfDPUsPerNode)
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
	}).WithTimeout(30 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())
}

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
		g.Expect(kclient.Get(ctx, client.ObjectKey{Namespace: DPFOperatorSystemNamespace, Name: ConfigName}, dpfOperatorConfig)).To(Succeed())
		g.Expect(conditions.IsTrue(dpfOperatorConfig, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(timeout).WithPolling(1 * time.Second).Should(Succeed())
}

// VerifyProvisioningControllerPodsArg waits and verifies that all provisioning controller pods have the given argument
// in their manager container args.
func VerifyProvisioningControllerPodsArg(ctx context.Context, kclient client.Client, arg string, timeout time.Duration) {
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(kclient.List(ctx, pods,
			client.InNamespace(DPFOperatorSystemNamespace),
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
		err := input.Client.List(ctx, workerNodes, client.InNamespace(DPFOperatorSystemNamespace), client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
		g.Expect(err).NotTo(HaveOccurred())
		return len(workerNodes.Items)
	}, time.Second*30, time.Millisecond*250).Should(Equal(0))

	By("Verify DPU devices are not present")
	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) int {
		err := input.Client.List(ctx, dpuDeviceList, client.InNamespace(input.SystemNamespace))
		g.Expect(err).NotTo(HaveOccurred())
		return len(dpuDeviceList.Items)
	}, time.Second*30, time.Millisecond*250).Should(Equal(0))

	By("Creating DpuDiscovery")
	Expect(input.DPUDiscovery).NotTo(BeNil(), "dpuDiscovery config is required for ZeroTrust")
	discovery := input.DPUDiscovery.DeepCopy()
	discovery.SetNamespace(input.SystemNamespace)
	discovery.SetLabels(CleanupScope.Suite)

	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, discovery))).NotTo(HaveOccurred())

	By("Waiting for DPU discovery to complete and create DPU devices")
	dpuDeviceList = &provisioningv1.DPUDeviceList{}
	Eventually(func(g Gomega) int {
		err := input.Client.List(ctx, dpuDeviceList, client.InNamespace(input.SystemNamespace))
		g.Expect(err).NotTo(HaveOccurred())
		return len(dpuDeviceList.Items)
	}, time.Minute*5, time.Millisecond*250).Should(Equal(input.NumberOfDPUNodes))
}

// ValidateDPUAgentStatus verifies that the DPU agent has reported its status correctly
// on every ready DPU. Each DPU is validated against the supplied expected AgentStatus.
func ValidateDPUAgentStatus(ctx context.Context, input *SystemTestInput, expected provisioningv1.AgentStatus) {
	if !input.HasDpuNodes() {
		Skip("Skip DPU Agent validation as there are no DPU nodes")
	}

	expectedDPUs := input.TotalDPUs()

	By("Listing all DPUs and validating agent status")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.Client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(expectedDPUs), "expected %d DPUs", expectedDPUs)

		for i := range dpus.Items {
			validateSingleDPUAgentStatus(g, &dpus.Items[i], expected)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

// validateSingleDPUAgentStatus checks one DPU's agent-reported status against the expected AgentStatus.
// Pointer fields that are nil in expected are skipped (not validated).
// Non-nil pointer fields are validated for equality with the actual value.
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

	g.Expect(actualAgentStatus.KubeletVersion).NotTo(BeNil(),
		"DPU %s AgentStatus.KubeletVersion should be reported", dpu.Name)
	g.Expect(*actualAgentStatus.KubeletVersion).NotTo(BeEmpty(),
		"DPU %s AgentStatus.KubeletVersion should not be empty", dpu.Name)

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
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue),
			"DPU %s agent condition %q should be True, got %v (reason: %s, message: %s)",
			dpu.Name, cond.Type, cond.Status, cond.Reason, cond.Message)
		g.Expect(cond.Reason).NotTo(BeEmpty(),
			"DPU %s agent condition %q should have a non-empty Reason", dpu.Name, cond.Type)
		g.Expect(cond.LastTransitionTime).NotTo(BeZero(),
			"DPU %s agent condition %q should have LastTransitionTime set", dpu.Name, cond.Type)
	}
}

// verifyDPUServicesReady checks that the DPUService is ready.
func verifyDPUServicesReady(ctx context.Context, input *SystemTestInput, dpuServiceNamespace string, dpuServiceName []string) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		for _, name := range dpuServiceName {
			tracker.By(name, "verify DPUService %s is ready", name)
			dpuService := &dpuservicev1.DPUService{}
			g.Expect(input.Client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: name}, dpuService)).To(Succeed())
			g.Expect(conditions.IsTrue(dpuService, conditions.TypeReady)).To(BeTrue())
		}
		// A timeout of 20 minutes is necessary here. We have alot of trouble pulling our images for all
		// DPUServices on the DPUCluster, so we need to wait for the images to be pulled and the pods to be ready.
	}).WithTimeout(20 * time.Minute).Should(Succeed())
}

// getDPUClusterClient retrieves the DPUCluster client for the cluster at the given index. This function is internal and should not be called directly.
// Instead, use getDPUClusterClients to retrieve all clients for all clusters.
func getDPUClusterClient(ctx context.Context, input ProvisionDPUClustersInput, clusterIndex int) {
	var tun *tunnel.Tunnel

	Eventually(func(g Gomega) {
		refreshable, ok := DPUClusterClient[clusterIndex].(*refreshableclient.Client)
		g.Expect(ok).To(BeTrue(), "DPUCluster client %d should be a refreshable client", clusterIndex)

		g.Expect(input.Client.Get(ctx, client.ObjectKeyFromObject(input.DPUClusters[clusterIndex]), input.DPUClusters[clusterIndex])).To(Succeed())
		g.Expect(input.DPUClusters[clusterIndex].Spec.Kubeconfig).ToNot(BeEmpty(), "DPUCluster kubeconfig should be populated")

		var err error
		var restCfg *rest.Config
		restCfg, tun, err = tunnel.NewTunneledRestConfig(ctx, input.Client, input.RestConfig, input.DPUClusters[clusterIndex])
		g.Expect(err).NotTo(HaveOccurred(), "Should create tunneled REST config")

		dpuClient, err := client.New(restCfg, client.Options{})
		g.Expect(err).NotTo(HaveOccurred(), "Should create tunneled client")
		refreshable.Set(dpuClient)

		// Setup the dpuClusterRestClient
		restCfg.APIPath = "/api"
		restCfg.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
		restCfg.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
		DPUClusterRestConfig[clusterIndex] = restCfg
		DPUClusterRestClient[clusterIndex], err = rest.RESTClientFor(restCfg)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	// Start a go routine that monitors the health of the tunnel and recreates the client and rest config
	// if the health check fails.
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
	numClusters := len(input.DPUClusters)
	DPUClusterClient = make([]client.Client, numClusters)
	DPUClusterRestConfig = make([]*rest.Config, numClusters)
	DPUClusterRestClient = make([]*rest.RESTClient, numClusters)
	for i := range input.DPUClusters {
		DPUClusterClient[i] = refreshableclient.New()
	}

	for i := range input.DPUClusters {
		getDPUClusterClient(ctx, input, i)
	}
}

const bmcIPLabelKey = "host-bmc-ip"

// GetDPUNodeToBMCIPs ensures the expected DPUNodes exist and maps each DPUNode name to its host BMC IP based on a static inventory file.
// In Zero-Trust mode, this mapping validates each DPUNode against the inventory, failing clearly if not found.
// bmcInventoryPath is sourced from $E2E_ZT_BMC_INVENTORY_PATH by getEnvVariables() and required-ness is enforced by validateFlags() for ZT runs.
func GetDPUNodeToBMCIPs(ctx context.Context, c client.Client,
	expectedDPUNodes int) map[string]string {

	raw, err := os.ReadFile(bmcInventoryPath)
	Expect(err).NotTo(HaveOccurred(),
		"reading BMC inventory at %s (set $E2E_ZT_BMC_INVENTORY_PATH to a valid path)",
		bmcInventoryPath)
	inventory := map[string]string{}
	Expect(yaml.Unmarshal(raw, &inventory)).To(Succeed(),
		"parsing %s", bmcInventoryPath)

	By(fmt.Sprintf("Resolving DPUNode -> host BMC IP for %d DPUNodes via %s",
		expectedDPUNodes, bmcInventoryPath))
	var observed []provisioningv1.DPUNode
	Eventually(func(g Gomega) {
		nodes := &provisioningv1.DPUNodeList{}
		g.Expect(c.List(ctx, nodes, client.InNamespace(DPFOperatorSystemNamespace))).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(expectedDPUNodes))
		observed = nodes.Items
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	out := make(map[string]string, len(observed))
	for i := range observed {
		serial := strings.TrimPrefix(strings.ToLower(observed[i].Name), "dpu-node-")
		bmcIP, ok := inventory[serial]
		Expect(ok).To(BeTrue(),
			"DPUNode %q has no entry in %s; add the DPU serial there",
			observed[i].Name, bmcInventoryPath)
		out[observed[i].Name] = bmcIP
	}
	return out
}

// Name of the Secret referenced by the reboot ConfigMap pod-template to provide the BMC password.
// Must match between Go code and the YAML fixture for script-based node reboot.
const (
	bmcCredentialsSecretName = "dpunode-reboot-bmc-credentials"
	bmcPasswordSecretKey     = "BMC_PASSWORD"
)

// ApplyNodeRebootConfigMap creates the BMC credentials Secret (sourced from
// $E2E_ZT_BMC_PASSWORD) and applies the reboot ConfigMap fixture as-is.
func ApplyNodeRebootConfigMap(ctx context.Context, c client.Client, configMapPath string) {

	applyBMCCredentialsSecret(ctx, c)

	By(fmt.Sprintf("Applying node reboot ConfigMap fixture %s", configMapPath))
	data, err := os.ReadFile(configMapPath)
	Expect(err).ToNot(HaveOccurred(), "reading node reboot ConfigMap fixture")

	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	if obj.GetNamespace() == "" {
		obj.SetNamespace(DPFOperatorSystemNamespace)
	}
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	maps.Copy(labels, CleanupScope.Suite)
	obj.SetLabels(labels)
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, obj))).To(Succeed())
}

// applyBMCCredentialsSecret creates a suite-scoped Secret for the reboot pod with the BMC password, ensuring it is not exposed in the ConfigMap.
// bmcPassword is sourced from $E2E_ZT_BMC_PASSWORD by getEnvVariables() and required-ness is enforced by validateFlags() for ZT runs.
func applyBMCCredentialsSecret(ctx context.Context, c client.Client) {
	By(fmt.Sprintf("Creating BMC credentials Secret %s/%s",
		DPFOperatorSystemNamespace, bmcCredentialsSecretName))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmcCredentialsSecretName,
			Namespace: DPFOperatorSystemNamespace,
			Labels:    maps.Clone(CleanupScope.Suite),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
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
		g.Expect(c.List(ctx, nodes, client.InNamespace(DPFOperatorSystemNamespace))).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(expectedDPUNodes))
		observed = nodes.Items
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

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

func unstructuredFromFile(path string) *unstructured.Unstructured {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	obj.SetLabels(CleanupScope.Suite)
	return obj
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
