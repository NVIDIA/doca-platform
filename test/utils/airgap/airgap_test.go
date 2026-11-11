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

package airgap

import (
	"strings"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/airgap/mirror"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

const (
	testMirror = "10.0.110.253:5000/airgap"
	// releaseBOM is the committed BOM, so the overrides are checked against the images DPF ships.
	releaseBOM = "../../../deploy/charts/dpf-operator/airgap/airgapped.yaml"
)

func newTestMirror(t *testing.T, apt string) *Mirror {
	t.Helper()
	m, err := New(Config{
		Mirror:      testMirror,
		AptMirror:   apt,
		BOMPath:     releaseBOM,
		DPFRegistry: "harbor.example.com/dpf",
		DPFVersion:  "v0.1.0-abc",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return m
}

func TestApplyToDPFOperatorConfig(t *testing.T) {
	g := NewWithT(t)
	m := newTestMirror(t, "")
	cfg := &operatorv1.DPFOperatorConfig{
		Spec: operatorv1.DPFOperatorConfigSpec{
			Security: &operatorv1.SecurityConfiguration{
				Kata: &operatorv1.KataContainersConfiguration{},
			},
			Multus: &operatorv1.MultusConfiguration{},
		},
	}
	//nolint:staticcheck // the deprecated field must be cleared
	cfg.Spec.Multus.Image = ptr.To("ghcr.io/k8snetworkplumbingwg/multus-cni:old")
	set, err := m.ApplyToDPFOperatorConfig(cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(set).To(ContainElement("spec.kamajiClusterManager.dpuClusterImages.addonsRepository=10.0.110.253:5000/airgap"))

	g.Expect(*cfg.Spec.SFCController.Controller.Image).To(Equal("10.0.110.253:5000/airgap/dpf-system:v0.1.0-abc"))
	g.Expect(cfg.Spec.ServiceSetController).To(BeNil(), "the service chain set controller runs in the host cluster")
	g.Expect(*cfg.Spec.NVIPAM.Node.Image).To(HavePrefix("10.0.110.253:5000/airgap/nvidia-k8s-ipam:"))
	g.Expect(cfg.Spec.NVIPAM.Controller).To(BeNil(), "the nvipam controller runs in the host cluster")
	g.Expect(*cfg.Spec.Multus.CNI.Image).To(HavePrefix("10.0.110.253:5000/airgap/multus-cni:"))
	g.Expect(*cfg.Spec.Security.Kata.Daemon.Image).To(HavePrefix("10.0.110.253:5000/airgap/kata-deploy:"))
	//nolint:staticcheck // the deprecated field must be cleared
	g.Expect(cfg.Spec.Multus.Image).To(BeNil())
	// Components that are not deployed are left alone.
	g.Expect(cfg.Spec.Security.SPIFFE).To(BeNil())
	g.Expect(cfg.Spec.Monitoring.OpenTelemetryCollector).To(BeNil())
	for _, s := range set {
		_, value, _ := strings.Cut(s, "=")
		// In the flat layout the addons repository is the mirror itself.
		g.Expect(value == testMirror || m.Contains(value)).To(BeTrue(), s)
	}
}

func TestAddonsRepositoryNested(t *testing.T) {
	g := NewWithT(t)
	m, err := New(Config{Mirror: testMirror, Layout: mirror.LayoutNested, BOMPath: releaseBOM, DPFRegistry: "harbor.example.com/dpf", DPFVersion: "v0.1.0-abc"})
	g.Expect(err).NotTo(HaveOccurred())
	addons, err := m.AddonsRepository()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(addons).To(Equal("10.0.110.253:5000/airgap/registry.k8s.io"))
	g.Expect(m.SandboxImage()).To(Equal("10.0.110.253:5000/airgap/registry.k8s.io/pause:3.9"))
}

func TestApplyToDPUFlavor(t *testing.T) {
	g := NewWithT(t)
	m := newTestMirror(t, "10.0.110.253:8080")
	flavor := &provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
		Packages: []provisioningv1.PackageSpec{{Name: "tree"}},
	}}
	m.ApplyToDPUFlavor(flavor)

	g.Expect(flavor.Spec.ContainerdConfig.SandboxImage).To(Equal("10.0.110.253:5000/airgap/pause:3.9"))
	g.Expect(flavor.Spec.ContainerdConfig.RegistryEndpoint).To(BeEmpty())
	files := map[string]string{}
	for _, f := range flavor.Spec.ConfigFiles {
		files[f.Path] = *f.Raw
	}
	g.Expect(files).To(HaveKeyWithValue("/etc/containerd/certs.d/10.0.110.253:5000/hosts.toml",
		ContainSubstring(`server = "http://10.0.110.253:5000"`)))
	g.Expect(files).To(HaveKeyWithValue(AptSourcesFile, "deb [trusted=yes] http://10.0.110.253:8080/ ./\n"))
	g.Expect(files).To(HaveKey("/etc/systemd/system/" + QuietUnit))
	g.Expect(files).To(HaveKey(pollinateSeeded))
	g.Expect(files).To(HaveKey(esmAptHook))
	g.Expect(flavor.Spec.Packages[0].RepoFileRef).To(Equal(AptSourcesFile))
	g.Expect(flavor.Spec.SystemdServices).To(ContainElement(provisioningv1.SystemdServiceSpec{
		Name:      QuietUnit,
		Operation: provisioningv1.SystemdServiceEnableAndStart,
	}))

	// Without an apt mirror the packages are left alone.
	flavor = &provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
		Packages: []provisioningv1.PackageSpec{{Name: "tree"}},
	}}
	newTestMirror(t, "").ApplyToDPUFlavor(flavor)
	g.Expect(flavor.Spec.Packages[0].RepoFileRef).To(BeEmpty())
}

