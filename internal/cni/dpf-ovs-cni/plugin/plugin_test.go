//go:build linux

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

package plugin

import (
	"context"

	cnitypes "github.com/nvidia/doca-platform/internal/cni/dpf-ovs-cni/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func uintPtr(v uint) *uint {
	return &v
}

var _ = Describe("plugin helpers", func() {
	Describe("splitVlanIds", func() {
		It("rejects a trunk ID above 4096", func() {
			_, err := splitVlanIds([]*cnitypes.Trunk{{ID: uintPtr(4097)}})
			Expect(err).To(MatchError(ContainSubstring("incorrect trunk id parameter")))
		})

		It("accepts VLAN ID 4096", func() {
			got, err := splitVlanIds([]*cnitypes.Trunk{{ID: uintPtr(4096)}})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]uint{4096}))
		})
	})

	Describe("waitLinkUp", func() {
		It("rejects a retry count below one", func() {
			err := waitLinkUp(context.Background(), nil, "pf0vf0", 0, 0)
			Expect(err).To(MatchError(ContainSubstring("retryCount must be at least 1")))
		})
	})
})
