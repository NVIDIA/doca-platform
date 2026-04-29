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
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
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

	Context("when DPUCondRebooted is not True", func() {
		It("should stay in DPURebooting (HostAgent, Rebooted condition absent)", func() {
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

		It("should stay in DPURebooting (HostAgent, Rebooted condition False)", func() {
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
				Status:  metav1.ConditionFalse,
				Reason:  "WaitingForReboot",
				Message: "host reboot not yet reported",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})

		It("should stay in DPURebooting (External + RedFish, Rebooted condition absent)", func() {
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

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})

		It("should stay in DPURebooting (External + RedFish, Rebooted condition False)", func() {
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
				Status:  metav1.ConditionFalse,
				Reason:  "WaitingForReboot",
				Message: "external reboot not yet confirmed",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})

		It("should stay in DPURebooting (Script + RedFish, Rebooted condition False)", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				Script: &provisioningv1.Script{Name: "reboot-script"},
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
				Status:  metav1.ConditionFalse,
				Reason:  "WaitingForReboot",
				Message: "script reboot not yet confirmed",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})
	})

	Context("HostAgent reboot method", func() {
		It("should move to DPUHostNetworkConfiguration when Rebooted is True with ModeUpdate message but no PreviousPhase", func() {
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
		})

		It("should move to DPUInitializeInterface when Rebooted is True, PreviousPhase is DPUInitializeInterface", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.PreviousPhase = provisioningv1.DPUInitializeInterface
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		})

		It("should move to DPUInitializeInterface when OSInstalled is set and PreviousPhase is DPUInitializeInterface", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.PreviousPhase = provisioningv1.DPUInitializeInterface
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondOSInstalled, "", ""))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
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
	})

	Context("RebootMethodDiscovery condition", func() {
		discoveryCondition := metav1.Condition{
			Type:   cutil.AgentCondRebootMethodDiscovery,
			Status: metav1.ConditionTrue,
			Reason: string(provisioningv1.RebootMethodSystemLevelReset),
		}

		DescribeTable("should move to DPUConfig when discovery is true and PreviousPhase is DPUConfig (same for ZT and HT)",
			func(mutate func(*provisioningv1.DPUNode, *provisioningv1.DPU, *dutil.DPUOptions)) {
				dpuNode := dpuNodeObj(defaultDPUNodeName)
				dpu := dpuObj(defaultDPUName)
				opts := &dutil.DPUOptions{}
				mutate(dpuNode, dpu, opts)
				createObject(dpuNode)

				dpu.Spec.DPUNodeName = dpuNode.Name
				dpu.Status.Phase = provisioningv1.DPURebooting
				dpu.Status.PreviousPhase = provisioningv1.DPUConfig
				dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
					Conditions: []metav1.Condition{discoveryCondition},
				}
				cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
				cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
					Type:    string(provisioningv1.DPUCondRebooted),
					Status:  metav1.ConditionTrue,
					Reason:  "Rebooted",
					Message: "",
				})

				status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
					Client:  k8sClient,
					Options: *opts,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			},
			Entry("HostAgent node reboot", func(dn *provisioningv1.DPUNode, _ *provisioningv1.DPU, _ *dutil.DPUOptions) {
				dn.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
					HostAgent: &provisioningv1.HostAgent{},
				}
			}),
			Entry("External reboot + host-trusted install", func(dn *provisioningv1.DPUNode, dpu *provisioningv1.DPU, o *dutil.DPUOptions) {
				dn.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
					External: &provisioningv1.External{},
				}
				dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
				o.DPUInstallInterface = string(provisioningv1.InstallViaHostAgent)
			}),
			Entry("External reboot + RedFish (zero trust)", func(dn *provisioningv1.DPUNode, dpu *provisioningv1.DPU, o *dutil.DPUOptions) {
				dn.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
					External: &provisioningv1.External{},
				}
				dpu.Status.DPUMode = provisioningv1.DpuMode
				dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
				o.DPUInstallInterface = string(provisioningv1.InstallViaRedFish)
			}),
		)

		It("should still move to DPUInitializeInterface when PreviousPhase matches even if RebootMethodDiscovery is True", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.PreviousPhase = provisioningv1.DPUInitializeInterface
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Conditions: []metav1.Condition{discoveryCondition},
			}
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		})

		It("should use legacy host reboot phase when RebootMethodDiscovery is True but PreviousPhase is not DPUConfig", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				HostAgent: &provisioningv1.HostAgent{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			// PreviousPhase left unset (not DPUConfig) — discovery alone must not route to DPUConfig.
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Conditions: []metav1.Condition{discoveryCondition},
			}
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    string(provisioningv1.DPUCondRebooted),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "",
			})

			status, err := state.Rebooting(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
		})
	})

	Context("External reboot method", func() {
		It("should move to DPUHostNetworkConfiguration when Rebooted is True with ModeUpdate message but no PreviousPhase (HostAgent install)", func() {
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
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

		It("should move to DPUHostNetworkConfiguration when Rebooted is True via RedFish under host-trusted deployment mode", func() {
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
					DeploymentMode:      string(operatorv1.DeploymentModeHostTrusted),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
		})

		It("should move to DPUClusterConfig when Rebooted is True via RedFish and NIC mode without PreviousPhase", func() {
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
		})

		It("should move to DPUInitializeInterface when Rebooted is True via RedFish, DpuMode on status, and PreviousPhase is DPUInitializeInterface", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.PreviousPhase = provisioningv1.DPUInitializeInterface
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			cond := meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondRebooted.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should move to DPUInitializeInterface when OSInstalled is set and PreviousPhase is DPUInitializeInterface (RedFish external)", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			createObject(dpuNode)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.PreviousPhase = provisioningv1.DPUInitializeInterface
			dpu.Status.DPUMode = provisioningv1.DpuMode
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondOSInstalled, "", ""))
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
		})
	})

	Context("Script reboot method", func() {
		It("should move to DPUHostNetworkConfiguration when Rebooted is True with ModeUpdate message but no PreviousPhase", func() {
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
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

	Context("script reboot condition handling", func() {
		createScriptDPUNode := func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				Script: &provisioningv1.Script{Name: "reboot-script"},
			}
			createObject(dpuNode)
		}

		rebootingDPU := func() *provisioningv1.DPU {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = defaultDPUNodeName
			dpu.Status.Phase = provisioningv1.DPURebooting
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
			return dpu
		}

		hostAgentCtx := func() *dutil.ControllerContext {
			return &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaHostAgent),
				},
			}
		}

		It("should stay in DPURebooting when script fails (condition preserved for user)", func() {
			createScriptDPUNode()
			dpu := rebootingDPU()
			cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), fmt.Errorf("exit code 1"), cutil.ReasonRebootScriptFailed, ""))

			status, err := state.Rebooting(ctx, dpu, hostAgentCtx())
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondRebooted))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(cutil.ReasonRebootScriptFailed))
			Expect(cond.Message).To(ContainSubstring("exit code 1"))
		})

		It("should stay in DPURebooting for non-terminal script reasons", func() {
			createScriptDPUNode()
			for _, reason := range []string{cutil.ReasonRebootScriptFailedToFetchJob, cutil.ReasonRebootScriptWaiting} {
				dpu := rebootingDPU()
				cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), fmt.Errorf("err"), reason, "details"))

				status, err := state.Rebooting(ctx, dpu, hostAgentCtx())
				Expect(err).NotTo(HaveOccurred(), "reason=%s", reason)
				Expect(status.Phase).To(Equal(provisioningv1.DPURebooting), "reason=%s", reason)
			}
		})

		It("should stay in DPURebooting when no Rebooted condition exists yet", func() {
			createScriptDPUNode()
			dpu := rebootingDPU()

			status, err := state.Rebooting(ctx, dpu, hostAgentCtx())
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		})
	})
})
