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
)

func VerifyKSMMetricsCollection(ctx context.Context) {
	By("verify KMS metrics endpoint is accessible")
	Eventually(func(g Gomega) {
		request := hostClusterRESTClient.Get().AbsPath(metricsURI)
		response, err := request.DoRaw(ctx)
		g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURI, err))
		g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %s ", metricsURI))
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

		expectedMetricsNames["dpu"] = []string{"created", "info", "status_phase", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpuset"] = []string{"created", "info", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpunode"] = []string{"created", "info", "reboot_in_progress", "status_conditions", "status_condition_last_transition_time"}
		expectedMetricsNames["dpudevice"] = []string{"created", "info", "status_conditions", "status_condition_last_transition_time"}
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}
