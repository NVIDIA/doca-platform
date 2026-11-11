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

// Package airgap points the objects of an e2e run at an air-gap mirror registry and checks that
// the DPU cluster pulls nothing else. The mirror holds the DPU images of the air-gap BOM in a
// layout of dpfctl airgap images (flat <mirror>/<name>:<tag> by default), filled by the CI
// (hack/scripts/do_not_publish/ci-airgap.sh). The DPU reaches it over plain HTTP.
package airgap

import (
	"fmt"
	"path"
	"strings"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
	"github.com/nvidia/doca-platform/internal/airgap/mirror"
)

// Config configures a Mirror.
type Config struct {
	// Mirror is the registry the DPU pulls from, host[:port][/path], e.g. 10.0.110.253:5000/airgap.
	Mirror string
	// Layout is the layout of the mirror, as dpfctl airgap images --layout; empty is flat.
	Layout mirror.Layout
	// AptMirror is the apt repository the DPU installs the DPUFlavor packages from, host[:port] of
	// a flat repository served over plain HTTP. Empty leaves the packages alone.
	AptMirror string
	// BOMPath is the air-gap BOM, e.g. deploy/charts/dpf-operator/airgap/airgapped.yaml.
	BOMPath string
	// DPFRegistry and DPFVersion are the registry and tag the DPF images are installed from, which
	// the BOM lists by name only.
	DPFRegistry string
	DPFVersion  string
}

// Mirror maps the DPU images of the air-gap BOM to their copies in the mirror.
type Mirror struct {
	cfg Config
	// host is the registry host of the mirror, host[:port].
	host string
	// images maps the upstream repository of each DPU image of the BOM to its mirror copy.
	images map[string]string
	// sandbox is the mirror copy of the containerd sandbox image of the BOM.
	sandbox string
}

// New returns the Mirror of cfg.
func New(cfg Config) (*Mirror, error) {
	b, err := bom.Load(cfg.BOMPath)
	if err != nil {
		return nil, err
	}
	rel := mirror.Release{BOM: b, Registry: cfg.DPFRegistry, Version: cfg.DPFVersion}
	copies, err := mirror.ImageCopies(rel, mirror.Options{Mirror: cfg.Mirror, Layout: cfg.Layout, Scopes: []bom.Scope{bom.ScopeDPU}})
	if err != nil {
		return nil, err
	}
	m := &Mirror{
		cfg:    cfg,
		host:   strings.SplitN(strings.TrimPrefix(cfg.Mirror, "oci://"), "/", 2)[0],
		images: map[string]string{},
	}
	for _, c := range copies {
		ref, err := bom.ParseImageRef(c.Reference)
		if err != nil {
			return nil, err
		}
		if _, ok := m.images[ref.Repository()]; !ok {
			m.images[ref.Repository()] = c.Destination
		}
	}
	for _, img := range b.Images {
		if !img.Sandbox {
			continue
		}
		if m.sandbox, err = mirror.Destination(cfg.Mirror, cfg.Layout, img.Ref); err != nil {
			return nil, err
		}
	}
	if m.sandbox == "" {
		return nil, fmt.Errorf("BOM %s lists no sandbox image", cfg.BOMPath)
	}
	return m, nil
}

// Host is the registry host of the mirror, host[:port].
func (m *Mirror) Host() string {
	return m.host
}

// Image returns the mirror copy of the image reference ref.
func (m *Mirror) Image(ref string) (string, error) {
	return mirror.Destination(m.cfg.Mirror, m.cfg.Layout, ref)
}

// Repository returns the mirror repository of the images of the repository repo (host/path).
func (m *Mirror) Repository(repo string) (string, error) {
	return mirror.DestinationRepository(m.cfg.Mirror, m.cfg.Layout, repo)
}

// bomImage returns the mirror copy of the DPU image of the BOM in repository, a full repository or
// the name of a DPF image, and whether the BOM lists it.
func (m *Mirror) bomImage(repository string) (string, bool) {
	if !strings.Contains(repository, "/") {
		repository = strings.TrimSuffix(m.cfg.DPFRegistry, "/") + "/" + repository
	}
	ref, err := bom.ParseImageRef(repository)
	if err != nil {
		return "", false
	}
	img, ok := m.images[ref.Repository()]
	return img, ok
}

// AddonsRepository returns the repository the Kamaji DPU cluster addons are pulled from: the parent
// of the mirror repository of kube-proxy, <mirror> in the flat layout and <mirror>/registry.k8s.io
// in the nested one. kubeadm pulls kube-proxy from <repository>/kube-proxy and CoreDNS from
// <repository>/coredns, so in the nested layout CoreDNS must be copied a second time.
func (m *Mirror) AddonsRepository() (string, error) {
	repo, err := m.Repository(kubeProxyRepository)
	if err != nil {
		return "", err
	}
	return path.Dir(repo), nil
}

// Contains reports whether the image reference is pulled from the mirror.
func (m *Mirror) Contains(image string) bool {
	return strings.HasPrefix(image, strings.TrimSuffix(strings.TrimPrefix(m.cfg.Mirror, "oci://"), "/")+"/")
}
