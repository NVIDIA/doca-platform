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
	"net/http"
	"time"

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

var _ = Describe("BMCForceRestart", func() {
	const (
		dpuName       = "dpu-bmc-force-restart-test"
		dpuDeviceName = "dpu-device-bmc-force-restart-test"
	)

	var (
		mockServer *redfishmock.RedfishMockServer
		dpu        *provisioningv1.DPU
		ctrlCtx    *dutil.ControllerContext
	)

	BeforeEach(func() {
		mockServer = redfishmock.NewRedfishMockServer("BF-26.04", "password")
		mockServer.SetDpuVersion(redfishmock.BF4)
		mockServer.Start()

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
			ObjectMeta: metav1.ObjectMeta{Name: "dpf-provisioning-redfish-client-secret-bf4", Namespace: testNS.Name},
			Data:       map[string][]byte{"tls.crt": clientCrt, "tls.key": clientKey},
		})).To(Succeed())

		dpuDevice := dpuDeviceObj(dpuDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
		dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField4
		dpuDevice.Status.Conditions = []metav1.Condition{{
			Type:               string(provisioningv1.ConditionDpuDeviceReady),
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.Now(),
		}}
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

		dpu = dpuObj(dpuName)
		dpu.Spec.DPUDeviceName = dpuDeviceName
		dpu.Status.Phase = provisioningv1.DPUBMCForceRestart
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		// FwBundleActivated survives the update cycle so OSInstalling picks a host power cycle.
		dpu.Status.Conditions = []metav1.Condition{{
			Type:               provisioningv1.DPUCondFwBundleActivated.String(),
			Status:             metav1.ConditionTrue,
			Reason:             "Activated",
			LastTransitionTime: metav1.Now(),
		}}

		ctrlCtx = &dutil.ControllerContext{Client: k8sClient}
	})

	AfterEach(func() {
		mockServer.Stop()
	})

	reconcile := func() (provisioningv1.DPUStatus, error) {
		status, err := BMCForceRestart(ctx, dpu, ctrlCtx)
		dpu.Status = status
		return status, err
	}

	restartCondition := func(status provisioningv1.DPUStatus) *metav1.Condition {
		for i := range status.Conditions {
			if status.Conditions[i].Type == provisioningv1.DPUCondBMCForceRestarted.String() {
				return &status.Conditions[i]
			}
		}
		return nil
	}

	It("should restart the BMC once and move to PrepareBFB when it is back", func() {
		By("requesting one ForceRestart and recording the LastResetTime read before it")
		status, err := reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUBMCForceRestart))
		Expect(status.BMCLastResetTimeBeforeRestart).To(HaveValue(Equal(mockServer.GetBMCLastResetTime())))
		Expect(restartCondition(status)).To(And(
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "Requested"),
		))
		Expect(mockServer.GetBMCResetTypes()).To(Equal([]string{"ForceRestart"}))

		By("not sending the restart again while the BMC still answers with the old LastResetTime")
		status, err = reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(restartCondition(status)).To(HaveField("Reason", "Requested"))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(1))

		By("waiting while the BMC is rebooting, without reporting an error")
		mockServer.SetBMCManagerDown(true)
		status, err = reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUBMCForceRestart))
		Expect(restartCondition(status)).To(HaveField("Reason", "Requested"))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(1))

		By("moving to PrepareBFB once the BMC reports a new LastResetTime")
		mockServer.SetBMCManagerDown(false)
		mockServer.BumpBMCLastResetTime()
		status, err = reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
		Expect(restartCondition(status)).To(And(
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", "Completed"),
		))
		Expect(status.Conditions).To(ContainElement(And(
			HaveField("Type", provisioningv1.DPUCondFwBundleActivated.String()),
			HaveField("Status", metav1.ConditionTrue),
		)))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(1))
	})

	It("should not send the restart again when only the recorded LastResetTime survived", func() {
		// The condition may have been overwritten by another branch or lost with a controller
		// restart; the recorded LastResetTime alone must prevent a second request.
		dpu.Status.BMCLastResetTimeBeforeRestart = ptr.To(mockServer.GetBMCLastResetTime())

		status, err := reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUBMCForceRestart))
		Expect(restartCondition(status)).To(BeNil())
		Expect(mockServer.GetBMCResetRequests()).To(Equal(0))

		mockServer.BumpBMCLastResetTime()
		status, err = reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
		Expect(restartCondition(status)).To(HaveField("Status", metav1.ConditionTrue))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(0))
	})

	It("should retry the restart request when Manager.Reset fails", func() {
		mockServer.SetBMCResetStatus(http.StatusInternalServerError)
		status, err := reconcile()
		Expect(err).To(MatchError(ContainSubstring("unexpected status code from Manager.Reset: 500")))
		Expect(status.Phase).To(Equal(provisioningv1.DPUBMCForceRestart))
		Expect(status.BMCLastResetTimeBeforeRestart).To(BeNil())
		Expect(restartCondition(status)).To(And(
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "FailedToRequestBMCReset"),
		))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(1))

		mockServer.SetBMCResetStatus(0)
		status, err = reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.BMCLastResetTimeBeforeRestart).NotTo(BeNil())
		Expect(restartCondition(status)).To(HaveField("Reason", "Requested"))
		Expect(mockServer.GetBMCResetTypes()).To(Equal([]string{"ForceRestart", "ForceRestart"}))
	})

	It("should report the error when the BMC manager cannot be read before the restart", func() {
		mockServer.SetBMCManagerDown(true)

		status, err := reconcile()
		Expect(err).To(MatchError(ContainSubstring("failed to read the BMC manager")))
		Expect(status.BMCLastResetTimeBeforeRestart).To(BeNil())
		Expect(restartCondition(status)).To(HaveField("Reason", "FailedToGetBMCManager"))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(0))
	})

	It("should transition to DPUError when the firmware update timeout is exceeded while waiting", func() {
		dpu.Status.BMCLastResetTimeBeforeRestart = ptr.To("2026-01-01T00:00:00Z")
		dpu.Status.Conditions = append(dpu.Status.Conditions,
			metav1.Condition{
				Type:               provisioningv1.DPUCondInterfaceInitialized.String(),
				Status:             metav1.ConditionTrue,
				Reason:             provisioningv1.DPUCondInterfaceInitialized.String(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
			},
			metav1.Condition{
				Type:               provisioningv1.DPUCondBMCForceRestarted.String(),
				Status:             metav1.ConditionFalse,
				Reason:             "Requested",
				LastTransitionTime: metav1.NewTime(time.Now().Add(-80 * time.Minute)),
			},
		)
		mockServer.SetBMCManagerDown(true)
		ctrlCtx.Options.FirmwareUpdateTimeout = time.Hour

		status, err := reconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(restartCondition(status)).To(HaveField("Reason", "FirmwareUpdateTimeout"))
		Expect(mockServer.GetBMCResetRequests()).To(Equal(0))
	})
})
