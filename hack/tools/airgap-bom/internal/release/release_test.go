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
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
)

// repoRoot is the root of this repository, relative to the package directory.
const repoRoot = "../../../../.."

// update rewrites the golden BOM: go test ./hack/tools/airgap-bom/internal/release -run TestRelease -update
var update = flag.Bool("update", false, "update the golden BOM")

const goldenBOM = "testdata/release.golden.yaml"

// fakeRenderer renders a pod per chart whose image is named after the chart, and one for every
// component of the values enabled, so a BOM shows which chart and which component produced an image.
type fakeRenderer struct {
	requests []bom.RenderRequest
}

func (f *fakeRenderer) Render(_ context.Context, req bom.RenderRequest) ([]byte, error) {
	f.requests = append(f.requests, req)
	images := []string{"ghcr.io/example/" + req.Chart.Name + ":v1"}
	for _, k := range slices.Sorted(maps.Keys(req.Values)) {
		if m, ok := req.Values[k].(map[string]any); ok && m["enabled"] == true {
			images = append(images, "ghcr.io/example/"+req.Chart.Name+"/"+strings.ToLower(k)+":v1")
		}
	}
	out := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: p\nspec:\n  containers:\n"
	for i, image := range images {
		out += fmt.Sprintf("    - name: c%d\n      image: %s\n", i, image)
	}
	return []byte(out), nil
}

func TestReleaseChartNames(t *testing.T) {
	g := NewWithT(t)
	makefile := []byte(`
A_CHART_NAME ?= a-chart
export B_CHART_NAME = b-chart
TEST_CHART_NAME = test-chart
x:
	@for tag in $(TAGS); do \
		$(call record_release_artifact,chart,$(A_CHART_NAME),$(HELM_REGISTRY)/$(A_CHART_NAME):$$tag,release) \
	done
	@$(call record_release_artifact,chart,$(B_CHART_NAME),$(HELM_REGISTRY)/$(B_CHART_NAME):$(TAG),release)
	@$(call record_release_artifact,chart,$(TEST_CHART_NAME),$(HELM_REGISTRY)/$(TEST_CHART_NAME):$(TAG),test_helper)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(A_IMAGE_NAME),$(A_IMAGE):$(TAG),release))
`)
	names, err := recordedNames(makefile, releaseChartRe, "chart")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(names).To(Equal([]string{"a-chart", "b-chart"}))
	_, err = recordedNames([]byte("x:\n"), releaseChartRe, "chart")
	g.Expect(err).To(MatchError(ContainSubstring("no release charts")))

	// The repository Makefile: the charts of HELM_TARGETS, not the test-only dummydpuservice chart.
	names, err = ReleaseChartNames(repoRoot)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(names).To(Equal([]string{"dpf-operator", "dpf-storage", "dpu-networking"}))

	names, err = FirstPartyChartNames(repoRoot)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(names).To(Equal([]string{"dpf-operator", "dpf-storage", "dpf-vpc-ovn", "dpf-weave", "dpu-networking", "ovn-chart"}))
	g.Expect(checkChartNames([]string{"dpf-operator", "x"}, names)).To(MatchError(ContainSubstring(`chart "x" is used but is not a release chart`)))
	g.Expect(checkChartNames([]string{"dpf-operator"}, names)).To(Succeed())
}

