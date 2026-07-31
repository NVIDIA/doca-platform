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

package nvidia

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("staticKey rotation verification", func() {
	Describe("configHashFromMetrics", func() {
		It("matches the loaded encryption config hash", func() {
			metrics := `
apiserver_encryption_config_controller_last_config_info{apiserver_id_hash="a",hash="sha256:expected"} 1
`
			matched, err := configHashFromMetrics(strings.NewReader(metrics), "sha256:expected")
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())

			matched, err = configHashFromMetrics(strings.NewReader(metrics), "sha256:other")
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeFalse())
		})

		It("rejects metrics responses larger than the configured limit", func() {
			metrics := strings.Repeat("# padding\n", int(apiserverMetricsMaxResponseBytes/10)+1)

			matched, err := configHashFromMetrics(strings.NewReader(metrics), "sha256:expected")

			Expect(matched).To(BeFalse())
			Expect(err).To(MatchError(ContainSubstring("metrics response exceeds")))
		})

		It("matches the loaded encryption config hash before the response limit", func() {
			metrics := `apiserver_encryption_config_controller_last_config_info{apiserver_id_hash="a",hash="sha256:expected"} 1
` + strings.Repeat("# padding\n", int(apiserverMetricsMaxResponseBytes/10)+1)

			matched, err := configHashFromMetrics(strings.NewReader(metrics), "sha256:expected")

			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())
		})
	})

	Describe("deploymentStable", func() {
		It("requires all replicas to be updated, ready, and available", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2,
					UpdatedReplicas:    3,
					ReadyReplicas:      3,
					AvailableReplicas:  3,
				},
			}
			Expect(deploymentStable(deployment)).To(BeTrue())

			deployment.Status.ReadyReplicas = 2
			Expect(deploymentStable(deployment)).To(BeFalse())
		})
	})

	Describe("podReady", func() {
		It("requires a running pod with a true Ready condition", func() {
			pod := corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			Expect(podReady(pod)).To(BeTrue())

			pod.Status.Conditions[0].Status = corev1.ConditionFalse
			Expect(podReady(pod)).To(BeFalse())
		})
	})

	Describe("apiserverMetricsPort", func() {
		It("uses the configured port and defaults to the kube-apiserver secure port", func() {
			tcp := &kamajiv1.TenantControlPlane{}
			Expect(apiserverMetricsPort(tcp)).To(Equal(apiserverDefaultSecurePort))

			tcp.Spec.NetworkProfile.Port = 30443
			Expect(apiserverMetricsPort(tcp)).To(Equal(int32(30443)))
		})
	})

	Describe("sameMetricsEndpoints", func() {
		It("compares endpoint sets independent of order", func() {
			first := []podMetricsEndpoint{
				{key: "pod-a", address: "10.0.0.1", port: 6443},
				{key: "pod-b", address: "10.0.0.2", port: 6443},
			}
			second := []podMetricsEndpoint{
				{key: "pod-b", address: "10.0.0.2", port: 6443},
				{key: "pod-a", address: "10.0.0.1", port: 6443},
			}
			Expect(sameMetricsEndpoints(first, second)).To(BeTrue())

			second[1].key = "pod-c"
			Expect(sameMetricsEndpoints(first, second)).To(BeFalse())
		})
	})

	Describe("metricsReloadVerifier", func() {
		It("verifies the expected config hash for a stable kube-apiserver pod set", func() {
			configData := []byte("test encryption config")
			expectedHash := fmt.Sprintf("sha256:%x", sha256.Sum256(configData))
			const address = "127.0.0.1"
			listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", address))
			Expect(err).NotTo(HaveOccurred())
			metricsPort := int32(listener.Addr().(*net.TCPAddr).Port)
			ca, caKey, caPEM := testCertificateAuthority()
			serverCert, _, _ := testSignedKeyPair(ca, caKey,
				[]string{fmt.Sprintf("%s.%s.svc", "test-cluster", "test-ns")},
				[]net.IP{net.ParseIP(address)})
			_, clientKeyPEM, clientCertPEM := testSignedKeyPair(ca, caKey, nil, nil)
			adminKubeconfig, err := clientcmd.Write(clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"test-cluster": {
						Server:                   "https://test-cluster.test-ns.svc:6443",
						CertificateAuthorityData: caPEM,
					},
				},
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					"kubernetes-admin": {
						ClientCertificateData: clientCertPEM,
						ClientKeyData:         clientKeyPEM,
					},
				},
				Contexts: map[string]*clientcmdapi.Context{
					"kubernetes-admin@test-cluster": {
						Cluster:  "test-cluster",
						AuthInfo: "kubernetes-admin",
					},
				},
				CurrentContext: "kubernetes-admin@test-cluster",
			})
			Expect(err).NotTo(HaveOccurred())
			metricsServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, "%s{hash=%q} 1\n", encryptionConfigLastConfigInfoMetric, expectedHash)
			}))
			Expect(metricsServer.Listener.Close()).To(Succeed())
			metricsServer.Listener = listener
			clientRoots := x509.NewCertPool()
			Expect(clientRoots.AppendCertsFromPEM(caPEM)).To(BeTrue())
			metricsServer.TLS = &tls.Config{
				Certificates: []tls.Certificate{serverCert},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    clientRoots,
			}
			metricsServer.StartTLS()
			DeferCleanup(metricsServer.Close)

			const namespace = "test-ns"
			dc := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
			}
			tcp := &kamajiv1.TenantControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: dc.Name, Namespace: namespace},
				Spec: kamajiv1.TenantControlPlaneSpec{
					NetworkProfile: kamajiv1.NetworkProfileSpec{Port: metricsPort},
				},
				Status: kamajiv1.TenantControlPlaneStatus{
					KubeConfig: kamajiv1.KubeconfigsStatus{
						Admin: kamajiv1.KubeconfigStatus{SecretName: "admin-kubeconfig"},
					},
					Kubernetes: kamajiv1.KubernetesStatus{
						Deployment: kamajiv1.KubernetesDeploymentStatus{
							Name:      "kube-apiserver",
							Namespace: namespace,
						},
					},
				},
			}
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "kube-apiserver", Namespace: namespace, Generation: 1},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "kube-apiserver"}},
				},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 1,
					UpdatedReplicas:    1,
					ReadyReplicas:      1,
					AvailableReplicas:  1,
				},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-apiserver-1",
					Namespace: namespace,
					UID:       types.UID("pod-1"),
					Labels:    map[string]string{"app": "kube-apiserver"},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					PodIP: address,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "admin-kubeconfig", Namespace: namespace},
				Data: map[string][]byte{
					apiserverMetricsAdminKubeconfigKey: adminKubeconfig,
				},
			}
			verifier := &metricsReloadVerifier{
				client: fake.NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(tcp, deployment, pod, adminSecret).
					Build(),
			}
			config := testStaticKeyConfig{
				testConfig: testConfig{
					provider: operatorv1.EtcdEncryptionProviderStaticKey,
					name:     types.NamespacedName{Name: "encryption-config", Namespace: namespace},
				},
				phase: encryptionconfig.PhasePrepared,
				hash:  expectedHash,
			}

			verified, err := verifier.VerifyReload(context.Background(), dc, config)

			Expect(err).NotTo(HaveOccurred())
			Expect(verified).To(BeTrue())
		})
	})
})

// testCertificateAuthority builds a short-lived CA for metrics TLS tests.
func testCertificateAuthority() (*x509.Certificate, *rsa.PrivateKey, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	cert := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return cert, key, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
}

// testSignedKeyPair signs a short-lived certificate for metrics TLS tests.
func testSignedKeyPair(ca *x509.Certificate, caKey *rsa.PrivateKey, dnsNames []string, ipAddresses []net.IP) (tls.Certificate, []byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "test-cert"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	Expect(err).NotTo(HaveOccurred())
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	Expect(err).NotTo(HaveOccurred())
	return tlsCert, keyPEM, certPEM
}
