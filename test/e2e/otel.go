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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpuservicecontroller "github.com/nvidia/doca-platform/internal/dpuservice/controllers"
	dpuservicechaincontroller "github.com/nvidia/doca-platform/internal/dpuservicechain/controllers"
	nodesriovdeviceplugincontrollers "github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/controllers"
	dpucontroller "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	dpudevicecontroller "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpudevice"
	dpusetcontroller "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	sfcsetcontroller "github.com/nvidia/doca-platform/internal/servicechainset/controllers"
	sfccontroller "github.com/nvidia/doca-platform/internal/sfccontroller/controllers"
	"github.com/nvidia/doca-platform/test/utils/loki"
	"github.com/nvidia/doca-platform/test/utils/prometheus"
	"github.com/nvidia/doca-platform/test/utils/tempo"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// otelNodePort is the NodePort used for OpenTelemetry Collector endpoint as configured in otel Helm Chart.
const (
	otelNodePort       = 30050
	otelEndpointSchema = "http://"
	// otelTLSNodePort is the NodePort of the management collector's TLS OTLP/HTTP receiver.
	otelTLSNodePort       = 30051
	otelTLSEndpointSchema = "https://"
	// otelTLSGRPCNodePort is the NodePort of the management collector's TLS OTLP/gRPC receiver.
	otelTLSGRPCNodePort = 30052
	// otelServingCertSecretName is the cert-manager managed Secret holding the management
	// collector's serving certificate, created by deploy/helmfiles/hooks/apply-otel-certificates.sh.
	// Its "ca.crt" is the CA the DPU cluster collectors verify the endpoint against, referenced
	// directly by the caSecretRef of DPFOperatorConfig
	// spec.monitoring.openTelemetryCollector.logging and .metrics.
	otelServingCertSecretName = "otel-gateway-server-tls"
	// otelInClusterHost is the management collector's in-cluster Service DNS name, a SAN the
	// serving certificate already covers (unlike the node IP / keepalived VIP otelExportAddress
	// resolves, which may be a placeholder address in environments with no real DPU nodes - see
	// openTelemetryCollectorConfiguration's use for Tracing).
	otelInClusterHost = "opentelemetry-collector.dpf-operator-system.svc.cluster.local"
	// otelInClusterHTTPPort/otelInClusterHTTPSPort are the collector's in-cluster Service ports
	// (same container ports the NodePorts above front, see deploy/helmfiles/values/opentelemetry-collector.yaml).
	otelInClusterHTTPPort  = 4318
	otelInClusterHTTPSPort = 4320
)

// otelExportAddress returns the address DPU cluster collectors export to: a DPUCluster keepalived
// VIP when one exists, otherwise the control plane node IP. The endpoint is a single field shared by
// every DPU cluster and every VIP fronts the same management cluster nodes, so any of them works.
// Read from the manifests because the DPFOperatorConfig is built before the DPUClusters exist, and
// must match the DPUCLUSTER_VIP added to the collector certificate's IP SANs.
func otelExportAddress(ctx context.Context, c client.Client, dpuClusterPaths []string) string {
	for _, dpuClusterPath := range dpuClusterPaths {
		dpuCluster := objectFromFile[provisioningv1.DPUCluster](dpuClusterPath)
		endpoint := dpuCluster.Spec.ClusterEndpoint
		if endpoint != nil && endpoint.Keepalived != nil && endpoint.Keepalived.VIP != "" {
			return endpoint.Keepalived.VIP
		}
	}
	return getClusterControlPlaneIP(ctx, c)
}

// otelTracingEndpoint returns an endpoint DPF exports traces to. The DPF controllers in this cluster
// use the collector's in-cluster Service: a name the serving certificate always covers and always
// dialable from here, unlike the node IP / keepalived VIP, which may be a placeholder address in a
// DPUCluster fixture with no real DPU nodes. The DPU clusters cannot resolve that name, so for them
// (forDPUClusters) it is the external NodePort/VIP they use for logs and metrics.
func otelTracingEndpoint(exportAddress string, forDPUClusters, tls bool) string {
	schema, port, host := otelEndpointSchema, otelInClusterHTTPPort, otelInClusterHost
	if tls {
		schema, port = otelTLSEndpointSchema, otelInClusterHTTPSPort
	}
	if forDPUClusters {
		host, port = exportAddress, otelNodePort
		if tls {
			port = otelTLSNodePort
		}
	}
	return fmt.Sprintf("%s%s:%d", schema, host, port)
}

