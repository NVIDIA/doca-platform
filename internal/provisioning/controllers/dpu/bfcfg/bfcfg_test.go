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

package bfcfg

import (
	"os"
	"path/filepath"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
)

func TestGenerate(t *testing.T) {
	g := NewWithT(t)
	fileName := "custom-bfb.cfg.template"
	validTemplate := []byte("{{.KubeadmJoinCMD}}")
	dir, err := os.MkdirTemp("", "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(os.WriteFile(filepath.Join(dir, fileName), validTemplate, 0644)).To(Succeed())
	defer func() {
		g.Expect(os.RemoveAll(dir)).To(Succeed())
	}()

	tests := []struct {
		name           string
		flavor         *provisioningv1.DPUFlavor
		bfbCFGFilepath string
		dpuName        string
		joinCmd        string
		want           []byte
		wantErr        bool
	}{
		{
			name:           "test with default bf.cfg",
			flavor:         &provisioningv1.DPUFlavor{},
			bfbCFGFilepath: "",
			dpuName:        "dpu",
			joinCmd:        "kubeadm join",
			want:           nil,
			wantErr:        false,
		},
		{
			name:           "error if custom bf.cfg does not exist",
			flavor:         &provisioningv1.DPUFlavor{},
			bfbCFGFilepath: "/files/does-not-exist",
			dpuName:        "dpu",
			joinCmd:        "kubeadm join",
			want:           nil,
			wantErr:        true,
		},
		{
			name:           "generate with correctly formatted template",
			flavor:         &provisioningv1.DPUFlavor{},
			bfbCFGFilepath: filepath.Join(dir, fileName),
			dpuName:        "dpu",
			joinCmd:        "kubeadm join",
			want:           []byte("kubeadm join"),
			wantErr:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Generate(tt.flavor, "name", tt.joinCmd, false, tt.bfbCFGFilepath)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			if tt.want != nil {
				g.Expect(got).To(Equal(tt.want))
			}
		})
	}
}
