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

package release

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"sigs.k8s.io/yaml"
)

const (
	kamajiValues = "deploy/helmfiles/values/kamaji.yaml"
	// kubernetesImageRegistry is the registry Kamaji pulls its images from by default, see
	// kamajiControlPlaneImages.
	kubernetesImageRegistry = "registry.k8s.io"
	// kubeadmConstantsURL is the kubeadm constants of a Kubernetes release (%s), which hold the
	// CoreDNS version kubeadm deploys by default.
	kubeadmConstantsURL = "https://raw.githubusercontent.com/kubernetes/kubernetes/%s/cmd/kubeadm/app/constants/constants.go"
)

// kamajiImageTag matches the tag of a Kamaji image, <Kubernetes version>[-<Kamaji release>], e.g.
// v1.36.0-26.6.4: the Kubernetes release Kamaji is built against.
var kamajiImageTag = regexp.MustCompile(`^(v\d+\.\d+\.\d+)(-.+)?$`)

var kubeadmCoreDNSVersion = regexp.MustCompile(`(?m)^\s*CoreDNSVersion\s*=\s*"(v?\d+\.\d+\.\d+)"`)

// kamajiControlPlaneImages are the images of a Kamaji TenantControlPlane: the kubebuilder defaults
// of RegistrySettings of the forked Kamaji API, so Kamaji pulls <registry>/<image>:<Kubernetes
// version>. The DPFOperatorConfig can point them at another repository
// (dpuClusterImages.hostedControlPlaneRepository); the BOM lists the default one.
var kamajiControlPlaneImages = []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler"}

// kamajiImages returns the Kamaji DPUCluster images and the keepalived DaemonSet images.
func kamajiImages(ctx context.Context, opts Options) ([]bom.Image, error) {
	host, dpu := kamajiRefs()
	images := make([]bom.Image, 0, len(host)+len(dpu)+2)
	for _, ref := range host {
		images = append(images, opts.Engine.Image(ref, bom.ScopeHost))
	}
	for _, ref := range dpu {
		images = append(images, kamajiAddonImage(opts.Engine, ref))
	}
	lookup := opts.KamajiCoreDNSVersion
	if lookup == nil {
		lookup = func(ctx context.Context) (string, error) {
			return lookupCoreDNSVersion(ctx, opts.RepoRoot, nil, "")
		}
	}
	version, err := lookup(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := kamajiCoreDNSRef(version)
	if err != nil {
		return nil, err
	}
	images = append(images, kamajiAddonImage(opts.Engine, ref))
	keepalived, err := keepalivedImages(opts)
	if err != nil {
		return nil, err
	}
	return append(images, keepalived...), nil
}

// lookupCoreDNSVersion returns the CoreDNS version Kamaji deploys with its CoreDNS addon when the
// DPUCluster sets no image tag, as DPF does not: the CoreDNSVersion of kubeadm of the Kubernetes
// release the deployed Kamaji image (kamajiValues) is built against, not of
// cutil.KubernetesVersion. The constants are read from urlFormat (empty: kubeadmConstantsURL) with
// the Kubernetes version in place of %s, using client (nil: a client with a timeout).
func lookupCoreDNSVersion(ctx context.Context, repoRoot string, client *http.Client, urlFormat string) (string, error) {
	image, err := deployedKamajiImage(repoRoot)
	if err != nil {
		return "", err
	}
	version, err := kamajiKubernetesVersion(image)
	if err != nil {
		return "", err
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	if urlFormat == "" {
		urlFormat = kubeadmConstantsURL
	}
	url := fmt.Sprintf(urlFormat, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to read the kubeadm constants of %s: %w", version, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to read the kubeadm constants of %s: %s: %s", version, url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read the kubeadm constants of %s: %w", version, err)
	}
	m := kubeadmCoreDNSVersion.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("no CoreDNSVersion in the kubeadm constants of %s (%s)", version, url)
	}
	return string(m[1]), nil
}

func deployedKamajiImage(repoRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, kamajiValues))
	if err != nil {
		return "", err
	}
	values := struct {
		Image struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"image"`
	}{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("%s: %w", kamajiValues, err)
	}
	if values.Image.Repository == "" || values.Image.Tag == "" {
		return "", fmt.Errorf("%s: image.repository and image.tag must be set", kamajiValues)
	}
	return values.Image.Repository + ":" + values.Image.Tag, nil
}

// kamajiKubernetesVersion returns the Kubernetes release the Kamaji image is built against, which
// its tag starts with.
func kamajiKubernetesVersion(image string) (string, error) {
	ref, err := bom.ParseImageRef(image)
	if err != nil {
		return "", err
	}
	m := kamajiImageTag.FindStringSubmatch(ref.Tag)
	if m == nil {
		return "", fmt.Errorf("the tag of the Kamaji image %s is not <Kubernetes version>[-<Kamaji release>], e.g. v1.36.0-26.6.4", image)
	}
	return m[1], nil
}

// kamajiRefs returns the images of a Kamaji DPUCluster with the versions DPF pins: the tenant
// control plane, which runs on the host cluster, and the kube-proxy addon, which runs on the DPUs.
// The CoreDNS addon is kamajiCoreDNSRef.
func kamajiRefs() (host, dpu []bom.ImageRef) {
	for _, name := range kamajiControlPlaneImages {
		host = append(host, bom.MustParseImageRef(kubernetesImageRegistry+"/"+name+":"+cutil.KubernetesVersion))
	}
	dpu = []bom.ImageRef{
		// Kamaji's kube-proxy addon defaults to the registry and the TenantControlPlane version.
		bom.MustParseImageRef(kubernetesImageRegistry + "/kube-proxy:" + cutil.KubernetesVersion),
	}
	return host, dpu
}

// kamajiAddonImage returns the BOM entry of an addon image of a Kamaji DPUCluster, which runs on
// the DPUs.
func kamajiAddonImage(engine *bom.Engine, ref bom.ImageRef) bom.Image {
	img := engine.Image(ref, bom.ScopeDPU)
	img.KamajiAddon = true
	return img
}

// kamajiCoreDNSRef returns the CoreDNS addon image of a Kamaji DPUCluster, which runs on the DPUs:
// kubeadm maps the default registry to its coredns/ sub-path.
func kamajiCoreDNSRef(version string) (bom.ImageRef, error) {
	return bom.ParseImageRef(kubernetesImageRegistry + "/coredns/coredns:" + version)
}
