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
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ConfigFWParameters", func() {
	var (
		defaultDPUName       = "dpu-config-fw-test"
		defaultDPUDeviceName = "dpu-device-config-fw-test"
	)

	createBF4MockRedfishServer := func() *redfishmock.RedfishMockServer {
		server := redfishmock.NewRedfishMockServer("BF-26.04", "password")
		server.SetDpuVersion(redfishmock.BF4)
		server.Start()
		return server
	}

	createBMCAndMTLSSecretsForBF4 := func(mockServerIP string) {
		By("create BMC credentials secret")
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bmc-shared-password",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"password": []byte("password"),
			},
		}
		Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

		By("create CA and client certificate secrets for mTLS")
		caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServerIP)

		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-ca-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": caCrt,
			},
		}
		Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())

		clientSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-redfish-client-secret-bf4",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": clientCrt,
				"tls.key": clientKey,
			},
		}
		Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())
	}

	prepareBF4DPUDevice := func(mockServer *redfishmock.RedfishMockServer) *provisioningv1.DPUDevice {
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)

		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
		dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField4
		dpuDevice.Status.Conditions = []metav1.Condition{
			{
				Type:               string(provisioningv1.ConditionDpuDeviceReady),
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				Message:            "DPUDevice is ready",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: dpuDevice.Generation,
			},
		}
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
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
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()

		createBMCAndMTLSSecretsForBF4(mockServer.GetIPAddress())

		dpuDevice := prepareBF4DPUDevice(mockServer)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := ConfigFWParameters(ctx, dpu,
			&dutil.ControllerContext{Client: k8sClient},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		Expect(status.Conditions).To(ContainElement(
			HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
		))
		Expect(mockServer.GetHostPrivilegeMode()).To(Equal("Restricted"))
	})

	It("should fail when BF4 host privilege endpoint returns error", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()
		mockServer.SetHostPrivilegeError(true)

		createBMCAndMTLSSecretsForBF4(mockServer.GetIPAddress())

		dpuDevice := prepareBF4DPUDevice(mockServer)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := ConfigFWParameters(ctx, dpu,
			&dutil.ControllerContext{Client: k8sClient},
		)
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).NotTo(Equal(provisioningv1.DPUPrepareBFB))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
				HaveField("Reason", "FailedToSetHostPrivilege"),
			),
		))
	})
})
