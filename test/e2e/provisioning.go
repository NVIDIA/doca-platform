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
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// skipDPUClusterDeletionInProvisioningTest avoids a known race: if the test deletes
// DPUCluster before DPUServices (and their Argo CD Applications) finish cleanup,
// Applications cannot reach the cluster (no route to host), DPUServices get stuck
// with finalizers, and AfterSuite cannot delete DPFOperatorConfig.
// Set to false once the deletion ordering is fixed in the product.
// RM: 4869399
const skipDPUClusterDeletionInProvisioningTest = true

// ProvisioningExpected holds all expected counts for provisioning tests
type ProvisioningExpected struct {
	DPUNodes      int
	DPUsPerNode   int
	TotalDPUs     int
	DPUClusters   int
	DPUSets       int
	DPUFlavors    int
	BFBs          int
	Prerequisites int
	DPUServices   int
}

// provisioningExpected is the centralized expected values for the current test
var provisioningExpected ProvisioningExpected

// initProvisioningExpected initializes the expected counts from input
func initProvisioningExpected(input *systemTestInput) {
	provisioningExpected = ProvisioningExpected{
		DPUNodes:      input.numberOfDPUNodes,
		DPUsPerNode:   input.numberOfDPUsPerNode,
		TotalDPUs:     input.totalDPUs(),
		DPUClusters:   1, // Provisioning tests create one DPUCluster
		DPUSets:       1, // Provisioning tests create one DPUSet
		BFBs:          1, // Provisioning tests create one BFB
		Prerequisites: len(input.dpuClusterPrerequisites),
		DPUServices:   6, // Multus, Flannel, SRIOV, NVIPAM, OVS-CNI, SFC-Controller
	}

	provisioningExpected.DPUFlavors = 1 // DPUFlavor is always required
}

// printProvisioningConfiguration prints the expected test configuration
func printProvisioningConfiguration(input *systemTestInput) {
	By("========== PROVISIONING TEST CONFIGURATION ==========")
	By(fmt.Sprintf("  DPU Nodes:           %d", provisioningExpected.DPUNodes))
	By(fmt.Sprintf("  DPUs per Node:       %d", provisioningExpected.DPUsPerNode))
	By(fmt.Sprintf("  Total Expected DPUs: %d", provisioningExpected.TotalDPUs))
	By(fmt.Sprintf("  DPU Clusters:        %d", provisioningExpected.DPUClusters))
	By(fmt.Sprintf("  DPU Sets:            %d", provisioningExpected.DPUSets))
	By(fmt.Sprintf("  BFBs:                %d", provisioningExpected.BFBs))
	By(fmt.Sprintf("  DPU Flavors:         %d", provisioningExpected.DPUFlavors))
	By(fmt.Sprintf("  Prerequisites:       %d", provisioningExpected.Prerequisites))
	By(fmt.Sprintf("  DPU Services:        %d", provisioningExpected.DPUServices))
	By(fmt.Sprintf("  DPU Flavor Name:     %s", input.dpuFlavor.Name))
	By("=====================================================")
}

