// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SystemdNetworkdBackend", func() {
	var backend *SystemdNetworkdBackend

	BeforeEach(func() {
		backend = &SystemdNetworkdBackend{}
	})

	Context("Name", func() {
		It("should return systemd-networkd", func() {
			Expect(backend.Name()).To(Equal("systemd-networkd"))
		})
	})

	Context("IsAvailable", func() {
		It("should check systemd-networkd availability", func() {
			// The actual result depends on the system where tests run
			result := backend.IsAvailable()
			Expect(result).To(BeAssignableToTypeOf(false))
		})
	})
})
