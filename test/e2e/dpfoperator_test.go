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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"
	kamajiv1 "github.com/nvidia/doca-platform/internal/kamaji/api/v1alpha1"
	nvipamv1 "github.com/nvidia/doca-platform/internal/nvipam/api/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/collector"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// pullSecretName must match the name given to the secret in `create-artefact-secrets.sh`
	pullSecretName = "dpf-pull-secret"
)

var (
	testObjectsPath            = "../objects/"
	numClusters                = 1
	dpfOperatorSystemNamespace = "dpf-operator-system"
	argoCDInstanceLabel        = "argocd.argoproj.io/instance"

	// Labels and resources targeted for cleanup before running our e2e tests.
	// This cleanup is typically handled by cleanupObjs, but if an e2e test fails, the standard cleanup may not be executed.
	cleanupLabels     = map[string]string{"dpf-operator-e2e-test-cleanup": "true"}
	labelSelector     = labels.SelectorFromSet(cleanupLabels)
	resourcesToDelete = []client.ObjectList{
		&dpuservicev1.DPUDeploymentList{},
		&dpuservicev1.DPUServiceCredentialRequestList{},
		&dpuservicev1.DPUServiceList{},
		&dpuservicev1.DPUServiceConfigurationList{},
		&dpuservicev1.DPUServiceTemplateList{},
		&provisioningv1.DPUSetList{},
		&provisioningv1.DPUList{},
		&provisioningv1.BFBList{},
		&provisioningv1.DPUClusterList{},
		&dpuservicev1.DPUServiceIPAMList{},
		&dpuservicev1.DPUServiceChainList{},
		&dpuservicev1.DPUServiceInterfaceList{},
		&kamajiv1.TenantControlPlaneList{},
		&operatorv1.DPFOperatorConfigList{},
		&appsv1.DeploymentList{},
		&appsv1.DaemonSetList{},
		&corev1.PersistentVolumeClaimList{},
		&corev1.NamespaceList{},
	}
)

//nolint:dupl
var _ = Describe("DOCA Platform Framework", Ordered, func() {
	// TODO: Consolidate all the DPUService* objects in one namespace to illustrate user behavior
	dpfProvisioningControllerPVCName := "bfb-pvc"
	extraPullSecretName := fmt.Sprintf("%s-extra", pullSecretName)

	// The DPFOperatorConfig for the test.
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpfoperatorconfig",
			Namespace: dpfOperatorSystemNamespace,
			Labels:    cleanupLabels,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: dpfProvisioningControllerPVCName,
			},
			StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
				Disable: ptr.To(false),
			},
			// Disable the Kamaji cluster manager so only one cluster manager is running.
			// TODO: Enable Kamaji by default in the e2e tests.
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				Disable: ptr.To(true),
			},
			ImagePullSecrets: []string{
				pullSecretName,
				extraPullSecretName,
			},
		},
	}

	Context("DPF Operator initialization", func() {
		BeforeAll(func() {
			By("cleaning up objects created during recent tests", func() {
				Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labelSelector, resourcesToDelete...)).To(Succeed())
			})
		})

		AfterAll(func() {
			By("collecting resources and logs for the clusters")
			err := collectResourcesAndLogs(ctx)
			if err != nil {
				// Don't fail the test if the log collector fails - just print the errors.
				GinkgoLogr.Error(err, "failed to collect resources and logs for the clusters")
			}
			if skipCleanup {
				return
			}
			By("cleaning up objects created during the test", func() {
				Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labelSelector, resourcesToDelete...)).To(Succeed())
			})
		})

		pvcSize := "10Mi"
		pvcStorageClass := ""
		if numNodes > 0 {
			pvcSize = "10Gi"
			pvcStorageClass = "local-path"
		}

		DeployDPFSystemComponents(ctx, DeployDPFSystemComponentsInput{
			DPFSystemNamespace:                    dpfOperatorSystemNamespace,
			DPFOperatorConfig:                     config,
			ImagePullSecrets:                      []string{pullSecretName, extraPullSecretName},
			ProvisioningControllerPVCName:         dpfProvisioningControllerPVCName,
			ProvisioningControllerPVCSize:         pvcSize,
			ProvisioningControllerPVCStorageClass: pvcStorageClass,
		})

		baseClusterName := "dpu-cluster-%d"
		dpuClusterPrerequisiteObjects := []client.Object{}
		for i := 0; i < numClusters; i++ {
			// Read the TenantControlPlane from file and create it.
			// Note: This process doesn't cover all the places in the file the name should be set.
			tenantControlPlane := unstructuredFromFile("infrastructure/kamaji.yaml")
			tenantControlPlane.SetName(fmt.Sprintf(baseClusterName, i+1))
			tenantControlPlane.SetNamespace(dpfOperatorSystemNamespace)
			tenantControlPlane.SetLabels(cleanupLabels)

			// If we have real nodes use the ClusterIP service type.
			if numNodes > 0 {
				Expect(unstructured.SetNestedField(tenantControlPlane.UnstructuredContent(), "ClusterIP", "spec", "controlPlane", "service", "serviceType")).To(Succeed())
			}
			dpuClusterPrerequisiteObjects = append(dpuClusterPrerequisiteObjects, tenantControlPlane)
		}

		ProvisionDPUClusters(ctx, ProvisionDPUClustersInput{
			numberOfNodesPerCluster: numNodes,
			numberOfClusters:        1,
			baseClusterName:         "dpu-cluster-%d",
			clusterNamespace:        dpfOperatorSystemNamespace,
			dpuClusterPrerequisites: dpuClusterPrerequisiteObjects,
			nodeAnnotations: map[string]string{
				// This annotation prevents nodes from being restarted during the e2e provisioning test flow which speeds up the test.
				"provisioning.dpu.nvidia.com/reboot-command": "skip",
			},
			baseDPUClusterFile: "infrastructure/dpucluster.yaml",
			baseDPUSetFile:     "infrastructure/dpuset.yaml",
			baseBFBFile:        "infrastructure/bfb.yaml",
		})

		VerifyDPFOperatorConfiguration(ctx, config)
		ValidateDPUService(ctx, config)
		ValidateDPUDeployment(ctx)
		ValidateDPUServiceIPAM(ctx)
		ValidateDPUServiceChain(ctx)
		ValidateDPUServiceCredentialRequest(ctx)

		It("delete DPUs, DPUSets and BFBs and ensure they are deleted", func() {
			if skipCleanup {
				Skip("Skip cleanup resources")
			}

			Eventually(func(g Gomega) {
				dpuSetList := &provisioningv1.DPUSetList{}
				dpuList := &provisioningv1.DPUList{}
				g.Expect(client.IgnoreNotFound(testClient.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())
				g.Expect(testClient.List(ctx, dpuSetList)).To(Succeed())
				g.Expect(dpuSetList.Items).To(BeEmpty())

				// Expect all DPUs to have been deleted.
				g.Expect(testClient.List(ctx, dpuList)).To(Succeed())
				g.Expect(dpuList.Items).To(BeEmpty())

				dpuClusters, err := dpucluster.GetConfigs(ctx, testClient)
				Expect(err).ToNot(HaveOccurred())
				for _, conf := range dpuClusters {
					nodes := &corev1.NodeList{}
					dpuClient, err := conf.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(dpuClient.List(ctx, nodes)).To(Succeed())
					By(fmt.Sprintf("Expected number of nodes %d to equal %d", len(nodes.Items), 0))
					g.Expect(nodes.Items).To(BeEmpty())
				}
			}).WithTimeout(10 * time.Minute).Should(Succeed())

			Eventually(func(g Gomega) {
				bfb := unstructuredFromFile("infrastructure/bfb.yaml")

				g.Expect(client.IgnoreNotFound(testClient.Delete(ctx, bfb))).To(Succeed())
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(bfb), bfb))).To(BeTrue())
			}).WithTimeout(60 * time.Second).Should(Succeed())
		})

		It("delete the DPUClusters and ensure they are deleted", func() {
			if skipCleanup {
				Skip("Skip cleanup resources")
			}
			Eventually(func(g Gomega) {
				dpuClusters, err := dpucluster.GetConfigs(ctx, testClient)
				Expect(err).ToNot(HaveOccurred())
				for _, dpuCluster := range dpuClusters {
					g.Expect(testClient.Delete(ctx, dpuCluster.Cluster)).To(Succeed())
				}
			}).WithTimeout(10 * time.Minute).Should(Succeed())
		})

		It("delete the DPFOperatorConfig and ensure it is deleted", func() {
			if skipCleanup {
				Skip("Skip cleanup resources")
			}
			// Check that all deployments and DPUServices are deleted.
			Eventually(func(g Gomega) {
				g.Expect(client.IgnoreNotFound(testClient.Delete(ctx, config))).To(Succeed())
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(config), config))).To(BeTrue())
			}).WithTimeout(600 * time.Second).Should(Succeed())
		})
	})
})

