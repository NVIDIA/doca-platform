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
	"fmt"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/internal/airgap/bom"

	. "github.com/onsi/gomega"
)

const digest = "sha256:7031c1b283388d2c2e09b57badb803c05ebed362dc88d84b480cc47f72a21097"

func testRelease() Release {
	return Release{
		BOM: &bom.BOM{
			SchemaVersion: bom.SchemaVersion,
			Images: []bom.Image{
				{Name: "dpf-system", Scope: []bom.Scope{bom.ScopeDPU, bom.ScopeHost}},
				{Ref: "registry.k8s.io/kube-apiserver:v1.35.6", Scope: []bom.Scope{bom.ScopeHost}},
				{Ref: "registry.k8s.io/pause:3.9@" + digest, Scope: []bom.Scope{bom.ScopeDPU}, Sandbox: true},
			},
			Services: []bom.Service{
				{Chart: bom.Chart{Name: "dpf-storage"}, Images: []bom.Image{{Name: "storage-host", Scope: []bom.Scope{bom.ScopeHost}}}},
				{
					Chart:  bom.Chart{RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca", Name: "doca-hbn", Version: "3.5.0"},
					Images: []bom.Image{{Ref: "nvcr.io/nvidia/doca/doca_hbn:3.5.0", Scope: []bom.Scope{bom.ScopeDPU}}},
				},
			},
			Charts: []bom.Chart{
				{Name: "dpf-operator"},
				{Name: "dpf-storage"},
				{RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca", Name: "doca-hbn", Version: "3.5.0"},
				{RepoURL: "oci://ghcr.io/mellanox/charts", Name: "ovn-kubernetes-chart", Version: "v26.8.0"},
			},
		},
		Registry:  "nvcr.io/nvidia/doca",
		Version:   "v26.10.0",
		ChartRepo: "https://helm.ngc.nvidia.com/nvidia/doca",
	}
}

func sources(copies []ImageCopy) []string {
	out := make([]string, 0, len(copies))
	for _, c := range copies {
		out = append(out, c.Source)
	}
	return out
}

func destinations(copies []ImageCopy) []string {
	out := make([]string, 0, len(copies))
	for _, c := range copies {
		out = append(out, c.Destination)
	}
	return out
}

func TestImageCopies(t *testing.T) {
	extra := bom.Service{
		Chart:  bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "my-service", Version: "1.0.0"},
		Images: []bom.Image{{Ref: "registry.example.com/my-service:1.0.0", Scope: []bom.Scope{bom.ScopeDPU}}},
	}
	tests := []struct {
		name    string
		opts    Options
		want    []string
		wantErr string
	}{
		{
			name: "all scopes, no services",
			opts: Options{Mirror: "mirror.example.com:5000"},
			want: []string{
				"nvcr.io/nvidia/doca/dpf-system:v26.10.0",
				"registry.k8s.io/kube-apiserver:v1.35.6",
				"registry.k8s.io/pause@" + digest,
			},
		},
		{
			name: "dpu scope with a service of the BOM",
			opts: Options{Mirror: "mirror.example.com:5000", Scopes: []bom.Scope{bom.ScopeDPU}, Services: []string{"doca-hbn"}},
			want: []string{
				"nvcr.io/nvidia/doca/doca_hbn:3.5.0",
				"nvcr.io/nvidia/doca/dpf-system:v26.10.0",
				"registry.k8s.io/pause@" + digest,
			},
		},
		{
			name: "host scope with all services",
			opts: Options{Mirror: "mirror.example.com:5000", Scopes: []bom.Scope{bom.ScopeHost}, AllServices: true},
			want: []string{
				"nvcr.io/nvidia/doca/dpf-system:v26.10.0",
				"nvcr.io/nvidia/doca/storage-host:v26.10.0",
				"registry.k8s.io/kube-apiserver:v1.35.6",
			},
		},
		{
			name: "extra service",
			opts: Options{Mirror: "mirror.example.com:5000", Scopes: []bom.Scope{bom.ScopeDPU}, Extra: []bom.Service{extra}},
			want: []string{
				"nvcr.io/nvidia/doca/dpf-system:v26.10.0",
				"registry.example.com/my-service:1.0.0",
				"registry.k8s.io/pause@" + digest,
			},
		},
		{
			name:    "unknown service",
			opts:    Options{Mirror: "mirror.example.com:5000", Services: []string{"nope"}},
			wantErr: "unknown service nope, the services of the BOM are: dpf-storage, doca-hbn",
		},
		{
			name:    "invalid mirror",
			opts:    Options{Mirror: "https://mirror.example.com"},
			wantErr: "invalid mirror registry",
		},
		{
			name:    "no mirror",
			opts:    Options{},
			wantErr: "the mirror registry is not set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got, err := ImageCopies(testRelease(), tt.opts)
			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(sources(got)).To(Equal(tt.want))
		})
	}
}

