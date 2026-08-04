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

	"github.com/nvidia/doca-platform/test/utils/loki"
	"github.com/nvidia/doca-platform/test/utils/prometheus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// otelNodePort is the NodePort used for OpenTelemetry Collector endpoint as configured in otel Helm Chart.
const (
	otelNodePort       = 30050
	otelEndpointSchema = "http://"
)

// ValidateDPUClusterOpenTelemetryConfiguration verifies DPU cluster collector configuration
func ValidateDPUClusterOpenTelemetryConfiguration(ctx context.Context, input *systemTestInput) {
	for i, dpuClient := range dpuClusterClient {
		clusterName := input.dpuClusters[i].Name
		By(fmt.Sprintf("Checking OpenTelemetry Collector ConfigMap in DPU cluster %s", clusterName))

		cm := &corev1.ConfigMap{}
		Eventually(func(g Gomega) {
			err := dpuClient.Get(ctx, client.ObjectKey{
				Namespace: dpfOperatorSystemNamespace,
				Name:      clusterName + "-opentelemetry-collector-config",
			}, cm)
			g.Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap contains OTLP exporter configuration
			c, exists := cm.Data["otel-collector-config.yaml"]
			g.Expect(exists).To(BeTrue(), "otel-collector-config.yaml not found in ConfigMap")
			g.Expect(c).To(ContainSubstring(fmt.Sprintf(":%d", otelNodePort)),
				fmt.Sprintf("ConfigMap should contain management endpoint with NodePort %d", otelNodePort))
			g.Expect(c).To(ContainSubstring("otlphttp/log:"),
				"ConfigMap should contain OTLP HTTP exporter for logging")
			// Verify DOCA log receiver configuration
			g.Expect(c).To(ContainSubstring("filelog/doca"),
				"ConfigMap should contain filelog/doca receiver")
			g.Expect(c).To(ContainSubstring("/var/log/doca"),
				"ConfigMap should contain /var/log/doca path")
			g.Expect(c).To(ContainSubstring("**/*.log"),
				"ConfigMap should contain log file pattern")
			// Verify metrics receiver and exporter configuration
			g.Expect(c).To(ContainSubstring("kubeletstats:"),
				"ConfigMap should contain kubeletstats receiver")
			g.Expect(c).To(ContainSubstring("otlphttp/metrics:"),
				"ConfigMap should contain OTLP HTTP exporter for metrics")
		}).WithTimeout(10 * time.Second).WithPolling(time.Second).Should(Succeed())
	}
}

