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

package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CardType", func() {
	Context("CardTypeFromOPN", func() {
		DescribeTable("derives the card type from the OPN",
			func(opn string, expected CardType) {
				Expect(CardTypeFromOPN(opn)).To(Equal(expected))
			},
			Entry("production key card", "900-9D3B4-00SC-EA0", CardTypePK),
			Entry("development key card", "900-9D3B4-00SC-EAA", CardTypeDK),
			Entry("qualification card", "900-9D3B4-00SC-EAB", CardTypeQP),
			Entry("production key card of another board family", "900-9D3D4-00EN-HA0", CardTypePK),
			Entry("unknown last character", "900-9D3B4-00SC-EAZ", CardTypeUnknown),
			Entry("partner SKU", "900-9D3B4-00CV-AAA_DK", CardTypeUnknown),
			Entry("too few segments", "900-9D3B4-EA0", CardTypeUnknown),
			Entry("last segment too short", "900-9D3B4-00SC-A0", CardTypeUnknown),
			Entry("empty", "", CardTypeUnknown),
		)
	})

	Context("CardTypeFromBFBFileName", func() {
		DescribeTable("derives the card type from the BFB file name",
			func(fileName string, expected CardType) {
				Expect(CardTypeFromBFBFileName(fileName)).To(Equal(expected))
			},
			Entry("released prod signed file",
				"bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_prod.bfb", CardTypePK),
			Entry("released dev signed file",
				"bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_dev.bfb", CardTypeDK),
			Entry("documented dot separated prod suffix", "bf-bundle.prod.bfb", CardTypePK),
			Entry("documented dot separated dev suffix", "bf-bundle.dev.bfb", CardTypeDK),
			Entry("documented unsigned name", "bf-bundle.bfb", CardTypeQP),
			Entry("suffix must be a separate token", "bf-bundle-noprod.bfb", CardTypeQP),
			// The release tree aliases serve the bootstream directly, so the file name
			// they were built under is never visible to DPF.
			Entry("release tree pk alias", "last_stable_ubuntu_24.04_64k_pk", CardTypeUnknown),
			Entry("release tree qp alias", "last_stable_ubuntu_24.04_64k_qp", CardTypeUnknown),
			Entry("empty", "", CardTypeUnknown),
		)
	})

	Context("BFBFileNameFromURL", func() {
		DescribeTable("takes the file name from the download URL",
			func(url, expected string) {
				Expect(BFBFileNameFromURL(url)).To(Equal(expected))
			},
			Entry("released file",
				"https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_prod.bfb",
				"bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_prod.bfb"),
			Entry("query string", "https://example.com/bfb/bf-bundle_dev.bfb?token=abc", "bf-bundle_dev.bfb"),
			Entry("fragment", "https://example.com/bfb/bf-bundle_dev.bfb#sha256", "bf-bundle_dev.bfb"),
			Entry("release tree alias",
				"https://nbu-nfs.gtm.nvidia.com/auto/sw_mc_soc_release/doca_dpu/doca_3.4.0/last_stable_ubuntu_24.04_64k_pk",
				"last_stable_ubuntu_24.04_64k_pk"),
			Entry("empty", "", ""),
		)
	})
})