// openTelemetryCollectorConfiguration returns the collector configuration for the generated
// DPFOperatorConfig. When the monitoring stack created the management collector's serving
// certificate, DPU clusters export via the TLS endpoints and verify them against the CA in the
// cert-manager-managed serving certificate Secret, referenced directly by caSecretRef. Logs go
// over OTLP/HTTP and metrics over OTLP/gRPC, so both transports and both caSecretRef paths are
// covered end to end against a real cert-manager Secret. Suites deployed without the monitoring
// stack fall back to the plaintext endpoint.
func openTelemetryCollectorConfiguration(ctx context.Context, c client.Client, dpuClusterPaths []string) *operatorv1.OpenTelemetryCollectorConfiguration {
	exportAddress := otelExportAddress(ctx, c, dpuClusterPaths)

	if !otelServingCertAvailable(ctx, c) {
		return &operatorv1.OpenTelemetryCollectorConfiguration{
			Logging: &operatorv1.OpenTelemetryCollectorExportConfiguration{
				Endpoint: fmt.Sprintf("%s%s:%d", otelEndpointSchema, exportAddress, otelNodePort),
			},
			Metrics: &operatorv1.OpenTelemetryCollectorExportConfiguration{
				Endpoint: fmt.Sprintf("%s%s:%d", otelEndpointSchema, exportAddress, otelNodePort),
			},
			// The DPU clusters export their traces to the external NodePort/VIP, like logs and metrics.
			Tracing: &operatorv1.OpenTelemetryCollectorExportConfiguration{
				Endpoint: otelTracingEndpoint(exportAddress, true, false),
			},
		}
	}

	// The serving certificate covers the node InternalIPs and the keepalived VIP as IP SANs.
	// The CA is read straight from the cert-manager-managed serving certificate Secret
	// (namespace and key default to the DPFOperatorConfig namespace and "ca.crt").
	return &operatorv1.OpenTelemetryCollectorConfiguration{
		Logging: &operatorv1.OpenTelemetryCollectorExportConfiguration{
			Endpoint:    fmt.Sprintf("%s%s:%d", otelTLSEndpointSchema, exportAddress, otelTLSNodePort),
			CASecretRef: &operatorv1.OpenTelemetryCollectorCASecretReference{Name: otelServingCertSecretName},
		},
		Metrics: &operatorv1.OpenTelemetryCollectorExportConfiguration{
			Endpoint:    fmt.Sprintf("%s%s:%d", otelTLSEndpointSchema, exportAddress, otelTLSGRPCNodePort),
			Transport:   ptr.To(operatorv1.OpenTelemetryCollectorTransportGRPC),
			CASecretRef: &operatorv1.OpenTelemetryCollectorCASecretReference{Name: otelServingCertSecretName},
		},
		Tracing: &operatorv1.OpenTelemetryCollectorExportConfiguration{
			Endpoint:    otelTracingEndpoint(exportAddress, true, true),
			CASecretRef: &operatorv1.OpenTelemetryCollectorCASecretReference{Name: otelServingCertSecretName},
		},
	}
}

// tracingConfiguration returns the tracing configuration for the generated DPFOperatorConfig. The DPF
// controllers run in this cluster, so they export to the collector's in-cluster Service.
func tracingConfiguration(ctx context.Context, c client.Client) *operatorv1.TracingConfiguration {
	tracing := &operatorv1.TracingConfiguration{
		Otlp:                      operatorv1.OpenTelemetryCollectorExportConfiguration{Endpoint: otelTracingEndpoint("", false, false)},
		RecordUnchangedReconciles: ptr.To(true),
	}
	if otelServingCertAvailable(ctx, c) {
		tracing.Otlp.Endpoint = otelTracingEndpoint("", false, true)
		tracing.Otlp.CASecretRef = &operatorv1.OpenTelemetryCollectorCASecretReference{Name: otelServingCertSecretName}
	}
	return tracing
}

// otelServingCertAvailable reports whether the monitoring stack created the management collector's
// serving certificate with a CA.
func otelServingCertAvailable(ctx context.Context, c client.Client) bool {
	servingCert := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: otelServingCertSecretName}, servingCert)
	if apierrors.IsNotFound(err) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return len(servingCert.Data[operatorv1.OpenTelemetryCollectorCASecretKey]) > 0
}

