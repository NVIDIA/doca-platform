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

package bmcdump

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	redfishclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// newTestCA mints a self-signed CA standing in for the authority that anchors the DPF PKI.
func newTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	g := NewWithT(t)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-bmc-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	g.Expect(err).NotTo(HaveOccurred())
	cert, err := x509.ParseCertificate(der)
	g.Expect(err).NotTo(HaveOccurred())

	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// newCNOnlyServerCert issues a server certificate that carries host in the Common Name and in no
// subjectAltName extension, which is the shape BlueField BMC firmware produces on both BF3 and BF4:
// it is handed "AlternativeNames" in the Redfish CSR request and encodes them as Subject DN
// attributes instead of as an extension. Go's own verifier rejects such a certificate outright, so
// anything built on crypto/tls defaults cannot talk to a real BMC.
func newCNOnlyServerCert(t *testing.T, host string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	g := NewWithT(t)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		// DNSNames and IPAddresses are deliberately left unset so no SAN extension is emitted.
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	g.Expect(err).NotTo(HaveOccurred())

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// testCAPool returns a pool holding one throwaway CA, for tests that need a usable collector
// without caring which authority it trusts.
func testCAPool(t *testing.T) *x509.CertPool {
	t.Helper()
	g := NewWithT(t)

	_, _, caPEM := newTestCA(t)
	pool, err := caPoolFromPEM(caPEM, "test-ca")
	g.Expect(err).NotTo(HaveOccurred())
	return pool
}

// TestBaseURLPinsTheDialedHost checks the invariant the BMC certificate is verified under: the
// identity newCollector pins is taken from the URL it dials, so for every address shape a DPUDevice
// can carry, parsing that URL has to yield the bare host and nothing else. A stray scheme, port or
// IPv6 bracket here would reject a perfectly good certificate.
func TestBaseURLPinsTheDialedHost(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		wantURL  string
		wantHost string
	}{
		{
			name:     "bare ipv4",
			ip:       "10.0.0.10",
			wantURL:  "https://10.0.0.10",
			wantHost: "10.0.0.10",
		},
		{
			name:     "ipv4 with port",
			ip:       "10.0.0.10:8443",
			wantURL:  "https://10.0.0.10:8443",
			wantHost: "10.0.0.10",
		},
		{
			name:     "scheme is stripped",
			ip:       "https://10.0.0.10",
			wantURL:  "https://10.0.0.10",
			wantHost: "10.0.0.10",
		},
		{
			name:     "hostname",
			ip:       "bmc.example.com",
			wantURL:  "https://bmc.example.com",
			wantHost: "bmc.example.com",
		},
		{
			name:     "bare ipv6",
			ip:       "2001:db8::10",
			wantURL:  "https://[2001:db8::10]",
			wantHost: "2001:db8::10",
		},
		{
			name:     "bracketed ipv6",
			ip:       "[2001:db8::10]",
			wantURL:  "https://[2001:db8::10]",
			wantHost: "2001:db8::10",
		},
		{
			name:     "bracketed ipv6 with port",
			ip:       "[2001:db8::10]:8443",
			wantURL:  "https://[2001:db8::10]:8443",
			wantHost: "2001:db8::10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			dialURL := baseURL(logTarget{IP: tt.ip, Port: defaultPort})
			g.Expect(dialURL).To(Equal(tt.wantURL))

			dialed, err := url.ParseRequestURI(dialURL)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dialed.Hostname()).To(Equal(tt.wantHost))
		})
	}
}

