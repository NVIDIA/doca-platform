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

package mirror

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

const testBOM = `schemaVersion: v1alpha1
images:
  - name: dpf-system
    scope: [dpu, host]
`

// writeChartDir writes a dpf-operator chart directory with the given files and returns it.
func writeChartDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "dpf-operator")
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		NewWithT(t).Expect(os.MkdirAll(filepath.Dir(p), 0o700)).To(Succeed())
		NewWithT(t).Expect(os.WriteFile(p, []byte(content), 0o600)).To(Succeed())
	}
	return dir
}

// writeChartArchive writes the files as a chart archive, below the dpf-operator directory as helm
// package does, and returns it.
func writeChartArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	g := NewWithT(t)
	path := filepath.Join(t.TempDir(), "dpf-operator-v26.10.0.tgz")
	f, err := os.Create(path)
	g.Expect(err).NotTo(HaveOccurred())
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		g.Expect(tw.WriteHeader(&tar.Header{Name: "dpf-operator/" + name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg})).To(Succeed())
		_, err := tw.Write([]byte(content))
		g.Expect(err).NotTo(HaveOccurred())
	}
	g.Expect(tw.Close()).To(Succeed())
	g.Expect(gz.Close()).To(Succeed())
	g.Expect(f.Close()).To(Succeed())
	return path
}

func TestReleaseFromChart(t *testing.T) {
	released := map[string]string{
		"Chart.yaml":  "name: dpf-operator\nversion: v26.10.0\nappVersion: v26.10.0\n",
		"values.yaml": "controllerManager:\n  image:\n    repository: nvcr.io/nvidia/doca/dpf-system\n    tag: \"\"\n",
		BOMPath:       testBOM,
		// A subchart's BOM is not the chart's.
		"charts/sub/" + BOMPath: "not: a bom\n",
	}
	unreleased := map[string]string{
		"Chart.yaml":  "name: dpf-operator\nversion: v0.1.0\nappVersion: v0.1.0\n",
		"values.yaml": "controllerManager:\n  image:\n    repository: \"\"\n",
		BOMPath:       testBOM,
	}
	tests := []struct {
		name         string
		chart        func(*testing.T) string
		wantRegistry string
		wantVersion  string
		wantErr      string
	}{
		{name: "archive of a release", chart: func(t *testing.T) string { return writeChartArchive(t, released) },
			wantRegistry: "nvcr.io/nvidia/doca", wantVersion: "v26.10.0"},
		{name: "directory of a release", chart: func(t *testing.T) string { return writeChartDir(t, released) },
			wantRegistry: "nvcr.io/nvidia/doca", wantVersion: "v26.10.0"},
		{name: "chart of the repository", chart: func(t *testing.T) string { return writeChartDir(t, unreleased) }},
		{name: "no BOM", chart: func(t *testing.T) string {
			return writeChartArchive(t, map[string]string{"Chart.yaml": "name: dpf-operator\n", "values.yaml": ""})
		}, wantErr: "has no air-gap BOM"},
		{name: "not a chart", chart: func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "x.tgz")
			NewWithT(t).Expect(os.WriteFile(p, []byte("text"), 0o600)).To(Succeed())
			return p
		}, wantErr: "neither a directory nor a chart archive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			rel, err := ReleaseFromChart(tt.chart(t))
			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rel.Registry).To(Equal(tt.wantRegistry))
			g.Expect(rel.Version).To(Equal(tt.wantVersion))
			g.Expect(rel.BOM.Images).To(HaveLen(1))
		})
	}
}

func TestReleaseFromChartOfTheRepository(t *testing.T) {
	g := NewWithT(t)
	rel, err := ReleaseFromChart("../../../deploy/charts/dpf-operator")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rel.BOM.Images).NotTo(BeEmpty())
	g.Expect(rel.Registry).To(BeEmpty())
}

func TestParseChartURL(t *testing.T) {
	tests := []struct {
		in       string
		wantRepo string
		wantName string
		wantErr  bool
	}{
		{in: "https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator", wantRepo: "https://helm.ngc.nvidia.com/nvidia/doca", wantName: "dpf-operator"},
		{in: "oci://ghcr.io/org/charts/dpf-operator/", wantRepo: "oci://ghcr.io/org/charts", wantName: "dpf-operator"},
		{in: "https://charts.example.com", wantErr: true},
		{in: "dpf-repository/dpf-operator", wantErr: true},
		{in: "file:///charts/dpf-operator", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			g := NewWithT(t)
			repo, name, err := ParseChartURL(tt.in)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(repo).To(Equal(tt.wantRepo))
			g.Expect(name).To(Equal(tt.wantName))
		})
	}
}
