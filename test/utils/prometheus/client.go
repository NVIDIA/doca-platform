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

package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/client-go/rest"
)

const (
	// DefaultService is the Prometheus service deployed by kube-prometheus-stack.
	DefaultService = "kube-prometheus-stack-prometheus"
	// DefaultPort is the default Prometheus HTTP port.
	DefaultPort = 9090
)

// Client queries Prometheus via the Kubernetes API server service proxy.
type Client struct {
	restClient *rest.RESTClient
	namespace  string
	service    string
	port       int
}

// Sample is a single instant-query result: the series labels and its value.
type Sample struct {
	Metric map[string]string
	Value  float64
}

func (s *Sample) String() string {
	labels := make([]string, 0, len(s.Metric))
	for k, v := range s.Metric {
		labels = append(labels, fmt.Sprintf("%s=%q, ", k, v))
	}
	return fmt.Sprintf("{%s} %f", strings.Join(labels, ", "), s.Value)
}

// queryResponse is the envelope returned by /api/v1/query for a vector result.
type queryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"` // [unixTimestamp, "stringValue"]
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

// NewClient returns a Client that proxies through the kube-prometheus-stack
// Prometheus service in the given namespace.
func NewClient(restClient *rest.RESTClient, namespace string) *Client {
	return &Client{
		restClient: restClient,
		namespace:  namespace,
		service:    DefaultService,
		port:       DefaultPort,
	}
}

// QueryInstant executes an instant PromQL query and returns the matching samples.
func (c *Client) QueryInstant(ctx context.Context, query string) ([]Sample, error) {
	proxyPath := fmt.Sprintf(
		"/api/v1/namespaces/%s/services/%s:%d/proxy/api/v1/query",
		c.namespace, c.service, c.port,
	)

	body, err := c.restClient.Get().AbsPath(proxyPath).Param("query", query).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("prometheus query %q failed: %w", query, err)
	}

	var resp queryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse prometheus response: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus query %q returned non-success status %q: %s", query, resp.Status, resp.Error)
	}

	samples := make([]Sample, 0, len(resp.Data.Result))
	for _, r := range resp.Data.Result {
		s := Sample{Metric: r.Metric}
		// value is [unixTimestamp, "stringValue"]; parse the string value if present.
		if len(r.Value) == 2 {
			if raw, ok := r.Value[1].(string); ok {
				if v, err := strconv.ParseFloat(raw, 64); err == nil {
					s.Value = v
				}
			}
		}
		samples = append(samples, s)
	}
	return samples, nil
}
