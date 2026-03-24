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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/metrics"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func VerifyHostKSMMetricsCollection(ctx context.Context) {
	By("verify host cluster kube-state-metrics endpoint is accessible")
	Eventually(func(g Gomega) {
		request := hostClusterRESTClient.Get().AbsPath(metricsURI)
		response, err := request.DoRaw(ctx)
		g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURI, err))
		g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %s ", metricsURI))
	}).WithTimeout(30 * time.Second).Should(Succeed())
}

func VerifyDPUKSMMetricsCollection(ctx context.Context, input *systemTestInput) {
	By("verify DPU cluster kube-state-metrics endpoint is accessible")
	Eventually(func(g Gomega) {
		// Get the KSM metrics URI for the first DPUCluster
		// Note: The in-cluster kube-state-metrics service runs on the management cluster,
		// not on the DPU cluster. It connects remotely to collect DPU cluster metrics.
		g.Expect(input.dpuClusters).ToNot(BeEmpty(), "No DPUClusters found in test input")
		dpuKSMMetricsURI, err := metrics.GetKSMMetricsURIForDPUCluster(ctx, input.client, input.dpuClusters[0], dpfOperatorSystemNamespace, 8080, "/metrics")
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get KSM metrics URI for DPUCluster")
		g.Expect(dpuKSMMetricsURI).NotTo(BeEmpty())

		// Use hostClusterRESTClient because the in-cluster KSM service runs on the management cluster
		request := hostClusterRESTClient.Get().AbsPath(dpuKSMMetricsURI)
		response, err := request.DoRaw(ctx)
		g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", dpuKSMMetricsURI, err))
		g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %s ", dpuKSMMetricsURI))
	}).WithTimeout(30 * time.Second).Should(Succeed())
}

func ValidateGeneralDPFMetrics(ctx context.Context, input *systemTestInput) {
	By("verify metrics are being collected")
	expectedMetricsNames := map[string][]string{
		"bfb":               {"created", "info", "status_phase"},
		"dpfoperatorconfig": {"created", "info", "status_conditions", "status_condition_last_transition_time"}, // "paused" missed
		"dpucluster":        {"created", "info", "status_phase", "status_conditions", "status_condition_last_transition_time"},
	}

	if input.hasDpuNodes() {
		By("Checking that DPUs are created")
		Eventually(func(g Gomega) {
			// A DPU object is created for each DPU device, not each DPU node.
			// totalDPUs() = numberOfDPUNodes * numberOfDPUsPerNode
			dpus := &provisioningv1.DPUList{}
			g.Expect(input.client.List(ctx, dpus)).To(Succeed())
			g.Expect(dpus.Items).To(HaveLen(input.totalDPUs()))
		}).WithTimeout(60 * time.Second).Should(Succeed())

		expectedMetricsNames["dpu"] = []string{"created", "info", "status_phase", "status_conditions", "status_condition_last_transition_time", "operational_conditions", "operational_condition_last_transition_time"}
		expectedMetricsNames["dpuset"] = []string{"created", "info", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpunode"] = []string{"created", "info", "reboot_in_progress", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpudevice"] = []string{"created", "info", "status_conditions", "status_condition_last_transition_time"}
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func VerifyNodeProblemDetectorConditions(ctx context.Context, input *systemTestInput) {
	Eventually(func(g Gomega) {
		for i, dpuCluster := range input.dpuClusters {
			By(fmt.Sprintf("Checking node conditions in DPUCluster %s", dpuCluster.Name))

			nodes := &corev1.NodeList{}
			g.Expect(dpuClusterClient[i].List(ctx, nodes)).To(Succeed(),
				fmt.Sprintf("Failed to list nodes in DPUCluster %s", dpuCluster.Name))
			g.Expect(nodes.Items).ToNot(BeEmpty(),
				fmt.Sprintf("No nodes found in DPUCluster %s", dpuCluster.Name))

			for _, node := range nodes.Items {
				nodeConditions := make(map[string]corev1.ConditionStatus)
				for _, condition := range node.Status.Conditions {
					nodeConditions[string(condition.Type)] = condition.Status
				}

				// Verify that the full set of Node conditions on DPUCluster nodes matches exactly
				// the expected NPD and kubelet condition types.
				//
				// This assertion is intentionally strict: it will fail if any condition type is
				// added, removed, or renamed (including non-NPD conditions). This guards against
				// unintended changes to the overall condition surface of these nodes.
				//
				// If new legitimate condition types are introduced, this list must be updated
				// accordingly.
				//
				// Keep in sync with GetNodeProblemDetectorConditions() in api/provisioning/v1alpha1/dpu_types.go:168
				Expect(node.Status.Conditions).
					To(ConsistOf(
						HaveField("Type", Equal(provisioningv1.NPDConditionKernelDeadlock)),
						HaveField("Type", Equal(provisioningv1.NPDConditionReadonlyFilesystem)),
						HaveField("Type", Equal(provisioningv1.NPDConditionOVSvSwitchdHealthy)),
						HaveField("Type", Equal(provisioningv1.NPDConditionOVSDBHealthy)),
						HaveField("Type", Equal(provisioningv1.NPDConditionOVSHealthy)),
						HaveField("Type", Equal(provisioningv1.NPDConditionDPUModeCorrect)),
						HaveField("Type", Equal(provisioningv1.NPDConditionUplinkHealthy)),
						HaveField("Type", Equal(provisioningv1.NPDConditionSRIOVHealthy)),
						HaveField("Type", Equal(provisioningv1.NPDConditionMTUConfigured)),
						// Kubelet conditions, not part of GetNodeProblemDetectorConditions()
						HaveField("Type", Equal(corev1.NodeReady)),
						HaveField("Type", Equal(corev1.NodeMemoryPressure)),
						HaveField("Type", Equal(corev1.NodeDiskPressure)),
						HaveField("Type", Equal(corev1.NodePIDPressure)),
						HaveField("Type", Equal(corev1.NodeNetworkUnavailable)),
					), "Node conditions do not match expected conditions")
			}
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())
}
