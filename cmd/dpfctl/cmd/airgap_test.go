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

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
	"github.com/nvidia/doca-platform/internal/airgap/mirror"

	. "github.com/onsi/gomega"
)

const airgapTestBOM = "../../../deploy/charts/dpf-operator/airgap/airgapped.yaml"

func TestAirgapOptionsValidate(t *testing.T) {
	valid := airgapOptions{bomFile: "bom.yaml", dpfRegistry: "nvcr.io/nvidia/doca", dpfVersion: "v26.10.0", layout: "flat", output: airgapOutputPairs}
	tests := []struct {
		name    string
		modify  func(o *airgapOptions)
		wantErr string
	}{
		{name: "valid", modify: func(*airgapOptions) {}},
		{name: "neither chart nor bom", modify: func(o *airgapOptions) { o.bomFile = "" }, wantErr: "pass the dpf-operator chart"},
		{name: "chart and bom", modify: func(o *airgapOptions) { o.chart = "x" }, wantErr: "mutually exclusive"},
		{name: "bom without registry", modify: func(o *airgapOptions) { o.dpfRegistry = "" }, wantErr: "--bom needs --dpf-registry and --dpf-version"},
		{name: "invalid scope", modify: func(o *airgapOptions) { o.scopes = []string{"dpu", "cloud"} }, wantErr: `invalid --scope "cloud"`},
		{name: "service and all services", modify: func(o *airgapOptions) {
			o.services = []string{"dpf-storage"}
			o.allServices = true
		}, wantErr: "--service and --all-services are mutually exclusive"},
		{name: "stdin once", modify: func(o *airgapOptions) { o.files = []string{"-", "dir"} }},
		{name: "stdin twice", modify: func(o *airgapOptions) { o.files = []string{"-", "dir", "-"} }, wantErr: "-f - is given twice"},
		{name: "invalid output", modify: func(o *airgapOptions) { o.output = "yaml" }, wantErr: `invalid --output "yaml"`},
		{name: "nested layout", modify: func(o *airgapOptions) { o.layout = "nested" }},
		{name: "invalid layout", modify: func(o *airgapOptions) { o.layout = "tree" }, wantErr: `invalid --layout "tree"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			o := valid
			tt.modify(&o)
			err := o.validate()
			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
				return
			}
			g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
		})
	}
}

func noRenderer() (*bom.HelmRenderer, error) {
	return nil, errors.New("no renderer in this test")
}

func TestAirgapRelease(t *testing.T) {
	g := NewWithT(t)
	logf := func(string, ...any) {}

	o := &airgapOptions{bomFile: airgapTestBOM, dpfRegistry: "nvcr.io/nvidia/doca", dpfVersion: "v26.10.0", dpfChartRepo: "https://charts.example.com"}
	rel, err := o.release(context.Background(), noRenderer, logf)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rel.BOM.Images).NotTo(BeEmpty())
	g.Expect(rel.Registry).To(Equal("nvcr.io/nvidia/doca"))
	g.Expect(rel.Version).To(Equal("v26.10.0"))
	g.Expect(rel.ChartRepo).To(Equal("https://charts.example.com"))

	// The chart of the repository does not pin the operator image.
	o = &airgapOptions{chart: "../../../deploy/charts/dpf-operator"}
	_, err = o.release(context.Background(), noRenderer, logf)
	g.Expect(err).To(MatchError(ContainSubstring("does not pin the DPF operator image: pass --dpf-registry and --dpf-version")))
	o.dpfRegistry, o.dpfVersion = "nvcr.io/nvidia/doca", "v26.10.0"
	rel, err = o.release(context.Background(), noRenderer, logf)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rel.Registry).To(Equal("nvcr.io/nvidia/doca"))

	// A chart of a repository needs a version, and is downloaded.
	o = &airgapOptions{chart: "https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator"}
	_, err = o.release(context.Background(), noRenderer, logf)
	g.Expect(err).To(MatchError(ContainSubstring("--version is required")))
	o.chartVersion = "v26.10.0"
	_, err = o.release(context.Background(), noRenderer, logf)
	g.Expect(err).To(MatchError(ContainSubstring("no renderer in this test")))
}

// fakeRenderer renders a Deployment using the image of the "image" value.
type fakeRenderer struct{}

func (fakeRenderer) Render(_ context.Context, req bom.RenderRequest) ([]byte, error) {
	return fmt.Appendf(nil, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
spec:
  template:
    spec:
      containers:
        - name: c
          image: %v
`, req.Chart.Name, req.Values["image"]), nil
}

const airgapTestService = `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: my-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: oci://registry.example.com/charts
      chart: my-service
      version: 1.0.0
    values:
      image: registry.example.com/my-service:1.0.0
`

func TestAirgapRenderServices(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(dir, "svc.yaml"), []byte(airgapTestService), 0o600)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, "other.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"), 0o600)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("# not a manifest\n"), 0o600)).To(Succeed())
	other := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(other, "svc.tmpl"), []byte(airgapTestService), 0o600)).To(Succeed())
	g.Expect(os.Symlink(dir, filepath.Join(other, "link"))).To(Succeed())
	want := []bom.Service{{
		Chart:  bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "my-service", Version: "1.0.0"},
		Images: []bom.Image{{Ref: "registry.example.com/my-service:1.0.0", Scope: []bom.Scope{bom.ScopeDPU}}},
	}}
	logf := func(string, ...any) {}

	tests := []struct {
		name  string
		path  string
		stdin string
	}{
		{name: "directory", path: dir},
		{name: "standard input", path: "-", stdin: airgapTestService},
		{name: "file without a manifest extension", path: filepath.Join(other, "svc.tmpl")},
		{name: "symbolic link to a directory", path: filepath.Join(other, "link")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			services, err := renderServices(context.Background(), fakeRenderer{}, strings.NewReader(tt.stdin), []string{tt.path}, logf)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(services).To(Equal(want))
		})
	}

	_, err := renderServices(context.Background(), fakeRenderer{}, strings.NewReader(""), []string{filepath.Join(dir, "other.yaml")}, logf)
	g.Expect(err).To(MatchError(ContainSubstring("no DPUService or DPUServiceTemplate found")))
}

