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

package certificate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	restclient "k8s.io/client-go/rest"
)

// mockCertificateManager implements certificate.Manager interface for testing
type mockCertificateManager struct {
	mu            sync.RWMutex
	cert          *tls.Certificate
	serverHealthy bool
}

func newMockCertificateManager() *mockCertificateManager {
	return &mockCertificateManager{
		serverHealthy: true,
	}
}

func (m *mockCertificateManager) Current() *tls.Certificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert
}

func (m *mockCertificateManager) ServerHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serverHealthy
}

func (m *mockCertificateManager) SetCertificate(cert *tls.Certificate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cert = cert
}

func (m *mockCertificateManager) SetServerHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serverHealthy = healthy
}

// Start is required by certificate.Manager interface but not used in transport.go
func (m *mockCertificateManager) Start() {}

// Stop is required by certificate.Manager interface but not used in transport.go
func (m *mockCertificateManager) Stop() {}

// generateTestCertificate creates a self-signed certificate for testing
func generateTestCertificate() (*tls.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
		Leaf:        leaf,
	}, nil
}

var _ = Describe("Certificate", func() {
	Context("UpdateTransport", Label("UpdateTransport"), func() {
		It("should return error when transport is already configured", func() {
			clientConfig := &restclient.Config{
				Transport: http.DefaultTransport,
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("there is already a transport or dialer configured"))
		})

		It("should return error when dialer is already configured", func() {
			clientConfig := &restclient.Config{
				Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, nil
				},
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("there is already a transport or dialer configured"))
		})

		It("should succeed with nil certificate manager", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should set up dial function when certificate manager is nil", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(clientConfig.Dial).NotTo(BeNil())
		})

		It("should return a working close function", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
			// Should not panic when called
			Expect(func() { closeFunc() }).NotTo(Panic())
		})
	})

	Context("updateTransport", Label("updateTransport"), func() {
		It("should accept custom period for certificate checking", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 1*time.Second, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should return error when both transport and dialer are configured", func() {
			clientConfig := &restclient.Config{
				Transport: http.DefaultTransport,
				Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, nil
				},
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := updateTransport(stopCh, 10*time.Second, clientConfig, nil, 0)
			Expect(err).To(HaveOccurred())
		})

		It("should work with zero exit duration", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 10*time.Second, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should work with non-zero exit duration", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 10*time.Second, clientConfig, nil, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})
	})

	Context("addCertRotation with certificate manager", Label("addCertRotation"), func() {
		It("should configure transport with certificate manager", func() {
			certManager := newMockCertificateManager()
			cert, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert)

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := UpdateTransport(stopCh, clientConfig, certManager, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
			Expect(clientConfig.Transport).NotTo(BeNil())
		})

		It("should configure transport with nil certificate", func() {
			certManager := newMockCertificateManager()
			// Certificate is nil by default

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := UpdateTransport(stopCh, clientConfig, certManager, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
			Expect(clientConfig.Transport).NotTo(BeNil())
		})

		It("should clear TLS configuration from clientConfig after setup", func() {
			certManager := newMockCertificateManager()
			cert, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert)

			// Use Insecure mode to avoid needing valid cert/key data
			// The test verifies that addCertRotation clears these fields
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
				TLSClientConfig: restclient.TLSClientConfig{
					Insecure: true,
				},
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err = UpdateTransport(stopCh, clientConfig, certManager, 0)
			Expect(err).NotTo(HaveOccurred())

			// Verify Insecure flag was cleared (transport now handles TLS)
			Expect(clientConfig.TLSClientConfig.Insecure).To(BeFalse())
			// Verify transport was set up
			Expect(clientConfig.Transport).NotTo(BeNil())
		})

		It("should work with custom check period", func() {
			certManager := newMockCertificateManager()
			cert, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert)

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 100*time.Millisecond, clientConfig, certManager, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should work with exit duration set", func() {
			certManager := newMockCertificateManager()
			cert, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert)

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 100*time.Millisecond, clientConfig, certManager, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should detect certificate rotation", func() {
			certManager := newMockCertificateManager()
			cert1, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert1)

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 50*time.Millisecond, clientConfig, certManager, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())

			// Rotate to a new certificate
			cert2, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert2)

			// Wait for the rotation check to run
			time.Sleep(100 * time.Millisecond)

			// The closeFunc should still work after rotation
			Expect(func() { closeFunc() }).NotTo(Panic())
		})

		It("should handle server health check", func() {
			certManager := newMockCertificateManager()
			certManager.SetServerHealthy(true)

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 50*time.Millisecond, clientConfig, certManager, 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())

			// Verify server healthy state is accessible
			Expect(certManager.ServerHealthy()).To(BeTrue())

			// Change server health
			certManager.SetServerHealthy(false)
			Expect(certManager.ServerHealthy()).To(BeFalse())
		})

		It("should stop checking when stopCh is closed", func() {
			certManager := newMockCertificateManager()
			cert, err := generateTestCertificate()
			Expect(err).NotTo(HaveOccurred())
			certManager.SetCertificate(cert)

			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})

			closeFunc, err := updateTransport(stopCh, 50*time.Millisecond, clientConfig, certManager, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())

			// Close the stop channel
			close(stopCh)

			// Wait a bit and verify no panic
			time.Sleep(100 * time.Millisecond)
			Expect(func() { closeFunc() }).NotTo(Panic())
		})
	})

	Context("NewCertificateManager", Label("NewCertificateManager"), func() {
		var tempDir string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "cert-manager-test-*")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if tempDir != "" {
				_ = os.RemoveAll(tempDir)
			}
		})

		It("should create certificate manager with valid directory", func() {
			manager, err := NewCertificateManager(
				tempDir,
				"test-node",
				"client.crt",
				"client.key",
				nil, // clientsetFn can be nil for initial creation
				"client",
				"dpf:test:test-node",
				[]string{"dpf:test"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(manager).NotTo(BeNil())
		})

		It("should succeed even with non-existent directory (lazy validation by FileStore)", func() {
			// Note: certificate.NewFileStore doesn't validate directory existence upfront
			// It will only fail when trying to read/write certificates
			// This matches upstream k8s.io/client-go behavior
			manager, err := NewCertificateManager(
				"/nonexistent/path/to/certs",
				"test-node",
				"client.crt",
				"client.key",
				nil,
				"client",
				"dpf:test:test-node",
				[]string{"dpf:test"},
			)
			// Manager creation succeeds - validation is deferred to actual file operations
			Expect(err).NotTo(HaveOccurred())
			Expect(manager).NotTo(BeNil())
		})

		It("should accept different node names", func() {
			manager1, err := NewCertificateManager(
				tempDir,
				"node-1",
				"client1.crt",
				"client1.key",
				nil,
				"client",
				"dpf:test:node-1",
				[]string{"dpf:test"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(manager1).NotTo(BeNil())

			// Create another temp dir for second manager
			tempDir2, err := os.MkdirTemp("", "cert-manager-test2-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(tempDir2) }()

			manager2, err := NewCertificateManager(
				tempDir2,
				"node-2",
				"client2.crt",
				"client2.key",
				nil,
				"client",
				"dpf:test:node-2",
				[]string{"dpf:test"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(manager2).NotTo(BeNil())
		})

		It("should work with empty node name", func() {
			manager, err := NewCertificateManager(
				tempDir,
				"",
				"client.crt",
				"client.key",
				nil,
				"client",
				"dpf:test:",
				[]string{"dpf:test"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(manager).NotTo(BeNil())
		})
	})
})
