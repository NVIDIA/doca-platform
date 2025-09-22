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

package e2e

import (
	"context"
	"fmt"
	"os"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProvisionDPUClustersInput struct {
	numberOfNodesPerCluster int
	dpuClusterPrerequisites []client.Object
	dpuCluster              *provisioningv1.DPUCluster
	dpuFlavor               *provisioningv1.DPUFlavor
	bfb                     *provisioningv1.BFB
	dpuSet                  *provisioningv1.DPUSet
	client                  client.Client
	bfbImageURL             string
	restConfig              *rest.Config
}

type systemTestInput struct {
	namespace                     string
	config                        *operatorv1.DPFOperatorConfig
	pvc                           *corev1.PersistentVolumeClaim
	additionalProvisioningObjects []client.Object
	dpuCluster                    *provisioningv1.DPUCluster
	dpuFlavor                     *provisioningv1.DPUFlavor
	dpuService                    *dpuservicev1.DPUService
	dpuServiceHBN                 *dpuservicev1.DPUService
	dpuServiceInterface           *dpuservicev1.DPUServiceInterface
	dpuServiceInterfaceTemplate   *dpuservicev1.DPUServiceInterface
	dpuServiceChain               *dpuservicev1.DPUServiceChain
	dpuServiceChainTemplate       *dpuservicev1.DPUServiceChain
	bfb                           *provisioningv1.BFB
	dpuSet                        *provisioningv1.DPUSet
	dpuDeployment                 *dpuservicev1.DPUDeployment
	dpuServiceConfiguration       *dpuservicev1.DPUServiceConfiguration
	dpuServiceTemplate            *dpuservicev1.DPUServiceTemplate
	dpuServiceIPAMTemplate        *dpuservicev1.DPUServiceIPAM
	cidrDPUServiceIPAM            *dpuservicev1.DPUServiceIPAM
	ipPoolDPUServiceIPAM          *dpuservicev1.DPUServiceIPAM
	dpuServiceCredentialRequest   *dpuservicev1.DPUServiceCredentialRequest
	numberOfDPUNodes              int
	useExternalNodeReboot         bool
	pullSecretNames               []string
	client                        client.Client
	skipCleanup                   bool
	bfbImageURL                   string
	restConfig                    *rest.Config
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

func (t *systemTestInput) applyOVNKHBNConfig(conf config) {
	// placeholder for DPUDeployment and DPUSet required for OVNK HBN
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

	dpuCluster := &provisioningv1.DPUCluster{}
	dpuClusterUnstructured := unstructuredFromFile(conf.DPUClusterPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuClusterUnstructured.Object, dpuCluster)).To(Succeed())
	t.dpuCluster = dpuCluster

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
	t.additionalProvisioningObjects = dpuClusterPrerequisiteObjects

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
	t.useExternalNodeReboot = conf.UseExternalNodeReboot
}

func (t *systemTestInput) hasDpuNodes() bool {
	return t.numberOfDPUNodes > 0
}

type DeployDPFSystemComponentsInput struct {
	operatorConfig            *operatorv1.DPFOperatorConfig
	systemNamespace           string
	ProvisioningControllerPVC *corev1.PersistentVolumeClaim
	ImagePullSecrets          []string
	client                    client.Client
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
		pvc.SetName(input.operatorConfig.Spec.ProvisioningController.BFBPersistentVolumeClaimName)
		pvc.SetNamespace(input.systemNamespace)
		pvc.SetLabels(afterAllCleanupLabels)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, pvc))).NotTo(HaveOccurred())
	}

	By("creates the imagePullSecrets for the DPFOperatorConfig")
	for _, secretName := range input.ImagePullSecrets {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: input.systemNamespace,
				Labels:    afterAllCleanupLabels,
			},
		}
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, secret))).ToNot(HaveOccurred())
	}

	By("create the DPFOperatorConfig for the system")
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, input.operatorConfig))).NotTo(HaveOccurred())

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
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("ensure the system DPUServices are created")
	Eventually(func(g Gomega) {
		// TODO: Remove as soon as we have version aware upgrade logic for the pre-upgrade validation
		gotDPFOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(input.operatorConfig), gotDPFOperatorConfig)).NotTo(HaveOccurred())
		g.Expect(gotDPFOperatorConfig.Status.Version).NotTo(BeNil())
		isUpgradeFrom25Dot7 := operatorutils.IsUpgradeFrom25Dot7(*gotDPFOperatorConfig.Status.Version)

		dpuServices := &dpuservicev1.DPUServiceList{}
		g.Expect(testClient.List(ctx, dpuServices)).To(Succeed())
		if isUpgradeFrom25Dot7 {
			g.Expect(dpuServices.Items).To(HaveLen(8))
		} else {
			g.Expect(dpuServices.Items).To(HaveLen(9))
		}
		found := map[string]bool{}
		for i := range dpuServices.Items {
			found[dpuServices.Items[i].Name] = true
		}

		// Expect each of the following to have been created by the operator.
		g.Expect(found).To(HaveKey(operatorv1.MultusName.String()))
		g.Expect(found).To(HaveKey(operatorv1.SRIOVDevicePluginName.String()))
		g.Expect(found).To(HaveKey(operatorv1.ServiceSetControllerName.String()))
		g.Expect(found).To(HaveKey(operatorv1.FlannelName.String()))
		g.Expect(found).To(HaveKey(operatorv1.NVIPAMName.String()))
		g.Expect(found).To(HaveKey(operatorv1.OVSCNIName.String()))
		g.Expect(found).To(HaveKey(operatorv1.SFCControllerName.String()))
		if !isUpgradeFrom25Dot7 {
			g.Expect(found).To(HaveKey(operatorv1.CNIInstallerName.String()))
		}
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// ProvisionDPUCluster provisions a DPUCluster and creates a BFB.
func ProvisionDPUCluster(ctx context.Context, input ProvisionDPUClustersInput) {
	// TODO: Pass this in as config instead of as a global.
	if input.bfbImageURL != "" {
		By("override BFB URL with the value from the env BFB_IMAGE_URL")
		input.bfb.Spec.URL = input.bfbImageURL
	}

	By("create prerequisites objects for DPUClusters")
	for _, obj := range input.dpuClusterPrerequisites {
		obj.SetLabels(afterAllCleanupLabels)
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

	By("create DPUCluster")
	input.dpuCluster.SetLabels(afterAllCleanupLabels)
	By(fmt.Sprintf("Creating DPU Cluster %s/%s", input.dpuCluster.GetNamespace(), input.dpuCluster.GetName()))
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, input.dpuCluster))).NotTo(HaveOccurred())

	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(1))
		for _, cluster := range clusters.Items {
			g.Expect(cluster.Status.Phase).Should(Equal(provisioningv1.PhaseReady))
		}
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("create the BFB and DPUSet")
	Eventually(func(g Gomega) {
		By("creating the BFB")
		bfb := input.bfb.DeepCopy()
		bfb.SetLabels(afterAllCleanupLabels)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, bfb))).NotTo(HaveOccurred())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	Eventually(func(g Gomega) {
		// Return if no DPUFlavor is provided.
		if input.dpuFlavor == nil {
			return
		}
		By("Creating the DPUFlavor")
		dpuFlavor := input.dpuFlavor.DeepCopy()
		dpuFlavor.SetLabels(afterAllCleanupLabels)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuFlavor))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// ProvisionDPUSet DPUSet that will provision DPUs in the background if the environment has such DPUs.