type DeployDPFSystemComponentsInput struct {
	DPFOperatorConfig                     *operatorv1.DPFOperatorConfig
	DPFSystemNamespace                    string
	ProvisioningControllerPVCName         string
	ProvisioningControllerPVCSize         string
	ProvisioningControllerPVCStorageClass string
	ImagePullSecrets                      []string
}

// DeployDPFSystemComponents creates the DPFOperatorConfig and some dependencies and checks that the system components
// are deployed from the operator.
// 1) Ensures the DPF Operator is running and ready
// 2) Creates a PersistentVolumeClaim for the Provisioning controller
// 3) Creates ImagePullSecrets which are tested as part of the e2e flow (note these are fake and could possibly be replaced by real ones)
// 4) Creates the DPFOperatorConfig for the test
// 5) Ensures the DPF System components - including DPUServices - have been deployed.
func DeployDPFSystemComponents(ctx context.Context, input DeployDPFSystemComponentsInput) {
	It("ensure the DPF Operator is running and ready", func() {
		Eventually(func(g Gomega) {
			deployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: input.DPFSystemNamespace,
				Name:      "dpf-operator-controller-manager"},
				deployment)).To(Succeed())
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
		}).WithTimeout(60 * time.Second).Should(Succeed())
	})

	It("create the PersistentVolumeClaim for the DPF Provisioning controller", func() {
		pvcUnstructured := unstructuredFromFile("infrastructure/dpf-provisioning-pvc.yaml")
		pvc := &corev1.PersistentVolumeClaim{}
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(pvcUnstructured.Object, pvc)).To(Succeed())
		pvc.SetName(input.ProvisioningControllerPVCName)
		pvc.SetNamespace(input.DPFSystemNamespace)
		pvc.SetLabels(cleanupLabels)
		// If there are real nodes we need to allocate storage for the volume.
		pvc.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
		pvc.Spec.Resources.Requests = corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse(input.ProvisioningControllerPVCSize),
		}
		if input.ProvisioningControllerPVCStorageClass != "" {
			pvc.Spec.StorageClassName = ptr.To(input.ProvisioningControllerPVCStorageClass)
		}
		Expect(testClient.Create(ctx, pvc)).To(Succeed())
	})

	It("creates the imagePullSecrets for the DPF OperatorConfig", func() {
		for _, secretName := range input.ImagePullSecrets {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: input.DPFSystemNamespace,
					Labels:    cleanupLabels,
				},
			}
			Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, secret))).ToNot(HaveOccurred())
		}
	})

	It("create the DPFOperatorConfig for the system", func() {
		Expect(testClient.Create(ctx, input.DPFOperatorConfig)).To(Succeed())
	})

	It("ensure the DPF controllers are running and ready", func() {
		Eventually(func(g Gomega) {
			// Check the DPUService controller manager is up and ready.
			dpuServiceDeployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: dpfOperatorSystemNamespace,
				Name:      "dpuservice-controller-manager"},
				dpuServiceDeployment)).To(Succeed())
			g.Expect(dpuServiceDeployment.Status.ReadyReplicas).To(Equal(*dpuServiceDeployment.Spec.Replicas))

			// Check the DPF provisioning controller manager is up and ready.
			dpfProvisioningDeployment := &appsv1.Deployment{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: dpfOperatorSystemNamespace,
				Name:      "dpf-provisioning-controller-manager"},
				dpfProvisioningDeployment)).To(Succeed())
			g.Expect(dpfProvisioningDeployment.Status.ReadyReplicas).To(Equal(*dpfProvisioningDeployment.Spec.Replicas))
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})

	It("ensure the system DPUServices are created", func() {
		Eventually(func(g Gomega) {
			dpuServices := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx, dpuServices)).To(Succeed())
			g.Expect(dpuServices.Items).To(HaveLen(8))
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

type ProvisionDPUClustersInput struct {
	numberOfNodesPerCluster int
	numberOfClusters        int
	baseClusterName         string
	clusterNamespace        string
	dpuClusterPrerequisites []client.Object
	nodeAnnotations         map[string]string
	baseDPUClusterFile      string
	baseDPUSetFile          string
	baseBFBFile             string
}

func ProvisionDPUClusters(ctx context.Context, input ProvisionDPUClustersInput) {
	baseDPUCluster := &provisioningv1.DPUCluster{}
	baseBFB := &provisioningv1.BFB{}
	baseDPUSet := &provisioningv1.DPUSet{}

	It("read provisioning objects from files", func() {
		dpuClusterUnstructured := unstructuredFromFile(input.baseDPUClusterFile)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpuClusterUnstructured.Object, baseDPUCluster)).To(Succeed())

		bfbUnstructured := unstructuredFromFile(input.baseBFBFile)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(bfbUnstructured.Object, baseBFB)).To(Succeed())

		dpusetUnstructured := unstructuredFromFile(input.baseDPUSetFile)
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dpusetUnstructured.Object, baseDPUSet)).To(Succeed())
	})

	// Add additional annotations to the Nodes.
	if len(input.nodeAnnotations) > 0 {
		It("annotate nodes from cluster", func() {
			Eventually(func(g Gomega) {
				nodes := &corev1.NodeList{}
				g.Expect(testClient.List(ctx, nodes)).To(Succeed())
				for _, node := range nodes.Items {
					original := node.DeepCopy()
					annotations := node.GetAnnotations()
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

	It("create DPUClusters ", func() {
		for i := 0; i < input.numberOfClusters; i++ {
			dpuCluster := baseDPUCluster.DeepCopy()
			dpuCluster.SetName(fmt.Sprintf(input.baseClusterName, i+1))
			dpuCluster.SetNamespace(input.clusterNamespace)
			dpuCluster.SetLabels(cleanupLabels)

			By(fmt.Sprintf("Creating DPU Cluster %s/%s", dpuCluster.GetNamespace(), dpuCluster.GetName()))
			Expect(testClient.Create(ctx, dpuCluster)).To(Succeed())
		}
		Eventually(func(g Gomega) {
			clusters := &provisioningv1.DPUClusterList{}
			g.Expect(testClient.List(ctx, clusters)).To(Succeed())
			g.Expect(clusters.Items).To(HaveLen(numClusters))
			for _, cluster := range clusters.Items {
				g.Expect(cluster.Status.Phase).Should(Equal(provisioningv1.PhaseReady))
			}
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})

	It("create the BFB and DPUSet", func() {
		Eventually(func(g Gomega) {
			By("creating the BFB")
			bfb := baseBFB.DeepCopy()
			bfb.SetLabels(cleanupLabels)
			g.Expect(testClient.Create(ctx, bfb)).To(Succeed())
		}).WithTimeout(10 * time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			for i := 0; i < numClusters; i++ {
				By(fmt.Sprintf("Creating the DPUSet for cluster %s/%s", fmt.Sprintf(input.baseClusterName, i+1), input.clusterNamespace))
				dpuset := baseDPUSet.DeepCopy()
				// TODO: Test the cleanup of the node related to the DPU.
				dpuset.SetLabels(cleanupLabels)
				g.Expect(testClient.Create(ctx, dpuset)).To(Succeed())
			}
		}).WithTimeout(60 * time.Second).Should(Succeed())

		By(fmt.Sprintf("checking that the number of nodes is equal to %d", numNodes))
		Eventually(func(g Gomega) {
			// If we're not expecting any nodes in the cluster return with success.
			if numNodes == 0 {
				return
			}
			dpuClusters, err := dpucluster.GetConfigs(ctx, testClient)
			Expect(err).ToNot(HaveOccurred())

			for i := range dpuClusters {
				nodes := &corev1.NodeList{}
				dpuClient, err := dpuClusters[i].Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.List(ctx, nodes)).To(Succeed())
				By(fmt.Sprintf("Expected number of nodes %d to equal %d", len(nodes.Items), numNodes))
				g.Expect(nodes.Items).To(HaveLen(numNodes))
			}
		}).WithTimeout(30 * time.Minute).WithPolling(120 * time.Second).Should(Succeed())
	})

	It("ensure the system DPUServices are created in the DPUClusters", func() {
		By("Checking that DPUService objects have been mirrored to the DPUClusters")
		dpuControlPlanes, err := dpucluster.GetConfigs(ctx, testClient)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			for i := range dpuControlPlanes {
				dpuClient, err := dpuControlPlanes[i].Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				deployments := appsv1.DeploymentList{}
				g.Expect(dpuClient.List(ctx, &deployments, client.HasLabels{argoCDInstanceLabel})).To(Succeed())
				found := map[string]bool{}
				for i := range deployments.Items {
					g.Expect(deployments.Items[i].GetLabels()).To(HaveKey(argoCDInstanceLabel))
					g.Expect(deployments.Items[i].GetLabels()[argoCDInstanceLabel]).NotTo(Equal(""))
					found[deployments.Items[i].GetLabels()[argoCDInstanceLabel]] = true
				}
				daemonsets := appsv1.DaemonSetList{}
				g.Expect(dpuClient.List(ctx, &daemonsets, client.HasLabels{argoCDInstanceLabel})).To(Succeed())
				for i := range daemonsets.Items {
					g.Expect(daemonsets.Items[i].GetLabels()).To(HaveKey(argoCDInstanceLabel))
					g.Expect(daemonsets.Items[i].GetLabels()[argoCDInstanceLabel]).NotTo(Equal(""))
					found[daemonsets.Items[i].GetLabels()[argoCDInstanceLabel]] = true
				}

				// Expect each of the following to have been created by the operator.
				// These are labels on the appv1 type - e.g. DaemonSet or Deployment on the DPU cluster.
				g.Expect(found).To(HaveLen(8))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.MultusName)))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.FlannelName)))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SRIOVDevicePluginName)))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.ServiceSetControllerName)))
				// Note: The NVIPAM DPUService contains both a Daemonset and a Deployment - but this is overwritten in the map.
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.NVIPAMName)))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSCNIName)))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SFCControllerName)))
				g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSHelperName)))
			}
		}).WithTimeout(600 * time.Second).Should(Succeed())
	})
}

