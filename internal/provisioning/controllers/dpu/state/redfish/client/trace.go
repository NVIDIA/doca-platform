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

package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/nvidia/doca-platform/internal/utils/tracing"

	"github.com/go-resty/resty/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// installRedfishTracing records one dpu.redfish span for each HTTP call on rc.
// The parent is the span on the request context, or traceParent when the request has none.
func (c *Client) installRedfishTracing(rc *resty.Client) {
	rc.OnBeforeRequest(func(rc *resty.Client, r *resty.Request) error {
		c.beginRedfishSpan(rc, r)
		return nil
	})
	rc.OnSuccess(func(_ *resty.Client, resp *resty.Response) {
		if resp == nil {
			return
		}
		var err error
		if resp.StatusCode() >= http.StatusBadRequest {
			err = fmt.Errorf("HTTP %s", resp.Status())
		}
		endRedfishHTTPSpan(resp.Request, resp.StatusCode(), err)
	})
	rc.OnError(func(r *resty.Request, err error) {
		status := 0
		var respErr *resty.ResponseError
		if errors.As(err, &respErr) && respErr.Response != nil {
			status = respErr.Response.StatusCode()
		}
		endRedfishHTTPSpan(r, status, err)
	})
	rc.OnPanic(func(r *resty.Request, err error) {
		endRedfishHTTPSpan(r, 0, err)
	})
}

// beginRedfishSpan starts a dpu.redfish child and stores it on the request.
// A missing request span is filled from traceParent without dropping the request's deadline.
func (c *Client) beginRedfishSpan(rc *resty.Client, r *resty.Request) {
	parent := r.Context()
	if !trace.SpanFromContext(parent).SpanContext().IsValid() && c.traceParent != nil {
		if span := trace.SpanFromContext(c.traceParent); span.SpanContext().IsValid() {
			parent = trace.ContextWithSpan(parent, span)
		}
	}
	ctx, _ := tracing.StartSpan(parent, redfishSpanName(r), redfishAttributes(rc, r, c.BaseURL, c.mtlsUser)...)
	// RawRequest is built after this hook and copies r.Context().
	r.SetContext(ctx)
}

// endRedfishHTTPSpan ends the span stored on r. status 0 means the call produced no HTTP response.
// path is read here because resty fills in path parameters after the span starts.
func endRedfishHTTPSpan(r *resty.Request, status int, err error) {
	if r == nil {
		return
	}
	span := trace.SpanFromContext(r.Context())
	if path := requestPath(r.URL); path != "" {
		span.SetAttributes(attribute.String("path", path))
	}
	if status > 0 {
		span.SetAttributes(attribute.Int("http.response.status_code", status))
	}
	tracing.EndSpan(span, err)
}

// traceParentFrom returns ctx's active span without ctx's cancellation.
// It returns nil when ctx has no span.
func traceParentFrom(ctx context.Context) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}
	return trace.ContextWithSpan(context.Background(), span)
}

// redfishSpanName returns dpu.redfish.<API name> for r.
func redfishSpanName(r *resty.Request) string {
	name := RedfishAPIs.Name(r.URL)
	if name == "" {
		return "dpu.redfish"
	}
	return "dpu.redfish." + name
}

// RedfishAPIMap maps an API name to its path template and the template back to the name.
type RedfishAPIMap struct {
	paths  map[string]string
	byPath map[string]string
}

// newRedfishAPIMap returns a map that looks up a path by name and a name by template path.
func newRedfishAPIMap(paths map[string]string) RedfishAPIMap {
	byPath := make(map[string]string, len(paths))
	for name, path := range paths {
		byPath[strings.Trim(path, "/")] = name
	}
	return RedfishAPIMap{
		paths:  paths,
		byPath: byPath,
	}
}

// Path returns the request path for name.
func (m RedfishAPIMap) Path(name string) string {
	return m.paths[name]
}

// Name returns the API name for rawURL.
// An unsubstituted template matches byPath exactly. A {TOKEN} segment otherwise
// matches one path segment, and the path with more literal segments wins.
// The query string is ignored.
func (m RedfishAPIMap) Name(rawURL string) string {
	request := strings.Trim(requestPath(rawURL), "/")
	if request == "" {
		return ""
	}
	if name, ok := m.byPath[request]; ok {
		return name
	}
	requestSegments := strings.Split(request, "/")
	bestName := ""
	bestLiterals := -1
	for name, pattern := range m.paths {
		literals, ok := redfishPathLiterals(pattern, requestSegments)
		if ok && literals > bestLiterals {
			bestLiterals = literals
			bestName = name
		}
	}
	return bestName
}

// redfishPathLiterals reports how many literal segments of pattern equal requestSegments.
// A {TOKEN} segment matches any one request segment. ok is false when the paths differ.
func redfishPathLiterals(pattern string, requestSegments []string) (literals int, ok bool) {
	patternSegments := strings.Split(strings.Trim(pattern, "/"), "/")
	if len(patternSegments) != len(requestSegments) {
		return 0, false
	}
	for i, segment := range patternSegments {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			continue
		}
		if segment != requestSegments[i] {
			return 0, false
		}
		literals++
	}
	return literals, true
}

// requestPath returns rawURL's path with the query string removed.
func requestPath(rawURL string) string {
	if parsed, err := url.Parse(rawURL); err == nil {
		if parsed.Path != "" {
			return parsed.Path
		}
		if parsed.Host != "" || parsed.RawQuery != "" {
			return ""
		}
	} else if i := strings.Index(rawURL, "?"); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}

// redfishAttributes returns method, host, port, and user for the BMC call.
// user is omitted when the client has no basic-auth username, URL username, or mTLS user.
func redfishAttributes(rc *resty.Client, r *resty.Request, baseURL, mtlsUser string) []attribute.KeyValue {
	method := r.Method
	if method == "" {
		method = "HTTP"
	}
	attrs := []attribute.KeyValue{attribute.String("method", method)}
	host, port := bmcHostPort(baseURL)
	if host != "" {
		attrs = append(attrs, attribute.String("host", host))
	}
	if port != "" {
		attrs = append(attrs, attribute.String("port", port))
	}
	if user := redfishUser(rc, baseURL, mtlsUser); user != "" {
		attrs = append(attrs, attribute.String("user", user))
	}
	return attrs
}

// bmcHostPort returns the BMC host and port. A missing port defaults from the scheme.
// Credentials and the query string are omitted. An empty address returns empty strings.
func bmcHostPort(address string) (host, port string) {
	if address == "" {
		return "", ""
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("https://" + address)
		if err != nil || parsed.Host == "" {
			return "", ""
		}
	}
	host = parsed.Hostname()
	port = parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https", "":
			port = "443"
		}
	}
	return host, port
}

// redfishUser returns the BMC username from basic auth, then from the base URL userinfo, then
// mtlsUser.
func redfishUser(rc *resty.Client, baseURL, mtlsUser string) string {
	if rc != nil && rc.UserInfo != nil && rc.UserInfo.Username != "" {
		return rc.UserInfo.Username
	}
	if parsed, err := url.Parse(baseURL); err == nil && parsed.User != nil && parsed.User.Username() != "" {
		return parsed.User.Username()
	}
	return mtlsUser
}
