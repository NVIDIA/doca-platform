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

package service

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ForwardProxy", func() {
	var proxyServer *http.Server
	var proxyListener net.Listener
	var proxyAddress string

	BeforeEach(func() {
		var err error
		proxyListener, err = net.Listen("tcp", "127.0.0.1:0")
		Expect(err).To(Succeed())
		proxyAddress = proxyListener.Addr().String()
		proxyServer = &http.Server{Handler: newForwardProxyHandler()}
		go func() {
			_ = proxyServer.Serve(proxyListener)
		}()
	})

	AfterEach(func() {
		Expect(proxyServer.Close()).To(Succeed())
	})

	proxyURL := func() *url.URL {
		u, err := url.Parse(fmt.Sprintf("http://%s", proxyAddress))
		Expect(err).To(Succeed())
		return u
	}

	It("should forward absolute-form HTTP requests", func() {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.String()).To(Equal("/pkg.deb"))
			w.Header().Set("X-Test-Proxy", "ok")
			_, _ = w.Write([]byte("package-data"))
		}))
		defer upstream.Close()

		client := &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL())},
		}
		defer client.CloseIdleConnections()
		resp, err := client.Get(upstream.URL + "/pkg.deb")
		Expect(err).To(Succeed())
		defer func() {
			_ = resp.Body.Close()
		}()

		body, err := io.ReadAll(resp.Body)
		Expect(err).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("X-Test-Proxy")).To(Equal("ok"))
		Expect(string(body)).To(Equal("package-data"))
	})

	It("should tunnel HTTPS requests with CONNECT", func() {
		upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.String()).To(Equal("/api"))
			_, _ = w.Write([]byte("api-data"))
		}))
		defer upstream.Close()

		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL()),
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // test server uses a self-signed certificate.
				},
			},
		}
		defer client.CloseIdleConnections()
		resp, err := client.Get(upstream.URL + "/api")
		Expect(err).To(Succeed())
		defer func() {
			_ = resp.Body.Close()
		}()

		body, err := io.ReadAll(resp.Body)
		Expect(err).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(string(body)).To(Equal("api-data"))
	})
})
