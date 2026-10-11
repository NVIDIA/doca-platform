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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const providerNamespace = "dpf-provisioning-test"

// ca is a throwaway certificate authority for provider fixtures.
type ca struct {
	pem  []byte
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newCA(t *testing.T, commonName string) *ca {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &ca{pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert: cert, key: key}
}

// issue signs a leaf for commonName. notAfter in the past produces expired material, which is what
// the expiry cases need.
func (c *ca) issue(t *testing.T, commonName string, ips []net.IP, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// clientPair issues a client key pair valid for the next day.
func (c *ca) clientPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	return c.issue(t, "dpf-redfish-client", nil, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
}

// certDir writes a mounted client key pair layout and points the process at it. The provider
// captures the directory at construction, so this must run before NewProvider.
func certDir(t *testing.T, ca *ca) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "redfish-client-cert")
	for _, d := range []string{dir, dir + "-bf4"} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		certPEM, keyPEM := ca.clientPair(t)
		writePair(t, d, certPEM, keyPEM)
	}
	previous := getClientCertDir()
	SetClientCertDir(dir)
	t.Cleanup(func() { SetClientCertDir(previous) })
	return dir
}

// writePair writes a client key pair into dir as the mounted volume would.
func writePair(t *testing.T, dir string, certPEM, keyPEM []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, corev1.TLSCertKey), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, corev1.TLSPrivateKeyKey), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

// caConfigMap returns the namespace trust bundle ConfigMap holding caPEM.
func caConfigMap(namespace string, caPEM []byte) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: CATrustBundleConfigMap, Namespace: namespace},
		Data:       map[string]string{CATrustBundleKey: string(caPEM)},
	}
}

// bmc is a Redfish test server that counts requests per path and presents a CA-signed certificate,
// so a fully verified mTLS client can talk to it.
type bmc struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
	// conns counts accepted TCP connections, which is how connection reuse is observed.
	conns int
}

func newBMC(t *testing.T, ca *ca, handler func(w http.ResponseWriter, r *http.Request)) *bmc {
	t.Helper()
	b := &bmc{counts: map[string]int{}}
	certPEM, keyPEM := ca.issue(t, "127.0.0.1", []net.IP{net.ParseIP("127.0.0.1")},
		time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.counts[r.URL.Path]++
		b.mu.Unlock()
		if handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Product":"BlueField-3"}`))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			b.mu.Lock()
			b.conns++
			b.mu.Unlock()
		}
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	b.Server = srv
	return b
}

// count returns how many requests the server received for path.
func (b *bmc) count(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.counts[path]
}

// connections returns how many TCP connections the server accepted.
func (b *bmc) connections() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conns
}

// newTestProvider builds a provider over a fake API reader holding the trust bundle.
func newTestProvider(t *testing.T, objects ...client.Object) (*Provider, client.Client) {
	t.Helper()
	builder := fake.NewClientBuilder()
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	k8s := builder.Build()
	return NewProvider(k8s), k8s
}
