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

package util

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("ValidatePoolNames", func() {
	It("rejects a pool shared by an SF and a VF group", func() {
		err := ValidatePoolNames(&provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
			ScalableFunctions: []provisioningv1.ScalableFunction{{Count: ptr.To(int32(1)), PoolName: ptr.To("shared")}},
			VirtualFunctions:  []provisioningv1.VirtualFunction{{Count: ptr.To(int32(1)), PoolName: ptr.To("shared")}},
		}})
		Expect(err).To(MatchError(ContainSubstring("holds one device type")))
	})

	It("allows the same pool name across groups of one kind", func() {
		err := ValidatePoolNames(&provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
			ScalableFunctions: []provisioningv1.ScalableFunction{
				{Count: ptr.To(int32(1)), PoolName: ptr.To("bf_sf")},
				{Count: ptr.To(int32(2)), PoolName: ptr.To("bf_sf")},
			},
			VirtualFunctions: []provisioningv1.VirtualFunction{{Count: ptr.To(int32(1)), PoolName: ptr.To("bf_vf")}},
		}})
		Expect(err).NotTo(HaveOccurred())
	})

	It("ignores groups created in hardware only", func() {
		err := ValidatePoolNames(&provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
			ScalableFunctions: []provisioningv1.ScalableFunction{{Count: ptr.To(int32(1))}},
			VirtualFunctions:  []provisioningv1.VirtualFunction{{Count: ptr.To(int32(1))}},
		}})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("ValidateScalableFunctions", func() {
	// pinned is a group of count SFs on device starting at sfNumStart.
	pinned := func(device string, sfNumStart, count int32) provisioningv1.ScalableFunction {
		return provisioningv1.ScalableFunction{
			Count:   ptr.To(count),
			Device:  ptr.To(device),
			Options: &provisioningv1.ScalableFunctionOptions{SFNumStart: ptr.To(sfNumStart)},
		}
	}

	validate := func(groups ...provisioningv1.ScalableFunction) error {
		return ValidateScalableFunctions(&provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{ScalableFunctions: groups},
		})
	}

	It("rejects overlapping runs pinned on the same device", func() {
		Expect(validate(pinned("p0", 10, 4), pinned("p0", 12, 2))).To(
			MatchError(ContainSubstring(`scalableFunctions[1] pins sfnums 12-13 on device "p0", overlapping the 10-13 pinned by scalableFunctions[0]`)))
	})

	It("rejects the same sfNumStart declared twice", func() {
		Expect(validate(pinned("*", 101, 1), pinned("*", 101, 1))).To(
			MatchError(ContainSubstring("overlapping")))
	})

	It("accepts runs that touch but do not overlap", func() {
		Expect(validate(pinned("p0", 0, 4), pinned("p0", 4, 4))).To(Succeed())
	})

	It("compares selectors regardless of case and of an unset device", func() {
		By("folding case, so P0 and p0 are one selector")
		Expect(validate(pinned("P0", 5, 2), pinned("p0", 6, 2))).To(MatchError(ContainSubstring("overlapping")))

		By("treating an unset device as the * it resolves to")
		unset := pinned("*", 5, 2)
		unset.Device = nil
		Expect(validate(unset, pinned("*", 5, 2))).To(MatchError(ContainSubstring("overlapping")))
	})

	It("leaves overlaps across different selectors to the agent, which knows the ports", func() {
		Expect(validate(pinned("p0", 10, 2), pinned("p1", 10, 2))).To(Succeed())
		Expect(validate(pinned("p0", 10, 2), pinned("*", 10, 2))).To(Succeed())
		Expect(validate(pinned("p0", 10, 2), pinned("0000:03:00.0", 10, 2))).To(Succeed())
	})

	It("ignores groups the agent numbers itself", func() {
		By("no options at all")
		Expect(validate(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(4)), Device: ptr.To("p0")},
			provisioningv1.ScalableFunction{Count: ptr.To(int32(4)), Device: ptr.To("p0")},
		)).To(Succeed())

		By("options without sfNumStart")
		Expect(validate(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(4)), Device: ptr.To("p0"),
				Options: &provisioningv1.ScalableFunctionOptions{Trusted: ptr.To(true)}},
			pinned("p0", 0, 4),
		)).To(Succeed())
	})

	It("ignores a pinned group that creates nothing", func() {
		Expect(validate(pinned("p0", 10, 0), pinned("p0", 10, 2))).To(Succeed())
	})

	It("accepts an empty list", func() {
		Expect(ValidateScalableFunctions(&provisioningv1.DPUFlavor{})).To(Succeed())
	})
})
