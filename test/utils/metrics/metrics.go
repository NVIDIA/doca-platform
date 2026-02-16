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

package metrics

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetKSMMetrics Returns all unique metrics names mapped to metrics prefix
func GetKSMMetrics(ctx context.Context, restClient *rest.RESTClient, metricsURI string) map[string][]string {
	var metricsURL string
	var resBody []byte

	request := restClient.Get().AbsPath(metricsURI)
	resBody, err := request.DoRaw(ctx)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURI, err))
	Expect(resBody).NotTo(BeEmpty(), "Response body is empty for url %s", metricsURL)

	actualMetrics := parseRawMetrics(resBody)
	return actualMetrics
}

// Parse raw metrics from KMS, return actualMetrics map with all found unique metrics where key: prefix, value: metric names list
func parseRawMetrics(response []byte) map[string][]string {
	actualMetrics := make(map[string][]string)
	reMetric := regexp.MustCompile(`\n(\w+)_(\w+)\{.*\n`)
	resValues := reMetric.FindAll(response, -1)
	for _, metric := range resValues {
		fullName := strings.ReplaceAll(strings.Split(string(metric), "{")[0], "\n", "")
		actualMetrics = addMetricToMap(actualMetrics, fullName)
	}
	return actualMetrics
}

// VerifyMetrics Compares expectedMetrics map to actualMetrics map and returns the diff map with missed actual metrics
func VerifyMetrics(expectedMetrics map[string][]string, actualMetrics map[string][]string) map[string][]string {
	diffMetrics := make(map[string][]string)
	for expectedKey, expectedValue := range expectedMetrics {
		var actualValue []string
		actualValue, ok := actualMetrics[expectedKey]
		if ok {
			for _, k := range actualValue {
				if !slices.Contains(expectedValue, k) {
					diffMetrics[expectedKey] = append(diffMetrics[expectedKey], k)
				}
			}
		} else {
			diffMetrics[expectedKey] = expectedValue
		}
	}
	return diffMetrics
}

// addMetricToMap Adds metric to the map, transforms full_name to pref[name], appends only unique entries
func addMetricToMap(metricMap map[string][]string, fullName string) map[string][]string {
	pref := strings.Split(fullName, "_")[0]
	name := fullName[len(pref+"_"):]
	if !slices.Contains(metricMap[pref], name) {
		metricMap[pref] = append(metricMap[pref], name)
	}
	return metricMap
}

func GetMetricsURI(serviceName string, namespace string, port int, endpoint string) string {
	return fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%d/proxy%s", namespace, serviceName, port, endpoint)
}

// GetKSMMetricsURIForDPUCluster finds the kube-state-metrics DPUService for a specific DPUCluster
// and returns the metrics URI for that service.
func GetKSMMetricsURIForDPUCluster(ctx context.Context, c client.Client, dpuCluster *provisioningv1.DPUCluster, namespace string, port int, endpoint string) (string, error) {
	// List all kube-state-metrics DPUServices
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := c.List(ctx, dpuServiceList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			operatorv1.DPFComponentLabelKey:            operatorv1.KubeStateMetricsName.String(),
			provisioningv1.DPUClusterNameLabelKey:      dpuCluster.Name,
			provisioningv1.DPUClusterNamespaceLabelKey: dpuCluster.Namespace,
		}); err != nil {
		return "", fmt.Errorf("failed to list DPUServices: %w", err)
	}

	if len(dpuServiceList.Items) == 0 {
		return "", fmt.Errorf("no kube-state-metrics DPUService found for DPUCluster %s/%s", dpuCluster.Namespace, dpuCluster.Name)
	}

	if len(dpuServiceList.Items) > 1 {
		return "", fmt.Errorf("multiple kube-state-metrics DPUServices found for DPUCluster %s/%s", dpuCluster.Namespace, dpuCluster.Name)
	}

	// The service name is prefixed with "in-cluster-" for in-cluster DPUServices
	dpuServiceName := dpuServiceList.Items[0].Name
	serviceName := fmt.Sprintf("in-cluster-%s", dpuServiceName)
	return GetMetricsURI(serviceName, namespace, port, endpoint), nil
}
