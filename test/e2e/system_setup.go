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
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl"
	operatorutils "github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/collector"
	"github.com/nvidia/doca-platform/test/utils/tunnel"

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
	numberOfDPUNodes        int
	numberOfDPUsPerNode     int
	dpuClusterPrerequisites []client.Object
	dpuClusters             []*provisioningv1.DPUCluster
	dpuFlavor               *provisioningv1.DPUFlavor
	bfb                     *provisioningv1.BFB
	dpuSet                  *provisioningv1.DPUSet
	client                  client.Client
	bfbImageURL             string
	restConfig              *rest.Config
	HostRebootScript        string
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
type systemTestInput struct {
	namespace                   string
	config                      *operatorv1.DPFOperatorConfig
	pvc                         *corev1.PersistentVolumeClaim
	dpuClusterPrerequisites     []client.Object
	dpuClusters                 []*provisioningv1.DPUCluster
	dpuFlavor                   *provisioningv1.DPUFlavor
	dpuDiscovery                *provisioningv1.DPUDiscovery
	dpuService                  *dpuservicev1.DPUService
	dpuServiceHBN               *dpuservicev1.DPUService
	dpuServiceInterface         *dpuservicev1.DPUServiceInterface
	dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface
	dpuServiceChain             *dpuservicev1.DPUServiceChain
	dpuServiceChainTemplate     *dpuservicev1.DPUServiceChain
	bfb                         *provisioningv1.BFB
	dpuSet                      *provisioningv1.DPUSet
	dpuDeployment               *dpuservicev1.DPUDeployment
	dpuServiceConfiguration     *dpuservicev1.DPUServiceConfiguration
	dpuServiceTemplate          *dpuservicev1.DPUServiceTemplate
	dpuServiceIPAMTemplate      *dpuservicev1.DPUServiceIPAM
	dpuServiceNAD               *dpuservicev1.DPUServiceNAD
	cidrDPUServiceIPAM          *dpuservicev1.DPUServiceIPAM
	ipPoolDPUServiceIPAM        *dpuservicev1.DPUServiceIPAM
	dpuServiceCredentialRequest *dpuservicev1.DPUServiceCredentialRequest
	numberOfDPUNodes            int
	numberOfDPUsPerNode         int
	useExternalNodeReboot       bool
	pullSecretNames             []string
	client                      client.Client
	cleanupFlags                *cleanup.CleanupFlags
	bfbImageURL                 string
	restConfig                  *rest.Config
	HostRebootScript            string
}

func (t *systemTestInput) applySDNConfig(conf config) {
	dpuServiceInterfaceTemplate := &dpuservicev1.DPUServiceInterface{}
	dsiTemplate := unstructuredFromFile(conf.DPUServiceInterfaceTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dsiTemplate.Object, dpuServiceInterfaceTemplate)).To(Succeed())
	t.dpuServiceInterfaceTemplate = dpuServiceInterfaceTemplate

	dpuServiceHBN := &dpuservicev1.DPUService{}
	svcHBN := unstructuredFromFile(conf.DPUServiceHBNPath)

	// Override HBN image if HBN_IMAGE_URL is set
	if hbnImageURL != "" {
		parts := strings.SplitN(hbnImageURL, ":", 2)
		repository := parts[0]
		tag := parts[1]
		updateHBNImage(svcHBN, repository, tag)
	}

	if ngcAPIKey != "" {
		updateImagePullSecret(svcHBN, ngcPullSecretName)
	}

	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcHBN.Object, dpuServiceHBN)).To(Succeed())
	t.dpuServiceHBN = dpuServiceHBN

	dpuServiceIPAMTemplate := &dpuservicev1.DPUServiceIPAM{}
	ipam := unstructuredFromFile(conf.DPUServiceIPAMTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ipam.Object, dpuServiceIPAMTemplate)).To(Succeed())
	t.dpuServiceIPAMTemplate = dpuServiceIPAMTemplate

	dpuServiceNAD := &dpuservicev1.DPUServiceNAD{}
	nad := unstructuredFromFile(conf.DPUServiceNADPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(nad.Object, dpuServiceNAD)).To(Succeed())
	t.dpuServiceNAD = dpuServiceNAD

	dpuServiceChainTemplate := &dpuservicev1.DPUServiceChain{}
	chainTemplate := unstructuredFromFile(conf.DPUServiceChainTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(chainTemplate.Object, dpuServiceChainTemplate)).To(Succeed())
	t.dpuServiceChainTemplate = dpuServiceChainTemplate
}

