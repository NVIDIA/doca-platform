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
	"os"
	"path/filepath"
	"slices"
	"testing"

	. "github.com/onsi/gomega"
)

func loadTestBOM(t *testing.T) *BOM {
	t.Helper()
	b, err := Load("testdata/bom.yaml")
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return b
}

func TestLoadMarshalRoundTrip(t *testing.T) {
	g := NewWithT(t)
	want, err := os.ReadFile("testdata/bom.yaml")
	g.Expect(err).NotTo(HaveOccurred())

	b := loadTestBOM(t)
	g.Expect(b.Images).To(HaveLen(5))
	g.Expect(b.Services).To(HaveLen(2))

	got, err := b.Marshal(Header)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(got)).To(Equal(string(want)))

	path := filepath.Join(t.TempDir(), "bom.yaml")
	g.Expect(b.WriteFile(path, Header)).To(Succeed())
	written, err := os.ReadFile(path)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(written).To(Equal(want))
}

func TestParseRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"unknown field":         "schemaVersion: v1alpha1\nfoo: bar\n",
		"wrong version":         "schemaVersion: v2\n",
		"name and ref":          "schemaVersion: v1alpha1\nimages:\n  - name: a\n    ref: ghcr.io/a:v1@" + testDigest + "\n    scope: [dpu]\n",
		"neither name nor ref":  "schemaVersion: v1alpha1\nimages:\n  - scope: [dpu]\n",
		"invalid ref":           "schemaVersion: v1alpha1\nimages:\n  - ref: $X/a:v1\n    scope: [dpu]\n",
		"unknown chart field":   "schemaVersion: v1alpha1\ncharts:\n  - name: a\n    scope: [host]\n",
		"chart without name":    "schemaVersion: v1alpha1\ncharts:\n  - repoURL: https://r\n    version: 1.0.0\n",
		"chart without version": "schemaVersion: v1alpha1\ncharts:\n  - repoURL: https://r\n    name: a\n",
		"service chart without repoURL": "schemaVersion: v1alpha1\nservices:\n  - name: a\n    version: 1.0.0\n" +
			"    images:\n      - name: n\n        scope: [dpu]\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(data))
			NewWithT(t).Expect(err).To(HaveOccurred())
		})
	}
}

func TestNormalize(t *testing.T) {
	g := NewWithT(t)
	b := &BOM{
		Images: []Image{
			{Ref: "ghcr.io/b/b:v1", Scope: []Scope{ScopeHost}},
			{Name: "dpf-system", Scope: []Scope{ScopeHost}},
			{Ref: "ghcr.io/b/b:v1@" + testDigest, Scope: []Scope{ScopeDPU}},
			{Name: "dpf-system", Scope: []Scope{ScopeDPU, ScopeHost}},
			{Ref: "nginx:1.27", Scope: []Scope{ScopeDPU}, Sandbox: true},
			// Listed as written: folded into the pinned entry of the tag too.
			{Ref: "ghcr.io/b/b:v1", Scope: []Scope{ScopeDPU}},
		},
		Services: []Service{
			{Chart: Chart{RepoURL: "https://r", Name: "a", Version: "1"}, Images: []Image{{Name: "n", Scope: []Scope{ScopeDPU}}}},
			{Chart: Chart{Name: "z"}},
			{Chart: Chart{RepoURL: "https://r", Name: "a", Version: "1"}, Images: []Image{{Name: "n", Scope: []Scope{ScopeHost}}}},
		},
	}
	b.Normalize()
	g.Expect(b.SchemaVersion).To(Equal(SchemaVersion))
	g.Expect(b.Images).To(Equal([]Image{
		{Name: "dpf-system", Scope: []Scope{ScopeDPU, ScopeHost}},
		{Ref: "ghcr.io/b/b:v1@" + testDigest, Scope: []Scope{ScopeDPU, ScopeHost}},
		{Ref: "nginx:1.27", Scope: []Scope{ScopeDPU}, Sandbox: true},
	}))
	g.Expect(b.Services).To(HaveLen(2))
	// First-party charts first, and the services of one chart merged.
	g.Expect(b.Services[0].Chart).To(Equal(Chart{Name: "z"}))
	g.Expect(b.Services[0].Images).To(BeEmpty())
	g.Expect(b.Services[1].Chart).To(Equal(Chart{RepoURL: "https://r", Name: "a", Version: "1"}))
	g.Expect(b.Services[1].Images).To(Equal([]Image{{Name: "n", Scope: []Scope{ScopeDPU, ScopeHost}}}))
}