// ValidateDPUClusterOpenTelemetryConfiguration verifies DPU cluster collector configuration
func ValidateDPUClusterOpenTelemetryConfiguration(ctx context.Context, input *systemTestInput) {
	logging := input.config.Spec.Monitoring.OpenTelemetryCollector.Logging
	metrics := input.config.Spec.Monitoring.OpenTelemetryCollector.Metrics

	// The exporter type encodes the OTLP transport: "otlp" is OTLP/gRPC, "otlphttp" is OTLP/HTTP.
	expectedExporter := "otlphttp/log:"
	if logging.Transport != nil && *logging.Transport == operatorv1.OpenTelemetryCollectorTransportGRPC {
		expectedExporter = "otlp/log:"
	}
	expectedMetricsExporter := "otlphttp/metrics:"
	if metrics.Transport != nil && *metrics.Transport == operatorv1.OpenTelemetryCollectorTransportGRPC {
		expectedMetricsExporter = "otlp/metrics:"
	}

	tracing := input.config.Spec.Monitoring.OpenTelemetryCollector.Tracing
	expectedTracesExporter := "otlphttp/traces:"
	if tracing.Transport != nil && *tracing.Transport == operatorv1.OpenTelemetryCollectorTransportGRPC {
		expectedTracesExporter = "otlp/traces:"
	}

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
			g.Expect(c).To(ContainSubstring(logging.Endpoint),
				fmt.Sprintf("ConfigMap should contain the management endpoint %s", logging.Endpoint))
			g.Expect(c).To(ContainSubstring(expectedExporter),
				"ConfigMap should contain the OTLP exporter for logging")
			if logging.CASecretRef != nil && metrics.CASecretRef != nil {
				// The CA travels as an inline PEM. Verification must not be disabled.
				g.Expect(c).To(ContainSubstring("ca_pem:"),
					"ConfigMap should contain the CA certificate for TLS verification")
				g.Expect(c).NotTo(ContainSubstring("insecure: true"),
					"ConfigMap should not disable TLS for the exporters")
			}
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
			g.Expect(c).To(ContainSubstring(metrics.Endpoint),
				fmt.Sprintf("ConfigMap should contain the management metrics endpoint %s", metrics.Endpoint))
			g.Expect(c).To(ContainSubstring(expectedMetricsExporter),
				"ConfigMap should contain the OTLP exporter for metrics")
			// Verify the traces pipeline forwards to the management tracing endpoint
			g.Expect(c).To(ContainSubstring(tracing.Endpoint),
				fmt.Sprintf("ConfigMap should contain the management tracing endpoint %s", tracing.Endpoint))
			g.Expect(c).To(ContainSubstring(expectedTracesExporter),
				"ConfigMap should contain the OTLP exporter for traces")
		}).WithTimeout(10 * time.Second).WithPolling(time.Second).Should(Succeed())
	}
}

const (
	// additionalOpenTelemetryCollectorName is a user DPUService. Its name stays
	// distinct from the operator-managed opentelemetry-collector DPUService.
	additionalOpenTelemetryCollectorName = "custom-otel"
	// additionalOpenTelemetryCollectorEndpoint is rendered only by the user collector.
	additionalOpenTelemetryCollectorEndpoint = "https://custom-otel.e2e.example:4318"
)