func updateHBNImage(svcHBN *unstructured.Unstructured, repository, tag string) {
	err := unstructured.SetNestedField(svcHBN.Object, repository,
		"spec", "helmChart", "values", "image", "repository")
	Expect(err).ToNot(HaveOccurred())

	err = unstructured.SetNestedField(svcHBN.Object, tag,
		"spec", "helmChart", "values", "image", "tag")
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

func (t *systemTestInput) applyConfig(conf config) {
	bfb := &provisioningv1.BFB{}
	bfbUnstructured := unstructuredFromFile(conf.BFBPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(bfbUnstructured.Object, bfb)).To(Succeed())
	t.bfb = bfb

	dpuSet := &provisioningv1.DPUSet{}
	dpuSetUnstructured := unstructuredFromFile(conf.DPUSetPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuSetUnstructured.Object, dpuSet)).To(Succeed())
	t.dpuSet = dpuSet

	pvc := &corev1.PersistentVolumeClaim{}
	if conf.ProvisioningControllerPVCPath != nil {
		pvcUnstructured := unstructuredFromFile(*conf.ProvisioningControllerPVCPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(pvcUnstructured.Object, pvc)).To(Succeed())
		t.pvc = pvc
	}

	// Load all DPU clusters
	t.dpuClusters = make([]*provisioningv1.DPUCluster, 0, len(conf.DPUClusterPaths))
	for _, dpuClusterPath := range conf.DPUClusterPaths {
		dpuCluster := &provisioningv1.DPUCluster{}
		dpuClusterUnstructured := unstructuredFromFile(dpuClusterPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuClusterUnstructured.Object, dpuCluster)).To(Succeed())
		// Override interface if DPUCLUSTER_INTERFACE environment variable is set
		if dpuClusterInterface != "" && dpuCluster.Spec.ClusterEndpoint != nil && dpuCluster.Spec.ClusterEndpoint.Keepalived != nil {
			By(fmt.Sprintf("Overriding DPUCluster interface with DPUCLUSTER_INTERFACE=%s", dpuClusterInterface))
			dpuCluster.Spec.ClusterEndpoint.Keepalived.Interface = dpuClusterInterface
		}
		t.dpuClusters = append(t.dpuClusters, dpuCluster)
	}

	if conf.DPUDiscoveryPath != nil {
		dpuDiscovery := &provisioningv1.DPUDiscovery{}
		dpuDiscoveryUnstructured := unstructuredFromFile(*conf.DPUDiscoveryPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuDiscoveryUnstructured.Object, dpuDiscovery)).To(Succeed())
		t.dpuDiscovery = dpuDiscovery
	}

	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
	dsi := unstructuredFromFile(conf.DPUServiceInterfacePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dsi.Object, dpuServiceInterface)).To(Succeed())
	t.dpuServiceInterface = dpuServiceInterface

	dpuService := &dpuservicev1.DPUService{}
	svc := unstructuredFromFile(conf.DPUServicePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svc.Object, dpuService)).To(Succeed())
	t.dpuService = dpuService

	dpuClusterPrerequisiteObjects := []client.Object{}
	for _, path := range conf.DPUClusterPrerequisiteObjectPaths {
		dpuClusterPrerequisiteObjects = append(dpuClusterPrerequisiteObjects, unstructuredFromFile(path))
	}
	t.dpuClusterPrerequisites = dpuClusterPrerequisiteObjects

	dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
	tmp := unstructuredFromFile(conf.DPUServiceTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(tmp.Object, dpuServiceTemplate)).To(Succeed())
	t.dpuServiceTemplate = dpuServiceTemplate

	dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
	svcConfig := unstructuredFromFile(conf.DPUServiceConfiguration)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcConfig.Object, dpuServiceConfiguration)).To(Succeed())
	t.dpuServiceConfiguration = dpuServiceConfiguration

	dpuDeployment := &dpuservicev1.DPUDeployment{}
	deployment := unstructuredFromFile(conf.DPUDeploymentPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(deployment.Object, dpuDeployment)).To(Succeed())
	t.dpuDeployment = dpuDeployment

	dpuFlavor := &provisioningv1.DPUFlavor{}
	if conf.DPUFlavorPath != nil {
		dpuFlavorUnstructured := unstructuredFromFile(*conf.DPUFlavorPath)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuFlavorUnstructured.Object, dpuFlavor)).To(Succeed())
		t.dpuFlavor = dpuFlavor
		t.dpuDeployment.Spec.DPUs.Flavor = dpuFlavor.Name
		t.dpuSet.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor.Name
	}

	ipPoolDPUServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
	subnetIPAM := unstructuredFromFile(conf.IPPoolDPUServiceIPAMPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(subnetIPAM.Object, ipPoolDPUServiceIPAM)).To(Succeed())
	t.ipPoolDPUServiceIPAM = ipPoolDPUServiceIPAM

	cidrDPUServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
	cidrIPAM := unstructuredFromFile(conf.CIDRPoolDPUServiceIPAMPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(cidrIPAM.Object, cidrDPUServiceIPAM)).To(Succeed())
	t.cidrDPUServiceIPAM = cidrDPUServiceIPAM

	dpuServiceChain := &dpuservicev1.DPUServiceChain{}
	chain := unstructuredFromFile(conf.DPUServiceChainPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(chain.Object, dpuServiceChain)).To(Succeed())
	t.dpuServiceChain = dpuServiceChain

	dpuServiceCredentialRequest := &dpuservicev1.DPUServiceCredentialRequest{}
	request := unstructuredFromFile(conf.DPUServiceCredentialRequestPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(request.Object, dpuServiceCredentialRequest)).To(Succeed())
	t.dpuServiceCredentialRequest = dpuServiceCredentialRequest

	t.numberOfDPUNodes = conf.NumberOfDPUNodes
	t.numberOfDPUsPerNode = conf.NumberOfDPUsPerNode
	t.useExternalNodeReboot = conf.UseExternalNodeReboot
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
	By("ensure the DPF Operator is running and ready")
	Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: input.systemNamespace,
			Name:      "dpf-operator-controller-manager"},
			deployment)).To(Succeed())
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("create the PersistentVolumeClaim for the DPF Provisioning controller")
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

	By("creates the imagePullSecrets for the DPFOperatorConfig")
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

	By("create the DPFOperatorConfig for the system")
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, input.operatorConfig))).NotTo(HaveOccurred())

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Deploy DPUDiscovery for ZeroTrust")
		CreateDPUDiscovery(ctx, input)
	}

	By("ensure the DPF controllers are running and ready")
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
		By("verify bfb-registry Service and pods (created by provisioning controller leader)")
		Eventually(func(g Gomega) {
			const bfbRegistryNodePort = 30082
			svc := &corev1.Service{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: input.systemNamespace,
				Name:      "bfb-registry",
			}, svc)).To(Succeed(), "bfb-registry Service should be created by provisioning controller leader")
			g.Expect(svc.Spec.Ports).ToNot(BeEmpty())
			g.Expect(svc.Spec.Ports[0].NodePort).To(Equal(int32(bfbRegistryNodePort)),
				"bfb-registry Service should have NodePort %d", bfbRegistryNodePort)
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
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}

	By("ensure the system DPUServices are created")
	Eventually(func(g Gomega) {
		// TODO: Remove as soon as we have version aware upgrade logic for the pre-upgrade validation
		gotDPFOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(input.operatorConfig), gotDPFOperatorConfig)).NotTo(HaveOccurred())
		g.Expect(gotDPFOperatorConfig.Status.Version).NotTo(BeNil())
		isCurrentVersionLastReleasedGA := operatorutils.IsUpgradeFromLastReleasedGA(*gotDPFOperatorConfig.Status.Version)

		dpuServices := &dpuservicev1.DPUServiceList{}
		g.Expect(testClient.List(ctx, dpuServices)).To(Succeed())

		itemNames := []string{}
		for _, item := range dpuServices.Items {
			itemNames = append(itemNames, item.Name)
		}

		// Validate the expected number of DPUServices.
		// If: standard e2e run, or post-upgrade phase of the upgrade test (current branch state).
		// Else: initial phase of the upgrade test (deployed from the last GA release).
		if !isCurrentVersionLastReleasedGA {
			g.Expect(dpuServices.Items).To(HaveLen(10),
				"Expected 10 DPUServices, got %d: [%s]", len(dpuServices.Items), strings.Join(itemNames, ", "))
		} else {
			g.Expect(dpuServices.Items).To(HaveLen(9),
				"Expected 9 DPUServices, got %d: [%s]", len(dpuServices.Items), strings.Join(itemNames, ", "))
		}

		found := map[string]bool{}
		for i := range dpuServices.Items {
			found[dpuServices.Items[i].Name] = true
		}

		// Validate the expected DPUServices by installation phase.
		// If: standard e2e run, or post-upgrade phase of the upgrade test (current branch state).
		// Else: initial phase of the upgrade test (deployed from the last GA release).
		if !isCurrentVersionLastReleasedGA {
			g.Expect(found).To(HaveKey(operatorv1.ServiceChainSetCRDsName.String()))
			g.Expect(found).To(HaveKey(operatorv1.KubeStateMetricsRBACName.String()))
			g.Expect(found).To(HaveKey(operatorv1.NodeProblemDetectorName.String()))
		} else {
			g.Expect(found).To(HaveKey(operatorv1.ServiceSetControllerName.String()))
		}

		// Expect each of the following to have been created by the operator.
		g.Expect(found).To(HaveKey(operatorv1.MultusName.String()))
		g.Expect(found).To(HaveKey(operatorv1.SRIOVDevicePluginName.String()))
		g.Expect(found).To(HaveKey(operatorv1.FlannelName.String()))
		g.Expect(found).To(HaveKey(operatorv1.NVIPAMName.String()))
		g.Expect(found).To(HaveKey(operatorv1.OVSCNIName.String()))
		g.Expect(found).To(HaveKey(operatorv1.SFCControllerName.String()))
		g.Expect(found).To(HaveKey(operatorv1.CNIInstallerName.String()))
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// ProvisionDPUClusters provisions DPUClusters.
func ProvisionDPUClusters(ctx context.Context, input ProvisionDPUClustersInput) {
	By("create prerequisites objects for DPUClusters")
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

	By("create DPUClusters")
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
		}
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("creating a client for the DPUCluster")
	getDPUClusterClients(ctx, input)
}

