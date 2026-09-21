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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	redfishmock "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ConfigFWParameters", func() {
	const (
		defaultDPUName       = "dpu-config-fw-test"
		defaultDPUDeviceName = "dpu-device-config-fw-test"
	)

	createMockRedfishServer := func() *redfishmock.RedfishMockServer {
		bmcVersion := "BF-24.10-17"

		server := redfishmock.NewRedfishMockServer(bmcVersion, "password")
		server.Start()
		return server
	}

	// Helper function to create BMC and mTLS certificate secrets
	createBMCAndMTLSSecrets := func(mockServerIP string) {
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

		By("Create CA and client certificate secrets for mTLS")
		// Generate mTLS certificates for testing
		caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServerIP)

		// Create CA certificate secret
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

		// Create client certificate secret
		clientSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-redfish-client-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": clientCrt,
				"tls.key": clientKey,
			},
		}
		Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())
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
		mockServer := createMockRedfishServer()
		createBMCAndMTLSSecrets(mockServer.GetIPAddress())
		dpuDevice := prepareDPUDevice(mockServer, provisioningv1.DPUTypeBlueField3)
		createObject(dpuFlavorObj("dpu-flavor"))
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
		return mockServer, dpu
	}

	It("force-restarts the DPU Arm when PowerState is Paused and advances once it powers on", func() {
		mockServer, dpu := prepareBF3Fixture()
		defer mockServer.Stop()
		mockServer.SetSystemPowerState("Paused")
		mockServer.SetBMCRShimEnabled(true)

		status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
		Expect(mockServer.GetLastResetType()).To(Equal("ForceRestart"))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondFWArmRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))

		mockServer.SetSystemPowerState("")
		dpu.Status = status
		status, err = ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
	})

	It("goes to DPUError when the Arm does not power on within the timeout", func() {
		mockServer, dpu := prepareBF3Fixture()
		defer mockServer.Stop()
		mockServer.SetSystemPowerState("Paused")
		// SetDPUCondition always stamps LastTransitionTime with now, so the expired
		// restart condition has to be injected directly.
		dpu.Status.Conditions = append(dpu.Status.Conditions, metav1.Condition{
			Type:               provisioningv1.DPUCondFWArmRestarted.String(),
			Status:             metav1.ConditionTrue,
			Reason:             provisioningv1.DPUCondFWArmRestarted.String(),
			LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * armPowerOnWaitTimeout)),
		})

		status, err := ConfigFWParameters(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondFWConfigured.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("DPUArmPowerOnTimeout"))
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