// ValidateAdditionalDPUCollector deploys a second collector beside the packaged one.
// The user DPUService carries the custom document. The operator-managed collector
// keeps the pipelines from DPFOperatorConfig.
func ValidateAdditionalDPUCollector(ctx context.Context, input *systemTestInput) {
	additionalService := objectFromFile[dpuservicev1.DPUService]("../objects/application/dpuservice-custom-otel.yaml")
	additionalService.Spec.HelmChart.Source.RepoURL = helmRegistry
	additionalService.Spec.HelmChart.Source.Version = tag
	Expect(input.client.Create(ctx, additionalService)).To(Succeed())
	DeferCleanup(func(ctx context.Context) {
		Expect(client.IgnoreNotFound(input.client.Delete(ctx, additionalService))).To(Succeed())
		for i, dpuClient := range dpuClusterClient {
			clusterName := input.dpuClusters[i].Name
			Eventually(func(g Gomega) {
				err := dpuClient.Get(ctx, client.ObjectKey{
					Namespace: dpfOperatorSystemNamespace,
					Name:      additionalOpenTelemetryCollectorConfigMapName(clusterName),
				}, &corev1.ConfigMap{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())
		}
	})

	for i, dpuClient := range dpuClusterClient {
		clusterName := input.dpuClusters[i].Name
		By(fmt.Sprintf("Checking the additional and packaged collectors in DPU cluster %s", clusterName))
		Eventually(func(g Gomega) {
			custom := collectorConfigMapData(ctx, g, dpuClient, additionalOpenTelemetryCollectorConfigMapName(clusterName))
			g.Expect(custom).To(ContainSubstring(additionalOpenTelemetryCollectorEndpoint))
			g.Expect(custom).To(ContainSubstring("health_check:"))
			g.Expect(custom).NotTo(ContainSubstring("filelog/doca"))

			packaged := collectorConfigMapData(ctx, g, dpuClient, clusterName+"-opentelemetry-collector-config")
			g.Expect(packaged).To(ContainSubstring("filelog/doca"))
			g.Expect(packaged).To(ContainSubstring(input.config.Spec.Monitoring.OpenTelemetryCollector.Logging.Endpoint))
			g.Expect(packaged).NotTo(ContainSubstring(additionalOpenTelemetryCollectorEndpoint))
		}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())

		VerifyClusterPods(ctx, dpuClient, []string{additionalOpenTelemetryCollectorName})
	}
}

// additionalOpenTelemetryCollectorConfigMapName is the ConfigMap rendered for the user
// DPUService. The Helm release name is {cluster}-custom-otel, which does not contain the
// chart name, so the full name is {release}-opentelemetry-collector.
func additionalOpenTelemetryCollectorConfigMapName(clusterName string) string {
	return clusterName + "-" + additionalOpenTelemetryCollectorName + "-opentelemetry-collector-config"
}

func collectorConfigMapData(ctx context.Context, g Gomega, dpuClient client.Client, name string) string {
	cm := &corev1.ConfigMap{}
	g.Expect(dpuClient.Get(ctx, client.ObjectKey{
		Namespace: dpfOperatorSystemNamespace,
		Name:      name,
	}, cm)).To(Succeed())
	data, exists := cm.Data["otel-collector-config.yaml"]
	g.Expect(exists).To(BeTrue(), "otel-collector-config.yaml not found in ConfigMap %s", name)
	return data
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
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
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
	}).WithTimeout(3 * time.Minute).WithPolling(time.Second).Should(Succeed())
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
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
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

// traceLookback is how far back the trace search looks. A stable cluster stops reconciling, so the
// spans of the system DPUServices and the Node controller date from startup.
const traceLookback = 2 * time.Hour

// tracePollInterval is how often the specs ask Tempo. Each poll fetches whole traces, and Tempo is
// OOM-killed when it is polled every second.
const tracePollInterval = 5 * time.Second

// ValidateManagementClusterTraceFlow verifies trace spans flow from DPF controller reconciles to
// Tempo. DPF deploys several system DPUServices (kube-state-metrics, node-problem-detector,
// opentelemetry-collector, ...) that the DPUService controller reconciles once tracing is
// enabled, each producing a span tagged controller=dpuservice-manager; this waits for one to show
// up rather than manufacturing a dedicated test DPUService. Reconciles that find nothing changed
// are not recorded unless the DPFOperatorConfig asks for it, so the spans can be older than the
// check and the lookback is generous. The e2e config records unchanged reconciles, so the SR-IOV
// device plugin Node controller shows up too, although it has nothing to do on a bench without DPUs.
//
// The match is on the span's tags, not on a trace shared across controllers.
func ValidateManagementClusterTraceFlow(ctx context.Context, input *systemTestInput) {
	tempoClient := tempo.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)

	for _, source := range expectedTraceSources(input) {
		By(fmt.Sprintf("Waiting for %s reconcile spans from %s to appear in Tempo", source.controller, source.serviceName))
		Eventually(func(g Gomega) {
			traces, err := tempoClient.SearchByTags(ctx, map[string]string{
				"controller":   source.controller,
				"service.name": source.serviceName,
			}, traceLookback)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(traces).NotTo(BeEmpty(), "No %s reconcile spans from %s found in Tempo", source.controller, source.serviceName)
		}).WithTimeout(5 * time.Minute).WithPolling(tracePollInterval).Should(Succeed())
	}
}

// ValidateDPUDeploymentTraceFlow verifies that a DPUDeployment yields one trace across the
// controllers that act on it, from the DPUDeployment down to the ServiceChainSet and
// ServiceInterfaceSet it leads to, and the DPUSet it creates. Nothing upstream sets a traceparent on the DPUDeployment, so DPF
// derives the trace from its UID and generation and copies it onto every object it creates. A trace
// that contains the spans of all these controllers shows that every hop passed the context on.
func ValidateDPUDeploymentTraceFlow(ctx context.Context, input *systemTestInput, dpuDeployment *dpuservicev1.DPUDeployment) {
	tempoClient := tempo.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	wantControllers := []string{
		dpuservicecontroller.DPUDeploymentControllerName,
		dpuservicecontroller.DPUServiceControllerName,
		dpuservicechaincontroller.DPUServiceChainControllerName,
		dpuservicechaincontroller.DPUServiceInterfaceControllerName,
		sfcsetcontroller.ServiceChainSetControllerName,
		sfcsetcontroller.ServiceInterfaceSetControllerName,
	}
	if !provisioningTracingDisabled(input) {
		wantControllers = append(wantControllers, dpusetcontroller.DPUSetControllerName)
	}

	By(fmt.Sprintf("Waiting for one trace of DPUDeployment %s to contain the spans of %v", dpuDeployment.Name, wantControllers))
	Eventually(func(g Gomega) {
		traces, err := tempoClient.SearchByTags(ctx, map[string]string{
			"controller":     dpuservicecontroller.DPUDeploymentControllerName,
			"k8s.object.uid": string(dpuDeployment.GetUID()),
		}, 15*time.Minute)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(traces).NotTo(BeEmpty(), "No DPUDeployment reconcile spans found in Tempo")

		// A new generation starts a new trace, so look for the one that has them all.
		var missing []string
		for _, trace := range traces {
			spans, err := tempoClient.GetTrace(ctx, trace.TraceID)
			g.Expect(err).NotTo(HaveOccurred())
			missing = missingControllers(spans, wantControllers)
			if len(missing) == 0 {
				return
			}
		}
		g.Expect(missing).To(BeEmpty(), "Trace is missing the spans of these controllers")
	}).WithTimeout(5 * time.Minute).WithPolling(tracePollInterval).Should(Succeed())
}

// missingControllers returns the controllers in want that no span of the trace is tagged with.
func missingControllers(spans []tempo.Span, want []string) []string {
	seen := map[string]bool{}
	for _, span := range spans {
		seen[span.Attributes["controller"]] = true
	}
	var missing []string
	for _, controller := range want {
		if !seen[controller] {
			missing = append(missing, controller)
		}
	}
	return missing
}

// ValidateDPUClusterTraceFlow verifies spans from the sfc-controller running on the DPUs reach
// Tempo: the DPU cluster collector receives them and forwards them to the management collector.
// The match is on the binary's service.name alone, because which of its reconcilers runs first
// depends on the ServiceInterfaces and ServiceChains the test suite created.
func ValidateDPUClusterTraceFlow(ctx context.Context, input *systemTestInput) {
	tempoClient := tempo.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)

	By(fmt.Sprintf("Waiting for spans from %s to appear in Tempo", sfccontroller.TracingServiceName))
	Eventually(func(g Gomega) {
		traces, err := tempoClient.SearchByTags(ctx, map[string]string{
			"service.name": sfccontroller.TracingServiceName,
		}, traceLookback)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(traces).NotTo(BeEmpty(), "No spans from %s found in Tempo", sfccontroller.TracingServiceName)
	}).WithTimeout(5 * time.Minute).WithPolling(tracePollInterval).Should(Succeed())
}

