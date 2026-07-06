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
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// expectedDPUServicesCurrent returns the v26.04+ DPUService shape:
// nvidia-k8s-ipam, servicechainset-controller and kube-state-metrics are
// each split into a per-cluster controller service plus a node/RBAC
// companion service. Every phase of the regular GA upgrade path runs against
// this shape; a future LTS path can reuse it for its v26.04+ phases.
func expectedDPUServicesCurrent(input *SystemTestInput) []string {
	c := input.DPUClusters[0]
	return []string{
		operatorv1.FlannelName.String(),
		operatorv1.MultusName.String(),
		operatorv1.SRIOVDevicePluginName.String(),
		operatorv1.SFCControllerName.String(),
		operatorv1.ServiceChainSetCRDsName.String(),
		operatorv1.CNIInstallerName.String(),
		getPerClusterDPUServiceName(operatorv1.NVIPAMControllerName, c.Name, c.Namespace),
		operatorv1.NVIPAMNodeName.String(),
		getPerClusterDPUServiceName(operatorv1.ServiceSetControllerName, c.Name, c.Namespace),
		getPerClusterDPUServiceName(operatorv1.KubeStateMetricsName, c.Name, c.Namespace),
		operatorv1.KubeStateMetricsRBACName.String(),
	}
}

// expectedChangesCurrent lists the spec changes an upgrade to the current HEAD
// release intentionally introduces. Shared by every hop that lands on HEAD: the
// regular previous-GA → HEAD upgrade and the BFB LTS v26.4 → v26.7 hop.
var expectedChangesCurrent = []UpgradeExpectedChange{
	// DPUService .spec.security is newly defaulted at HEAD: "before" lacks it while
	// "after" has it, so strip it from "after" (before's generation is bumped by one).
	{
		GVK: dpuservicev1.GroupVersion.WithKind("DPUService"),
		Transform: func(artifact map[string]interface{}) {
			unstructured.RemoveNestedField(artifact, "spec", "security")
		},
	},
}

// The regular previous-GA → main/release-branch upgrade: an install phase that
// provisions against the previous GA release, then a validation phase after the
// operator has been upgraded externally. Each phase is its own labeled Ginkgo
// container, selected by CI via its label.
var _ = Describe("DPF Upgrade", func() {
	InstallPhase("previous GA", InstallPhaseInput{
		Label:           Domain.DPFUpgrade,
		SkipBFBImageURL: true,
		// The previous GA (LAST_STABLE_DPF_VERSION, default v26.4.0) pins its own
		// Kubernetes version, which differs from HEAD's util.KubernetesVersion.
		ExpectedKubernetesVersion: "v1.34.0",
		ArtifactsKey:              "before",
		ExpectedDPUServices:       expectedDPUServicesCurrent,
	})

	ValidationPhase("GA-to-current", ValidationPhaseInput{
		Label:                Domain.DPFUpgradeValidation,
		PatchDeploymentMode:  true,
		CaptureBeforeRollout: true,
		ArtifactsKey:         "after",
		PrevArtifactsKey:     "before",
		RolloutDependencies:  true,
		ExpectedChanges:      expectedChangesCurrent,
		ExpectedDPUServices:  expectedDPUServicesCurrent,
	})
})