// ProvisionBFBAndDPUFlavor creates the BFB and optionally the DPUFlavor resources
func ProvisionBFBAndDPUFlavor(ctx context.Context, input ProvisionDPUClustersInput) {
	// TODO: Pass this in as config instead of as a global.
	if input.bfbImageURL != "" {
		By(fmt.Sprintf("override BFB URL with env variable BFB_IMAGE_URL=%s", input.bfbImageURL))
		input.bfb.Spec.URL = input.bfbImageURL
	}
	By("create the BFB and DPUFlavor")
	Eventually(func(g Gomega) {
		By("creating the BFB")
		bfb := input.bfb.DeepCopy()
		bfb.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, bfb))).NotTo(HaveOccurred())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	Eventually(func(g Gomega) {
		// Return if no DPUFlavor is provided.
		if input.dpuFlavor == nil {
			return
		}
		By("Creating the DPUFlavor")
		dpuFlavor := input.dpuFlavor.DeepCopy()
		dpuFlavor.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuFlavor))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("checking that BFB is ready")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)).To(Succeed())
		g.Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady))
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// ProvisionDPUSet DPUSet that will provision DPUs in the background if the environment has such DPUs.
// It doesn't check whether the DPUs become ready intentionally to allow for subsequent tests to be executed in the meantime.
func ProvisionDPUSet(ctx context.Context, input ProvisionDPUClustersInput) {
	Eventually(func(g Gomega) {
		By("Creating the DPUSet")
		dpuset := input.dpuSet.DeepCopy()
		// TODO: Test the cleanup of the node related to the DPU.
		dpuset.SetLabels(CleanupScope.Suite)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuset))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Checking the DPUServices have been mirrored to the target cluster")
	Eventually(func(g Gomega) {
		serviceSetDeployment := &appsv1.Deployment{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      fmt.Sprintf("in-cluster-%s", getServiceChainSetControllerDPUServiceName(input.dpuClusters[0].Name, input.dpuClusters[0].Namespace))},
			serviceSetDeployment)).To(Succeed())
		g.Expect(serviceSetDeployment.Status.ReadyReplicas).To(Equal(*serviceSetDeployment.Spec.Replicas))
	}).WithTimeout(600 * time.Second).Should(Succeed())

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
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.NVIPAMName.String())))
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSCNIName.String())))
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SFCControllerName.String())))
	}).WithTimeout(600 * time.Second).Should(Succeed())
}