// VerifyDPFOperatorConfiguration verifies that DPFOperatorConfiguration options work.
// It changes the images for all system components to arbitrary values, checks that the changes have propagated
// and then changes them back to their default versions.
// This function tests DPUService image setting as it is complex and requires e2e testing.
func VerifyDPFOperatorConfiguration(ctx context.Context, config *operatorv1.DPFOperatorConfig) {
	It("verify ImageConfiguration from DPUServices", func() {
		modifiedConfig := config.DeepCopy()
		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		originalConfig := modifiedConfig.DeepCopy()

		dummyRegistryName := "dummy-registry.com"
		imageTemplate := "%s/%s:v1.0"
		// Update the config with a new image and tag.

		// For objects which are deployed as DPUServices set the helm chart field in configuration.
		// Excluding flannel which DPF Operator does not allow setting an image for.
		modifiedConfig.Spec.ServiceSetController = &operatorv1.ServiceSetControllerConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ServiceSetControllerName)),
		}
		modifiedConfig.Spec.Multus = &operatorv1.MultusConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.MultusName)),
		}
		modifiedConfig.Spec.SRIOVDevicePlugin = &operatorv1.SRIOVDevicePluginConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SRIOVDevicePluginName)),
		}
		modifiedConfig.Spec.OVSCNI = &operatorv1.OVSCNIConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.OVSCNIName)),
		}
		modifiedConfig.Spec.NVIPAM = &operatorv1.NVIPAMConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMName)),
		}
		modifiedConfig.Spec.SFCController = &operatorv1.SFCControllerConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SFCControllerName)),
		}
		modifiedConfig.Spec.OVSHelper = &operatorv1.OVSHelperConfiguration{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.OVSHelperName)),
		}
		Expect(testClient.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

		// Assert the images are set for the system components.
		Eventually(func(g Gomega) {
			deploymentDPUservices := map[string]bool{
				operatorv1.NVIPAMName:               true,
				operatorv1.ServiceSetControllerName: true,
			}
			daemonSetDPUServices := map[string]bool{
				operatorv1.SRIOVDevicePluginName: true,
				operatorv1.SFCControllerName:     true,
				operatorv1.OVSCNIName:            true,
				operatorv1.NVIPAMName:            true,
				operatorv1.MultusName:            true,
				operatorv1.OVSHelperName:         true,
				// Ignoring flannel as the image is never set.
			}

			// Verify images in the DPUClusters
			dpuClusterConfig, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).NotTo(HaveOccurred())
			for _, clusterConfig := range dpuClusterConfig {
				dpuClient, err := clusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				for name := range deploymentDPUservices {
					deployments := appsv1.DeploymentList{}
					nameForCluster := fmt.Sprintf("%s-%s", clusterConfig.Cluster.Name, name)
					g.Expect(dpuClient.List(ctx, &deployments,
						client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
					g.Expect(deployments.Items).To(HaveLen(1))
					deployment := deployments.Items[0]
					g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
					g.Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(dummyRegistryName))
				}
				for name := range daemonSetDPUServices {
					daemonSets := appsv1.DaemonSetList{}
					nameForCluster := fmt.Sprintf("%s-%s", clusterConfig.Cluster.Name, name)
					g.Expect(dpuClient.List(ctx, &daemonSets,
						client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
					g.Expect(daemonSets.Items).To(HaveLen(1))
					daemonSet := daemonSets.Items[0]
					g.Expect(daemonSet.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(dummyRegistryName))
				}
			}
		}).WithTimeout(120 * time.Second).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
			resetConfig := modifiedConfig.DeepCopy()
			resetConfig.Spec = originalConfig.Spec
			// Revert the image versions to their previous values.
			g.Expect(testClient.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
			// Ensure the changes are reverted before continuing.
		}).Should(Succeed())
	})

	// Get dpuClusterConfigs to loop over all DPU clusters.
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
	Expect(err).ToNot(HaveOccurred())

	It("verify that the current MTU in the DPU clusters flannel configmap is 1500", func() {
		for _, dpuClusterConfig := range dpuClusterConfigs {
			dpuClient, err := dpuClusterConfig.Client(ctx)
			Expect(err).ToNot(HaveOccurred())
			By("verify flannel configmap for cluster " + dpuClusterConfig.Cluster.GetName())
			flannelConfigMap := &corev1.ConfigMap{}
			Expect(dpuClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
			Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("MTU\": 1500,"))
		}
	})

	It("change the MTU in the DPFOperatorConfig to 1501 and verify that the DPU clusters flannel configmap is updated", func() {
		By("get the DPFOperatorConfig")
		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		By("update the MTU in the DPFOperatorConfig")
		if config.Spec.Networking == nil {
			config.Spec.Networking = &operatorv1.Networking{}
		}
		config.Spec.Networking.ControlPlaneMTU = ptr.To(1501)
		Expect(testClient.Update(ctx, config)).To(Succeed())

		for _, dpuClusterConfig := range dpuClusterConfigs {
			dpuClient, err := dpuClusterConfig.Client(ctx)
			Expect(err).ToNot(HaveOccurred())
			By("verify flannel configmap for cluster " + dpuClusterConfig.Cluster.GetName())
			Eventually(func(g Gomega) {
				flannelConfigMap := &corev1.ConfigMap{}
				Expect(dpuClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
				Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("MTU\": 1501,"))
			}, time.Second*300, time.Millisecond*250).Should(Succeed())
		}
	})
}

