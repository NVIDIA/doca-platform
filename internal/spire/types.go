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

// Package spire holds the SPIFFE/SPIRE identifier builders shared across the DPF
// provisioning controllers. The builders are the single source of truth for the
// shapes of the DPU Agent's SPIFFE workload ID, its SPIRE ClusterStaticEntry name,
// and the on-DPU SPIFFE path.
//
// Serial-handling contract (Decision S = reject, not filter): the builders reject
// serials that contain characters outside their target charset rather than stripping
// them. Stripping is a many-to-one mapping that can collapse two distinct DPU serials
// into one identifier -- an identity collision. Since DPU.spec.serialNumber is only
// length-validated at the API (no charset constraint), the builders fail closed: an
// out-of-charset serial yields an error and therefore no SPIFFE identity, which the
// DPUDevice controller surfaces as SPIFFEEntryReady=False / SerialNumberInvalid.
package spire

import (
	"fmt"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

const dpuAgentClusterStaticEntryPrefix = "dpu-agent-"

// maxClusterStaticEntrySerialLen is the maximum serial length that yields a valid
// Kubernetes metadata.name after prepending dpu-agent- (253 - len(prefix)).
const maxClusterStaticEntrySerialLen = k8svalidation.DNS1123SubdomainMaxLength - len(dpuAgentClusterStaticEntryPrefix)

// spiffePathSegment lowercases serial and validates that every character is a
// member of the RFC 3986 "unreserved" set (a-z, 0-9, and "-._~"). It rejects
// (returns an error for) an empty serial or any out-of-charset character rather
// than filtering, to avoid collapsing distinct serials into the same SPIFFE path.
func spiffePathSegment(serial string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(serial))
	if trimmed == "" {
		return "", fmt.Errorf("serial is empty after trimming")
	}
	for _, r := range trimmed {
		isUnreserved := (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '.' || r == '_' || r == '~'
		if !isUnreserved {
			return "", fmt.Errorf("serial %q contains character %q outside the RFC 3986 unreserved set", serial, r)
		}
	}
	return trimmed, nil
}

// k8sName lowercases serial and validates that the result is a DNS-1123 subdomain
// suitable for use as a Kubernetes metadata.name component. It rejects (returns an
// error for) pathological inputs (empty, out-of-charset, leading/trailing hyphen,
// over-length) rather than stripping, for the same anti-collision reason as
// spiffePathSegment.
func k8sName(serial string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(serial))
	if errs := k8svalidation.IsDNS1123Subdomain(trimmed); len(errs) > 0 {
		return "", fmt.Errorf("serial %q is not a valid DNS-1123 subdomain: %s", serial, strings.Join(errs, "; "))
	}
	return trimmed, nil
}

// spireTrustDomain validates a SPIRE trust domain for interpolation into a SPIFFE ID
// authority segment. It rejects empty/whitespace-only values and characters (such as '/'
// or spaces) that would produce a malformed or semantically shifted SPIFFE URI.
func spireTrustDomain(td string) (string, error) {
	trimmed := strings.TrimSpace(td)
	if trimmed == "" {
		return "", fmt.Errorf("trust domain is empty after trimming")
	}
	if errs := k8svalidation.IsDNS1123Subdomain(trimmed); len(errs) > 0 {
		return "", fmt.Errorf("trust domain %q is not a valid DNS-1123 subdomain: %s", td, strings.Join(errs, "; "))
	}
	return trimmed, nil
}

// DPUAgentSpiffePath returns the on-DPU SPIFFE path for the DPU Agent workload:
// /dpu/<segment>/process/dpu-agent.
//
// NKE caveat: this path shape is stable pending the NKE round-trip on the
// workload-ID layout. If the layout changes, DPUAgentSpiffePath and SpireWorkloadID
// must change together (and their golden tests with them).
func DPUAgentSpiffePath(serial string) (string, error) {
	segment, err := spiffePathSegment(serial)
	if err != nil {
		return "", fmt.Errorf("building SPIFFE path for serial %q: %w", serial, err)
	}
	return fmt.Sprintf("/dpu/%s/process/dpu-agent", segment), nil
}

// SpireWorkloadID returns the literal DPU Agent workload SPIFFE ID:
// spiffe://<spireTd>/dpu/<segment>/process/dpu-agent. This is the value used as the
// RBAC subject for the DPU Agent in SPIFFE identity mode.
//
// NKE caveat: see DPUAgentSpiffePath.
func SpireWorkloadID(spireTd, serial string) (string, error) {
	td, err := spireTrustDomain(spireTd)
	if err != nil {
		return "", fmt.Errorf("building SPIFFE workload ID for trust domain %q: %w", spireTd, err)
	}
	segment, err := spiffePathSegment(serial)
	if err != nil {
		return "", fmt.Errorf("building SPIFFE workload ID for serial %q: %w", serial, err)
	}
	return fmt.Sprintf("spiffe://%s/dpu/%s/process/dpu-agent", td, segment), nil
}

// DPUAgentClusterStaticEntryName returns the metadata.name for the per-DPU SPIRE
// ClusterStaticEntry: dpu-agent-<k8sName(serial)>.
func DPUAgentClusterStaticEntryName(serial string) (string, error) {
	name, err := k8sName(serial)
	if err != nil {
		return "", fmt.Errorf("building cluster static entry name for serial %q: %w", serial, err)
	}
	if len(name) > maxClusterStaticEntrySerialLen {
		return "", fmt.Errorf("serial %q is too long for cluster static entry name (max %d chars after %q prefix)", serial, maxClusterStaticEntrySerialLen, dpuAgentClusterStaticEntryPrefix)
	}
	return dpuAgentClusterStaticEntryPrefix + name, nil
}
