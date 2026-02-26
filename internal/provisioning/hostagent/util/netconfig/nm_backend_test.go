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

package netconfig

import (
	"errors"
	"fmt"

	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/godbus/dbus/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NetworkManagerBackend", func() {
	var (
		mock    *mockNMClient
		backend *NetworkManagerBackend

		origGetCurrentMTU    func(string) (int, error)
		origGetBridgeMembers func(string) ([]string, error)
		origGetIfaceName     func(string, int) (string, error)
	)

	BeforeEach(func() {
		mock = newMockNMClient()
		backend = &NetworkManagerBackend{client: mock}

		origGetCurrentMTU = getCurrentMTUFunc
		origGetBridgeMembers = getBridgeMembersFunc
		origGetIfaceName = getInterfaceNameFunc
	})

	AfterEach(func() {
		getCurrentMTUFunc = origGetCurrentMTU
		getBridgeMembersFunc = origGetBridgeMembers
		getInterfaceNameFunc = origGetIfaceName
	})

	It("should report its name", func() {
		Expect(backend.Name()).To(Equal("NetworkManager"))
	})

	DescribeTable("IsDHCPConfigured",
		func(conns map[ConnectionPath]ConnectionSettings, iface string, want bool) {
			for p, s := range conns {
				mock.addTestConnection(p, s)
			}
			got, err := backend.IsDHCPConfigured(iface)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("auto -> true", map[ConnectionPath]ConnectionSettings{
			"/c/1": {"connection": {"interface-name": dbus.MakeVariant("eth0")}, "ipv4": {"method": dbus.MakeVariant("auto")}},
		}, "eth0", true),
		Entry("manual -> false", map[ConnectionPath]ConnectionSettings{
			"/c/1": {"connection": {"interface-name": dbus.MakeVariant("eth0")}, "ipv4": {"method": dbus.MakeVariant("manual")}},
		}, "eth0", false),
		Entry("no connection -> false", map[ConnectionPath]ConnectionSettings{}, "eth0", false),
		Entry("no ipv4 section -> false", map[ConnectionPath]ConnectionSettings{
			"/c/1": {"connection": {"interface-name": dbus.MakeVariant("eth0")}},
		}, "eth0", false),
	)

	Context("getConnectionForInterface", func() {
		It("should match by interface-name, fall back to ID, and prefer interface-name", func() {
			mock.addTestConnection("/conn/id-match", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("eth0")},
			})
			mock.addTestConnection("/conn/ifname-match", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("other"), "interface-name": dbus.MakeVariant("eth0")},
			})

			path, err := backend.getConnectionForInterface("eth0")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(ConnectionPath("/conn/ifname-match")))
		})

		It("should fall back to ID match when no interface-name match exists", func() {
			mock.addTestConnection("/conn/1", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("br-dpu")},
			})
			path, err := backend.getConnectionForInterface("br-dpu")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(ConnectionPath("/conn/1")))
		})

		It("should error when nothing matches", func() {
			_, err := backend.getConnectionForInterface("nonexistent")
			Expect(errors.Is(err, errConnectionNotFound)).To(BeTrue())
		})
	})

	Context("getOrCreateConnectionForInterface", func() {
		It("should return existing connection or create a new one", func() {
			By("creating when none exists")
			path, err := backend.getOrCreateConnectionForInterface("eth0", "802-3-ethernet")
			Expect(err).NotTo(HaveOccurred())
			Expect(mock.addedPaths).To(HaveLen(1))

			By("returning existing on second call")
			path2, err := backend.getOrCreateConnectionForInterface("eth0", "802-3-ethernet")
			Expect(err).NotTo(HaveOccurred())
			Expect(path2).To(Equal(path))
			Expect(mock.addedPaths).To(HaveLen(1))
		})

		It("should propagate D-Bus errors instead of creating duplicates", func() {
			mock.listErr = fmt.Errorf("D-Bus connection lost")
			_, err := backend.getOrCreateConnectionForInterface("eth0", "802-3-ethernet")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to look up connection"))
			Expect(mock.addedPaths).To(BeEmpty(), "should not create a connection on D-Bus error")
		})
	})

	Context("ApplyConfiguration", func() {
		It("should activate modified connections and propagate errors", func() {
			By("activating all tracked connections")
			backend.modifiedConnPaths = []ConnectionPath{"/conn/1", "/conn/2"}
			Expect(backend.ApplyConfiguration()).To(Succeed())
			Expect(mock.activated).To(ConsistOf(ConnectionPath("/conn/1"), ConnectionPath("/conn/2")))

			By("being a no-op when empty")
			mock.activated = nil
			Expect(backend.ApplyConfiguration()).To(Succeed())
			Expect(mock.activated).To(BeEmpty())

			By("returning the first activation error")
			mock.activateErr = fmt.Errorf("activation failed")
			backend.modifiedConnPaths = []ConnectionPath{"/conn/1"}
			Expect(backend.ApplyConfiguration()).To(MatchError(ContainSubstring("activation failed")))
		})
	})

	It("should deduplicate tracked connections", func() {
		backend.trackModifiedConnection("/conn/1")
		backend.trackModifiedConnection("/conn/1")
		backend.trackModifiedConnection("/conn/2")
		Expect(backend.modifiedConnPaths).To(HaveLen(2))
	})

	Context("mergeAndUpdateConnection", func() {
		It("should merge changes and strip unsafe round-trip properties", func() {
			mock.addTestConnection("/conn/1", ConnectionSettings{
				"connection":     {"id": dbus.MakeVariant("eth0")},
				"ipv4":           {"method": dbus.MakeVariant("manual"), "addresses": dbus.MakeVariant("bad")},
				"ipv6":           {"method": dbus.MakeVariant("auto"), "routes": dbus.MakeVariant("bad")},
				"802-3-ethernet": {"mtu": dbus.MakeVariant(uint32(1500))},
			})

			Expect(backend.mergeAndUpdateConnection("/conn/1", ConnectionSettings{
				"ipv4": {"method": dbus.MakeVariant("auto")},
			})).To(Succeed())

			updated := mock.updatedMap["/conn/1"]
			Expect(updated["ipv4"]["method"].Value()).To(Equal("auto"))
			Expect(updated["connection"]["id"].Value()).To(Equal("eth0"))
			Expect(updated["802-3-ethernet"]["mtu"].Value()).To(Equal(uint32(1500)))
			_, hasAddr := updated["ipv4"]["addresses"]
			Expect(hasAddr).To(BeFalse(), "unsafe ipv4.addresses should be stripped")
			_, hasRoutes := updated["ipv6"]["routes"]
			Expect(hasRoutes).To(BeFalse(), "unsafe ipv6.routes should be stripped")
		})

		It("should not mutate original settings map", func() {
			original := ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("eth0")},
				"ipv4":       {"method": dbus.MakeVariant("manual"), "addresses": dbus.MakeVariant("bad")},
			}
			mock.addTestConnection("/conn/1", original)

			Expect(backend.mergeAndUpdateConnection("/conn/1", ConnectionSettings{
				"ipv4": {"method": dbus.MakeVariant("auto")},
			})).To(Succeed())

			_, hasAddr := original["ipv4"]["addresses"]
			Expect(hasAddr).To(BeTrue(), "original map should not be mutated by merge")
		})

		It("should propagate update errors", func() {
			mock.addTestConnection("/conn/1", ConnectionSettings{"connection": {"id": dbus.MakeVariant("eth0")}})
			mock.updateErr = fmt.Errorf("update denied")
			Expect(backend.mergeAndUpdateConnection("/conn/1", ConnectionSettings{})).To(MatchError(ContainSubstring("update denied")))
		})
	})

	Context("ConfigurePFInterfaces", func() {
		BeforeEach(func() {
			getInterfaceNameFunc = func(_ string, port int) (string, error) {
				return fmt.Sprintf("eth%d", port), nil
			}
		})

		It("should update MTU when current differs from desired", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 1500, nil }
			mock.addTestConnection("/conn/eth0", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("eth0"), "interface-name": dbus.MakeVariant("eth0")},
			})

			mtu := int32(9000)
			needsApply, err := backend.ConfigurePFInterfaces("0000:4d:00", []hostutil.PortConfig{
				{PortNumber: 0, MTU: &mtu},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeTrue())
			Expect(backend.modifiedConnPaths).To(HaveLen(1))

			updated := mock.updatedMap["/conn/eth0"]
			Expect(updated["802-3-ethernet"]["mtu"].Value()).To(Equal(uint32(9000)))
		})

		It("should skip when MTU already matches", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 9000, nil }
			mock.addTestConnection("/conn/eth0", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("eth0"), "interface-name": dbus.MakeVariant("eth0")},
			})

			mtu := int32(9000)
			needsApply, err := backend.ConfigurePFInterfaces("0000:4d:00", []hostutil.PortConfig{
				{PortNumber: 0, MTU: &mtu},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeFalse())
			Expect(backend.modifiedConnPaths).To(BeEmpty())
		})

		It("should update DHCP when current differs", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 1500, nil }
			mock.addTestConnection("/conn/eth0", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("eth0"), "interface-name": dbus.MakeVariant("eth0")},
				"ipv4":       {"method": dbus.MakeVariant("manual")},
			})

			dhcp := true
			needsApply, err := backend.ConfigurePFInterfaces("0000:4d:00", []hostutil.PortConfig{
				{PortNumber: 0, DHCP: &dhcp},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeTrue())

			updated := mock.updatedMap["/conn/eth0"]
			Expect(updated["ipv4"]["method"].Value()).To(Equal("auto"))
		})

		It("should skip ports with no MTU or DHCP config", func() {
			needsApply, err := backend.ConfigurePFInterfaces("0000:4d:00", []hostutil.PortConfig{
				{PortNumber: 0},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeFalse())
		})

		It("should create new connection when none exists", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 1500, nil }
			mtu := int32(9000)

			needsApply, err := backend.ConfigurePFInterfaces("0000:4d:00", []hostutil.PortConfig{
				{PortNumber: 0, MTU: &mtu},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeTrue())
			Expect(mock.addedPaths).To(HaveLen(1))
		})
	})

	Context("ConfigureBridgeMTU", func() {
		It("should update bridge and member MTUs when they differ", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 1500, nil }
			getBridgeMembersFunc = func(name string) ([]string, error) { return []string{"enp1s0f0"}, nil }

			mock.addTestConnection("/conn/br", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("br-dpu"), "interface-name": dbus.MakeVariant("br-dpu")},
			})
			mock.addTestConnection("/conn/member", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("enp1s0f0"), "interface-name": dbus.MakeVariant("enp1s0f0")},
			})

			needsApply, err := backend.ConfigureBridgeMTU("br-dpu", 9000)
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeTrue())
			Expect(backend.modifiedConnPaths).To(HaveLen(2))

			bridgeUpdated := mock.updatedMap["/conn/br"]
			Expect(bridgeUpdated["802-3-ethernet"]["mtu"].Value()).To(Equal(uint32(9000)))

			memberUpdated := mock.updatedMap["/conn/member"]
			Expect(memberUpdated["802-3-ethernet"]["mtu"].Value()).To(Equal(uint32(9000)))
			Expect(memberUpdated["connection"]["master"].Value()).To(Equal("br-dpu"))
			Expect(memberUpdated["connection"]["slave-type"].Value()).To(Equal("bridge"))
		})

		It("should skip when MTUs already match", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 9000, nil }
			getBridgeMembersFunc = func(name string) ([]string, error) { return []string{"enp1s0f0"}, nil }

			mock.addTestConnection("/conn/br", ConnectionSettings{
				"connection": {"id": dbus.MakeVariant("br-dpu"), "interface-name": dbus.MakeVariant("br-dpu")},
			})

			needsApply, err := backend.ConfigureBridgeMTU("br-dpu", 9000)
			Expect(err).NotTo(HaveOccurred())
			Expect(needsApply).To(BeFalse())
		})

		It("should propagate GetBridgeMembers errors", func() {
			getCurrentMTUFunc = func(name string) (int, error) { return 9000, nil }
			getBridgeMembersFunc = func(name string) ([]string, error) {
				return nil, fmt.Errorf("bridge not found")
			}

			needsApply, err := backend.ConfigureBridgeMTU("br-dpu", 9000)
			Expect(err).To(MatchError(ContainSubstring("bridge not found")))
			Expect(needsApply).To(BeFalse())
		})
	})

	Context("ResetPendingChanges", func() {
		It("should clear stale modifiedConnPaths", func() {
			backend.modifiedConnPaths = []ConnectionPath{"/conn/stale"}
			backend.ResetPendingChanges()
			Expect(backend.modifiedConnPaths).To(BeNil())
		})
	})
})

