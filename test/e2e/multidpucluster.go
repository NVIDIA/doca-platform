/*
Copyright 2026 NVIDIA

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

//nolint:goconst,dupl
package e2e

import (
	"context"
	"fmt"
	"slices"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ProvisionDPUDeploymentWithEachDPUJoiningADifferentDPUCluster creates a DPUDeployment where each DPU joins a different cluster
func ProvisionDPUDeploymentWithEachDPUJoiningADifferentDPUCluster(ctx context.Context, input *systemTestInput) {
	expectedTotalDPUs := input.totalDPUs()

	By("Verifying preconditions: number of clusters equals total DPUs")
	Expect(input.dpuClusters).To(HaveLen(expectedTotalDPUs),
		fmt.Sprintf("This test requires one DPUCluster per DPU. Expected %d clusters for %d nodes * %d DPUs/node",
			expectedTotalDPUs, input.numberOfDPUNodes, input.numberOfDPUsPerNode))

	By("Getting DPUDevices")
	dpuDevices := &provisioningv1.DPUDeviceList{}
	Expect(input.client.List(ctx, dpuDevices)).To(Succeed())
	Expect(dpuDevices.Items).To(HaveLen(expectedTotalDPUs),
		fmt.Sprintf("Expected %d DPUDevices (%d nodes * %d DPUs/node)",
			expectedTotalDPUs, input.numberOfDPUNodes, input.numberOfDPUsPerNode))

	By("Creating DPUServiceNAD")
	nadName := "brsfc-no-ipam"
	dpuServiceNAD := constructDPUServiceNAD(nadName, "dpf-operator-system", 1500)
	dpuServiceNAD.Labels = CleanupScope.Suite
	Expect(input.client.Create(ctx, dpuServiceNAD)).To(Succeed())

	By("Creating DPUServiceTemplate")
	dpuServiceTemplate := generateDPUServiceTemplate(input, "")
	useDummyDPUServiceChart(dpuServiceTemplate)
	Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())

	By("Creating DPUServiceConfiguration")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "net1", Network: nadName}}
	Expect(input.client.Create(ctx, dpuServiceConfiguration)).To(Succeed())

	By("Creating DPUDeployment with each DPU joining a different cluster")
	dpuDeployment := generateDPUDeployment(input, "")

	// Create a DPUSet for each DPU joining a different DPUCluster
	dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{}
	for i, dpuCluster := range input.dpuClusters {
		dpuSet := dpuservicev1.DPUSet{
			NameSuffix: fmt.Sprintf("cluster-%d", i),
			DPUClusterSelector: map[string]string{
				"svc.dpu.nvidia.com/cluster": dpuCluster.Name,
			},
			//nolint:staticcheck // Using deprecated field until it's removed
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/hostname": dpuDevices.Items[i].Labels[provisioningv1.DPUNodeNameLabel],
				},
			},
		}
		dpuDeployment.Spec.DPUs.DPUSets = append(dpuDeployment.Spec.DPUs.DPUSets, dpuSet)
	}

	// Configure service chains without IPAM. IPAM will be used in the subsequent tests.
	dpuDeployment.Spec.ServiceChains.Switches = []dpuservicev1.DPUDeploymentSwitch{
		{
			Ports: []dpuservicev1.DPUDeploymentPort{
				{
					Service: &dpuservicev1.DPUDeploymentService{
						InterfaceName: "net1",
						Name:          dpuServiceTemplate.Spec.DeploymentServiceName,
					},
				},
			},
		},
	}

	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	By("Waiting for DPUDeployment underlying objects to be created")
	Eventually(func(g Gomega) {
		g.Expect(VerifyDeploymentUnderlyingObjectsCreated(ctx, g, input.client, dpuDeployment)).To(BeTrue())
	}).WithTimeout(180 * time.Second).Should(Succeed())

	By("Verifying that the DPUDeployment is ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(dpuDeploymentReadyTimeout).WithPolling(1 * time.Second).Should(Succeed())

	By("Verifying DPUs joined the correct clusters")
	for i, dpuCluster := range input.dpuClusters {
		dpuDevice := &dpuDevices.Items[i]
		expectedHost := dpuDevice.Labels[provisioningv1.DPUNodeNameLabel]
		By(fmt.Sprintf("Verifying DPU from DPUDevice %s (host: %s) joined DPUCluster %s", dpuDevice.Name, expectedHost, dpuCluster.Name))

		Eventually(func(g Gomega) {
			nodes := &corev1.NodeList{}
			g.Expect(dpuClusterClient[i].List(ctx, nodes)).To(Succeed())

			g.Expect(nodes.Items).To(HaveLen(1), fmt.Sprintf("DPUCluster %s should have exactly 1 node", dpuCluster.Name))

			node := nodes.Items[0]
			dpuNodeLabel, exists := node.Labels[provisioningv1.DPUNodeNameLabel]
			g.Expect(exists).To(BeTrue(), fmt.Sprintf("Node %s in DPUCluster %s must have label %s", node.Name, dpuCluster.Name, provisioningv1.DPUNodeNameLabel))
			g.Expect(dpuNodeLabel).To(Equal(expectedHost), fmt.Sprintf("Node %s in DPUCluster %s must have host label value %s", node.Name, dpuCluster.Name, expectedHost))
		}).WithTimeout(5 * time.Minute).Should(Succeed())
	}
}

// ValidateDPUServiceIPAMInL2ModePerDPUCluster validates per-DPUCluster DPUServiceIPAM configuration in L2 mode.
// This covers the advanced use case where each DPUCluster requires its own DPUServiceIPAM object (via DPUClusterSelector),
// where the user splits the CIDR on their own per DPUCluster.
func ValidateDPUServiceIPAMInL2ModePerDPUCluster(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l2-pool",
	}
	dpuServiceIPAMTemplate := input.ipPoolDPUServiceIPAM.DeepCopy()
	dpuServiceIPAMTemplate.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAMTemplate.Labels = CleanupScope.Suite
	dpuServiceIPAMTemplate.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAMTemplate.Spec.NodeSelector = nil
	By("Creating DPUServiceIPAM for the first cluster")
	dpuServiceIPAM1 := dpuServiceIPAMTemplate.DeepCopy()
	dpuServiceIPAM1.SetName("l2-ipam-cluster-1")
	dpuServiceIPAM1.Spec.IPV4Subnet = &dpuservicev1.Subnet{
		Subnet:         "192.168.10.1/28",
		Gateway:        "192.168.10.1",
		PerNodeIPCount: 6,
		ExcludeRanges: []dpuservicev1.IPRange{
			{
				StartIP: "192.168.10.7",
				EndIP:   "192.168.10.15",
			},
		},
	}
	dpuServiceIPAM1.Spec.DPUClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"svc.dpu.nvidia.com/cluster": input.dpuClusters[0].Name,
		},
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM1)).To(Succeed())

	By("Waiting for DPUServiceIPAM for first cluster to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM1, 5*time.Minute)

	By("Creating DPUServiceIPAM for second cluster")
	dpuServiceIPAM2 := dpuServiceIPAMTemplate.DeepCopy()
	dpuServiceIPAM2.SetName("l2-ipam-cluster-2")
	dpuServiceIPAM2.Spec.IPV4Subnet = &dpuservicev1.Subnet{
		Subnet:         "192.168.10.1/28",
		Gateway:        "192.168.10.1",
		PerNodeIPCount: 6,
		ExcludeRanges: []dpuservicev1.IPRange{
			{
				StartIP: "192.168.10.1",
				EndIP:   "192.168.10.6",
			},
		},
	}
	dpuServiceIPAM2.Spec.DPUClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"svc.dpu.nvidia.com/cluster": input.dpuClusters[1].Name,
		},
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM2)).To(Succeed())

	By("Waiting for DPUServiceIPAM for second cluster to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM2, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	// Update the service port to include IPAM
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.10.2", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.10.7", 2)
}

// ValidateDPUServiceIPAMInL3ModePerDPUCluster validates per-DPUCluster DPUServiceIPAM configuration in L3 mode.
// This covers the advanced use case where each DPUCluster requires its own DPUServiceIPAM object (via DPUClusterSelector),
// where the user splits the CIDR on their own per DPUCluster.
func ValidateDPUServiceIPAMInL3ModePerDPUCluster(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l3-pool",
	}
	dpuServiceIPAMTemplate := input.cidrDPUServiceIPAM.DeepCopy()
	dpuServiceIPAMTemplate.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAMTemplate.Labels = CleanupScope.Suite
	dpuServiceIPAMTemplate.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAMTemplate.Spec.NodeSelector = nil

	By("Creating DPUServiceIPAM for the first cluster")
	dpuServiceIPAM1 := dpuServiceIPAMTemplate.DeepCopy()
	dpuServiceIPAM1.SetName("l3-ipam-cluster-1")
	dpuServiceIPAM1.Spec.IPV4Network = &dpuservicev1.Network{
		Network:      "192.168.20.0/28",
		GatewayIndex: ptr.To[int32](1),
		PrefixSize:   30,
		ExcludeRanges: []dpuservicev1.IPRange{
			{
				StartIP: "192.168.20.8",
				EndIP:   "192.168.20.15",
			},
		},
	}
	dpuServiceIPAM1.Spec.DPUClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"svc.dpu.nvidia.com/cluster": input.dpuClusters[0].Name,
		},
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM1)).To(Succeed())

	By("Waiting for DPUServiceIPAM for first cluster to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM1, 5*time.Minute)

	By("Creating DPUServiceIPAM for second cluster")
	dpuServiceIPAM2 := dpuServiceIPAMTemplate.DeepCopy()
	dpuServiceIPAM2.SetName("l3-ipam-cluster-2")
	dpuServiceIPAM2.Spec.IPV4Network = &dpuservicev1.Network{
		Network:      "192.168.20.0/28",
		GatewayIndex: ptr.To[int32](1),
		PrefixSize:   30,
		ExcludeRanges: []dpuservicev1.IPRange{
			{
				StartIP: "192.168.20.0",
				EndIP:   "192.168.20.7",
			},
		},
	}
	dpuServiceIPAM2.Spec.DPUClusterSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"svc.dpu.nvidia.com/cluster": input.dpuClusters[1].Name,
		},
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM2)).To(Succeed())

	By("Waiting for DPUServiceIPAM for second cluster to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM2, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	// Update the service port to include IPAM
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.20.2", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.20.10", 2)
}

// ValidateDPUServiceIPAMInL2ModeSharedAcrossDPUClusters validates a single DPUServiceIPAM object in L2 mode that spans
// all DPUClusters without a DPUClusterSelector. This is the standard multi-DPUCluster use case where the controller
// distributes IP allocations from a shared pool across all clusters automatically.
func ValidateDPUServiceIPAMInL2ModeSharedAcrossDPUClusters(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l2-shared-pool",
	}

	By("Creating a single DPUServiceIPAM spanning all clusters")
	dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetName("l2-ipam-shared")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAM.Labels = CleanupScope.Suite
	dpuServiceIPAM.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAM.Spec.NodeSelector = nil
	dpuServiceIPAM.Spec.DPUClusterSelector = nil
	dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.Subnet{
		Subnet:              "192.168.50.1/27",
		Gateway:             "192.168.50.1",
		PerNodeIPCount:      6,
		BlocksPerDPUCluster: ptr.To[int32](2),
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Waiting for DPUServiceIPAM to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	// .2 because we don't explicitly request the gateway
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.50.2", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.50.13", 2)
}

// ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClusters validates a single DPUServiceIPAM object in L3 mode that spans
// all DPUClusters without a DPUClusterSelector. This is the standard multi-DPUCluster use case where the controller
// distributes IP prefix allocations from a shared network across all clusters automatically.
func ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClusters(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l3-shared-pool",
	}

	By("Creating a single DPUServiceIPAM spanning all clusters")
	dpuServiceIPAM := input.cidrDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetName("l3-ipam-shared")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAM.Labels = CleanupScope.Suite
	dpuServiceIPAM.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAM.Spec.NodeSelector = nil
	dpuServiceIPAM.Spec.DPUClusterSelector = nil
	dpuServiceIPAM.Spec.IPV4Network = &dpuservicev1.Network{
		Network:              "192.168.60.0/27",
		GatewayIndex:         ptr.To[int32](1),
		PrefixSize:           30,
		SubnetsPerDPUCluster: ptr.To[int32](2),
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Waiting for DPUServiceIPAM to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.60.2", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.60.10", 2)
}

// ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClustersWithStaticAllocations validates a single DPUServiceIPAM in L3 mode
// that uses static allocations to explicitly pin each node across all DPUClusters to a specific IP prefix. This is the
// standard multi-DPUCluster use case for static allocations where one DPUServiceIPAM covers all clusters.
func ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClustersWithStaticAllocations(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l3-static-shared-pool",
	}

	By("Getting node names from each DPU cluster")
	nodeNames := make([]string, len(dpuClusterClient))
	for i, clusterClient := range dpuClusterClient {
		nodes := &corev1.NodeList{}
		Expect(clusterClient.List(ctx, nodes)).To(Succeed())
		Expect(nodes.Items).To(HaveLen(1))
		nodeNames[i] = nodes.Items[0].Name
	}

	By("Creating a single DPUServiceIPAM with static allocations spanning all clusters")
	dpuServiceIPAM := input.cidrDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetName("l3-ipam-static-shared")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAM.Labels = CleanupScope.Suite
	dpuServiceIPAM.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAM.Spec.NodeSelector = nil
	dpuServiceIPAM.Spec.DPUClusterSelector = nil
	dpuServiceIPAM.Spec.IPV4Network = &dpuservicev1.Network{
		Network:              "192.168.70.0/28",
		GatewayIndex:         ptr.To[int32](1),
		PrefixSize:           30,
		SubnetsPerDPUCluster: ptr.To[int32](2),
		// We know that the first node joins the first cluster that is supposed to distribute /30 subnets derived from
		// 192.168.70.0/29, while the other should distribute from 192.168.70.8/29. We explicitly request a subnet
		// outside of these ranges to showcase that allocations will work no matter which ranges are allocated per
		// DPUCluster.
		Allocations: map[string]string{
			nodeNames[0]: "192.168.70.8/30",
			nodeNames[1]: "192.168.70.4/30",
		},
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Waiting for DPUServiceIPAM to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.70.10", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.70.6", 2)
}

// ValidateDPUServiceIPAMInL2ModeSharedAcrossDPUClustersWithSingleIPPerNode validates a single DPUServiceIPAM in L2 mode
// spanning all DPUClusters where each node receives exactly one IP (PerNodeIPCount: 1).
func ValidateDPUServiceIPAMInL2ModeSharedAcrossDPUClustersWithSingleIPPerNode(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l2-single-ip-pool",
	}

	By("Creating a single DPUServiceIPAM with one IP per node spanning all clusters")
	dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetName("l2-ipam-single-ip")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAM.Labels = CleanupScope.Suite
	dpuServiceIPAM.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAM.Spec.NodeSelector = nil
	dpuServiceIPAM.Spec.DPUClusterSelector = nil
	dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.Subnet{
		Subnet:              "192.168.100.1/29",
		Gateway:             "192.168.100.1",
		PerNodeIPCount:      1,
		BlocksPerDPUCluster: ptr.To[int32](2),
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Waiting for DPUServiceIPAM to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	// .2 because we don't explicitly request the gateway
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.100.2", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.100.3", 2)
}

// ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClustersWithSingleIPPerNode validates a single DPUServiceIPAM in L3 mode
// spanning all DPUClusters where each node receives a /32 prefix (one IP address).
func ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClustersWithSingleIPPerNode(ctx context.Context, input *systemTestInput) {
	By("Getting existing DPUServiceConfiguration and updating it to use br-sfc network with IPAM requirement")
	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	dpuServiceConfigurationOriginal := dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.Spec.Interfaces[0].Network = "mybrsfc"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(dpuServiceConfigurationOriginal))).To(Succeed())

	poolLabel := map[string]string{
		"svc.dpu.nvidia.com/pool": "l3-single-ip-pool",
	}

	By("Creating a single DPUServiceIPAM with /32 prefix per node spanning all clusters")
	dpuServiceIPAM := input.cidrDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetName("l3-ipam-single-ip")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAM.Labels = CleanupScope.Suite
	dpuServiceIPAM.Spec.ObjectMeta.Labels = poolLabel
	dpuServiceIPAM.Spec.NodeSelector = nil
	dpuServiceIPAM.Spec.DPUClusterSelector = nil
	dpuServiceIPAM.Spec.IPV4Network = &dpuservicev1.Network{
		Network:              "192.168.110.0/27",
		PrefixSize:           32,
		SubnetsPerDPUCluster: ptr.To[int32](2),
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Waiting for DPUServiceIPAM to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM, 5*time.Minute)

	By("Getting existing DPUDeployment and updating its ServiceChains to use DPUServiceIPAM")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.IPAM = &dpuservicev1.IPAM{MatchLabels: poolLabel}
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for DPUDeployment to become ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuDeployment, 15*time.Minute)

	By("Getting the ServiceID for example service from the DPUService")
	serviceIDForExample := GetServiceIDForDPUDeploymentService(ctx, input.client, dpuDeployment, "example")

	By("Validating DPUService Pod in first cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[0], serviceIDForExample, "192.168.110.0", 1)

	By("Validating DPUService Pod in second cluster has secondary IP from correct subnet")
	validateDPUServicePodIPInCluster(ctx, dpuClusterClient[1], serviceIDForExample, "192.168.110.2", 2)
}

// ValidateDPUClusterDeletion validates the system when first DPUCluster is deleted.
// It uses the existing DPUDeployment (with each DPU joining a different cluster) and verifies that after cluster 1 is
// deleted the system remains healthy: DPFOperatorConfig, all DPUServices, DPUServiceChains, DPUServiceInterfaces,
// DPUServiceIPAMs, and the DPUDeployment are ready.
func ValidateDPUClusterDeletion(ctx context.Context, input *systemTestInput) {
	firstDPUCluster := input.dpuClusters[0]

	By("Deleting first DPUCluster")
	Expect(input.client.Delete(ctx, firstDPUCluster)).To(Succeed())

	By("Patching DPUDeployment to remove the DPUSet referencing first DPUCluster")
	dpuDeployment := generateDPUDeployment(input, "")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
	dpuDeploymentOriginal := dpuDeployment.DeepCopy()
	dpuDeployment.Spec.DPUs.DPUSets = slices.DeleteFunc(dpuDeployment.Spec.DPUs.DPUSets, func(s dpuservicev1.DPUSet) bool {
		return s.DPUClusterSelector["svc.dpu.nvidia.com/cluster"] == firstDPUCluster.Name
	})
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(dpuDeploymentOriginal))).To(Succeed())

	By("Waiting for first DPUCluster to be completely deleted")
	Eventually(func(g Gomega) {
		err := input.client.Get(ctx, client.ObjectKeyFromObject(firstDPUCluster), &provisioningv1.DPUCluster{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "DPUCluster %s should be completely deleted", client.ObjectKeyFromObject(firstDPUCluster))
	}).WithTimeout(15 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("Verifying DPFOperatorConfig is ready")
	VerifyDPFOperatorConfigReady(ctx, input.client, 10*time.Minute)

	By("Verifying all DPUServices are ready")
	Eventually(func(g Gomega) {
		dpuServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(input.client.List(ctx, dpuServiceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		for _, dpuService := range dpuServiceList.Items {
			g.Expect(conditions.IsTrue(&dpuService, conditions.TypeReady)).To(BeTrue(),
				fmt.Sprintf("DPUService %s should be ready", dpuService.Name))
		}
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("Verifying all DPUServiceChains are ready")
	Eventually(func(g Gomega) {
		dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
		g.Expect(input.client.List(ctx, dpuServiceChainList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		for _, dpuServiceChain := range dpuServiceChainList.Items {
			g.Expect(conditions.IsTrue(&dpuServiceChain, conditions.TypeReady)).To(BeTrue(),
				fmt.Sprintf("DPUServiceChain %s should be ready", dpuServiceChain.Name))
		}
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("Verifying all DPUServiceInterfaces are ready")
	Eventually(func(g Gomega) {
		dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
		g.Expect(input.client.List(ctx, dpuServiceInterfaceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		for _, dpuServiceInterface := range dpuServiceInterfaceList.Items {
			g.Expect(conditions.IsTrue(&dpuServiceInterface, conditions.TypeReady)).To(BeTrue(),
				fmt.Sprintf("DPUServiceInterface %s should be ready", dpuServiceInterface.Name))
		}
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("Verifying all DPUServiceIPAMs are ready")
	Eventually(func(g Gomega) {
		dpuServiceIPAMList := &dpuservicev1.DPUServiceIPAMList{}
		g.Expect(input.client.List(ctx, dpuServiceIPAMList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		for _, dpuServiceIPAM := range dpuServiceIPAMList.Items {
			g.Expect(conditions.IsTrue(&dpuServiceIPAM, conditions.TypeReady)).To(BeTrue(),
				fmt.Sprintf("DPUServiceIPAM %s should be ready", dpuServiceIPAM.Name))
		}
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("Verifying DPUDeployment is ready")
	Eventually(func(g Gomega) {
		got := &dpuservicev1.DPUDeployment{}
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), got)).To(Succeed())
		g.Expect(conditions.IsTrue(got, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

// validateDPUServicePodIPInCluster asserts that the single DPUService pod selected by serviceID in the given cluster
// has the expected secondary IP on interface net1.
func validateDPUServicePodIPInCluster(ctx context.Context, clusterClient client.Client, serviceID, expectedIP string, clusterNum int) {
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(clusterClient.List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceID},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).ToNot(BeEmpty())
		g.Expect(podList.Items).To(HaveLen(1))

		pod := podList.Items[0]
		ipStr := getPodIPForInterface(g, pod, "net1")
		// Note that if the pod is restarted for whatever reason, NVIPAM will allocate the next IP in the block and this
		// check will fail. This indicates that another issue occurs and should be checked why this happened to identify
		// potential issues on other components. If this fails a lot, we can relax the check to check that the IP is part
		// of the block we expect it to be.
		g.Expect(ipStr).To(Equal(expectedIP),
			fmt.Sprintf("Pod %s in cluster %d should have IP %s", pod.Name, clusterNum, expectedIP))
	}).WithTimeout(30 * time.Second).Should(Succeed())
}
