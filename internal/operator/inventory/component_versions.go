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

package inventory

import (
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	"github.com/Masterminds/semver/v3"
)

// componentIntroducedVersions tracks the DPF version when a component was first introduced.
// Components not listed here are assumed to have existed since the minimum supported upgrade version
// and will always be checked during upgrade validation.
var componentIntroducedVersions = map[operatorv1.ComponentName]string{
	operatorv1.KubeStateMetricsName:       "v26.4.0",
	operatorv1.NodeProblemDetectorName:    "v26.4.0",
	operatorv1.OpenTelemetryCollectorName: "v26.4.0",
	operatorv1.KataContainersName:         "v26.4.1",
	operatorv1.VaultKMSName:               "v26.8.0",
	operatorv1.DPUMonitoringName:          "v26.8.0",
	operatorv1.CoreDNSName:                "v26.8.0",
}

// ShouldSkipUpgradeCheck determines if a component's upgrade readiness check should be skipped
// based on whether the component existed in the version being upgraded from.
func ShouldSkipUpgradeCheck(componentName operatorv1.ComponentName, upgradeFromVersion string) (bool, error) {
	introducedVersion, exists := componentIntroducedVersions[componentName]
	if !exists {
		return false, nil
	}

	fromVer, err := semver.NewVersion(upgradeFromVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse upgrade source version %s: %w", upgradeFromVersion, err)
	}

	// Unset prerelease version for comparison so the comparison is fine when upgrading to a pre-release version.
	// Otherwise testing upgrades from a pre-release which contains the feature would incorreclty skip
	// the checks.
	if fromVer.Prerelease() != "" {
		newFromVer, _ := fromVer.SetPrerelease("")
		fromVer = &newFromVer
	}

	introducedVer, err := semver.NewVersion(introducedVersion)
	if err != nil {
		return false, fmt.Errorf("failed to parse component introduction version %s for %s: %w",
			introducedVersion, componentName, err)
	}

	// Skip check if we're upgrading from a version before this component was introduced
	return fromVer.LessThan(introducedVer), nil
}
