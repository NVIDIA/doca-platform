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
	"maps"
	"time"

	"github.com/nvidia/doca-platform/test/utils/metrics"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func VerifyKSMMetricsCollection(ctx context.Context, input systemTestInput) {
	It("validate DPF metrics services are accessible", func() {
		if !deployKSM {
			Skip("Skip KSM metrics accessibility test due to KSM is not deployed")
		}

		By("verify KMS metrics endpoint is accessible")
		Eventually(func(g Gomega) {
			request := testRESTClient.Get().AbsPath(metricsURI)
			response, err := request.DoRaw(ctx)
			g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURI, err))
			g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %s ", metricsURI))
		}).WithTimeout(30 * time.Second).Should(Succeed())
	})
}

func ValidateGeneralDPFMetrics(ctx context.Context, input systemTestInput) {
	testClient := input.client
	hostPrometheusName := "prometheus"
	It("validate DPF metrics services are accessible", func() {
		if !deployPrometheus {
			Skip("Skip prometheus metrics tests")
		}
		By("verify prometheus is running")
		Eventually(func(g Gomega) {
			prometheusPods := &corev1.PodList{}
			g.Expect(testClient.List(ctx, prometheusPods, client.MatchingLabels{"app.kubernetes.io/name": hostPrometheusName})).To(Succeed())
			g.Expect(prometheusPods.Items).NotTo(BeEmpty(), fmt.Sprintf("Expected number of Prometheus pods %d >= %d", len(prometheusPods.Items), 0))
			for _, pod := range prometheusPods.Items {
				g.Expect(string(pod.Status.Phase)).To(Equal("Running"), "Pod %s status is %s", pod.Name, pod.Status.Phase)
			}
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})

	It("validate DPF metric on Prometheus", func() {
		if !deployPrometheus {
			Skip("Skip prometheus metrics tests due to Prometheus is not deployed")
		}
		By("verify metrics are being collected")
		expectedMetricsNames := map[string][]string{
			"bfb":               {"created", "info", "status_phase"},
			"dpfoperatorconfig": {"created", "info", "status_conditions", "status_condition_last_transition_time"}, // "paused" missed
			"dpucluster":        {"created", "info", "status_phase", "status_conditions", "status_condition_last_transition_time"},
			//"dpu":               {"created", "info", "status_phase", "status_conditions", "status_condition_last_transition_time"}, // all missed
		}

		By("verify Prometheus proxy is accessible")
		Eventually(func(g Gomega) {
			// Prepare metrics request URL and query
			query := "/api/v1/query"
			metricsURL := metrics.GetMetricsURI("dpf-operator-prometheus-server", dpfOperatorSystemNamespace, 80, query)
			request := testRESTClient.Get().AbsPath(metricsURL).Param("query", `{__name__=~"dpf.*"}`)
			// Request metrics from prometheus
			response, err := request.DoRaw(ctx)
			g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %v failed with err: %v", request, err))
			g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %v ", request))
		}).WithTimeout(20 * time.Second).Should(Succeed())

		By("verify metrics keys Prometheus")
		Eventually(func(g Gomega) {
			// No checks for bfb metrics if the numberOfDPUNodes is 0
			if input.numberOfDPUNodes == 0 {
				delete(expectedMetricsNames, "bfb")
			}
			actualMetricsNames := metrics.GetPrometheusMetrics(ctx, testRESTClient, g, maps.Keys(expectedMetricsNames), dpfOperatorSystemNamespace)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(20 * time.Second).Should(Succeed())
	})
}
