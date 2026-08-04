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

package dpudevice

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// testNamespace is the namespace used by the BMC server-certificate rotation tests.
const testNamespace = "test-namespace"

// redfishClientCACertPEM is the CA that signed the Redfish client key pair mounted for the suite
// (see BeforeSuite). NewTLSClient self-checks that the mounted client cert chains to the CA trust
// bundle, so every dpf-ca-trust-bundle ConfigMap used with the verified mTLS client must include
// this CA in addition to the mock BMC server certificate. Use caTrustBundleWithClientCA to build
// such a bundle.
var redfishClientCACertPEM []byte

// caTrustBundleWithClientCA returns a PEM trust bundle containing both the mock BMC server
// certificate (so the BMC server cert verifies) and the Redfish client CA (so the mounted client
// cert passes the NewTLSClient chain self-check).
func caTrustBundleWithClientCA(serverCertPEM []byte) string {
	bundle := append([]byte{}, serverCertPEM...)
	bundle = append(bundle, '\n')
	bundle = append(bundle, redfishClientCACertPEM...)
	return string(bundle)
}

func TestDPUDeviceController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DPUDevice Controller Non exported Suite")
}

// The verified mTLS client reads its key pair from a mounted directory. Provide one for the suite so
// NewTLSClient can build the client in tests (the mock BMC does not require client auth).
var _ = BeforeSuite(func() {
	clientCACrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts("127.0.0.1")
	redfishClientCACertPEM = clientCACrt
	certDir, err := os.MkdirTemp("", "dpudevice-client-cert")
	Expect(err).NotTo(HaveOccurred())
	for _, d := range []string{certDir, certDir + "-bf4"} {
		Expect(os.MkdirAll(d, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(d, corev1.TLSCertKey), clientCrt, 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(d, corev1.TLSPrivateKeyKey), clientKey, 0o600)).To(Succeed())
	}
	rfclient.SetClientCertDir(certDir)
	DeferCleanup(func() {
		_ = os.RemoveAll(certDir)
		_ = os.RemoveAll(certDir + "-bf4")
	})
})

