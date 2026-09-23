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

package artifact

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// Paths of the cloud-init write_files entries the mock needs to impersonate the dpu-agent.
const (
	AgentConfPath           = "/opt/dpf/dpuagent.conf"
	BootstrapKubeconfigPath = "/var/lib/dpf/dpuagent/bootstrap-kubeconfig"
	CATrustBundlePath       = "/usr/local/share/ca-certificates/dpf-ca.crt"
)

// WriteFile is one cloud-init write_files entry.
type WriteFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type userData struct {
	WriteFiles []WriteFile `json:"write_files"`
}

// ParseWriteFiles returns the write_files entries of a cloud-init user-data document keyed by
// path. The document may start with the "#cloud-config" comment line.
func ParseWriteFiles(data []byte) (map[string]string, error) {
	doc := &userData{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("parse cloud-init user-data: %w", err)
	}
	files := make(map[string]string, len(doc.WriteFiles))
	for _, f := range doc.WriteFiles {
		if f.Path == "" {
			continue
		}
		files[f.Path] = f.Content
	}
	return files, nil
}

// AgentFiles are the per-DPU files the install artifact carries for the dpu-agent.
type AgentFiles struct {
	AgentConf           string
	BootstrapKubeconfig string
	CATrustBundle       string
}

// AgentFilesFromUserData picks the dpu-agent files out of a cloud-init user-data document.
// SPIFFE-mode DPUs carry no bootstrap kubeconfig and are rejected: the mock only implements the
// bootstrap token identity path.
func AgentFilesFromUserData(data []byte) (*AgentFiles, error) {
	files, err := ParseWriteFiles(data)
	if err != nil {
		return nil, err
	}
	out := &AgentFiles{
		AgentConf:           files[AgentConfPath],
		BootstrapKubeconfig: files[BootstrapKubeconfigPath],
		CATrustBundle:       files[CATrustBundlePath],
	}
	if out.AgentConf == "" {
		return nil, fmt.Errorf("user-data does not write %s", AgentConfPath)
	}
	if out.BootstrapKubeconfig == "" {
		return nil, fmt.Errorf("user-data does not write %s (SPIFFE-mode DPUs are not supported by mock-dpuagent)", BootstrapKubeconfigPath)
	}
	return out, nil
}
