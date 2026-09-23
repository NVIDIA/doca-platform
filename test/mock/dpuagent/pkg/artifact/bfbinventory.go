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

package artifact

import (
	"encoding/json"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	bfbutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
)

// maxPrintableRun bounds a single printable run kept in memory. The inventory JSON lines are
// short; anything longer is binary that happens to be printable and is dropped.
const maxPrintableRun = 64 * 1024

// InventoryScanner is the streaming twin of bfbutil.VersionFromBFBFile. It collects printable
// ASCII runs of at least four characters, starts recording at the line containing
// "This JSON represents" and stops at the line containing `"Members@odata.count":`.
// The BFB controller derives BFB.status.versions with the same rule, so both sides agree.
type InventoryScanner struct {
	current   []byte
	overflow  bool
	inJSON    bool
	done      bool
	jsonLines []string
}

// NewInventoryScanner returns an empty scanner.
func NewInventoryScanner() *InventoryScanner {
	return &InventoryScanner{}
}

// Write consumes the next chunk of the BFB stream.
func (s *InventoryScanner) Write(p []byte) (int, error) {
	if s.done {
		return len(p), nil
	}
	for _, c := range p {
		if c >= 0x20 && c <= 0x7e {
			if s.overflow {
				continue
			}
			if len(s.current) >= maxPrintableRun {
				s.overflow = true
				s.current = s.current[:0]
				continue
			}
			s.current = append(s.current, c)
			continue
		}
		s.flush()
		if s.done {
			break
		}
	}
	return len(p), nil
}

func (s *InventoryScanner) flush() {
	defer func() {
		s.current = s.current[:0]
		s.overflow = false
	}()
	if s.overflow || len(s.current) < 4 {
		return
	}
	line := string(s.current)
	if strings.Contains(line, "This JSON represents") {
		s.jsonLines = append(s.jsonLines, "{")
		s.inJSON = true
	}
	if !s.inJSON {
		return
	}
	s.jsonLines = append(s.jsonLines, strings.TrimSpace(line))
	if strings.Contains(line, `"Members@odata.count":`) {
		s.jsonLines = append(s.jsonLines, "}")
		s.done = true
	}
}

// Found reports whether a complete inventory JSON was seen.
func (s *InventoryScanner) Found() bool {
	return s.done
}

type softwareInventory struct {
	Members []struct {
		Name    string `json:"Name"`
		Version string `json:"Version"`
	} `json:"Members"`
}

// Versions parses the collected inventory into the BFB version fields, applying the same DOCA
// normalization as the BFB controller. It fails when no inventory was found in the stream.
func (s *InventoryScanner) Versions() (*provisioningv1.BFBVersions, error) {
	if !s.done {
		return nil, fmt.Errorf("software inventory JSON not found in BFB")
	}
	inventory := &softwareInventory{}
	if err := json.Unmarshal([]byte(strings.Join(s.jsonLines, "\n")), inventory); err != nil {
		return nil, fmt.Errorf("parse BFB software inventory: %w", err)
	}
	versions := &provisioningv1.BFBVersions{}
	for _, item := range inventory.Members {
		switch item.Name {
		case "BF3_ATF":
			versions.ATF = item.Version
		case "DOCA":
			formatted, err := bfbutil.FormatDOCAVersion(item.Version)
			if err != nil {
				return nil, fmt.Errorf("format DOCA version: %w", err)
			}
			versions.DOCA = formatted
		case "BF3_BSP":
			versions.BSP = item.Version
		case "BF3_UEFI":
			versions.UEFI = item.Version
		}
	}
	return versions, nil
}