// It doesn't check whether the DPUs become ready intentionally to allow for subsequent tests to be executed in the meantime.
func ProvisionDPUSet(ctx context.Context, input ProvisionDPUClustersInput) {
	Eventually(func(g Gomega) {
		By("Creating the DPUSet")
		dpuset := input.dpuSet.DeepCopy()
		// TODO: Test the cleanup of the node related to the DPU.
		dpuset.SetLabels(afterAllCleanupLabels)
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuset))).NotTo(HaveOccurred())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("creating a client for the DPUCluster")
	getDPUClusterClient(ctx, input)

	By("Checking the DPUServices have been mirrored to the target cluster")
	Eventually(func(g Gomega) {
		serviceSetDeployment := &appsv1.Deployment{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      "servicechainset-controller-manager"},
			serviceSetDeployment)).To(Succeed())
		g.Expect(serviceSetDeployment.Status.ReadyReplicas).To(Equal(*serviceSetDeployment.Spec.Replicas))
	}).WithTimeout(600 * time.Second).Should(Succeed())

	By("Checking that DPUService objects have been mirrored to the DPUClusters")
	Eventually(func(g Gomega) {
		deployments := &appsv1.DeploymentList{}
		g.Expect(dpuClusterClient.List(ctx, deployments, client.HasLabels{argoCDInstanceLabel})).To(Succeed())
		found := map[string]bool{}
		for i := range deployments.Items {
			g.Expect(deployments.Items[i].GetLabels()).To(HaveKey(argoCDInstanceLabel))
			g.Expect(deployments.Items[i].GetLabels()[argoCDInstanceLabel]).NotTo(Equal(""))
			found[deployments.Items[i].GetLabels()[argoCDInstanceLabel]] = true
		}
		daemonsets := appsv1.DaemonSetList{}
		g.Expect(dpuClusterClient.List(ctx, &daemonsets, client.HasLabels{argoCDInstanceLabel}, client.InNamespace(input.dpuCluster.GetNamespace()))).To(Succeed())
		for i := range daemonsets.Items {
			g.Expect(daemonsets.Items[i].GetLabels()).To(HaveKey(argoCDInstanceLabel))
			g.Expect(daemonsets.Items[i].GetLabels()[argoCDInstanceLabel]).NotTo(Equal(""))
			found[daemonsets.Items[i].GetLabels()[argoCDInstanceLabel]] = true
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
func VerifyDPUClusterWithNodes(ctx context.Context, input ProvisionDPUClustersInput) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient.List(ctx, nodes)).ToNot(HaveOccurred())
		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), input.numberOfNodesPerCluster)
		tracker.By(nodeKey, "Checking that the number of nodes %d is equal to %d", len(nodes.Items), input.numberOfNodesPerCluster)
		g.Expect(nodes.Items).To(HaveLen(input.numberOfNodesPerCluster))
	}).WithTimeout(45 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).ToNot(HaveOccurred())
		g.Expect(dpus.Items).To(HaveLen(input.numberOfNodesPerCluster))
		for _, dpu := range dpus.Items {
			g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady))
		}
	}).WithTimeout(5 * time.Minute).Should(Succeed())
}