func TestRewriteHelmValues(t *testing.T) {
	g := NewWithT(t)
	m := newTestMirror(t, "")
	in := &runtime.RawExtension{Raw: []byte(`{
		"image": {"repository": "mirror.gcr.io/nginx", "tag": "1.27"},
		"sidecar": {"image": {"repository": "10.0.110.253:5000/airgap/ghcr.io/x/y"}},
		"list": [{"image": {"repository": "harbor.example.com:8443/dpf/netutils"}}],
		"pinned": {"image": {"repository": "nvcr.io/x:1.0"}},
		"other": {"repository": "not/an-image"}
	}`)}
	out, err := m.RewriteHelmValues(in)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(out.Raw)).To(And(
		ContainSubstring(`"repository":"10.0.110.253:5000/airgap/nginx"`),
		ContainSubstring(`"repository":"10.0.110.253:5000/airgap/ghcr.io/x/y"`),
		ContainSubstring(`"repository":"10.0.110.253:5000/airgap/netutils"`),
		ContainSubstring(`"repository":"nvcr.io/x:1.0"`),
		ContainSubstring(`"repository":"not/an-image"`),
	))

	nilOut, err := m.RewriteHelmValues(nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nilOut).To(BeNil())
}

func TestUnmirrored(t *testing.T) {
	g := NewWithT(t)
	m := newTestMirror(t, "")
	got := m.Unmirrored([]PodImage{
		{Image: "10.0.110.253:5000/airgap/ghcr.io/flannel-io/flannel:v0.27.2", Where: "a"},
		{Image: "ghcr.io/flannel-io/flannel:v0.27.2", Where: "b"},
		{Image: "10.0.110.253:5000/other/x:1", Where: "c"},
	})
	g.Expect(got).To(Equal([]PodImage{
		{Image: "ghcr.io/flannel-io/flannel:v0.27.2", Where: "b"},
		{Image: "10.0.110.253:5000/other/x:1", Where: "c"},
	}))
}
