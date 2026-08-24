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
	"github.com/nvidia/doca-platform/test/utils/prometheus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func VerifyHostKSMMetricsCollection(ctx context.Context) {
	By("Verify host cluster kube-state-metrics endpoint is accessible")
	Eventually(func(g Gomega) {
		request := hostClusterRESTClient.Get().AbsPath(metricsURI)
		response, err := request.DoRaw(ctx)
		g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURI, err))
		g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %s ", metricsURI))
	}).WithTimeout(30 * time.Second).Should(Succeed())
}

func VerifyDPUKSMMetricsCollection(ctx context.Context, input *systemTestInput) {
	By("Verify DPU cluster kube-state-metrics endpoint is accessible")
	Eventually(func(g Gomega) {
		// Get the KSM metrics URI for the first DPUCluster
		// Note: The in-cluster kube-state-metrics service runs on the management cluster,
		// not on the DPU cluster. It connects remotely to collect DPU cluster metrics.
		g.Expect(input.dpuClusters).ToNot(BeEmpty(), "No DPUClusters found in test input")
		dpuKSMMetricsURI, err := metrics.GetKSMMetricsURIForDPUCluster(ctx, input.client, input.dpuClusters[0], dpfOperatorSystemNamespace, kubeStateMetricsPort, "/metrics")
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
	skipMetricNamesInOCPReuse()

	By("Verify metrics are being collected")
	expectedMetricsNames := map[string][]string{
		"dpf_dpfoperatorconfig": {"created", "info", "status_conditions", "status_condition_last_transition_time", "version"}, // "paused" missed
		"dpf_dpucluster":        {"created", "info", "status_phase", "status_conditions", "status_condition_last_transition_time", "status_nodes_count"},
	}

	if input.bfb != nil {
		expectedMetricsNames["dpf_bfb"] = []string{"created", "info", "status_phase", "version_bsp", "version_doca", "version_uefi", "version_atf", "file_name"}
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

		expectedMetricsNames["dpf_dpu"] = []string{"created", "info", "required_reset", "status_phase", "status_conditions", "status_condition_last_transition_time", "operational_conditions", "operational_condition_last_transition_time", "agent_conditions", "agent_condition_last_transition_time", "outdated_timestamp", "outdated_reason"}
		expectedMetricsNames["dpf_dpuset"] = []string{"created", "info", "status_dpu_statistics", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpf_dpunode"] = []string{"created", "info", "reboot_in_progress", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpf_dpudevice"] = []string{"created", "info", "status_conditions", "status_condition_last_transition_time"}
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

// ValidateDPFMetricsScrapedByPrometheus confirms that DPF kube-state-metrics are
// not only produced but actually scraped into Prometheus, where the dashboards
// read them. ValidateGeneralDPFMetrics checks the KSM endpoint directly, which
// isolates producer correctness but does not exercise the Prometheus scrape
// config; this closes that gap. dpfoperatorconfig is a singleton, so
// dpf_dpfoperatorconfig_info is always present once the scrape has run.
func ValidateDPFMetricsScrapedByPrometheus(ctx context.Context) {
	// The pinned release operator also names this metric without the dpf_ prefix,
	// but the unreachable endpoint fails the query first.
	skipPrometheusInOCPReuse()

	By("Verify DPF metrics are scraped into Prometheus")
	promClient := prometheus.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	Eventually(func(g Gomega) {
		samples, err := promClient.QueryInstant(ctx, "dpf_dpfoperatorconfig_info")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(samples).NotTo(BeEmpty(),
			"No dpf_dpfoperatorconfig_info series in Prometheus; the kube-state-metrics scrape may be misconfigured")
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// ValidatePrometheusTargetsHealthy asserts that every scrape target known to
// Prometheus is currently up. It first checks that each DPU cluster has exactly
// the expected three control-plane scrape targets (apiserver, kube-controller-manager,
// kube-scheduler) and that each is up. This positive existence check catches the case
// where a missing credential Secret causes prometheus-operator to silently drop the
// entire ServiceMonitor — in that scenario no up{cluster=<name>} series exists at all,
// so the subsequent up==0 check would pass vacuously. It then queries `up == 0`
// (targets whose last scrape failed) and fails with a table of the offending
// job/instance pairs so that failures are immediately actionable without manual
// Prometheus inspection.
func ValidatePrometheusTargetsHealthy(ctx context.Context, input *systemTestInput) {
	skipPrometheusInOCPReuse()

	By("Verify all Prometheus scrape targets are healthy")
	promClient := prometheus.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	Eventually(func(g Gomega) {
		// Per DPU cluster: assert all three control-plane endpoints are present and up.
		for _, dpuCluster := range input.dpuClusters {
			if dpuCluster.Spec.Type != string(provisioningv1.KamajiCluster) {
				continue
			}

			clusterSamples, err := promClient.QueryInstant(ctx, fmt.Sprintf(`up{cluster=%q}`, dpuCluster.Name))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(clusterSamples).ToNot(HaveLen(3),
				fmt.Sprintf("DPU cluster %q: expected 3 control-plane targets (apiserver, kube-controller-manager, kube-scheduler), got %d; the ServiceMonitor or its credential Secret may be absent", dpuCluster.Name, len(clusterSamples)))
		}

		// We ignore the management cluster as it depends on how it was set up.
		// E.g. kube-proxy mostly listens on localhost and might not be reachable.
		// Also etcd or control-plane components might not be reachable for prometheus.
		samples, err := promClient.QueryInstant(ctx, "up{cluster!=\"management\"} == 0")
		g.Expect(err).NotTo(HaveOccurred())

		msg := fmt.Sprintf("%d Prometheus scrape target(s) are down:\n", len(samples))
		for _, s := range samples {
			msg += fmt.Sprintf("  up%s\n", s.String())
		}
		g.Expect(samples).To(BeEmpty(), msg)
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

func VerifyNodeProblemDetectorConditions(ctx context.Context, input *systemTestInput) {
	// The condition set is asserted exactly. The pinned release operator used in OCP
	// reuse mode reports DPUModeCorrect and SRIOVHealthy and omits PFRepresentorsHealthy.
	skipInOCPReuse("the pinned release operator reports a different node condition set")

	t := NewByTracker()
	Eventually(func(g Gomega) {
		for i, dpuCluster := range input.dpuClusters {
			t.By(dpuCluster.Name, "Checking node conditions in DPUCluster %s", dpuCluster.Name)

			nodes := &corev1.NodeList{}
			g.Expect(dpuClusterClient[i].List(ctx, nodes)).To(Succeed(),
				fmt.Sprintf("Failed to list nodes in DPUCluster %s", dpuCluster.Name))
			g.Expect(nodes.Items).ToNot(BeEmpty(),
				fmt.Sprintf("No nodes found in DPUCluster %s", dpuCluster.Name))

			for _, node := range nodes.Items {
				// Verify that the full set of Node conditions on DPUCluster nodes matches exactly
				// the expected NPD and kubelet condition types, and that each is in its happy-path
				// status (no problems detected, healthy services).
				//
				// This assertion is intentionally strict: it will fail if any condition type is
				// added, removed, or renamed (including non-NPD conditions). This guards against
				// unintended changes to the overall condition surface of these nodes.
				//
				// If new legitimate condition types are introduced, this list must be updated
				// accordingly.
				//
				// Keep in sync with GetNodeProblemDetectorConditions() in api/provisioning/v1alpha1/dpu_types.go:168
				g.Expect(node.Status.Conditions).
					To(ConsistOf(
						// All NPD conditions: False = no problem detected (happy path).
						// NPD permanent rules set status to True when a problem script fires;
						// the default (no problem) state is always False regardless of the
						// condition's name (including "Healthy"-named ones).
						And(HaveField("Type", Equal(provisioningv1.NPDConditionKernelDeadlock)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionReadonlyFilesystem)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionOVSvSwitchdHealthy)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionOVSDBHealthy)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionOVSHealthy)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionUplinkHealthy)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionPFRepresentorsHealthy)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(provisioningv1.NPDConditionMTUConfigured)), HaveField("Status", Equal(corev1.ConditionFalse))),
						// Kubelet conditions, not part of GetNodeProblemDetectorConditions()
						And(HaveField("Type", Equal(corev1.NodeReady)), HaveField("Status", Equal(corev1.ConditionTrue))),
						And(HaveField("Type", Equal(corev1.NodeMemoryPressure)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(corev1.NodeDiskPressure)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(corev1.NodePIDPressure)), HaveField("Status", Equal(corev1.ConditionFalse))),
						And(HaveField("Type", Equal(corev1.NodeNetworkUnavailable)), HaveField("Status", Equal(corev1.ConditionFalse))),
					), "Node conditions do not match expected conditions")
			}
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())
}
