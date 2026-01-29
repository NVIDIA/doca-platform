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

package checkbridge

import (
	"context"
	"errors"
	"net"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
)

// mockLink is a minimal implementation of netlink.Link for testing
type mockLink struct {
	name string
}

func (m *mockLink) Attrs() *netlink.LinkAttrs {
	return &netlink.LinkAttrs{Name: m.name}
}

func (m *mockLink) Type() string {
	return "bridge"
}

var _ = Describe("CheckBridgeIP", func() {
	var (
		checkBridge *CheckBridgeIP
		optCtx      *operations.Context
	)

	BeforeEach(func() {
		checkBridge = &CheckBridgeIP{}
		optCtx = &operations.Context{
			Options: opts.Options{
				ZeroTrustMode: false,
			},
		}
	})

	Describe("ShouldSkip", func() {
		It("should return true in ZeroTrustMode", func() {
			optCtx.Options.ZeroTrustMode = true
			Expect(checkBridge.ShouldSkip(optCtx)).To(BeTrue())
		})

		It("should return false when not in ZeroTrustMode", func() {
			optCtx.Options.ZeroTrustMode = false
			Expect(checkBridge.ShouldSkip(optCtx)).To(BeFalse())
		})
	})

	Describe("ShouldUpdateStatusBeforeContinue", func() {
		It("should return false", func() {
			Expect(checkBridge.ShouldUpdateStatusBeforeContinue(optCtx)).To(BeFalse())
		})
	})

	Describe("Execute", func() {
		It("should return error when LinkByName fails", func() {
			checkBridge.linkByName = func(name string) (netlink.Link, error) {
				return nil, errors.New("link not found")
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
		})

		It("should return error when AddrList fails", func() {
			checkBridge.linkByName = func(name string) (netlink.Link, error) {
				return &mockLink{name: name}, nil
			}
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return nil, errors.New("failed to list addresses")
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
		})

		It("should return error when no addresses are found", func() {
			checkBridge.linkByName = func(name string) (netlink.Link, error) {
				return &mockLink{name: name}, nil
			}
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return []netlink.Addr{}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
		})

		It("should succeed when bridge has an IP address", func() {
			checkBridge.linkByName = func(name string) (netlink.Link, error) {
				Expect(name).To(Equal("br-comm-ch"))
				return &mockLink{name: name}, nil
			}
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				Expect(family).To(Equal(netlink.FAMILY_V4))
				return []netlink.Addr{
					{
						IPNet: &net.IPNet{
							IP:   net.ParseIP("192.168.1.1"),
							Mask: net.CIDRMask(24, 32),
						},
					},
				}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should use the first IP address when multiple addresses exist", func() {
			checkBridge.linkByName = func(name string) (netlink.Link, error) {
				return &mockLink{name: name}, nil
			}
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return []netlink.Addr{
					{
						IPNet: &net.IPNet{
							IP:   net.ParseIP("10.0.0.1"),
							Mask: net.CIDRMask(24, 32),
						},
					},
					{
						IPNet: &net.IPNet{
							IP:   net.ParseIP("10.0.0.2"),
							Mask: net.CIDRMask(24, 32),
						},
					},
				}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
