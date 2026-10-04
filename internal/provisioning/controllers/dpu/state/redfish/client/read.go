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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// ReadTimeout is the total budget for a logical read, including prerequisite GETs.
const ReadTimeout = 30 * time.Second
const readConnectTimeout = 10 * time.Second

// ReadContext bounds a logical read, including acquisition and prerequisite GETs.
// Keep the parent context for mutations and status persistence.
func ReadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, ReadTimeout)
}

// RequestError retains the HTTP result independently of a transport/decode failure.
// It deliberately contains neither the response body nor credentials/request URL.
type RequestError struct {
	Operation  string
	StatusCode int
	Code       string
	MessageIDs []string
	Cause      error
}

// Error describes the failure without exposing response bodies or credential-bearing URLs.
func (e *RequestError) Error() string {
	// The wrapped transport error may contain a credential-bearing URL. Preserve it
	// for errors.Is/As, but do not automatically render it into controller logs.
	reason := "unexpected HTTP status"
	if e.Cause != nil {
		reason = "request or response failure"
		if errors.Is(e.Cause, context.DeadlineExceeded) {
			reason = "deadline exceeded"
		} else if errors.Is(e.Cause, context.Canceled) {
			reason = "canceled"
		}
	}
	return fmt.Sprintf("Redfish %s: %s (HTTP %d)", e.Operation, reason, e.StatusCode)
}

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (e *RequestError) Unwrap() error { return e.Cause }

// HasHTTPStatus only matches a completed HTTP failure, never a partially read
// response whose status happened to be received before a transport error.
func HasHTTPStatus(err error, status int) bool {
	var requestErr *RequestError
	return errors.As(err, &requestErr) && requestErr.Cause == nil && requestErr.StatusCode == status
}

// requestError extracts HTTP status and Redfish identifiers without retaining the body.
func requestError(resp *resty.Response, cause error) *RequestError {
	e := &RequestError{Operation: http.MethodGet, Cause: cause}
	if resp == nil || resp.RawResponse == nil {
		return e
	}
	e.StatusCode = resp.StatusCode()
	var payload struct {
		RedfishError
		ExtendedInfo []MessageExtendedInfo `json:"@Message.ExtendedInfo"`
	}
	if json.Unmarshal(resp.Body(), &payload) == nil {
		e.Code = payload.Error.Code
		for _, info := range append(payload.Error.ExtendedInfo, payload.ExtendedInfo...) {
			if info.MessageID != "" {
				e.MessageIDs = append(e.MessageIDs, info.MessageID)
			}
		}
	}
	return e
}

// readClient is initialized once, before any read, from the immutable client
// configuration. Its transport/deadlines never modify the mutation client.
func (c *Client) readClient() *resty.Client {
	c.readOnce.Do(func() {
		hc := *c.Client.GetClient()
		if transport, ok := hc.Transport.(*http.Transport); ok {
			transport = transport.Clone()
			transport.DialContext = (&net.Dialer{Timeout: readConnectTimeout, KeepAlive: 30 * time.Second}).DialContext
			transport.TLSHandshakeTimeout = readConnectTimeout
			transport.MaxConnsPerHost = 1
			transport.MaxIdleConnsPerHost = 1
			hc.Transport = transport
		}
		hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		c.reader = resty.NewWithClient(&hc).SetBaseURL(c.BaseURL)
		c.reader.Header = c.Header.Clone()
		if c.UserInfo != nil {
			c.reader.SetBasicAuth(c.UserInfo.Username, c.UserInfo.Password)
		}
	})
	return c.reader
}

// CloseIdleConnections releases this client's idle connections without
// interrupting requests already in progress.
func (c *Client) CloseIdleConnections() {
	c.Client.GetClient().CloseIdleConnections()
	c.readClient().GetClient().CloseIdleConnections()
}

// read performs one bounded GET and decodes a non-null HTTP 200 payload.
func read[T any](ctx context.Context, c *Client, path string) (*resty.Response, *T, error) {
	// Composite operations supply an earlier shared deadline; this also bounds
	// standalone requests without repeating timeout setup in every GET method.
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, nil, requestError(nil, err)
	}
	// Collection member links must not send authentication to another host.
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(path, "//") {
		return nil, nil, requestError(nil, errors.New("expected a relative Redfish resource URI"))
	}
	resp, err := c.readClient().R().SetContext(ctx).Get(path)
	if err != nil {
		return resp, nil, requestError(resp, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return resp, nil, requestError(resp, nil)
	}
	var result *T
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return resp, nil, requestError(resp, err)
	}
	if result == nil {
		return resp, nil, requestError(resp, errors.New("missing required Redfish payload"))
	}
	return resp, result, nil
}
