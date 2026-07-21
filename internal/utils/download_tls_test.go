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

package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

// tlsServerCAFile starts an httptest TLS server and writes its (self-signed) certificate as a
// PEM CA bundle into dir, returning the server and the bundle path. Because the server cert is
// self-signed it acts as its own CA, so trusting it validates the server.
func tlsServerCAFile(t *testing.T, g *WithT, dir, body string) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	g.Expect(server.Certificate()).NotTo(BeNil())
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	caPath := filepath.Join(dir, "ca.crt")
	g.Expect(os.WriteFile(caPath, caPEM, 0o644)).To(Succeed())
	return server, caPath
}

func TestHTTPClientWithCABundle(t *testing.T) {
	t.Run("returns a system-roots client when the path is empty", func(t *testing.T) {
		g := NewWithT(t)
		client, err := HTTPClientWithCABundle("")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(client).NotTo(BeNil())
	})

	t.Run("falls back to system roots when the bundle file is missing", func(t *testing.T) {
		g := NewWithT(t)
		client, err := HTTPClientWithCABundle(filepath.Join(t.TempDir(), "absent.crt"))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(client).NotTo(BeNil())
	})

	t.Run("errors when the bundle contains no valid certificates", func(t *testing.T) {
		g := NewWithT(t)
		path := filepath.Join(t.TempDir(), "garbage.crt")
		g.Expect(os.WriteFile(path, []byte("not a pem certificate"), 0o644)).To(Succeed())
		_, err := HTTPClientWithCABundle(path)
		g.Expect(err).To(HaveOccurred())
	})

	t.Run("trusts a CA present in the bundle", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		server, caPath := tlsServerCAFile(t, g, dir, "ok")

		client, err := HTTPClientWithCABundle(caPath)
		g.Expect(err).NotTo(HaveOccurred())

		resp, err := client.Get(server.URL)
		g.Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
}

func TestDownloadFileWithClient_HTTPS(t *testing.T) {
	t.Run("downloads over HTTPS when the CA is trusted", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		body := "bfb-content"
		server, caPath := tlsServerCAFile(t, g, dir, body)

		client, err := HTTPClientWithCABundle(caPath)
		g.Expect(err).NotTo(HaveOccurred())

		dst := filepath.Join(dir, "out.bin")
		g.Expect(DownloadFileWithClient(context.Background(), client, server.URL, dst, 0o644)).To(Succeed())

		got, err := os.ReadFile(dst)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(got)).To(Equal(body))
	})

	t.Run("fails when the server certificate is not trusted", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		server, _ := tlsServerCAFile(t, g, dir, "denied")

		// A client backed by an empty cert pool does not trust the server's self-signed cert.
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    x509.NewCertPool(),
		}}}

		dst := filepath.Join(dir, "out.bin")
		err := DownloadFileWithClient(context.Background(), client, server.URL, dst, 0o644)
		g.Expect(err).To(HaveOccurred())
		_, statErr := os.Stat(dst)
		g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	})
}
