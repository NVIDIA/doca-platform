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

package dpuagent

import (
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

func TestParseDMASFObservations(t *testing.T) {
	g := NewWithT(t)

	// A consumable DMA SF: an ibdev, no netdev of its own, on an ibdev-less ECPF.
	out := "aux=mlx5_core.sf.9 parent=0001:03:00.0 rdma=[mlx5_2 ] netdev=[] parentrdma=[]\n"
	observations := parseDMASFObservations(out)
	g.Expect(observations).To(HaveLen(1))
	g.Expect(observations[0].auxDev).To(Equal("mlx5_core.sf.9"))
	g.Expect(observations[0].parentBDF).To(Equal("0001:03:00.0"))
	g.Expect(observations[0].rdmaDevs).To(Equal([]string{"mlx5_2"}))
	g.Expect(observations[0].netdevs).To(BeEmpty())
	g.Expect(observations[0].parentRDMA).To(BeEmpty())

	// A non-consumable one: netdev still present, ECPF exposes an ibdev.
	out = "aux=mlx5_core.sf.4 parent=0000:03:00.0 rdma=[mlx5_3 ] netdev=[en3f1pf0sf8000 ] parentrdma=[mlx5_0 ]\n"
	observations = parseDMASFObservations(out)
	g.Expect(observations).To(HaveLen(1))
	g.Expect(observations[0].netdevs).To(Equal([]string{"en3f1pf0sf8000"}))
	g.Expect(observations[0].parentRDMA).To(Equal([]string{"mlx5_0"}))

	// No matching SF, and unrelated output lines are ignored.
	g.Expect(parseDMASFObservations("")).To(BeEmpty())
	g.Expect(parseDMASFObservations("ls: cannot access ...\n")).To(BeEmpty())
}

func TestDMASFNumFromFlavor(t *testing.T) {
	g := NewWithT(t)

	flavorWith := func(sf []provisioningv1.ScalableFunction) *provisioningv1.DPUFlavor {
		return &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{ScalableFunctions: sf},
		}
	}

	_, enabled := dmaSFNumFromFlavor(nil)
	g.Expect(enabled).To(BeFalse())

	_, enabled = dmaSFNumFromFlavor(flavorWith(nil))
	g.Expect(enabled).To(BeFalse())

	_, enabled = dmaSFNumFromFlavor(flavorWith([]provisioningv1.ScalableFunction{
		{Count: ptr.To(int32(1))},
	}))
	g.Expect(enabled).To(BeFalse(), "an entry without type=dma must not enable the feature")

	sfNum, enabled := dmaSFNumFromFlavor(flavorWith([]provisioningv1.ScalableFunction{
		{Count: ptr.To(int32(1)), Type: provisioningv1.ScalableFunctionTypeDMA},
	}))
	g.Expect(enabled).To(BeTrue())
	g.Expect(sfNum).To(Equal(DefaultDMASFNum), "an unset sfNumStart must default to the SNAP discovery ABI sfnum")

	sfNum, enabled = dmaSFNumFromFlavor(flavorWith([]provisioningv1.ScalableFunction{
		{Count: ptr.To(int32(1)), Type: provisioningv1.ScalableFunctionTypeDMA, SFNumStart: ptr.To(int32(9000))},
	}))
	g.Expect(enabled).To(BeTrue())
	g.Expect(sfNum).To(Equal(9000))
}
