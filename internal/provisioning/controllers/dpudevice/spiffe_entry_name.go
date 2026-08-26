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

package dpudevice

import (
	"fmt"
	"strings"

	spireidentity "github.com/nvidia/doca-platform/internal/spire/identity"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

const dpuAgentClusterStaticEntryPrefix = "dpu-agent-"

// maxClusterStaticEntrySerialLen matches the DPU identity serial limit. The resulting
// prefixed name is also well within the Kubernetes DNS-1123 subdomain length limit.
const maxClusterStaticEntrySerialLen = spireidentity.MaxSerialLen

// dpuAgentClusterStaticEntryName returns the metadata.name for the per-DPU SPIRE
// ClusterStaticEntry: dpu-agent-<lowercase serial>.
func dpuAgentClusterStaticEntryName(serial string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(serial))
	if errs := k8svalidation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return "", fmt.Errorf("serial %q is not a valid DNS-1123 subdomain: %s", serial, strings.Join(errs, "; "))
	}
	if len(name) > maxClusterStaticEntrySerialLen {
		return "", fmt.Errorf("serial %q is too long for DPU Agent identity (max %d chars)", serial, maxClusterStaticEntrySerialLen)
	}
	return dpuAgentClusterStaticEntryPrefix + name, nil
}