func ValidateDPUService(ctx context.Context, config *operatorv1.DPFOperatorConfig) {
	dpuServiceName := "dpu-01"
	hostDPUServiceName := "host-dpu-service"
	dpuServiceNamespace := "dpu-test-ns"

	It("create a DPUService and check Objects and ImagePullSecrets are mirrored correctly", func() {

		By("create namespace for DPUService")
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceNamespace}}
		testNS.SetLabels(cleanupLabels)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())

		By("create ImagePullSecret for DPUService in user namespace")
		testNSImagePullSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pullSecretName,
				Namespace: dpuServiceNamespace,
				Labels: map[string]string{
					dpuservicev1.DPFImagePullSecretLabelKey: "",
				},
			},
		}
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNSImagePullSecret))).ToNot(HaveOccurred())

		By("create a DPUServiceInterface")
		dsi := unstructuredFromFile("application/dpuserviceinterface_service_type.yaml")
		dsi.SetName("net1-service")
		dsi.SetNamespace(dpuServiceNamespace)
		dsi.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dsi)).To(Succeed())

		By("create a DPUService to be deployed on the DPUCluster")
		// Create DPUCluster DPUService and check it's correctly reconciled.
		dpuService := getDPUService(dpuServiceNamespace, dpuServiceName, false)
		Expect(unstructured.SetNestedSlice(dpuService.UnstructuredContent(), []interface{}{"net1-service"}, "spec", "interfaces")).Should(Succeed())
		Expect(unstructured.SetNestedField(dpuService.UnstructuredContent(), "my-service", "spec", "serviceID")).Should(Succeed())
		dpuService.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuService)).To(Succeed())

		By("create a DPUService to be deployed on the host cluster")
		// Create a host DPUService and check it's correctly reconciled
		// Read the DPUService from file and create it.
		hostDPUService := getDPUService(dpuServiceNamespace, hostDPUServiceName, true)
		hostDPUService.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, hostDPUService)).To(Succeed())

		By("verify DPUServices and deployments are created")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())

				// Check the deployment from the DPUService can be found on the destination cluster.
				deploymentList := appsv1.DeploymentList{}
				g.Expect(dpuClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
				g.Expect(deploymentList.Items).To(HaveLen(1))
				g.Expect(deploymentList.Items[0].Name).To(ContainSubstring("helm-guestbook"))

				// Check an imagePullSecret was created in the same namespace in the destination cluster.
				g.Expect(dpuClient.Get(ctx, client.ObjectKey{
					Namespace: dpuService.GetNamespace(),
					Name:      config.Spec.ImagePullSecrets[0]}, &corev1.Secret{})).To(Succeed())
			}
		}).WithTimeout(600 * time.Second).Should(Succeed())

		By("verify DPUService is created in the host cluster")
		Eventually(func(g Gomega) {
			// Check the deployment from the DPUService can be found on the host cluster.
			deploymentList := appsv1.DeploymentList{}
			g.Expect(testClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
			g.Expect(deploymentList.Items).To(HaveLen(1))
			g.Expect(deploymentList.Items[0].Name).To(ContainSubstring("helm-guestbook"))
		}).WithTimeout(600 * time.Second).Should(Succeed())
	})

	It("delete the DPUServices and check that the applications are cleaned up", func() {
		if skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("delete the DPUServices")
		svc := &dpuservicev1.DPUService{}
		// Delete the DPUCluster DPUService.
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: dpuServiceName}, svc)).To(Succeed())
		Expect(testClient.Delete(ctx, svc)).To(Succeed())

		// Delete the host cluster DPUService.
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svc)).To(Succeed())
		Expect(testClient.Delete(ctx, svc)).To(Succeed())

		dsi := &dpuservicev1.DPUServiceInterface{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: "net1-service"}, dsi)).To(Succeed())
		Expect(utils.CleanupAndWait(ctx, testClient, dsi)).To(Succeed())

		// Check the DPUCluster DPUService is correctly deleted.
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				deploymentList := appsv1.DeploymentList{}
				g.Expect(dpuClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
				g.Expect(deploymentList.Items).To(BeEmpty())
			}
		}).WithTimeout(300 * time.Second).Should(Succeed())

		// Ensure the hostDPUService deployment is deleted from the host cluster.
		Eventually(func(g Gomega) {
			deploymentList := appsv1.DeploymentList{}
			g.Expect(testClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
			g.Expect(deploymentList.Items).To(BeEmpty())
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})

	It("verify that the ImagePullSecrets have been synced correctly and cleaned up", func() {
		// Verify that we have 2 secrets in the DPU Cluster.
		verifyImagePullSecretsCount(dpfOperatorSystemNamespace, 2)

		desiredConf := &operatorv1.DPFOperatorConfig{}
		Eventually(testClient.Get).WithArguments(ctx, client.ObjectKeyFromObject(config), desiredConf).Should(Succeed())
		currentConf := desiredConf.DeepCopy()

		// Patch the DPFOperatorConfig to remove the second secret. This causes the label to be removed.
		desiredConf.Spec.ImagePullSecrets = append(desiredConf.Spec.ImagePullSecrets[:1], desiredConf.Spec.ImagePullSecrets[2:]...)

		Eventually(testClient.Patch).WithArguments(ctx, desiredConf, client.MergeFrom(currentConf)).Should(Succeed())

		// Patch a DPUService to trigger a reconciliation. The DPUService should clean  this secret up from
		// clusters to which it was previously mirrored.
		Eventually(utils.ForceObjectReconcileWithAnnotation).WithArguments(ctx, testClient,
			&dpuservicev1.DPUService{ObjectMeta: metav1.ObjectMeta{Name: operatorv1.MultusName, Namespace: "dpf-operator-system"}}).Should(Succeed())
		// Verify we only have one image pull secret.
		verifyImagePullSecretsCount(dpfOperatorSystemNamespace, 1)
	})
}
func getDPUService(namespace, name string, host bool) *unstructured.Unstructured {
	// Create a host DPUService and check it's correctly reconciled
	// Read the DPUService from file and create it.
	svc := unstructuredFromFile("application/dpuservice.yaml")
	svc.SetName(name)
	svc.SetNamespace(namespace)

	// This annotation is what defines a host DPUService.
	if host {
		dpuService := &dpuservicev1.DPUService{}
		Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svc.UnstructuredContent(), dpuService)).ToNot(HaveOccurred())
		dpuService.Spec.DeployInCluster = &host
		obj, err := machineryruntime.DefaultUnstructuredConverter.ToUnstructured(dpuService)
		Expect(err).ToNot(HaveOccurred())
		svc = &unstructured.Unstructured{
			Object: obj,
		}
	}
	return svc
}
func verifyImagePullSecretsCount(namespace string, count int) {
	secrets := &corev1.SecretList{}
	Expect(testClient.List(ctx, secrets,
		client.InNamespace(namespace),
		client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}),
	).ToNot(HaveOccurred())
	Eventually(func(g Gomega) {
		dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
		g.Expect(err).ToNot(HaveOccurred())
		for _, dpuClusterConfig := range dpuClusterConfigs {
			dpuClient, err := dpuClusterConfig.Client(ctx)
			g.Expect(err).ToNot(HaveOccurred())

			// Check the imagePullSecrets has been deleted.
			secrets := &corev1.SecretList{}
			g.Expect(dpuClient.List(ctx, secrets,
				client.InNamespace(namespace),
				client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}),
			).To(Succeed())
			g.Expect(secrets.Items).To(HaveLen(count))
		}
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

func ValidateDPUDeployment(ctx context.Context) {
	It("create a DPUDeployment with its dependencies and ensure that the underlying objects are created", func() {
		By("creating the dependencies")
		dpuServiceTemplate := unstructuredFromFile("application/dpuservicetemplate.yaml")
		dpuServiceTemplate.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())

		dpuServiceConfiguration := unstructuredFromFile("application/dpuserviceconfiguration.yaml")
		dpuServiceConfiguration.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())

		By("creating the dpudeployment")
		dpuDeployment := unstructuredFromFile("application/dpudeployment.yaml")
		dpuDeployment.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())

		By("checking that the underlying objects are created")
		Eventually(func(g Gomega) {
			gotDPUSetList := &provisioningv1.DPUSetList{}
			g.Expect(testClient.List(ctx,
				gotDPUSetList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUSetList.Items).To(HaveLen(1))

			gotDPUServiceList := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

			gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceChainList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

			gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceInterfaceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("delete the DPUDeployment and ensure the underlying objects are gone", func() {
		if skipCleanup {
			Skip("Skip cleanup resources")
		}

		By("deleting the dpudeployment")
		dpuDeployment := unstructuredFromFile("application/dpudeployment.yaml")
		Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

		By("checking that the underlying objects are deleted")
		Eventually(func(g Gomega) {
			gotDPUSetList := &provisioningv1.DPUSetList{}
			g.Expect(testClient.List(ctx,
				gotDPUSetList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUSetList.Items).To(BeEmpty())

			gotDPUServiceList := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUServiceList.Items).To(BeEmpty())

			gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceChainList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

			gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceInterfaceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"dpu.nvidia.com/dpudeployment-name": dpuDeployment.GetName(),
				})).To(Succeed())
			g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())

			// Expect the DPUDeployment to be deleted
			err := testClient.Get(ctx, client.ObjectKey{Namespace: dpuDeployment.GetNamespace(), Name: dpuDeployment.GetName()}, &dpuservicev1.DPUDeployment{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})
}

func ValidateDPUServiceIPAM(ctx context.Context) {
	dpuServiceIPAMWithIPPoolName := "switched-application"
	dpuServiceIPAMWithCIDRPoolName := "routed-application"
	dpuServiceIPAMNamespace := dpfOperatorSystemNamespace

	It("create an invalid DPUServiceIPAM and ensure that the webhook rejects the request", func() {
		By("creating the invalid DPUServiceIPAM CR")
		dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-name",
				Namespace: dpuServiceIPAMNamespace,
			},
		}
		dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
		dpuServiceIPAM.SetLabels(cleanupLabels)
		err := testClient.Create(ctx, dpuServiceIPAM)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsBadRequest(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("either ipv4Subnet or ipv4Network must be specified"))
	})

	It("create a DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is created to each cluster", func() {
		By("creating the DPUServiceIPAM CR")
		dpuServiceIPAM := unstructuredFromFile("application/dpuserviceipam_subnet.yaml")
		dpuServiceIPAM.SetName(dpuServiceIPAMWithIPPoolName)
		dpuServiceIPAM.SetNamespace(dpuServiceIPAMNamespace)
		dpuServiceIPAM.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())

		By("checking that NVIPAM IPPool CR is created in the DPU clusters")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())

				ipPools := &nvipamv1.IPPoolList{}
				g.Expect(dpuClient.List(ctx, ipPools, client.MatchingLabels{
					"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
					"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
				})).To(Succeed())
				g.Expect(ipPools.Items).To(HaveLen(1))

				// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
			}
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("delete the DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is deleted in each cluster", func() {
		if skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("deleting the DPUServiceIPAM")
		dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceIPAMNamespace, Name: dpuServiceIPAMWithIPPoolName}, dpuServiceIPAM)).To(Succeed())
		Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

		By("checking that NVIPAM IPPool CR is deleted in each DPU cluster")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())

				ipPools := &nvipamv1.IPPoolList{}
				g.Expect(dpuClient.List(ctx, ipPools, client.MatchingLabels{
					"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
					"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
				})).To(Succeed())
				g.Expect(ipPools.Items).To(BeEmpty())
			}
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("create a DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is created to each cluster", func() {
		By("creating the DPUServiceIPAM CR")
		dpuServiceIPAM := unstructuredFromFile("application/dpuserviceipam_cidr.yaml")
		dpuServiceIPAM.SetName(dpuServiceIPAMWithCIDRPoolName)
		dpuServiceIPAM.SetNamespace(dpuServiceIPAMNamespace)
		dpuServiceIPAM.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())

		By("checking that NVIPAM CIDRPool CR is created in the DPU clusters")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())

				cidrPools := &nvipamv1.CIDRPoolList{}
				g.Expect(dpuClient.List(ctx, cidrPools, client.MatchingLabels{
					"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
					"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
				})).To(Succeed())
				g.Expect(cidrPools.Items).To(HaveLen(1))

				// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
			}
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("delete the DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is deleted in each cluster", func() {
		if skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("deleting the DPUServiceIPAM")
		dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceIPAMNamespace, Name: dpuServiceIPAMWithCIDRPoolName}, dpuServiceIPAM)).To(Succeed())
		Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

		By("checking that NVIPAM CIDRPool CR is deleted in each DPU cluster")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())

				cidrPools := &nvipamv1.CIDRPoolList{}
				g.Expect(dpuClient.List(ctx, cidrPools, client.MatchingLabels{
					"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
					"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
				})).To(Succeed())
				g.Expect(cidrPools.Items).To(BeEmpty())
			}
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

}