// VerifyDPUClusterWithNodes waits and verifies if the DPUCluster has nodes meaning that there were DPUs provisioned. In
// addition verifies that the DPUs become ready.
// Note: Each DPU joins the DPU cluster as a separate K8s node, so the number of nodes in the DPU cluster equals totalDPUs.
func VerifyDPUClusterWithNodes(ctx context.Context, input ProvisionDPUClustersInput) {
	expectedDPUs := input.numberOfDPUNodes * input.numberOfDPUsPerNode
	tracker := NewByTracker()

	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		ProcessDPUNodeMaintenanceHold(ctx, input)
		RebootAndVerifyDPU(ctx, input)
	}

	// Verify nodes are present in DPUCluster,
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).ToNot(HaveOccurred())
		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), expectedDPUs)
		tracker.By(nodeKey, "Checking that the number of nodes %d is equal to %d", len(nodes.Items), expectedDPUs)
		g.Expect(nodes.Items).To(HaveLen(expectedDPUs))
	}).WithTimeout(45 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	// Verify DPUs are ready
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).ToNot(HaveOccurred())
		g.Expect(dpus.Items).To(HaveLen(expectedDPUs))
		for _, dpu := range dpus.Items {
			dpuStatusKey := fmt.Sprintf("%s/%v", dpu.Name, dpu.Status.Phase)
			tracker.By(dpuStatusKey, "DPU %s dpu.Status.Phase=%v", dpu.Name, dpu.Status.Phase)
			g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady))
		}
	}).WithTimeout(20 * time.Minute).Should(Succeed())

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
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	// Patch all DPUNodeMaintenance CRs to set hold annotation to "false"
	By("Setting hold annotation to false on all DPUNodeMaintenance CRs to allow provisioning to continue")
	for i := range dpuNodeMaintenanceList.Items {
		if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[i]) {
			Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.client, &dpuNodeMaintenanceList.Items[i]).WithTimeout(30 * time.Second).Should(Succeed())
			By(fmt.Sprintf("Released hold on DPUNodeMaintenance %s", dpuNodeMaintenanceList.Items[i].Name))
		}
	}
}

