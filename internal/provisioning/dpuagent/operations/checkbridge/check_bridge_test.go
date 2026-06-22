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
	"bytes"
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
	mtu  int
}

func (m *mockLink) Attrs() *netlink.LinkAttrs {
	return &netlink.LinkAttrs{Name: m.name, MTU: m.mtu}
}

func (m *mockLink) Type() string {
	return "bridge"
}

const testMTU = 1500

func mockLinkByName(links map[string]*mockLink) linkByNameFunc {
	return func(name string) (netlink.Link, error) {
		if l, ok := links[name]; ok {
			return l, nil
		}
		return nil, errors.New("link not found: " + name)
	}
}

var _ = Describe("CheckBridge", func() {
	var (
		checkBridge *CheckBridge
		optCtx      *operations.Context
	)

	BeforeEach(func() {
		checkBridge = &CheckBridge{}
		optCtx = &operations.Context{
			Options: opts.Options{
				ZeroTrustMode:   false,
				ControlPlaneMTU: testMTU,
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
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: testMTU},
				"pf0vf0":     {name: "pf0vf0", mtu: testMTU},
			})
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return nil, errors.New("failed to list addresses")
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
		})

		It("should return error and run netplan apply when no addresses are found", func() {
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: testMTU},
				"pf0vf0":     {name: "pf0vf0", mtu: testMTU},
			})
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return []netlink.Addr{}, nil
			}
			netplanApplied := false
			checkBridge.runBash = func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				Expect(cmd).To(Equal("netplan apply"))
				netplanApplied = true
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(netplanApplied).To(BeTrue())
		})

		It("should succeed when bridge has an IP address and MTU matches", func() {
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: testMTU},
				"pf0vf0":     {name: "pf0vf0", mtu: testMTU},
			})
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

		It("should succeed when multiple IP addresses exist and MTU matches", func() {
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: testMTU},
				"pf0vf0":     {name: "pf0vf0", mtu: testMTU},
			})
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

		It("should return error and run netplan apply when br-comm-ch MTU mismatches", func() {
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: 9000},
				"pf0vf0":     {name: "pf0vf0", mtu: testMTU},
			})
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return []netlink.Addr{
					{IPNet: &net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)}},
				}, nil
			}
			netplanApplied := false
			checkBridge.runBash = func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				Expect(cmd).To(Equal("netplan apply"))
				netplanApplied = true
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(netplanApplied).To(BeTrue())
		})

		It("should return error and run netplan apply when pf0vf0 MTU mismatches", func() {
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: testMTU},
				"pf0vf0":     {name: "pf0vf0", mtu: 9000},
			})
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return []netlink.Addr{
					{IPNet: &net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)}},
				}, nil
			}
			netplanApplied := false
			checkBridge.runBash = func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				Expect(cmd).To(Equal("netplan apply"))
				netplanApplied = true
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(netplanApplied).To(BeTrue())
		})

		It("should return error when LinkByName fails for pf0vf0 during MTU check", func() {
			checkBridge.linkByName = mockLinkByName(map[string]*mockLink{
				"br-comm-ch": {name: "br-comm-ch", mtu: testMTU},
			})
			checkBridge.addrList = func(link netlink.Link, family int) ([]netlink.Addr, error) {
				return []netlink.Addr{
					{IPNet: &net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)}},
				}, nil
			}

			err := checkBridge.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pf0vf0"))
		})

	})
})
