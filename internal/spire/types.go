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
// Serial-handling contract: the builders reject serials that contain characters
// outside their target charset rather than stripping them. Stripping is a many-to-one
// mapping that can collapse two distinct DPU serials into one identifier -- an identity
// collision. Since DPU.spec.serialNumber is only length-validated at the API (no charset
// constraint), the builders fail closed: an out-of-charset serial yields an error and
// therefore no SPIFFE identity, which the DPUDevice controller surfaces as
// SPIFFEEntryReady=False / SerialNumberInvalid.
//
// SPIFFE-path serial validation and trust-domain validation are delegated to the
// identity package: identity.NormalizeSerial is the single serial policy (RFC 3986
// unreserved charset + identity.MaxSerialLen cap) shared with the SPIRE NodeAttestor
// plugin, and identity.ValidateTrustDomain is the single trust-domain policy.
package spire

import (
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/spire/identity"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

const dpuAgentClusterStaticEntryPrefix = "dpu-agent-"

// maxClusterStaticEntrySerialLen is the maximum serial length that yields a valid
// Kubernetes metadata.name after prepending dpu-agent- (253 - len(prefix)).
const maxClusterStaticEntrySerialLen = k8svalidation.DNS1123SubdomainMaxLength - len(dpuAgentClusterStaticEntryPrefix)

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

// DPUAgentSpiffePath returns the on-DPU SPIFFE path for the DPU Agent workload:
// /dpu/<segment>/process/dpu-agent. The serial is validated via identity.NormalizeSerial,
// so serials longer than identity.MaxSerialLen are rejected.
//
// NKE caveat: this path shape is stable pending the NKE round-trip on the
// workload-ID layout. If the layout changes, DPUAgentSpiffePath and SpireWorkloadID
// must change together (and their golden tests with them).
func DPUAgentSpiffePath(serial string) (string, error) {
	segment, err := identity.NormalizeSerial(serial)
	if err != nil {
		return "", fmt.Errorf("building SPIFFE path for serial %q: %w", serial, err)
	}
	return fmt.Sprintf("/dpu/%s/process/dpu-agent", segment), nil
}

// SpireWorkloadID returns the literal DPU Agent workload SPIFFE ID:
// spiffe://<spireTd>/dpu/<segment>/process/dpu-agent. This is the value used as the
// RBAC subject for the DPU Agent in SPIFFE identity mode. The trust domain is validated
// via identity.ValidateTrustDomain and the serial via identity.NormalizeSerial.
//
// NKE caveat: see DPUAgentSpiffePath.
func SpireWorkloadID(spireTd, serial string) (string, error) {
	workloadID, _, err := SpireDPUAgentIDs(spireTd, serial)
	return workloadID, err
}

// SpireDPUAgentIDs returns both the DPU Agent workload SPIFFE ID and its parent (agent) SPIFFE ID,
// validating the trust domain and serial exactly once. See SpireWorkloadID for the workload-ID
// layout and the NKE caveat.
func SpireDPUAgentIDs(spireTd, serial string) (workloadID, parentID string, err error) {
	td, err := identity.ValidateTrustDomain(spireTd)
	if err != nil {
		return "", "", fmt.Errorf("building SPIFFE IDs for trust domain %q: %w", spireTd, err)
	}
	segment, err := identity.NormalizeSerial(serial)
	if err != nil {
		return "", "", fmt.Errorf("building SPIFFE IDs for serial %q: %w", serial, err)
	}
	return fmt.Sprintf("spiffe://%s/dpu/%s/process/dpu-agent", td, segment), identity.MakeAgentID(td, segment), nil
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
