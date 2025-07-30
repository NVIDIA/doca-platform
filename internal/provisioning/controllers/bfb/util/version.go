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

package util

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

type softwareInventory struct {
	ODataID string                    `json:"@odata.id"`
	Name    string                    `json:"Name"`
	Members []softwareInventoryMember `json:"Members"`
}

// softwareInventoryMember represents an individual software component in the inventory
type softwareInventoryMember struct {
	ODataID    string `json:"@odata.id"`
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	Version    string `json:"Version"`
	ODataType  string `json:"@odata.type"`
	SoftwareID string `json:"SoftwareId"`
}

// VersionFromBFBFile reads a BlueField BFB file and returns the version information.
// This function extracts version information from the software inventory JSON embedded in the BFB.
func VersionFromBFBFile(filename string) (version *provisioningv1.BFBVersions, reterr error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			reterr = err
		}
	}()
	// Extract software inventory from JSON
	jsonLines, err := extractJSONLinesFromBFB(file)
	if err != nil {
		return nil, err
	}

	inventory := &softwareInventory{}
	err = json.Unmarshal([]byte(strings.Join(jsonLines, "\n")), inventory)
	if err != nil {
		return nil, err
	}

	// Extract versions from software inventory
	return extractVersionsFromInventory(*inventory)
}

// extractJSONLinesFromBFB extracts JSON lines from the BFB file.
// This is the equivalent of running:
// strings  bfb-file.bfb |  grep -A1000  "This JSON represents" | grep -B 1000 "Members@odata.count"
// this is a similar implementation to https://github.com/vladsokolovsky/dpu-update/blob/main/OobUpdate.py#L171
// This function also does some formatting of the JSON content to enable parsing it.
func extractJSONLinesFromBFB(file *os.File) ([]string, error) {
	var jsonLines []string
	var currentString []rune
	inJSON := false

	r := bufio.NewReader(file)
	for {
		char, _, err := r.ReadRune()
		if err != nil {
			break
		}

		// This file is encoded as ASCII.
		if char <= 0x7F && unicode.IsPrint(char) {
			currentString = append(currentString, char)
		} else {
			if len(currentString) >= 4 {
				line := string(currentString)

				// Look for the start of the JSON structure
				if strings.Contains(line, "This JSON represents") {
					// The json software inventory in the BFB file does not have opening and closing braces.
					// We need to add those manually here.
					jsonLines = append(jsonLines, "{")
					inJSON = true
				}

				if inJSON {
					// Collect JSON lines
					jsonLines = append(jsonLines, strings.TrimSpace(line))
					// Stop when we have the count line
					if strings.Contains(line, `"Members@odata.count":`) {
						jsonLines = append(jsonLines, "}")
						break
					}
				}
			}
			currentString = nil
		}
	}

	if len(jsonLines) == 0 {
		return nil, fmt.Errorf("software inventory JSON not found in BFB")
	}
	return jsonLines, nil
}

// The DOCA version contains a build version.
// The first digit of the patch version will be aligned with the user-facing documented version.
// e.g. 3.0.0058 becomes 3.0.0, 2.9.3008 becomes 2.9.3
func formatDOCAVersion(version string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("DOCA version cannot be empty")
	}

	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("DOCA version must have 3 components (major.minor.patchbuild), got: %s", version)
	}

	// Extract patch from the patchbuild component (first digit)
	patchBuild := parts[2]
	if len(patchBuild) == 0 {
		return "", fmt.Errorf("DOCA version patchbuild component cannot be empty")
	}

	patch := string(patchBuild[0])

	return fmt.Sprintf("%s.%s.%s", parts[0], parts[1], patch), nil
}

// extractVersionsFromInventory extracts BFB versions from the software inventory.
func extractVersionsFromInventory(inventory softwareInventory) (*provisioningv1.BFBVersions, error) {
	versions := &provisioningv1.BFBVersions{}
	for _, item := range inventory.Members {
		switch item.Name {
		case "BF3_ATF":
			versions.ATF = item.Version
		case "DOCA":
			formattedVersion, err := formatDOCAVersion(item.Version)
			if err != nil {
				return nil, fmt.Errorf("failed to format DOCA version: %w", err)
			}
			versions.DOCA = formattedVersion
		case "BF3_BSP":
			versions.BSP = item.Version
		case "BF3_UEFI":
			versions.UEFI = item.Version
		}
	}
	return versions, nil
}
