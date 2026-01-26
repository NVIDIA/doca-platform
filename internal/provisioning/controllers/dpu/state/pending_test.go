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
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPU: pending", func() {
	var (
		defaultDPUName               = "dpu-pending-test"
		defaultBFBName               = "bfb-pending-test"
		defaultDPUFlavorName         = "dpu-flavor-pending-test"
		defaultBlueFieldSoftwareName = "bluefield-software-pending-test"
	)

	Context("successful cases", func() {
		It("should transition to DPUNodeEffect", func() {
			bfb := bfbObj(defaultBFBName)
			createObject(bfb)
			patch := client.MergeFrom(bfb.DeepCopy())
			bfb.Status.Phase = provisioningv1.BFBReady
			bfb.Status.FileName = "bfb-file.bfb"
			Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())

			dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
			createObject(dpuFlavor)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.BFB = bfb.Name
			dpu.Spec.DPUFlavor = dpuFlavor.Name
			dpu.Status.Phase = provisioningv1.DPUPending
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3

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
				Expect(dpuMap.CanProceed(dutil.DPUID("test-dpu"))).To(HaveOccurred())
			})
		})
	})

	Context("error handling", func() {
		It("should retry if BFB is not found", func() {
			dpuFlavor := dpuFlavorObj(defaultDPUFlavorName)
			createObject(dpuFlavor)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.BFB = "not-existing-bfb"
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
		bfb.Status.FileName = "bfb-file.bfb"
		Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.BFB = bfb.Name
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
		dpu.Spec.BlueFieldSoftware = blueFieldSoftware.Name
		dpu.Spec.BlueFieldSoftware = "not-existing-blue-field-software"
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
		dpu.Spec.BFB = bfb.Name
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
})