// VerifyDPUServicesDeployed verifies that the expected DPUServices are deployed in the DPU cluster.
// This function can be reused across different test files (provisioning, system_setup, etc.)
func VerifyDPUServicesDeployed(ctx context.Context, clusterClient client.Client, namespace string) {
	By("Verifying DPUServices deployed")
	servicesTracker := NewByTracker()
	Eventually(func(g Gomega) {
		// Check Deployments with argocd instance annotation
		deployments := &appsv1.DeploymentList{}
		g.Expect(dpuClusterClient[0].List(ctx, deployments)).To(Succeed())
		found := map[string]bool{}
		for i := range deployments.Items {
			if _, hasAnnotation := deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]; hasAnnotation {
				g.Expect(deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]).NotTo(Equal(""))
				found[deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]] = true
			}
		}

		// Check DaemonSets with argocd instance annotation
		daemonsets := appsv1.DaemonSetList{}
		g.Expect(dpuClusterClient[0].List(ctx, &daemonsets, client.InNamespace(input.dpuClusters[0].GetNamespace()))).To(Succeed())
		for i := range daemonsets.Items {
			if _, hasAnnotation := daemonsets.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]; hasAnnotation {
				g.Expect(daemonsets.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]).NotTo(Equal(""))
				found[daemonsets.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]] = true
			}
		}

		// Count found services
		foundCount := 0
		serviceNames := []string{
			operatorv1.MultusName.String(),
			operatorv1.FlannelName.String(),
			operatorv1.SRIOVDevicePluginName.String(),
			operatorv1.NVIPAMName.String(),
			operatorv1.OVSCNIName.String(),
			operatorv1.SFCControllerName.String(),
		}
		for _, svcName := range serviceNames {
			for key := range found {
				if strings.Contains(key, svcName) {
					foundCount++
					break
				}
			}
		}
		servicesTracker.By(fmt.Sprintf("%d", foundCount),
			"DPUServices deployed [%d/%d]", foundCount, provisioningExpected.DPUServices)

		// Verify expected DPUServices are deployed
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.MultusName.String())), "Multus should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.FlannelName.String())), "Flannel should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SRIOVDevicePluginName.String())), "SRIOV should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.NVIPAMName.String())), "NVIPAM should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSCNIName.String())), "OVS-CNI should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SFCControllerName.String())), "SFC-Controller should be deployed")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func BeforeProvisioning(ctx context.Context, input *systemTestInput) {
	// Initialize expected counts from input
	initProvisioningExpected(input)

	// Print test configuration at the start
	printProvisioningConfiguration(input)
	By("Verifying DPU nodes are available for provisioning tests")
	Expect(input.hasDpuNodes()).To(BeTrue(),
		"SETUP ERROR: No DPU nodes found in cluster. "+
			"Provisioning tests require DPU nodes to be configured. "+
			"Please ensure DPU hardware is available and properly configured before running these tests.")

	By("Verifying no unexpected provisioning resources")
	provisioningResources := map[string]client.ObjectList{
		"DPUSet":     &provisioningv1.DPUSetList{},
		"DPU":        &provisioningv1.DPUList{},
		"BFB":        &provisioningv1.BFBList{},
		"DPUCluster": &provisioningv1.DPUClusterList{},
	}

	var dirty []string
	for name, list := range provisioningResources {
		if err := input.client.List(ctx, list); err != nil {
			continue
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			continue
		}
		if len(items) > 0 {
			dirty = append(dirty, fmt.Sprintf("  • %s: %d instances", name, len(items)))
		}
	}

	if len(dirty) > 0 {
		Fail(fmt.Sprintf("Found unexpected provisioning resources:\n%s\n\n"+
			"These resources should have been cleaned up by the previous test's AfterAll.\n"+
			"Run cleanup manually or delete these resources before running tests again.",
			strings.Join(dirty, "\n")))
	}
}

