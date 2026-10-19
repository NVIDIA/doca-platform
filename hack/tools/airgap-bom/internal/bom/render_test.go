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

package bom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

// newTestRenderer returns a HelmRenderer, skipping the test when there is no helm binary.
func newTestRenderer(t *testing.T, opts HelmRendererOptions) *HelmRenderer {
	t.Helper()
	if _, err := findHelm(opts.Helm); err != nil {
		t.Skipf("the helm binary is needed to render the charts: %v", err)
	}
	r, err := NewHelmRenderer(opts)
	if errors.Is(err, errNotHelmV3) {
		t.Skipf("the charts are rendered with helm v3: %v", err)
	}
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// writeChart writes a chart of one pod running image into dir.
func writeChart(t *testing.T, dir, name, version, image string) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(os.MkdirAll(filepath.Join(dir, "templates"), 0o700)).To(Succeed())
	chartYAML := fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\n", name, version)
	g.Expect(os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(chartYAML), 0o600)).To(Succeed())
	pod := fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\nspec:\n  containers:\n    - name: c\n      image: %s\n", name, image)
	g.Expect(os.WriteFile(filepath.Join(dir, "templates", "pod.yaml"), []byte(pod), 0o600)).To(Succeed())
}

func renderedImages(t *testing.T, r Renderer, req RenderRequest) []string {
	t.Helper()
	g := NewWithT(t)
	data, err := r.Render(context.Background(), req)
	g.Expect(err).NotTo(HaveOccurred())
	found, err := ImagesFromManifests(data)
	g.Expect(err).NotTo(HaveOccurred())
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.Raw)
	}
	return out
}

func TestHelmRendererLocalChart(t *testing.T) {
	r := newTestRenderer(t, HelmRendererOptions{KubeVersion: "v1.35.6"})
	chart := ChartRef{Path: "testdata/chart", Name: "app"}

	t.Run("defaults, with hooks", func(t *testing.T) {
		NewWithT(t).Expect(renderedImages(t, r, RenderRequest{Chart: chart})).To(ConsistOf(
			"ghcr.io/example/app:v1.0.0",
			"docker.io/library/busybox:1.36",
		))
	})
	t.Run("values enable a subchart", func(t *testing.T) {
		NewWithT(t).Expect(renderedImages(t, r, RenderRequest{
			Chart: chart,
			Values: map[string]any{
				"image": map[string]any{"tag": "v2.0.0"},
				"sub":   map[string]any{"enabled": true, "tag": "3.0.0"},
			},
			ReleaseName: "rel",
			Namespace:   "ns",
		})).To(ConsistOf(
			"ghcr.io/example/app:v2.0.0",
			"docker.io/library/busybox:1.36",
			"quay.io/example/sub:3.0.0",
		))
	})
}

// TestHelmRendererReleaseNameIsNoFlag checks that a release name starting with -- reaches helm as
// the release name, not as a flag: helm fails with "invalid release name" instead of running the
// post-renderer the name points at.
func TestHelmRendererReleaseNameIsNoFlag(t *testing.T) {
	r := newTestRenderer(t, HelmRendererOptions{})
	req := RenderRequest{Chart: ChartRef{Path: "testdata/chart", Name: "app"}, ReleaseName: "--post-renderer=./evil.sh"}
	_, err := r.Render(context.Background(), req)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("invalid release name")))
}

func TestHelmRendererMissingDependency(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeChart(t, dir, "app", "0.1.0", "ghcr.io/example/app:v1")
	chartYAML := "apiVersion: v2\nname: app\nversion: 0.1.0\ndependencies:\n  - name: remote\n    version: 1.0.0\n    repository: http://127.0.0.1:1/charts\n"
	g.Expect(os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(chartYAML), 0o600)).To(Succeed())

	req := RenderRequest{Chart: ChartRef{Path: dir, Name: "app"}}
	_, err := newTestRenderer(t, HelmRendererOptions{}).Render(context.Background(), req)
	g.Expect(err).To(MatchError(ContainSubstring("dependency remote")))
}