func TestImageCopiesDetails(t *testing.T) {
	g := NewWithT(t)
	got, err := ImageCopies(testRelease(), Options{Mirror: "oci://mirror.example.com:5000/dpf/", Layout: LayoutNested, Services: []string{"doca-hbn"}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(ContainElements(
		ImageCopy{
			Reference:   "nvcr.io/nvidia/doca/dpf-system:v26.10.0",
			Source:      "nvcr.io/nvidia/doca/dpf-system:v26.10.0",
			Destination: "mirror.example.com:5000/dpf/nvcr.io/nvidia/doca/dpf-system:v26.10.0",
			Scope:       []bom.Scope{bom.ScopeDPU, bom.ScopeHost},
			Origins:     []string{OriginBOM},
		},
		ImageCopy{
			Reference:   "registry.k8s.io/pause:3.9@" + digest,
			Source:      "registry.k8s.io/pause@" + digest,
			Destination: "mirror.example.com:5000/dpf/registry.k8s.io/pause:3.9",
			Scope:       []bom.Scope{bom.ScopeDPU},
			Origins:     []string{OriginBOM},
		},
		ImageCopy{
			Reference:   "nvcr.io/nvidia/doca/doca_hbn:3.5.0",
			Source:      "nvcr.io/nvidia/doca/doca_hbn:3.5.0",
			Destination: "mirror.example.com:5000/dpf/nvcr.io/nvidia/doca/doca_hbn:3.5.0",
			Scope:       []bom.Scope{bom.ScopeDPU},
			Origins:     []string{"service https://helm.ngc.nvidia.com/nvidia/doca/doca-hbn:3.5.0"},
		},
	))
}

func TestImageCopiesFlat(t *testing.T) {
	g := NewWithT(t)
	got, err := ImageCopies(testRelease(), Options{Mirror: "mirror.example.com:5000/dpf", Services: []string{"doca-hbn"}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(destinations(got)).To(ConsistOf(
		"mirror.example.com:5000/dpf/doca_hbn:3.5.0",
		"mirror.example.com:5000/dpf/dpf-system:v26.10.0",
		"mirror.example.com:5000/dpf/kube-apiserver:v1.35.6",
		"mirror.example.com:5000/dpf/pause:3.9",
	))
}

func TestImageCopiesFlatCollision(t *testing.T) {
	g := NewWithT(t)
	extra := bom.Service{
		Chart:  bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "my-service", Version: "1.0.0"},
		Images: []bom.Image{{Ref: "registry.example.com/other/pause:3.9", Scope: []bom.Scope{bom.ScopeDPU}}},
	}
	_, err := ImageCopies(testRelease(), Options{Mirror: "mirror.example.com", Extra: []bom.Service{extra}})
	g.Expect(err).To(MatchError(ContainSubstring("would both be copied to mirror.example.com/pause:3.9; use the nested layout")))

	// The nested layout keeps them apart.
	_, err = ImageCopies(testRelease(), Options{Mirror: "mirror.example.com", Layout: LayoutNested, Extra: []bom.Service{extra}})
	g.Expect(err).NotTo(HaveOccurred())

	// Two tags of one repository share a flat repository.
	extra.Images = []bom.Image{{Ref: "registry.k8s.io/pause:3.10", Scope: []bom.Scope{bom.ScopeDPU}}}
	_, err = ImageCopies(testRelease(), Options{Mirror: "mirror.example.com", Extra: []bom.Service{extra}})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = ImageCopies(testRelease(), Options{Mirror: "mirror.example.com", Layout: "tree"})
	g.Expect(err).To(MatchError(ContainSubstring(`invalid mirror layout "tree"`)))
}

func TestImageCopiesMergesDuplicates(t *testing.T) {
	g := NewWithT(t)
	rel := testRelease()
	extra := bom.Service{
		Chart:  bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "my-service", Version: "1.0.0"},
		Images: []bom.Image{{Ref: "nvcr.io/nvidia/doca/dpf-system:v26.10.0", Scope: []bom.Scope{bom.ScopeDPU}}},
	}
	got, err := ImageCopies(rel, Options{Mirror: "mirror.example.com", Extra: []bom.Service{extra}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got[0].Reference).To(Equal("nvcr.io/nvidia/doca/dpf-system:v26.10.0"))
	g.Expect(got[0].Origins).To(Equal([]string{OriginBOM, "service oci://registry.example.com/charts/my-service:1.0.0"}))
}

func TestImageCopiesTwoDigestsOfOneTag(t *testing.T) {
	g := NewWithT(t)
	otherDigest := "sha256:" + strings.Repeat("a", 64)
	extra := bom.Service{
		Chart: bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "my-service", Version: "1.0.0"},
		Images: []bom.Image{
			{Ref: "registry.k8s.io/pause:3.9@" + digest, Scope: []bom.Scope{bom.ScopeDPU}},
			{Ref: "registry.k8s.io/pause:3.9@" + otherDigest, Scope: []bom.Scope{bom.ScopeDPU}},
		},
	}
	var warnings []string
	logf := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }
	got, err := ImageCopies(testRelease(), Options{Mirror: "mirror.example.com", Extra: []bom.Service{extra}, Logf: logf})
	g.Expect(err).NotTo(HaveOccurred())

	// The digest of the BOM merges with the same digest of the service; the other digest is a
	// second copy to the same tag.
	var pauseSources []string
	for _, c := range got {
		if c.Destination == "mirror.example.com/pause:3.9" {
			pauseSources = append(pauseSources, c.Source)
		}
	}
	g.Expect(pauseSources).To(ConsistOf("registry.k8s.io/pause@"+digest, "registry.k8s.io/pause@"+otherDigest))
	g.Expect(warnings).To(ConsistOf(ContainSubstring("have different digests but target mirror.example.com/pause:3.9")))
}

func TestImageCopiesKamajiAddon(t *testing.T) {
	rel := testRelease()
	rel.BOM.Images = []bom.Image{
		{Ref: "registry.k8s.io/coredns/coredns:v1.14.2", Scope: []bom.Scope{bom.ScopeDPU}, KamajiAddon: true},
		{Ref: "registry.k8s.io/kube-proxy:v1.35.6", Scope: []bom.Scope{bom.ScopeDPU}, KamajiAddon: true},
	}
	tests := []struct {
		layout Layout
		want   []string
	}{
		{
			layout: LayoutNested,
			want: []string{
				"mirror.example.com/dpf/registry.k8s.io/coredns/coredns:v1.14.2",
				"mirror.example.com/dpf/registry.k8s.io/coredns:v1.14.2",
				"mirror.example.com/dpf/registry.k8s.io/kube-proxy:v1.35.6",
			},
		},
		{
			layout: LayoutFlat,
			want: []string{
				"mirror.example.com/dpf/coredns:v1.14.2",
				"mirror.example.com/dpf/kube-proxy:v1.35.6",
			},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.layout), func(t *testing.T) {
			g := NewWithT(t)
			got, err := ImageCopies(rel, Options{Mirror: "mirror.example.com/dpf", Layout: tt.layout})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(destinations(got)).To(Equal(tt.want))
		})
	}
}