// TestVPCImageNames checks the image names in the hand-written VPC chart list.
func TestVPCImageNames(t *testing.T) {
	g := NewWithT(t)
	g.Expect(VPCImageNames()).To(Equal([]string{"ovn", "vpc-system", "weave-system"}))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestLookupCoreDNSVersion looks up the CoreDNS version in the kubeadm constants of the
// Kubernetes release the deployed Kamaji image is built against.
func TestLookupCoreDNSVersion(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeKamajiValues(t, dir, "v1.36.0-26.6.4")

	var paths []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		body, status := "", http.StatusOK
		switch r.URL.Path {
		case "/v1.36.0/constants.go":
			body = "const (\n\t// CoreDNSImageName specifies the name of the image for CoreDNS add-on\n\tCoreDNSImageName = \"coredns\"\n\n\t// CoreDNSVersion is the version of CoreDNS to be deployed if it is used\n\tCoreDNSVersion = \"v1.14.2\"\n)\n"
		case "/v1.36.0-empty/constants.go":
			body = "package constants\n"
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	const urlFormat = "https://kubeadm.invalid/%s/constants.go"

	version, err := lookupCoreDNSVersion(context.Background(), dir, client, urlFormat)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(version).To(Equal("v1.14.2"))
	g.Expect(paths).To(Equal([]string{"/v1.36.0/constants.go"}))

	writeKamajiValues(t, dir, "v1.99.0-1.0.0")
	_, err = lookupCoreDNSVersion(context.Background(), dir, client, urlFormat)
	g.Expect(err).To(MatchError(ContainSubstring("404")))

	writeKamajiValues(t, dir, "v1.36.0")
	_, err = lookupCoreDNSVersion(context.Background(), dir, client, "https://kubeadm.invalid/%s-empty/constants.go")
	g.Expect(err).To(MatchError(ContainSubstring("no CoreDNSVersion")))

	// The tag of the Kamaji image is <Kubernetes version>-<Kamaji release>.
	writeKamajiValues(t, dir, "latest")
	_, err = lookupCoreDNSVersion(context.Background(), dir, client, urlFormat)
	g.Expect(err).To(HaveOccurred())

	// The Kamaji the repository deploys.
	image, err := deployedKamajiImage(repoRoot)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = kamajiKubernetesVersion(image)
	g.Expect(err).NotTo(HaveOccurred())
}

// writeKamajiValues writes the values of the Kamaji chart with the image tag below dir.
func writeKamajiValues(t *testing.T, dir, tag string) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(os.MkdirAll(filepath.Join(dir, "deploy/helmfiles/values"), 0o755)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, kamajiValues), []byte("replicas: 2\nimage:\n  repository: ghcr.io/nvidia/kamaji\n  tag: "+tag+"\n"), 0o600)).To(Succeed())
}

// TestCheckPlaceholders accepts the generated inputs of the repository, as committed, and fails
// for release defaults generated for another registry.
func TestCheckPlaceholders(t *testing.T) {
	g := NewWithT(t)
	g.Expect(checkPlaceholders(repoRoot)).To(Succeed())

	good := map[string]string{
		releaseDefaultsFile: "dpfSystemImage: example.com/dpf-system:v0.1.0\n",
		generatedInputs[1]:  "image:\n  repository: example.com/storage-system\n",
	}
	repo := writeRepo(t, good)
	g.Expect(checkPlaceholders(repo)).To(Succeed())
	for _, file := range generatedInputs {
		repo := writeRepo(t, good)
		g.Expect(os.WriteFile(filepath.Join(repo, file), []byte("image: nvcr.io/nvidia/doca/x:v26.10.0\n"), 0o600)).To(Succeed())
		g.Expect(checkPlaceholders(repo)).To(MatchError(ContainSubstring(file + " was generated for a registry other than example.com")))
	}
}

// TestKamajiRefs checks the Kamaji control plane images against the defaults of the forked
// Kamaji API.
func TestKamajiRefs(t *testing.T) {
	g := NewWithT(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "third_party/forked/github.com/clastix/kamaji/api/v1alpha1/tenantcontrolplane_registrysettings.go"))
	g.Expect(err).NotTo(HaveOccurred())
	defaults := regexp.MustCompile(`\+kubebuilder:default="([^"]+)"`).FindAllStringSubmatch(string(data), -1)
	g.Expect(defaults).To(HaveLen(4))
	g.Expect(defaults[0][1]).To(Equal(kubernetesImageRegistry))
	images := make([]string, 0, len(defaults)-1)
	for _, d := range defaults[1:] {
		images = append(images, d[1])
	}
	g.Expect(kamajiControlPlaneImages).To(Equal(images))

	host, dpu := kamajiRefs()
	g.Expect(host).To(HaveLen(3))
	g.Expect(host[0].String()).To(Equal("registry.k8s.io/kube-apiserver:" + cutil.KubernetesVersion))
	g.Expect(dpu[0].String()).To(Equal("registry.k8s.io/kube-proxy:" + cutil.KubernetesVersion))
	g.Expect(dpu).To(HaveLen(1))
	coreDNS, err := kamajiCoreDNSRef("v1.14.2")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(coreDNS.String()).To(Equal("registry.k8s.io/coredns/coredns:v1.14.2"))
}

