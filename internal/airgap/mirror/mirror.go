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

// Package mirror computes what to copy into a mirror registry for an air-gapped DPF installation:
// the images and helm charts of the air-gap BOM of a release, narrowed to the clusters and the
// services the installation deploys. It only lists the copies; a tool like skopeo, crane or helm
// copies them.
//
// The layout of the mirror says where below it a copy goes:
//   - flat (the default): an image is copied to <mirror>/<name>:<tag>, its name being the last
//     element of its repository (nvcr.io/nvidia/doca/doca_hbn:3.5.0 to <mirror>/doca_hbn:3.5.0),
//     and the charts are pushed to oci://<mirror>. Two images or charts that would be copied to the
//     same place are an error.
//   - nested: a copy keeps its upstream location below the mirror: the image is copied to
//     <mirror>/nvcr.io/nvidia/doca/doca_hbn:3.5.0, and the charts of the helm repository
//     https://helm.ngc.nvidia.com/nvidia/doca to oci://<mirror>/helm.ngc.nvidia.com/nvidia/doca.
//     The upstream host is lower case in the path, with the colon of a port replaced by an
//     underscore. A Kamaji addon image gets a second copy where Kamaji pulls it, see
//     kamajiAddonDestination.
package mirror

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
)

// Release is the air-gap BOM of a DPF release and where its first-party images and charts are
// pulled from, which the BOM lists by name only.
type Release struct {
	BOM *bom.BOM
	// Registry is the registry (host/path) the first-party images are pulled from, e.g.
	// nvcr.io/nvidia/doca.
	Registry string
	// Version is the release version, the tag of the first-party images and the version of the
	// first-party charts.
	Version string
	// ChartRepo is the helm repository the first-party charts are pulled from, an http(s) helm
	// repository or oci://host/path. Only needed to list the charts.
	ChartRepo string
}

// Layout is where below the mirror a copy goes, see the package documentation.
type Layout string

const (
	LayoutFlat   Layout = "flat"
	LayoutNested Layout = "nested"
)

