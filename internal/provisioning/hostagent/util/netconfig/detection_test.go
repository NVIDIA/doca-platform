// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNetconfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Netconfig Suite")
}

var _ = Describe("Backend Detection", func() {
	Context("HasNetworkManager", func() {
		It("should check if nmcli command exists", func() {
			// This test checks if the function runs without error
			// The actual result depends on the system where tests run
			result := HasNetworkManager()
			Expect(result).To(BeAssignableToTypeOf(false))
		})
	})

	Context("HasSystemdNetworkd", func() {
		It("should check if systemd-networkd exists", func() {
			// This test checks if the function runs without error
			// The actual result depends on the system where tests run
			result := HasSystemdNetworkd()
			Expect(result).To(BeAssignableToTypeOf(false))
		})
	})

	Context("HasNetplan", func() {
		It("should check if netplan command exists", func() {
			// This test checks if the function runs without error
			// The actual result depends on the system where tests run
			result := HasNetplan()
			Expect(result).To(BeAssignableToTypeOf(false))
		})
	})

	Context("DetectBackend", func() {
		It("should return a backend or error", func() {
			// This test verifies the function returns either a valid backend or an error
			backend, err := DetectBackend()
			if err != nil {
				// If error, it should be about no backend found
				Expect(err.Error()).To(ContainSubstring("no supported network configuration backend found"))
				Expect(backend).To(BeNil())
			} else {
				// If no error, backend should be non-nil and have a name
				Expect(backend).ToNot(BeNil())
				Expect(backend.Name()).ToNot(BeEmpty())
			}
		})
	})
})
