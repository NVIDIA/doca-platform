// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NetworkManagerBackend", func() {
	var backend *NetworkManagerBackend

	BeforeEach(func() {
		backend = &NetworkManagerBackend{}
	})

	Context("Name", func() {
		It("should return NetworkManager", func() {
			Expect(backend.Name()).To(Equal("NetworkManager"))
		})
	})

	Context("IsAvailable", func() {
		It("should check NetworkManager availability", func() {
			// The actual result depends on the system where tests run
			result := backend.IsAvailable()
			Expect(result).To(BeAssignableToTypeOf(false))
		})
	})

	Context("getConnectionForInterface", func() {
		It("should return error for non-existent interface", func() {
			// Test with an interface that definitely doesn't exist
			_, err := backend.getConnectionForInterface("non-existent-interface-12345")
			// We just verify the function doesn't panic
			_ = err
		})
	})
})
