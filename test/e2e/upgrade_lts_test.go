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

package e2e

import (
	"os"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
)

// expectedDPUServicesV2510 returns the pre-v26.04 DPUService shape: singleton
// nvidia-k8s-ipam and servicechainset-controller services, no
// kube-state-metrics on the DPU cluster, no per-cluster controller split.
// Only the BFB LTS Phase 1 install runs against this shape; once the operator
// is upgraded to v26.4 the controller reshapes DPUServices to the current
// layout (see expectedDPUServicesCurrent in upgrade_test.go) without needing
// a DPU reprovision.
func expectedDPUServicesV2510(_ *SystemTestInput) []string {
	return []string{
		operatorv1.FlannelName.String(),
		operatorv1.MultusName.String(),
		operatorv1.SRIOVDevicePluginName.String(),
		operatorv1.OVSCNIName.String(),
		operatorv1.SFCControllerName.String(),
		operatorv1.ServiceChainSetCRDsName.String(),
		operatorv1.CNIInstallerName.String(),
		operatorv1.NVIPAMControllerName.String(),
		operatorv1.ServiceSetControllerName.String(),
	}
}

// The BFB LTS multi-hop upgrade path: install v25.10, validate the v26.4 hop
// with a mandatory full DPU rollout (so DPUs start reporting KubeletVersion),
// then validate the v26.7 hop without reprovisioning. Each phase is its own
// labeled Ginkgo container, selected by CI via its label. Append a new
// validationPhase for each future hop (v26.10 → …).
var _ = Describe("DPF Upgrade LTS", func() {
	InstallPhase("BFB LTS v25.10", InstallPhaseInput{
		Label: Domain.DPFBFBLTSUpgrade,

		// Pin to the LTS BFB manifest even when CI exports BFB_IMAGE_URL.
		SkipBFBImageURL: true,
		// v25.10's servicechainset-controller creates a DPUServiceCredentialRequest with an
		// empty spec.targetCluster.name that the current CRD rejects. Provisioning works
		// without it being Ready, so skip the DPFOperatorConfig.Ready wait.
		SkipSystemComponentValidation: true,

		ExpectedKubernetesVersion: "v1.34.0",
		ArtifactsKey:              "v25.10",
		ExpectedDPUServices:       expectedDPUServicesV2510,
	})

	ValidationPhase("v26.4", ValidationPhaseInput{
		Label: Domain.DPFBFBLTSUpgradeV264,

		// Reprovision all DPUs under v26.4 so they start reporting KubeletVersion
		// (required for the v26.7 skew check), then exercise a dependency rollout.
		RolloutAllDPUs:         true,
		RolloutDPFVersionMinor: "v26.4",
		RolloutDependencies:    true,
		VerifyKubeletVersion:   true,

		ExpectedDPFVersion:        envOrDefault("DPF_V264_VERSION", "v26.4.0"),
		ExpectedKubernetesVersion: "v1.34.0",
		// Phase runs with -e2e.skip-cleanup, so clear the stale dpudevice-protection
		// finalizers here rather than at teardown (#5048585).
		RemoveStaleDPUDeviceFinalizers: true,

		// v26.4 post-rollout artifacts become the v26.7 comparison baseline.
		ArtifactsKey:               "v26.4",
		PreRolloutArtifactsKey:     "v26.4-pre-rollout",
		PreRolloutPrevArtifactsKey: "v25.10",

		ExpectedDPUServices: expectedDPUServicesCurrent,
	})

	ValidationPhase("current", ValidationPhaseInput{
		Label: Domain.DPFBFBLTSUpgradeCurrent,

		// No rollout. BFB stays at LTS 3.2.1 and DPUs are not reprovisioned.
		RolloutAllDPUs:       false,
		VerifyKubeletVersion: true,
		PatchDeploymentMode:  true,

		ArtifactsKey:     "current",
		PrevArtifactsKey: "v26.4",
		ExpectedChanges:  expectedChangesCurrent,

		ExpectedDPUServices: expectedDPUServicesCurrent,
	})
})

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
