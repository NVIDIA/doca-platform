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
	"testing"
)

func Test_parseDOCAVersionFromFilename(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"published bundle", "/bfb/dpf-operator-system-bf-bundle-bf4-os-doca-bundle-3.3.0-341_26.01_ubuntu-24.04_64k.iso", "3.3.0-341"},
		{"jenkins rename", "bf4-os-doca-bundle-3.3.0-310_26.01_ubuntu-24.04_64k.iso", "3.3.0-310"},
		{"no build suffix", "something-doca-bundle-2.9.3_x.iso", "2.9.3"},
		{"no version", "bf4-noble-64k-arm64.iso", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseDOCAVersionFromFilename(tc.path); got != tc.want {
				t.Fatalf("parseDOCAVersionFromFilename(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func Test_ExtractDOCAVersionFromISO_filename(t *testing.T) {
	// The version comes from the filename, so no ISO read is required.
	isoPath := "/bfb/dpf-operator-system-bf-bundle-bf4-os-doca-bundle-3.3.0-341_26.01_ubuntu-24.04_64k.iso"

	formatted, raw, err := ExtractDOCAVersionFromISO(isoPath)
	if err != nil {
		t.Fatalf("ExtractDOCAVersionFromISO() error = %v", err)
	}
	if raw != "3.3.0-341" {
		t.Fatalf("raw = %q, want %q", raw, "3.3.0-341")
	}
	if formatted != "3.3.0" {
		t.Fatalf("formatted = %q, want %q", formatted, "3.3.0")
	}
}

func Test_ExtractDOCAVersionFromISO_missingVersion(t *testing.T) {
	if _, _, err := ExtractDOCAVersionFromISO("/bfb/bf4-noble-64k-arm64.iso"); err == nil {
		t.Fatal("expected error when filename carries no DOCA version")
	}
}
