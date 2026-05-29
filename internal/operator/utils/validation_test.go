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

package utils

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Validation Test Suite")
}

var _ = Describe("ValidateDPFVersion", func() {
	var originalAllowSourceVersions []string

	BeforeEach(func() {
		originalAllowSourceVersions = make([]string, len(allowSourceVersions))
		copy(originalAllowSourceVersions, allowSourceVersions)
	})

	AfterEach(func() {
		allowSourceVersions = originalAllowSourceVersions
	})

	DescribeTable("version validation",
		func(prevVer *string, allowedSources []string, shouldSucceed bool, expectedErrorSubstring string) {
			if allowedSources != nil {
				allowSourceVersions = allowedSources
			}

			err := ValidateDPFVersion(prevVer)

			if shouldSucceed {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
				if expectedErrorSubstring != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorSubstring))
				}
			}
		},
		Entry("nil previous version should fail", nil, nil, false, "upgrades are only supported starting from DPF version"),
		Entry("invalid previous version format should fail", ptr.To("invalid-version"), nil, false, "parse requested version"),
		Entry("v25.4.0 previous version should fail with multiple allowed sources", ptr.To("v25.4.0"), []string{"v25.10.0", "v25.7.0"}, false, "only upgrades from v25.10.0, v25.7.0 are supported"),
		Entry("v24.10.0 previous version should fail when not in allowed sources", ptr.To("v24.10.0"), []string{"v25.7.0"}, false, "is not compatible with current DPF version"),

		Entry("last released source version should succeed by default", ptr.To("v26.4.0-rc.2"), nil, true, ""),
		Entry("same minor line as last released source should succeed by default", ptr.To("v26.4.1-rc.1"), nil, true, ""),
		Entry("v25.7.0-beta.3 should succeed to upgrade from", ptr.To("v25.7.0-beta.3"), []string{"v25.7.0"}, true, ""),
		Entry("v25.7.0 previous version should succeed when exact match", ptr.To("v25.7.0"), []string{"v25.7.0"}, true, ""),
		Entry("v25.7.5 previous version should succeed when v25.7.0 is allowed", ptr.To("v25.7.5"), []string{"v25.7.0"}, true, ""),
		Entry("v25.10.2 previous version should succeed when v25.10.0 is allowed", ptr.To("v25.10.2"), []string{"v25.10.0"}, true, ""),
		Entry("v26.1.1 previous version should succeed when v26.1.0 is allowed", ptr.To("v26.1.1"), []string{"v26.1.0"}, true, ""),
		Entry("v25.10.2 previous version should succeed with multiple allowed sources", ptr.To("v25.10.2"), []string{"v25.10.0", "v25.7.0"}, true, ""),
		Entry("v25.7.5 previous version should succeed with multiple allowed sources", ptr.To("v25.7.5"), []string{"v25.10.0", "v25.7.0"}, true, ""),
		Entry("v25.7.0 should succeed when upgrading from v25.7.0-rc.2", ptr.To("v0.1.0-1e000000-test"), []string{"v25.7.0"}, true, ""),
	)
})

var _ = Describe("isSameMajorMinor", func() {
	DescribeTable("version comparison",
		func(prevVersionStr string, targetVersion string, expected bool) {
			prevVersion := semver.MustParse(prevVersionStr)
			result := isSameMajorMinor(prevVersion, targetVersion)
			Expect(result).To(Equal(expected))
		},
		Entry("same version", "v25.7.0", "v25.7.0", true),
		Entry("same major.minor with different patch", "v25.7.0", "v25.7.1", true),
		Entry("same major.minor with prerelease", "v25.7.0", "v25.7.0-beta.3", true),
		Entry("update from beta to another beta", "v25.10.0-beta.4", "v25.10.0-beta.5", true),
		Entry("different minor", "v25.7.0", "v25.10.0", false),
		Entry("different major", "v25.7.0", "v26.7.0", false),
		Entry("invalid target version", "v25.7.0", "invalid-version", false),
	)
})
