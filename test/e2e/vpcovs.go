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
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ovsVPCPodsToVerify is used by OVS VPC tests to wait for VPC OVS workloads on the DPU cluster (pod names contain these substrings).
var ovsVPCPodsToVerify = []string{
	"vpc-ovs-dhcp-agent",
	"vpc-ovs-flow-controller",
}

// getProvisionDPUClustersInputForOVSVPC returns provision input for OVS VPC tests..gitlab/ci/e2e-jobs.yml
// When DPU_CLUSTER_NAME and DPU_CLUSTER_NAMESPACE are set, uses that cluster instead of
// config-loaded clusters (e.g. for environments where the DPUCluster is created externally
// with different name/namespace, such as dpu-cplane-tenant1/dpu-cplane-tenant1).
// When the config-loaded cluster does not exist (e.g. Kamaji-created cluster with different name),
// discovers a Ready DPUCluster with kubeconfig from the management cluster.
func getProvisionDPUClustersInputForOVSVPC(ctx context.Context, provisionInput ProvisionDPUClustersInput, cl client.Client) ProvisionDPUClustersInput {
	if dpuClusterName != "" && dpuClusterNamespace != "" {
		name, ns := dpuClusterName, dpuClusterNamespace
		dc := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
		if err := cl.Get(ctx, client.ObjectKeyFromObject(dc), dc); err == nil {
			provisionInput.dpuClusters = []*provisioningv1.DPUCluster{dc}
			return provisionInput
		}
	}
	if len(provisionInput.dpuClusters) > 0 {
		key := client.ObjectKeyFromObject(provisionInput.dpuClusters[0])
		if err := cl.Get(ctx, key, provisionInput.dpuClusters[0]); err == nil {
			return provisionInput
		}
	}
	// No readiness filter needed here — getDPUClusterClients waits for the
	// DPUCluster to be ready and have a kubeconfig before creating the client.
	list := &provisioningv1.DPUClusterList{}
	Expect(cl.List(ctx, list)).To(Succeed())
	if len(list.Items) > 0 {
		provisionInput.dpuClusters = []*provisioningv1.DPUCluster{&list.Items[0]}
	}
	return provisionInput
}

func getReadyFlowControllerPods(ctx context.Context, dpuClient client.Client) []*corev1.Pod {
	pods := &corev1.PodList{}
	Expect(dpuClient.List(ctx, pods, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	var flowControllerPods []*corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		if strings.Contains(p.Name, "vpc-ovs-flow-controller") && netshoot.IsPodRunningAndReady(p) {
			flowControllerPods = append(flowControllerPods, p)
		}
	}
	return flowControllerPods
}

// verifyOVSCommands verifies that OVS commands can be executed on every flow-controller pod (each DPU node).
func verifyOVSCommands(pods []*corev1.Pod) {
	By("Executing OVS commands in vpc-ovs-flow-controller pods")
	Eventually(func(g Gomega) {
		for _, pod := range pods {
			By(fmt.Sprintf("Executing ovs-vsctl show in pod %s (node %s)", pod.Name, pod.Spec.NodeName))
			output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"ovs-vsctl", "show"})
			g.Expect(err).ToNot(HaveOccurred(), "ovs-vsctl show should succeed on pod %s", pod.Name)
			g.Expect(output).ToNot(BeEmpty(), "ovs-vsctl show should return output on pod %s", pod.Name)

			By(fmt.Sprintf("Executing ovs-vsctl list-br in pod %s (node %s)", pod.Name, pod.Spec.NodeName))
			output, err = netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"ovs-vsctl", "list-br"})
			g.Expect(err).ToNot(HaveOccurred(), "ovs-vsctl list-br should succeed on pod %s", pod.Name)
			By(fmt.Sprintf("Found OVS bridges on %s: %s", pod.Spec.NodeName, strings.TrimSpace(output)))
		}
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// verifyGRPCListVirtualNetworks verifies that the gRPC ListVirtualNetworks API is accessible on every flow-controller pod (each DPU node).
func verifyGRPCListVirtualNetworks(pods []*corev1.Pod) {
	By("Verifying gRPC ListVirtualNetworks on vpc-ovs-flow-controller pods")
	Eventually(func(g Gomega) {
		command := []string{"/vpcctl", "list-vnet"}
		for _, pod := range pods {
			By(fmt.Sprintf("Executing vpcctl list-vnet on pod %s (node %s)", pod.Name, pod.Spec.NodeName))
			output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, command)
			g.Expect(err).ToNot(HaveOccurred(), "vpcctl list-vnet should succeed on pod %s", pod.Name)
			g.Expect(output).To(Or(
				ContainSubstring("virtualNetworks"),
				ContainSubstring("{}"),
				ContainSubstring("[]"),
			), "vpcctl response on pod %s should contain valid JSON structure", pod.Name)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}
