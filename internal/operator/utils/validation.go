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
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/release"

	"github.com/Masterminds/semver/v3"
)

const (
	// minSupportedVersion is the minimum supported DPF version for upgrades.
	minSupportedVersion = "v25.7.0"
)

// allowSourceVersions is the version we allow upgrades from.
// This will consist usually only of a single version because we only support upgrades from the previous version.
// With LTS releases, this may include multiple versions in the future.
var allowSourceVersions = []string{
	release.LastReleasedDPFGAVersion,
}

// parseVersion parses a version string into a semver.Version object.
// Returns an error if the version string is empty.
func parseVersion(version string) (*semver.Version, error) {
	if version == "" {
		return nil, fmt.Errorf("version must not be empty")
	}

	sVer, err := semver.NewVersion(version)
	if err != nil {
		return nil, fmt.Errorf("invalid version %s: %v", version, err)
	}

	return sVer, nil
}

// isSameMajorMinor returns true if the previous version and the DPF release version
// have the same major and minor version.
func isSameMajorMinor(prevVersion *semver.Version, version string) bool {
	releaseVersion, err := semver.NewVersion(version)
	if err != nil {
		return false
	}

	return prevVersion.Major() == releaseVersion.Major() &&
		prevVersion.Minor() == releaseVersion.Minor()
}

// isAllowedSourceVersion returns true if prevVersion is allowed by allowSourceVersions (including pre-releases of allowed).
func isAllowedSourceVersion(prevVersion *semver.Version, allowedSources []string) bool {
	for _, allowed := range allowedSources {
		// Allow exact match, tilde range, or pre-release of allowed
		if isSameMajorMinor(prevVersion, allowed) {
			return true
		}
	}
	return false
}

// ValidateDPFVersion checks if the provided version is compatible with the installed DPF version.
// It allows upgrades from the same major.minor version (including pre-releases) and from specific allowed versions.
func ValidateDPFVersion(prevVersionStr *string) error {
	if prevVersionStr == nil {
		return fmt.Errorf("upgrades are only supported starting from DPF version %s", minSupportedVersion)
	}

	prevVersion, err := parseVersion(*prevVersionStr)
	if err != nil {
		return fmt.Errorf("parse requested version: %w", err)
	}

	if isSameMajorMinor(prevVersion, release.DPFVersion()) {
		return nil
	}
	if isAllowedSourceVersion(prevVersion, allowSourceVersions) {
		return nil
	}

	return fmt.Errorf("DPF version %s is not compatible with current DPF version %s, only upgrades from %s are supported",
		*prevVersionStr, release.DPFVersion(), strings.Join(allowSourceVersions, ", "))
}

// IsUpgradeFromLastReleasedGA checks if the given version is an upgrade from the last released GA.
// TODO: remove as soon as we have version aware upgrade logic for the pre-upgrade validation.
func IsUpgradeFromLastReleasedGA(version string) bool {
	v := semver.MustParse(release.LastReleasedDPFGAVersion)
	return isSameMajorMinor(v, version)
}