// traceSource is a controller whose reconcile spans are expected in Tempo.
type traceSource struct {
	// controller is the "controller" span attribute and serviceName the service.name of the binary.
	controller  string
	serviceName string
}

// provisioningTracingDisabled reports whether the provisioning controller opted out of tracing, so
// it exports no spans to wait for.
func provisioningTracingDisabled(input *systemTestInput) bool {
	provisioning := input.config.Spec.ProvisioningController
	return provisioning != nil && ptr.Deref(provisioning.DisableTracing, false)
}

// sfcTracingDisabled reports whether the SFC controller opted out of tracing.
func sfcTracingDisabled(input *systemTestInput) bool {
	sfc := input.config.Spec.SFCController
	return sfc != nil && ptr.Deref(sfc.DisableTracing, false)
}

// ValidateDPUProvisioningTraceFlow verifies the traces of a DPUSet that no caller traces, as the
// suite creates it. A DPU then follows the workflow of its DPUDevice, so the trace of a provisioned
// DPU has to contain the spans of the DPUDevice and the DPU. It looks at the trace of every DPU and
// needs one of them to have both. The DPUSet exports spans of its own, in a trace of its own: only a
// DPUSet with a trace from a caller, such as a DPUDeployment, shares it with its DPUs, see
// ValidateDPUDeploymentTraceFlow. Provisioning happens early in a run, so the lookback is generous.
func ValidateDPUProvisioningTraceFlow(ctx context.Context) {
	tempoClient := tempo.NewClient(hostClusterRESTClient, dpfOperatorSystemNamespace)
	wantControllers := []string{dpudevicecontroller.DPUDeviceControllerName, dpucontroller.DPUControllerName}

	dpuList := &provisioningv1.DPUList{}
	Expect(testClient.List(ctx, dpuList)).To(Succeed())
	Expect(dpuList.Items).NotTo(BeEmpty(), "No DPUs to look for traces of")

	By(fmt.Sprintf("Waiting for one trace of a DPU to contain the spans of %v", wantControllers))
	Eventually(func(g Gomega) {
		tracesFound := 0
		var missing []string
		for _, dpu := range dpuList.Items {
			traces, err := tempoClient.SearchByTags(ctx, map[string]string{
				"controller":     dpucontroller.DPUControllerName,
				"k8s.object.uid": string(dpu.GetUID()),
			}, traceLookback)
			g.Expect(err).NotTo(HaveOccurred())
			for _, trace := range traces {
				spans, err := tempoClient.GetTrace(ctx, trace.TraceID)
				g.Expect(err).NotTo(HaveOccurred())
				tracesFound++
				missing = missingControllers(spans, wantControllers)
				if len(missing) == 0 {
					return
				}
			}
		}
		// No trace had both controllers. Without any trace, missing is nil, so check that first.
		g.Expect(tracesFound).NotTo(BeZero(), "No trace of a DPU found in Tempo")
		g.Expect(missing).To(BeEmpty(), "No trace of a DPU contains the spans of these controllers")
	}).WithTimeout(5 * time.Minute).WithPolling(tracePollInterval).Should(Succeed())

	dpuSetList := &provisioningv1.DPUSetList{}
	Expect(testClient.List(ctx, dpuSetList)).To(Succeed())
	Expect(dpuSetList.Items).NotTo(BeEmpty(), "No DPUSets to look for traces of")

	By("Waiting for the reconcile spans of a DPUSet in a trace of its own")
	Eventually(func(g Gomega) {
		tracesFound := 0
		for _, dpuSet := range dpuSetList.Items {
			traces, err := tempoClient.SearchByTags(ctx, map[string]string{
				"controller":     dpusetcontroller.DPUSetControllerName,
				"k8s.object.uid": string(dpuSet.GetUID()),
			}, traceLookback)
			g.Expect(err).NotTo(HaveOccurred())
			tracesFound += len(traces)
		}
		g.Expect(tracesFound).NotTo(BeZero(), "No trace of a DPUSet found in Tempo")
	}).WithTimeout(5 * time.Minute).WithPolling(tracePollInterval).Should(Succeed())
}

