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
	"fmt"
	"strings"
)

const (
	// DPUAgentOrganization is the Kubernetes auth group encoded into DPU agent
	// client certificates via the Organization field.
	DPUAgentOrganization = "dpu-agents"
)

// DPUAgentUsername returns the Kubernetes username derived from a DPU agent's
// client certificate CN. This name is used as the RBAC identity for per-DPU
// Role/RoleBinding and must match the CN in the DPU agent's CSR.
func DPUAgentUsername(dpuName string) string {
	return fmt.Sprintf("da-%s", dpuName)
}

// DPUNameFromAgentUsername extracts the DPU name from a DPU agent certificate
// username. The username must match the format returned by DPUAgentUsername().
func DPUNameFromAgentUsername(username string) (string, bool) {
	const prefix = "da-"

	if !strings.HasPrefix(username, prefix) {
		return "", false
	}

	dpuName := strings.TrimPrefix(username, prefix)
	if dpuName == "" {
		return "", false
	}

	return dpuName, true
}