func CreateProvisioningDPUCluster(ctx context.Context, input *systemTestInput) {
	// Create prerequisite objects
	for i, obj := range input.dpuClusterPrerequisites {
		// Deep copy to avoid mutating the shared original object
		objCopy := obj.DeepCopyObject().(client.Object)
		objCopy.SetLabels(CleanupScope.Suite)

		existing := objCopy.DeepCopyObject().(client.Object)
		err := input.client.Get(ctx, types.NamespacedName{
			Namespace: objCopy.GetNamespace(),
			Name:      objCopy.GetName(),
		}, existing)

		if apierrors.IsNotFound(err) {
			By(fmt.Sprintf("Creating prerequisite [%d/%d] %s/%s",
				i+1, provisioningExpected.Prerequisites,
				objCopy.GetNamespace(),
				objCopy.GetName()))
			Expect(input.client.Create(ctx, objCopy)).To(Succeed())
		} else {
			By(fmt.Sprintf("Prerequisite [%d/%d] %s/%s already exists",
				i+1, provisioningExpected.Prerequisites,
				objCopy.GetNamespace(),
				objCopy.GetName()))
			Expect(err).To(Succeed(), "Failed to check existing prerequisite")
		}
	}

	// Deep copy to avoid mutating the shared original object
	dpuCluster := input.dpuClusters[0].DeepCopy()
	dpuCluster.SetLabels(CleanupScope.Suite)

	By(fmt.Sprintf("Creating DPUCluster %s/%s",
		dpuCluster.GetNamespace(),
		dpuCluster.GetName()))
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuCluster))).To(Succeed())

	By("Verifying DPUCluster exists")
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(provisioningExpected.DPUClusters),
			fmt.Sprintf("Expected %d DPU cluster(s)", provisioningExpected.DPUClusters))
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for DPUCluster to reach Ready phase")
	clusterTracker := NewByTracker()
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(provisioningExpected.DPUClusters))

		cluster := clusters.Items[0]
		clusterTracker.By(cluster.Name+string(cluster.Status.Phase),
			"DPUCluster %s Phase: %s", cluster.Name, cluster.Status.Phase)
		g.Expect(cluster.Status.Phase).To(Equal(provisioningv1.PhaseReady),
			"DPU cluster should reach Ready phase")
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	By("Creating DPU cluster client connection")
	// getDPUClusterClients requires ProvisionDPUClustersInput (defined in system_setup.go)
	getDPUClusterClients(ctx, ProvisionDPUClustersInput{
		dpuClusters: input.dpuClusters,
		client:      input.client,
		restConfig:  input.restConfig,
	})

	bfb := input.bfb.DeepCopy()
	bfb.SetLabels(CleanupScope.Suite)

	// Override BFB URL if environment variable is set (on the copy, not the original)
	if input.bfbImageURL != "" {
		By(fmt.Sprintf("Overriding BFB URL with: %s", input.bfbImageURL))
		bfb.Spec.URL = input.bfbImageURL
	}
	By(fmt.Sprintf("Creating BFB %s/%s", bfb.GetNamespace(), bfb.GetName()))
	Eventually(func(g Gomega) {
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, bfb))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Verifying BFB object exists")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.client.Get(ctx, types.NamespacedName{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)).To(Succeed(), "BFB should be created")
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for BFB to reach Ready phase")
	bfbTracker := NewByTracker()
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.client.Get(ctx, types.NamespacedName{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)).To(Succeed())
		bfbTracker.By(bfb.Name+string(bfb.Status.Phase),
			"BFB %s Phase: %s", bfb.Name, bfb.Status.Phase)
		g.Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady),
			"BFB should reach Ready phase")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func CreateProvisioningDPUSet(ctx context.Context, input *systemTestInput) {
	// DPUFlavor is required for provisioning - fail fast if missing
	Expect(input.dpuFlavor).NotTo(BeNil(), "dpuFlavor is required - check test configuration")

	dpuFlavor := input.dpuFlavor.DeepCopy()
	dpuFlavor.SetLabels(CleanupScope.Suite)
	By(fmt.Sprintf("Creating DPUFlavor %s/%s", dpuFlavor.GetNamespace(), dpuFlavor.GetName()))
	Eventually(func(g Gomega) {
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuFlavor))).To(Succeed())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Verifying DPUFlavor exists")
	Eventually(func(g Gomega) {
		dpuFlavor := &provisioningv1.DPUFlavor{}
		g.Expect(input.client.Get(ctx, types.NamespacedName{
			Name:      input.dpuFlavor.Name,
			Namespace: input.dpuFlavor.Namespace,
		}, dpuFlavor)).To(Succeed(), "DPUFlavor should be created")
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	dpuset := input.dpuSet.DeepCopy()
	dpuset.SetLabels(CleanupScope.Suite)
	By(fmt.Sprintf("Creating DPUSet %s/%s", dpuset.GetNamespace(), dpuset.GetName()))
	Eventually(func(g Gomega) {
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuset))).To(Succeed())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Verifying DPUSet exists")
	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(HaveLen(provisioningExpected.DPUSets),
			fmt.Sprintf("Expected %d DPUSet(s)", provisioningExpected.DPUSets))
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Waiting for DPUSet controller to create DPU objects")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(provisioningExpected.TotalDPUs),
			fmt.Sprintf("Expected %d DPU objects, found %d", provisioningExpected.TotalDPUs, len(dpus.Items)))

		for _, dpu := range dpus.Items {
			By(fmt.Sprintf("DPU %s created with Phase: %s", dpu.Name, dpu.Status.Phase))
			g.Expect(dpu.Spec.BFB).NotTo(BeEmpty(), "DPU should reference BFB")
			g.Expect(dpu.Spec.Cluster.Name).NotTo(BeEmpty(), "DPU should reference DPUCluster")
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for DPU nodes to join the DPU cluster as K8s Nodes")
	nodesTracker := NewByTracker()
	dpuPhaseTracker := NewByTracker()
	Eventually(func(g Gomega) {
		// Track DPU phases during node joining
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		for _, dpu := range dpus.Items {
			dpuPhaseTracker.By(dpu.Name+string(dpu.Status.Phase), "DPU %s: %s", dpu.Name, dpu.Status.Phase)
		}

		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).To(Succeed(), "Should be able to list nodes in DPU cluster")

		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), provisioningExpected.TotalDPUs)
		nodesTracker.By(nodeKey, "K8s nodes in DPU cluster [%d/%d]", len(nodes.Items), provisioningExpected.TotalDPUs)
		g.Expect(nodes.Items).To(HaveLen(provisioningExpected.TotalDPUs),
			fmt.Sprintf("DPU cluster should have %d K8s nodes, found %d", provisioningExpected.TotalDPUs, len(nodes.Items)))
	}).WithTimeout(45 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for all DPU objects to reach Ready phase")
	dpuTracker := NewByTracker()
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(provisioningExpected.TotalDPUs))

		readyCount := 0
		for _, dpu := range dpus.Items {
			if dpu.Status.Phase == provisioningv1.DPUReady {
				readyCount++
			}
		}

		dpuKey := fmt.Sprintf("%d/%d", readyCount, provisioningExpected.TotalDPUs)
		dpuTracker.By(dpuKey, "DPUs Ready [%d/%d]", readyCount, provisioningExpected.TotalDPUs)
		for _, dpu := range dpus.Items {
			dpuTracker.By(string(dpu.Status.Phase)+dpu.Name, "DPU %s Phase: %s", dpu.Name, dpu.Status.Phase)
		}

		g.Expect(readyCount).To(Equal(provisioningExpected.TotalDPUs),
			fmt.Sprintf("All %d DPUs should reach Ready phase, only %d ready", provisioningExpected.TotalDPUs, readyCount))
	}).WithTimeout(30 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
}