// expectedTraceSources returns the controllers that export spans in this run. The DPUService
// controller does unless it opted out through disableTracing. The SR-IOV device plugin Node
// controller reconciles on every Node event, but is disabled in some configurations (ZeroTrust) and
// then exports nothing, as it does after an opt-out. The provisioning controller reconciles the DPUs,
// so it exports spans once there are DPU nodes.
func expectedTraceSources(input *systemTestInput) []traceSource {
	sources := []traceSource{}
	if input.hasDpuNodes() && !provisioningTracingDisabled(input) {
		sources = append(sources, traceSource{dpucontroller.DPUControllerName, dpucontroller.TracingServiceName})
	}
	if dpuService := input.config.Spec.DPUServiceController; dpuService == nil || !ptr.Deref(dpuService.DisableTracing, false) {
		sources = append(sources, traceSource{dpuservicecontroller.DPUServiceControllerName, dpuservicecontroller.TracingServiceName})
	}
	if nodeSRIOV := input.config.Spec.NodeSRIOVDevicePluginController; nodeSRIOV != nil && !nodeSRIOV.Disabled() && !ptr.Deref(nodeSRIOV.DisableTracing, false) {
		sources = append(sources, traceSource{
			nodesriovdeviceplugincontrollers.NodeControllerName, nodesriovdeviceplugincontrollers.TracingServiceName,
		})
	}
	return sources
}