var _ = Describe("DPUDeviceController Non exported", func() {
	Context("generateCR", func() {
		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			testCSR    string
		)

		BeforeEach(func() {
			reconciler = &DPUDeviceReconciler{}
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: testNamespace,
					UID:       types.UID("test-uid-123"),
				},
			}
			testCSR = "-----BEGIN CERTIFICATE REQUEST-----\nMIIBVjCB...\n-----END CERTIFICATE REQUEST-----"
		})

		It("should generate a CertificateRequest with correct GVK", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())
			Expect(cr).NotTo(BeNil())

			gvk := cr.GroupVersionKind()
			Expect(gvk.Group).To(Equal("cert-manager.io"))
			Expect(gvk.Version).To(Equal("v1"))
			Expect(gvk.Kind).To(Equal("CertificateRequest"))
		})

		It("should set correct name and namespace", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			Expect(cr.GetName()).To(Equal("test-dpudevice-server"))
			Expect(cr.GetNamespace()).To(Equal(testNamespace))
		})

		It("should set owner reference to DPUDevice", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			ownerRefs := cr.GetOwnerReferences()
			Expect(ownerRefs).To(HaveLen(1))
			Expect(ownerRefs[0].Name).To(Equal("test-dpudevice"))
			Expect(ownerRefs[0].Kind).To(Equal("DPUDevice"))
			Expect(*ownerRefs[0].Controller).To(BeTrue())
		})

		It("should encode CSR in base64", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, found, err := unstructured.NestedMap(cr.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			request, found, err := unstructured.NestedString(spec, "request")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			// Decode and verify
			decoded, err := base64.StdEncoding.DecodeString(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(decoded)).To(Equal(testCSR))
		})

		It("should set isCA to false", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
			isCA, found, err := unstructured.NestedBool(spec, "isCA")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(isCA).To(BeFalse())
		})

		It("should set correct usages", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
			usages, found, err := unstructured.NestedStringSlice(spec, "usages")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(usages).To(ContainElements("server auth", "key encipherment", "digital signature"))
		})

		It("should set issuerRef", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
			issuerRef, found, err := unstructured.NestedMap(spec, "issuerRef")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(issuerRef["kind"]).To(Equal("Issuer"))
			Expect(issuerRef["group"]).To(Equal("cert-manager.io"))
		})
	})

	Context("createServerCertCR", func() {
		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			testCSR    string
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: testNamespace,
					UID:       types.UID("test-uid-123"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: fakeClient,
			}

			testCSR = "-----BEGIN CERTIFICATE REQUEST-----\nMIIBVjCB...\n-----END CERTIFICATE REQUEST-----"
		})

		It("should create CertificateRequest successfully", func() {
			err := reconciler.createServerCertCR(ctx, dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			// Verify the CR was created
			createdCR := &unstructured.Unstructured{}
			createdCR.SetGroupVersionKind(crGVK())
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice-server",
				Namespace: testNamespace,
			}, createdCR)
			Expect(err).NotTo(HaveOccurred())
			Expect(createdCR.GetName()).To(Equal("test-dpudevice-server"))
		})

		It("should fail if CR already exists", func() {
			// Create the CR first time
			err := reconciler.createServerCertCR(ctx, dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			// Try to create again - should fail
			err = reconciler.createServerCertCR(ctx, dpuDevice, testCSR)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("hostless DPUNode attachment", func() {
		It("should generate a valid synthetic DPUNode name for long DPUDevice names", func() {
			name := hostlessDPUNodeName("this-device-name-is-longer-than-the-dpunode-limit")

			Expect(name).To(HavePrefix("hostless-"))
			Expect(name).To(HaveLen(48))
		})

		It("should create a synthetic DPUNode and attach the DPUDevice", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bf3-cmx-01",
					Namespace: testNamespace,
					UID:       types.UID("hostless-device-uid"),
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/hostless": "true",
						"rack":                                 "r1",
					},
					Annotations: map[string]string{
						"owner": "storage",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004C7",
				},
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice).
				Build()
			reconciler := &DPUDeviceReconciler{Client: fakeClient}

			shouldContinue, result, err := reconciler.checkDPUNodeAttachment(ctx, dpuDevice)

			Expect(err).NotTo(HaveOccurred())
			Expect(shouldContinue).To(BeTrue())
			Expect(result).To(Equal(ctrl.Result{}))
			Expect(dpuDevice.Labels[provisioningv1.DPUNodeNameLabel]).To(Equal("hostless-bf3-cmx-01"))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceNodeAttached)).Status).To(Equal(metav1.ConditionTrue))

			dpuNode := &provisioningv1.DPUNode{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "hostless-bf3-cmx-01", Namespace: testNamespace}, dpuNode)).To(Succeed())
			Expect(dpuNode.Spec.DPUs).To(Equal([]provisioningv1.DPURef{{Name: "bf3-cmx-01"}}))
			Expect(dpuNode.Spec.NodeRebootMethod).NotTo(BeNil())
			Expect(dpuNode.Spec.NodeRebootMethod.None).NotTo(BeNil())
			Expect(dpuNode.Labels["provisioning.dpu.nvidia.com/hostless"]).To(Equal("true"))
			Expect(dpuNode.Labels[cutil.NodeSelectorLabel]).To(Equal("true"))
			Expect(dpuNode.Labels["rack"]).To(Equal("r1"))
			Expect(dpuNode.Annotations["owner"]).To(Equal("storage"))
			Expect(dpuNode.OwnerReferences).To(HaveLen(1))
			Expect(dpuNode.OwnerReferences[0].Kind).To(Equal(provisioningv1.DPUDeviceKind))
			Expect(dpuNode.OwnerReferences[0].Name).To(Equal("bf3-cmx-01"))
		})
	})

	Context("initializeDPUDevice", func() {
		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: fakeClient,
			}
		})

		It("should return error when BMCIP is nil", func() {
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: testNamespace,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: nil, // BMCIP is not set
				},
			}

			err := reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("BMCIP is required but not set"))

			// Verify condition is set
			conditions := dpuDevice.GetConditions()
			Expect(conditions).NotTo(BeEmpty())

			var foundCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1.ConditionDpuDeviceInitialized) {
					foundCondition = &conditions[i]
					break
				}
			}
			Expect(foundCondition).NotTo(BeNil())
			Expect(foundCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(foundCondition.Reason).To(Equal("MissingBMCIP"))
		})

		It("should fail when InitPassword fails", func() {
			bmcIP := "192.168.1.100"
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: testNamespace,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: &bmcIP,
				},
			}

			// This will fail because there's no BMC to connect to
			err := reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
			// The error could be about password initialization or connection
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("failed to initialize password"),
				ContainSubstring("connection"),
				ContainSubstring("dial"),
			))

			// Verify condition is set with failure
			conditions := dpuDevice.GetConditions()
			Expect(conditions).NotTo(BeEmpty())

			var foundCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1.ConditionDpuDeviceInitialized) {
					foundCondition = &conditions[i]
					break
				}
			}
			Expect(foundCondition).NotTo(BeNil())
			Expect(foundCondition.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("discoverDPUDevice", func() {
		It("should fail when TLS client creation fails (no secrets)", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: fakeClient,
			}

			bmcIP := "192.168.1.100"
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: testNamespace,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: &bmcIP,
				},
			}

			// Will fail because no TLS secrets exist in the fake client
			err := reconciler.discoverDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
		})

		It("should successfully discover a BlueField 3 card using Redfish mock", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			// Create and start the mock Redfish server
			mockServer, err := mock.CreateMockRedfishServer("BF-24.10", "testpassword")
			Expect(err).NotTo(HaveOccurred())
			defer mockServer.Stop()

			// Get the mock server address
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			// Create a DPUDevice with the mock server's address
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-bf3",
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: mock.DpuSerialNumber,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}

			// Create TLS secrets for the mock server
			// Generate mTLS certificates for testing
			_, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

			// CA trust bundle must include the mock BMC server certificate so the verified mTLS
			// client can validate the httptest self-signed server cert, and the Redfish client CA
			// so the mounted client cert passes the NewTLSClient chain self-check.
			caSecret := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-ca-trust-bundle",
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"ca.crt": caTrustBundleWithClientCA(mockServer.GetServerCertPEM()),
				},
			}

			// Client certificate secret for mTLS
			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-redfish-client-secret",
					Namespace: testNamespace,
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": clientCrt,
					"tls.key": clientKey,
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, caSecret, clientSecret).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: fakeClient,
			}

			// Call discoverDPUDevice
			err = reconciler.discoverDPUDevice(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the DPU type is BlueField 3
			Expect(dpuDevice.Status.DPUType).To(Equal(provisioningv1.DPUTypeBlueField3))

			// Verify other discovered fields
			Expect(dpuDevice.Status.SerialNumber).NotTo(BeNil())
			Expect(*dpuDevice.Status.SerialNumber).To(Equal(mock.DpuSerialNumber))

			Expect(dpuDevice.Status.OPN).NotTo(BeNil())
			Expect(*dpuDevice.Status.OPN).To(Equal(mock.DpuOPN))

			Expect(dpuDevice.Status.PSID).NotTo(BeNil())
			Expect(*dpuDevice.Status.PSID).To(Equal(mock.DpuPSIDBF3))

			Expect(dpuDevice.Status.DPUMode).To(Equal(provisioningv1.DpuMode))

			Expect(dpuDevice.Status.PF0MAC).NotTo(BeNil())
			Expect(*dpuDevice.Status.PF0MAC).To(Equal("00:1B:21:C0:8F:32"))

			// Verify labels are set
			Expect(dpuDevice.Labels).NotTo(BeNil())
			Expect(dpuDevice.Labels).To(HaveKey("provisioning.dpu.nvidia.com/dpudevice-bmc-ip"))
			Expect(dpuDevice.Labels).To(HaveKey("provisioning.dpu.nvidia.com/dpudevice-opn"))
			Expect(dpuDevice.Labels).To(HaveKeyWithValue("provisioning.dpu.nvidia.com/dpudevice-psid", mock.DpuPSIDBF3))

			// Verify Secure Boot detection (default: enabled)
			Expect(reconciler.reconcileDynamicFields(ctx, dpuDevice)).To(Succeed())
			Expect(dpuDevice.Status.SecureBoot).NotTo(BeNil())
			Expect(dpuDevice.Status.SecureBoot.Enabled).NotTo(BeNil())
			Expect(*dpuDevice.Status.SecureBoot.Enabled).To(BeTrue())
		})

		It("should set PSID from the DPU_BOARD firmware inventory on BlueField 3", func() {
			ctx := context.Background()
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-psid-na")

			Expect(reconciler.discoverDPUDevice(ctx, dpuDevice)).To(Succeed())
			Expect(dpuDevice.Status.PSID).NotTo(BeNil())
			Expect(*dpuDevice.Status.PSID).To(Equal(mock.DpuPSIDBF3))
			Expect(dpuDevice.Labels).To(HaveKeyWithValue("provisioning.dpu.nvidia.com/dpudevice-psid", mock.DpuPSIDBF3))
		})

		It("should set PSID from chassis AssetTag when it is not N/A", func() {
			ctx := context.Background()
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			mockServer.SetDpuVersion(mock.BF4)
			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-psid-valid")

			Expect(reconciler.discoverDPUDevice(ctx, dpuDevice)).To(Succeed())
			Expect(dpuDevice.Status.PSID).NotTo(BeNil())
			Expect(*dpuDevice.Status.PSID).To(Equal(mock.DpuPSIDBF4))
			Expect(dpuDevice.Labels).To(HaveKeyWithValue("provisioning.dpu.nvidia.com/dpudevice-psid", mock.DpuPSIDBF4))
		})

		It("should detect Secure Boot disabled state", func() {
			ctx := context.Background()
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			// Configure Secure Boot as disabled
			mockServer.SetSecureBootEnable(false)
			mockServer.ApplySecureBootAfterReboot()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-sb-disabled")

			Expect(reconciler.discoverDPUDevice(ctx, dpuDevice)).To(Succeed())
			Expect(reconciler.reconcileDynamicFields(ctx, dpuDevice)).To(Succeed())
			Expect(dpuDevice.Status.SecureBoot).NotTo(BeNil())
			Expect(dpuDevice.Status.SecureBoot.Enabled).NotTo(BeNil())
			Expect(*dpuDevice.Status.SecureBoot.Enabled).To(BeFalse())
		})

		It("should fail when Secure Boot detection fails", func() {
			ctx := context.Background()
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			// Simulate Secure Boot endpoint failure
			mockServer.SetSecureBootError(true)

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-sb-error")

			Expect(reconciler.discoverDPUDevice(ctx, dpuDevice)).To(Succeed())
			Expect(reconciler.reconcileDynamicFields(ctx, dpuDevice)).NotTo(Succeed())
		})

		It("should fail when DPU type is unknown in DPU mode", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			// Create and start the mock Redfish server
			mockServer, err := mock.CreateMockRedfishServer("BF-24.10", "testpassword")
			Expect(err).NotTo(HaveOccurred())
			defer mockServer.Stop()

			// Set a model that doesn't match the BlueField pattern to trigger unknown DPU type
			mockServer.SetModel("Unknown-DPU-Model")

			// Get the mock server address
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			// Create a DPUDevice with the mock server's address
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-unknown",
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: mock.DpuSerialNumber,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}

			// Create TLS secrets for the mock server
			_, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

			caSecret := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-ca-trust-bundle",
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"ca.crt": caTrustBundleWithClientCA(mockServer.GetServerCertPEM()),
				},
			}

			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-redfish-client-secret",
					Namespace: testNamespace,
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": clientCrt,
					"tls.key": clientKey,
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, caSecret, clientSecret).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: fakeClient,
			}

			// Call discoverDPUDevice - should fail with unknown DPU type error
			err = reconciler.discoverDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown DPU type"))

			// Verify that the DPU type is set to Unknown
			Expect(dpuDevice.Status.DPUType).To(Equal(provisioningv1.DPUTypeUnknown))

			// Verify that the DPU mode is DPU mode (default)
			Expect(dpuDevice.Status.DPUMode).To(Equal(provisioningv1.DpuMode))
		})

		It("should succeed when DPU type is unknown in NIC mode", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			// Create and start the mock Redfish server
			mockServer, err := mock.CreateMockRedfishServer("BF-24.10", "testpassword")
			Expect(err).NotTo(HaveOccurred())
			defer mockServer.Stop()

			// Set the DPU to NIC mode
			mockServer.SetNicMode("NicMode")

			// Set a model that doesn't match the BlueField pattern to trigger unknown DPU type
			mockServer.SetModel("Unknown-DPU-Model")

			// Get the mock server address
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			// Create a DPUDevice with the mock server's address
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-nic-unknown",
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: mock.DpuSerialNumber,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}

			// Create TLS secrets for the mock server
			_, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

			caSecret := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-ca-trust-bundle",
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"ca.crt": caTrustBundleWithClientCA(mockServer.GetServerCertPEM()),
				},
			}

			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-redfish-client-secret",
					Namespace: testNamespace,
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": clientCrt,
					"tls.key": clientKey,
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, caSecret, clientSecret).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: fakeClient,
			}

			// Call discoverDPUDevice - should succeed even with unknown DPU type because it's in NIC mode
			err = reconciler.discoverDPUDevice(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the DPU type is set to Unknown
			Expect(dpuDevice.Status.DPUType).To(Equal(provisioningv1.DPUTypeUnknown))

			// Verify that the DPU mode is NIC mode
			Expect(dpuDevice.Status.DPUMode).To(Equal(provisioningv1.NicMode))

			// Verify other fields are still set correctly
			Expect(dpuDevice.Status.SerialNumber).NotTo(BeNil())
			Expect(*dpuDevice.Status.SerialNumber).To(Equal(mock.DpuSerialNumber))
		})
	})

	Context("initializeDPUDevice BMC firmware update conditions", func() {
		const (
			testDeviceName = "test-dpudevice-bmc"
			testUID        = "test-uid-bmc"
		)

		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			ctx        context.Context
			mockServer *mock.RedfishMockServer
		)

		setupBMCUpdateTest := func(bmcVersion string) {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			var err error
			mockServer, err = mock.CreateMockRedfishServer(bmcVersion, "testpassword")
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDeviceName,
					Namespace: testNamespace,
					UID:       types.UID(testUID),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}

			passwdSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bmc-shared-password",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"password": []byte("testpassword"),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(passwdSecret).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: k8sClient,
			}
		}

		AfterEach(func() {
			if mockServer != nil {
				mockServer.Stop()
			}
			dutil.BmcFwUpdateTaskMap.Delete(fmt.Sprintf("%s-%s", testDeviceName, testUID))
		})

		It("should set Initialized=False when BMC update is started", func() {
			setupBMCUpdateTest("BF-24.04-1")

			tmpFile, err := os.CreateTemp("", "bf3-bmc-*.fwpkg")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpFile.Name()) }()
			_, err = tmpFile.WriteString("fake firmware data")
			Expect(err).NotTo(HaveOccurred())
			_ = tmpFile.Close()
			GinkgoT().Setenv("BMC_FW_FILE", tmpFile.Name())

			err = reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("Pending"))
			Expect(condition.Message).To(ContainSubstring("BMC firmware update started"))
			Expect(condition.Message).To(ContainSubstring("BF-24.04-1"))
		})

		It("should set Initialized=False with progress when BMC update is in progress", func() {
			setupBMCUpdateTest("BF-24.04-1")
			mockServer.SetTaskState("Running")

			taskName := fmt.Sprintf("%s-%s", dpuDevice.Name, dpuDevice.UID)
			dutil.BmcFwUpdateTaskMap.Store(taskName, "0")

			err := reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("Pending"))
			Expect(condition.Message).To(Equal("BMC firmware update in progress"))
		})

		It("should set Initialized=False with resetting message when BMC update is completed", func() {
			setupBMCUpdateTest("BF-24.04-1")
			mockServer.SetTaskState("Completed")

			taskName := fmt.Sprintf("%s-%s", dpuDevice.Name, dpuDevice.UID)
			dutil.BmcFwUpdateTaskMap.Store(taskName, "0")

			err := reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("Pending"))
			Expect(condition.Message).To(Equal("BMC firmware update completed, resetting BMC"))
		})
	})

	Context("resolveAndAuthenticateBMC status updates", func() {
		const (
			perDeviceSecretV1 = "my-dpu-bmc-v1"
			perDeviceSecretV2 = "my-dpu-bmc-v2"
			sharedSecretName  = "bmc-shared-password"
			testPassword      = "testpassword"
		)

		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			ctx        context.Context
			mockServer *mock.RedfishMockServer
		)

		setupWithPerDeviceSecret := func(password string) {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", password)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNamespace,
					UID:       types.UID("test-uid-cred"),
				},
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}

			perDeviceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"password": []byte(password),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(perDeviceSecret).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: k8sClient,
			}
		}

		setupWithSharedSecret := func(password string) {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", password)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNamespace,
					UID:       types.UID("test-uid-cred"),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}

			sharedSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sharedSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"password": []byte(password),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(sharedSecret).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: k8sClient,
			}
		}

		AfterEach(func() {
			if mockServer != nil {
				mockServer.Stop()
			}
		})

		It("should set status.bmcCredentialSecretName to spec value after successful per-device auth", func() {
			setupWithPerDeviceSecret(testPassword)

			bmcAddress := dpuDevice.BMCAddress()
			_, err := reconciler.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
			Expect(err).NotTo(HaveOccurred())

			Expect(dpuDevice.Status.BMCCredentialSecretName).NotTo(BeNil())
			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(perDeviceSecretV1))
		})

		It("should set status.bmcCredentialSecretName to bmc-shared-password after successful shared auth", func() {
			setupWithSharedSecret(testPassword)

			bmcAddress := dpuDevice.BMCAddress()
			_, err := reconciler.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
			Expect(err).NotTo(HaveOccurred())

			Expect(dpuDevice.Status.BMCCredentialSecretName).NotTo(BeNil())
			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(sharedSecretName))
		})

		It("should not update status.bmcCredentialSecretName on credential resolution failure", func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNamespace,
					UID:       types.UID("test-uid-cred"),
				},
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To("nonexistent-secret"),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: ptr.To("192.168.1.100"),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			bmcAddress := dpuDevice.BMCAddress()
			_, err := reconciler.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
			Expect(err).To(HaveOccurred())

			Expect(dpuDevice.Status.BMCCredentialSecretName).To(BeNil())
		})

		It("should set BMCCredentialsReady=True after successful authentication", func() {
			setupWithPerDeviceSecret(testPassword)

			bmcAddress := dpuDevice.BMCAddress()
			_, err := reconciler.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
			Expect(err).NotTo(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set BMCCredentialsReady=False with CredentialsSecretNotFound when referenced secret missing", func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To("nonexistent-secret"),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: ptr.To("192.168.1.100"),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			bmcAddress := dpuDevice.BMCAddress()
			_, err := reconciler.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
			Expect(err).To(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonCredentialsSecretNotFound))
			Expect(condition.Message).To(ContainSubstring("not found"))
		})

		It("should set BMCCredentialsReady=False with CredentialsSecretInvalid when secret malformed", func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			invalidSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"password": []byte(""),
				},
			}

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: ptr.To("192.168.1.100"),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(invalidSecret).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			bmcAddress := dpuDevice.BMCAddress()
			_, err := reconciler.resolveAndAuthenticateBMC(ctx, dpuDevice, bmcAddress, false)
			Expect(err).To(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonCredentialsSecretInvalid))
			Expect(condition.Message).To(ContainSubstring("empty or missing"))
		})

		It("should set BMCCredentialsReady=False with BMCAuthenticationFailed when password rejected", func() {
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNamespace,
				},
			}

			reconciler = &DPUDeviceReconciler{}
			reconciler.setBMCCredentialsConditionFromError(dpuDevice, fmt.Errorf("the default BMC password has been changed and the given password is wrong"))

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonBMCAuthenticationFailed))
			Expect(condition.Message).To(ContainSubstring("password is wrong"))
		})
	})

	Context("CA trust bundle condition reasons", func() {
		It("should set CATrustBundleReady=False with CATrustBundleUnavailable when bundle ConfigMap is missing", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: k8sClient,
			}

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpudevice-ca-unavailable",
					Namespace:  "test-namespace",
					Generation: 3,
				},
			}

			result, err := reconciler.reconcileCATrustBundle(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceCATrustBundleReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonCATrustBundleUnavailable))
			Expect(condition.Message).To(ContainSubstring("failed to get ConfigMap"))
			Expect(condition.ObservedGeneration).To(Equal(int64(3)))
		})

		It("should set CATrustBundleReady=False with CATrustBundleUnavailable when bundle-hash is missing", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			caCrt, _, _, _, _ := testutils.CreateMTLSCerts("127.0.0.1")
			caTrustBundle := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      CATrustBundleConfigMap,
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					CATrustBundleDataKey: string(caCrt),
				},
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(caTrustBundle).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: k8sClient,
			}

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpudevice-ca-missing-generation",
					Namespace:  "test-namespace",
					Generation: 5,
				},
			}

			result, err := reconciler.reconcileCATrustBundle(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceCATrustBundleReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonCATrustBundleUnavailable))
			Expect(condition.Message).To(ContainSubstring(`is missing "bundle-hash"`))
			Expect(condition.ObservedGeneration).To(Equal(int64(5)))
		})

		It("should write CATrustBundleSyncing reason on condition update", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpudevice-ca-syncing",
					Namespace:  "test-namespace",
					Generation: 7,
				},
			}

			setCATrustBundleCondition(
				dpuDevice,
				metav1.ConditionFalse,
				provisioningv1.ReasonCATrustBundleSyncing,
				"syncing BMC truststore with desired CA bundle",
			)

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceCATrustBundleReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonCATrustBundleSyncing))
			Expect(condition.Message).To(Equal("syncing BMC truststore with desired CA bundle"))
			Expect(condition.ObservedGeneration).To(Equal(int64(7)))
		})
	})

	Context("reconcile credential lifecycle", func() {
		const (
			testDeviceName    = "test-dpudevice"
			perDeviceSecretV1 = "my-dpu-bmc-v1"
			perDeviceSecretV2 = "my-dpu-bmc-v2"
			sharedSecretName  = "bmc-shared-password"
			testPassword      = "testpassword"
		)

		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			ctx        context.Context
			mockServer *mock.RedfishMockServer
			k8sClient  client.Client
		)

		buildScheme := func() *runtime.Scheme {
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = operatorv1.AddToScheme(scheme)
			return scheme
		}

		newDPFOperatorConfig := func() *operatorv1.DPFOperatorConfig {
			return &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpfoperatorconfig",
					Namespace: testNamespace,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						InstallInterface: &operatorv1.ProvisioningInstallInterface{
							InstallViaRedfish: &operatorv1.InstallViaRedfish{},
						},
					},
				},
			}
		}

		newDPUNode := func() *provisioningv1.DPUNode {
			return &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpunode",
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUNodeSpec{
					DPUs: []provisioningv1.DPURef{{Name: testDeviceName}},
				},
			}
		}

		newInitializedDPUDevice := func(bmcIP string, bmcPort uint32) *provisioningv1.DPUDevice {
			device := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDeviceName,
					Namespace: testNamespace,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP:   &bmcIP,
					BMCPort: &bmcPort,
				},
			}
			device.SetConditions([]metav1.Condition{
				{Type: "NodeAttached", Status: metav1.ConditionTrue, Reason: "Attached", LastTransitionTime: metav1.Now()},
				{Type: "Initialized", Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: metav1.Now()},
				{Type: "Discovered", Status: metav1.ConditionTrue, Reason: "Discovered", LastTransitionTime: metav1.Now()},
			})
			return device
		}

		AfterEach(func() {
			if mockServer != nil {
				mockServer.Stop()
			}
		})

		It("should no-op when spec matches status and ensure BMCCredentialsReady=True", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)
			dpuDevice.Status.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode()).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(perDeviceSecretV1))
			cond := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should recover BMCCredentialsReady after ModeSwitchNotAllowed is resolved", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)
			dpuDevice.Status.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)
			dpuDevice.SetConditions(append(dpuDevice.GetConditions(), metav1.Condition{
				Type:               string(provisioningv1.ConditionBMCCredentialsReady),
				Status:             metav1.ConditionFalse,
				Reason:             string(provisioningv1.ReasonModeSwitchNotAllowed),
				Message:            "Switching from per-device to shared credentials is not allowed.",
				LastTransitionTime: metav1.Now(),
			}))

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode()).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			cond := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should adopt per-device credential when status is nil - first adoption", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)
			dpuDevice.Status.BMCCredentialSecretName = nil

			perDeviceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte(testPassword)},
			}

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode(), perDeviceSecret).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			Expect(dpuDevice.Status.BMCCredentialSecretName).NotTo(BeNil())
			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(perDeviceSecretV1))

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))

			updated := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))

			Expect(controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer)).To(BeTrue(),
				"DPUDevice should have BMCCredentialFinalizer to ensure cleanup on deletion")
		})

		It("should rotate password when spec differs from status", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To(perDeviceSecretV2)
			dpuDevice.Status.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)

			oldSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte(testPassword)},
			}
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV2,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte(testPassword)},
			}

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode(), oldSecret, newSecret).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(perDeviceSecretV2))

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))

			updatedOld := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updatedOld)).To(Succeed())
			Expect(updatedOld.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))

			updatedNew := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV2, Namespace: testNamespace}, updatedNew)).To(Succeed())
			Expect(updatedNew.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should return error when referenced secret does not exist during adoption", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To("nonexistent-secret")
			dpuDevice.Status.BMCCredentialSecretName = nil

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode()).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())

			condition := findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonCredentialsSecretNotFound))

			Expect(dpuDevice.Status.BMCCredentialSecretName).To(BeNil())
		})

		It("should return error when old secret missing during rotation", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To(perDeviceSecretV2)
			dpuDevice.Status.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)

			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV2,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte(testPassword)},
			}

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode(), newSecret).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())

			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(perDeviceSecretV1))
		})

		It("should rotate from shared to per-device secret", func() {
			ctx = context.Background()
			scheme := buildScheme()

			var err error
			mockServer, err = mock.CreateMockRedfishServer("BF-24.10", testPassword)
			Expect(err).NotTo(HaveOccurred())

			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			dpuDevice = newInitializedDPUDevice(bmcIP, bmcPort)
			dpuDevice.Spec.BMCCredentialSecretName = ptr.To(perDeviceSecretV1)
			dpuDevice.Status.BMCCredentialSecretName = ptr.To(sharedSecretName)

			sharedSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: sharedSecretName, Namespace: testNamespace},
				Data:       map[string][]byte{"password": []byte(testPassword)},
			}
			perDeviceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: perDeviceSecretV1, Namespace: testNamespace},
				Data:       map[string][]byte{"password": []byte(testPassword)},
			}

			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice, newDPFOperatorConfig(), newDPUNode(), sharedSecret, perDeviceSecret).
				WithStatusSubresource(dpuDevice).
				Build()

			reconciler = &DPUDeviceReconciler{Client: k8sClient}

			_, err = reconciler.reconcile(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			Expect(*dpuDevice.Status.BMCCredentialSecretName).To(Equal(perDeviceSecretV1))

			updated := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})
	})

	Context("isModeSwitchAllowed", func() {
		const (
			perDeviceSecretV1 = "my-dpu-bmc-v1"
			perDeviceSecretV2 = "my-dpu-bmc-v2"
			sharedSecretName  = "bmc-shared-password"
		)

		var reconciler *DPUDeviceReconciler

		BeforeEach(func() {
			reconciler = &DPUDeviceReconciler{}
		})

		It("should return true when status.bmcCredentialSecretName is nil", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: nil,
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeTrue())
		})

		It("should return true when status is shared (bmc-shared-password)", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(sharedSecretName),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeTrue())
		})

		It("should return false when switching from per-device to nil", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: nil,
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeFalse())
		})

		It("should return false when switching from per-device to empty string", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(""),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeFalse())
		})

		It("should return false when switching from per-device to bmc-shared-password", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(sharedSecretName),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeFalse())
		})

		It("should return true when rotating from per-device v1 to v2", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV2),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeTrue())
		})

		It("should return true when switching from shared to per-device", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(sharedSecretName),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeTrue())
		})

		It("should return true when spec matches status (no change)", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}
			Expect(reconciler.isModeSwitchAllowed(dpuDevice)).To(BeTrue())
		})
	})

	Context("credential finalizer management", func() {
		const (
			perDeviceSecretV1 = "my-dpu-bmc-v1"
			perDeviceSecretV2 = "my-dpu-bmc-v2"
			sharedSecretName  = "bmc-shared-password"
		)

		var (
			reconciler *DPUDeviceReconciler
			ctx        context.Context
			fakeClient client.Client
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: fakeClient,
			}
		})

		It("should add finalizer to per-device secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.ensureCredentialFinalizer(ctx, testNamespace, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should set secret to immutable when adding finalizer", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.ensureCredentialFinalizer(ctx, testNamespace, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Immutable).NotTo(BeNil())
			Expect(*updated.Immutable).To(BeTrue())
		})

		It("should not duplicate finalizer if already present", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.ensureCredentialFinalizer(ctx, testNamespace, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			count := 0
			for _, f := range updated.Finalizers {
				if f == provisioningv1.BMCCredentialFinalizer {
					count++
				}
			}
			Expect(count).To(Equal(1))
		})

		It("should remove finalizer from secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.removeCredentialFinalizer(ctx, testNamespace, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should not error when removing finalizer from nonexistent secret", func() {
			err := reconciler.removeCredentialFinalizer(ctx, testNamespace, "nonexistent-secret")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should move finalizer from old to new secret during rotation", func() {
			oldSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("old-pass")},
			}
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV2,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte("new-pass")},
			}
			Expect(fakeClient.Create(ctx, oldSecret)).To(Succeed())
			Expect(fakeClient.Create(ctx, newSecret)).To(Succeed())

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
			}

			err := reconciler.moveCredentialFinalizer(ctx, dpuDevice, perDeviceSecretV1, perDeviceSecretV2)
			Expect(err).NotTo(HaveOccurred())

			updatedOld := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updatedOld)).To(Succeed())
			Expect(updatedOld.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))

			updatedNew := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV2, Namespace: testNamespace}, updatedNew)).To(Succeed())
			Expect(updatedNew.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should skip finalizer operations for bmc-shared-password", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
			}

			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, newSecret)).To(Succeed())

			err := reconciler.moveCredentialFinalizer(ctx, dpuDevice, sharedSecretName, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updatedNew := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updatedNew)).To(Succeed())
			Expect(updatedNew.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should clean up finalizer on DPUDevice deletion with per-device secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updated)).To(Succeed())
			Expect(updated.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should not error on DPUDevice deletion with shared secret", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(sharedSecretName),
				},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not error on DPUDevice deletion with nil status secret name", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
				Status:     provisioningv1.DPUDeviceStatus{},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove finalizer from secret in Terminating state", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			Expect(fakeClient.Delete(ctx, secret)).To(Succeed())

			deleting := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, deleting)).To(Succeed())
			Expect(deleting.DeletionTimestamp.IsZero()).To(BeFalse(), "secret should have a deletion timestamp")
			Expect(deleting.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer),
				"finalizer should still be present before cleanup")

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			finalSecret := &corev1.Secret{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, finalSecret)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(),
				"secret should be gone after finalizer removal since it was already marked for deletion")
		})

		It("should remove BMCCredentialFinalizer from DPUDevice on deletion via Reconcile", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpudevice",
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}

			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(secret, dpuDevice).
				WithStatusSubresource(dpuDevice).
				Build()

			r := &DPUDeviceReconciler{Client: cl}

			// Simulate deletion by setting DeletionTimestamp
			Expect(cl.Delete(ctx, dpuDevice)).To(Succeed())

			// Reconcile should clean up both finalizers
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-dpudevice", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Secret should have its finalizer removed
			updatedSecret := &corev1.Secret{}
			Expect(cl.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNamespace}, updatedSecret)).To(Succeed())
			Expect(updatedSecret.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))

			// DPUDevice should have its finalizer removed
			updatedDevice := &provisioningv1.DPUDevice{}
			err = cl.Get(ctx, types.NamespacedName{Name: "test-dpudevice", Namespace: testNamespace}, updatedDevice)
			if err == nil {
				Expect(controllerutil.ContainsFinalizer(updatedDevice, provisioningv1.BMCCredentialFinalizer)).To(BeFalse(),
					"BMCCredentialFinalizer should be removed from DPUDevice after deletion reconcile")
			}
		})
	})

	Context("certNotAfter", func() {
		It("returns the NotAfter of a valid certificate", func() {
			_, certPEM, _, _, _ := testutils.CreateMTLSCerts("127.0.0.1")

			notAfter, err := certNotAfter(string(certPEM))
			Expect(err).NotTo(HaveOccurred())
			Expect(notAfter.IsZero()).To(BeFalse())
			Expect(notAfter.After(time.Now())).To(BeTrue())
		})

		It("fails when the input contains no PEM block", func() {
			_, err := certNotAfter("not a pem")
			Expect(err).To(MatchError(ContainSubstring("no PEM block")))
		})

		It("fails when the PEM block is not a parseable certificate", func() {
			notACert := "-----BEGIN CERTIFICATE-----\naGVsbG8=\n-----END CERTIFICATE-----\n"
			_, err := certNotAfter(notACert)
			Expect(err).To(MatchError(ContainSubstring("failed to parse certificate")))
		})
	})

	Context("requeueUntil", func() {
		It("floors the requeue at minServerCertRequeue when the renew boundary is past", func() {
			// Already expired: time until the renew boundary is negative.
			Expect(requeueUntil(time.Now().Add(-time.Hour), defaultBMCServerCertRenewBefore)).To(Equal(minServerCertRequeue))
		})

		It("caps the requeue at maxServerCertRequeue for far-future certificates", func() {
			Expect(requeueUntil(time.Now().Add(serverCertDuration), defaultBMCServerCertRenewBefore)).To(Equal(maxServerCertRequeue))
		})

		It("returns the time until the renew boundary when within bounds", func() {
			// Renew boundary ~2h out (renewBefore zero): between min (1m) and max (24h).
			got := requeueUntil(time.Now().Add(2*time.Hour), 0)
			Expect(got).To(BeNumerically(">", minServerCertRequeue))
			Expect(got).To(BeNumerically("<", maxServerCertRequeue))
		})
	})

	Context("bmcServerCertRenewBefore", func() {
		It("falls back to the default when config is nil", func() {
			Expect(bmcServerCertRenewBefore(nil)).To(Equal(defaultBMCServerCertRenewBefore))
		})

		It("honors a configured value", func() {
			cfg := &operatorv1.DPFOperatorConfig{
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BMCServerCertRenewBefore: &metav1.Duration{Duration: 100 * time.Hour},
					},
				},
			}
			Expect(bmcServerCertRenewBefore(cfg)).To(Equal(100 * time.Hour))
		})

		It("clamps a zero value to half the cert duration", func() {
			cfg := &operatorv1.DPFOperatorConfig{
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BMCServerCertRenewBefore: &metav1.Duration{Duration: 0},
					},
				},
			}
			Expect(bmcServerCertRenewBefore(cfg)).To(Equal(serverCertDuration / 2))
		})

		It("clamps a value >= the cert duration to half the cert duration", func() {
			cfg := &operatorv1.DPFOperatorConfig{
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BMCServerCertRenewBefore: &metav1.Duration{Duration: serverCertDuration + time.Hour},
					},
				},
			}
			Expect(bmcServerCertRenewBefore(cfg)).To(Equal(serverCertDuration / 2))
		})
	})

	Context("installIssuedServerCert", func() {
		var (
			ctx       context.Context
			dpuDevice *provisioningv1.DPUDevice
		)

		BeforeEach(func() {
			ctx = context.Background()
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpudevice", Namespace: testNamespace},
			}
		})

		It("errors when the CertificateRequest vanished mid-rotation", func() {
			reconciler := &DPUDeviceReconciler{}
			cr := newServerCertRequest(dpuDevice)

			// mtlsClient is nil because it must not be used on this path.
			result, err := reconciler.installIssuedServerCert(nil, cr, apierrors.NewNotFound(schema.GroupResource{}, cr.GetName()))
			Expect(result).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("not found during rotation")))
		})

		It("returns errCertRequestPending while the request is unsigned", func() {
			reconciler := &DPUDeviceReconciler{}
			cr := newServerCertRequest(dpuDevice)

			result, err := reconciler.installIssuedServerCert(nil, cr, nil)
			Expect(result).To(BeNil())
			Expect(errors.Is(err, errCertRequestPending)).To(BeTrue())
		})

		setConditions := func(cr *unstructured.Unstructured, conds ...map[string]interface{}) {
			slice := make([]interface{}, 0, len(conds))
			for _, c := range conds {
				slice = append(slice, c)
			}
			Expect(unstructured.SetNestedSlice(cr.Object, slice, "status", "conditions")).To(Succeed())
		}

		It("returns a terminal error when cert-manager denied the request", func() {
			reconciler := &DPUDeviceReconciler{}
			cr := newServerCertRequest(dpuDevice)
			setConditions(cr, map[string]interface{}{"type": "Denied", "status": "True", "message": "not approved"})

			result, err := reconciler.installIssuedServerCert(nil, cr, nil)
			Expect(result).To(BeNil())
			Expect(errors.Is(err, errCertRequestPending)).To(BeFalse())
			Expect(err).To(MatchError(ContainSubstring("denied")))
		})

		It("returns a terminal error when the request is invalid", func() {
			reconciler := &DPUDeviceReconciler{}
			cr := newServerCertRequest(dpuDevice)
			setConditions(cr, map[string]interface{}{"type": "InvalidRequest", "status": "True", "message": "bad CSR"})

			result, err := reconciler.installIssuedServerCert(nil, cr, nil)
			Expect(result).To(BeNil())
			Expect(errors.Is(err, errCertRequestPending)).To(BeFalse())
			Expect(err).To(MatchError(ContainSubstring("invalid")))
		})

		It("returns a terminal error when issuance failed", func() {
			reconciler := &DPUDeviceReconciler{}
			cr := newServerCertRequest(dpuDevice)
			setConditions(cr, map[string]interface{}{"type": "Ready", "status": "False", "reason": "Failed", "message": "issuer error"})

			result, err := reconciler.installIssuedServerCert(nil, cr, nil)
			Expect(result).To(BeNil())
			Expect(errors.Is(err, errCertRequestPending)).To(BeFalse())
			Expect(err).To(MatchError(ContainSubstring("failed to issue")))
		})

		It("keeps waiting while Ready is False with a pending reason", func() {
			reconciler := &DPUDeviceReconciler{}
			cr := newServerCertRequest(dpuDevice)
			setConditions(cr, map[string]interface{}{"type": "Ready", "status": "False", "reason": "Pending", "message": "waiting for approval"})

			result, err := reconciler.installIssuedServerCert(nil, cr, nil)
			Expect(result).To(BeNil())
			Expect(errors.Is(err, errCertRequestPending)).To(BeTrue())
		})

		It("installs the issued certificate and returns its NotAfter", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice = createTestDPUDevice(mockServer, "test-dpudevice-install")
			mtlsClient, err := rfclient.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpuDevice.Namespace, reconciler.Client)
			Expect(err).NotTo(HaveOccurred())

			// A real, parseable certificate to stand in for the cert-manager-issued one.
			_, issuedCrt, _, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())
			expectedNotAfter, err := certNotAfter(string(issuedCrt))
			Expect(err).NotTo(HaveOccurred())

			cr := newServerCertRequest(dpuDevice)
			Expect(unstructured.SetNestedField(cr.Object, base64.StdEncoding.EncodeToString(issuedCrt), "status", "certificate")).To(Succeed())

			result, err := reconciler.installIssuedServerCert(mtlsClient, cr, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Time.Equal(expectedNotAfter)).To(BeTrue())
		})
	})

	Context("rotateServerCert dispatch", func() {
		var (
			ctx       context.Context
			dpuDevice *provisioningv1.DPUDevice
		)

		BeforeEach(func() {
			ctx = context.Background()
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpudevice", Namespace: testNamespace, UID: types.UID("uid")},
			}
		})

		newReconciler := func(objs ...client.Object) *DPUDeviceReconciler {
			scheme := runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			return &DPUDeviceReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
			}
		}

		It("errors when in-progress rotation finds no CertificateRequest", func() {
			reconciler := newReconciler()

			result, err := reconciler.rotateServerCert(ctx, dpuDevice, nil, true)
			Expect(result).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("not found during rotation")))
		})

		It("returns errCertRequestPending when the CertificateRequest exists but is unsigned", func() {
			reconciler := newReconciler()
			Expect(reconciler.createServerCertCR(ctx, dpuDevice, "dummy-csr")).To(Succeed())

			result, err := reconciler.rotateServerCert(ctx, dpuDevice, nil, true)
			Expect(result).To(BeNil())
			Expect(errors.Is(err, errCertRequestPending)).To(BeTrue())
		})
	})

	Context("backfillServerCertExpiry", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("records the served certificate's expiry and signals handled when outside the renew window", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-backfill")
			dpuDevice.Status.BMCServerCertificate = &provisioningv1.CertificateStatus{}
			mtlsClient, err := rfclient.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpuDevice.Namespace, reconciler.Client)
			Expect(err).NotTo(HaveOccurred())

			handled, result := reconciler.backfillServerCertExpiry(ctx, dpuDevice, mtlsClient, defaultBMCServerCertRenewBefore)
			Expect(handled).To(BeTrue())
			Expect(dpuDevice.Status.BMCServerCertificate.NotAfter).NotTo(BeNil())
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))
			Expect(result.RequeueAfter).To(BeNumerically("<=", maxServerCertRequeue))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)).Status).To(Equal(metav1.ConditionTrue))
		})

		It("records the expiry but signals not-handled when the served cert is already in the renew window", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-backfill-inwindow")
			dpuDevice.Status.BMCServerCertificate = &provisioningv1.CertificateStatus{}
			mtlsClient, err := rfclient.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpuDevice.Namespace, reconciler.Client)
			Expect(err).NotTo(HaveOccurred())

			// renewBefore larger than the served cert's remaining lifetime => already in window.
			handled, _ := reconciler.backfillServerCertExpiry(ctx, dpuDevice, mtlsClient, 100*365*24*time.Hour)
			Expect(handled).To(BeFalse())
			Expect(dpuDevice.Status.BMCServerCertificate.NotAfter).NotTo(BeNil())
		})
	})

	Context("reconcileServerCertRotation", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("is a no-op (Ready) when the expiry is known and outside the renew window", func() {
			scheme := runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			reconciler := &DPUDeviceReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpudevice", Namespace: testNamespace},
				Status: provisioningv1.DPUDeviceStatus{
					BMCServerCertificate: &provisioningv1.CertificateStatus{
						NotAfter: &metav1.Time{Time: time.Now().Add(200 * 24 * time.Hour)},
					},
				},
			}

			result, err := reconciler.reconcileServerCertRotation(ctx, dpuDevice, defaultBMCServerCertRenewBefore)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))
			Expect(result.RequeueAfter).To(BeNumerically("<=", maxServerCertRequeue))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)).Status).To(Equal(metav1.ConditionTrue))
		})

		It("keeps an in-progress rotation active when expiry is outside the renew window", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-rotation-in-progress")
			dpuDevice.UID = types.UID("test-dpudevice-rotation-in-progress")
			dpuDevice.Status.BMCServerCertificate = &provisioningv1.CertificateStatus{
				NotAfter: &metav1.Time{Time: time.Now().Add(200 * 24 * time.Hour)},
			}
			dpuDevice.Status.Conditions = []metav1.Condition{
				{
					Type:   string(provisioningv1.ConditionDpuDeviceBMCServerCertificateReady),
					Status: metav1.ConditionFalse,
					Reason: provisioningv1.ReasonBMCServerCertificateRotating,
				},
			}
			Expect(reconciler.createServerCertCR(ctx, dpuDevice, "dummy-csr")).To(Succeed())

			result, err := reconciler.reconcileServerCertRotation(ctx, dpuDevice, defaultBMCServerCertRenewBefore)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(serverCertIssuanceRequeue))

			cond := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCServerCertificateReady))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(provisioningv1.ReasonBMCServerCertificateRotating))
		})

		It("backfills the expiry on cold start instead of rotating", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-coldstart")

			result, err := reconciler.reconcileServerCertRotation(ctx, dpuDevice, defaultBMCServerCertRenewBefore)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuDevice.Status.BMCServerCertificate).NotTo(BeNil())
			Expect(dpuDevice.Status.BMCServerCertificate.NotAfter).NotTo(BeNil())
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)).Status).To(Equal(metav1.ConditionTrue))
		})

		It("marks rotation failed and backs off when the mTLS client cannot be opened", func() {
			scheme := runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			// No TLS secrets => NewTLSClient fails.
			reconciler := &DPUDeviceReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}

			bmcIP := "127.0.0.1"
			bmcPort := uint32(1) // nothing is listening here
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpudevice-nofail", Namespace: testNamespace},
				Status:     provisioningv1.DPUDeviceStatus{BMCIP: &bmcIP, BMCPort: &bmcPort},
			}

			result, err := reconciler.reconcileServerCertRotation(ctx, dpuDevice, defaultBMCServerCertRenewBefore)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(serverCertRotationBackoff))
			cond := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCServerCertificateReady))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(provisioningv1.ReasonBMCServerCertificateRotationFailed))
		})
	})

	Context("setUpMTLS server-cert self-heal", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		// newIssuedServerCertCR pre-creates the fixed-name CertificateRequest carrying an already
		// issued certificate, so setUpMTLS skips GenerateCSR and reaches the ReplaceServerCert step.
		newIssuedServerCertCR := func(reconciler *DPUDeviceReconciler, dpuDevice *provisioningv1.DPUDevice, certPEM []byte) {
			cr := &unstructured.Unstructured{}
			cr.SetGroupVersionKind(crGVK())
			cr.SetName(cutil.GenerateBMCServerCertRequestName(dpuDevice.Name))
			cr.SetNamespace(dpuDevice.Namespace)
			Expect(unstructured.SetNestedField(cr.Object,
				base64.StdEncoding.EncodeToString(certPEM), "status", "certificate")).To(Succeed())
			Expect(reconciler.Client.Create(ctx, cr)).To(Succeed())
		}

		serverCertCRExists := func(reconciler *DPUDeviceReconciler, dpuDevice *provisioningv1.DPUDevice) bool {
			cr := &unstructured.Unstructured{}
			cr.SetGroupVersionKind(crGVK())
			err := reconciler.Client.Get(ctx, types.NamespacedName{
				Name:      cutil.GenerateBMCServerCertRequestName(dpuDevice.Name),
				Namespace: dpuDevice.Namespace,
			}, cr)
			if apierrors.IsNotFound(err) {
				return false
			}
			Expect(err).NotTo(HaveOccurred())
			return true
		}

		It("deletes the stale CertificateRequest when the BMC rejects the server certificate", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()
			// Emulate a BMC key mismatch: ReplaceCertificate returns 500.
			mockServer.SetReplaceCertError(true)

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-mtls-selfheal")
			newIssuedServerCertCR(reconciler, dpuDevice, mockServer.GetServerCertPEM())

			basicAuthClient, err := rfclient.NewBasicAuthClient(dpuDevice.BMCAddress(), "root", "testpassword")
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.setUpMTLS(ctx, dpuDevice, basicAuthClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("replace server cert"))

			// The stale CertificateRequest must be deleted so the next pass regenerates a fresh CSR
			// against the BMC's current key instead of retrying the same orphaned certificate.
			Expect(serverCertCRExists(reconciler, dpuDevice)).To(BeFalse())
		})

		It("keeps the CertificateRequest when the server certificate installs successfully", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-mtls-success")
			newIssuedServerCertCR(reconciler, dpuDevice, mockServer.GetServerCertPEM())

			basicAuthClient, err := rfclient.NewBasicAuthClient(dpuDevice.BMCAddress(), "root", "testpassword")
			Expect(err).NotTo(HaveOccurred())

			needReset, err := reconciler.setUpMTLS(ctx, dpuDevice, basicAuthClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(needReset).To(BeFalse())

			// A successful install must NOT delete the CertificateRequest.
			Expect(serverCertCRExists(reconciler, dpuDevice)).To(BeTrue())
		})

		It("regenerates the CertificateRequest when the issued cert does not chain to the current CA", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-mtls-stale-ca")
			// Seed a CR whose leaf is signed by an unrelated CA so chain verification against the
			// current trust bundle fails (upgrade/self-heal path).
			newIssuedServerCertCR(reconciler, dpuDevice, generateUnrelatedServerCertPEM())

			basicAuthClient, err := rfclient.NewBasicAuthClient(dpuDevice.BMCAddress(), "root", "testpassword")
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.setUpMTLS(ctx, dpuDevice, basicAuthClient)
			Expect(err).To(HaveOccurred())

			// Stale issued CR must be deleted so the next reconcile creates a fresh CSR/CR against
			// the current CA (same effect as the manual kubectl delete workaround).
			Expect(serverCertCRExists(reconciler, dpuDevice)).To(BeFalse())
		})

		It("regenerates the CertificateRequest when the issued cert identity does not match the BMC IP", func() {
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-mtls-stale-identity")
			caPEM, leafPEM := generateCAAndLeafServerCertPEM("10.9.9.9")
			// Replace the trust bundle with a CA we control so the leaf chains, but pins the wrong IP.
			cm := &corev1.ConfigMap{}
			Expect(reconciler.Client.Get(ctx, types.NamespacedName{
				Name:      "dpf-ca-trust-bundle",
				Namespace: testNamespace,
			}, cm)).To(Succeed())
			cm.Data["ca.crt"] = string(caPEM)
			Expect(reconciler.Client.Update(ctx, cm)).To(Succeed())
			newIssuedServerCertCR(reconciler, dpuDevice, leafPEM)

			basicAuthClient, err := rfclient.NewBasicAuthClient(dpuDevice.BMCAddress(), "root", "testpassword")
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.setUpMTLS(ctx, dpuDevice, basicAuthClient)
			Expect(err).To(HaveOccurred())
			Expect(serverCertCRExists(reconciler, dpuDevice)).To(BeFalse())
		})
	})
})