// Options selects what to copy and where to.
type Options struct {
	// Mirror is the registry to copy to, host[:port] optionally followed by a path, e.g.
	// mirror.example.com:5000/dpf.
	Mirror string
	// Layout is the layout of the mirror; empty is LayoutFlat.
	Layout Layout
	// Scopes are the clusters to copy the images of; empty is all of them.
	Scopes []bom.Scope
	// Services are the names of the optional services of the BOM to copy.
	Services []string
	// AllServices copies all the optional services of the BOM.
	AllServices bool
	// Extra are services found elsewhere, e.g. rendered from the DPUServices to be deployed.
	Extra []bom.Service
	// Logf receives warnings. When nil, nothing is logged.
	Logf func(format string, args ...any)
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// ImageCopy is an image to copy from Source to Destination.
type ImageCopy struct {
	// Reference is the image as the BOM or a chart references it, which is pulled from the mirror.
	Reference string `json:"reference"`
	// Source is where the image is copied from: the reference by digest when it has one, else
	// by tag. Copy tools such as skopeo do not take a reference with both.
	Source string `json:"source"`
	// Destination is where the image is copied to below the mirror. The copy keeps the tag, or the
	// digest when the reference has no tag; a reference with neither is copied as latest. Copied
	// with its digests preserved, the reference by digest still matches.
	Destination string      `json:"destination"`
	Scope       []bom.Scope `json:"scope"`
	// Origins are the parts of the BOM or the extra services that list the image.
	Origins []string `json:"origins"`
}

// ChartCopy is a helm chart to pull from RepoURL and push to Destination, an OCI repository.
type ChartCopy struct {
	RepoURL     string   `json:"repoURL"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Destination string   `json:"destination"`
	Origins     []string `json:"origins"`
}

// OriginBOM marks images and charts every installation needs.
const OriginBOM = "bom"

// ServiceOrigin is the origin of the images and the chart of a service.
func ServiceOrigin(s bom.Service) string {
	return "service " + s.Chart.String()
}

// ImageCopies lists the images to copy: those of the BOM and of the selected and extra services,
// pulled by one of the selected clusters, sorted by reference. Two references that pin different
// digests of one tag are both copied to that tag, with a warning to Logf.
func ImageCopies(rel Release, opts Options) ([]ImageCopy, error) {
	mirror, err := parseMirror(opts.Mirror)
	if err != nil {
		return nil, err
	}
	layout, err := parseLayout(opts.Layout)
	if err != nil {
		return nil, err
	}
	services, err := selectServices(rel.BOM, opts)
	if err != nil {
		return nil, err
	}
	type copyKey struct{ reference, destination string }
	byKey := map[copyKey]*ImageCopy{}
	// The upstream repository copied to each destination, to catch two copied to the same place.
	repoByDestination := map[string]string{}
	// The first reference pinned by digest copied to each destination, to warn about another digest
	// of the same tag.
	pinnedByDestination := map[string]bom.ImageRef{}
	var errs []error
	add := func(img bom.Image, origin string) {
		if !inScope(img.Scope, opts.Scopes) {
			return
		}
		src, err := resolveImage(rel, img)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", origin, err))
			return
		}
		dsts := []string{destinationImage(mirror, layout, src)}
		if layout == LayoutNested && img.KamajiAddon {
			dsts = appendUnique(dsts, kamajiAddonDestination(mirror, src))
		}
		for _, dst := range dsts {
			key := copyKey{reference: src.String(), destination: dst}
			c, ok := byKey[key]
			if !ok {
				if repo, ok := repoByDestination[dst]; ok && repo != src.Repository() {
					errs = append(errs, fmt.Errorf("%s: images %s and %s would both be copied to %s; use the nested layout",
						origin, repo, src.Repository(), dst))
					continue
				}
				repoByDestination[dst] = src.Repository()
				if src.Digest != "" {
					if prev, ok := pinnedByDestination[dst]; !ok {
						pinnedByDestination[dst] = src
					} else if prev.Digest != src.Digest {
						opts.logf(
							"warning: %s: images %s and %s have different digests but target %s; "+
								"pull by digest to select the intended image", origin, prev, src, dst,
						)
					}
				}
				c = &ImageCopy{Reference: key.reference, Source: sourceImage(src), Destination: dst}
				byKey[key] = c
			}
			c.Scope = appendUnique(c.Scope, img.Scope...)
			c.Origins = appendUnique(c.Origins, origin)
		}
	}
	for _, img := range rel.BOM.Images {
		add(img, OriginBOM)
	}
	for _, s := range services {
		for _, img := range s.Images {
			add(img, ServiceOrigin(s))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	out := make([]ImageCopy, 0, len(byKey))
	for _, c := range byKey {
		slices.Sort(c.Scope)
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b ImageCopy) int {
		return cmp.Or(cmp.Compare(a.Reference, b.Reference), cmp.Compare(a.Destination, b.Destination))
	})
	return out, nil
}

// ChartCopies lists the charts to copy: the charts of the BOM that no service lists, which every
// installation needs, and the charts of the selected and extra services, sorted.
func ChartCopies(rel Release, opts Options) ([]ChartCopy, error) {
	mirror, err := parseMirror(opts.Mirror)
	if err != nil {
		return nil, err
	}
	layout, err := parseLayout(opts.Layout)
	if err != nil {
		return nil, err
	}
	services, err := selectServices(rel.BOM, opts)
	if err != nil {
		return nil, err
	}
	serviceCharts := map[bom.Chart]bool{}
	for _, s := range rel.BOM.Services {
		serviceCharts[s.Chart] = true
	}
	byKey := map[bom.Chart]*ChartCopy{}
	// The repository of the chart copied to each destination chart, to catch two copied to the
	// same place.
	repoByDestination := map[string]string{}
	var errs []error
	add := func(chart bom.Chart, origin string) {
		repoURL, version := chart.RepoURL, chart.Version
		if chart.FirstParty() {
			if rel.ChartRepo == "" {
				errs = append(errs, fmt.Errorf("%s: chart %s is a DPF chart and the DPF chart repository is unknown", origin, chart.Name))
				return
			}
			repoURL, version = rel.ChartRepo, rel.Version
		}
		dst, err := destinationChartRepo(mirror, layout, repoURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: chart %s: %w", origin, chart, err))
			return
		}
		key := bom.Chart{RepoURL: repoURL, Name: chart.Name, Version: version}
		c, ok := byKey[key]
		if !ok {
			dstChart := dst + "/" + chart.Name + ":" + version
			if repo, ok := repoByDestination[dstChart]; ok && repo != repoURL {
				errs = append(errs, fmt.Errorf("%s: the charts %s of %s and of %s would both be copied to %s; use the nested layout",
					origin, chart.Name, repo, repoURL, dstChart))
				return
			}
			repoByDestination[dstChart] = repoURL
			c = &ChartCopy{RepoURL: repoURL, Name: chart.Name, Version: version, Destination: dst}
			byKey[key] = c
		}
		c.Origins = appendUnique(c.Origins, origin)
	}
	for _, chart := range rel.BOM.Charts {
		if !serviceCharts[chart] {
			add(chart, OriginBOM)
		}
	}
	for _, s := range services {
		add(s.Chart, ServiceOrigin(s))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	out := make([]ChartCopy, 0, len(byKey))
	for _, c := range byKey {
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b ChartCopy) int {
		return cmp.Or(cmp.Compare(a.RepoURL, b.RepoURL), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Version, b.Version))
	})
	return out, nil
}

// selectServices returns the optional services of the BOM that opts selects, followed by the
// extra services. An unknown service name is an error naming the services of the BOM.
func selectServices(b *bom.BOM, opts Options) ([]bom.Service, error) {
	if b == nil {
		return nil, errors.New("no BOM")
	}
	var out []bom.Service
	if opts.AllServices {
		out = append(out, b.Services...)
	} else {
		var unknown []string
		for _, name := range opts.Services {
			i := slices.IndexFunc(b.Services, func(s bom.Service) bool { return s.Name == name })
			if i < 0 {
				unknown = append(unknown, name)
				continue
			}
			out = append(out, b.Services[i])
		}
		if len(unknown) > 0 {
			return nil, fmt.Errorf("unknown service %s, the services of the BOM are: %s",
				strings.Join(unknown, ", "), strings.Join(ServiceNames(b), ", "))
		}
	}
	return append(out, opts.Extra...), nil
}

// ServiceNames returns the names of the optional services of the BOM.
func ServiceNames(b *bom.BOM) []string {
	names := make([]string, 0, len(b.Services))
	for _, s := range b.Services {
		names = append(names, s.Name)
	}
	return names
}

// inScope reports whether an image pulled by scopes is pulled by one of the selected clusters.
func inScope(scopes, selected []bom.Scope) bool {
	if len(selected) == 0 {
		return true
	}
	return slices.ContainsFunc(scopes, func(s bom.Scope) bool { return slices.Contains(selected, s) })
}

// resolveImage returns the reference an image entry is pulled by: a first-party name in the
// release registry, tagged with the release version.
func resolveImage(rel Release, img bom.Image) (bom.ImageRef, error) {
	if img.Ref != "" {
		return bom.ParseImageRef(img.Ref)
	}
	if rel.Registry == "" || rel.Version == "" {
		return bom.ImageRef{}, fmt.Errorf("image %s is a DPF image and the DPF registry or version is unknown", img.Name)
	}
	return bom.ParseImageRef(strings.TrimSuffix(rel.Registry, "/") + "/" + img.Name + ":" + rel.Version)
}

// parseLayout checks a layout; empty is LayoutFlat.
func parseLayout(l Layout) (Layout, error) {
	switch l {
	case "":
		return LayoutFlat, nil
	case LayoutFlat, LayoutNested:
		return l, nil
	default:
		return "", fmt.Errorf("invalid mirror layout %q: expected %s or %s", l, LayoutFlat, LayoutNested)
	}
}

// parseMirror checks the mirror registry, host[:port][/path], and returns it without a trailing slash.
func parseMirror(mirror string) (string, error) {
	mirror = strings.TrimSuffix(strings.TrimPrefix(mirror, "oci://"), "/")
	if mirror == "" {
		return "", errors.New("the mirror registry is not set")
	}
	ref, err := bom.ParseImageRef(mirror + "/image")
	if err != nil || ref.Repository() != mirror+"/image" {
		return "", fmt.Errorf("invalid mirror registry %q: expected host[:port][/path], e.g. mirror.example.com:5000/dpf", mirror)
	}
	return mirror, nil
}

// sourceImage returns the reference an image is copied from: by digest when it has one.
func sourceImage(ref bom.ImageRef) string {
	if ref.Digest != "" {
		return ref.Repository() + "@" + ref.Digest
	}
	return ref.String()
}

// destinationImage returns where an image is copied to below the mirror. The copy keeps the tag,
// or the digest when the image has no tag. An image with neither is copied as latest, the tag
// that a container runtime pulls for it.
func destinationImage(mirror string, layout Layout, src bom.ImageRef) string {
	dst := mirror + "/" + path.Base(src.Path)
	if layout == LayoutNested {
		dst = mirror + "/" + hostPathComponent(src.Host) + "/" + src.Path
	}
	switch {
	case src.Tag != "":
		return dst + ":" + src.Tag
	case src.Digest != "":
		return dst + "@" + src.Digest
	default:
		return dst + ":latest"
	}
}

// kamajiAddonDestination returns where Kamaji pulls an addon image in the nested layout,
// <mirror>/<host>/<last path element>:<tag>. Kamaji pulls an addon as
// <addonsRepository>/<last path element>:<tag>, and addonsRepository is the copy of the upstream
// registry, e.g. <mirror>/registry.k8s.io. The regular copy keeps the full upstream path, so an
// addon below a sub-path, such as registry.k8s.io/coredns/coredns, needs this second copy.
func kamajiAddonDestination(mirror string, src bom.ImageRef) string {
	src.Path = path.Base(src.Path)
	return destinationImage(mirror, LayoutNested, src)
}

// destinationChartRepo returns the OCI repository below the mirror the charts of a helm repository
// are pushed to.
func destinationChartRepo(mirror string, layout Layout, repoURL string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("invalid chart repository %q: %w", repoURL, err)
	}
	switch u.Scheme {
	case "http", "https", "oci":
	default:
		return "", fmt.Errorf("invalid chart repository %q: expected an http(s) or oci:// URL", repoURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid chart repository %q: no host", repoURL)
	}
	if layout == LayoutFlat {
		return "oci://" + mirror, nil
	}
	dst := "oci://" + mirror + "/" + hostPathComponent(u.Host)
	if p := strings.Trim(u.Path, "/"); p != "" {
		dst += "/" + p
	}
	return dst, nil
}

// hostPathComponent returns the repository path component an upstream registry host is copied
// below: lower case, with the colon of a port, which a repository path cannot contain, replaced by
// an underscore (harbor.example.com:8443 becomes harbor.example.com_8443).
func hostPathComponent(host string) string {
	return strings.ReplaceAll(strings.ToLower(host), ":", "_")
}

func appendUnique[T comparable](s []T, items ...T) []T {
	for _, item := range items {
		if !slices.Contains(s, item) {
			s = append(s, item)
		}
	}
	return s
}
