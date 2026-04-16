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

package csr

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	certmanager "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("CSR Controller", func() {
	Describe("shouldProcessCSRObject", func() {
		It("selects host-agent bootstrap group", func() {
			Expect(shouldProcessCSRObject(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{hostAgentBootstrapGroup},
				},
			})).To(BeTrue())
		})

		It("selects dpu-agent bootstrap group", func() {
			Expect(shouldProcessCSRObject(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{cutil.DPUAgentBootstrapGroup},
				},
			})).To(BeTrue())
		})

		It("selects dpu-agent certificate organization group", func() {
			Expect(shouldProcessCSRObject(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{providentity.DPUAgentOrganization},
				},
			})).To(BeTrue())
		})

		It("ignores unrelated groups", func() {
			Expect(shouldProcessCSRObject(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{"system:authenticated"},
				},
			})).To(BeFalse())
		})

		It("ignores non-CSR objects", func() {
			Expect(shouldProcessCSRObject(&corev1.Secret{})).To(BeFalse())
		})
	})

	Describe("isDPUAgentCSR", func() {
		It("treats dpu bootstrap csr as dpu-agent csr", func() {
			Expect(isDPUAgentCSR(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{cutil.DPUAgentBootstrapGroup},
				},
			})).To(BeTrue())
		})

		It("treats dpu organization csr as dpu-agent csr", func() {
			Expect(isDPUAgentCSR(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{providentity.DPUAgentOrganization},
				},
			})).To(BeTrue())
		})

		It("does not treat unrelated csr as dpu-agent csr", func() {
			Expect(isDPUAgentCSR(&certmanager.CertificateSigningRequest{
				Spec: certmanager.CertificateSigningRequestSpec{
					Groups: []string{"unrelated-group"},
				},
			})).To(BeFalse())
		})
	})

	Describe("validateDPUAgentCSR", func() {
		It("accepts valid DPU agent CSR", func() {
			reconciler := &CSRReconciler{
				RuntimeClient: newFakeRuntimeClient(newTestDPU()),
			}

			csr := newTestCSR(testCSRSpec{
				commonName:   providentity.DPUAgentUsername("dpu-01"),
				organization: []string{providentity.DPUAgentOrganization},
				signerName:   certmanager.KubeAPIServerClientSignerName,
			})

			Expect(reconciler.validateDPUAgentCSR(ctx, csr)).To(Succeed())
		})

		It("reports an error for unsupported signer", func() {
			reconciler := &CSRReconciler{
				RuntimeClient: newFakeRuntimeClient(newTestDPU()),
			}

			csr := newTestCSR(testCSRSpec{
				commonName:   providentity.DPUAgentUsername("dpu-01"),
				organization: []string{providentity.DPUAgentOrganization},
				signerName:   "example.com/custom-signer",
			})

			expectValidationErrorContains(reconciler.validateDPUAgentCSR(ctx, csr), "unsupported signer")
		})

		It("reports an error for invalid common name", func() {
			reconciler := &CSRReconciler{
				RuntimeClient: newFakeRuntimeClient(newTestDPU()),
			}

			csr := newTestCSR(testCSRSpec{
				commonName:   "not-a-dpu-agent",
				organization: []string{providentity.DPUAgentOrganization},
				signerName:   certmanager.KubeAPIServerClientSignerName,
			})

			expectValidationErrorContains(reconciler.validateDPUAgentCSR(ctx, csr), "must match da-{dpu-name}")
		})

		It("reports an error for invalid organization", func() {
			reconciler := &CSRReconciler{
				RuntimeClient: newFakeRuntimeClient(newTestDPU()),
			}

			csr := newTestCSR(testCSRSpec{
				commonName:   providentity.DPUAgentUsername("dpu-01"),
				organization: []string{"system:nodes"},
				signerName:   certmanager.KubeAPIServerClientSignerName,
			})

			expectValidationErrorContains(reconciler.validateDPUAgentCSR(ctx, csr), "organization must be")
		})

		It("reports an error when the DPU does not exist", func() {
			reconciler := &CSRReconciler{
				RuntimeClient: newFakeRuntimeClient(),
			}

			csr := newTestCSR(testCSRSpec{
				commonName:   providentity.DPUAgentUsername("dpu-01"),
				organization: []string{providentity.DPUAgentOrganization},
				signerName:   certmanager.KubeAPIServerClientSignerName,
			})

			expectValidationErrorContains(reconciler.validateDPUAgentCSR(ctx, csr), "DPU "+hostutil.DPFNamespace+"/dpu-01 not found")
		})
	})
})

type testCSRSpec struct {
	commonName   string
	organization []string
	signerName   string
}

func newTestCSR(spec testCSRSpec) *certmanager.CertificateSigningRequest {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	requestDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   spec.commonName,
			Organization: spec.organization,
		},
	}, privateKey)
	Expect(err).NotTo(HaveOccurred())

	return &certmanager.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-csr",
		},
		Spec: certmanager.CertificateSigningRequestSpec{
			Request:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: requestDER}),
			SignerName: spec.signerName,
		},
	}
}

func newFakeRuntimeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func newTestDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-01",
			Namespace: hostutil.DPFNamespace,
		},
	}
}

func expectValidationErrorContains(err error, wantSubstring string) {
	var validationErr *csrValidationError
	Expect(errors.As(err, &validationErr)).To(BeTrue(), "expected csrValidationError")
	Expect(validationErr.Error()).To(ContainSubstring(wantSubstring))
}
