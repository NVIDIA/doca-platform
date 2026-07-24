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

package vault

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	vaultapi "github.com/hashicorp/vault/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// clearEnv unsets an environment variable for the duration of the spec.
func clearEnv(key string) {
	old, had := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	DeferCleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		}
	})
}

func clearVaultEnvironment() {
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(key, vaultEnvironmentVariablePrefix) {
			clearEnv(key)
		}
	}
}

type recordedVaultRequest struct {
	method string
	path   string
	token  string
	body   map[string]interface{}
}

// testCertificateAuthority holds a generated CA certificate and key for signing test server certificates.
type testCertificateAuthority struct {
	certificate *x509.Certificate
	privateKey  *rsa.PrivateKey
	pem         []byte
}

// newTestCertificateAuthority creates a self-signed CA bundle for TLS rotation tests.
func newTestCertificateAuthority(serial int64) testCertificateAuthority {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	certificate, err := x509.ParseCertificate(der)
	Expect(err).NotTo(HaveOccurred())
	return testCertificateAuthority{
		certificate: certificate,
		privateKey:  key,
		pem:         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// newTestServerCertificate creates a localhost server certificate signed by the given test CA.
func newTestServerCertificate(ca testCertificateAuthority, serial int64) tls.Certificate {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, &key.PublicKey, ca.privateKey)
	Expect(err).NotTo(HaveOccurred())
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	Expect(err).NotTo(HaveOccurred())
	return certificate
}

// newRotatingTLSServer serves Vault-like responses with a certificate that tests can swap.
func newRotatingTLSServer(initial tls.Certificate) (*httptest.Server, *atomic.Pointer[tls.Certificate]) {
	currentCertificate := &atomic.Pointer[tls.Certificate]{}
	currentCertificate.Store(&initial)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"ok"}}`))
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{initial},
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return &tls.Config{
				MinVersion:   tls.VersionTLS12,
				NextProtos:   []string{"http/1.1"},
				Certificates: []tls.Certificate{*currentCertificate.Load()},
			}, nil
		},
	}
	server.StartTLS()
	DeferCleanup(server.Close)
	return server, currentCertificate
}

// requestTestVault performs a Vault request so tests exercise the configured HTTP transport.
func requestTestVault(client *vaultapi.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.Logical().ReadWithContext(ctx, "sys/health")
	return err
}

// publishTestCABundle models the symlink layout used by Kubernetes AtomicWriter.
// A nil bundle publishes a revision without ca.crt to model a missing update.
func publishTestCABundle(root, revision string, bundle []byte) string {
	Expect(os.MkdirAll(root, 0o700)).To(Succeed())
	revisionDir := filepath.Join(root, revision)
	Expect(os.Mkdir(revisionDir, 0o700)).To(Succeed())
	if bundle != nil {
		Expect(os.WriteFile(filepath.Join(revisionDir, "ca.crt"), bundle, 0o600)).To(Succeed())
	}

	dataLink := filepath.Join(root, "..data")
	caPath := filepath.Join(root, "ca.crt")
	if _, err := os.Lstat(dataLink); os.IsNotExist(err) {
		Expect(os.Symlink(revision, dataLink)).To(Succeed())
		Expect(os.Symlink(filepath.Join("..data", "ca.crt"), caPath)).To(Succeed())
		return caPath
	} else {
		Expect(err).NotTo(HaveOccurred())
	}

	temporaryLink := filepath.Join(root, "..data_tmp")
	if err := os.Remove(temporaryLink); err != nil {
		Expect(os.IsNotExist(err)).To(BeTrue())
	}
	Expect(os.Symlink(revision, temporaryLink)).To(Succeed())
	Expect(os.Rename(temporaryLink, dataLink)).To(Succeed())
	return caPath
}

func recordVaultRequest(r *http.Request) recordedVaultRequest {
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return recordedVaultRequest{
		method: r.Method,
		path:   r.URL.Path,
		token:  r.Header.Get("X-Vault-Token"),
		body:   body,
	}
}

func newAdapterTestClient(handler http.HandlerFunc) (*vaultapi.Client, LimitedVaultClient) {
	server := httptest.NewServer(handler)
	DeferCleanup(server.Close)

	cfg := vaultapi.DefaultConfig()
	cfg.Address = server.URL
	client, err := vaultapi.NewClient(cfg)
	Expect(err).NotTo(HaveOccurred())

	return client, newLimitedVaultClient(client)
}

var _ = Describe("NewClient", func() {
	BeforeEach(func() {
		clearVaultEnvironment()
	})

	It("returns the plugin adapter", func() {
		client, err := NewClient("https://vault.example:8200", "", "", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeAssignableToTypeOf(&apiClientAdapter{}))
	})
})

var _ = Describe("newAPIClient", func() {
	BeforeEach(func() {
		clearVaultEnvironment()
	})

	writeTestCA := func(path string) {
		ca := newTestCertificateAuthority(1)
		Expect(os.WriteFile(path, ca.pem, 0o600)).To(Succeed())
	}

	It("applies the address from the argument", func() {
		client, _, err := newAPIClient("https://vault.example:8200", "", "", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(client.Address()).To(Equal("https://vault.example:8200"))
	})

	It("applies the namespace from the argument", func() {
		client, _, err := newAPIClient("https://vault.example:8200", "", "platform/kubernetes", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(client.Namespace()).To(Equal("platform/kubernetes"))
	})

	It("ignores and removes all VAULT_ environment variables", func() {
		const unknownVaultEnvironmentVariable = "VAULT_FUTURE_SETTING"
		Expect(os.Setenv(vaultapi.EnvVaultAddress, "https://ambient.example:8200")).To(Succeed())
		Expect(os.Setenv(vaultapi.EnvVaultNamespace, "ambient")).To(Succeed())
		Expect(os.Setenv(vaultapi.EnvVaultMaxRetries, "invalid")).To(Succeed())
		Expect(os.Setenv(unknownVaultEnvironmentVariable, "value")).To(Succeed())

		client, _, err := newAPIClient("https://vault.example:8200", "", "explicit", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(client.Address()).To(Equal("https://vault.example:8200"))
		Expect(client.Namespace()).To(Equal("explicit"))

		for _, name := range []string{
			vaultapi.EnvVaultAddress,
			vaultapi.EnvVaultNamespace,
			vaultapi.EnvVaultMaxRetries,
			unknownVaultEnvironmentVariable,
		} {
			_, exists := os.LookupEnv(name)
			Expect(exists).To(BeFalse(), name)
		}
	})

	It("does not configure a token when VAULT_TOKEN is absent", func() {
		client, _, err := newAPIClient("https://127.0.0.1:8200", "", "", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(client.Token()).To(BeEmpty())
	})

	It("returns an error when the CA certificate file does not exist", func() {
		_, _, err := newAPIClient("https://127.0.0.1:8200", filepath.Join(GinkgoT().TempDir(), "missing.pem"), "", logr.Discard())
		Expect(err).To(HaveOccurred())
	})

	It("accepts a valid CA certificate file", func() {
		caPath := filepath.Join(GinkgoT().TempDir(), "ca.pem")
		writeTestCA(caPath)

		_, _, err := newAPIClient("https://127.0.0.1:8200", caPath, "", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error when the CA certificate file is empty", func() {
		caPath := filepath.Join(GinkgoT().TempDir(), "ca.pem")
		Expect(os.WriteFile(caPath, []byte{}, 0o600)).To(Succeed())

		_, _, err := newAPIClient("https://127.0.0.1:8200", caPath, "", logr.Discard())
		Expect(err).To(MatchError(ContainSubstring("CA certificate bundle is empty")))
	})

	It("returns an error when the CA certificate file is malformed", func() {
		caPath := filepath.Join(GinkgoT().TempDir(), "ca.pem")
		Expect(os.WriteFile(caPath, []byte("not a certificate"), 0o600)).To(Succeed())

		_, _, err := newAPIClient("https://127.0.0.1:8200", caPath, "", logr.Discard())
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Vault CA reload", func() {
	BeforeEach(func() {
		clearVaultEnvironment()
	})

	It("reloads a CA bundle after a Kubernetes-style atomic symlink update", func() {
		caA := newTestCertificateAuthority(1)
		caB := newTestCertificateAuthority(2)
		serverCertificateA := newTestServerCertificate(caA, 11)
		serverCertificateB := newTestServerCertificate(caB, 12)
		caPath := publishTestCABundle(filepath.Join(GinkgoT().TempDir(), "ca-volume"), "..revision-a", caA.pem)
		server, currentServerCertificate := newRotatingTLSServer(serverCertificateA)

		client, reloader, err := newAPIClient(server.URL, caPath, "", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(reloader).NotTo(BeNil())
		client.SetMaxRetries(0)
		Expect(requestTestVault(client)).To(Succeed())

		reloader.pollInterval = 10 * time.Millisecond
		runCtx, cancel := context.WithCancel(context.Background())
		runDone := make(chan struct{})
		go func() {
			defer close(runDone)
			reloader.Run(runCtx)
		}()
		DeferCleanup(func() {
			cancel()
			Eventually(runDone).Should(BeClosed())
		})

		initialTransport := reloader.transport.current.Load()
		publishTestCABundle(filepath.Dir(caPath), "..revision-b", caB.pem)
		Eventually(func() *http.Transport {
			return reloader.transport.current.Load()
		}).WithTimeout(5 * time.Second).WithPolling(10 * time.Millisecond).ShouldNot(BeIdenticalTo(initialTransport))

		currentServerCertificate.Store(&serverCertificateB)
		server.CloseClientConnections()
		Expect(requestTestVault(client)).To(Succeed())

		cancel()
		Eventually(runDone).Should(BeClosed())
	})

	It("keeps the last known good CA and recovers after invalid, empty, and missing updates", func() {
		caA := newTestCertificateAuthority(1)
		caB := newTestCertificateAuthority(2)
		serverCertificateA := newTestServerCertificate(caA, 11)
		serverCertificateB := newTestServerCertificate(caB, 12)
		caPath := publishTestCABundle(filepath.Join(GinkgoT().TempDir(), "ca-volume"), "..revision-a", caA.pem)
		server, currentServerCertificate := newRotatingTLSServer(serverCertificateA)

		client, reloader, err := newAPIClient(server.URL, caPath, "", logr.Discard())
		Expect(err).NotTo(HaveOccurred())
		Expect(reloader).NotTo(BeNil())
		DeferCleanup(reloader.transport.CloseIdleConnections)
		client.SetMaxRetries(0)
		Expect(requestTestVault(client)).To(Succeed())

		initialTransport := reloader.transport.current.Load()
		reloader.reload()
		Expect(reloader.transport.current.Load()).To(BeIdenticalTo(initialTransport))

		publishTestCABundle(filepath.Dir(caPath), "..revision-invalid", []byte("not a certificate"))
		reloader.reload()
		Expect(reloader.transport.current.Load()).To(BeIdenticalTo(initialTransport))
		Expect(requestTestVault(client)).To(Succeed())

		publishTestCABundle(filepath.Dir(caPath), "..revision-empty", []byte{})
		reloader.reload()
		Expect(reloader.transport.current.Load()).To(BeIdenticalTo(initialTransport))
		Expect(requestTestVault(client)).To(Succeed())

		publishTestCABundle(filepath.Dir(caPath), "..revision-missing", nil)
		reloader.reload()
		Expect(reloader.transport.current.Load()).To(BeIdenticalTo(initialTransport))
		Expect(requestTestVault(client)).To(Succeed())

		publishTestCABundle(filepath.Dir(caPath), "..revision-b", caB.pem)
		reloader.reload()
		Expect(reloader.transport.current.Load()).NotTo(BeIdenticalTo(initialTransport))

		currentServerCertificate.Store(&serverCertificateB)
		server.CloseClientConnections()
		Expect(requestTestVault(client)).To(Succeed())
	})
})

var _ = Describe("apiClientAdapter", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("validates a candidate token without installing it on the original client", func() {
		requests := make(chan recordedVaultRequest, 1)
		client, adapter := newAdapterTestClient(func(w http.ResponseWriter, r *http.Request) {
			requests <- recordVaultRequest(r)
			if r.URL.Path != "/v1/auth/token/lookup-self" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"id":"candidate-token"}}`))
		})

		Expect(adapter.ValidateToken(ctx, "candidate-token")).To(Succeed())

		var req recordedVaultRequest
		Eventually(requests).Should(Receive(&req))
		Expect(req.method).To(Equal(http.MethodGet))
		Expect(req.path).To(Equal("/v1/auth/token/lookup-self"))
		Expect(req.token).To(Equal("candidate-token"))
		Expect(client.Token()).To(BeEmpty())
	})

	It("sets the token and writes logical requests through the wrapped client", func() {
		requests := make(chan recordedVaultRequest, 1)
		_, adapter := newAdapterTestClient(func(w http.ResponseWriter, r *http.Request) {
			requests <- recordVaultRequest(r)
			if r.URL.Path != "/v1/transit/encrypt/k8s" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"ciphertext":"vault:v1:abc"}}`))
		})

		adapter.SetToken("active-token")
		secret, err := adapter.Write(ctx, "transit/encrypt/k8s", map[string]interface{}{"plaintext": "abc"})
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.Data["ciphertext"]).To(Equal("vault:v1:abc"))

		var req recordedVaultRequest
		Eventually(requests).Should(Receive(&req))
		Expect(req.method).To(Equal(http.MethodPut))
		Expect(req.path).To(Equal("/v1/transit/encrypt/k8s"))
		Expect(req.token).To(Equal("active-token"))
		Expect(req.body).To(Equal(map[string]interface{}{"plaintext": "abc"}))
	})

	It("logs in with an auth method and installs the returned token", func() {
		requests := make(chan recordedVaultRequest, 1)
		client, adapter := newAdapterTestClient(func(w http.ResponseWriter, r *http.Request) {
			requests <- recordVaultRequest(r)
			if r.URL.Path != "/v1/auth/jwt/login" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"auth":{"client_token":"logged-in-token"}}`))
		})
		method, err := newJWTAuth("role", "jwt", "")
		Expect(err).NotTo(HaveOccurred())

		Expect(adapter.Login(ctx, method)).To(Succeed())

		var req recordedVaultRequest
		Eventually(requests).Should(Receive(&req))
		Expect(req.method).To(Equal(http.MethodPut))
		Expect(req.path).To(Equal("/v1/auth/jwt/login"))
		Expect(req.body).To(Equal(map[string]interface{}{"role": "role", "jwt": "jwt"}))
		Expect(client.Token()).To(Equal("logged-in-token"))
	})

	It("renews and looks up the active token through the wrapped client", func() {
		requests := make(chan recordedVaultRequest, 2)
		_, adapter := newAdapterTestClient(func(w http.ResponseWriter, r *http.Request) {
			requests <- recordVaultRequest(r)
			switch r.URL.Path {
			case "/v1/auth/token/renew-self":
				_, _ = w.Write([]byte(`{"auth":{"client_token":"active-token","lease_duration":3600,"renewable":true}}`))
			case "/v1/auth/token/lookup-self":
				_, _ = w.Write([]byte(`{"data":{"ttl":3600,"creation_ttl":3600,"renewable":true}}`))
			default:
				http.NotFound(w, r)
			}
		})

		adapter.SetToken("active-token")
		Expect(adapter.RenewSelf(ctx)).To(Succeed())
		secret, err := adapter.LookupSelf(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.Data).To(HaveKey("ttl"))

		var renewReq, lookupReq recordedVaultRequest
		Eventually(requests).Should(Receive(&renewReq))
		Eventually(requests).Should(Receive(&lookupReq))
		Expect(renewReq.method).To(Equal(http.MethodPut))
		Expect(renewReq.path).To(Equal("/v1/auth/token/renew-self"))
		Expect(renewReq.token).To(Equal("active-token"))
		Expect(lookupReq.method).To(Equal(http.MethodGet))
		Expect(lookupReq.path).To(Equal("/v1/auth/token/lookup-self"))
		Expect(lookupReq.token).To(Equal("active-token"))
	})
})
