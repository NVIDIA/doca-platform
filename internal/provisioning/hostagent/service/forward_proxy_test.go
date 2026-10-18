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
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"time"

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

	allowAllDestinations := func() {
		original := allowProxyDestination
		allowProxyDestination = func(netip.Addr) bool { return true }
		DeferCleanup(func() { allowProxyDestination = original })
	}

	It("should fall back to a reachable address without waiting for an unreachable one", func() {
		allowAllDestinations()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).To(Succeed())
		defer listener.Close() //nolint:errcheck
		listenAddr := netip.MustParseAddrPort(listener.Addr().String())

		h := newForwardProxyHandler().(*forwardProxyHandler)
		unroutable := netip.MustParseAddr("192.0.2.1")
		start := time.Now()
		conn, err := h.dialFirst(context.Background(), []netip.Addr{unroutable, listenAddr.Addr()}, listenAddr.Port())
		Expect(err).To(Succeed())
		_ = conn.Close()
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	})

	It("should reject destinations on the host itself", func() {
		var upstreamHit atomic.Bool
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			upstreamHit.Store(true)
		}))
		defer upstream.Close()
		_, port, err := net.SplitHostPort(upstream.Listener.Addr().String())
		Expect(err).To(Succeed())

		client := &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL())},
		}
		defer client.CloseIdleConnections()
		for _, host := range []string{"127.0.0.1", "localhost"} {
			resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/")
			Expect(err).To(Succeed())
			_ = resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadGateway), "host %q", host)
		}

		_, err = client.Get("https://" + net.JoinHostPort("localhost", port) + "/")
		Expect(err).To(MatchError(ContainSubstring("Bad Gateway")))
		Expect(upstreamHit.Load()).To(BeFalse())
	})

	DescribeTable("allowProxyDestination",
		func(addr string, allowed bool) {
			Expect(allowProxyDestination(netip.MustParseAddr(addr))).To(Equal(allowed))
		},
		Entry("public IPv4", "8.8.8.8", true),
		Entry("private IPv4", "10.96.0.1", true),
		Entry("public IPv6", "2001:4860:4860::8888", true),
		Entry("IPv4 loopback", "127.0.0.1", false),
		Entry("IPv6 loopback", "::1", false),
		Entry("IPv4-mapped loopback", "::ffff:127.0.0.1", false),
		Entry("unspecified", "0.0.0.0", false),
		Entry("cloud metadata", "169.254.169.254", false),
		Entry("IPv6 link-local", "fe80::1", false),
		Entry("multicast", "224.0.0.1", false),
	)

	It("should forward absolute-form HTTP requests", func() {
		allowAllDestinations()
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
		allowAllDestinations()
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