func VerifyDPUClusterPods(ctx context.Context, podSubstrToVerify []string) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(dpuClusterClient.List(ctx, pods)).To(Succeed())

		// Create a map to track which pods from podsToVerify we've found
		foundPods := make(map[string]bool)
		for _, podSubstr := range podSubstrToVerify {
			foundPods[podSubstr] = false
		}

		for _, pod := range pods.Items {
			// Check if this pod matches any of the expected prefixes.
			// If it does, we add it to the foundPods map.
			var matchedSubstr string
			for _, prefix := range podSubstrToVerify {
				if !strings.Contains(pod.Name, prefix) {
					continue
				}
				matchedSubstr = prefix
			}
			if matchedSubstr == "" {
				continue
			}

			foundPods[matchedSubstr] = true
			podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			tracker.By(podKey, "Verifying pod %s", podKey)
			g.Expect(pod.Status.ContainerStatuses).ToNot(BeEmpty())

			for _, containerStatus := range pod.Status.ContainerStatuses {
				containerKey := fmt.Sprintf("%s/%s", podKey, containerStatus.Name)
				tracker.By(containerKey, "Verifying container %s with image %s", containerKey, containerStatus.Image)
				g.Expect(containerStatus.ImageID).ToNot(BeEmpty())
			}
		}

		// Verify all expected pods were found
		for podPrefix, found := range foundPods {
			tracker.By(podPrefix, "Verifying pod %s was scheduled", podPrefix)
			g.Expect(found).To(BeTrue())
		}
	}).WithTimeout(20 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

// verifyDPUServicesReady checks that the DPUService is ready.
func verifyDPUServicesReady(ctx context.Context, input *systemTestInput, dpuServiceNamespace string, dpuServiceName []string) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		for _, name := range dpuServiceName {
			tracker.By(name, "verify DPUService %s is ready", name)
			// Check the DPUService is ready.
			dpuService := &dpuservicev1.DPUService{}
			g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: name}, dpuService)).To(Succeed())
			g.Expect(conditions.IsTrue(dpuService, conditions.TypeReady)).To(BeTrue())
		}
		// A timeout of 20 minutes is necessary here. We have alot of trouble pulling our images for all
		// DPUServices on the DPUCluster, so we need to wait for the images to be pulled and the pods to be ready.
	}).WithTimeout(20 * time.Minute).Should(Succeed())
}

// getDPUClusterClient retrieves the DPUCluster client for the given input.
func getDPUClusterClient(ctx context.Context, input ProvisionDPUClustersInput) {
	Eventually(func(g Gomega) {
		// Use the new tunnel helper to create a client for the Kamaji cluster
		dpuClusterClient = tunnel.NewTunneledClient(ctx, input.client, input.restConfig, input.dpuCluster)
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

func unstructuredFromFile(path string) *unstructured.Unstructured {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	obj.SetLabels(afterAllCleanupLabels)
	return obj
}

type collectResourcesInput struct {
	collectResources bool
	testClient       client.Client
	clientset        *kubernetes.Clientset
	restConfig       *rest.Config
}

func collectResourcesAndLogs(ctx context.Context, input collectResourcesInput, testName string) error {
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
	artifactsPath := filepath.Join(filepath.Dir(basePath), fmt.Sprintf("../../artifacts/%s", testName))
	inventoryManifestsPath := filepath.Join(filepath.Dir(basePath), "../../internal/operator/inventory/manifests")

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
