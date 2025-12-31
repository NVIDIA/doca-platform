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

	It("should set DPU mode to DpuMode if DPU is in NicMode with full transition flow", func() {
		By("prepare mock Redfish server")
		mockServer, err := redfishmock.CreateMockRedfishServer("BF-24.10", "password")
		mockServer.SetNicMode("NicMode")
		Expect(err).NotTo(HaveOccurred())
		defer mockServer.Stop()

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
		// Generate mTLS certificates for testing
		caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())

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

		By("prepare DPUFlavor CR")
		dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
		createObject(dpuFlavor)

		By("prepare DPUDevice CR with NicMode and Redfish BMC")
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Status.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		dpuDevice.Status.DPUMode = provisioningv1.NicMode
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
		patch = client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status = provisioningv1.DPUNodeStatus{
			DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaRedFish)),
			Conditions: []metav1.Condition{
				{
					Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
					Status:             metav1.ConditionTrue,
					Reason:             "BridgeConfigured",
					Message:            "Bridge configured",
					LastTransitionTime: metav1.Now(),
				},
			},
		}
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		By("prepare DPUCluster CR")
		dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
		createObject(dpuCluster)

		By("prepare DPU CR in InitializeInterface phase")
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.DPUFlavor = dpuFlavor.Name
		dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
		dpu.Spec.Cluster.Name = dpuCluster.Name
		dpu.Status.Phase = provisioningv1.DPUInitializeInterface
		dpu.Status.DPUMode = provisioningv1.NicMode
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
		patch = client.MergeFrom(dpuNode.DeepCopy())
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
	})
})
