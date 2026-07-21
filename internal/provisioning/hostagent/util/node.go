/*
Copyright 2025 NVIDIA

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
	"fmt"
	"os"
	"strings"
)

const (
	DPFNamespace          = "dpf-operator-system"
	MaximumHostNameLength = 48
	NodeNameEnv           = "NODE_NAME"
	K8sNodeNameEnv        = "KUBERNETES_NODE_NAME"
	K8sPodNamespaceEnv    = "POD_NAMESPACE"

	// CATrustBundleDir is the directory where the dpf CA trust bundle ConfigMap is mounted into the
	// HostAgent (DMS) Pod. It is a non-subPath volume so kubelet keeps it in sync as the bundle is
	// updated during CA rotation.
	CATrustBundleDir = "/etc/dpf/ca"
	// CATrustBundleFile is the full path to the CA trust bundle PEM inside the HostAgent Pod. The
	// file name matches the ConfigMap data key (operatorv1.CATrustBundleKey = "ca.crt").
	CATrustBundleFile = CATrustBundleDir + "/ca.crt"
)

// GetNodeName returns the name of the DPUNode.
// If the hostname is longer than MaximumHostNameLength, it is truncated and true is returned.
func GetNodeName() (string, bool, error) {
	var nodeName string
	if name := os.Getenv(NodeNameEnv); name != "" {
		nodeName = name
	} else {
		name, err := os.Hostname()
		if err != nil {
			return "", false, fmt.Errorf("failed to get hostname: %w", err)
		}
		nodeName = name
	}
	nodeName = strings.ToLower(nodeName)
	nodeName = strings.TrimSpace(nodeName)
	if len(nodeName) <= MaximumHostNameLength {
		return nodeName, false, nil
	}
	truncated := nodeName[:MaximumHostNameLength]
	return truncated, true, nil
}
