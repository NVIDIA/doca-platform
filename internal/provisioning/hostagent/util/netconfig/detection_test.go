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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	cmdSystemctl    = "systemctl"
	svcNetworkMgr   = "NetworkManager"
	svcSystemdNetwd = "systemd-networkd"
)

var _ = Describe("DetectBackend", func() {
	var (
		origCommandRunner      func(string, ...string) ([]byte, error)
		origNewNMClientFn      func() (NMClient, error)
		origNMManagesDevicesFn func(NMClient) bool
	)

	BeforeEach(func() {
		origCommandRunner = commandRunner
		origNewNMClientFn = newNMClientFunc
		origNMManagesDevicesFn = nmManagesDevicesFunc
	})
	AfterEach(func() {
		commandRunner = origCommandRunner
		newNMClientFunc = origNewNMClientFn
		nmManagesDevicesFunc = origNMManagesDevicesFn
	})

	setupMocks := func(systemd, nm, nmManaging bool) {
		commandRunner = func(name string, args ...string) ([]byte, error) {
			switch {
			case name == cmdSystemctl && args[1] == svcSystemdNetwd:
				if systemd {
					return []byte("active\n"), nil
				}
				return []byte("inactive\n"), fmt.Errorf("exit status 3")
			case name == cmdSystemctl && args[1] == svcNetworkMgr:
				if nm {
					return []byte("active\n"), nil
				}
				return []byte("inactive\n"), fmt.Errorf("exit status 3")
			}
			return nil, fmt.Errorf("unexpected: %s %v", name, args)
		}
		newNMClientFunc = func() (NMClient, error) {
			if nm {
				return newMockNMClient(), nil
			}
			return nil, fmt.Errorf("D-Bus not available")
		}
		nmManagesDevicesFunc = func(_ NMClient) bool { return nmManaging }
	}

	type detectCase struct {
		systemd, nm, nmManaging bool
		wantBackend             string // empty means expect error
	}

	DescribeTable("selects the correct backend",
		func(tc detectCase) {
			setupMocks(tc.systemd, tc.nm, tc.nmManaging)
			backend, err := DetectBackend()
			if tc.wantBackend == "" {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no supported network backend found"))
				Expect(backend).To(BeNil())
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(backend.Name()).To(Equal(tc.wantBackend))
			}
		},
		Entry("only systemd-networkd active", detectCase{systemd: true, wantBackend: "systemd-networkd"}),
		Entry("only NetworkManager active", detectCase{nm: true, wantBackend: "NetworkManager"}),
		Entry("both active, NM managing devices", detectCase{systemd: true, nm: true, nmManaging: true, wantBackend: "NetworkManager"}),
		Entry("both active, NM not managing devices", detectCase{systemd: true, nm: true, wantBackend: "systemd-networkd"}),
		Entry("neither active", detectCase{wantBackend: ""}),
	)

	It("should treat NM as inactive when service is active but D-Bus is unreachable", func() {
		commandRunner = func(name string, args ...string) ([]byte, error) {
			if name == cmdSystemctl && args[1] == svcNetworkMgr {
				return []byte("active\n"), nil
			}
			return []byte("inactive\n"), fmt.Errorf("exit status 3")
		}
		newNMClientFunc = func() (NMClient, error) {
			return nil, fmt.Errorf("D-Bus connection refused")
		}

		backend, err := DetectBackend()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no supported network backend found"),
			"NM with unreachable D-Bus should be treated as inactive, leading to neither-active error")
		Expect(backend).To(BeNil())
	})

	It("should treat NM as inactive when GetVersion fails", func() {
		commandRunner = func(name string, args ...string) ([]byte, error) {
			if name == cmdSystemctl && args[1] == svcNetworkMgr {
				return []byte("active\n"), nil
			}
			return []byte("inactive\n"), fmt.Errorf("exit status 3")
		}
		mockClient := newMockNMClient()
		mockClient.versionErr = fmt.Errorf("version mismatch")
		newNMClientFunc = func() (NMClient, error) {
			return mockClient, nil
		}

		backend, err := DetectBackend()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no supported network backend found"),
			"NM with failing GetVersion should be treated as inactive")
		Expect(backend).To(BeNil())
	})
})