// ValidateManagementClusterLogFlow verifies logs flow from management cluster to Loki
func ValidateManagementClusterLogFlow(ctx context.Context, input *systemTestInput) {
	lokiClient := loki.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	testNamespacePrefix := "test-logging-mgmt-"
	uniqueMessage := fmt.Sprintf("test-log-mgmt-%d", time.Now().Unix())

	By("Creating test namespace in management cluster")
	testNS := createTestNamespaceInCluster(ctx, input.client, testNamespacePrefix)

	By(fmt.Sprintf("Creating log generator pod with message: %s", uniqueMessage))
	createLogGeneratorPod(ctx, input.client, testNS, "log-generator", uniqueMessage)

	By("Waiting for logs to be collected and forwarded to Loki")
	Eventually(func(g Gomega) {
		labels := map[string]string{
			"cluster": "management",
		}
		entries, err := lokiClient.QueryLogs(ctx, uniqueMessage, labels, 5*time.Minute)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(entries).NotTo(BeEmpty(), "No logs found in Loki for management cluster")

		By("Verifying log content and cluster attribution in Loki")
		found := false
		for _, entry := range entries {
			if !strings.Contains(entry.Line, uniqueMessage) {
				continue
			}
			if entry.Stream["k8s_namespace_name"] != testNS {
				continue
			}

			// Verify cluster label
			g.Expect(entry.Stream).To(HaveKeyWithValue("cluster", "management"))
			// Verify Kubernetes metadata in stream labels
			g.Expect(entry.Stream).To(HaveKey("k8s_pod_name"))
			g.Expect(entry.Stream).To(HaveKey("k8s_container_name"))
			found = true
			break
		}
		g.Expect(found).To(BeTrue(), "Expected log message not found in Loki")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// ValidateDPUClusterLogFlow verifies logs flow from DPU cluster to Loki
func ValidateDPUClusterLogFlow(ctx context.Context, input *systemTestInput) {
	lokiClient := loki.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	testNamespacePrefix := "test-logging-dpu-"
	uniqueMessage := fmt.Sprintf("test-log-dpu-%d", time.Now().Unix())

	By("Creating test namespace in DPU cluster")
	testNS := createTestNamespaceInCluster(ctx, dpuClusterClient[0], testNamespacePrefix)

	By(fmt.Sprintf("Creating log generator pod in DPU cluster with message: %s", uniqueMessage))
	createLogGeneratorPod(ctx, dpuClusterClient[0], testNS, "log-generator-dpu", uniqueMessage)

	By("Waiting for logs to be collected and forwarded to Loki")
	clusterName := input.dpuClusters[0].Name
	Eventually(func(g Gomega) {
		labels := map[string]string{
			"cluster": clusterName,
		}
		entries, err := lokiClient.QueryLogs(ctx, uniqueMessage, labels, 5*time.Minute)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(entries).NotTo(BeEmpty(),
			fmt.Sprintf("No logs found in Loki for DPU cluster %s", clusterName))

		// Verify log content and cluster attribution
		found := false
		for _, entry := range entries {
			if !strings.Contains(entry.Line, uniqueMessage) {
				continue
			}
			if entry.Stream["k8s_namespace_name"] != testNS {
				continue
			}

			// Verify cluster label
			g.Expect(entry.Stream).To(HaveKeyWithValue("cluster", clusterName))
			// Verify Kubernetes metadata in stream labels
			g.Expect(entry.Stream).To(HaveKey("k8s_pod_name"))
			g.Expect(entry.Stream).To(HaveKey("k8s_container_name"))
			found = true
			break
		}
		g.Expect(found).To(BeTrue(), "Expected log message not found in Loki")
	}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())
}

// ValidateKamajiAuditLogFlow verifies that kube-apiserver audit logs from Kamaji DPU
// clusters are collected by the management cluster otel-agent and forwarded to Loki.
// It creates a namespace in the DPU cluster (which generates audit events) then checks
// that matching audit log entries appear in Loki tagged with the DPU cluster name.
func ValidateKamajiAuditLogFlow(ctx context.Context, input *systemTestInput) {
	lokiClient := loki.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	clusterName := input.dpuClusters[0].Name

	By(fmt.Sprintf("Creating test namespace in DPU cluster %s to generate audit events", clusterName))
	testNS := createTestNamespaceInCluster(ctx, dpuClusterClient[0], "test-audit-")

	// At Metadata level objectRef.name comes from the request path, so the create
	// above (which carries the name in the body) logs no event naming the namespace.
	// A get by name does.
	By(fmt.Sprintf("Reading namespace %s back by name to generate a named audit event", testNS))
	Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: testNS}, &corev1.Namespace{})).To(Succeed())

	By(fmt.Sprintf("Waiting for audit logs for cluster %s to appear in Loki", clusterName))
	Eventually(func(g Gomega) {
		labels := map[string]string{
			"cluster": clusterName,
		}
		entries, err := lokiClient.QueryLogs(ctx, testNS, labels, 5*time.Minute)
		g.Expect(err).NotTo(HaveOccurred())
		if len(entries) == 0 {
			// Repeat the search without the cluster stream selector to tell "never
			// reached Loki" apart from "reached Loki but not attributed to the cluster".
			unlabeled, unlabeledErr := lokiClient.QueryLogs(ctx, testNS, nil, 5*time.Minute)
			g.Expect(entries).NotTo(BeEmpty(), fmt.Sprintf(
				"No audit logs found in Loki for DPU cluster %s. The same search without the cluster label returned %d entries (err: %v)",
				clusterName, len(unlabeled), unlabeledErr))
		}

		found := false
		for _, entry := range entries {
			if strings.Contains(entry.Line, testNS) {
				g.Expect(entry.Stream).To(HaveKeyWithValue("cluster", clusterName))
				found = true
				break
			}
		}
		g.Expect(found).To(BeTrue(), "Expected audit log entry not found in Loki")
	}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// ValidateDPUClusterMetricsFlow verifies that container, pod, and node metrics
// scraped by the DPU cluster collector's kubeletstats receiver are streamed to
// the host cluster and land in the host Prometheus. The DPU collector stamps
// every series with cluster=<DPUCluster name>, which uniquely identifies
// DPU-origin metrics and distinguishes them from host cadvisor metrics. The test
// queries Prometheus for kubelet-origin workload series carrying that label.
func ValidateDPUClusterMetricsFlow(ctx context.Context, input *systemTestInput) {
	promClient := prometheus.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	clusterName := input.dpuClusters[0].Name

	By(fmt.Sprintf("Querying host Prometheus for kubelet metrics streamed from DPU cluster %s", clusterName))
	// Match the kubeletstats-derived pod and container series (names carry unit
	// and _total suffixes added by the prometheusremotewrite exporter, so match
	// by prefix regex) that are attributed to the DPU cluster.
	query := fmt.Sprintf(`count({__name__=~"k8s_pod_.+|container_.+", cluster=%q})`, clusterName)
	Eventually(func(g Gomega) {
		samples, err := promClient.QueryInstant(ctx, query)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(samples).NotTo(BeEmpty(),
			fmt.Sprintf("No kubelet metrics found in host Prometheus for DPU cluster %s", clusterName))
		g.Expect(samples[0].Value).To(BeNumerically(">", 0),
			fmt.Sprintf("Expected streamed kubelet metrics for DPU cluster %s, found none", clusterName))
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

// createLogGeneratorPod creates a busybox pod that continuously echoes a unique log message
func createLogGeneratorPod(ctx context.Context, c client.Client, namespace, name, logMessage string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    CleanupScope.It,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "log-generator",
					Image:   fmt.Sprintf("%s/busybox:latest", dockerIORegistry),
					Command: []string{"sh", "-c", fmt.Sprintf("while true; do echo '%s'; sleep 1; done", logMessage)},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
				{
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	Expect(client.IgnoreAlreadyExists(c.Create(ctx, pod))).To(Succeed())

	// Wait for pod to be running
	Eventually(func(g Gomega) {
		updatedPod := &corev1.Pod{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, updatedPod)).To(Succeed())
		g.Expect(updatedPod.Status.Phase).To(Equal(corev1.PodRunning))
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// createTestNamespaceInCluster creates a namespace in a specific cluster with cleanup labels
func createTestNamespaceInCluster(ctx context.Context, c client.Client, namespacePrefix string) string {
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namespacePrefix,
			Labels:       CleanupScope.It,
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, testNS))).To(Succeed())
	return testNS.GetName()
}
