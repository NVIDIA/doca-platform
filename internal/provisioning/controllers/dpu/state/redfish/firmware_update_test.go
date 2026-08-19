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
	"os"
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

var _ = Describe("FirmwareUpdate", func() {
	const (
		defaultDPUName         = "dpu-firmware-update-test"
		defaultDPUDeviceName   = "dpu-device-firmware-update-test"
		defaultBlueFieldSWName = "bluefield-software-firmware-update-test"
		targetBMCVersion       = "BF-26.04"
		targetBMCErotVersion   = "01.04.0000.0000"
		targetSBIOSVersion     = "3.20.00"
		targetBFNicFwVersion   = "32.44.1014"
	)

	createBF4MockRedfishServer := func() *redfishmock.RedfishMockServer {
		server := redfishmock.NewRedfishMockServer(targetBMCVersion, "password")
		server.SetDpuVersion(redfishmock.BF4)
		server.SetFirmwareVersions(
			targetBMCVersion,
			targetBMCErotVersion,
			targetSBIOSVersion,
			targetBFNicFwVersion,
		)
		server.Start()
		return server
	}

	createBMCAndMTLSSecretsForBF4 := func(mockServer *redfishmock.RedfishMockServer) {
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
		// The verified mTLS client validates the server cert, so the CA secret must trust the
		// cert the mock server actually serves (its httptest leaf, which carries the 127.0.0.1 SAN).
		_, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())

		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-ca-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": mockServer.GetServerCertPEM(),
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

	prepareBF4DPUDevice := func(mockServer *redfishmock.RedfishMockServer) {
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
	}

	createBlueFieldSoftware := func(pldmFwBundlePath string, withVersions bool) {
		bfs := &provisioningv1.BlueFieldSoftware{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBlueFieldSWName,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.BlueFieldSpec{
				OsIso: "https://test.com/os.iso",
			},
		}
		createObject(bfs)

		patch := client.MergeFrom(bfs.DeepCopy())
		bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
		if pldmFwBundlePath != "" {
			bfs.Status.DownloadedComponents.PldmFwBundle = pldmFwBundlePath
		}
		if withVersions {
			bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{
				BMCVersion:     targetBMCVersion,
				BMCErotVersion: targetBMCErotVersion,
				SBIOSVersion:   targetSBIOSVersion,
				BFNicFwVersion: targetBFNicFwVersion,
			}
		}
		Expect(k8sClient.Status().Patch(ctx, bfs, patch)).To(Succeed())
	}

	createTempPldmFwBundle := func() string {
		tmpFile, err := os.CreateTemp("", "pldm-fw-bundle-*.tar.gz")
		Expect(err).NotTo(HaveOccurred())
		_, err = tmpFile.Write([]byte("test-pldm-fw-bundle"))
		Expect(err).NotTo(HaveOccurred())
		Expect(tmpFile.Close()).To(Succeed())
		return tmpFile.Name()
	}

	It("should advance to PrepareBFB when no PLDM firmware bundle is configured", func() {
		createBlueFieldSoftware("", false)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "NoPldmFwBundle"),
				HaveField("Status", metav1.ConditionTrue),
			),
		))
	})

	It("should wait when PLDM is configured but BlueFieldSoftware is re-downloading", func() {
		bfs := &provisioningv1.BlueFieldSoftware{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBlueFieldSWName,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.BlueFieldSpec{
				OsIso:        "https://test.com/os.iso",
				PldmFwBundle: ptr.To("https://test.com/fw.fwpkg"),
			},
		}
		createObject(bfs)

		patch := client.MergeFrom(bfs.DeepCopy())
		bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDownloading
		Expect(k8sClient.Status().Patch(ctx, bfs, patch)).To(Succeed())

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "WaitingForPldmFwBundle"),
				HaveField("Status", metav1.ConditionFalse),
			),
		))
	})

	It("should wait when PLDM is configured, BlueFieldSoftware is Ready, but downloaded path is empty", func() {
		bfs := &provisioningv1.BlueFieldSoftware{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBlueFieldSWName,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.BlueFieldSpec{
				OsIso:        "https://test.com/os.iso",
				PldmFwBundle: ptr.To("https://test.com/fw.fwpkg"),
			},
		}
		createObject(bfs)

		patch := client.MergeFrom(bfs.DeepCopy())
		bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
		Expect(k8sClient.Status().Patch(ctx, bfs, patch)).To(Succeed())

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "WaitingForPldmFwBundle"),
				HaveField("Status", metav1.ConditionFalse),
			),
		))
	})

	It("should advance to PrepareBFB when firmware versions already match", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
		Expect(status.Conditions).To(ContainElement(
			HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
		))
	})

	It("should return error when BlueFieldSoftware is not found", func() {
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BlueFieldSoftware = ptr.To("missing-bluefield-software")
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "BlueFieldSoftwareNotFound"),
			),
		))
	})

	It("should transition to DPUError when firmware update timeout is exceeded", func() {
		createBlueFieldSoftware("", false)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		dpu.Status.Conditions = []metav1.Condition{
			{
				Type:               provisioningv1.DPUCondFWConfigured.String(),
				Status:             metav1.ConditionTrue,
				Reason:             "Configured",
				Message:            "FW configured",
				LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
			},
		}

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{
			Client: k8sClient,
			Options: dutil.DPUOptions{
				FirmwareUpdateTimeout: time.Hour,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "FirmwareUpdateTimeout"),
			),
		))
	})

	It("should submit PLDM firmware update when versions mismatch", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()
		mockServer.SetFirmwareVersions("old-bmc", "old-erot", "old-sbios", "old-nic")

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.RedfishTaskID).NotTo(BeNil())
		Expect(*status.RedfishTaskID).To(Equal("0"))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleSubmitted.String()),
				HaveField("Reason", "Submitting"),
			),
		))
		Expect(mockServer.GetLastForceUpdate()).To(BeFalse())
	})

	It("should force the PLDM firmware update when the DPU is annotated and versions mismatch", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()
		mockServer.SetFirmwareVersions("old-bmc", "old-erot", "old-sbios", "old-nic")

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Annotations = map[string]string{cutil.DPUForceFwUpdateAnnotation: "true"}
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.RedfishTaskID).NotTo(BeNil())
		Expect(mockServer.GetLastForceUpdate()).To(BeTrue())
	})

	It("should complete PLDM firmware update after task completion and activation", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()
		mockServer.SetFirmwareVersions("old-bmc", "old-erot", "old-sbios", "old-nic")

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

		ctrlCtx := &dutil.ControllerContext{Client: k8sClient}

		By("submitting PLDM firmware update")
		status, err := FirmwareUpdate(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.RedfishTaskID).NotTo(BeNil())

		By("monitoring task, activating pending bundle and shutting down the DPU Arm")
		dpu.Status = status
		status, err = FirmwareUpdate(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))

		By("moving to Rebooting once the DPU Arm is powered off")
		dpu.Status = status
		status, err = FirmwareUpdate(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		Expect(status.RedfishTaskID).To(BeNil())
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "Updated"),
			),
		))

		By("advancing to PrepareBFB after firmware update completes")
		dpu.Status = status
		status, err = FirmwareUpdate(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPrepareBFB))
	})

	It("should report FirmwareVersionsMismatch after reboot when versions still mismatch", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()
		mockServer.SetFirmwareVersions("old-bmc", "old-erot", "old-sbios", "old-nic")

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.PreviousPhase = provisioningv1.DPURebooting
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(
			provisioningv1.DPUCondFwBundleUpdated.String(),
			nil,
			"Updated",
			"PLDM Firmware Updated",
		))

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFwBundleUpdated.String()),
				HaveField("Reason", "FirmwareVersionsMismatch"),
			),
		))
	})

	It("should transition to Rebooting when bundle was activated on another reconcile", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(
			provisioningv1.DPUCondFwBundleUpdated.String(),
			nil,
			"Updated",
			"PLDM Firmware Updated",
		))

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		Expect(status.RebootStatus).NotTo(BeNil())
		Expect(*status.RebootStatus.Method).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
	})

	It("should transition to DPUError when task monitoring fails", func() {
		mockServer := createBF4MockRedfishServer()
		defer mockServer.Stop()
		mockServer.SetFirmwareVersions("old-bmc", "old-erot", "old-sbios", "old-nic")
		mockServer.SetTaskState("Exception")
		mockServer.SetTaskMessages([]map[string]interface{}{
			{"Message": "PLDM update failed"},
		})

		createBMCAndMTLSSecretsForBF4(mockServer)
		prepareBF4DPUDevice(mockServer)

		pldmPath := createTempPldmFwBundle()
		defer func() { _ = os.Remove(pldmPath) }()
		createBlueFieldSoftware(pldmPath, true)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUDeviceName = defaultDPUDeviceName
		dpu.Spec.BlueFieldSoftware = ptr.To(defaultBlueFieldSWName)
		dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(
			provisioningv1.DPUCondFwBundleSubmitted.String(),
			nil,
			"Submitting",
			"Submitting PLDM Firmware",
		))
		taskID := "0"
		dpu.Status.RedfishTaskID = &taskID

		status, err := FirmwareUpdate(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(status.RedfishTaskID).To(BeNil())
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
				HaveField("Reason", "FailedToUpdatePldmFwBundle"),
			),
		))
	})

	Context("ERoT background copy status", func() {
		preparePldmUpdate := func(mockServer *redfishmock.RedfishMockServer) (*provisioningv1.DPU, string) {
			createBMCAndMTLSSecretsForBF4(mockServer)
			prepareBF4DPUDevice(mockServer)

			pldmPath := createTempPldmFwBundle()
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = defaultDPUDeviceName
			dpu.Status.Phase = provisioningv1.DPUUpdateFirmware
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
			return dpu, pldmPath
		}

		It("should wait when ERoT background copy is not completed", func() {
			mockServer := createBF4MockRedfishServer()
			defer mockServer.Stop()
			mockServer.SetErotBackgroundCopyStatus("InProgress")

			dpu, pldmPath := preparePldmUpdate(mockServer)
			defer func() { _ = os.Remove(pldmPath) }()

			status, err := updatePldmFwBundle(ctx, dpu, &dutil.ControllerContext{Client: k8sClient}, pldmPath, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.RedfishTaskID).To(BeNil())
			Expect(status.Phase).To(Equal(provisioningv1.DPUUpdateFirmware))
		})

		It("should submit PLDM update when ERoT background copy is completed", func() {
			mockServer := createBF4MockRedfishServer()
			defer mockServer.Stop()
			mockServer.SetErotBackgroundCopyStatus("Completed")

			dpu, pldmPath := preparePldmUpdate(mockServer)
			defer func() { _ = os.Remove(pldmPath) }()

			status, err := updatePldmFwBundle(ctx, dpu, &dutil.ControllerContext{Client: k8sClient}, pldmPath, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.RedfishTaskID).NotTo(BeNil())
			Expect(status.Conditions).To(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondFwBundleSubmitted.String()),
					HaveField("Reason", "Submitting"),
				),
			))
		})

		It("should return error when ERoT chassis cannot be retrieved", func() {
			mockServer := createBF4MockRedfishServer()
			defer mockServer.Stop()
			mockServer.SetErotChassisError(true)

			dpu, pldmPath := preparePldmUpdate(mockServer)
			defer func() { _ = os.Remove(pldmPath) }()

			status, err := updatePldmFwBundle(ctx, dpu, &dutil.ControllerContext{Client: k8sClient}, pldmPath, false)
			Expect(err).To(HaveOccurred())
			Expect(status.RedfishTaskID).To(BeNil())
			Expect(status.Conditions).To(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Reason", "FailedToGetErotChassis"),
				),
			))
		})

		It("should return error when ERoT chassis Nvidia OEM is not found", func() {
			mockServer := createBF4MockRedfishServer()
			defer mockServer.Stop()
			mockServer.SetErotChassisOemPresent(false)

			dpu, pldmPath := preparePldmUpdate(mockServer)
			defer func() { _ = os.Remove(pldmPath) }()

			status, err := updatePldmFwBundle(ctx, dpu, &dutil.ControllerContext{Client: k8sClient}, pldmPath, false)
			Expect(err).To(MatchError("ERoT chassis is not found"))
			Expect(status.RedfishTaskID).To(BeNil())
			Expect(status.Conditions).To(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Reason", "ERoTChassisNotFound"),
				),
			))
		})

		It("should return error when ERoT background copy status is not found", func() {
			mockServer := createBF4MockRedfishServer()
			defer mockServer.Stop()
			mockServer.SetErotBackgroundCopyStatusPresent(false)

			dpu, pldmPath := preparePldmUpdate(mockServer)
			defer func() { _ = os.Remove(pldmPath) }()

			status, err := updatePldmFwBundle(ctx, dpu, &dutil.ControllerContext{Client: k8sClient}, pldmPath, false)
			Expect(err).To(MatchError("ERoT background copy status is not found"))
			Expect(status.RedfishTaskID).To(BeNil())
			Expect(status.Conditions).To(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Reason", "ERoTBackgroundCopyStatusNotFound"),
				),
			))
		})
	})

	Context("checkFirmwareUpdateTimeout", func() {
		It("should return nil when timeout is zero", func() {
			state := &provisioningv1.DPUStatus{}
			Expect(checkFirmwareUpdateTimeout(state, 0)).NotTo(HaveOccurred())
		})

		It("should return nil when FWConfigured condition is missing", func() {
			state := &provisioningv1.DPUStatus{}
			Expect(checkFirmwareUpdateTimeout(state, time.Hour)).NotTo(HaveOccurred())
		})

		It("should return nil when timeout has not been exceeded", func() {
			state := &provisioningv1.DPUStatus{}
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondFWConfigured), nil, "Configured", ""))
			Expect(checkFirmwareUpdateTimeout(state, time.Hour)).NotTo(HaveOccurred())
		})

		It("should return error when timeout has been exceeded", func() {
			state := &provisioningv1.DPUStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUCondFWConfigured),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
						Reason:             "Configured",
					},
				},
			}
			err := checkFirmwareUpdateTimeout(state, time.Hour)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("firmware update timeout exceeded"))
		})
	})
})
