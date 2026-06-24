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

// Package identity normalizes DPU hardware serials and builds agent SPIFFE IDs.
package identity

import (
	"fmt"
	"strings"
)

// MaxSerialLen is the maximum normalized serial length.
const MaxSerialLen = 64

// NormalizeSerial trims whitespace, lowercases, and validates that the serial
// is non-empty, at most MaxSerialLen, and contains only RFC 3986 unreserved
// characters (a-z, 0-9, "-._~"). Invalid characters are rejected rather than
// stripped.
func NormalizeSerial(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", fmt.Errorf("serial is empty after trimming")
	}
	if len(s) > MaxSerialLen {
		return "", fmt.Errorf("serial length %d exceeds maximum %d", len(s), MaxSerialLen)
	}
	for _, r := range s {
		if !isUnreservedSerialRune(r) {
			return "", fmt.Errorf("serial contains invalid character %q (allowed: RFC 3986 unreserved)", r)
		}
	}
	return s, nil
}

func isUnreservedSerialRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '.' || r == '_' || r == '~'
}