func TestImageCopiesUntagged(t *testing.T) {
	extra := bom.Service{
		Chart:  bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "my-service", Version: "1.0.0"},
		Images: []bom.Image{{Ref: "nginx", Scope: []bom.Scope{bom.ScopeDPU}}},
	}
	for layout, want := range map[Layout]string{
		LayoutFlat:   "mirror.example.com/dpf/nginx:latest",
		LayoutNested: "mirror.example.com/dpf/docker.io/library/nginx:latest",
	} {
		t.Run(string(layout), func(t *testing.T) {
			g := NewWithT(t)
			got, err := ImageCopies(testRelease(), Options{Mirror: "mirror.example.com/dpf", Layout: layout, Extra: []bom.Service{extra}})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(ContainElement(HaveField("Destination", want)))
		})
	}
}

func TestImageCopiesRegistryWithPort(t *testing.T) {
	g := NewWithT(t)
	rel := testRelease()
	rel.Registry = "registry.example.com:8443/dpf"
	got, err := ImageCopies(rel, Options{Mirror: "mirror.example.com:5000", Layout: LayoutNested, Scopes: []bom.Scope{bom.ScopeDPU}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got[0].Source).To(Equal("registry.example.com:8443/dpf/dpf-system:v26.10.0"))
	g.Expect(got[0].Destination).To(Equal("mirror.example.com:5000/registry.example.com_8443/dpf/dpf-system:v26.10.0"))
	g.Expect(bom.ParseImageRef(got[0].Destination)).Error().NotTo(HaveOccurred())
}

func TestImageCopiesNeedsRegistryForDPFImages(t *testing.T) {
	g := NewWithT(t)
	rel := testRelease()
	rel.Registry = ""
	_, err := ImageCopies(rel, Options{Mirror: "mirror.example.com"})
	g.Expect(err).To(MatchError(ContainSubstring("image dpf-system is a DPF image and the DPF registry or version is unknown")))
}

func TestChartCopies(t *testing.T) {
	g := NewWithT(t)
	got, err := ChartCopies(testRelease(), Options{Mirror: "mirror.example.com:5000", Layout: LayoutNested, Services: []string{"doca-hbn"}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(Equal([]ChartCopy{
		{
			RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca", Name: "doca-hbn", Version: "3.5.0",
			Destination: "oci://mirror.example.com:5000/helm.ngc.nvidia.com/nvidia/doca",
			Origins:     []string{"service https://helm.ngc.nvidia.com/nvidia/doca/doca-hbn:3.5.0"},
		},
		{
			RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca", Name: "dpf-operator", Version: "v26.10.0",
			Destination: "oci://mirror.example.com:5000/helm.ngc.nvidia.com/nvidia/doca",
			Origins:     []string{OriginBOM},
		},
		{
			RepoURL: "oci://ghcr.io/mellanox/charts", Name: "ovn-kubernetes-chart", Version: "v26.8.0",
			Destination: "oci://mirror.example.com:5000/ghcr.io/mellanox/charts",
			Origins:     []string{OriginBOM},
		},
	}))

	all, err := ChartCopies(testRelease(), Options{Mirror: "mirror.example.com:5000", Layout: LayoutNested, AllServices: true})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(all).To(ContainElement(ChartCopy{
		RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca", Name: "dpf-storage", Version: "v26.10.0",
		Destination: "oci://mirror.example.com:5000/helm.ngc.nvidia.com/nvidia/doca",
		Origins:     []string{"service dpf-storage"},
	}))

	rel := testRelease()
	rel.ChartRepo = ""
	_, err = ChartCopies(rel, Options{Mirror: "mirror.example.com:5000"})
	g.Expect(err).To(MatchError(ContainSubstring("chart dpf-operator is a DPF chart and the DPF chart repository is unknown")))
}

func TestChartCopiesFlat(t *testing.T) {
	g := NewWithT(t)
	got, err := ChartCopies(testRelease(), Options{Mirror: "mirror.example.com:5000/dpf", Services: []string{"doca-hbn"}})
	g.Expect(err).NotTo(HaveOccurred())
	for _, c := range got {
		g.Expect(c.Destination).To(Equal("oci://mirror.example.com:5000/dpf"), c.Name)
	}

	extra := bom.Service{Chart: bom.Chart{RepoURL: "oci://registry.example.com/charts", Name: "doca-hbn", Version: "3.5.0"}}
	_, err = ChartCopies(testRelease(), Options{Mirror: "mirror.example.com", Services: []string{"doca-hbn"}, Extra: []bom.Service{extra}})
	g.Expect(err).To(MatchError(ContainSubstring("would both be copied to oci://mirror.example.com/doca-hbn:3.5.0")))
}

func TestDestinationChartRepo(t *testing.T) {
	tests := []struct {
		repoURL string
		want    string
		wantErr bool
	}{
		{repoURL: "https://helm.ngc.nvidia.com/nvidia/doca/", want: "oci://m:5000/helm.ngc.nvidia.com/nvidia/doca"},
		{repoURL: "oci://ghcr.io/mellanox/charts", want: "oci://m:5000/ghcr.io/mellanox/charts"},
		{repoURL: "http://charts.example.com", want: "oci://m:5000/charts.example.com"},
		{repoURL: "https://Harbor.example.com:8443/charts", want: "oci://m:5000/harbor.example.com_8443/charts"},
		{repoURL: "file:///charts", wantErr: true},
		{repoURL: "charts.example.com/x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.repoURL, func(t *testing.T) {
			g := NewWithT(t)
			got, err := destinationChartRepo("m:5000", LayoutNested, tt.repoURL)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}
