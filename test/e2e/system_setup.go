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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl"
	"github.com/nvidia/doca-platform/internal/dpucluster"
	"github.com/nvidia/doca-platform/test/utils/collector"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProvisionDPUClustersInput struct {
	numberOfNodesPerCluster int
	dpuClusterPrerequisites []client.Object
	nodeAnnotations         map[string]string
	dpuClusterClientOptions []dpucluster.ClientOption
	dpuCluster              *provisioningv1.DPUCluster
	dpuFlavor               *provisioningv1.DPUFlavor
	bfb                     *provisioningv1.BFB
	dpuSet                  *provisioningv1.DPUSet
	client                  client.Client
	bfbImageURL             string
}

// DPFSystemTest provisions a cluster with a DPF system and runs the inputted tests on it.
// This function is designed to be run inside a ginkgo `Describe` test spec.
func DPFSystemTest(ctx context.Context, input systemTestInput, tests []dpfTest) {
	DeployDPFSystemComponents(ctx, DeployDPFSystemComponentsInput{
		systemNamespace:           input.namespace,
		operatorConfig:            input.config,
		ImagePullSecrets:          input.pullSecretNames,
		ProvisioningControllerPVC: input.pvc,
		client:                    input.client,
	})

	ProvisionDPUClusters(ctx, ProvisionDPUClustersInput{
		numberOfNodesPerCluster: input.numberOfDPUNodes,
		dpuClusterPrerequisites: input.additionalProvisioningObjects,
		// This annotation prevents nodes from being restarted during the e2e provisioning test flow which speeds up the test.
		nodeAnnotations: map[string]string{"provisioning.dpu.nvidia.com/reboot-command": "skip"},
		dpuCluster:      input.dpuCluster,
		dpuSet:          input.dpuSet,
		bfb:             input.bfb,
		dpuFlavor:       input.dpuFlavor,
		// This server override enables running the e2e tests using Docker Desktop on MacOS. The port must match the port contained
		// in the nodeport defined in the nodePortService in the dpuClusterPrerequisites.
		dpuClusterClientOptions: []dpucluster.ClientOption{dpucluster.OverrideClientConfigHost{Server: "https://127.0.0.1:32443"}},
		client:                  input.client,
		bfbImageURL:             input.bfbImageURL,
	})

	// Run each additional test passed to the spec
	// TODO: Consider using a map here to ensure the tests are independent.
	for _, test := range tests {
		test(ctx, input)
	}
}

type dpfTest func(context.Context, systemTestInput)

