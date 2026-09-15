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

package sriovconfig

import (
	"strconv"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// twoPorts is the discovered N/S port set the plan specs resolve against.
var twoPorts = []pciutil.NICPort{
	{Netdev: "p0", PCIAddress: "0000:03:00.0"},
	{Netdev: "p1", PCIAddress: "0000:03:00.1"},
}

var _ = Describe("resolveSFPlan", func() {
	flavorWithSFs := func(sfs ...provisioningv1.ScalableFunction) *provisioningv1.DPUFlavor {
		return &provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{ScalableFunctions: sfs}}
	}

	// sfSummary renders the plan as "<netdev>/<sfnum>[pool]" so a spec can state the
	// expected numbering in one line.
	sfSummary := func(plan *sfPlan) []string {
		var out []string
		for _, sf := range plan.sfs {
			out = append(out, sf.netdev+"/"+strconv.Itoa(sf.sfNum)+"["+sf.poolName+"]")
		}
		return out
	}

	It("numbers groups sequentially per device, in declaration order", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(2)), PoolName: ptr.To("bf_sf")},
			provisioningv1.ScalableFunction{Count: ptr.To(int32(1)), Device: ptr.To("p1"), PoolName: ptr.To("bf_sf")},
		), twoPorts, true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(sfSummary(plan)).To(Equal([]string{
			"p0/0[bf_sf]", "p0/1[bf_sf]", "p1/0[bf_sf]", "p1/1[bf_sf]", "p1/2[bf_sf]",
		}))
	})

	It("creates count SFs on every selected device, not count in total", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(3)), Device: ptr.To("*")},
		), twoPorts, true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.sfs).To(HaveLen(6))
	})

	It("skips a pinned sfnum when numbering the other groups", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(2))},
			provisioningv1.ScalableFunction{
				Count:   ptr.To(int32(1)),
				Options: &provisioningv1.ScalableFunctionOptions{SFNumStart: ptr.To(int32(1))},
			},
		), twoPorts[:1], true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(sfSummary(plan)).To(Equal([]string{"p0/0[]", "p0/2[]", "p0/1[]"}))
	})

	It("numbers a multi-SF group sequentially from its pinned start", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(3))},
			provisioningv1.ScalableFunction{
				Count:   ptr.To(int32(4)),
				Options: &provisioningv1.ScalableFunctionOptions{SFNumStart: ptr.To(int32(10))},
			},
		), twoPorts[:1], true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(sfSummary(plan)).To(Equal([]string{
			"p0/0[]", "p0/1[]", "p0/2[]",
			"p0/10[]", "p0/11[]", "p0/12[]", "p0/13[]",
		}))
	})

	It("lets the sequential groups fill the numbers a pinned run left free", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{
				Count:   ptr.To(int32(2)),
				Options: &provisioningv1.ScalableFunctionOptions{SFNumStart: ptr.To(int32(1))},
			},
			provisioningv1.ScalableFunction{Count: ptr.To(int32(3))},
		), twoPorts[:1], true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(sfSummary(plan)).To(Equal([]string{"p0/1[]", "p0/2[]", "p0/0[]", "p0/3[]", "p0/4[]"}))
	})

	It("rejects two groups whose pinned runs overlap on one device", func() {
		_, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{
				Count:   ptr.To(int32(4)),
				Options: &provisioningv1.ScalableFunctionOptions{SFNumStart: ptr.To(int32(5))},
			},
			provisioningv1.ScalableFunction{
				Count:   ptr.To(int32(1)),
				Options: &provisioningv1.ScalableFunctionOptions{SFNumStart: ptr.To(int32(7))},
			},
		), twoPorts[:1], true, "")
		Expect(err).To(MatchError(ContainSubstring("sfnum 7 is already taken")))
	})

	It("selects a device by PCI address as well as by netdev", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(1)), Device: ptr.To("0000:03:00.1")},
		), twoPorts, true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.sfs).To(HaveLen(1))
		Expect(plan.sfs[0].netdev).To(Equal("p1"))
	})

	It("fails when a group with a positive count selects no discovered device", func() {
		_, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(1)), Device: ptr.To("p7")},
		), twoPorts, true, "")
		Expect(err).To(MatchError(ContainSubstring("matches no discovered port")))
	})

	It("creates nothing for a count of zero, so a templated group stays valid", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(0)), Device: ptr.To("p7")},
		), twoPorts, true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.sfs).To(BeEmpty())
	})

	It("treats hostDevice as controller 1 and lets options.controller override it", func() {
		plan, err := resolveSFPlan(flavorWithSFs(
			provisioningv1.ScalableFunction{Count: ptr.To(int32(1)), Device: ptr.To("p0"), HostDevice: ptr.To(true)},
			provisioningv1.ScalableFunction{
				Count:      ptr.To(int32(1)),
				Device:     ptr.To("p0"),
				HostDevice: ptr.To(true),
				Options:    &provisioningv1.ScalableFunctionOptions{Controller: ptr.To(int32(2))},
			},
		), twoPorts, true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(*plan.sfs[0].controller).To(Equal(int32(1)))
		Expect(*plan.sfs[1].controller).To(Equal(int32(2)))
	})

	Context("compatibility with flavors that predate the function groups", func() {
		compatFlavor := func(pfTotalSF, trusted string) *provisioningv1.DPUFlavor {
			flavor := &provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
				NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"PF_TOTAL_SF=" + pfTotalSF}}},
			}}
			if trusted != "" {
				flavor.ObjectMeta = metav1.ObjectMeta{Annotations: legacyTrustedSFCountAnnotation(trusted)}
			}
			return flavor
		}

		It("derives workload and trusted groups from PF_TOTAL_SF and the annotation", func() {
			plan, err := resolveSFPlan(compatFlavor("3", "1"), twoPorts, true, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(sfSummary(plan)).To(Equal([]string{
				"p0/0[bf_sf]", "p0/1[bf_sf]", "p1/0[bf_sf]", "p1/1[bf_sf]",
				"p0/101[bf_sf_trusted]", "p1/101[bf_sf_trusted]",
			}))
		})

		It("stays on p0 for pre-BlueField-4 generations", func() {
			plan, err := resolveSFPlan(compatFlavor("1", ""), twoPorts, false, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(sfSummary(plan)).To(Equal([]string{"p0/0[bf_sf]"}))
		})

		It("leaves a slot for the DMA SF on the ECPF that hosts it", func() {
			plan, err := resolveSFPlan(compatFlavor("2", ""), twoPorts, true, "0000:03:00.1")
			Expect(err).NotTo(HaveOccurred())
			Expect(sfSummary(plan)).To(Equal([]string{"p0/0[bf_sf]", "p0/1[bf_sf]", "p1/0[bf_sf]"}))
		})

		It("does not apply once the flavor declares groups of its own", func() {
			flavor := compatFlavor("8", "2")
			flavor.Spec.ScalableFunctions = []provisioningv1.ScalableFunction{
				{Count: ptr.To(int32(1)), Device: ptr.To("p0"), PoolName: ptr.To("bf_sf")},
			}
			plan, err := resolveSFPlan(flavor, twoPorts, true, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(sfSummary(plan)).To(Equal([]string{"p0/0[bf_sf]"}))
		})

		It("does not apply once the flavor declares Virtual Functions", func() {
			flavor := compatFlavor("8", "")
			flavor.Spec.VirtualFunctions = []provisioningv1.VirtualFunction{{Count: ptr.To(int32(2)), Device: ptr.To("p0")}}
			plan, err := resolveSFPlan(flavor, twoPorts, true, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.sfs).To(BeEmpty())
		})
	})
})
