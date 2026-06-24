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

package identity

import (
	"strings"
	"testing"
)

func TestNormalizeSerial(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "lowercases", raw: "MT2152X00ABC", want: "mt2152x00abc"},
		{name: "trims surrounding whitespace", raw: "  mt2152x00abc\n", want: "mt2152x00abc"},
		{name: "keeps hyphen and underscore", raw: "mt-2152_x00", want: "mt-2152_x00"},
		{name: "keeps dot and tilde", raw: "mt.2152~x00", want: "mt.2152~x00"},
		{name: "already normalized is stable", raw: "abc123", want: "abc123"},
		{name: "max length 64 accepted", raw: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},

		{name: "empty rejected", raw: "", wantErr: true},
		{name: "whitespace-only rejected", raw: "   ", wantErr: true},
		{name: "over max length rejected", raw: strings.Repeat("a", 65), wantErr: true},
		{name: "space inside rejected", raw: "mt 2152", wantErr: true},
		{name: "slash rejected (path separator)", raw: "mt/2152", wantErr: true},
		{name: "unicode rejected", raw: "mt2152\u00e9", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeSerial(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeSerial(%q) = %q, want error", tc.raw, got)
				}
				if got != "" {
					t.Fatalf("NormalizeSerial(%q) returned %q on error, want empty string", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSerial(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeSerial(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeSerialIsIdempotent(t *testing.T) {
	inputs := []string{"mt2152x00abc", "ab-cd_ef", "mt.2152~x00", strings.Repeat("z", 64)}
	for _, in := range inputs {
		once, err := NormalizeSerial(in)
		if err != nil {
			t.Fatalf("NormalizeSerial(%q) unexpected error: %v", in, err)
		}
		twice, err := NormalizeSerial(once)
		if err != nil {
			t.Fatalf("NormalizeSerial(%q) (2nd pass) unexpected error: %v", once, err)
		}
		if once != twice {
			t.Fatalf("NormalizeSerial not idempotent: %q -> %q -> %q", in, once, twice)
		}
	}
}