// TestResolveCAPool checks CA resolution from the published trust bundle and a supplied file.
func TestResolveCAPool(t *testing.T) {
	g := NewWithT(t)

	_, _, caPEM := newTestCA(t)
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(operatorv1.AddToScheme(scheme)).To(Succeed())

	namedTrustBundle := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: DefaultNamespace,
			},
			Data: data,
		}
	}
	trustBundle := func(data map[string]string) *corev1.ConfigMap {
		return namedTrustBundle(operatorv1.DefaultCATrustBundleConfigMapName, data)
	}
	// A DPFOperatorConfig that publishes the bundle under a name of the cluster's choosing.
	renamingConfig := func(bundleName string) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpfoperatorconfig",
				Namespace: DefaultNamespace,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				Security: &operatorv1.SecurityConfiguration{
					CertManagement: &operatorv1.CertManagementConfiguration{TrustBundleConfigMapName: bundleName},
				},
			},
		}
	}

	otherNamespaceConfig := func(namespace string) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpfoperatorconfig",
				Namespace: namespace,
			},
		}
	}

	caFile := filepath.Join(t.TempDir(), "ca.crt")
	g.Expect(os.WriteFile(caFile, caPEM, 0600)).To(Succeed())
	junkFile := filepath.Join(t.TempDir(), "junk.crt")
	g.Expect(os.WriteFile(junkFile, []byte("not a certificate"), 0600)).To(Succeed())
	absentFile := filepath.Join(t.TempDir(), "absent.crt")

	tests := []struct {
		name         string
		objects      []client.Object
		forbidConfig bool
		opts         CollectOptions
		wantErr      string
	}{
		{
			name:    "reads the ca from the published trust bundle",
			objects: []client.Object{trustBundle(map[string]string{redfishclient.CATrustBundleKey: string(caPEM)})},
			opts:    CollectOptions{Namespace: DefaultNamespace},
		},
		{
			name:    "ca cert file wins over the published trust bundle",
			objects: []client.Object{trustBundle(map[string]string{redfishclient.CATrustBundleKey: "not a certificate"})},
			opts:    CollectOptions{Namespace: DefaultNamespace, CACertFile: caFile},
		},
		{
			name: "follows the bundle name the DPFOperatorConfig publishes",
			objects: []client.Object{
				renamingConfig("custom-ca-bundle"),
				namedTrustBundle("custom-ca-bundle", map[string]string{redfishclient.CATrustBundleKey: string(caPEM)}),
			},
			opts: CollectOptions{Namespace: DefaultNamespace},
		},
		{
			name: "does not fall back to the default name once the bundle is renamed",
			objects: []client.Object{
				renamingConfig("custom-ca-bundle"),
				trustBundle(map[string]string{redfishclient.CATrustBundleKey: string(caPEM)}),
			},
			opts:    CollectOptions{Namespace: DefaultNamespace},
			wantErr: "custom-ca-bundle",
		},
		{
			name: "reads the bundle from the namespace of the DPFOperatorConfig",
			objects: []client.Object{
				otherNamespaceConfig("operator-ns"),
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.DefaultCATrustBundleConfigMapName,
						Namespace: "operator-ns",
					},
					Data: map[string]string{redfishclient.CATrustBundleKey: string(caPEM)},
				},
			},
			opts: CollectOptions{Namespace: DefaultNamespace},
		},
		{
			name:         "falls back to the default bundle when DPFOperatorConfigs cannot be listed",
			objects:      []client.Object{trustBundle(map[string]string{redfishclient.CATrustBundleKey: string(caPEM)})},
			forbidConfig: true,
			opts:         CollectOptions{Namespace: DefaultNamespace},
		},
		{
			name:    "missing trust bundle names the flag that replaces it",
			opts:    CollectOptions{Namespace: DefaultNamespace},
			wantErr: "--ca-cert-file",
		},
		{
			name:    "trust bundle in another namespace is not used",
			objects: []client.Object{trustBundle(map[string]string{redfishclient.CATrustBundleKey: string(caPEM)})},
			opts:    CollectOptions{Namespace: "other-namespace"},
			wantErr: "reading bmc ca trust bundle other-namespace",
		},
		{
			name:    "empty trust bundle key fails closed",
			objects: []client.Object{trustBundle(map[string]string{redfishclient.CATrustBundleKey: ""})},
			opts:    CollectOptions{Namespace: DefaultNamespace},
			wantErr: "no PEM certificate found",
		},
		{
			name:    "trust bundle under the wrong key fails closed",
			objects: []client.Object{trustBundle(map[string]string{"tls.crt": string(caPEM)})},
			opts:    CollectOptions{Namespace: DefaultNamespace},
			wantErr: "no PEM certificate found",
		},
		{
			name:    "unparseable ca cert file fails closed",
			opts:    CollectOptions{Namespace: DefaultNamespace, CACertFile: junkFile},
			wantErr: "no PEM certificate found",
		},
		{
			name:    "unreadable ca cert file is reported with its path",
			opts:    CollectOptions{Namespace: DefaultNamespace, CACertFile: absentFile},
			wantErr: "absent.crt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...)
			if tt.forbidConfig {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
						return apierrors.NewForbidden(operatorv1.GroupVersion.WithResource("dpfoperatorconfigs").GroupResource(), "", errors.New("denied"))
					},
				})
			}
			c := builder.Build()

			pool, err := resolveCAPool(context.Background(), c, tt.opts)

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantErr))
				g.Expect(pool).To(BeNil())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pool).NotTo(BeNil())
		})
	}
}

// TestRedfishClientVerifiesCNOnlyBMCCertificate drives a real TLS handshake against a certificate
// shaped like the one a BlueField BMC serves. It is the regression guard for the collector's TLS
// settings: the trust decision has to come from the DPF CA and the dialed address, never from the
// host trust store and never from skipping verification.
func TestRedfishClientVerifiesCNOnlyBMCCertificate(t *testing.T) {
	g := NewWithT(t)

	const host = "127.0.0.1"
	ca, caKey, caPEM := newTestCA(t)
	serverCert := newCNOnlyServerCert(t, host, ca, caKey)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Product":"BlueField-4"}`))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)

	// Guard the fixture itself: if this certificate ever gains a SAN the test stops exercising the
	// firmware behavior it exists for, and Go's default verifier would have sufficed all along.
	leaf, err := x509.ParseCertificate(serverCert.Certificate[0])
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(leaf.IPAddresses).To(BeEmpty())
	g.Expect(leaf.DNSNames).To(BeEmpty())
	g.Expect(leaf.VerifyHostname(host)).To(HaveOccurred())

	trusted, err := caPoolFromPEM(caPEM, "test-ca")
	g.Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name     string
		caPool   *x509.CertPool
		host     string
		insecure bool
		wantErr  string
	}{
		{
			name:   "accepts a CN-only certificate that chains to the trusted CA",
			caPool: trusted,
			host:   host,
		},
		{
			name:    "rejects a certificate that does not chain to the trusted CA",
			caPool:  testCAPool(t),
			host:    host,
			wantErr: "failed chain verification",
		},
		{
			name:    "rejects a certificate whose identity is not the dialed host",
			caPool:  trusted,
			host:    "10.0.0.1",
			wantErr: "identity mismatch",
		},
		{
			name:     "accepts an untrusted certificate when the test-only skip is set",
			host:     "10.0.0.1",
			insecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			redfishClient, err := newRedfishClient(server.URL, tt.host, CollectOptions{
				RequestTimeout:        10 * time.Second,
				caPool:                tt.caPool,
				InsecureSkipTLSVerify: tt.insecure,
			})
			g.Expect(err).NotTo(HaveOccurred())

			resp, err := redfishClient.R().Get(rootServicePath)

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantErr))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			g.Expect(string(resp.Body())).To(ContainSubstring("BlueField-4"))
		})
	}
}
