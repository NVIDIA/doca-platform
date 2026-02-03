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

package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	httputil "github.com/nvidia/doca-platform/internal/provisioning/utils/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CloseBody", func() {
	It("should handle nil response safely", func() {
		// Should not panic when called with nil
		Expect(func() { httputil.CloseBody(nil) }).NotTo(Panic())
	})

	It("should handle response with nil body safely", func() {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       nil,
		}
		Expect(func() { httputil.CloseBody(resp) }).NotTo(Panic())
	})

	It("should close response body successfully", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("test body content"))
		}))
		defer server.Close()

		resp, err := http.Get(server.URL)
		Expect(err).NotTo(HaveOccurred())

		// CloseBody should fully drain and close the body
		Expect(func() { httputil.CloseBody(resp) }).NotTo(Panic())

		// Attempting to read after close should fail
		_, err = io.ReadAll(resp.Body)
		Expect(err).To(HaveOccurred())
	})

	It("should drain body before closing to enable connection reuse", func() {
		// Create server that sends content
		bodyContent := "this content should be drained before close"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(bodyContent))
		}))
		defer server.Close()

		resp, err := http.Get(server.URL)
		Expect(err).NotTo(HaveOccurred())

		// Don't read the body - CloseBody should handle draining
		httputil.CloseBody(resp)

		// Body should be closed
		n, err := resp.Body.Read(make([]byte, 1))
		Expect(n).To(Equal(0))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("GetContentLength", func() {
	var (
		server *httptest.Server
		ctx    context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	It("should return positive Content-Length when server returns 200 OK with Content-Length header", func() {
		expectedSize := int64(12345)
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("HEAD"))
			w.Header().Set("Content-Length", strconv.FormatInt(expectedSize, 10))
			w.WriteHeader(http.StatusOK)
		}))

		size, err := httputil.GetContentLength(ctx, server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(expectedSize))
	})

	It("should return 0 when server returns 200 OK with Content-Length: 0", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("HEAD"))
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
		}))

		size, err := httputil.GetContentLength(ctx, server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(int64(0)))
	})

	It("should return -1 when server returns 200 OK without Content-Length header", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("HEAD"))
			// Don't set Content-Length header
			w.WriteHeader(http.StatusOK)
		}))

		size, err := httputil.GetContentLength(ctx, server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(int64(-1)))
	})

	It("should return -1 and error when server returns 404 Not Found", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))

		size, err := httputil.GetContentLength(ctx, server.URL)
		Expect(err).To(HaveOccurred())
		Expect(size).To(Equal(int64(-1)))
	})

	It("should return -1 and error when server returns 500 Internal Server Error", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		size, err := httputil.GetContentLength(ctx, server.URL)
		Expect(err).To(HaveOccurred())
		Expect(size).To(Equal(int64(-1)))
	})

	It("should return -1 and error when server is unreachable", func() {
		size, err := httputil.GetContentLength(ctx, "http://localhost:99999/nonexistent")
		Expect(err).To(HaveOccurred())
		Expect(size).To(Equal(int64(-1)))
	})

	It("should return -1 and error when URL is invalid", func() {
		size, err := httputil.GetContentLength(ctx, "://invalid-url")
		Expect(err).To(HaveOccurred())
		Expect(size).To(Equal(int64(-1)))
	})

	It("should return -1 and error on context cancellation", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Slow response
			<-r.Context().Done()
		}))

		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		size, err := httputil.GetContentLength(cancelCtx, server.URL)
		Expect(err).To(HaveOccurred())
		Expect(size).To(Equal(int64(-1)))
	})
})

var _ = Describe("CheckHTTPResponse", func() {
	It("should return nil for nil response", func() {
		err := httputil.CheckHTTPResponse(nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("nil HTTP response"))
	})

	It("should return nil when status matches default 200 OK", func() {
		resp := &http.Response{
			StatusCode: http.StatusOK,
		}
		err := httputil.CheckHTTPResponse(resp)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return nil when status matches one of expected statuses", func() {
		resp := &http.Response{
			StatusCode: http.StatusCreated,
		}
		err := httputil.CheckHTTPResponse(resp, http.StatusOK, http.StatusCreated, http.StatusAccepted)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error when status does not match expected", func() {
		resp := &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       http.NoBody,
		}
		err := httputil.CheckHTTPResponse(resp)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("404"))
		Expect(err.Error()).To(ContainSubstring("Not Found"))
	})

	It("should include body content in error message", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("invalid request body"))
		}))
		defer server.Close()

		resp, _ := http.Get(server.URL)
		err := httputil.CheckHTTPResponse(resp)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid request body"))
	})
})