// TestNormalizeKeepsDigests checks that different digests of one repository or tag are kept as
// different images, and that an entry without digest is folded only into the single pinned entry of the
// same tag.
func TestNormalizeKeepsDigests(t *testing.T) {
	const otherDigest = "sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	dpu, host := []Scope{ScopeDPU}, []Scope{ScopeHost}
	tests := []struct {
		name   string
		images []Image
		want   []Image
	}{
		{
			name: "two digest-only references of one repository",
			images: []Image{
				{Ref: "nvcr.io/x@" + testDigest, Scope: dpu},
				{Ref: "nvcr.io/x@" + otherDigest, Scope: dpu},
			},
			want: []Image{
				{Ref: "nvcr.io/x@" + otherDigest, Scope: dpu},
				{Ref: "nvcr.io/x@" + testDigest, Scope: dpu},
			},
		},
		{
			name: "one tag with two digests",
			images: []Image{
				{Ref: "ghcr.io/a/b:1.0@" + testDigest, Scope: dpu},
				{Ref: "ghcr.io/a/b:1.0@" + otherDigest, Scope: host},
			},
			want: []Image{
				{Ref: "ghcr.io/a/b:1.0@" + otherDigest, Scope: host},
				{Ref: "ghcr.io/a/b:1.0@" + testDigest, Scope: dpu},
			},
		},
		{
			name: "unpinned folded into the single pinned entry of the same tag",
			images: []Image{
				{Ref: "ghcr.io/a/b:1.0", Scope: host},
				{Ref: "ghcr.io/a/b:1.0@" + testDigest, Scope: dpu},
			},
			want: []Image{{Ref: "ghcr.io/a/b:1.0@" + testDigest, Scope: []Scope{ScopeDPU, ScopeHost}}},
		},
		{
			name: "tagless unpinned folded into the pinned latest entry",
			images: []Image{
				{Ref: "ghcr.io/a/b", Scope: host},
				{Ref: "ghcr.io/a/b:latest", Scope: host},
				{Ref: "ghcr.io/a/b:latest@" + testDigest, Scope: dpu},
			},
			want: []Image{{Ref: "ghcr.io/a/b:latest@" + testDigest, Scope: []Scope{ScopeDPU, ScopeHost}}},
		},
		{
			name: "unpinned kept when the tag has several pinned entries",
			images: []Image{
				{Ref: "ghcr.io/a/b:1.0", Scope: host},
				{Ref: "ghcr.io/a/b:1.0@" + testDigest, Scope: dpu},
				{Ref: "ghcr.io/a/b:1.0@" + otherDigest, Scope: dpu},
			},
			want: []Image{
				{Ref: "ghcr.io/a/b:1.0", Scope: host},
				{Ref: "ghcr.io/a/b:1.0@" + otherDigest, Scope: dpu},
				{Ref: "ghcr.io/a/b:1.0@" + testDigest, Scope: dpu},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			b := &BOM{Images: tt.images}
			b.Normalize()
			g.Expect(b.Images).To(Equal(tt.want))
			// Normalizing duplicated entries gives the same result.
			twice := &BOM{Images: append(slices.Clone(b.Images), b.Images...)}
			twice.Normalize()
			g.Expect(twice.Images).To(Equal(tt.want))
		})
	}
}

// chartsBOM has a system chart (dpf-operator), a first-party service chart (dpf-storage), the
// third-party chart of a service (doca-hbn) and a third-party chart without service
// (doca-telemetry), with duplicates.
const chartsBOM = `schemaVersion: v1alpha1
services:
  - name: dpf-storage
    images:
      - name: storage-host
        scope: [host]
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-hbn
    version: 3.5.0
    images:
      - ref: nvcr.io/nvidia/doca/doca_hbn:3.5.0
        scope: [dpu]
charts:
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-telemetry
    version: 1.26.5
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-hbn
    version: 3.5.0
  - name: dpf-storage
  - name: dpf-operator
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-hbn
    version: 3.5.0
`

func TestCharts(t *testing.T) {
	g := NewWithT(t)
	b, err := Parse([]byte(chartsBOM))
	g.Expect(err).NotTo(HaveOccurred())

	// Marshal sorts first-party charts first, and removes duplicates.
	data, err := b.Marshal("")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(data)).To(HaveSuffix(`charts:
  - name: dpf-operator
  - name: dpf-storage
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-hbn
    version: 3.5.0
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-telemetry
    version: 1.26.5
`))
	again, err := Parse(data)
	g.Expect(err).NotTo(HaveOccurred())
	data2, err := again.Marshal("")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(data2).To(Equal(data))
	g.Expect(b.ChartNames()).To(Equal([]string{"dpf-operator", "dpf-storage"}))
}
