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

package util

import (
	"context"
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

			// Server that should not be called
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
			Expect(err.Error()).To(ContainSubstring("status: 404"))

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
			Expect(err.Error()).To(ContainSubstring("status: 500"))
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

		It("should handle context cancellation", func() {
			// Create a server that delays response
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Write header but delay body - this allows the request to start
				// but gives time for cancellation
				time.Sleep(100 * time.Millisecond)
				_, _ = w.Write([]byte("delayed content"))
			}))

			ctx, cancel := context.WithCancel(context.Background())
			// Cancel immediately
			cancel()

			dst := filepath.Join(tempDir, "cancelled.txt")
			err := DownloadFile(ctx, server.URL, dst, 0644)
			Expect(err).To(HaveOccurred())
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
	})
})
