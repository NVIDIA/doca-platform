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

// Package identity_test holds cross-package invariants between the spire builders and
// the identity policy package. It lives in an external test package so it can import
// spire (which imports identity) without forming an import cycle.
package identity_test

import (
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/internal/spire"
	"github.com/nvidia/doca-platform/internal/spire/identity"
)

// TestSerialPolicyInvariant asserts the spire SPIFFE-path builders and the identity
// package share a single serial policy. For every serial, the spire builder must accept
// exactly when identity.NormalizeSerial accepts, and the embedded segment must equal the
// normalized serial. This guards against re-introducing a divergent serial validator.
func TestSerialPolicyInvariant(t *testing.T) {
	serials := []string{
		"MT2440600YYW",
		"  mt-24.40_0~1  ",
		"",
		"   ",
		"MT:24/40",
		"mt 2440",
		strings.Repeat("a", 64),
		strings.Repeat("a", 65),
	}
	for _, s := range serials {
		norm, normErr := identity.NormalizeSerial(s)
		path, pathErr := spire.DPUAgentSpiffePath(s)
		if (normErr == nil) != (pathErr == nil) {
			t.Fatalf("serial %q: identity.NormalizeSerial err=%v but spire.DPUAgentSpiffePath err=%v (serial policies diverged)", s, normErr, pathErr)
		}
		if normErr != nil {
			continue
		}
		if want := "/dpu/" + norm + "/process/dpu-agent"; path != want {
			t.Fatalf("serial %q: spire path %q does not embed normalized serial; want %q", s, path, want)
		}
	}
}

// TestTrustDomainPolicyInvariant asserts SpireWorkloadID validates the trust domain via
// identity.ValidateTrustDomain: the builder accepts exactly when the policy accepts.
func TestTrustDomainPolicyInvariant(t *testing.T) {
	const serial = "MT2440600YYW"
	for _, td := range []string{"cs.internal", "", " ", "cs.internal/extra"} {
		_, tdErr := identity.ValidateTrustDomain(td)
		_, widErr := spire.SpireWorkloadID(td, serial)
		if (tdErr == nil) != (widErr == nil) {
			t.Fatalf("trust domain %q: identity.ValidateTrustDomain err=%v but spire.SpireWorkloadID err=%v (trust-domain policies diverged)", td, tdErr, widErr)
		}
	}
}
