/*
Copyright 2024 NVIDIA

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

package release

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func TestDefaults_Parse(t *testing.T) {
	g := NewGomegaWithT(t)

	// Load the generated defaults file for testing
	defaultsFilePath := filepath.Join("manifests", "defaults.yaml")
	generatedDefaults, err := os.ReadFile(defaultsFilePath)
	if err != nil {
		t.Fatalf("Failed to read defaults file (run 'make generate-manifests-release-defaults'): %v", err)
	}

	defaultValues := map[string]string{
		"dmsImage":                   "example.com/hostdriver:v0.1.0",
		"dpfSystemImage":             "example.com/dpf-system:v0.1.0",
		"ovsCniImage":                "example.com/ovs-cni-plugin:v0.1.0",
		"bfbRegistryImage":           "example.com/bfb-registry:v0.1.0",
		"dpfStorageSystemImage":      "example.com/storage-system:v0.1.0",
		"dpfStorageHostImage":        "example.com/storage-host:v0.1.0",
		"cniInstallerImage":          "example.com/dpf-cni-installer:v0.1.0",
		"keepalivedImage":            "example.com/dpf-keepalived:v0.1.0",
		"nodeSRIOVDevicePluginImage": "example.com/node-sriov-device-plugin:v0.1.0",
		"dpuNetworkingHelmChart":     "oci://example.com/dpu-networking:v0.1.0",
	}
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{
			name:    "succeed on the generated yaml",
			content: generatedDefaults,
			wantErr: false,
		},
		{
			name:    "fail when dmsImage empty/missing",
			content: withoutValue(g, defaultValues, "dmsImage"),
			wantErr: true,
		},
		{
			name:    "fail when dpfSystemImage empty/missing",
			content: withoutValue(g, defaultValues, "dpfSystemImage"),
			wantErr: true,
		},
		{
			name:    "fail when cniInstallerImage empty/missing",
			content: withoutValue(g, defaultValues, "cniInstallerImage"),
			wantErr: true,
		},
		{
			name:    "fail when dpuNetworkingHelmChart empty/missing",
			content: withoutValue(g, defaultValues, "dpuNetworkingHelmChart"),
			wantErr: true,
		},
		{
			name:    "fail when ovsCniImage empty/missing",
			content: withoutValue(g, defaultValues, "ovsCniImage"),
			wantErr: true,
		},
		{
			name:    "fail when keepalivedImage empty/missing",
			content: withoutValue(g, defaultValues, "keepalivedImage"),
			wantErr: true,
		},
		{
			name:    "fail when nodeSRIOVDevicePluginImage empty/missing",
			content: withoutValue(g, defaultValues, "nodeSRIOVDevicePluginImage"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaultsContentForTesting(tt.content)
			err := NewDefaults().Parse()
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
		})
	}
}

func withoutValue(g Gomega, defaults map[string]string, valueToRemove string) []byte {
	copied := map[string]string{}
	for k, v := range defaults {
		copied[k] = v
	}
	copied[valueToRemove] = ""
	output, err := yaml.Marshal(copied)
	g.Expect(err).ShouldNot(HaveOccurred())
	return output
}
