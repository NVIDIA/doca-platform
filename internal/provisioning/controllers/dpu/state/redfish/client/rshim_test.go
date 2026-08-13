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
	"net/http"
	"net/http/httptest"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetBMCRShimEnabled", func() {
	It("returns true when BmcRShimEnabled is true", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			Expect(req.Method).To(Equal(http.MethodGet))
			Expect(req.URL.Path).To(Equal("/" + APIEnableBMCRshim))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"BmcRShim":{"BmcRShimEnabled":true}}`))
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}
		enabled, resp, err := client.GetBMCRShimEnabled()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))
		Expect(enabled).To(BeTrue())
	})

	It("returns false when BmcRShimEnabled is false", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"BmcRShim":{"BmcRShimEnabled":false}}`))
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}
		enabled, resp, err := client.GetBMCRShimEnabled()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))
		Expect(enabled).To(BeFalse())
	})

	It("returns an error on non-200", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"BmcRShim":{"BmcRShimEnabled":false}}`))
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}
		_, resp, err := client.GetBMCRShimEnabled()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unexpected status code"))
		Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))
	})

	It("returns an error when the body cannot be decoded", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}
		_, resp, err := client.GetBMCRShimEnabled()
		Expect(err).To(HaveOccurred())
		Expect(resp).NotTo(BeNil())
	})

	It("returns an error when BmcRShimEnabled is missing", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"BmcRShim":{}}`))
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}
		_, resp, err := client.GetBMCRShimEnabled()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("BmcRShimEnabled missing"))
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))
	})
})
