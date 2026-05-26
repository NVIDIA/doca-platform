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
	"encoding/base64"
	"fmt"
	"os"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
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

func TestDPUDeviceController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DPUDevice Controller Non exported Suite")
}

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
					Namespace: "test-namespace",
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

			Expect(cr.GetName()).To(Equal("test-dpudevice"))
			Expect(cr.GetNamespace()).To(Equal("test-namespace"))
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

	Context("createCR", func() {
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
					Namespace: "test-namespace",
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
			err := reconciler.createCR(ctx, dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			// Verify the CR was created
			createdCR := &unstructured.Unstructured{}
			createdCR.SetGroupVersionKind(crGVK())
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice",
				Namespace: "test-namespace",
			}, createdCR)
			Expect(err).NotTo(HaveOccurred())
			Expect(createdCR.GetName()).To(Equal("test-dpudevice"))
		})

		It("should fail if CR already exists", func() {
			// Create the CR first time
			err := reconciler.createCR(ctx, dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			// Try to create again - should fail
			err = reconciler.createCR(ctx, dpuDevice, testCSR)
			Expect(err).To(HaveOccurred())
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
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
			caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

			// CA secret that the client expects
			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-ca-secret",
					Namespace: "test-namespace",
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": caCrt,
				},
			}

			// Client certificate secret for mTLS
			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-redfish-client-secret",
					Namespace: "test-namespace",
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
			Expect(*dpuDevice.Status.PSID).To(Equal("N/A"))

			Expect(dpuDevice.Status.DPUMode).To(Equal(provisioningv1.DpuMode))

			Expect(dpuDevice.Status.PF0MAC).NotTo(BeNil())
			Expect(*dpuDevice.Status.PF0MAC).To(Equal("00:1B:21:C0:8F:32"))

			// Verify labels are set
			Expect(dpuDevice.Labels).NotTo(BeNil())
			Expect(dpuDevice.Labels).To(HaveKey("provisioning.dpu.nvidia.com/dpudevice-bmc-ip"))
			Expect(dpuDevice.Labels).To(HaveKey("provisioning.dpu.nvidia.com/dpudevice-opn"))
			Expect(dpuDevice.Labels).To(HaveKey("provisioning.dpu.nvidia.com/dpudevice-psid"))

			// Verify Secure Boot detection (default: enabled)
			Expect(dpuDevice.Status.SecureBoot).NotTo(BeNil())
			Expect(dpuDevice.Status.SecureBoot.Enabled).NotTo(BeNil())
			Expect(*dpuDevice.Status.SecureBoot.Enabled).To(BeTrue())
		})

		It("should detect Secure Boot disabled state", func() {
			ctx := context.Background()
			mockServer, reconciler := setupDiscoveryTest()
			defer mockServer.Stop()

			// Configure Secure Boot as disabled
			mockServer.SetSecureBootEnable(false)
			mockServer.ApplySecureBootAfterReboot()

			dpuDevice := createTestDPUDevice(mockServer, "test-dpudevice-sb-disabled")

			err := reconciler.discoverDPUDevice(ctx, dpuDevice)

			Expect(err).NotTo(HaveOccurred())
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

			err := reconciler.discoverDPUDevice(ctx, dpuDevice)

			Expect(err).To(HaveOccurred())
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
					Namespace: "test-namespace",
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
			caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-ca-secret",
					Namespace: "test-namespace",
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": caCrt,
				},
			}

			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-redfish-client-secret",
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
			caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-ca-secret",
					Namespace: "test-namespace",
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": caCrt,
				},
			}

			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-redfish-client-secret",
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
			testNS            = "test-namespace"
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
				},
				Data: map[string][]byte{
					"password": []byte(""),
				},
			}

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice-cred",
					Namespace: testNS,
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
					Namespace: testNS,
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

	Context("reconcile credential lifecycle", func() {
		const (
			testNS            = "test-namespace"
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
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
					Namespace: testNS,
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
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
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte(testPassword)},
			}
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV2,
					Namespace: testNS,
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updatedOld)).To(Succeed())
			Expect(updatedOld.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))

			updatedNew := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV2, Namespace: testNS}, updatedNew)).To(Succeed())
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
					Namespace: testNS,
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
				ObjectMeta: metav1.ObjectMeta{Name: sharedSecretName, Namespace: testNS},
				Data:       map[string][]byte{"password": []byte(testPassword)},
			}
			perDeviceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: perDeviceSecretV1, Namespace: testNS},
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
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
			testNS            = "test-namespace"
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
					Namespace: testNS,
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.ensureCredentialFinalizer(ctx, testNS, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should set secret to immutable when adding finalizer", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNS,
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.ensureCredentialFinalizer(ctx, testNS, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
			Expect(updated.Immutable).NotTo(BeNil())
			Expect(*updated.Immutable).To(BeTrue())
		})

		It("should not duplicate finalizer if already present", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.ensureCredentialFinalizer(ctx, testNS, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
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
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			err := reconciler.removeCredentialFinalizer(ctx, testNS, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
			Expect(updated.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should not error when removing finalizer from nonexistent secret", func() {
			err := reconciler.removeCredentialFinalizer(ctx, testNS, "nonexistent-secret")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should move finalizer from old to new secret during rotation", func() {
			oldSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("old-pass")},
			}
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV2,
					Namespace: testNS,
				},
				Data: map[string][]byte{"password": []byte("new-pass")},
			}
			Expect(fakeClient.Create(ctx, oldSecret)).To(Succeed())
			Expect(fakeClient.Create(ctx, newSecret)).To(Succeed())

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS},
			}

			err := reconciler.moveCredentialFinalizer(ctx, dpuDevice, perDeviceSecretV1, perDeviceSecretV2)
			Expect(err).NotTo(HaveOccurred())

			updatedOld := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updatedOld)).To(Succeed())
			Expect(updatedOld.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))

			updatedNew := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV2, Namespace: testNS}, updatedNew)).To(Succeed())
			Expect(updatedNew.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should skip finalizer operations for bmc-shared-password", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS},
			}

			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      perDeviceSecretV1,
					Namespace: testNS,
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, newSecret)).To(Succeed())

			err := reconciler.moveCredentialFinalizer(ctx, dpuDevice, sharedSecretName, perDeviceSecretV1)
			Expect(err).NotTo(HaveOccurred())

			updatedNew := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updatedNew)).To(Succeed())
			Expect(updatedNew.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should clean up finalizer on DPUDevice deletion with per-device secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updated)).To(Succeed())
			Expect(updated.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))
		})

		It("should not error on DPUDevice deletion with shared secret", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(sharedSecretName),
				},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not error on DPUDevice deletion with nil status secret name", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS},
				Status:     provisioningv1.DPUDeviceStatus{},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove finalizer from secret in Terminating state", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}
			Expect(fakeClient.Create(ctx, secret)).To(Succeed())

			Expect(fakeClient.Delete(ctx, secret)).To(Succeed())

			deleting := &corev1.Secret{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, deleting)).To(Succeed())
			Expect(deleting.DeletionTimestamp.IsZero()).To(BeFalse(), "secret should have a deletion timestamp")
			Expect(deleting.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer),
				"finalizer should still be present before cleanup")

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNS},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: ptr.To(perDeviceSecretV1),
				},
			}

			err := reconciler.cleanupCredentialFinalizer(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			finalSecret := &corev1.Secret{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, finalSecret)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(),
				"secret should be gone after finalizer removal since it was already marked for deletion")
		})

		It("should remove BMCCredentialFinalizer from DPUDevice on deletion via Reconcile", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       perDeviceSecretV1,
					Namespace:  testNS,
					Finalizers: []string{provisioningv1.BMCCredentialFinalizer},
				},
				Data: map[string][]byte{"password": []byte("pass")},
			}

			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dpudevice",
					Namespace:  testNS,
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
				NamespacedName: types.NamespacedName{Name: "test-dpudevice", Namespace: testNS},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Secret should have its finalizer removed
			updatedSecret := &corev1.Secret{}
			Expect(cl.Get(ctx, types.NamespacedName{Name: perDeviceSecretV1, Namespace: testNS}, updatedSecret)).To(Succeed())
			Expect(updatedSecret.Finalizers).NotTo(ContainElement(provisioningv1.BMCCredentialFinalizer))

			// DPUDevice should have its finalizer removed
			updatedDevice := &provisioningv1.DPUDevice{}
			err = cl.Get(ctx, types.NamespacedName{Name: "test-dpudevice", Namespace: testNS}, updatedDevice)
			if err == nil {
				Expect(controllerutil.ContainsFinalizer(updatedDevice, provisioningv1.BMCCredentialFinalizer)).To(BeFalse(),
					"BMCCredentialFinalizer should be removed from DPUDevice after deletion reconcile")
			}
		})
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
	caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(bmcIP)

	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpf-provisioning-ca-secret",
			Namespace: "test-namespace",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": caCrt,
		},
	}

	clientSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpf-provisioning-redfish-client-secret",
			Namespace: "test-namespace",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": clientCrt,
			"tls.key": clientKey,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(caSecret, clientSecret).
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
			Namespace: "test-namespace",
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
