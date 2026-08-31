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

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

// The regular previous-GA → main/release-branch upgrade: an install phase that
// provisions against the previous GA release, then a validation phase after the
// operator has been upgraded externally. Each phase is its own labeled Ginkgo
// container, selected by CI via its label.
var _ = Describe("DPF Upgrade", func() {
	installPhase("previous GA", installPhaseInput{
		label:           Domain.DPFUpgrade,
		skipBFBImageURL: true,
		// The previous GA (LAST_STABLE_DPF_VERSION, default v26.8.0-alpha.3) pins its own
		// Kubernetes version, which differs from HEAD's util.KubernetesVersion.
		expectedKubernetesVersion: "v1.35.6",
		// TODO: Remove once we move to first beta release of 26.8
		dpuClusterRunsCoreDNS: true,
		artifactsKey:          "before",
		expectedDPUServices:   expectedDPUServicesCurrent,
	})

	validationPhase("GA-to-current", validationPhaseInput{
		label:                Domain.DPFUpgradeValidation,
		captureBeforeRollout: true,
		artifactsKey:         "after",
		prevArtifactsKey:     "before",
		rolloutDependencies:  true,
		expectedDPUServices:  expectedDPUServicesCurrent,
		expectedDPFVersion:   func() string { return tag },
	})
})
