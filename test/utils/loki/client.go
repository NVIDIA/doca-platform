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

package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

// logLimit is the limit of log entries to return from Loki queries.
const logLimit = "1000"

// Client provides methods to query Loki via Kubernetes service proxy
type Client struct {
	restClient *rest.RESTClient
	namespace  string
	service    string
	port       int
}

// LogEntry represents a single log entry returned by Loki
type LogEntry struct {
	Timestamp time.Time
	Line      string
	Stream    map[string]string
}

// QueryResponse represents the response from Loki's query_range endpoint
type QueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"` // [timestamp, log line]
		} `json:"result"`
	} `json:"data"`
}

// NewClient creates a new Loki client
func NewClient(restClient *rest.RESTClient, namespace string) *Client {
	return &Client{
		restClient: restClient,
		namespace:  namespace,
		service:    "loki",
		port:       3100, // Loki default port
	}
}

// QueryLogs performs a log search with multiple label filters
func (c *Client) QueryLogs(ctx context.Context, searchTerm string, labels map[string]string, lookback time.Duration) ([]LogEntry, error) {
	end := time.Now()
	start := end.Add(-lookback)
	return c.queryLogsInTimeRange(ctx, searchTerm, labels, start, end)
}

// queryLogsInTimeRange performs a time-bound log query
func (c *Client) queryLogsInTimeRange(ctx context.Context, searchTerm string, labels map[string]string, start, end time.Time) ([]LogEntry, error) {
	// Build LogQL query
	query := c.buildLogQLQuery(searchTerm, labels)

	// Build query parameters
	params := url.Values{}
	params.Add("query", query)
	params.Add("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Add("end", fmt.Sprintf("%d", end.UnixNano()))
	params.Add("limit", logLimit)

	// Build the proxy path (without query parameters)
	proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%d/proxy/loki/api/v1/query_range",
		c.namespace, c.service, c.port)

	// Execute request using the REST client
	// Use AbsPath for the proxy path and SetParam for query parameters
	req := c.restClient.Get().AbsPath(proxyPath)
	for key, values := range params {
		for _, value := range values {
			req = req.Param(key, value)
		}
	}

	resBody, err := req.DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("loki query request failed: %w", err)
	}

	// Parse response
	var response QueryResponse
	if err := json.Unmarshal(resBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse loki response: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("loki query returned non-success status: %s", response.Status)
	}

	// Extract log entries from Loki response
	// Example response.Data.Result[]: {
	//   stream: {"namespace": "default", "pod": "app-123"},
	//   values: [["1667069275158472827", "log message"]]
	//  }
	var entries []LogEntry
	for _, result := range response.Data.Result {
		for _, value := range result.Values {
			// Each value should contain exactly 2 elements: [timestamp, log line]
			if len(value) != 2 {
				continue
			}
			// Parse timestamp (nanoseconds since epoch)
			timestamp, err := parseTimestamp(value[0])
			if err != nil {
				continue
			}
			entries = append(entries, LogEntry{
				Timestamp: timestamp,
				Line:      value[1],
				Stream:    result.Stream,
			})
		}
	}

	return entries, nil
}

// buildLogQLQuery constructs a LogQL query string
func (c *Client) buildLogQLQuery(searchTerm string, labels map[string]string) string {
	queries := []string{}
	for key, value := range labels {
		queries = append(queries, fmt.Sprintf(`%s="%s"`, key, value))
	}

	// If no labels provided, use a catch-all that at least filters for logs
	if len(labels) == 0 {
		queries = []string{`k8s_namespace_name=~".+"`}
	}

	query := fmt.Sprintf("{%s}", strings.Join(queries, ","))

	// Add search term filter if provided
	if searchTerm != "" {
		query += fmt.Sprintf(` |= "%s"`, searchTerm)
	}

	return query
}

// parseTimestamp converts a Loki timestamp (nanoseconds since epoch) to time.Time
func parseTimestamp(ts string) (time.Time, error) {
	// Loki returns timestamps as nanosecond strings
	var nanos int64
	_, err := fmt.Sscanf(ts, "%d", &nanos)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nanos), nil
}
