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
	"testing"

	. "github.com/onsi/gomega"
)

// fakeRenderer renders a Deployment whose containers use the "images" value (a list), and
// records the requests.
type fakeRenderer struct {
	requests []RenderRequest
	err      error
}

func (f *fakeRenderer) Render(_ context.Context, req RenderRequest) ([]byte, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	images, _ := req.Values["images"].([]any)
	out := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + req.Chart.Name + "\nspec:\n  template:\n    spec:\n      containers:\n"
	for i, img := range images {
		out += fmt.Sprintf("        - name: c%d\n          image: %q\n", i, img)
	}
	return []byte(out), nil
}

// TestEngineRenderImagesMemoizes checks that a chart is rendered once per run for the same values
// .
func TestEngineRenderImagesMemoizes(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	renderer := &fakeRenderer{}
	e := &Engine{Renderer: renderer}
	req := RenderRequest{
		Chart:  ChartRef{RepoURL: "https://r", Name: "app", Version: "1.0.0"},
		Values: map[string]any{"images": []any{"ghcr.io/a/b:v1"}},
	}

	found, err := e.RenderImages(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(Equal([]FoundImage{{Raw: "ghcr.io/a/b:v1", Object: "Deployment/app", Field: ".spec.template.spec.containers[0].image", Strict: true}}))
	g.Expect(renderer.requests).To(HaveLen(1))

	// The same request is served from memory.
	again, err := e.RenderImages(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(again).To(Equal(found))
	g.Expect(renderer.requests).To(HaveLen(1))

	// Other values render again.
	req.Values = map[string]any{"images": []any{"ghcr.io/a/c:v1"}}
	_, err = e.RenderImages(ctx, req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(renderer.requests).To(HaveLen(2))
}

func TestEngineRenderErrors(t *testing.T) {
	g := NewWithT(t)
	req := RenderRequest{Chart: ChartRef{RepoURL: "https://r", Name: "app", Version: "1.0.0"}}
	e := &Engine{Renderer: &fakeRenderer{err: errors.New("boom")}}
	_, err := e.RenderImages(context.Background(), req)
	g.Expect(err).To(MatchError("boom"))

	_, err = (&Engine{}).RenderImages(context.Background(), req)
	g.Expect(err).To(MatchError(ContainSubstring("no renderer")))
}

// TestEngineImages checks the BOM entries of found images: first-party images by name, below the
// registries as written or as oci:// URLs; third-party ones normalised; an invalid strict value
// fails, and an invalid or bare-word non-strict one is dropped with a log line.
func TestEngineImages(t *testing.T) {
	g := NewWithT(t)
	var logged []string
	e := &Engine{
		FirstPartyRegistries: []string{"example.com", "oci://nvcr.io/nvidia/doca/"},
		Logf:                 func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) },
	}
	found := []FoundImage{
		{Raw: "example.com/dpf-system:v0.1.0", Strict: true},
		{Raw: "nvcr.io/nvidia/doca/hostdriver:v26.10.0", Strict: true},
		{Raw: "ghcr.io/a/b:v1", Strict: true},
		{Raw: "ghcr.io/a/b:v1", Strict: false},
		{Raw: "ghcr.io/a/b:latest", Strict: true},
		{Raw: "ghcr.io/a/b:v2@" + testDigest, Strict: true},
		{Raw: "not a ref", Strict: false}, // does not parse
		{Raw: "foo", Strict: false},       // parses, but is a bare word
	}
	images, err := e.Images(found, ScopeDPU)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(logged).To(ConsistOf(
		ContainSubstring(`"not a ref": not an image reference`),
		ContainSubstring(`"foo": not an image reference`),
	))
	dpu := []Scope{ScopeDPU}
	g.Expect(images).To(ConsistOf(
		Image{Name: "dpf-system", Scope: dpu},
		Image{Name: "hostdriver", Scope: dpu},
		Image{Ref: "ghcr.io/a/b:v1", Scope: dpu},
		Image{Ref: "ghcr.io/a/b:latest", Scope: dpu},
		Image{Ref: "ghcr.io/a/b:v2@" + testDigest, Scope: dpu},
	))

	_, err = e.Images([]FoundImage{{Raw: "$X/image:v1", Strict: true}}, ScopeDPU)
	g.Expect(err).To(MatchError(ContainSubstring("$X/image:v1")))

	// A container with an empty image field is an error.
	pod := map[string]any{"kind": "Pod", "metadata": map[string]any{"name": "p"}, "spec": map[string]any{
		"containers": []any{map[string]any{"name": "c", "image": ""}},
	}}
	_, err = e.Images(ImagesFromObject(pod), ScopeDPU)
	g.Expect(err).To(MatchError(ContainSubstring(`Pod/p .spec.containers[0].image: "": empty image reference`)))
}