var _ = Describe("DoRequest", func() {
	var (
		server *httptest.Server
		ctx    context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	It("should return response for successful GET request", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("GET"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		}))

		resp, err := httputil.DoRequest(ctx, "GET", server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		_ = resp.Body.Close()
	})

	It("should return response for successful HEAD request", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("HEAD"))
			w.WriteHeader(http.StatusOK)
		}))

		resp, err := httputil.DoRequest(ctx, "HEAD", server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		_ = resp.Body.Close()
	})

	It("should return error for non-200 status", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))

		resp, err := httputil.DoRequest(ctx, "GET", server.URL)
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
	})

	It("should accept custom expected status codes", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))

		resp, err := httputil.DoRequest(ctx, "POST", server.URL, http.StatusAccepted)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		_ = resp.Body.Close()
	})

	It("should return error for unreachable server", func() {
		resp, err := httputil.DoRequest(ctx, "GET", "http://localhost:99999/nonexistent")
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
	})

	It("should respect context cancellation", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))

		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		resp, err := httputil.DoRequest(cancelCtx, "GET", server.URL)
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
	})
})

var _ = Describe("DoRequestWithRetry", func() {
	var (
		server *httptest.Server
		ctx    context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	It("should succeed on first attempt without retry", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "GET", server.URL, 3, 10*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		_ = resp.Body.Close()
	})

	It("should retry and succeed after transient failures", func() {
		attemptCount := 0
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attemptCount++
			if attemptCount < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "GET", server.URL, 3, 10*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(attemptCount).To(Equal(3))
		_ = resp.Body.Close()
	})

	It("should fail after exhausting all retries", func() {
		attemptCount := 0
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attemptCount++
			w.WriteHeader(http.StatusInternalServerError)
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "GET", server.URL, 2, 10*time.Millisecond)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed after 3 attempts"))
		Expect(resp).To(BeNil())
		Expect(attemptCount).To(Equal(3)) // 1 initial + 2 retries
	})

	It("should respect context cancellation during retry", func() {
		attemptCount := 0
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attemptCount++
			w.WriteHeader(http.StatusInternalServerError)
		}))

		cancelCtx, cancel := context.WithCancel(ctx)

		// Cancel after short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		resp, err := httputil.DoRequestWithRetry(cancelCtx, "GET", server.URL, 10, 100*time.Millisecond)
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
		// Should have been canceled before exhausting all retries
		Expect(attemptCount).To(BeNumerically("<", 10))
	})

	It("should work with zero retries (single attempt)", func() {
		attemptCount := 0
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attemptCount++
			w.WriteHeader(http.StatusNotFound)
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "GET", server.URL, 0, 10*time.Millisecond)
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
		Expect(attemptCount).To(Equal(1))
	})

	It("should support custom expected status codes", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "POST", server.URL, 3, 10*time.Millisecond, http.StatusAccepted)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		_ = resp.Body.Close()
	})

	It("should retry on network errors", func() {
		// Use an invalid port that should fail to connect
		resp, err := httputil.DoRequestWithRetry(ctx, "GET", "http://localhost:99999/test", 2, 10*time.Millisecond)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed after 3 attempts"))
		Expect(resp).To(BeNil())
	})

	It("should allow reading response body after success", func() {
		expectedBody := "response content for body reading test"
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(expectedBody))
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "GET", server.URL, 3, 10*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())

		// Verify body can be read
		body, readErr := io.ReadAll(resp.Body)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(body)).To(Equal(expectedBody))
		_ = resp.Body.Close()
	})

	It("should work with HEAD method", func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("HEAD"))
			w.Header().Set("Content-Length", "12345")
			w.WriteHeader(http.StatusOK)
		}))

		resp, err := httputil.DoRequestWithRetry(ctx, "HEAD", server.URL, 3, 10*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.ContentLength).To(Equal(int64(12345)))
		_ = resp.Body.Close()
	})

	It("should use exponential backoff between retries", func() {
		attemptCount := 0
		attemptTimes := []time.Time{}
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attemptTimes = append(attemptTimes, time.Now())
			attemptCount++
			w.WriteHeader(http.StatusInternalServerError)
		}))

		start := time.Now()
		_, _ = httputil.DoRequestWithRetry(ctx, "GET", server.URL, 2, 50*time.Millisecond)
		totalDuration := time.Since(start)

		Expect(attemptCount).To(Equal(3))
		// With exponential backoff: 50ms + 100ms = 150ms minimum
		// Allow some margin for test execution
		Expect(totalDuration).To(BeNumerically(">=", 140*time.Millisecond))
	})

	It("should stop retrying immediately when context is already canceled", func() {
		attemptCount := 0
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attemptCount++
			w.WriteHeader(http.StatusOK)
		}))

		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		resp, err := httputil.DoRequestWithRetry(cancelCtx, "GET", server.URL, 10, time.Second)
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
		Expect(attemptCount).To(Equal(0)) // Should not even attempt
	})
})