// TestInventoryImages generates the operator inventory of the repository with a fake renderer.
func TestInventoryImages(t *testing.T) {
	g := NewWithT(t)
	charts, err := localCharts(repoRoot)
	g.Expect(err).NotTo(HaveOccurred())
	renderer := &fakeRenderer{}
	e := &bom.Engine{Renderer: renderer, FirstPartyRegistries: []string{FirstPartyHost}}

	images, err := inventoryImages(context.Background(), Options{RepoRoot: repoRoot, Engine: e}, charts)
	g.Expect(err).NotTo(HaveOccurred())
	b := &bom.BOM{Images: images}
	b.Normalize()

	byKey := map[string][]bom.Scope{}
	for _, img := range b.Images {
		key := img.Name
		if key == "" {
			key = img.Ref
		}
		byKey[key] = img.Scope
	}
	g.Expect(byKey).To(HaveKeyWithValue("dpf-system", []bom.Scope{bom.ScopeHost}))
	g.Expect(byKey).To(HaveKeyWithValue("hostdriver", []bom.Scope{bom.ScopeHost}))
	g.Expect(byKey).To(HaveKeyWithValue("bfb-registry", []bom.Scope{bom.ScopeHost}))
	g.Expect(byKey).To(HaveKeyWithValue("dpf-keepalived", []bom.Scope{bom.ScopeHost}))
	// The image of the dpu-networking chart appears in both clusters: system DPUServices run on the
	// DPUs and, with deployInCluster, on the host.
	g.Expect(byKey).To(HaveKeyWithValue("ghcr.io/example/dpu-networking:v1", []bom.Scope{bom.ScopeDPU, bom.ScopeHost}))

	// Every system DPUService renders the local dpu-networking chart with its component enabled.
	enabled := map[string]struct{}{}
	for _, req := range renderer.requests {
		g.Expect(req.Chart.Path).To(Equal(charts["dpu-networking"]))
		for k, v := range req.Values {
			if m, ok := v.(map[string]any); ok && m["enabled"] == true {
				enabled[k] = struct{}{}
			}
		}
	}
	for _, component := range []string{"flannel", "multus", "sriov-device-plugin", "coredns", "kata-containers", "cni-installer"} {
		g.Expect(enabled).To(HaveKey(component))
	}
}

// TestRelease generates the BOM of this repository with a fake renderer and CoreDNS lookup
// and compares it with the golden BOM, so a change of the BOM shows in the diff of a merge request.
// Run with -update to regenerate it.
func TestRelease(t *testing.T) {
	g := NewWithT(t)
	opts := Options{
		RepoRoot:             repoRoot,
		Engine:               &bom.Engine{Renderer: &fakeRenderer{}},
		KamajiCoreDNSVersion: func(context.Context) (string, error) { return "v1.14.2", nil },
	}
	b, err := Release(context.Background(), opts)
	g.Expect(err).NotTo(HaveOccurred())
	got, err := b.Marshal(bom.Header)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(got)).NotTo(ContainSubstring("$"), "no unresolved variable")
	g.Expect(string(got)).To(ContainSubstring("registry.k8s.io/kube-proxy:" + cutil.KubernetesVersion))

	if *update {
		g.Expect(os.WriteFile(goldenBOM, got, 0o644)).To(Succeed())
	}
	want, err := os.ReadFile(goldenBOM)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(got)).To(Equal(string(want)), cmp.Diff(string(want), string(got)))

	// A failing CoreDNS lookup fails the release.
	opts.Engine = &bom.Engine{Renderer: &fakeRenderer{}}
	opts.KamajiCoreDNSVersion = func(context.Context) (string, error) { return "", fmt.Errorf("no network") }
	_, err = Release(context.Background(), opts)
	g.Expect(err).To(MatchError(ContainSubstring("no network")))
}
