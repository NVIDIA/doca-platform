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

// Package release generates the air-gap BOM of DPF during a release: the images of everything the
// DPF operator deploys and of the optional services DPF ships (the storage and VPC charts), and the
// helm charts.
//
// The inputs are read from the repository as committed. The files the Makefile generates with the
// registry and tag of a build (release defaults, storage chart values) are committed with the
// placeholder registry and tag, and must not have been regenerated for another one
// (checkPlaceholders). The inputs are:
//
//   - the operator inventory generated with every system component enabled for one DPUCluster
//     served by host DNS: host objects give host images, DPUServices are rendered from the local
//     dpu-networking chart with the values the inventory sets;
//   - the dpf-operator chart and the keepalived manifest of the Kamaji cluster manager (host);
//   - the Kamaji tenant control plane (host) and the kube-proxy and CoreDNS addons (dpu);
//   - the DPU containerd sandbox image (SandboxImage);
//   - the services of serviceSources, and the VPC charts (vpcCharts), which cannot be rendered;
//   - the charts: the release charts of the Makefile, the VPC charts and the chart of every service.
//
// First-party images carry the placeholder registry and are listed by name. Generation is strict:
// it fails on an unresolved image value and a chart that cannot be rendered. Third-party images
// are listed as the repository and the rendered charts reference them, by tag or by digest.
package release

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nvidia/doca-platform/hack/tools/airgap-bom/internal/bom"
)

const (
	// FirstPartyHost is the placeholder registry of first-party images and charts, the default
	// REGISTRY of the Makefile.
	FirstPartyHost = "example.com"
	// FirstPartyVersion is the placeholder version, the default TAG of the Makefile.
	FirstPartyVersion = "v0.1.0"

	// SandboxImage is the sandbox image of the BFB's containerd, under the current name of the
	// Kubernetes registry; the BFB uses the old name. DPUFlavor containerdConfig.sandboxImage
	// points containerd at the mirror copy. Update it when the BFB changes its sandbox image.
	SandboxImage = "registry.k8s.io/pause:3.9"
)

// Options configures Release.
type Options struct {
	// RepoRoot is the root of the repository checkout.
	RepoRoot string
	// Engine renders the charts. Its FirstPartyRegistries are set by Release.
	Engine *bom.Engine
	// KamajiCoreDNSVersion returns the CoreDNS version of the Kamaji CoreDNS addon. Optional,
	// the default is the lookup in the kubeadm constants.
	KamajiCoreDNSVersion func(ctx context.Context) (string, error)
	// Logf receives progress. Optional.
	Logf func(format string, args ...any)
}

func (o Options) logf(msg string) {
	if o.Logf != nil {
		o.Logf("%s", msg)
	}
}

// Release returns the BOM of the harvested repository. It fails when an input cannot be
// rendered or resolved.
func Release(ctx context.Context, opts Options) (*bom.BOM, error) {
	if opts.Engine == nil {
		return nil, fmt.Errorf("an engine is required")
	}
	opts.Engine.FirstPartyRegistries = []string{FirstPartyHost}
	if err := checkPlaceholders(opts.RepoRoot); err != nil {
		return nil, err
	}

	charts, err := localCharts(opts.RepoRoot)
	if err != nil {
		return nil, err
	}

	out := &bom.BOM{SchemaVersion: bom.SchemaVersion}
	var errs []error

	opts.logf("harvesting the operator inventory")
	images, err := inventoryImages(ctx, opts, charts)
	if err != nil {
		errs = append(errs, fmt.Errorf("operator inventory: %w", err))
	}
	out.Images = append(out.Images, images...)

	opts.logf("harvesting the dpf-operator chart")
	images, err = operatorChartImages(ctx, opts, charts)
	if err != nil {
		errs = append(errs, fmt.Errorf("dpf-operator chart: %w", err))
	}
	out.Images = append(out.Images, images...)

	opts.logf("harvesting the Kamaji cluster manager")
	images, err = kamajiImages(ctx, opts)
	if err != nil {
		errs = append(errs, fmt.Errorf("kamaji: %w", err))
	}
	out.Images = append(out.Images, images...)

	sandbox := opts.Engine.Image(bom.MustParseImageRef(SandboxImage), bom.ScopeDPU)
	sandbox.Sandbox = true
	out.Images = append(out.Images, sandbox)

	opts.logf("harvesting the services")
	services, err := harvestServices(ctx, opts, charts)
	if err != nil {
		errs = append(errs, fmt.Errorf("services: %w", err))
	}
	out.Services = services
	for _, svc := range services {
		out.Charts = append(out.Charts, svc.Chart)
	}

	releaseCharts, err := FirstPartyChartNames(opts.RepoRoot)
	if err != nil {
		errs = append(errs, err)
	}
	for _, name := range releaseCharts {
		out.Charts = append(out.Charts, bom.Chart{Name: name})
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	out.Normalize()
	if err := checkChartNames(out.ChartNames(), releaseCharts); err != nil {
		return nil, err
	}
	return out, nil
}

// checkChartNames fails when a first-party chart of the BOM is not a first-party chart of the
// release: a test deploying a first-party chart the release does not publish fails.
func checkChartNames(bomNames, releaseNames []string) error {
	var errs []error
	for _, n := range bomNames {
		if !slices.Contains(releaseNames, n) {
			errs = append(errs, fmt.Errorf("first-party chart %q is used but is not a release chart of the Makefile or the VPC build", n))
		}
	}
	return errors.Join(errs...)
}

// ReleaseManifestScopes are the scopes of the release manifest whose images are first-party images
// of the BOM: release for the images of the Makefile and vpc for those of the VPC build. test_helper
// images are not part of the BOM.
var ReleaseManifestScopes = []string{"release", "vpc"}

// FirstPartyChartNames returns the first-party charts of a DPF release, sorted: the release charts
// of the Makefile and the charts of the VPC build. Their remote dependencies are not listed: the
// release packages them into the chart (e.g. flannel inside dpu-networking).
func FirstPartyChartNames(repoRoot string) ([]string, error) {
	names, err := ReleaseChartNames(repoRoot)
	if err != nil {
		return nil, err
	}
	names = append(names, vpcChartNames()...)
	slices.Sort(names)
	return slices.Compact(names), nil
}

// isFirstPartyChart reports whether a chart repository URL is the first-party placeholder.
func isFirstPartyChart(repoURL string) bool {
	host := repoURL
	for _, p := range []string{"oci://", "https://", "http://"} {
		host = strings.TrimPrefix(host, p)
	}
	host, _, _ = strings.Cut(host, "/")
	return host == FirstPartyHost
}
