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

package state_test

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Phase Rebooting", func() {
	var (
		ctx                context.Context
		defaultDPUName     = "dpu-rebooting-test"
		defaultDPUNodeName = "dpu-node-rebooting-test"
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("deletion cases", func() {
		It("should transition to DPUDeleting when DPU is being deleted", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPURebooting
			now := metav1.Now()
			dpu.DeletionTimestamp = &now

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
		})
	})

	Context("error handling", func() {
		It("should retry if DPUNode is not found", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = "non-existent-dpu-node"
			dpu.Status.Phase = provisioningv1.DPURebooting
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondRebooted.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUNodeNotFound"),
				),
			))
		})

		It("should return error when trying to reboot before interface initialized", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUMode = provisioningv1.DpuMode
			// No InterfaceInitialized condition set

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondRebooted.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "InvalidState"),
				),
			))
		})
	})

	Context("HostAgent reboot method", func() {
		It("should move to DPUInitializeInterface when Rebooted is True and InterfaceInitialized has HostPowerCycle message", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondInterfaceInitialized),
				Status:  metav1.ConditionTrue,
				Reason:  "",
				Message: string(provisioningv1.DPUCondMessageModeUpdate),
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			// Verify that conditions are removed
			_, rebootedCond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondRebooted.String())
			Expect(rebootedCond).To(BeNil())
			_, interfaceInitCond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(interfaceInitCond).To(BeNil())
		})

		It("should move to HostNetworkConfiguration when Rebooted is True but InterfaceInitialized does not have HostPowerCycle message", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondInterfaceInitialized),
				Status:  metav1.ConditionTrue,
				Reason:  "",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
		})

		It("should stay in Rebooting phase when Rebooted condition is not True", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})
	})

	Context("External reboot method", func() {
		It("should move to DPUInitializeInterface when Rebooted is True, InterfaceInitialized has HostPowerCycle message, not via RedFish", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondInterfaceInitialized),
				Status:  metav1.ConditionTrue,
				Reason:  "",
				Message: string(provisioningv1.DPUCondMessageModeUpdate),
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaHostAgent),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			// Verify that conditions are removed
			_, rebootedCond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondRebooted.String())
			Expect(rebootedCond).To(BeNil())
			_, interfaceInitCond := cutil.GetDPUCondition(&status, provisioningv1.DPUCondInterfaceInitialized.String())
			Expect(interfaceInitCond).To(BeNil())
		})

		It("should move to HostNetworkConfiguration when Rebooted is True but InterfaceInitialized does not have HostPowerCycle message", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaHostAgent),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
		})

		It("should move to DPUClusterConfig when Rebooted is True via RedFish and DPU mode", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUMode = provisioningv1.DpuMode
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
		})

		It("should move to DPUInitializeInterface when Rebooted is True via RedFish and NIC mode", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUMode = provisioningv1.NicMode
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			// Verify that Rebooted condition is removed
			cond := meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondRebooted.String())
			Expect(cond).To(BeNil())
		})
	})

	Context("Script reboot method", func() {
		It("should move to DPUInitializeInterface when Rebooted is True and InterfaceInitialized has HostPowerCycle message", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				Script: &provisioningv1.Script{
					Name: "test-script",
				},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondInterfaceInitialized),
				Status:  metav1.ConditionTrue,
				Reason:  "",
				Message: string(provisioningv1.DPUCondMessageModeUpdate),
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaHostAgent),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		})
	})

	Context("NIC mode cases", func() {
		It("should skip InterfaceInitialized check when in NIC mode", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUMode = provisioningv1.NicMode
			// No InterfaceInitialized condition set, but should not error

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})
	})
})