var _ = Describe("makeConnectionSettings", func() {
	It("should produce correct ethernet settings", func() {
		s := makeConnectionSettings("eth0", "802-3-ethernet", "eth0")
		Expect(s["connection"]["id"].Value()).To(Equal("eth0"))
		Expect(s["connection"]["type"].Value()).To(Equal("802-3-ethernet"))
		Expect(s["connection"]["interface-name"].Value()).To(Equal("eth0"))
		Expect(s["ipv4"]["method"].Value()).To(Equal("auto"))
	})

	It("should produce correct bridge settings with disabled IP", func() {
		s := makeConnectionSettings("br-dpu", "bridge", "br-dpu")
		Expect(s["connection"]["type"].Value()).To(Equal("bridge"))
		Expect(s["ipv4"]["method"].Value()).To(Equal("disabled"))
		Expect(s).To(HaveKey("bridge"))
	})

	It("should omit interface-name when empty", func() {
		s := makeConnectionSettings("test", "802-3-ethernet", "")
		_, has := s["connection"]["interface-name"]
		Expect(has).To(BeFalse())
	})
})

var _ = DescribeTable("extractStringProp",
	func(section map[string]dbus.Variant, key, want string) {
		Expect(extractStringProp(section, key)).To(Equal(want))
	},
	Entry("existing key", map[string]dbus.Variant{"k": dbus.MakeVariant("v")}, "k", "v"),
	Entry("missing key", map[string]dbus.Variant{}, "k", ""),
	Entry("non-string value", map[string]dbus.Variant{"k": dbus.MakeVariant(42)}, "k", ""),
)
