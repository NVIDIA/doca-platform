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
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
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
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

		By("Step 3: Run Rebooting phase handler")
		status, err = state.Rebooting(ctx, dpu,
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
		status, err = state.Rebooting(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			},
		)
		Expect(err).To(Succeed())
		Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface), "DPU should transition back to InitializeInterface phase after reboot with NicMode in DPUDevice")
		_, rebootedCondition := cutil.GetDPUCondition(&status, provisioningv1.DPUCondRebooted.String())
		Expect(rebootedCondition).To(BeNil(), "DPUCondRebooted condition should be removed when transitioning back to InitializeInterface")

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

})
