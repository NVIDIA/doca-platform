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
	"github.com/nvidia/doca-platform/test/e2e/upgrade/rollout"

	. "github.com/onsi/ginkgo/v2"
)

// The regular previous-GA → main/release-branch upgrade: an install phase that
// provisions against the previous GA release, then a validation phase after the
// operator has been upgraded externally. Each phase is its own labeled Ginkgo
// container, selected by CI via its label.
var _ = Describe("DPF Upgrade", func() {
	installPhase("previous-GA", installPhaseInput{
		label:               Domain.DPFUpgrade,
		artifactsKey:        "before",
		expectedDPUServices: expectedDPUServicesCurrent,
	})

	validationPhase("current", validationPhaseInput{
		label:                           Domain.DPFUpgradeValidation,
		artifactsKey:                    "after",
		compareArtifactsToBeforeRollout: "before",
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.ExpectDPFVersion(func() string { return tag }),
			rollout.ForDPUDeployment(0, rollout.WithCurrentDependencies()),
			rollout.ForDPUDeployment(1, rollout.ReprovisionWithExistingDependencies()),
		),
		expectedDPUServices: expectedDPUServicesCurrent,
		expectedDPFVersion:  func() string { return tag },
	})
})

// The LTS-BFB variant of the regular upgrade: a single-hop install phase that provisions against
// the LTS BFB (instead of the pinned previous-GA BFB the plain "DPF Upgrade" path above uses),
// then a validation phase after the operator has been upgraded externally that reprovisions one
// of the two per-node DPUDeployments onto the current/nightly BFB and reprovisions the other in
// place while it keeps its existing (LTS) BFB. This exercises reprovisioning on both the LTS and
// current BFB after an operator upgrade in a single run.
var _ = Describe("DPF Upgrade Using LTS BFB", func() {
	installPhase("previous-GA-lts-bfb", installPhaseInput{
		label:               Domain.DPFUpgradeUsingLTSBFB,
		artifactsKey:        "before",
		expectedDPUServices: expectedDPUServicesCurrent,
	})

	validationPhase("current-using-lts-bfb", validationPhaseInput{
		label:                           Domain.DPFUpgradeUsingLTSBFBValidation,
		artifactsKey:                    "after",
		compareArtifactsToBeforeRollout: "before",
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.ExpectDPFVersion(func() string { return tag }),
			rollout.ForDPUDeployment(0, rollout.ReprovisionWithCurrentDependencies()),
			rollout.ForDPUDeployment(1, rollout.ReprovisionWithExistingDependencies()),
		),
		expectedDPUServices: expectedDPUServicesCurrent,
		expectedDPFVersion:  func() string { return tag },
	})
})

// The ZeroTrust previous-GA → main/release-branch upgrade: an install phase
// that provisions against the previous GA release in ZeroTrust mode, then a
// validation phase after the operator has been upgraded externally.
var _ = Describe("DPF Upgrade ZT", func() {
	installPhase("previous GA ZeroTrust", installPhaseInput{
		label:     Domain.DPFUpgrade,
		zeroTrust: true,
		// ZeroTrust hosts have no PCI visibility of the DPU (see the DPUDeployment
		// creation step below), so the host-side component shape at this release
		// (e.g. kata-containers, which requires PCI passthrough) can genuinely
		// differ from host-trusted's. Skip the current-shape assertion here, same
		// as the BFB LTS v25.10 install phase.
		skipSystemComponentValidation: true,
		// The previous GA (LAST_STABLE_DPF_VERSION, default v26.8.0-latest-stable) pins
		// its own Kubernetes version, which differs from HEAD's util.KubernetesVersion.
		expectedKubernetesVersion: "v1.35.6",
		artifactsKey:              "before",
		expectedDPUServices:       expectedDPUServicesCurrent,
	})

	validationPhase("GA-to-current ZeroTrust", validationPhaseInput{
		label:                           Domain.DPFUpgradeValidation,
		zeroTrust:                       true,
		expectedDPFVersion:              func() string { return tag },
		artifactsKey:                    "after",
		compareArtifactsToBeforeRollout: "before",
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.ExpectDPFVersion(func() string { return tag }),
			rollout.ForDPUDeployment(0, rollout.WithCurrentDependencies()),
		),
		expectedDPUServices: expectedDPUServicesCurrent,
	})
})
