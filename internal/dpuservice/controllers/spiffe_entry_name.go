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

package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// dpuServiceClusterStaticEntryPrefix differs from the DPU Agent prefix so the two entry names
// can never collide for the same DPU.
const dpuServiceClusterStaticEntryPrefix = "dpu-service-"

// dpuServiceClusterStaticEntryDigestLen is how much of the digest the name carries. 8 hex chars
// keep the name readable while making a collision between two real DPUServices implausible.
const dpuServiceClusterStaticEntryDigestLen = 8

// dpuServiceClusterStaticEntryName returns the metadata.name for a per-DPU SPIRE
// ClusterStaticEntry: dpu-service-<namespace>-<dpuServiceName>-<lowercase serial>-<digest>. The
// serial is part of the name because ClusterStaticEntry carries a single parentID, so a
// DPUService targeting N DPUs needs N entries.
//
// It is keyed on namespace and object name rather than the service ID: the ID is user-settable
// and not unique across namespaces, so two DPUServices would share one entry and overwrite each
// other's selectors. Namespace and name are also already DNS-1123 by construction.
//
// The digest is what makes the name unique. "-" is legal inside all three parts, so joining on it
// is ambiguous: namespace "tenant" with name "a-svc" and namespace "tenant-a" with name "svc"
// produce the same string, and ClusterStaticEntry is cluster scoped, so one would silently
// overwrite the other. The digest is taken over the parts joined by NUL, which none of them can
// contain.
func dpuServiceClusterStaticEntryName(namespace, dpuServiceName, serial string) (string, error) {
	parts := make([]string, 0, 3)
	for _, part := range []struct{ kind, value string }{
		{"namespace", namespace},
		{"DPUService name", dpuServiceName},
		{"serial", serial},
	} {
		normalized := strings.ToLower(strings.TrimSpace(part.value))
		if errs := k8svalidation.IsDNS1123Subdomain(normalized); len(errs) > 0 {
			return "", fmt.Errorf("%s %q is not a valid DNS-1123 subdomain: %s", part.kind, part.value, strings.Join(errs, "; "))
		}
		parts = append(parts, normalized)
	}

	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	full := dpuServiceClusterStaticEntryPrefix + strings.Join(parts, "-") +
		"-" + hex.EncodeToString(digest[:])[:dpuServiceClusterStaticEntryDigestLen]
	if len(full) > k8svalidation.DNS1123SubdomainMaxLength {
		return "", fmt.Errorf("cluster static entry name for DPUService %q/%q and serial %q is %d chars, exceeding the %d char maximum",
			namespace, dpuServiceName, serial, len(full), k8svalidation.DNS1123SubdomainMaxLength)
	}
	return full, nil
}