// RebootAndVerifyDPU waits and verifies that the DPUs expecting reboot, triggers node and dpu reboot via power cycle
// here when expected condition met.
// In addition removes annotation from rebooted node after the reboot is finished.
// applies to ZeroTrust only
func RebootAndVerifyDPU(ctx context.Context, input ProvisionDPUClustersInput) {
	tracker := NewByTracker()
	// Verify DPUs are present in DPUCluster
	dpus := &provisioningv1.DPUList{}

	// applies to ZeroTrust only
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
	}).WithTimeout(45 * time.Minute).Should(Succeed())

	By("Trigger host reboot via script for all DPUs requiring reboot")
	Expect(input.client.List(ctx, dpus)).ToNot(HaveOccurred())
	for i := range dpus.Items {
		dpuKey := client.ObjectKey{Name: dpus.Items[i].Name, Namespace: dpus.Items[i].Namespace}
		current := &provisioningv1.DPU{}
		Expect(input.client.Get(ctx, dpuKey, current)).To(Succeed())

		if current.Status.Phase == provisioningv1.DPURebooting {
			nodeName := fmt.Sprintf("worker%d", i+1)
			By(fmt.Sprintf("Trigger host %s reboot via script for DPU %s", nodeName, current.Name))
			err := RebootHostByScript(input.HostRebootScript, nodeName)
			Expect(err).NotTo(HaveOccurred(), "Reboot host script failed for %s: %v", nodeName, err)
		}
	}

	By("Waiting for SSH connectivity to all hosts after reboot")
	Eventually(func(g Gomega) {
		for i := 0; i < input.numberOfDPUNodes; i++ {
			nodeName := fmt.Sprintf("worker%d", i+1)
			By(fmt.Sprintf("Checking SSH connectivity for %s", nodeName))

			// Try SSH with timeout
			cmd := exec.Command("ssh",
				"-o", "ConnectTimeout=3",
				"-o", "BatchMode=yes",
				"-o", "StrictHostKeyChecking=no",
				nodeName,
				"true")

			err := cmd.Run()
			g.Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("SSH connectivity check failed for %s", nodeName))
		}
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Removing provisioning.dpu.nvidia.com/dpunode-external-reboot-required annotation from DPUNodes after the host reboot")
	nodes := &provisioningv1.DPUNodeList{}
	Expect(input.client.List(ctx, nodes)).ToNot(HaveOccurred())

	for _, node := range nodes.Items {
		annotation := "provisioning.dpu.nvidia.com/dpunode-external-reboot-required"

		if _, ok := node.GetAnnotations()[annotation]; ok {
			patch := client.MergeFrom(node.DeepCopy())
			delete(node.Annotations, annotation)
			Expect(input.client.Patch(ctx, &node, patch)).ToNot(HaveOccurred(),
				fmt.Sprintf("Failed to patch DPUNode %s", node.Name))
		}
	}
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
		g.Expect(kclient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, dpfOperatorConfig)).To(Succeed())
		g.Expect(conditions.IsTrue(dpfOperatorConfig, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(timeout).WithPolling(1 * time.Second).Should(Succeed())
}

// CreateDPUDiscovery verifies no worker nodes and no DPUDevices are in the host cluster.
// Creates DPUDiscovery and verifies DPUDevices were found and added.
func CreateDPUDiscovery(ctx context.Context, input DeployDPFSystemComponentsInput) {
	By("Verify worker nodes are not present")
	workerNodes := &corev1.NodeList{}
	Eventually(func() int {
		err := input.client.List(ctx, workerNodes, client.InNamespace(dpfOperatorSystemNamespace), client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
		Expect(err).NotTo(HaveOccurred())
		return len(workerNodes.Items)
	}, time.Second*30, time.Millisecond*250).Should(Equal(0))

	By("Verify DPU devices are not present")
	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	Eventually(func() int {
		err := input.client.List(ctx, dpuDeviceList, client.InNamespace(input.systemNamespace))
		Expect(err).NotTo(HaveOccurred())
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
	Eventually(func() int {
		err := input.client.List(ctx, dpuDeviceList, client.InNamespace(input.systemNamespace))
		Expect(err).NotTo(HaveOccurred())
		return len(dpuDeviceList.Items)
	}, time.Minute*5, time.Millisecond*250).Should(Equal(input.numberOfDPUNodes))
}

// verifyDPUServicesReady checks that the DPUService is ready.
func verifyDPUServicesReady(ctx context.Context, input *systemTestInput, dpuServiceNamespace string, dpuServiceName []string) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		for _, name := range dpuServiceName {
			tracker.By(name, "verify DPUService %s is ready", name)
			dpuService := &dpuservicev1.DPUService{}
			g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: name}, dpuService)).To(Succeed())
			g.Expect(conditions.IsTrue(dpuService, conditions.TypeReady)).To(BeTrue())
		}
		// A timeout of 20 minutes is necessary here. We have alot of trouble pulling our images for all
		// DPUServices on the DPUCluster, so we need to wait for the images to be pulled and the pods to be ready.
	}).WithTimeout(20 * time.Minute).Should(Succeed())
}

