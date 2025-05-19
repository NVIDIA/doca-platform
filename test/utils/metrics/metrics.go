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
	"encoding/json"
	"fmt"
	"iter"
	"regexp"
	"slices"
	"strings"

	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
)

// GetKSMMetrics Returns all unique metrics names mapped to metrics prefix
func GetKSMMetrics(ctx context.Context, testRESTClient *rest.RESTClient, metricsURI string) map[string][]string {
	var metricsURL string
	var resBody []byte

	request := testRESTClient.Get().AbsPath(metricsURI)
	resBody, err := request.DoRaw(ctx)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURI, err))
	Expect(resBody).NotTo(BeEmpty(), "Response body is empty for url %s", metricsURL)

	actualMetrics := parseRawMetrics(resBody)
	return actualMetrics
}

// GetPrometheusMetrics Returns all unique metrics for the given metrics prefix
func GetPrometheusMetrics(ctx context.Context, testRESTClient *rest.RESTClient, g Gomega, metricsPrefix iter.Seq[string], namespace string) map[string][]string {
	actualMetrics := make(map[string][]string)
	for metricPref := range metricsPrefix {
		// Prepare metrics request URL and query
		queryValue := fmt.Sprintf(`{__name__=~"%s.*"}`, metricPref)
		metricsURL := GetMetricsURI("dpf-operator-prometheus-server", namespace, 80, "/api/v1/query")
		request := testRESTClient.Get().AbsPath(metricsURL).Param("query", queryValue)
		// Request metrics from Prometheus
		response, err := request.DoRaw(ctx)
		g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Request %s failed with err: %v", metricsURL, err))
		g.Expect(response).NotTo(BeNil(), fmt.Sprintf("Metrics api is not accessible by url %s ", metricsURL))

		actualMetrics = parseJSONMetrics(response, actualMetrics, metricPref)
	}
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

// Unmarshal and verify JSON response, return actualMetrics map with all found unique metrics where key: prefix, value: metric names list
func parseJSONMetrics(responseJSON []byte, actualMetrics map[string][]string, metricPref string) map[string][]string {
	metrics := make(map[string]interface{})
	Expect(json.Unmarshal(responseJSON, &metrics)).To(Succeed(), "Metrics JSON schema does not match for prefix %s", metricPref)
	resultJSON := metrics["data"].(map[string]interface{})["result"].([]interface{})
	Expect(resultJSON).NotTo(BeEmpty(), fmt.Sprintf("Actual metrics are empty for prefix %v\n", metricPref))
	for _, res := range resultJSON {
		fullName := res.(map[string]interface{})["metric"].(map[string]interface{})["__name__"].(string)
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