func ValidateDPUServiceCredentialRequest(ctx context.Context) {
	hostDPUServiceCredentialRequestName := "host-dpu-credential-request"
	dpuServiceCredentialRequestName := "dpu-01-credential-request"
	dpuServiceCredentialRequestNamespace := "dpucr-test-ns"

	It("create a DPUServiceCredentialRequest and check that the credentials are created", func() {
		By("create namespace for DPUServiceCredentialRequest")
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceCredentialRequestNamespace}}
		testNS.SetLabels(cleanupLabels)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())

		By("create a DPUServiceCredentialRequest targeting the DPUCluster")
		dcr := getDPUServiceCredentialRequest(dpuServiceCredentialRequestNamespace, dpuServiceCredentialRequestName,
			&dpuservicev1.NamespacedName{Name: "dpu-cluster-1", Namespace: ptr.To(dpfOperatorSystemNamespace)})
		dcr.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dcr)).To(Succeed())

		By("create a DPUServiceCredentialRequest targeting the host cluster")
		hostDsr := getDPUServiceCredentialRequest(dpuServiceCredentialRequestNamespace, hostDPUServiceCredentialRequestName, nil)
		hostDsr.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, hostDsr)).To(Succeed())

		By("verify reconciled DPUServiceCredentialRequest for DPUCluster")
		Eventually(func(g Gomega) {
			assertDPUServiceCredentialRequest(g, testClient, dcr, false)
		}).WithTimeout(300 * time.Second).Should(Succeed())

		By("verify reconciled DPUServiceCredentialRequest for host cluster")
		Eventually(func(g Gomega) {
			assertDPUServiceCredentialRequest(g, testClient, hostDsr, true)
		}).WithTimeout(600 * time.Second).Should(Succeed())
	})

	It("delete the DPUServiceCredentialRequest and check that the credentials are deleted", func() {
		if skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("delete the DPUServiceCredentialRequest")
		dcr := &dpuservicev1.DPUServiceCredentialRequest{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceCredentialRequestNamespace, Name: dpuServiceCredentialRequestName}, dcr)).To(Succeed())
		Expect(testClient.Delete(ctx, dcr)).To(Succeed())

		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceCredentialRequestNamespace, Name: hostDPUServiceCredentialRequestName}, dcr)).To(Succeed())
		Expect(testClient.Delete(ctx, dcr)).To(Succeed())
	})

}
func getDPUServiceCredentialRequest(namespace, name string, targetCluster *dpuservicev1.NamespacedName) *dpuservicev1.DPUServiceCredentialRequest {
	data, err := os.ReadFile(filepath.Join(testObjectsPath, "application/dpuservicecredentialrequest.yaml"))
	Expect(err).ToNot(HaveOccurred())
	dcr := &dpuservicev1.DPUServiceCredentialRequest{}
	Expect(yaml.Unmarshal(data, dcr)).To(Succeed())
	dcr.SetName(name)
	dcr.SetNamespace(namespace)

	// This annotation is what defines a host DPUService.
	if targetCluster != nil {
		dcr.Spec.TargetCluster = targetCluster
	}
	return dcr
}
func assertDPUServiceCredentialRequest(g Gomega, testClient client.Client, dcr *dpuservicev1.DPUServiceCredentialRequest, host bool) {
	gotDsr := &dpuservicev1.DPUServiceCredentialRequest{}
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dcr), gotDsr)).To(Succeed())
	g.Expect(gotDsr.Finalizers).To(ConsistOf([]string{dpuservicev1.DPUServiceCredentialRequestFinalizer}))
	g.Expect(gotDsr.Status.ServiceAccount).NotTo(BeNil())
	g.Expect(*gotDsr.Status.ServiceAccount).To(Equal(dcr.Spec.ServiceAccount.String()))
	g.Expect(gotDsr.Status.ExpirationTimestamp.Time).To(BeTemporally("~", time.Now().Add(time.Hour), time.Minute))
	g.Expect(gotDsr.Status.IssuedAt).NotTo(BeNil())
	if gotDsr.Spec.Duration != nil {
		iat := gotDsr.Status.ExpirationTimestamp.Time.Add(-1 * gotDsr.Spec.Duration.Duration)
		g.Expect(gotDsr.Status.IssuedAt.Time).To(BeTemporally("~", iat, time.Minute))
	}

	if host {
		g.Expect(gotDsr.Status.TargetCluster).To(BeNil())
	} else {
		g.Expect(gotDsr.Status.TargetCluster).To(Equal(ptr.To(dcr.Spec.TargetCluster.String())))
	}
}

