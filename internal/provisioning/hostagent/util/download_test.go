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

package util

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Download", func() {
	var (
		tempDir string
		server  *httptest.Server
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "download-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
		_ = os.RemoveAll(tempDir)
	})

	Context("DownloadFile", Label("DownloadFile"), func() {
		It("should download file successfully", func() {
			testContent := "test file content"
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testContent))
			}))

			dst := filepath.Join(tempDir, "downloaded_file.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(testContent))
		})

		It("should skip download when destination file already exists", func() {
			dst := filepath.Join(tempDir, "existing_file.txt")
			existingContent := "existing content"
			err := os.WriteFile(dst, []byte(existingContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			// Server should not be called at all since file exists
			serverCalled := false
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serverCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("new content"))
			}))

			err = DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())
			Expect(serverCalled).To(BeFalse())

			// Verify existing content was not overwritten
			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(existingContent))
		})

		It("should create destination directory if it does not exist", func() {
			testContent := "nested content"
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testContent))
			}))

			dst := filepath.Join(tempDir, "nested", "dir", "file.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(testContent))
		})

		It("should return error for HTTP 404 response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			dst := filepath.Join(tempDir, "not_found.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("404"))

			// Verify file was not created
			_, err = os.Stat(dst)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should return error for HTTP 500 response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			dst := filepath.Join(tempDir, "server_error.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("500"))
		})

		It("should return error for invalid URL", func() {
			dst := filepath.Join(tempDir, "invalid_url.txt")
			err := DownloadFile(context.Background(), "http://invalid.invalid.invalid:99999/file", dst, 0644)
			Expect(err).To(HaveOccurred())
		})

		It("should return error for malformed URL", func() {
			dst := filepath.Join(tempDir, "malformed.txt")
			err := DownloadFile(context.Background(), "://malformed-url", dst, 0644)
			Expect(err).To(HaveOccurred())
		})

		It("should set correct file permissions", func() {
			testContent := "permission test"
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testContent))
			}))

			dst := filepath.Join(tempDir, "permission_test.txt")
			expectedMode := os.FileMode(0600)
			err := DownloadFile(context.Background(), server.URL, dst, expectedMode)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(expectedMode))
		})

		It("should handle context cancellation before download starts", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("content"))
			}))

			ctx, cancel := context.WithCancel(context.Background())
			// Cancel immediately before download starts
			cancel()

			dst := filepath.Join(tempDir, "cancelled.txt")
			err := DownloadFile(ctx, server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
		})

		It("should handle interrupt during download", func() {
			// Create a server that sends data in chunks with delays
			// This simulates a slow download that can be interrupted mid-stream
			chunkSize := 1024
			totalChunks := 10
			chunk := make([]byte, chunkSize)
			for i := range chunk {
				chunk[i] = 'x'
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "HEAD" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusOK)
				// Send data in chunks with delays to allow cancellation mid-download
				for i := 0; i < totalChunks; i++ {
					select {
					case <-r.Context().Done():
						return
					default:
						_, _ = w.Write(chunk)
						if f, ok := w.(http.Flusher); ok {
							f.Flush()
						}
						time.Sleep(50 * time.Millisecond)
					}
				}
			}))

			ctx, cancel := context.WithCancel(context.Background())

			// Cancel after a short delay to interrupt mid-download
			go func() {
				time.Sleep(100 * time.Millisecond)
				cancel()
			}()

			dst := filepath.Join(tempDir, "interrupted.txt")
			err := DownloadFile(ctx, server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("download canceled"),
				ContainSubstring("context canceled"),
			))

			// Verify destination file was not created (temp file cleaned up)
			_, statErr := os.Stat(dst)
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("should handle large file download", func() {
			// Create content larger than a single read buffer chunk
			largeContent := make([]byte, 1024*1024) // 1MB
			for i := range largeContent {
				largeContent[i] = byte(i % 256)
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(largeContent)
			}))

			dst := filepath.Join(tempDir, "large_file.bin")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(largeContent))
		})

		It("should handle empty response body", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Empty body
			}))

			dst := filepath.Join(tempDir, "empty_file.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(BeEmpty())
		})

		It("should clean up temp file on HTTP error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))

			dst := filepath.Join(tempDir, "forbidden.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())

			// Check no temp files left behind
			files, err := os.ReadDir(tempDir)
			Expect(err).NotTo(HaveOccurred())
			for _, f := range files {
				Expect(f.Name()).NotTo(ContainSubstring(".tmp"))
			}
		})

		It("should return error when downloaded size does not match Content-Length", func() {
			actualContent := "this is the full content that will be sent"
			// Set Content-Length smaller than actual content to simulate size mismatch
			wrongSize := len(actualContent) - 10
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", wrongSize))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(actualContent))
			}))

			dst := filepath.Join(tempDir, "size_mismatch.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
			// Go's HTTP client may catch this as "unexpected EOF" or our validation catches it
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("downloaded file size mismatch"),
				ContainSubstring("unexpected EOF"),
			))

			// Verify file was not created (temp file should be cleaned up)
			_, err = os.Stat(dst)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should succeed when Content-Length matches downloaded size", func() {
			testContent := "exact size content"
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testContent))
			}))

			dst := filepath.Join(tempDir, "exact_size.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(testContent))
		})

		It("should return error when server truncates response (sends less than Content-Length)", func() {
			// This simulates the bug scenario where a 1.5GB file ends up as 555 bytes
			// Server claims to send 10000 bytes but only sends 100, then closes connection
			expectedSize := 10000
			actualSent := 100
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", expectedSize))
				w.WriteHeader(http.StatusOK)
				// Write partial content and let the connection close
				_, _ = w.Write(make([]byte, actualSent))
				// Connection closes here, simulating network drop or server-side truncation
			}))

			dst := filepath.Join(tempDir, "truncated.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
			// Go's HTTP client detects early connection close as "unexpected EOF"
			// or our validation catches it as size mismatch
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("downloaded file size mismatch"),
				ContainSubstring("unexpected EOF"),
				ContainSubstring("EOF"),
			))

			// Verify no corrupt file was created at destination
			_, statErr := os.Stat(dst)
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("should handle download without Content-Length header", func() {
			// Some servers don't set Content-Length (chunked encoding or unknown size)
			testContent := "content without length header"
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Don't set Content-Length, let Go handle chunked transfer
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testContent))
			}))

			dst := filepath.Join(tempDir, "no_content_length.txt")
			err := DownloadFile(context.Background(), server.URL, dst, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(testContent))
		})
	})
})