// generateUnrelatedServerCertPEM returns a self-signed leaf that does not chain to the mock BMC CA
// trust bundle used by setupDiscoveryTest.
func generateUnrelatedServerCertPEM() []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "stale-bmc-server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// generateCAAndLeafServerCertPEM returns a CA PEM and a leaf signed by that CA whose identity is
// pinned to leafHost (CN and IP SAN when leafHost is an IP).
func generateCAAndLeafServerCertPEM(leafHost string) (caPEM, leafPEM []byte) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: leafHost},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(leafHost); ip != nil {
		leafTmpl.IPAddresses = []net.IP{ip}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	leafPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	return caPEM, leafPEM
}

var _ = Describe("issuedServerCertUsableForBMC", func() {
	It("accepts a leaf that chains to the CA and matches the BMC host", func() {
		caPEM, leafPEM := generateCAAndLeafServerCertPEM("10.1.2.3")
		Expect(issuedServerCertUsableForBMC(leafPEM, caPEM, "10.1.2.3")).To(Succeed())
	})

	It("rejects a leaf that does not chain to the provided CA bundle", func() {
		Expect(issuedServerCertUsableForBMC(generateUnrelatedServerCertPEM(), generateUnrelatedServerCertPEM(), "10.1.2.3")).NotTo(Succeed())
	})

	It("rejects a leaf that chains but does not match the BMC host", func() {
		caPEM, leafPEM := generateCAAndLeafServerCertPEM("10.9.9.9")
		err := issuedServerCertUsableForBMC(leafPEM, caPEM, "10.1.2.3")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("identity mismatch"))
	})
})

