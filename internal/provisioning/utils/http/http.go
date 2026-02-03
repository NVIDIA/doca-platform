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

package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// CloseBody fully reads and closes the response body to enable HTTP connection reuse.
// For connections to be returned to the pool, the body must be fully read before closing.
// This function safely handles nil responses and bodies.
func CloseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// GetContentLength performs a HEAD request to get the expected file size from the server.
// Returns:
//   - >0: Known file size from Content-Length header
//   - 0: Server explicitly reported empty file (Content-Length: 0)
//   - -1: Content-Length header was not set (unknown size), or an error occurred
//
// Returns an error if the HEAD request fails or returns a non-200 status code.
// On error, returns -1 to indicate unknown size.
func GetContentLength(ctx context.Context, url string) (int64, error) {
	resp, err := DoRequest(ctx, "HEAD", url)
	if err != nil {
		return -1, err
	}
	defer CloseBody(resp)
	return resp.ContentLength, nil
}

// CheckHTTPResponse validates the HTTP response status code against expected values.
// If no expected statuses are provided, http.StatusOK is used as the default.
// Returns nil if the response status matches any of the expected statuses.
// On error, reads up to 1KB of the response body for better error context.
func CheckHTTPResponse(resp *http.Response, expectedStatus ...int) error {
	if resp == nil {
		return fmt.Errorf("nil HTTP response")
	}

	// Default to StatusOK if no expected statuses provided
	expected := []int{http.StatusOK}
	if len(expectedStatus) > 0 {
		expected = expectedStatus
	}

	// Check if response status matches any expected status
	for _, status := range expected {
		if resp.StatusCode == status {
			return nil
		}
	}

	// Read limited body for error context
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	url := ""
	if resp.Request != nil && resp.Request.URL != nil {
		url = resp.Request.URL.String()
	}

	return fmt.Errorf("unexpected HTTP status %d (%s) from %s: %s",
		resp.StatusCode,
		http.StatusText(resp.StatusCode),
		url,
		string(body))
}

// DoRequest performs an HTTP request with context and validates the response status.
// It creates the request, executes it, and checks the response status in one call.
//
// IMPORTANT: The caller MUST check the error before using the response.
// On success (err == nil): caller is responsible for closing resp.Body.
// On error (err != nil): returns nil response (body already closed internally).
//
// Example usage:
//
//	resp, err := DoRequest(ctx, "GET", url)
//	if err != nil {
//	    return err  // resp is nil, nothing to close
//	}
//	defer resp.Body.Close()  // Only after error check!
//
// Returns the response and nil error if the status matches expected (default: 200 OK).
func DoRequest(ctx context.Context, method, url string, expectedStatus ...int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if err := CheckHTTPResponse(resp, expectedStatus...); err != nil {
		CloseBody(resp)
		return nil, err
	}

	return resp, nil
}

// DoRequestWithRetry performs an HTTP request with retry logic.
// It retries on network errors and unexpected HTTP status codes.
//
// Parameters:
//   - maxRetries: maximum number of retry attempts (0 means no retries, just one attempt)
//   - retryInterval: initial wait time between retries (doubles with each retry)
//
// IMPORTANT: The caller MUST check the error before using the response.
// On success (err == nil): caller is responsible for closing resp.Body.
// On error (err != nil): returns nil response.
//
// Example usage:
//
//	resp, err := DoRequestWithRetry(ctx, "GET", url, 3, time.Second)
//	if err != nil {
//	    return err
//	}
//	defer resp.Body.Close()
func DoRequestWithRetry(ctx context.Context, method, url string,
	maxRetries int, retryInterval time.Duration, expectedStatus ...int) (*http.Response, error) {

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check context before each attempt
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := DoRequest(ctx, method, url, expectedStatus...)
		if err == nil {
			// Log response headers for debugging download issues:
			// - Content-Length: expected body size in bytes (RFC 7230)
			// - Content-Type: media type of the resource (RFC 7231)
			// - ETag: opaque resource version identifier for cache validation (RFC 7232)
			klog.V(3).Infof("HTTP %s %s: Status=%d, Content-Length=%d, Content-Type=%q, ETag=%q",
				method, url, resp.StatusCode, resp.ContentLength,
				resp.Header.Get("Content-Type"), resp.Header.Get("ETag"))
			return resp, nil
		}
		lastErr = err

		// Don't retry if parent context is canceled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Log and wait before retry (except for last attempt)
		if attempt < maxRetries {
			klog.V(3).Infof("HTTP %s %s failed (attempt %d/%d): %v, retrying in %v",
				method, url, attempt+1, maxRetries+1, err, retryInterval)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryInterval):
				// Exponential backoff: double the interval for next retry
				retryInterval *= 2
			}
		}
	}

	return nil, fmt.Errorf("HTTP %s %s failed after %d attempts: %w", method, url, maxRetries+1, lastErr)
}
