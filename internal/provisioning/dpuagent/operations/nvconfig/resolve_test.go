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

package nvconfig

import (
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("EnsureResolved", func() {
	It("caches successful resolve and does not rediscover ports", func() {
		calls := 0
		optCtx := &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{{Device: ptr.To("p0"), Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}}},
				},
			},
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				calls++
				return []pciutil.NICPort{
					{Netdev: "p0", PCIAddress: testPci0},
					{Netdev: "p1", PCIAddress: testPci1},
				}, nil
			},
		}
		first, err := EnsureResolved(optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.HostOSInitRequired).To(BeTrue())
		Expect(first.HostOSInitPCIs).To(Equal([]string{testPci0}))
		Expect(first.PCIToParams[testPci0]).To(ContainSubstring("DELAY_HOST_OS_INIT=0x3"))

		second, err := EnsureResolved(optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(BeIdenticalTo(first))
		Expect(calls).To(Equal(1))
	})

	It("does not cache discovery errors and retries", func() {
		calls := 0
		optCtx := &operations.Context{
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				calls++
				if calls == 1 {
					return nil, fmt.Errorf("discovery failed")
				}
				return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPci0}}, nil
			},
		}
		_, err := EnsureResolved(optCtx)
		Expect(err).To(HaveOccurred())
		Expect(optCtx.GetResolvedNVConfig()).To(BeNil())

		resolved, err := EnsureResolved(optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.HostOSInitRequired).To(BeFalse())
		Expect(calls).To(Equal(2))
	})

	It("maps wildcard DELAY_HOST_OS_INIT to every discovered port in PCI order", func() {
		optCtx := &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"DELAY_HOST_OS_INIT=ENABLE_USER"}}},
				},
			},
			DiscoverPorts: discoverPortsForTest(),
		}
		resolved, err := EnsureResolved(optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.HostOSInitRequired).To(BeTrue())
		Expect(resolved.HostOSInitPCIs).To(Equal([]string{testPci0, testPci1}))
	})

	It("deduplicates explicit DELAY_HOST_OS_INIT devices", func() {
		optCtx := &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{Device: ptr.To("p1"), Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}},
						{Device: ptr.To("p0"), Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}},
						{Device: ptr.To("P1"), Parameters: []string{"DELAY_HOST_OS_INIT=ENABLE_USER"}},
					},
				},
			},
			DiscoverPorts: discoverPortsForTest(),
		}
		resolved, err := EnsureResolved(optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.HostOSInitPCIs).To(Equal([]string{testPci0, testPci1}))
	})

	It("errors when named device has no matching port", func() {
		optCtx := &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{{Device: ptr.To("p1"), Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}}},
				},
			},
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPci0}}, nil
			},
		}
		_, err := EnsureResolved(optCtx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no PCI device found"))
		Expect(optCtx.GetResolvedNVConfig()).To(BeNil())
	})

	It("errors when wildcard matches no discovered ports", func() {
		optCtx := &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}}},
				},
			},
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) { return nil, nil },
		}
		_, err := EnsureResolved(optCtx)
		Expect(err).To(MatchError(ContainSubstring("no physical ports discovered")))
	})

	It("recognizes DELAY_HOST_OS_INIT user-mode values", func() {
		Expect(isDelayHostOSInitUserMode("0x3")).To(BeTrue())
		Expect(isDelayHostOSInitUserMode("3")).To(BeTrue())
		Expect(isDelayHostOSInitUserMode("ENABLE_USER")).To(BeTrue())
		Expect(isDelayHostOSInitUserMode("0x1")).To(BeFalse())
	})
})