func findCondition(dpuDevice *provisioningv1.DPUDevice, conditionType string) *metav1.Condition {
	for i := range dpuDevice.GetConditions() {
		if dpuDevice.GetConditions()[i].Type == conditionType {
			return &dpuDevice.GetConditions()[i]
		}
	}
	return nil
}

// setupDiscoveryTest creates a mock Redfish server and reconciler with necessary secrets
func setupDiscoveryTest() (*mock.RedfishMockServer, *DPUDeviceReconciler) {
	scheme := runtime.NewScheme()
	_ = provisioningv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	mockServer, err := mock.CreateMockRedfishServer("BF-24.10", "testpassword")
	Expect(err).NotTo(HaveOccurred())

	bmcIP := mockServer.GetIPAddress()
	_, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

	caSecret := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpf-ca-trust-bundle",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"ca.crt": caTrustBundleWithClientCA(mockServer.GetServerCertPEM()),
		},
	}

	clientSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpf-provisioning-redfish-client-secret",
			Namespace: testNamespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": clientCrt,
			"tls.key": clientKey,
		},
	}

	clientSecretBF4 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpf-provisioning-redfish-client-secret-bf4",
			Namespace: testNamespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": clientCrt,
			"tls.key": clientKey,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(caSecret, clientSecret, clientSecretBF4).
		Build()

	reconciler := &DPUDeviceReconciler{
		Client: fakeClient,
	}

	return mockServer, reconciler
}

// createTestDPUDevice creates a DPUDevice test object configured for the given mock server
func createTestDPUDevice(mockServer *mock.RedfishMockServer, name string) *provisioningv1.DPUDevice {
	bmcIP := mockServer.GetIPAddress()
	bmcPort := uint32(mockServer.GetPort())

	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: mock.DpuSerialNumber,
		},
		Status: provisioningv1.DPUDeviceStatus{
			BMCIP:   &bmcIP,
			BMCPort: &bmcPort,
		},
	}
}

// Helper to get CertificateRequest GVK
func crGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	}
}
