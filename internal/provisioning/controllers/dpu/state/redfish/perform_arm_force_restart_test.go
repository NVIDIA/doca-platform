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

var _ = Describe("PerformArmForceRestart", func() {
	var (
		mockServer *redfishmock.RedfishMockServer
		dpu        *provisioningv1.DPU
		dpuDevice  *provisioningv1.DPUDevice
		ctrlCtx    *dutil.ControllerContext
	)

	setupMockServerAndSecrets := func() {
		var err error
		mockServer, err = redfishmock.CreateMockRedfishServer("BF-24.10", "password")
		Expect(err).NotTo(HaveOccurred())

		// Create BMC credentials secret
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bmc-shared-password",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{"password": []byte("password")},
		}
		Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

		// Create mTLS certificate secrets
		caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-ca-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{"tls.crt": caCrt},
		}
		Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())

		clientSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-redfish-client-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{"tls.crt": clientCrt, "tls.key": clientKey},
		}
		Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())
	}

	setupDPUDevice := func() {
		dpuDevice = dpuDeviceObj("dpu-device-restart-test")
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)

		// Set device ready with BMC status
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
		dpuDevice.Status.DPUMode = provisioningv1.DpuMode
		dpuDevice.Status.Conditions = []metav1.Condition{
			{
				Type:               string(provisioningv1.ConditionDpuDeviceReady),
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				Message:            "DPUDevice is ready",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
	}

	setupDPU := func() {
		dpu = dpuObj("dpu-restart-test")
		dpu.Namespace = testNS.Name
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUPerformArmForceRestart
		dpu.Status.DPUMode = provisioningv1.DpuMode
	}

	BeforeEach(func() {
		setupMockServerAndSecrets()
		setupDPUDevice()
		setupDPU()
		ctrlCtx = &dutil.ControllerContext{
			Client: k8sClient,
			Options: dutil.DPUOptions{
				DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
			},
		}
	})

	AfterEach(func() {
		if mockServer != nil {
			mockServer.Stop()
		}
	})

	It("should return to InitializeInterface when tracker is missing", func() {
		// No tracker annotation set
		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
	})

	It("should return to InitializeInterface when tracker is stale", func() {
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           1,
			LastRestartTime:   time.Now().Add(-dutil.StaleTrackerTimeout - 5*time.Minute),
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		// Tracker should be cleared
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).To(BeNil())
	})

	It("should abort to InitializeInterface when spec generation changes", func() {
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			InitialGeneration: 99, // Different from dpu.Generation (0)
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
	})

	It("should go to Error phase when safety limit is exceeded", func() {
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           MaxSafetyLimit,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now(),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "error case must set condition False")
		Expect(cond.Reason).To(Equal("MaxSafetyLimitExceeded"))
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).NotTo(BeNil(), "tracker should be preserved on terminal error for forensics and race prevention")
		Expect(loaded.Attempt).To(Equal(MaxSafetyLimit))
	})

	It("should transition to DPUDeleting when DPU is being deleted", func() {
		now := metav1.Now()
		dpu.DeletionTimestamp = &now
		dpu.Finalizers = []string{provisioningv1.DPUFinalizer}
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
	})

	It("should trigger ARM restart, increment tracker, and set initial condition", func() {
		mockServer.SetOemLastState("OsIsRunning")
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           0,
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		// Phase should not change (stays in PerformArmForceRestart, waiting for next reconcile)
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))

		loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
		Expect(loadErr).NotTo(HaveOccurred())
		Expect(loaded.Attempt).To(Equal(1))

		// Verify condition is set (initial InProgress is overwritten by RestartTriggered
		// in the same reconcile since the first restart succeeds immediately). Status
		// must be False until the step is done (common pattern with other phases).
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "in-progress must set condition False")
		Expect(cond.Reason).To(Equal("RestartTriggered"))
	})

	It("should trigger first restart even when OS is not running (unsigned BFB + Secure Boot)", func() {
		mockServer.SetOemLastState("OsStarting") // OS not running
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           0,
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))

		loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
		Expect(loadErr).NotTo(HaveOccurred())
		Expect(loaded.Attempt).To(Equal(1), "restart should be triggered even when OS is down")

		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("RestartTriggered"))
	})

	It("should trigger next restart when OS is running and more restarts needed", func() {
		mockServer.SetOemLastState("OsIsRunning")
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           1,
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded.Attempt).To(Equal(2))
	})

	It("should return to InitializeInterface when all restarts done regardless of OS state", func() {
		mockServer.SetOemLastState("OsStarting") // OS not running
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           2,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now(),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue), "condition True only when step is done")
		// Tracker should NOT be cleared (InitializeInterface needs it for verification)
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).NotTo(BeNil())
	})

	It("should wait for OS to boot when not running after restart", func() {
		mockServer.SetOemLastState("OsStarting") // OS not running yet
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           1,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now(),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		// Should stay in same phase, waiting for boot
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "in-progress must set condition False")
		Expect(cond.Reason).To(Equal("WaitingForBoot"))
	})

	It("should go to Error on OS boot timeout", func() {
		mockServer.SetOemLastState("OsStarting")
		// Set LastRestartTime past OSRunningTimeout (5min) but within StaleTrackerTimeout (15min)
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           1,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now().Add(-OSRunningTimeout - 30*time.Second),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "error case must set condition False")
		Expect(cond.Reason).To(Equal("OSBootTimeout"))
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).NotTo(BeNil(), "tracker should be preserved on terminal error for forensics and race prevention")
		Expect(loaded.Attempt).To(Equal(1))
	})

	It("should skip restart when minimum restart interval has not elapsed", func() {
		mockServer.SetOemLastState("OsIsRunning")
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           0,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now(), // Just now — within MinRestartInterval
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		// Phase stays the same — no restart triggered, waiting for interval
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
		// Initial condition set to InProgress (Status False)
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "in-progress must set condition False")
		Expect(cond.Reason).To(Equal("InProgress"))
		// Attempt should NOT have incremented
		loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
		Expect(loadErr).NotTo(HaveOccurred())
		Expect(loaded.Attempt).To(Equal(0))
	})

	It("should return retryable error when BMC GetSystem returns non-200", func() {
		mockServer.SetSystemError(true)
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           0,
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).To(HaveOccurred())
		// Phase should not change — retryable error
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "error case must set condition False")
		Expect(cond.Reason).To(Equal("FailedToGetSystemState"))
	})

	It("should return retryable error when ForceRestartDPUArm fails", func() {
		mockServer.SetOemLastState("OsIsRunning")
		mockServer.SetResetSystemError(true)
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           0,
			InitialGeneration: dpu.Generation,
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).To(HaveOccurred())
		// Phase should not change — retryable error
		Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse), "error case must set condition False")
		Expect(cond.Reason).To(Equal("FailedToRebootDPUArm"))
		// Attempt should NOT have incremented
		loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
		Expect(loadErr).NotTo(HaveOccurred())
		Expect(loaded.Attempt).To(Equal(0))
	})

	It("should remain in DPUError on re-reconcile when tracker is preserved after safety limit", func() {
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           MaxSafetyLimit,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now(),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		By("First reconcile: should transition to DPUError")
		status1, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status1.Phase).To(Equal(provisioningv1.DPUError))

		By("Simulate race: second reconcile with preserved tracker and stale phase")
		dpu.Status = status1
		dpu.Status.Phase = provisioningv1.DPUPerformArmForceRestart
		status2, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status2.Phase).To(Equal(provisioningv1.DPUError),
			"second reconcile should still produce DPUError, not fall back to InitializeInterface")
		_, cond := cutil.GetDPUCondition(&status2, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("MaxSafetyLimitExceeded"))
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).NotTo(BeNil(), "tracker must remain to prevent race-driven re-staging")
	})

	It("should remain in DPUError on re-reconcile when tracker is preserved after OS boot timeout", func() {
		mockServer.SetOemLastState("OsStarting")
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           1,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now().Add(-OSRunningTimeout - 30*time.Second),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		By("First reconcile: should transition to DPUError")
		status1, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status1.Phase).To(Equal(provisioningv1.DPUError))

		By("Simulate race: second reconcile with preserved tracker and stale phase")
		dpu.Status = status1
		dpu.Status.Phase = provisioningv1.DPUPerformArmForceRestart
		status2, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status2.Phase).To(Equal(provisioningv1.DPUError),
			"second reconcile should still produce DPUError, not trigger more restarts")
		_, cond := cutil.GetDPUCondition(&status2, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("OSBootTimeout"))
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).NotTo(BeNil(), "tracker must remain to prevent race-driven re-staging")
	})

	It("should return to InitializeInterface for validation when all restarts done and OS running", func() {
		mockServer.SetOemLastState("OsIsRunning")
		tracker := &dutil.ArmRestartTracker{
			MaxAttempts:       2,
			Attempt:           2,
			InitialGeneration: dpu.Generation,
			LastRestartTime:   time.Now(),
		}
		Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

		status, err := PerformArmForceRestart(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondArmForceRestarted.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue), "condition True only when step is done")
		// Tracker should NOT be cleared (Init needs it for validation path)
		loaded, _ := dutil.LoadArmRestartTracker(dpu)
		Expect(loaded).NotTo(BeNil())
	})
})