func TestAirgapImagesCommand(t *testing.T) {
	g := NewWithT(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"airgap", "images", "--bom", airgapTestBOM, "--dpf-registry", "nvcr.io/nvidia/doca",
		"--dpf-version", "v26.10.0", "--mirror", "mirror.example.com:5000", "--scope", "dpu"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		airgapImagesOpts = airgapOptions{}
	})
	g.Expect(rootCmd.Execute()).To(Succeed())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	g.Expect(lines).To(ContainElements(
		"nvcr.io/nvidia/doca/dpf-system:v26.10.0 mirror.example.com:5000/dpf-system:v26.10.0",
		"registry.k8s.io/pause:3.9 mirror.example.com:5000/pause:3.9",
	))
	for _, l := range lines {
		g.Expect(strings.Fields(l)).To(HaveLen(2), l)
		g.Expect(l).NotTo(ContainSubstring("kube-apiserver"), "host-only image listed for --scope dpu")
	}
}

func TestPrintChartCopies(t *testing.T) {
	g := NewWithT(t)
	copies := []mirror.ChartCopy{{RepoURL: "https://r", Name: "n", Version: "1", Destination: "oci://m/r", Origins: []string{"bom"}}}
	var out bytes.Buffer
	g.Expect(printChartCopies(&out, airgapOutputPairs, copies)).To(Succeed())
	g.Expect(out.String()).To(Equal("https://r n 1 oci://m/r\n"))
	out.Reset()
	g.Expect(printChartCopies(&out, airgapOutputJSON, copies)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring(`"destination": "oci://m/r"`))
}
