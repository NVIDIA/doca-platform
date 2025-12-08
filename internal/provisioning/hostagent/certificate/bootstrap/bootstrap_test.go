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

package bootstrap

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var _ = Describe("Bootstrap", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "bootstrap-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("isClientConfigStillValid", Label("isClientConfigStillValid"), func() {
		It("should return false when kubeconfig file does not exist", func() {
			nonExistentPath := filepath.Join(tempDir, "nonexistent-kubeconfig")
			valid, err := isClientConfigStillValid(nonExistentPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(valid).To(BeFalse())
		})

		It("should return false for invalid kubeconfig content", func() {
			kubeconfigPath := filepath.Join(tempDir, "invalid-kubeconfig")
			Expect(os.WriteFile(kubeconfigPath, []byte("invalid yaml content: ["), 0644)).To(Succeed())

			valid, err := isClientConfigStillValid(kubeconfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(valid).To(BeFalse())
		})

		It("should return false for kubeconfig with expired certificate", func() {
			kubeconfigPath := filepath.Join(tempDir, "expired-kubeconfig")
			certPath := filepath.Join(tempDir, "expired-cert.pem")
			keyPath := filepath.Join(tempDir, "expired-key.pem")

			// Generate expired certificate
			cert, key := generateTestCertificate(time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
			Expect(os.WriteFile(certPath, cert, 0644)).To(Succeed())
			Expect(os.WriteFile(keyPath, key, 0600)).To(Succeed())

			// Create kubeconfig with expired certificate
			kubeconfig := clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                "https://localhost:6443",
						InsecureSkipTLSVerify: true,
					},
				},
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					"test-user": {
						ClientCertificate: certPath,
						ClientKey:         keyPath,
					},
				},
				Contexts: map[string]*clientcmdapi.Context{
					"test-context": {
						Cluster:  "test-cluster",
						AuthInfo: "test-user",
					},
				},
				CurrentContext: "test-context",
			}
			Expect(clientcmd.WriteToFile(kubeconfig, kubeconfigPath)).To(Succeed())

			valid, err := isClientConfigStillValid(kubeconfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(valid).To(BeFalse())
		})

		It("should return true for kubeconfig with valid certificate", func() {
			kubeconfigPath := filepath.Join(tempDir, "valid-kubeconfig")
			certPath := filepath.Join(tempDir, "valid-cert.pem")
			keyPath := filepath.Join(tempDir, "valid-key.pem")

			// Generate valid certificate (not expired)
			cert, key := generateTestCertificate(time.Now().Add(-1*time.Hour), time.Now().Add(24*time.Hour))
			Expect(os.WriteFile(certPath, cert, 0644)).To(Succeed())
			Expect(os.WriteFile(keyPath, key, 0600)).To(Succeed())

			// Create kubeconfig with valid certificate
			kubeconfig := clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                "https://localhost:6443",
						InsecureSkipTLSVerify: true,
					},
				},
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					"test-user": {
						ClientCertificate: certPath,
						ClientKey:         keyPath,
					},
				},
				Contexts: map[string]*clientcmdapi.Context{
					"test-context": {
						Cluster:  "test-cluster",
						AuthInfo: "test-user",
					},
				},
				CurrentContext: "test-context",
			}
			Expect(clientcmd.WriteToFile(kubeconfig, kubeconfigPath)).To(Succeed())

			valid, err := isClientConfigStillValid(kubeconfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(valid).To(BeTrue())
		})

		It("should return false for kubeconfig without certificate", func() {
			kubeconfigPath := filepath.Join(tempDir, "no-cert-kubeconfig")
			kubeconfig := clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                "https://localhost:6443",
						InsecureSkipTLSVerify: true,
					},
				},
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					"test-user": {
						Token: "test-token",
					},
				},
				Contexts: map[string]*clientcmdapi.Context{
					"test-context": {
						Cluster:  "test-cluster",
						AuthInfo: "test-user",
					},
				},
				CurrentContext: "test-context",
			}
			Expect(clientcmd.WriteToFile(kubeconfig, kubeconfigPath)).To(Succeed())

			valid, err := isClientConfigStillValid(kubeconfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(valid).To(BeFalse())
		})
	})

	Context("loadRESTClientConfig", Label("loadRESTClientConfig"), func() {
		It("should return error for non-existent file", func() {
			_, err := loadRESTClientConfig(filepath.Join(tempDir, "nonexistent"))
			Expect(err).To(HaveOccurred())
		})

		It("should return error for invalid kubeconfig", func() {
			invalidPath := filepath.Join(tempDir, "invalid-kubeconfig")
			Expect(os.WriteFile(invalidPath, []byte("not valid yaml: ["), 0644)).To(Succeed())

			_, err := loadRESTClientConfig(invalidPath)
			Expect(err).To(HaveOccurred())
		})

		It("should load valid kubeconfig", func() {
			kubeconfigPath := filepath.Join(tempDir, "valid-kubeconfig")
			kubeconfig := clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                "https://localhost:6443",
						InsecureSkipTLSVerify: true,
					},
				},
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					"test-user": {},
				},
				Contexts: map[string]*clientcmdapi.Context{
					"test-context": {
						Cluster:  "test-cluster",
						AuthInfo: "test-user",
					},
				},
				CurrentContext: "test-context",
			}
			Expect(clientcmd.WriteToFile(kubeconfig, kubeconfigPath)).To(Succeed())

			config, err := loadRESTClientConfig(kubeconfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.Host).To(Equal("https://localhost:6443"))
		})
	})

	Context("writeKubeconfigFromBootstrapping", Label("writeKubeconfigFromBootstrapping"), func() {
		It("should write kubeconfig file with CA file reference", func() {
			kubeconfigPath := filepath.Join(tempDir, "output-kubeconfig")
			pemPath := filepath.Join(tempDir, "client.pem")

			bootstrapConfig := &restclient.Config{
				Host:            "https://api.example.com:6443",
				TLSClientConfig: restclient.TLSClientConfig{CAFile: "/path/to/ca.crt"},
			}

			err := writeKubeconfigFromBootstrapping(bootstrapConfig, kubeconfigPath, pemPath)
			Expect(err).NotTo(HaveOccurred())

			// Verify file was created
			_, err = os.Stat(kubeconfigPath)
			Expect(err).NotTo(HaveOccurred())

			// Load and verify content
			loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
			loadedConfig, err := loader.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(loadedConfig.CurrentContext).To(Equal("default-context"))
			Expect(loadedConfig.Clusters["default-cluster"].Server).To(Equal("https://api.example.com:6443"))
			Expect(loadedConfig.Clusters["default-cluster"].CertificateAuthority).To(Equal("/path/to/ca.crt"))
			Expect(loadedConfig.AuthInfos["default-auth"].ClientCertificate).To(Equal(pemPath))
			Expect(loadedConfig.AuthInfos["default-auth"].ClientKey).To(Equal(pemPath))
		})

		It("should write kubeconfig file with CA data", func() {
			kubeconfigPath := filepath.Join(tempDir, "output-kubeconfig-data")
			pemPath := filepath.Join(tempDir, "client.pem")

			bootstrapConfig := &restclient.Config{
				Host: "https://api.example.com:6443",
				TLSClientConfig: restclient.TLSClientConfig{
					CAData: []byte("test-ca-data"),
				},
			}

			err := writeKubeconfigFromBootstrapping(bootstrapConfig, kubeconfigPath, pemPath)
			Expect(err).NotTo(HaveOccurred())

			// Load and verify content
			loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
			loadedConfig, err := loader.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(loadedConfig.Clusters["default-cluster"].CertificateAuthorityData).To(Equal([]byte("test-ca-data")))
		})

		It("should write kubeconfig with insecure flag", func() {
			kubeconfigPath := filepath.Join(tempDir, "output-kubeconfig-insecure")
			pemPath := filepath.Join(tempDir, "client.pem")

			bootstrapConfig := &restclient.Config{
				Host: "https://api.example.com:6443",
				TLSClientConfig: restclient.TLSClientConfig{
					Insecure: true,
				},
			}

			err := writeKubeconfigFromBootstrapping(bootstrapConfig, kubeconfigPath, pemPath)
			Expect(err).NotTo(HaveOccurred())

			// Load and verify content
			loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
			loadedConfig, err := loader.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(loadedConfig.Clusters["default-cluster"].InsecureSkipTLSVerify).To(BeTrue())
		})
	})

	Context("LoadClientConfig", Label("LoadClientConfig"), func() {
		It("should return error when kubeconfig does not exist and no bootstrap path", func() {
			kubeconfigPath := filepath.Join(tempDir, "nonexistent-kubeconfig")

			_, _, err := LoadClientConfig(kubeconfigPath, "", tempDir)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unable to load kubeconfig"))
		})

		It("should load kubeconfig when no bootstrap path provided", func() {
			kubeconfigPath := filepath.Join(tempDir, "kubeconfig")
			kubeconfig := clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                "https://localhost:6443",
						InsecureSkipTLSVerify: true,
					},
				},
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					"test-user": {},
				},
				Contexts: map[string]*clientcmdapi.Context{
					"test-context": {
						Cluster:  "test-cluster",
						AuthInfo: "test-user",
					},
				},
				CurrentContext: "test-context",
			}
			Expect(clientcmd.WriteToFile(kubeconfig, kubeconfigPath)).To(Succeed())

			certConfig, userConfig, err := LoadClientConfig(kubeconfigPath, "", tempDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(certConfig).NotTo(BeNil())
			Expect(userConfig).NotTo(BeNil())
			Expect(certConfig.Host).To(Equal("https://localhost:6443"))
			Expect(userConfig.Host).To(Equal("https://localhost:6443"))
		})

		It("should return error when bootstrap path is provided but invalid", func() {
			kubeconfigPath := filepath.Join(tempDir, "kubeconfig")
			bootstrapPath := filepath.Join(tempDir, "invalid-bootstrap")
			Expect(os.WriteFile(bootstrapPath, []byte("invalid: ["), 0644)).To(Succeed())

			// Create an invalid kubeconfig that will trigger bootstrap
			Expect(os.WriteFile(kubeconfigPath, []byte("invalid: ["), 0644)).To(Succeed())

			_, _, err := LoadClientConfig(kubeconfigPath, bootstrapPath, tempDir)
			Expect(err).To(HaveOccurred())
		})
	})
})

// generateTestCertificate creates a self-signed certificate for testing
func generateTestCertificate(notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "test-client",
			Organization: []string{"test-org"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	Expect(err).NotTo(HaveOccurred())

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Encode private key to PEM
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	return certPEM, keyPEM
}