func VerifyProvisioning(ctx context.Context, input *systemTestInput) {
	deploymentName := fmt.Sprintf("in-cluster-%s", getServiceChainSetControllerDPUServiceName(input.dpuClusters[0].Name, input.dpuClusters[0].Namespace))
	deploymentTracker := NewByTracker()
	By(fmt.Sprintf("Verifying Deployment %s/%s", dpfOperatorSystemNamespace, deploymentName))
	Eventually(func(g Gomega) {
		serviceSetDeployment := &appsv1.Deployment{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      deploymentName,
		}, serviceSetDeployment)).To(Succeed())

		deploymentTracker.By(fmt.Sprintf("%d/%d", serviceSetDeployment.Status.ReadyReplicas, *serviceSetDeployment.Spec.Replicas),
			"Deployment %s Replicas [%d/%d]", deploymentName,
			serviceSetDeployment.Status.ReadyReplicas,
			*serviceSetDeployment.Spec.Replicas)
		g.Expect(serviceSetDeployment.Status.ReadyReplicas).To(Equal(*serviceSetDeployment.Spec.Replicas),
			fmt.Sprintf("%s should be ready", deploymentName))
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	// Use shared function to verify DPUServices are deployed
	VerifyDPUServicesDeployed(ctx, dpuClusterClient[0], input.dpuClusters[0].GetNamespace())

	By("Verifying DPUSet statistics")
	dpuSetTracker := NewByTracker()
	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(HaveLen(provisioningExpected.DPUSets),
			fmt.Sprintf("Expected %d DPUSet(s)", provisioningExpected.DPUSets))

		dpuset := dpusets.Items[0]
		actualTotalDPUs := 0
		readyDPUs := 0

		for phase, count := range dpuset.Status.DPUStatistics {
			actualTotalDPUs += count
			if phase == provisioningv1.DPUReady {
				readyDPUs = count
			}
		}

		dpuSetTracker.By(fmt.Sprintf("%d/%d", readyDPUs, provisioningExpected.TotalDPUs),
			"DPUSet %s/%s Ready [%d/%d]",
			dpuset.GetNamespace(), dpuset.GetName(), readyDPUs, provisioningExpected.TotalDPUs)

		g.Expect(actualTotalDPUs).To(Equal(provisioningExpected.TotalDPUs),
			"DPUSet should track all DPUs")
		g.Expect(readyDPUs).To(Equal(provisioningExpected.TotalDPUs),
			"All DPUs should be in Ready phase")
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Waiting for all system pods to be ready in DPU cluster")
	VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
}

