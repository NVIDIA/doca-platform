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
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func TestDefaults_Parse(t *testing.T) {
	g := NewGomegaWithT(t)

	defaultValues := map[string]string{
		"customOVNKubernetesDPUImage":    "example.com/cloud-orchestration-dev/dpf/killian/test/ovn-kubernetes-dpu:v0.0.0",
		"customOVNKubernetesNonDPUImage": "example.com/cloud-orchestration-dev/dpf/killian/test/ovn-kubernetes-non-dpu:v0.0.0",
		"dmsImage":                       "example.com/cloud-orchestration-dev/dpf/killian/test/hostdriver:v0.0.0",
		"hostnetworksetupImage":          "example.com/cloud-orchestration-dev/dpf/killian/test/hostnetworksetup:v0.0.0",
		"dpfSystemImage":                 "example.com/cloud-orchestration-dev/dpf/killian/test/dpfSystem:v0.0.0",
		"dpuNetworkingHelmChart":         "example.com/cloud-orchestration-dev/dpf/killian/test/hostnetworksetup:v0.0.0",
	}
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{
			name:    "succeed on the generated yaml",
			content: defaultsContent,
			wantErr: false,
		},
		{
			name:    "fail when customOVNKubernetesDPUImage empty/missing",
			content: withoutValue(g, defaultValues, "customOVNKubernetesDPUImage"),
			wantErr: true,
		},
		{
			name:    "fail when customOVNKubernetesNonDPUImage empty/missing",
			content: withoutValue(g, defaultValues, "customOVNKubernetesNonDPUImage"),
			wantErr: true,
		},
		{
			name:    "fail when dmsImage empty/missing",
			content: withoutValue(g, defaultValues, "dmsImage"),
			wantErr: true,
		},
		{
			name:    "fail when hostnetworksetupImage empty/missing",
			content: withoutValue(g, defaultValues, "hostnetworksetupImage"),
			wantErr: true,
		},
		{
			name:    "fail when dpuNetworkingHelmChart empty/missing",
			content: withoutValue(g, defaultValues, "dpuNetworkingHelmChart"),
			wantErr: true,
		},
		{
			name:    "fail when dpfSystemImage is empty/missing",
			content: withoutValue(g, defaultValues, "dpfSystemImage"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultsContent = tt.content
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