type systemTestInput struct {
	namespace                     string
	config                        *operatorv1.DPFOperatorConfig
	pvc                           *corev1.PersistentVolumeClaim
	additionalProvisioningObjects []client.Object
	dpuCluster                    *provisioningv1.DPUCluster
	dpuFlavor                     *provisioningv1.DPUFlavor
	dpuService                    *dpuservicev1.DPUService
	dpuServiceInterface           *dpuservicev1.DPUServiceInterface
	dpuServiceChain               *dpuservicev1.DPUServiceChain
	bfb                           *provisioningv1.BFB
	dpuSet                        *provisioningv1.DPUSet
	dpuDeployment                 *dpuservicev1.DPUDeployment
	dpuServiceConfiguration       *dpuservicev1.DPUServiceConfiguration
	dpuServiceTemplate            *dpuservicev1.DPUServiceTemplate
	cidrDPUServiceIPAM            *dpuservicev1.DPUServiceIPAM
	ipPoolDPUServiceIPAM          *dpuservicev1.DPUServiceIPAM
	dpuServiceCredentialRequest   *dpuservicev1.DPUServiceCredentialRequest
	numberOfDPUNodes              int
	pullSecretNames               []string
	client                        client.Client
	skipCleanup                   bool
	bfbImageURL                   string
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
	pvcUnstructured := unstructuredFromFile(conf.ProvisioningControllerPVCPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(pvcUnstructured.Object, pvc)).To(Succeed())
	t.pvc = pvc

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
	It("ensure the DPF Operator is running and ready", func() {
		Eventually(func(g Gomega) {
			deployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: input.systemNamespace,
				Name:      "dpf-operator-controller-manager"},
				deployment)).To(Succeed())
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
		}).WithTimeout(120 * time.Second).Should(Succeed())
	})

	It("create the PersistentVolumeClaim for the DPF Provisioning controller", func() {
		pvc := input.ProvisioningControllerPVC.DeepCopy()
		pvc.SetName(input.operatorConfig.Spec.ProvisioningController.BFBPersistentVolumeClaimName)
		pvc.SetNamespace(input.systemNamespace)
		pvc.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, pvc)).To(Succeed())
	})

	It("creates the imagePullSecrets for the DPFOperatorConfig", func() {
		for _, secretName := range input.ImagePullSecrets {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: input.systemNamespace,
					Labels:    cleanupLabels,
				},
			}
			Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, secret))).ToNot(HaveOccurred())
		}
	})

	It("create the DPFOperatorConfig for the system", func() {
		Expect(testClient.Create(ctx, input.operatorConfig)).To(Succeed())
	})

	It("ensure the DPF controllers are running and ready", func() {
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
	})

	It("ensure the system DPUServices are created", func() {
		Eventually(func(g Gomega) {
			dpuServices := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx, dpuServices)).To(Succeed())
			g.Expect(dpuServices.Items).To(HaveLen(9))
			found := map[string]bool{}
			for i := range dpuServices.Items {
				found[dpuServices.Items[i].Name] = true
			}

			// Expect each of the following to have been created by the operator.
			g.Expect(found).To(HaveKey(operatorv1.MultusName))
			g.Expect(found).To(HaveKey(operatorv1.SRIOVDevicePluginName))
			g.Expect(found).To(HaveKey(operatorv1.ServiceSetControllerName))
			g.Expect(found).To(HaveKey(operatorv1.FlannelName))
			g.Expect(found).To(HaveKey(operatorv1.NVIPAMName))
			g.Expect(found).To(HaveKey(operatorv1.OVSCNIName))
			g.Expect(found).To(HaveKey(operatorv1.SFCControllerName))
			g.Expect(found).To(HaveKey(operatorv1.OVSHelperName))
		}).WithTimeout(60 * time.Second).Should(Succeed())
	})
}

