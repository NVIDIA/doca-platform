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

package redfish

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	redfishmock "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ConfigFWParameters", func() {
	const (
		defaultDPUName       = "dpu-config-fw-test"
		defaultDPUDeviceName = "dpu-device-config-fw-test"
	)

	createMockRedfishServer := func(version redfishmock.DpuVersion) *redfishmock.RedfishMockServer {
		bmcVersion := "BF-24.10-17"
		if version == redfishmock.BF4 {
			bmcVersion = "BF-26.04"
		}
		server := redfishmock.NewRedfishMockServer(bmcVersion, "password")
		server.SetDpuVersion(version)
		server.Start()
		return server
	}

	createBMCAndMTLSSecrets := func(mockServer *redfishmock.RedfishMockServer, clientSecretName string) {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bmc-shared-password", Namespace: testNS.Name},
			Data:       map[string][]byte{"password": []byte("password")},
		})).To(Succeed())

		_, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "dpf-provisioning-ca-secret", Namespace: testNS.Name},
			Data:       map[string][]byte{"tls.crt": mockServer.GetServerCertPEM()},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: clientSecretName, Namespace: testNS.Name},
			Data:       map[string][]byte{"tls.crt": clientCrt, "tls.key": clientKey},
		})).To(Succeed())
	}

	prepareDPUDevice := func(mockServer *redfishmock.RedfishMockServer, dpuType provisioningv1.DPUType) *provisioningv1.DPUDevice {
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)

		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
		dpuDevice.Status.DPUType = dpuType
		dpuDevice.Status.Conditions = []metav1.Condition{{
			Type:               string(provisioningv1.ConditionDpuDeviceReady),
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "DPUDevice is ready",
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: dpuDevice.Generation,
		}}
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
	}

	prepareBF3Fixture := func() (*redfishmock.RedfishMockServer, *provisioningv1.DPU) {
		mockServer := createMockRedfishServer(redfishmock.BF3)
		createBMCAndMTLSSecrets(mockServer, "dpf-provisioning-redfish-client-secret")
		dpuDevice := prepareDPUDevice(mockServer, provisioningv1.DPUTypeBlueField3)
		createObject(dpuFlavorObj("dpu-flavor"))
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
		return mockServer, dpu
	}

	It("should transition to DPUDeleting when DPU is being deleted", func() {
		now := metav1.Now()
		dpu := dpuObj(defaultDPUName)
		dpu.DeletionTimestamp = &now
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
	})

	It("should set host privilege to restricted for BF4 and advance to Firmware Update", func() {
		mockServer := createMockRedfishServer(redfishmock.BF4)
		defer mockServer.Stop()
		createBMCAndMTLSSecrets(mockServer, "dpf-provisioning-redfish-client-secret-bf4")
		dpuDevice := prepareDPUDevice(mockServer, provisioningv1.DPUTypeBlueField4)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		Expect(status.Conditions).To(ContainElement(
			HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
		))
		Expect(mockServer.GetHostPrivilegeMode()).To(Equal("Restricted"))
	})

	It("should fail when BF4 host privilege endpoint returns error", func() {
		mockServer := createMockRedfishServer(redfishmock.BF4)
		defer mockServer.Stop()
		mockServer.SetHostPrivilegeError(true)
		createBMCAndMTLSSecrets(mockServer, "dpf-provisioning-redfish-client-secret-bf4")
		dpuDevice := prepareDPUDevice(mockServer, provisioningv1.DPUTypeBlueField4)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).NotTo(Equal(provisioningv1.DPUPrepareBFB))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
				HaveField("Reason", "FailedToSetHostPrivilege"),
			),
		))
	})

	It("re-creates the per-DPU agent RBAC while waiting for the agent to apply NVConfig", func() {
		createObject(dpuFlavorObj("dpu-flavor"))
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		createObject(dpuDevice)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		createObject(dpu)
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
			PreInstall: &provisioningv1.AgentPreInstallStatus{AgentReported: ptr.To(metav1.Now())},
		}

		// The DPUSet generation that owned the previous RBAC was deleted, so garbage
		// collection took the Role and RoleBinding with it.
		rbacKey := client.ObjectKey{Name: "da-" + dpu.Name, Namespace: testNS.Name}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rbacKey, &rbacv1.Role{}))).To(BeTrue())

		status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
		Expect(k8sClient.Get(ctx, rbacKey, &rbacv1.Role{})).To(Succeed())
		Expect(k8sClient.Get(ctx, rbacKey, &rbacv1.RoleBinding{})).To(Succeed())
	})

	Context("BmcRShim poll (BF3)", func() {
		It("stays in ConfigFW with WaitingForBMCRShim when enable PATCH succeeds but flag is still false", func() {
			mockServer, dpu := prepareBF3Fixture()
			defer mockServer.Stop()

			status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondFWConfigured.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(waitingForBMCRShimReason))
		})

		It("advances to PrepareBFB on a later reconcile once BmcRShimEnabled is true", func() {
			mockServer, dpu := prepareBF3Fixture()
			defer mockServer.Stop()

			status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))

			mockServer.SetBMCRShimEnabled(true)
			dpu.Status = status
			status, err = ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondFWConfigured.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("advances in the same reconcile when BmcRShimEnabled is already true", func() {
			mockServer, dpu := prepareBF3Fixture()
			defer mockServer.Stop()
			mockServer.SetBMCRShimEnabled(true)

			status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
		})

		It("returns FailedToGetBMCRShim when GET Oem/Nvidia fails while waiting", func() {
			mockServer, dpu := prepareBF3Fixture()
			defer mockServer.Stop()

			status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))

			mockServer.SetBMCRShimGetError(true)
			dpu.Status = status
			status, err = ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondFWConfigured.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailedToGetBMCRShim"))
		})
	})
})
