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
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

const defaultProxyDialTimeout = 30 * time.Second

type forwardProxyHandler struct {
	dialer    *net.Dialer
	transport *http.Transport
}

func newForwardProxyHandler() http.Handler {
	dialer := &net.Dialer{
		Timeout:   defaultProxyDialTimeout,
		KeepAlive: defaultProxyDialTimeout,
	}
	return &forwardProxyHandler{
		dialer: dialer,
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   defaultProxyDialTimeout,
			ResponseHeaderTimeout: 0,
			DisableCompression:    true,
		},
	}
}

func (h *forwardProxyHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect {
		h.handleConnect(resp, req)
		return
	}
	h.handleHTTP(resp, req)
}

func (h *forwardProxyHandler) handleConnect(resp http.ResponseWriter, req *http.Request) {
	target := req.Host
	if target == "" && req.URL != nil {
		target = req.URL.Host
	}
	if target == "" {
		http.Error(resp, "missing CONNECT target", http.StatusBadRequest)
		return
	}

	upstream, err := h.dial(req.Context(), target)
	if err != nil {
		klog.Warningf("Failed to connect proxy upstream %s: %v", target, err)
		http.Error(resp, fmt.Sprintf("connect upstream: %v", err), http.StatusBadGateway)
		return
	}

	hijacker, ok := resp.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(resp, "response writer does not support hijacking", http.StatusInternalServerError)
		return
	}

	client, bufrw, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		http.Error(resp, "failed to hijack client connection", http.StatusInternalServerError)
		return
	}

	if _, err := bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		klog.Warningf("Failed to write CONNECT response for %s: %v", target, err)
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	if err := bufrw.Flush(); err != nil {
		klog.Warningf("Failed to flush CONNECT response for %s: %v", target, err)
		_ = client.Close()
		_ = upstream.Close()
		return
	}

	klog.V(3).Infof("Established CONNECT proxy tunnel to %s", target)
	proxyTunnel(client, upstream)
}

func (h *forwardProxyHandler) handleHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.URL == nil || req.URL.Scheme == "" || req.URL.Host == "" {
		http.Error(resp, "forward proxy requires an absolute-form request URI", http.StatusBadRequest)
		return
	}

	outReq := req.Clone(req.Context())
	outReq.RequestURI = ""
	removeHopByHopHeaders(outReq.Header)

	upstreamResp, err := h.transport.RoundTrip(outReq)
	if err != nil {
		klog.Warningf("Failed to forward proxy request %s %s: %v", req.Method, req.URL.String(), err)
		http.Error(resp, fmt.Sprintf("forward request: %v", err), http.StatusBadGateway)
		return
	}
	defer func() {
		_ = upstreamResp.Body.Close()
	}()

	removeHopByHopHeaders(upstreamResp.Header)
	copyHeader(resp.Header(), upstreamResp.Header)
	resp.WriteHeader(upstreamResp.StatusCode)
	if _, err := io.Copy(resp, upstreamResp.Body); err != nil {
		klog.V(3).Infof("Failed to stream proxy response for %s: %v", req.URL.String(), err)
	}
}

func (h *forwardProxyHandler) dial(ctx context.Context, address string) (net.Conn, error) {
	return h.dialer.DialContext(ctx, "tcp", address)
}

func proxyTunnel(client net.Conn, upstream net.Conn) {
	defer func() {
		_ = client.Close()
	}()
	defer func() {
		_ = upstream.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go copyAndCloseWrite(upstream, client, &wg)
	go copyAndCloseWrite(client, upstream, &wg)
	wg.Wait()
}

func copyAndCloseWrite(dst net.Conn, src net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
	if tcp, ok := dst.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
		return
	}
	_ = dst.Close()
}

func copyHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func removeHopByHopHeaders(header http.Header) {
	if connection := header.Get("Connection"); connection != "" {
		for _, name := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(name)
	}
}
