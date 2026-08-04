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
	"fmt"
	"path/filepath"
	"regexp"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	bfbutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
)

// docaBundleVersionPattern extracts the DOCA version embedded in the OS ISO filename,
// e.g. "bf4-os-doca-bundle-3.3.0-341_26.01_ubuntu-24.04_64k.iso" -> "3.3.0-341".
var docaBundleVersionPattern = regexp.MustCompile(`(?i)doca-bundle-(\d+\.\d+\.\d+(?:-\d+)?)`)

// ExtractDOCAVersionFromISO derives the DOCA version for an OS ISO from its filename. The
// published bf4-os-doca-bundle images encode the version there (e.g.
// bf4-os-doca-bundle-3.3.0-341_...), so the version can be read without unpacking the image.
// It returns the formatted user-facing DOCA version plus the raw version.
func ExtractDOCAVersionFromISO(isoPath string) (formatted string, raw string, err error) {
	raw = parseDOCAVersionFromFilename(isoPath)
	if raw == "" {
		return "", "", fmt.Errorf("DOCA version not found in ISO filename %q", filepath.Base(isoPath))
	}

	formatted, err = bfbutil.FormatDOCAVersion(raw)
	if err != nil {
		return "", "", fmt.Errorf("format DOCA version from ISO filename %q: %w", filepath.Base(isoPath), err)
	}

	return formatted, raw, nil
}

// parseDOCAVersionFromFilename returns the DOCA version encoded in the ISO filename, or
// an empty string when the filename does not follow the doca-bundle naming convention.
func parseDOCAVersionFromFilename(isoPath string) string {
	matches := docaBundleVersionPattern.FindStringSubmatch(filepath.Base(isoPath))
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// ApplyDOCAVersionFromISO extracts DOCA version metadata from the OS ISO and writes it to
// BlueFieldSoftware status.
func ApplyDOCAVersionFromISO(bfs *provisioningv1.BlueFieldSoftware, isoPath string) error {
	formatted, raw, err := ExtractDOCAVersionFromISO(isoPath)
	if err != nil {
		return err
	}
	if bfs.Status.Versions == nil {
		bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{}
	}
	bfs.Status.Versions.DOCA = formatted
	bfs.Status.Versions.OSISOVersion = raw
	return nil
}