func ValidateDPUServiceChain(ctx context.Context) {
	dpuServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test"
	dpuServiceChainName := "svc-chain-test"
	dpuServiceChainNamespace := "test-2"

	It("create DPUServiceInterface and check that it is mirrored to each cluster", func() {
		By("create test namespace")
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceNamespace}}
		testNS.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		By("create DPUServiceInterface")
		dpuServiceInterface := unstructuredFromFile("application/dpuserviceinterface.yaml")
		dpuServiceInterface.SetName(dpuServiceInterfaceName)
		dpuServiceInterface.SetNamespace(dpuServiceInterfaceNamespace)
		dpuServiceInterface.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
		By("verify ServiceInterfaceSet is created in DPF clusters")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceName, Namespace: dpuServiceInterfaceNamespace}}
				g.Expect(dpuClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}
		}, time.Second*300, time.Millisecond*250).Should(Succeed())
	})

	It("create DPUServiceChain and check that it is mirrored to each cluster", func() {
		By("create test namespace")
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceChainNamespace}}
		testNS.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		By("create DPUServiceChain")
		dpuServiceChain := unstructuredFromFile("application/dpuservicechain.yaml")
		dpuServiceChain.SetName(dpuServiceChainName)
		dpuServiceChain.SetNamespace(dpuServiceChainNamespace)
		dpuServiceChain.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
		By("verify ServiceChainSet is created in DPF clusters")
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceChainName, Namespace: dpuServiceChainNamespace}}
				g.Expect(dpuClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}
		}, time.Second*300, time.Millisecond*250).Should(Succeed())
	})

	It("delete the DPUServiceChain & DPUServiceInterface and check that the Sets are cleaned up", func() {
		if skipCleanup {
			Skip("Skip cleanup resources")
		}
		dsi := &dpuservicev1.DPUServiceInterface{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceInterfaceNamespace, Name: dpuServiceInterfaceName}, dsi)).To(Succeed())
		Expect(testClient.Delete(ctx, dsi)).To(Succeed())
		dsc := &dpuservicev1.DPUServiceChain{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceChainNamespace, Name: dpuServiceChainName}, dsc)).To(Succeed())
		Expect(testClient.Delete(ctx, dsc)).To(Succeed())
		// Get the control plane secrets.
		Eventually(func(g Gomega) {
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).ToNot(HaveOccurred())
			for _, dpuClusterConfig := range dpuClusterConfigs {
				dpuClient, err := dpuClusterConfig.Client(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				serviceChainSetList := dpuservicev1.ServiceChainSetList{}
				g.Expect(dpuClient.List(ctx, &serviceChainSetList,
					&client.ListOptions{Namespace: dpuServiceChainNamespace})).To(Succeed())
				g.Expect(serviceChainSetList.Items).To(BeEmpty())
				serviceInterfaceSetList := dpuservicev1.ServiceInterfaceSetList{}
				g.Expect(dpuClient.List(ctx, &serviceInterfaceSetList,
					&client.ListOptions{Namespace: dpuServiceInterfaceNamespace})).To(Succeed())
				g.Expect(serviceInterfaceSetList.Items).To(BeEmpty())
			}
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})
}

func unstructuredFromFile(path string) *unstructured.Unstructured {
	data, err := os.ReadFile(filepath.Join(testObjectsPath, path))
	Expect(err).ToNot(HaveOccurred())
	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(data, obj)).To(Succeed())
	obj.SetLabels(cleanupLabels)
	return obj
}

func collectResourcesAndLogs(ctx context.Context) error {
	// Get the path to place artifacts in
	_, basePath, _, _ := runtime.Caller(0)
	artifactsPath := filepath.Join(filepath.Dir(basePath), "../../artifacts")
	inventoryManifestsPath := filepath.Join(filepath.Dir(basePath), "../../internal/operator/inventory/manifests")

	// Create a resourceCollector to dump logs and resources for test debugging.
	clusters, err := collector.GetClusterCollectors(ctx, testClient, artifactsPath, inventoryManifestsPath, clientset)
	Expect(err).NotTo(HaveOccurred())
	return collector.New(clusters).Run(ctx)
}
