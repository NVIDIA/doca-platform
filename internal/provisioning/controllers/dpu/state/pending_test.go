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

package state_test

import (
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPU: pending", func() {
	var (
		defaultDPUName               = "dpu-pending-test"
		defaultBFBName               = "bfb-pending-test"
		defaultBFBFileName           = "bfb-file.bfb"
		defaultDPUFlavorName         = "dpu-flavor-pending-test"
		defaultBlueFieldSoftwareName = "bluefield-software-pending-test"
	)

	Context("successful cases", func() {
		It("should transition to DPUNodeEffect with Unknown DpuType", func() {
			bfb := bfbObj(defaultBFBName)
			createObject(bfb)
			patch := client.MergeFrom(bfb.DeepCopy())
			bfb.Status.Phase = provisioningv1.BFBReady
			bfb.Status.FileName = defaultBFBFileName
			Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())

			dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
			createObject(dpuFlavor)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.BFB = ptr.To(bfb.Name)
			dpu.Spec.DPUFlavor = dpuFlavor.Name
			dpu.Status.Phase = provisioningv1.DPUPending

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				dpuMap := dutil.NewDPUInProvisioningMap(1)
				status, err := state.Pending(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
						DPUInProvisioningMap: dpuMap,
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
					),
					And(
						HaveField("Type", provisioningv1.DPUCondDPUFlavorExists.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", provisioningv1.DPUCondDPUFlavorExists.String()),
					),
				))
				Expect(status.BFBFile).To(Equal("/bfb/" + defaultBFBFileName))
				Expect(dpuMap.CanProceed(dutil.DPUID("test-dpu"))).To(HaveOccurred())
			})
		})

		It("should transition to DPUNodeEffect with BlueField4 and DPUDevice", func() {
			blueFieldSoftware := blueFieldSoftwareObj(defaultBlueFieldSoftwareName)
			createObject(blueFieldSoftware)
			patch := client.MergeFrom(blueFieldSoftware.DeepCopy())
			blueFieldSoftware.Status.Phase = provisioningv1.BlueFieldSoftwareReady
			Expect(k8sClient.Status().Patch(ctx, blueFieldSoftware, patch)).To(Succeed())

			dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
			createObject(dpuFlavor)

			// Create DPUDevice as mock DMS would do
			dpuDevice := dpuDeviceObj("dpu-device-pending-test")
			dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField4
			createObject(dpuDevice)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.BlueFieldSoftware = ptr.To(blueFieldSoftware.Name)
			dpu.Spec.DPUFlavor = dpuFlavor.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUPending
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				dpuMap := dutil.NewDPUInProvisioningMap(1)
				status, err := state.Pending(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
						DPUInProvisioningMap: dpuMap,
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondBlueFieldSoftwareReady.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", provisioningv1.DPUCondBlueFieldSoftwareReady.String()),
					),
					And(
						HaveField("Type", provisioningv1.DPUCondDPUFlavorExists.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", provisioningv1.DPUCondDPUFlavorExists.String()),
					),
				))
				// BlueField4 uses PLDM-based installation with OS ISO, not BFB files
				// so BFBFile should remain empty
				Expect(status.BFBFile).To(BeEmpty())
				Expect(dpuMap.CanProceed(dutil.DPUID("test-dpu"))).To(HaveOccurred())
			})
		})
	})

	Context("error handling", func() {
		It("should retry if BFB is not found", func() {
			dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
			createObject(dpuFlavor)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.BFB = ptr.To("not-existing-bfb")
			dpu.Spec.DPUFlavor = dpuFlavor.Name
			dpu.Status.Phase = provisioningv1.DPUPending
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Pending(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "BFBNotFound"),
					),
				))
			})
		})
	})

	It("should retry if DPUFlavor is not found", func() {
		bfb := bfbObj(defaultBFBName)
		createObject(bfb)
		patch := client.MergeFrom(bfb.DeepCopy())
		bfb.Status.Phase = provisioningv1.BFBReady
		bfb.Status.FileName = defaultBFBFileName
		Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BFB = ptr.To(bfb.Name)
		dpu.Spec.DPUFlavor = "not-existing-dpu-flavor"
		dpu.Status.Phase = provisioningv1.DPUPending
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
		runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
			status, err := state.Pending(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(installInterface),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondDPUFlavorExists.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUFlavorNotFound"),
				),
			))
		})
	})

	It("should retry if BlueFieldSoftware is not found", func() {
		blueFieldSoftware := blueFieldSoftwareObj(defaultBlueFieldSoftwareName)
		createObject(blueFieldSoftware)

		dpu := dpuObj(defaultDPUName)
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
		dpu.Spec.BlueFieldSoftware = ptr.To(blueFieldSoftware.Name)
		dpu.Spec.BlueFieldSoftware = ptr.To("not-existing-blue-field-software")
		dpu.Status.Phase = provisioningv1.DPUPending
		runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
			status, err := state.Pending(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(installInterface),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondBlueFieldSoftwareReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "BlueFieldSoftwareNotFound"),
				),
			))
		})
	})

	It("should retry if BFB is not ready", func() {
		bfb := bfbObj(defaultBFBName)
		createObject(bfb)

		dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
		createObject(dpuFlavor)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BFB = ptr.To(bfb.Name)
		dpu.Spec.DPUFlavor = dpuFlavor.Name
		dpu.Status.Phase = provisioningv1.DPUPending
		dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
		runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
			status, err := state.Pending(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(installInterface),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "BFBIsNotReady"),
				),
			))
		})
	})

	Context("DELAY_HOST_OS_INIT deployment mode", func() {
		// readyBFBDPU returns a DPU in Pending referencing a ready BFB and the given flavor, so
		// Pending reaches the DELAY_HOST_OS_INIT check.
		readyBFBDPU := func(bfbName string, flavor *provisioningv1.DPUFlavor) *provisioningv1.DPU {
			bfb := bfbObj(bfbName)
			createObject(bfb)
			patch := client.MergeFrom(bfb.DeepCopy())
			bfb.Status.Phase = provisioningv1.BFBReady
			bfb.Status.FileName = defaultBFBFileName
			Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())

			createObject(flavor)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.BFB = ptr.To(bfb.Name)
			dpu.Spec.DPUFlavor = flavor.Name
			dpu.Status.Phase = provisioningv1.DPUPending
			return dpu
		}

		flavorWithHold := func(name string) *provisioningv1.DPUFlavor {
			flavor := dpuFlavorObj(name)
			flavor.Spec.NVConfig = []provisioningv1.NVConfig{
				{Device: ptr.To("p0"), Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}},
			}
			return flavor
		}

		// flavorWithoutHold carries both ways a flavor can mention the feature without asking for
		// the hold: a non-user-mode DELAY_HOST_OS_INIT value, and a service readiness gate, which
		// does not by itself request a host hold.
		flavorWithoutHold := func(name string) *provisioningv1.DPUFlavor {
			flavor := dpuFlavorObj(name)
			flavor.Spec.NVConfig = []provisioningv1.NVConfig{
				{Device: ptr.To("p0"), Parameters: []string{"DELAY_HOST_OS_INIT=0x0"}},
			}
			flavor.Spec.ServiceReadiness = &provisioningv1.ServiceReadiness{
				Gate: provisioningv1.GateOperationalReady,
			}
			return flavor
		}

		runPending := func(dpu *provisioningv1.DPU, deploymentMode string, installInterface provisioningv1.DPUInstallInterfaceType,
			dpuMap *dutil.DPUInProvisioningMap) (provisioningv1.DPUStatus, error) {
			return state.Pending(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DeploymentMode:      deploymentMode,
						DPUInstallInterface: string(installInterface),
					},
					DPUInProvisioningMap: dpuMap,
				},
			)
		}

		It("should fail to Error before provisioning when the flavor holds the host outside zero-trust", func() {
			dpu := readyBFBDPU("bfb-hold-host-trusted", flavorWithHold("dpu-flavor-hold-host-trusted"))
			dpuMap := dutil.NewDPUInProvisioningMap(1)

			status, err := runPending(dpu, string(operatorv1.DeploymentModeHostTrusted), provisioningv1.InstallViaHostAgent, dpuMap)
			// Terminal, so no error either: Error phase keeps it off the retry path.
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondPending.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUFlavorRequiresZeroTrustMode"),
					HaveField("Message", ContainSubstring("DELAY_HOST_OS_INIT requires zero-trust mode")),
				),
			))
			// The rejection must land before the provisioning slot is claimed, otherwise a DPU that
			// can never proceed would occupy capacity for the whole fleet.
			Expect(dpuMap.CanProceed(dutil.DPUID("other-dpu"))).To(Succeed())
		})

		DescribeTable("should proceed when the hold is permitted or absent",
			func(name string, newFlavor func(string) *provisioningv1.DPUFlavor, deploymentMode string, installInterface provisioningv1.DPUInstallInterfaceType) {
				dpu := readyBFBDPU("bfb-"+name, newFlavor("dpu-flavor-"+name))

				status, err := runPending(dpu, deploymentMode, installInterface, dutil.NewDPUInProvisioningMap(1))
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			},
			Entry("hold requested in zero-trust", "hold-zero-trust", flavorWithHold,
				string(operatorv1.DeploymentModeZeroTrust), provisioningv1.InstallViaRedFish),
			Entry("no hold requested outside zero-trust", "no-hold-host-trusted", flavorWithoutHold,
				string(operatorv1.DeploymentModeHostTrusted), provisioningv1.InstallViaHostAgent),
		)
	})

	Context("DPF Operator upgrade", func() {
		configWithVersions := func(deployedVersion, targetVersion string) *operatorv1.DPFOperatorConfig {
			return &operatorv1.DPFOperatorConfig{
				Status: operatorv1.DPFOperatorConfigStatus{
					Version:       ptr.To(deployedVersion),
					TargetVersion: ptr.To(targetVersion),
				},
			}
		}

		runPending := func(dpu *provisioningv1.DPU, config *operatorv1.DPFOperatorConfig,
			dpuMap *dutil.DPUInProvisioningMap) (provisioningv1.DPUStatus, error) {
			return state.Pending(ctx, dpu,
				&dutil.ControllerContext{
					Client:               k8sClient,
					Options:              dutil.DPUOptions{DPUInstallInterface: string(provisioningv1.InstallViaHostAgent)},
					DPUInProvisioningMap: dpuMap,
					DPFOperatorConfig:    config,
				},
			)
		}

		It("should hold the DPU in Pending while the upgrade is in progress", func() {
			dpu := dpuObj("dpu-pending-upgrade-in-progress")
			dpu.Status.Phase = provisioningv1.DPUPending
			dpuMap := dutil.NewDPUInProvisioningMap(1)

			status, err := runPending(dpu, configWithVersions("v25.10.0", release.DPFVersion()), dpuMap)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondPending.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", cutil.ReasonDPFOperatorUpgradeInProgress),
				),
			))
			// The hold must land before the provisioning slot is claimed, otherwise held DPUs would
			// occupy capacity for the whole fleet while the upgrade is running.
			Expect(dpuMap.CanProceed(dutil.DPUID("other-dpu"))).To(Succeed())
		})

		It("should proceed when no upgrade is in progress", func() {
			bfb := bfbObj("bfb-pending-no-upgrade")
			createObject(bfb)
			patch := client.MergeFrom(bfb.DeepCopy())
			bfb.Status.Phase = provisioningv1.BFBReady
			bfb.Status.FileName = defaultBFBFileName
			Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())

			dpuFlavor := dpuFlavorObj("dpu-flavor-pending-no-upgrade")
			createObject(dpuFlavor)

			dpu := dpuObj("dpu-pending-no-upgrade")
			dpu.Spec.BFB = ptr.To(bfb.Name)
			dpu.Spec.DPUFlavor = dpuFlavor.Name
			dpu.Status.Phase = provisioningv1.DPUPending

			status, err := runPending(dpu, configWithVersions(release.DPFVersion(), release.DPFVersion()),
				dutil.NewDPUInProvisioningMap(1))
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
		})
	})
})
