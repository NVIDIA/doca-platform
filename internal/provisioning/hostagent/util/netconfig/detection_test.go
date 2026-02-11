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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNetconfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Netconfig Suite")
}

var _ = Describe("Backend Detection", func() {
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
