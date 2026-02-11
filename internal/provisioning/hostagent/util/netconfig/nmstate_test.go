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

package netconfig

import (
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

// mockNmstateClient is a test double for the nmstate library.
type mockNmstateClient struct {
	// appliedStates records each JSON state passed to ApplyNetState
	appliedStates []string
	// applyErr is the error returned by ApplyNetState
	applyErr error
}

func (m *mockNmstateClient) ApplyNetState(state string) (string, error) {
	m.appliedStates = append(m.appliedStates, state)
	return state, m.applyErr
}

var _ = Describe("hostCommand", func() {
	It("should wrap the command with nsenter and the correct namespace flags", func() {
		cmd := hostCommand("nmstatectl", "apply")
		Expect(cmd.Path).To(HaveSuffix("nsenter"))
		Expect(cmd.Args).To(Equal([]string{
			"nsenter",
			"--target", "1",
			"--cgroup",
			"--mount",
			"--ipc",
			"--pid",
			"--",
			"nmstatectl", "apply",
		}))
	})

	It("should handle commands with no extra arguments", func() {
		cmd := hostCommand("nmstatectl")
		Expect(cmd.Args).To(Equal([]string{
			"nsenter",
			"--target", "1",
			"--cgroup",
			"--mount",
			"--ipc",
			"--pid",
			"--",
			"nmstatectl",
		}))
	})
})

var _ = Describe("NmstateBackend", func() {
	var (
		backend *NmstateBackend
		mock    *mockNmstateClient
	)

	BeforeEach(func() {
		mock = &mockNmstateClient{}
		backend = &NmstateBackend{nms: mock}
	})

	Context("Name", func() {
		It("should return nmstate", func() {
			Expect(backend.Name()).To(Equal("nmstate"))
		})
	})

	Context("ApplyConfiguration", func() {
		It("should be a no-op when no interfaces are pending", func() {
			Expect(backend.ApplyConfiguration()).To(Succeed())
			Expect(mock.appliedStates).To(BeEmpty())
		})

		It("should marshal and apply all pending interfaces in one call", func() {
			mtu := 9000
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "eth0", Type: "ethernet", State: "up", MTU: &mtu},
				{Name: "eth1", Type: "ethernet", State: "up", MTU: &mtu},
			}

			Expect(backend.ApplyConfiguration()).To(Succeed())
			Expect(mock.appliedStates).To(HaveLen(1))

			var applied nmstateNetState
			Expect(json.Unmarshal([]byte(mock.appliedStates[0]), &applied)).To(Succeed())
			Expect(applied.Interfaces).To(HaveLen(2))
			Expect(applied.Interfaces[0].Name).To(Equal("eth0"))
			Expect(applied.Interfaces[1].Name).To(Equal("eth1"))
		})

		It("should clear pending state after successful apply", func() {
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "eth0", Type: "ethernet", State: "up"},
			}
			Expect(backend.ApplyConfiguration()).To(Succeed())
			Expect(backend.pendingInterfaces).To(BeNil())
		})

		It("should propagate nmstate apply errors", func() {
			mock.applyErr = fmt.Errorf("nmstate error: verification failed")
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "eth0", Type: "ethernet", State: "up"},
			}

			err := backend.ApplyConfiguration()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("verification failed"))
			// Pending state should NOT be cleared on failure
			Expect(backend.pendingInterfaces).To(HaveLen(1))
		})
	})

	Context("nmstate JSON contract", func() {
		It("should produce valid nmstate desired state for ethernet with MTU", func() {
			mtu := 9000
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "enp3s0f0np0", Type: "ethernet", State: "up", MTU: &mtu},
			}

			state := nmstateNetState{Interfaces: backend.pendingInterfaces}
			data, err := json.Marshal(state)
			Expect(err).ToNot(HaveOccurred())

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())

			ifaces := parsed["interfaces"].([]interface{})
			Expect(ifaces).To(HaveLen(1))

			iface := ifaces[0].(map[string]interface{})
			Expect(iface["name"]).To(Equal("enp3s0f0np0"))
			Expect(iface["type"]).To(Equal("ethernet"))
			Expect(iface["state"]).To(Equal("up"))
			Expect(iface["mtu"]).To(BeNumerically("==", 9000))
			Expect(iface).ToNot(HaveKey("ipv4"))
		})

		It("should produce valid nmstate desired state for ethernet with DHCP", func() {
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "eth0", Type: "ethernet", State: "up",
					IPv4: &nmstateIPv4{Enabled: true, DHCP: true}},
			}

			state := nmstateNetState{Interfaces: backend.pendingInterfaces}
			data, err := json.Marshal(state)
			Expect(err).ToNot(HaveOccurred())

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())

			iface := parsed["interfaces"].([]interface{})[0].(map[string]interface{})
			Expect(iface).ToNot(HaveKey("mtu"))

			ipv4 := iface["ipv4"].(map[string]interface{})
			Expect(ipv4["enabled"]).To(BeTrue())
			Expect(ipv4["dhcp"]).To(BeTrue())
		})

		It("should omit optional fields when not set", func() {
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "eth0", Type: "ethernet", State: "up"},
			}

			state := nmstateNetState{Interfaces: backend.pendingInterfaces}
			data, err := json.Marshal(state)
			Expect(err).ToNot(HaveOccurred())

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())

			iface := parsed["interfaces"].([]interface{})[0].(map[string]interface{})
			Expect(iface).ToNot(HaveKey("mtu"))
			Expect(iface).ToNot(HaveKey("ipv4"))
		})

		It("should produce valid linux-bridge state without bridge subtree for MTU-only", func() {
			mtu := 1500
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "br-dpu", Type: "linux-bridge", State: "up", MTU: &mtu},
			}

			state := nmstateNetState{Interfaces: backend.pendingInterfaces}
			data, err := json.Marshal(state)
			Expect(err).ToNot(HaveOccurred())

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())

			iface := parsed["interfaces"].([]interface{})[0].(map[string]interface{})
			Expect(iface["name"]).To(Equal("br-dpu"))
			Expect(iface["type"]).To(Equal("linux-bridge"))
			Expect(iface["mtu"]).To(BeNumerically("==", 1500))
			Expect(iface).ToNot(HaveKey("bridge"))
		})
	})

	Context("ConfigurePFInterfaces state accumulation", func() {
		It("should discard stale pending state via Init", func() {
			// Simulate leftover state from a previous failed configuration cycle
			backend.pendingInterfaces = []nmstateInterface{
				{Name: "stale-eth0", Type: "ethernet", State: "up"},
				{Name: "stale-br-dpu", Type: "linux-bridge", State: "up"},
			}

			backend.Reset()
			Expect(backend.pendingInterfaces).To(BeNil())
		})

		It("should skip ports with no MTU and no DHCP", func() {
			configs := []PortConfig{
				{PortNumber: 0, MTU: nil, DHCP: nil},
				{PortNumber: 1, MTU: nil, DHCP: nil},
			}

			needsApply, err := backend.ConfigurePFInterfaces("0000:00:00.0", configs)
			Expect(err).ToNot(HaveOccurred())
			Expect(needsApply).To(BeFalse())
			Expect(backend.pendingInterfaces).To(BeEmpty())
		})

		It("should fail on PCI lookup for configured ports with invalid PCI address", func() {
			configs := []PortConfig{
				{PortNumber: 0, MTU: nil, DHCP: nil},                 // skipped
				{PortNumber: 1, MTU: ptr.To(int32(9000)), DHCP: nil}, // needs PCI
			}

			_, err := backend.ConfigurePFInterfaces("0000:ff:ff.0", configs)
			Expect(err).To(HaveOccurred())
			Expect(backend.pendingInterfaces).To(BeEmpty())
		})
	})
})