func DeleteProvisioning(ctx context.Context, input *systemTestInput) {
	By("========== DEPROVISIONING ==========")
	By(fmt.Sprintf("  DPUs to remove:      %d", provisioningExpected.TotalDPUs))
	By(fmt.Sprintf("  Prerequisites:       %d", provisioningExpected.Prerequisites))
	By("=====================================")

	By(fmt.Sprintf("Deleting DPUSet %s/%s", input.dpuSet.Namespace, input.dpuSet.Name))
	Eventually(func(g Gomega) {
		dpuset := &provisioningv1.DPUSet{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      input.dpuSet.Name,
			Namespace: input.dpuSet.Namespace,
		}, dpuset)

		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(input.client.Delete(ctx, dpuset)).To(Succeed())
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for DPUSet to be deleted")
	dpuSetDeleteTracker := NewByTracker()
	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		dpuSetDeleteTracker.By(fmt.Sprintf("%d", len(dpusets.Items)),
			"DPUSets remaining [%d]", len(dpusets.Items))
		g.Expect(dpusets.Items).To(BeEmpty(), "DPUSet should be deleted")
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for DPU objects to be deleted")
	dpuDeleteTracker := NewByTracker()
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		dpuDeleteTracker.By(fmt.Sprintf("%d", len(dpus.Items)),
			"DPUs remaining [%d]", len(dpus.Items))
		g.Expect(dpus.Items).To(BeEmpty(),
			"All DPU objects should be cleaned up after DPUSet deletion")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for K8s nodes to be removed from DPU cluster")
	nodesDeleteTracker := NewByTracker()
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).To(Succeed())
		nodesDeleteTracker.By(fmt.Sprintf("%d", len(nodes.Items)),
			"K8s nodes remaining [%d]", len(nodes.Items))
		g.Expect(nodes.Items).To(BeEmpty(),
			"DPU cluster should have no nodes after deprovisioning")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By(fmt.Sprintf("Deleting DPUFlavor %s/%s", input.dpuFlavor.Namespace, input.dpuFlavor.Name))
	Eventually(func(g Gomega) {
		dpuFlavor := &provisioningv1.DPUFlavor{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      input.dpuFlavor.Name,
			Namespace: input.dpuFlavor.Namespace,
		}, dpuFlavor)

		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(input.client.Delete(ctx, dpuFlavor)).To(Succeed())
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for DPUFlavor to be deleted")
	Eventually(func(g Gomega) {
		dpuFlavor := &provisioningv1.DPUFlavor{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      input.dpuFlavor.Name,
			Namespace: input.dpuFlavor.Namespace,
		}, dpuFlavor)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "DPUFlavor should be deleted")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	By(fmt.Sprintf("Deleting BFB %s/%s", input.bfb.Namespace, input.bfb.Name))
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)

		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(input.client.Delete(ctx, bfb)).To(Succeed())
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for BFB to be deleted")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      input.bfb.Name,
			Namespace: input.bfb.Namespace,
		}, bfb)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "BFB should be deleted")
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	if !skipDPUClusterDeletionInProvisioningTest {
		By(fmt.Sprintf("Deleting DPUCluster %s/%s", input.dpuClusters[0].Namespace, input.dpuClusters[0].Name))
		Eventually(func(g Gomega) {
			cluster := &provisioningv1.DPUCluster{}
			err := input.client.Get(ctx, types.NamespacedName{
				Name:      input.dpuClusters[0].Name,
				Namespace: input.dpuClusters[0].Namespace,
			}, cluster)

			if apierrors.IsNotFound(err) {
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(input.client.Delete(ctx, cluster)).To(Succeed())
		}).WithTimeout(1 * time.Minute).Should(Succeed())

		By("Waiting for DPUCluster to be deleted")
		clusterDeleteTracker := NewByTracker()
		Eventually(func(g Gomega) {
			clusters := &provisioningv1.DPUClusterList{}
			g.Expect(input.client.List(ctx, clusters)).To(Succeed())
			clusterDeleteTracker.By(fmt.Sprintf("%d", len(clusters.Items)),
				"DPUClusters remaining [%d]", len(clusters.Items))
			g.Expect(clusters.Items).To(BeEmpty(), "DPUCluster should be deleted")
		}).WithTimeout(15 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		// Delete prerequisite objects (TenantControlPlane, nodeport Service) only when deleting DPUCluster.
		// When DPUCluster deletion is skipped (RM 4869399), leaving these in place keeps the kubeconfig secret
		// so DPFOperatorConfig and DPUServices can complete teardown in AfterSuite.
		if len(input.dpuClusterPrerequisites) > 0 {
			By(fmt.Sprintf("Deleting %d prerequisite objects", len(input.dpuClusterPrerequisites)))
			Expect(testutils.CleanupAndWait(ctx, input.client, input.dpuClusterPrerequisites...)).To(Succeed())
		}
	} else {
		By("Skipping DPUCluster deletion (RM: 4869399 - DPUCluster/DPUService deletion race; cluster left for DPFOperatorConfig teardown)")
	}

	By("Verifying cleanup complete")
	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(BeEmpty(), "No DPUSets should remain")

		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(BeEmpty(), "No DPU objects should remain")

		bfbs := &provisioningv1.BFBList{}
		g.Expect(input.client.List(ctx, bfbs)).To(Succeed())
		g.Expect(bfbs.Items).To(BeEmpty(), "No BFBs should remain")

		if !skipDPUClusterDeletionInProvisioningTest {
			clusters := &provisioningv1.DPUClusterList{}
			g.Expect(input.client.List(ctx, clusters)).To(Succeed())
			g.Expect(clusters.Items).To(BeEmpty(), "No DPUClusters should remain")
		}

		flavors := &provisioningv1.DPUFlavorList{}
		g.Expect(input.client.List(ctx, flavors, client.InNamespace(input.dpuFlavor.Namespace))).To(Succeed())
		for _, flavor := range flavors.Items {
			g.Expect(flavor.Name).NotTo(Equal(input.dpuFlavor.Name),
				"DPUFlavor should be deleted")
		}
	}).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Deprovisioning completed successfully")
}
