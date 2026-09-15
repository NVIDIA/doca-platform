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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("resolveVFPlan", func() {
	flavorWithVFs := func(vfs ...provisioningv1.VirtualFunction) *provisioningv1.DPUFlavor {
		return &provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{VirtualFunctions: vfs}}
	}

	// vfSummary renders the VF runs as "<netdev>/<first>-<last>[pool]".
	vfSummary := func(plan *vfPlan) []string {
		var out []string
		for _, vf := range plan.vfs {
			out = append(out, vf.netdev+"/"+strconv.Itoa(vf.index)+"-"+strconv.Itoa(vf.index+vf.count-1)+"["+vf.poolName+"]")
		}
		return out
	}

	It("adds up the counts of groups selecting the same device", func() {
		plan, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(4)), Device: ptr.To("*"), PoolName: ptr.To("bf_vf")},
			provisioningv1.VirtualFunction{Count: ptr.To(int32(2)), Device: ptr.To("p1"), PoolName: ptr.To("bf_vf_2")},
		), twoPorts)
		Expect(err).NotTo(HaveOccurred())

		By("each group taking a contiguous run of indices, per device and in list order")
		Expect(vfSummary(plan)).To(Equal([]string{"p0/0-3[bf_vf]", "p1/0-3[bf_vf]", "p1/4-5[bf_vf_2]"}))

		By("the device total being what sriov_numvfs is set to")
		Expect(plan.totals).To(Equal(map[string]int{"0000:03:00.0": 4, "0000:03:00.1": 6}))
	})

	It("numbers groups on one device in declaration order", func() {
		plan, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(2)), Device: ptr.To("p0"), PoolName: ptr.To("second")},
			provisioningv1.VirtualFunction{Count: ptr.To(int32(4)), Device: ptr.To("p0"), PoolName: ptr.To("first")},
		), twoPorts)
		Expect(err).NotTo(HaveOccurred())
		Expect(vfSummary(plan)).To(Equal([]string{"p0/0-1[second]", "p0/2-5[first]"}))
	})

	It("pins a MAC to the index the group owns", func() {
		plan, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(4)), Device: ptr.To("p0")},
			provisioningv1.VirtualFunction{Count: ptr.To(int32(1)), Device: ptr.To("p0"),
				Options: &provisioningv1.VirtualFunctionOptions{MACAddress: ptr.To("02:40:51:7C:E3:0F")}},
		), twoPorts)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.vfs[1].index).To(Equal(4))
		Expect(plan.vfs[1].mac).To(Equal("02:40:51:7c:e3:0f"))
	})

	It("declares a zero total for a count 0 group, without a run", func() {
		plan, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(0)), Device: ptr.To("p0")},
		), twoPorts)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.vfs).To(BeEmpty())
		Expect(plan.totals).To(Equal(map[string]int{"0000:03:00.0": 0}))
	})

	It("leaves a device no group selects out of the totals", func() {
		plan, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(2)), Device: ptr.To("p0")},
		), twoPorts)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.totals).To(HaveKey("0000:03:00.0"))
		Expect(plan.totals).NotTo(HaveKey("0000:03:00.1"))
	})

	It("rejects a group whose device matches no port", func() {
		_, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(2)), Device: ptr.To("p9")},
		), twoPorts)
		Expect(err).To(MatchError(ContainSubstring(`virtualFunctions[0]: device "p9" matches no discovered port`)))
	})

	It("rejects an invalid MAC address", func() {
		_, err := resolveVFPlan(flavorWithVFs(
			provisioningv1.VirtualFunction{Count: ptr.To(int32(1)), Device: ptr.To("p0"),
				Options: &provisioningv1.VirtualFunctionOptions{MACAddress: ptr.To("not-a-mac")}},
		), twoPorts)
		Expect(err).To(MatchError(ContainSubstring("virtualFunctions[0]: options.macAddress")))
	})
})
