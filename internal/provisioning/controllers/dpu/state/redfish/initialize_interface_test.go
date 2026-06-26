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

var _ = Describe("InitializeInterface", func() {

	var (
		defaultDPUName        = "dpu-initialize-interface-test"
		defaultDPUNodeName    = "dpu-node-initialize-interface-test"
		defaultDPUDeviceName  = "dpu-device-initialize-interface-test"
		defaultDPUClusterName = "dpu-cluster-initialize-interface-test"
		defaultDPUFlavorName  = "dpu-flavor-initialize-interface-test"
		strTrue               = "true"
	)

	// Helper function to create mock Redfish server
	createMockRedfishServer := func() (*redfishmock.RedfishMockServer, error) {
		return redfishmock.CreateMockRedfishServer("BF-24.10", "password")
	}

	// Helper function to prepare DPUFlavor CR
	prepareDPUFlavor := func() {
		By("prepare DPUFlavor CR")
		dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
		createObject(dpuFlavor)
	}

	// Helper function to prepare DPUCluster CR
	prepareDPUCluster := func() {
		By("Prepare DPUCluster CR")
		dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
		createObject(dpuCluster)
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

	// Helper function to set DPUDevice status as ready
	setDPUDeviceReady := func(dpuDevice *provisioningv1.DPUDevice, dpuMode provisioningv1.DpuModeType) {
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
		dpuDevice.Status.DPUMode = dpuMode
		// Preserve DPUType if it was already set (e.g., to Unknown for testing)
		// Otherwise it will remain as it was initialized
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

	// Helper function to set DPUNode status with bridge configured
	setDPUNodeReady := func(dpuNode *provisioningv1.DPUNode) {
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status = provisioningv1.DPUNodeStatus{
			DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaRedFish)),
			Conditions: []metav1.Condition{
				{
					Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
					Status:             metav1.ConditionTrue,
					Reason:             "BridgeConfigured",
					Message:            "OOBBridgeConfigured",
					LastTransitionTime: metav1.Now(),
				},
			},
		}
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())
	}

	It("should set DPU mode to DpuMode if DPU is in NicMode with full transition flow", func() {
		By("prepare mock Redfish server")
		mockServer, err := createMockRedfishServer()
		mockServer.SetNicMode("NicMode")
		Expect(err).NotTo(HaveOccurred())
		defer mockServer.Stop()

		createBMCAndMTLSSecrets(mockServer.GetIPAddress())

		prepareDPUFlavor()

		By("prepare DPUDevice CR with NicMode and Redfish BMC")
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		setDPUDeviceReady(dpuDevice, provisioningv1.NicMode)

		By("prepare DPUNode CR with External reboot method")
		dpuNode := dpuNodeObj(defaultDPUNodeName)
		dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
		dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
		dpuNode.Spec.DPUs = []provisioningv1.DPURef{
			{
				Name: dpuDevice.Name,
			},
		}
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
			External: &provisioningv1.External{},
		}
		dpuNode.Annotations = map[string]string{
			provisioningv1.DPUNodeExternalRebootRequiredAnnotation: "true",
		}
		createObject(dpuNode)
		setDPUNodeReady(dpuNode)

		prepareDPUCluster()

		By("prepare DPU CR in InitializeInterface phase")
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.DPUFlavor = defaultDPUFlavorName
		dpu.Spec.Cluster.Namespace = defaultDPUClusterName
		dpu.Spec.Cluster.Name = defaultDPUClusterName
		dpu.Status.Phase = provisioningv1.DPUInitializeInterface
		dpu.Status.DPUMode = provisioningv1.NicMode
		dpu.Status.DPUType = provisioningv1.DPUTypeUnknown
		dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))

		By("Step 1: DPU in InitializeInterface phase detects NicMode and transitions to Rebooting")
		status, err := InitializeInterface(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(Succeed())
		Expect(status.Phase).To(Equal(provisioningv1.DPURebooting), "DPU should transition to Rebooting phase when in NicMode")

		By("Step 2: Update DPU status to Rebooting and set InterfaceInitialized condition")
		dpu.Status = status
		// Mirror dpu_controller.UpdateDPUStatus: when phase changes, PreviousPhase records the prior phase.
		dpu.Status.PreviousPhase = provisioningv1.DPUInitializeInterface
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

		By("Step 3: Run Rebooting phase handler")
		status, err = Rebooting(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(Succeed())
		Expect(status.Phase).To(Equal(provisioningv1.DPURebooting), "DPU should remain in Rebooting phase until reboot annotation is removed")

		By("Step 4: Simulate host reboot by removing reboot-required annotation and setting Rebooted condition")
		patch := client.MergeFrom(dpuNode.DeepCopy())
		delete(dpuNode.Annotations, provisioningv1.DPUNodeExternalRebootRequiredAnnotation)
		Expect(k8sClient.Patch(ctx, dpuNode, patch)).To(Succeed())

		// Update DPU status with Rebooted condition
		dpu.Status = status
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))

		By("Step 5: Run Rebooting phase again after reboot annotation removed - should transition to InitializeInterface")
		status, err = Rebooting(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(Succeed())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface), "DPU should transition back to InitializeInterface phase after reboot with NicMode in DPUDevice")

		mockServer.SetNicMode("DpuMode")

		By("Step 6: Run InitializeInterface phase again with DpuMode set")
		dpu.Status = status
		status, err = InitializeInterface(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(Succeed())
		Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters), "DPU should transition to ConfigFWParameters phase when in DpuMode")
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", string(provisioningv1.DPUCondInterfaceInitialized)),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", string(provisioningv1.DPUCondInterfaceInitialized)),
			),
		))
		Expect(status.DPUMode).To(Equal(provisioningv1.DpuMode), "DPU mode should be DpuMode")
		Expect(status.DPUType).To(Equal(provisioningv1.DPUTypeBlueField3), "DPU type should be BlueField3")
	})

	It("should fail when DpuDevice DPUtype is Unknown", func() {
		By("prepare mock Redfish server with DpuMode")
		mockServer, err := createMockRedfishServer()
		mockServer.SetNicMode("DpuMode")
		mockServer.SetModel("Unknown DPU Model") // Set unknown model to trigger Unknown DPU type
		Expect(err).NotTo(HaveOccurred())
		defer mockServer.Stop()

		createBMCAndMTLSSecrets(mockServer.GetIPAddress())

		By("prepare DPUDevice CR with Unknown DPU type")
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		dpuDevice.Status.DPUType = provisioningv1.DPUTypeUnknown
		createObject(dpuDevice)
		setDPUDeviceReady(dpuDevice, provisioningv1.NicMode)

		By("prepare DPUNode CR")
		dpuNode := dpuNodeObj(defaultDPUNodeName)
		createObject(dpuNode)
		setDPUNodeReady(dpuNode)

		prepareDPUCluster()

		prepareDPUFlavor()

		By("prepare DPU CR in InitializeInterface phase")
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.DPUFlavor = defaultDPUFlavorName
		dpu.Spec.Cluster.Namespace = defaultDPUClusterName
		dpu.Spec.Cluster.Name = defaultDPUClusterName
		dpu.Status.Phase = provisioningv1.DPUInitializeInterface
		dpu.Status.DPUMode = provisioningv1.NicMode
		dpu.Status.DPUType = provisioningv1.DPUTypeUnknown
		dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))

		By("Run InitializeInterface phase")
		status, err := InitializeInterface(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface), "DPU should remain in InitializeInterface phase when DPU type is Unknown")
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", string(provisioningv1.DPUCondInterfaceInitialized)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "FailedToGetDPUType"),
			),
		))
		Expect(status.DPUMode).To(Equal(provisioningv1.DpuMode), "DPU mode should be set to DpuMode")
		Expect(status.DPUType).To(Equal(provisioningv1.DPUTypeUnknown), "DPU type should be Unknown")
	})

	It("should proceed with provisioning when DPU is already in DpuMode", func() {
		By("prepare mock Redfish server with DpuMode")
		mockServer, err := createMockRedfishServer()
		mockServer.SetNicMode("DpuMode")
		Expect(err).NotTo(HaveOccurred())
		defer mockServer.Stop()

		createBMCAndMTLSSecrets(mockServer.GetIPAddress())

		prepareDPUFlavor()

		By("prepare DPUDevice CR already in DpuMode")
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		setDPUDeviceReady(dpuDevice, provisioningv1.DpuMode)

		By("prepare DPUNode CR")
		dpuNode := dpuNodeObj(defaultDPUNodeName)
		dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
		dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
		dpuNode.Spec.DPUs = []provisioningv1.DPURef{
			{
				Name: dpuDevice.Name,
			},
		}
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
			External: &provisioningv1.External{},
		}
		createObject(dpuNode)
		setDPUNodeReady(dpuNode)

		prepareDPUCluster()

		By("prepare DPU CR in InitializeInterface phase already in DpuMode")
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.DPUFlavor = defaultDPUFlavorName
		dpu.Spec.Cluster.Namespace = defaultDPUClusterName
		dpu.Spec.Cluster.Name = defaultDPUClusterName
		dpu.Status.Phase = provisioningv1.DPUInitializeInterface
		dpu.Status.DPUMode = provisioningv1.DpuMode
		dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))

		By("Run InitializeInterface phase when DPU is already in DpuMode")
		status, err := InitializeInterface(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(Succeed())
		Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters), "DPU should proceed to ConfigFWParameters phase when already in DpuMode")
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", string(provisioningv1.DPUCondInterfaceInitialized)),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", string(provisioningv1.DPUCondInterfaceInitialized)),
			),
		))
		Expect(status.DPUMode).To(Equal(provisioningv1.DpuMode), "DPU mode should remain DpuMode")
	})

	Context("Secure Boot configuration", func() {
		var (
			mockServer *redfishmock.RedfishMockServer
			dpu        *provisioningv1.DPU
			dpuDevice  *provisioningv1.DPUDevice
			ctrlCtx    *dutil.ControllerContext
		)

		BeforeEach(func() {
			var err error
			mockServer, err = createMockRedfishServer()
			Expect(err).NotTo(HaveOccurred())
			mockServer.SetNicMode("DpuMode")

			createBMCAndMTLSSecrets(mockServer.GetIPAddress())
			prepareDPUFlavor()

			dpuDevice = dpuDeviceObj(defaultDPUDeviceName)
			dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			createObject(dpuDevice)
			setDPUDeviceReady(dpuDevice, provisioningv1.DpuMode)

			dpu = dpuObj(defaultDPUName)
			dpu.Namespace = testNS.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.DPUFlavor = defaultDPUFlavorName
			dpu.Status.Phase = provisioningv1.DPUInitializeInterface
			dpu.Status.DPUMode = provisioningv1.DpuMode
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))

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

		It("should stage enable and transition to PerformArmForceRestart on mismatch", func() {
			mockServer.SetSecureBootCurrentBoot(false)
			mockServer.SetSecureBootEnable(false)
			dpu.Spec.SecureBoot = ptr.To(true)

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
			Expect(mockServer.GetSecureBootEnable()).To(BeTrue())
			loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
			Expect(loadErr).NotTo(HaveOccurred())
			Expect(loaded).NotTo(BeNil())
			Expect(loaded.MaxAttempts).To(Equal(2))
		})

		It("should stage disable and transition to PerformArmForceRestart on mismatch", func() {
			mockServer.SetSecureBootCurrentBoot(true)
			mockServer.SetSecureBootEnable(true)
			dpu.Spec.SecureBoot = ptr.To(false)

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart))
			Expect(mockServer.GetSecureBootEnable()).To(BeFalse())
		})

		It("should proceed when Secure Boot already matches desired state", func() {
			mockServer.SetSecureBootCurrentBoot(true)
			mockServer.SetSecureBootEnable(true)
			dpu.Spec.SecureBoot = ptr.To(true)

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(status.SecureBoot).NotTo(BeNil())
			Expect(*status.SecureBoot.Enabled).To(BeTrue())
		})

		It("should proceed when spec.SecureBoot is nil", func() {
			dpu.Spec.SecureBoot = nil

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
		})

		It("should re-assert PerformArmForceRestart when tracker in progress but phase is still Initialize Interface", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           0,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPerformArmForceRestart),
				"should re-assert phase so next reconcile runs PerformArmForceRestart handler")
		})

		It("should proceed and clear tracker after successful post-restart verification", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(true)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(*status.SecureBoot.Enabled).To(BeTrue())
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).To(BeNil())
		})

		It("should clear orphaned tracker and proceed when Spec.SecureBoot removed mid-flow", func() {
			dpu.Spec.SecureBoot = nil // Removed after restarts completed
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters),
				"should continue normal flow when SecureBoot spec was removed")
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).To(BeNil(), "orphaned tracker should be cleaned up")
		})

		It("should retry when BMC GetSecureBoot fails during initial detection", func() {
			mockServer.SetSecureBootError(true)
			dpu.Spec.SecureBoot = ptr.To(true)

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailedToGetSecureBootStatus"))
		})

		It("should retry when BMC Secure Boot staging fails", func() {
			mockServer.SetSecureBootCurrentBoot(true)
			mockServer.SetSecureBootEnable(true)
			mockServer.SetSecureBootPatchError(true)
			dpu.Spec.SecureBoot = ptr.To(false)

			_, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to stage Secure Boot"))
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).To(BeNil())
		})

		It("should preserve tracker when BMC GetSecureBoot fails during post-restart verification", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Simulate BMC failure during post-restart verification")
			mockServer.SetSecureBootError(true)

			_, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred())

			By("Verify tracker is preserved so next reconcile retries verification")
			loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
			Expect(loadErr).NotTo(HaveOccurred())
			Expect(loaded).NotTo(BeNil(), "tracker must be preserved on transient BMC failure")
			Expect(loaded.MaxAttempts).To(Equal(2))
			Expect(loaded.Attempt).To(Equal(2))

			By("Simulate BMC recovery on next reconcile")
			mockServer.SetSecureBootError(false)
			mockServer.SetSecureBootCurrentBoot(true)

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters),
				"should proceed after successful retry")
			Expect(*status.SecureBoot.Enabled).To(BeTrue())

			By("Verify tracker is cleared after successful verification")
			loaded, _ = dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).To(BeNil(), "tracker should be cleared after successful verification")
		})

		It("should return error when tracker annotation is corrupted", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			if dpu.Annotations == nil {
				dpu.Annotations = make(map[string]string)
			}
			dpu.Annotations[string(provisioningv1.AnnotationArmRestartTracker)] = "not-valid-json"

			_, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred())
		})

		It("should retry verification when SecureBootCurrentBoot mismatches within timeout", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(false)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Set ArmForceRestarted condition with recent LastTransitionTime (within timeout)")
			dpu.Status.Conditions = append(dpu.Status.Conditions, metav1.Condition{
				Type:               provisioningv1.DPUCondArmForceRestarted.String(),
				Status:             metav1.ConditionTrue,
				Reason:             provisioningv1.DPUCondArmForceRestarted.String(),
				LastTransitionTime: metav1.Now(),
			})

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface),
				"should stay in InitializeInterface while retrying verification")

			By("Verify tracker is preserved for next reconcile")
			loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
			Expect(loadErr).NotTo(HaveOccurred())
			Expect(loaded).NotTo(BeNil(), "tracker must be preserved during retry window")
			Expect(loaded.Attempt).To(Equal(2))
		})

		It("should succeed when SecureBootCurrentBoot matches after retry window", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(true)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Set ArmForceRestarted condition with recent LastTransitionTime")
			dpu.Status.Conditions = append(dpu.Status.Conditions, metav1.Condition{
				Type:               provisioningv1.DPUCondArmForceRestarted.String(),
				Status:             metav1.ConditionTrue,
				Reason:             provisioningv1.DPUCondArmForceRestarted.String(),
				LastTransitionTime: metav1.Now(),
			})

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters),
				"should proceed when BMC reports correct value")
			Expect(*status.SecureBoot.Enabled).To(BeTrue())

			By("Verify tracker is cleared after successful verification")
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).To(BeNil())
		})

		It("should requeue when ArmForceRestarted condition is absent during mismatch", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(false) // mismatch
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Do NOT set ArmForceRestarted condition - simulating cache staleness")

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface),
				"should requeue (not DPUError) when ArmForceRestarted condition is absent")

			By("Verify tracker is preserved")
			loaded, loadErr := dutil.LoadArmRestartTracker(dpu)
			Expect(loadErr).NotTo(HaveOccurred())
			Expect(loaded).NotTo(BeNil(), "tracker must be preserved when requeueing")
			Expect(loaded.Attempt).To(Equal(2))
		})

		It("should go to DPUError when ArmForceRestarted condition is absent and tracker is stale", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(false)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				LastRestartTime:   time.Now().Add(-dutil.StaleTrackerTimeout - time.Second),
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Do NOT set ArmForceRestarted condition - condition permanently absent")

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError),
				"should fall through to DPUError when tracker is stale and condition is absent")
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("SecureBootConfigurationFailed"))
			Expect(status.SecureBoot).NotTo(BeNil())
			Expect(*status.SecureBoot.Enabled).To(BeFalse(),
				"status should reflect actual BMC value, not desired")
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).NotTo(BeNil(), "tracker should be preserved on terminal error for forensics and race prevention")
			Expect(loaded.MaxAttempts).To(Equal(2))
			Expect(loaded.Attempt).To(Equal(2))
		})

		It("should go to DPUError when mismatch persists past verification timeout", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(false)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Set ArmForceRestarted condition with expired LastTransitionTime")
			dpu.Status.Conditions = append(dpu.Status.Conditions, metav1.Condition{
				Type:               provisioningv1.DPUCondArmForceRestarted.String(),
				Status:             metav1.ConditionTrue,
				Reason:             provisioningv1.DPUCondArmForceRestarted.String(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-secureBootVerificationTimeout - time.Second)),
			})

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError),
				"should transition to DPUError when mismatch persists past timeout")
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("SecureBootConfigurationFailed"))
			Expect(status.SecureBoot).NotTo(BeNil())
			Expect(*status.SecureBoot.Enabled).To(BeFalse(),
				"status should reflect actual BMC value, not desired")
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).NotTo(BeNil(), "tracker should be preserved on terminal error for forensics and race prevention")
			Expect(loaded.MaxAttempts).To(Equal(2))
			Expect(loaded.Attempt).To(Equal(2))
		})

		It("should remain in DPUError on re-reconcile when tracker is preserved after timeout", func() {
			dpu.Spec.SecureBoot = ptr.To(true)
			mockServer.SetSecureBootCurrentBoot(false)
			tracker := &dutil.ArmRestartTracker{
				MaxAttempts:       2,
				Attempt:           2,
				InitialGeneration: dpu.Generation,
			}
			Expect(dutil.SaveArmRestartTracker(dpu, tracker)).To(Succeed())

			By("Set ArmForceRestarted condition with expired LastTransitionTime")
			dpu.Status.Conditions = append(dpu.Status.Conditions, metav1.Condition{
				Type:               provisioningv1.DPUCondArmForceRestarted.String(),
				Status:             metav1.ConditionTrue,
				Reason:             provisioningv1.DPUCondArmForceRestarted.String(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-secureBootVerificationTimeout - time.Second)),
			})

			By("First reconcile: should transition to DPUError")
			status1, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status1.Phase).To(Equal(provisioningv1.DPUError))
			_, cond1 := cutil.GetDPUCondition(&status1, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond1).NotTo(BeNil())
			Expect(cond1.Reason).To(Equal("SecureBootConfigurationFailed"))

			By("Simulate race: second reconcile with preserved tracker and phase still InitializeInterface")
			dpu.Status = status1
			dpu.Status.Phase = provisioningv1.DPUInitializeInterface
			status2, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status2.Phase).To(Equal(provisioningv1.DPUError),
				"second reconcile should still produce DPUError, not re-stage Secure Boot")
			_, cond2 := cutil.GetDPUCondition(&status2, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond2).NotTo(BeNil())
			Expect(cond2.Reason).To(Equal("SecureBootConfigurationFailed"),
				"should not have re-staged (SecureBootConfigurationStaged)")
			loaded, _ := dutil.LoadArmRestartTracker(dpu)
			Expect(loaded).NotTo(BeNil(), "tracker must remain to prevent race-driven re-staging")
		})
	})

	Context("BMC connectivity transient failures", func() {
		var (
			mockServer *redfishmock.RedfishMockServer
			dpu        *provisioningv1.DPU
			dpuDevice  *provisioningv1.DPUDevice
			ctrlCtx    *dutil.ControllerContext
		)

		BeforeEach(func() {
			var err error
			mockServer, err = createMockRedfishServer()
			Expect(err).NotTo(HaveOccurred())
			mockServer.SetNicMode("DpuMode")

			createBMCAndMTLSSecrets(mockServer.GetIPAddress())
			prepareDPUFlavor()

			dpuDevice = dpuDeviceObj(defaultDPUDeviceName)
			dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			createObject(dpuDevice)
			setDPUDeviceReady(dpuDevice, provisioningv1.DpuMode)

			dpu = dpuObj(defaultDPUName)
			dpu.Namespace = testNS.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.DPUFlavor = defaultDPUFlavorName
			dpu.Status.Phase = provisioningv1.DPUInitializeInterface
			dpu.Status.DPUMode = provisioningv1.DpuMode
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))

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

		It("should set FailedToCreateClient when TLS client cannot be created", func() {
			By("Point DPUDevice BMC at an unreachable IP")
			unreachable := "192.0.2.1"
			patch := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.BMCIP = ptr.To(unreachable)
			Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred(), "should return error to trigger controller-runtime requeue")
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface),
				"DPU should stay in InitializeInterface, not transition to DPUError")
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("FailedToCreateClient"))
		})

		It("should stay in InitializeInterface when BMC is unreachable and recover when connectivity is restored", func() {
			By("Simulate BMC unreachable via ProductDescription endpoint error")
			mockServer.SetProductDescriptionError(true)

			By("Step 1: Call InitializeInterface — GetProductDescription fails due to BMC unreachable")
			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred(), "should return error to trigger controller-runtime requeue")
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface),
				"DPU should stay in InitializeInterface, not transition to DPUError")
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("FailedToGetProductDescription"))

			By("Step 2: Restore BMC connectivity")
			mockServer.SetProductDescriptionError(false)

			By("Step 3: Call InitializeInterface again — should proceed normally")
			status, err = InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters),
				"DPU should proceed to ConfigFWParameters after BMC recovers")
		})

		It("should stay in InitializeInterface when GetChassis fails transiently and recover", func() {
			By("Set DPUType to Unknown so GetChassis is called during InitializeInterface")
			dpu.Status.DPUType = provisioningv1.DPUTypeUnknown

			By("Step 1: Simulate GetChassis endpoint error")
			mockServer.SetChassisError(true)

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred(), "should return error to trigger controller-runtime requeue")
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface),
				"DPU should stay in InitializeInterface, not transition to DPUError")
			_, cond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailedToGetChassisInfo"),
				"condition should indicate the specific Redfish call that failed")

			By("Step 2: Restore GetChassis endpoint")
			mockServer.SetChassisError(false)

			By("Step 3: Call InitializeInterface again — should proceed normally")
			status, err = InitializeInterface(ctx, dpu, ctrlCtx)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters),
				"DPU should proceed to ConfigFWParameters after BMC recovers")
			Expect(status.DPUType).To(Equal(provisioningv1.DPUTypeBlueField3),
				"DPU type should be resolved after recovery")
		})
	})

	Context("BF4 product description", func() {

		It("should accept BF4 Oem/Nvidia response without Description or Mode", func() {
			server := redfishmock.NewRedfishMockServer("BF-26.04", "password")
			server.SetDpuVersion(redfishmock.BF4)
			server.Start()
			DeferCleanup(server.Stop)

			client, err := server.GetClient()
			Expect(err).NotTo(HaveOccurred())

			desc, err := getProductDescription(client)
			Expect(err).NotTo(HaveOccurred(), "BF4 Oem/Nvidia response without Description/Mode must not fail")
			Expect(desc).NotTo(BeNil())
			Expect(desc.Description).To(BeNil())
			Expect(desc.Mode).To(BeNil())
		})
	})
})
