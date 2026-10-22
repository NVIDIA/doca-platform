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

package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

// DefaultService is the Tempo service deployed by deploy/helmfiles/values/tempo.yaml.
const DefaultService = "tempo"

// DefaultPort is Tempo's query HTTP port, as exposed by the chart in deploy/helmfiles/monitoring.yaml.
const DefaultPort = 3200

// Client queries Tempo via the Kubernetes API server service proxy.
type Client struct {
	restClient *rest.RESTClient
	namespace  string
	service    string
	port       int
}

// TraceSummary is one entry from Tempo's /api/search results.
type TraceSummary struct {
	TraceID         string
	RootServiceName string
	RootTraceName   string
}

// searchResponse is the envelope returned by Tempo's /api/search endpoint.
type searchResponse struct {
	Traces []struct {
		TraceID         string `json:"traceID"`
		RootServiceName string `json:"rootServiceName"`
		RootTraceName   string `json:"rootTraceName"`
	} `json:"traces"`
}

// NewClient returns a Client that proxies through the Tempo service in the given namespace.
func NewClient(restClient *rest.RESTClient, namespace string) *Client {
	return &Client{
		restClient: restClient,
		namespace:  namespace,
		service:    DefaultService,
		port:       DefaultPort,
	}
}

// SearchByTags searches Tempo for traces containing a span that has all the given attributes,
// started within lookback of now, using Tempo's /api/search endpoint (tag-based search, enabled
// by default - no additional Tempo configuration required). Span and resource attributes both
// match, e.g. "controller" and "service.name".
func (c *Client) SearchByTags(ctx context.Context, tags map[string]string, lookback time.Duration) ([]TraceSummary, error) {
	end := time.Now()
	start := end.Add(-lookback)

	// Tempo takes the tags as a logfmt list, in a stable order so requests are reproducible.
	keys := slices.Sorted(maps.Keys(tags))
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, tags[k]))
	}
	tagQuery := strings.Join(pairs, " ")

	proxyPath := fmt.Sprintf(
		"/api/v1/namespaces/%s/services/%s:%d/proxy/api/search",
		c.namespace, c.service, c.port,
	)

	req := c.restClient.Get().AbsPath(proxyPath).
		Param("tags", tagQuery).
		Param("start", fmt.Sprintf("%d", start.Unix())).
		Param("end", fmt.Sprintf("%d", end.Unix()))

	body, err := req.DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("tempo search for %q failed: %w", tagQuery, err)
	}

	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tempo search response: %w", err)
	}

	summaries := make([]TraceSummary, 0, len(resp.Traces))
	for _, t := range resp.Traces {
		summaries = append(summaries, TraceSummary{
			TraceID:         t.TraceID,
			RootServiceName: t.RootServiceName,
			RootTraceName:   t.RootTraceName,
		})
	}
	return summaries, nil
}

// Span is one span of a trace returned by GetTrace.
type Span struct {
	Name        string
	ServiceName string
	// Attributes holds the span's string attributes.
	Attributes map[string]string
}

// traceResponse is the envelope returned by Tempo's /api/traces/<id> endpoint.
type traceResponse struct {
	Batches []struct {
		Resource struct {
			Attributes []attribute `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Spans []struct {
				Name       string      `json:"name"`
				Attributes []attribute `json:"attributes"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"batches"`
}

// attribute is an OTLP key/value pair, of which only string values are read.
type attribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
	} `json:"value"`
}

// GetTrace returns all spans of the trace with the given ID, as Tempo has them so far.
func (c *Client) GetTrace(ctx context.Context, traceID string) ([]Span, error) {
	proxyPath := fmt.Sprintf(
		"/api/v1/namespaces/%s/services/%s:%d/proxy/api/traces/%s",
		c.namespace, c.service, c.port, traceID,
	)

	body, err := c.restClient.Get().AbsPath(proxyPath).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("tempo trace %s lookup failed: %w", traceID, err)
	}

	var resp traceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tempo trace response: %w", err)
	}

	var spans []Span
	for _, batch := range resp.Batches {
		serviceName := ""
		for _, a := range batch.Resource.Attributes {
			if a.Key == "service.name" {
				serviceName = a.Value.StringValue
			}
		}
		for _, scope := range batch.ScopeSpans {
			for _, s := range scope.Spans {
				span := Span{Name: s.Name, ServiceName: serviceName, Attributes: map[string]string{}}
				for _, a := range s.Attributes {
					span.Attributes[a.Key] = a.Value.StringValue
				}
				spans = append(spans, span)
			}
		}
	}
	return spans, nil
}
