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

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Network", func() {
	Context("Constants", Label("Constants"), func() {
		It("should have correct BridgeName", func() {
			Expect(BridgeName).To(Equal("br-dpu"))
		})
	})

	// Netlink-based function tests - error paths (no root required)
	Context("GetCurrentMTU", Label("GetCurrentMTU"), func() {
		It("should return error for non-existent interface", func() {
			_, err := GetCurrentMTU("nonexistent-iface-12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get link"))
		})
	})

	Context("GetBridgeMembers", Label("GetBridgeMembers"), func() {
		It("should return error for non-existent bridge", func() {
			_, err := GetBridgeMembers("nonexistent-bridge-12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get bridge"))
		})
	})

	Context("SetLinkMTU", Label("SetLinkMTU"), func() {
		It("should return error for non-existent interface", func() {
			err := SetLinkMTU("nonexistent-iface-12345", 1500)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get link"))
		})
	})

	Context("AddVFToBridge", Label("AddVFToBridge"), func() {
		It("should return error for non-existent bridge", func() {
			err := AddVFToBridge("eth0", "nonexistent-bridge-12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get bridge link"))
		})
	})

	Context("RemoveVFFromBridge", Label("RemoveVFFromBridge"), func() {
		It("should return nil for non-existent VF (graceful handling)", func() {
			err := RemoveVFFromBridge("nonexistent-vf-12345")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