// getDPUClusterClient retrieves the DPUCluster client for the cluster at the given index. This function is internal and should not be called directly.
// Instead, use getDPUClusterClients to retrieve all clients for all clusters.
func getDPUClusterClient(ctx context.Context, input ProvisionDPUClustersInput, clusterIndex int) {
	var clientHealthCheck func() bool
	var restConfigHealthCheck func() bool

	Eventually(func(g Gomega) {
		// Use the new tunnel helper to create a client and the restConfig for the Kamaji cluster
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(input.dpuClusters[clusterIndex]), input.dpuClusters[clusterIndex])).To(Succeed())
		g.Expect(input.dpuClusters[clusterIndex].Spec.Kubeconfig).ToNot(BeEmpty(), "DPUCluster kubeconfig should be populated")

		dpuClusterClient[clusterIndex], clientHealthCheck = tunnel.NewTunneledClient(ctx, input.client, input.restConfig, input.dpuClusters[clusterIndex])
		dpuClusterRestConfig[clusterIndex], restConfigHealthCheck = tunnel.NewTunneledRestConfig(ctx, input.client, input.restConfig, input.dpuClusters[clusterIndex])
		// Setup the dpuClusterRestClient
		dpuClusterRestConfig[clusterIndex].APIPath = "/api"
		dpuClusterRestConfig[clusterIndex].GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
		dpuClusterRestConfig[clusterIndex].NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
		var err error
		dpuClusterRestClient[clusterIndex], err = rest.RESTClientFor(dpuClusterRestConfig[clusterIndex])
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
				if !clientHealthCheck() {
					By("Tunneled client health check failed, recreating client and rest config")
					getDPUClusterClient(ctx, input, clusterIndex)
					// Exit this goroutine as a new one will be created
					return
				}
				if !restConfigHealthCheck() {
					By("Tunneled rest config health check failed, recreating client and rest config")
					getDPUClusterClient(ctx, input, clusterIndex)
					// Exit this goroutine as a new one will be created
					return
				}
			}
		}
	}()
}

// getDPUClusterClients retrieves the DPUCluster clients for all clusters in the input.
// This function must only be called once per test suite as it reinitializes global client connections.
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
		getDPUClusterClient(ctx, input, i)
	}
}

func RebootHostByScript(rebootHostScript string, hostName string) error {
	cmd := exec.Command(rebootHostScript, hostName)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
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
	_, basePath, _, _ := runtime.Caller(0)
	inventoryManifestsPath := filepath.Join(filepath.Dir(basePath), "../../internal/operator/inventory/manifests")
	artifactsPath := filepath.Join(input.artifactsDir, testName)

	cc := collector.ClusterCollector{
		Client:     input.testClient,
		ClientSet:  input.clientset,
		RestConfig: input.restConfig,
	}

	// Create a resourceCollector to dump logs and resources for test debugging.
	clusters, err := collector.GetClusterCollectors(ctx, cc, artifactsPath, inventoryManifestsPath)
	Expect(err).NotTo(HaveOccurred())
	return collector.New(clusters).Run(ctx)
}
