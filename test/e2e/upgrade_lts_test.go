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
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/upgrade/rollout"

	. "github.com/onsi/ginkgo/v2"
)

// expectedDPUServicesCurrent returns the DPUService shape at HEAD. nvidia-k8s-ipam,
// servicechainset-controller and kube-state-metrics are each split into a per-cluster
// controller service plus a node/RBAC companion service.
//
// When a release changes the shape, rename this to the version it describes and add a
// new expectedDPUServicesCurrent on top of it, so that "Current" always tracks HEAD.
func expectedDPUServicesCurrent(input *systemTestInput) []string {
	c := input.dpuClusters[0]
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
		operatorv1.DPUMonitoringName.String(),
		operatorv1.NodeProblemDetectorName.String(),
		operatorv1.OpenTelemetryCollectorName.String(),
	}
}

// The BFB LTS upgrade path: install v30.0 with DPUs on BFB LTS 3.6.0, then
// validate HEAD by concurrently reprovisioning one DPU with the current BFB and
// the other with its existing BFB. Each phase is its own labeled Ginkgo
// container, selected by CI via its label. The path starts at the last released
// GA because that is the only release HEAD accepts as an upgrade source. Insert a
// validationPhase before "current" for each hop the path gains.
var _ = Describe("DPF Upgrade LTS", func() {
	installPhase("BFB LTS v30.0", installPhaseInput{
		label: Domain.DPFBFBLTSUpgrade,

		// v30.0 pins its own Kubernetes version, which can differ from HEAD's
		// util.KubernetesVersion.
		expectedKubernetesVersion: "v1.36.5",
		artifactsKey:              "v30.0",
		expectedDPUServices:       expectedDPUServicesCurrent,
	})

	validationPhase("current", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeCurrent,

		// Reprovision both DPUs concurrently: the selected DPU moves to the
		// current BFB while the other DPU keeps the existing BFB LTS 3.6.0.
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.ExpectDPFVersion(func() string { return tag }),
			rollout.ForDPUDeployment(0, rollout.ReprovisionWithCurrentDependencies()),
			rollout.ForDPUDeployment(1, rollout.ReprovisionWithExistingDependencies()),
		),
		verifyKubeletVersion: true,
		expectedDPFVersion:   func() string { return tag },

		artifactsKey:                    "current",
		compareArtifactsToBeforeRollout: "v30.0",

		expectedDPUServices: expectedDPUServicesCurrent,
	})
})
