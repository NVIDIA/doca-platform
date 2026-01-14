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

package e2e

import (
	"context"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ProvisionDPUDeploymentWithEachDPUJoiningADifferentDPUCluster creates a DPUDeployment where each DPU joins a different cluster
func ProvisionDPUDeploymentWithEachDPUJoiningADifferentDPUCluster(ctx context.Context, input *systemTestInput) {
	By("Getting DPUDevices")
	dpuDevices := &provisioningv1.DPUDeviceList{}
	Expect(input.client.List(ctx, dpuDevices)).To(Succeed())
	Expect(dpuDevices.Items).To(HaveLen(2))

	By("Creating DPUServiceNAD")
	nadName := "brsfc-no-ipam"
	dpuServiceNAD := constructDPUServiceNAD(nadName, "dpf-operator-system", 1500)
	dpuServiceNAD.Labels = utils.AfterAllCleanupLabels
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
					"kubernetes.io/hostname": dpuDevices.Items[i].Labels["provisioning.dpu.nvidia.com/dpunode-name"],
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

	By("verifying that the DPUDeployment is ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(45 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("Verifying DPUs joined the correct clusters")
	for i, dpuCluster := range input.dpuClusters {
		dpuDevice := &dpuDevices.Items[i]
		expectedHost := dpuDevice.Labels["provisioning.dpu.nvidia.com/dpunode-name"]
		By(fmt.Sprintf("Verifying DPU from DPUDevice %s (host: %s) joined DPUCluster %s", dpuDevice.Name, expectedHost, dpuCluster.Name))

		Eventually(func(g Gomega) {
			nodes := &corev1.NodeList{}
			g.Expect(dpuClusterClient[i].List(ctx, nodes)).To(Succeed())

			g.Expect(nodes.Items).To(HaveLen(1), fmt.Sprintf("DPUCluster %s should have exactly 1 node", dpuCluster.Name))

			node := nodes.Items[0]
			hostLabel, exists := node.Labels["provisioning.dpu.nvidia.com/host"]
			g.Expect(exists).To(BeTrue(), fmt.Sprintf("Node %s in DPUCluster %s must have label provisioning.dpu.nvidia.com/host", node.Name, dpuCluster.Name))
			g.Expect(hostLabel).To(Equal(expectedHost), fmt.Sprintf("Node %s in DPUCluster %s must have host label value %s", node.Name, dpuCluster.Name, expectedHost))
		}).WithTimeout(5 * time.Minute).Should(Succeed())
	}
}

// ValidateDPUServiceIPAMInL2ModeForMultiDPUCluster validates DPUService IPAM in L2 mode for multi-cluster setup
func ValidateDPUServiceIPAMInL2ModeForMultiDPUCluster(ctx context.Context, input *systemTestInput) {
	// TODO: Implement validation logic for L2 mode IPAM across multiple clusters
	Skip("ValidateDPUServiceIPAMInL2ModeForMultiDPUCluster not yet implemented")
}

// ValidateDPUServiceIPAMInL3ModeForMultiDPUCluster validates DPUService IPAM in L3 mode for multi-cluster setup
func ValidateDPUServiceIPAMInL3ModeForMultiDPUCluster(ctx context.Context, input *systemTestInput) {
	// TODO: Implement validation logic for L3 mode IPAM across multiple clusters
	Skip("ValidateDPUServiceIPAMInL3ModeForMultiDPUCluster not yet implemented")
}