func ProvisionDPUClusters(ctx context.Context, input ProvisionDPUClustersInput) {
	// TODO: Pass this in as config instead of as a global.
	if input.bfbImageURL != "" {
		It("override BFB URL with the value from the env BFB_IMAGE_URL", func() {
			input.bfb.Spec.URL = input.bfbImageURL
		})
	}
	// Add additional annotations to the Nodes.
	testClient := input.client
	if len(input.nodeAnnotations) > 0 {
		It("annotate nodes from cluster", func() {
			Eventually(func(g Gomega) {
				nodes := &corev1.NodeList{}
				g.Expect(testClient.List(ctx, nodes)).To(Succeed())
				for _, node := range nodes.Items {
					original := node.DeepCopy()
					annotations := node.GetAnnotations()
					if annotations == nil {
						annotations = map[string]string{}
					}
					for k, v := range input.nodeAnnotations {
						annotations[k] = v
					}
					node.SetAnnotations(annotations)
					g.Expect(testClient.Patch(ctx, &node, client.MergeFrom(original))).To(Succeed())
				}
			}).Should(Succeed())
		})
	}

	It("create prerequisites objects for DPUClusters", func() {
		for _, obj := range input.dpuClusterPrerequisites {
			obj.SetLabels(cleanupLabels)
			By(fmt.Sprintf("Creating prerequisite object %s %s/%s", obj.GetObjectKind().GroupVersionKind().String(), obj.GetNamespace(), obj.GetName()))
			Expect(testClient.Create(ctx, obj)).To(Succeed())
		}
	})

	It("create DPUCluster", func() {
		input.dpuCluster.SetLabels(cleanupLabels)
		By(fmt.Sprintf("Creating DPU Cluster %s/%s", input.dpuCluster.GetNamespace(), input.dpuCluster.GetName()))
		Expect(testClient.Create(ctx, input.dpuCluster)).To(Succeed())

		Eventually(func(g Gomega) {
			clusters := &provisioningv1.DPUClusterList{}
			g.Expect(testClient.List(ctx, clusters)).To(Succeed())
			g.Expect(clusters.Items).To(HaveLen(1))
			for _, cluster := range clusters.Items {
				g.Expect(cluster.Status.Phase).Should(Equal(provisioningv1.PhaseReady))
			}
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})

	It("create the BFB and DPUSet", func() {
		Eventually(func(g Gomega) {
			By("creating the BFB")
			bfb := input.bfb.DeepCopy()
			bfb.SetLabels(cleanupLabels)
			g.Expect(testClient.Create(ctx, bfb)).To(Succeed())
		}).WithTimeout(10 * time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			// Return if no DPUFlavor is provided.
			if input.dpuFlavor == nil {
				return
			}
			By("Creating the DPUFlavor")
			dpuFlavor := input.dpuFlavor.DeepCopy()
			dpuFlavor.SetLabels(cleanupLabels)
			g.Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
		}).WithTimeout(60 * time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			By("Creating the DPUSet")
			dpuset := input.dpuSet.DeepCopy()
			// TODO: Test the cleanup of the node related to the DPU.
			dpuset.SetLabels(cleanupLabels)
			g.Expect(testClient.Create(ctx, dpuset)).To(Succeed())
		}).WithTimeout(60 * time.Second).Should(Succeed())

		By("creating a client for the DPUCluster")
		Eventually(func(g Gomega) {
			var err error
			dpuClusters, err := dpucluster.GetConfigs(ctx, testClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(dpuClusters).To(HaveLen(1))
			dpuClusterClient, err = dpuClusters[0].Client(ctx, input.dpuClusterClientOptions...)
			g.Expect(err).ToNot(HaveOccurred())
		}).WithTimeout(10 * time.Second).Should(Succeed())

		By(fmt.Sprintf("checking that the number of nodes is equal to %d", input.numberOfNodesPerCluster))
		Eventually(func(g Gomega) {
			nodes := &corev1.NodeList{}
			g.Expect(dpuClusterClient.List(ctx, nodes)).ToNot(HaveOccurred())
			g.Expect(nodes.Items).To(HaveLen(input.numberOfNodesPerCluster))
		}).WithTimeout(30 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
	})

	It("ensure the system DPUServices are created correctly", func() {
		By("Checking the DPUServices have been mirrored to the target cluster")
		Eventually(func(g Gomega) {
			serviceSetDeployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
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
			g.Expect(dpuClusterClient.List(ctx, &daemonsets, client.HasLabels{argoCDInstanceLabel})).To(Succeed())
			for i := range daemonsets.Items {
				g.Expect(daemonsets.Items[i].GetLabels()).To(HaveKey(argoCDInstanceLabel))
				g.Expect(daemonsets.Items[i].GetLabels()[argoCDInstanceLabel]).NotTo(Equal(""))
				found[daemonsets.Items[i].GetLabels()[argoCDInstanceLabel]] = true
			}

			// Expect each of the following to have been created by the operator.
			// These are labels on the appv1 type - e.g. DaemonSet or Deployment on the DPU cluster.
			g.Expect(found).To(HaveLen(7))
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.MultusName)))
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.FlannelName)))
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SRIOVDevicePluginName)))
			// Note: The NVIPAM DPUService contains both a Daemonset and a Deployment - but this is overwritten in the map.
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.NVIPAMName)))
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSCNIName)))
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SFCControllerName)))
			g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSHelperName)))
		}).WithTimeout(600 * time.Second).Should(Succeed())
	})
}

func unstructuredFromFile(path string) *unstructured.Unstructured {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	obj.SetLabels(cleanupLabels)
	return obj
}

type collectResourcesInput struct {
	collectResources bool
	testClient       client.Client
	clientset        *kubernetes.Clientset
}

func collectResourcesAndLogs(ctx context.Context, input collectResourcesInput) error {
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
		if err := dpfctl.PrintObjectTree(t); err != nil {
			return err
		}
	}

	// Get the path to place artifacts in
	_, basePath, _, _ := runtime.Caller(0)
	artifactsPath := filepath.Join(filepath.Dir(basePath), "../../artifacts")
	inventoryManifestsPath := filepath.Join(filepath.Dir(basePath), "../../internal/operator/inventory/manifests")

	// Create a resourceCollector to dump logs and resources for test debugging.
	clusters, err := collector.GetClusterCollectors(ctx, input.testClient, artifactsPath, inventoryManifestsPath, input.clientset)
	Expect(err).NotTo(HaveOccurred())
	return collector.New(clusters).Run(ctx)
}
