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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("buildDevicePluginConfig", func() {
	It("advertises one resource per pool, with an sfnum range per port", func() {
		config := buildDevicePluginConfig(
			&sfPlan{sfs: []plannedSF{
				{netdev: "p0", sfNum: 0, poolName: "bf_sf"},
				{netdev: "p0", sfNum: 1, poolName: "bf_sf"},
				{netdev: "p1", sfNum: 0, poolName: "bf_sf"},
				{netdev: "p0", sfNum: 101, poolName: "bf_sf_trusted"},
				{netdev: "p0", sfNum: 900},
			}},
			&vfPlan{vfs: []plannedVF{
				{netdev: "p0", index: 0, count: 4, poolName: "bf_vf"},
				{netdev: "p1", index: 0, count: 4, poolName: "bf_vf"},
			}},
		)

		Expect(config.ResourceList).To(HaveLen(3))
		Expect(config.ResourceList[0].ResourceName).To(Equal("bf_sf"))
		Expect(config.ResourceList[0].DeviceType).To(Equal("auxNetDevice"))
		Expect(config.ResourceList[0].Selectors[0].PFNames).To(Equal([]string{"p0#0-1", "p1#0-0"}))
		Expect(config.ResourceList[0].Selectors[0].AuxTypes).To(Equal([]string{"sf"}))

		Expect(config.ResourceList[1].ResourceName).To(Equal("bf_sf_trusted"))
		Expect(config.ResourceList[1].Selectors[0].PFNames).To(Equal([]string{"p0#101-101"}))

		Expect(config.ResourceList[2].ResourceName).To(Equal("bf_vf"))
		Expect(config.ResourceList[2].DeviceType).To(Equal("netDevice"))
		Expect(config.ResourceList[2].Selectors[0].PFNames).To(Equal([]string{"p0#0-3", "p1#0-3"}))
		Expect(config.ResourceList[2].Selectors[0].AuxTypes).To(BeEmpty())
	})

	It("advertises only the pools of the plans that were resolved", func() {
		By("an SF plan alone, as after ReconcileSF or with VF configuration skipped")
		config := buildDevicePluginConfig(&sfPlan{sfs: []plannedSF{{netdev: "p0", sfNum: 0, poolName: "bf_sf"}}}, nil)
		Expect(config.ResourceList).To(HaveLen(1))
		Expect(config.ResourceList[0].ResourceName).To(Equal("bf_sf"))

		By("a VF plan alone, as with SF configuration skipped")
		config = buildDevicePluginConfig(nil, &vfPlan{vfs: []plannedVF{{netdev: "p0", index: 0, count: 2, poolName: "bf_vf"}}})
		Expect(config.ResourceList).To(HaveLen(1))
		Expect(config.ResourceList[0].ResourceName).To(Equal("bf_vf"))

		By("neither, which is an empty list rather than a null one")
		config = buildDevicePluginConfig(nil, nil)
		Expect(config.ResourceList).To(BeEmpty())
		Expect(config.ResourceList).NotTo(BeNil())
	})

	It("gives each VF pool on a port its own index range", func() {
		config := buildDevicePluginConfig(nil, &vfPlan{vfs: []plannedVF{
			{netdev: "p0", index: 0, count: 8, poolName: "bf_vf"},
			{netdev: "p0", index: 8, count: 4, poolName: "bf_vf_2"},
		}})

		Expect(config.ResourceList).To(HaveLen(2))
		Expect(config.ResourceList[0].ResourceName).To(Equal("bf_vf"))
		Expect(config.ResourceList[0].Selectors[0].PFNames).To(Equal([]string{"p0#0-7"}))
		Expect(config.ResourceList[1].ResourceName).To(Equal("bf_vf_2"))
		Expect(config.ResourceList[1].Selectors[0].PFNames).To(Equal([]string{"p0#8-11"}))
	})

	It("merges adjacent runs that share a VF pool into one range", func() {
		config := buildDevicePluginConfig(nil, &vfPlan{vfs: []plannedVF{
			{netdev: "p0", index: 0, count: 2, poolName: "bf_vf"},
			{netdev: "p0", index: 2, count: 2, poolName: "bf_vf"},
		}})
		Expect(config.ResourceList[0].Selectors[0].PFNames).To(Equal([]string{"p0#0-3"}))
	})

	It("omits VF runs that name no pool or create nothing", func() {
		config := buildDevicePluginConfig(nil, &vfPlan{vfs: []plannedVF{
			{netdev: "p0", index: 0, count: 4},
			{netdev: "p1", index: 0, count: 0, poolName: "bf_vf"},
		}})
		Expect(config.ResourceList).To(BeEmpty())
	})

	It("omits functions that name no pool", func() {
		config := buildDevicePluginConfig(&sfPlan{sfs: []plannedSF{{netdev: "p0", sfNum: 0}}}, nil)
		Expect(config.ResourceList).To(BeEmpty())
	})

	It("splits a pool's non-contiguous sfnums into separate ranges", func() {
		config := buildDevicePluginConfig(&sfPlan{sfs: []plannedSF{
			{netdev: "p0", sfNum: 0, poolName: "bf_sf"},
			{netdev: "p0", sfNum: 1, poolName: "bf_sf"},
			{netdev: "p0", sfNum: 5, poolName: "bf_sf"},
		}}, nil)
		Expect(config.ResourceList[0].Selectors[0].PFNames).To(Equal([]string{"p0#0-1", "p0#5-5"}))
	})
})
