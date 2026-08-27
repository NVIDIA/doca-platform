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

package util

import (
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestFilenameFromHTTPURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"iso basename", "https://nbu-nfs.example.com/auto/sw_mc_soc_release/doca_3.3_bf4/ISO/bf4-os-doca-bundle-3.3.0-321_26.01_ubuntu-24.04_64k.iso", "bf4-os-doca-bundle-3.3.0-321_26.01_ubuntu-24.04_64k.iso"},
		{"http", "http://host/path/fw.tar", "fw.tar"},
		{"opaque not url", "version-1.2.3", ""},
		{"empty path", "https://host", ""},
		{"file scheme", "file:///tmp/x.bin", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FilenameFromHTTPURL(tt.raw); got != tt.want {
				t.Fatalf("FilenameFromHTTPURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSpecURLForComponent(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{
				"MT_0000001665": "https://x/dpu-pldm",
			},
			PlatformPldmFwBundle: ptr.To("https://x/platform-pldm"),
			OsIso:                "https://x/iso",
			NicFw:                ptr.To("https://x/nic.bin"),
		},
	}
	tests := []struct {
		ct   ComponentType
		want string
	}{
		// The DPU PLDM bundle is per-PSID, not single-valued, so SpecURLForComponent returns "".
		{ComponentTypeFwBundle, ""},
		{ComponentTypePlatformFwBundle, "https://x/platform-pldm"},
		{ComponentTypeOSISO, "https://x/iso"},
		{ComponentTypeNicFw, "https://x/nic.bin"},
	}
	for _, tt := range tests {
		if got := SpecURLForComponent(bfs, tt.ct); got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestPldmFwBundles(t *testing.T) {
	if got := PldmFwBundles(&provisioningv1.BlueFieldSoftware{}); got != nil {
		t.Fatalf("nil spec should yield nil, got %v", got)
	}
	bfs := &provisioningv1.BlueFieldSoftware{
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{
				"MT_0000001665": "https://x/a.fwpkg",
				"MT_0000001775": "https://x/b.fwpkg",
				"MT_empty":      "",
			},
		},
	}
	got := PldmFwBundles(bfs)
	if len(got) != 2 || got["MT_0000001665"] != "https://x/a.fwpkg" || got["MT_0000001775"] != "https://x/b.fwpkg" {
		t.Fatalf("unexpected bundles (empty URLs should be dropped): %v", got)
	}
}

func TestComponentDownloadFilename(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{Name: "bf4", Namespace: "dpf-operator-system"},
	}
	url := "https://nbu-nfs.example.com/ISO/bf4-os-doca-bundle-3.3.0-321_26.01_ubuntu-24.04_64k.iso"
	wantISO := "dpf-operator-system-bf4-bf4-os-doca-bundle-3.3.0-321_26.01_ubuntu-24.04_64k.iso"
	if got := ComponentDownloadFilename(bfs, ComponentTypeOSISO, url); got != wantISO {
		t.Fatalf("got %q", got)
	}
	if got := ComponentDownloadFilename(bfs, ComponentTypeNicFw, "https://x/fw/nic.bin"); got != "dpf-operator-system-bf4-nic.bin" {
		t.Fatalf("got %q", got)
	}
	if got := ComponentDownloadFilename(bfs, ComponentTypeOSISO, "ref-only"); got != GenerateComponentTaskName(*bfs, ComponentTypeOSISO) {
		t.Fatalf("non-URL should fall back to default, got %q", got)
	}
}

func TestPldmComponentFilename(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{Name: "bf4", Namespace: "dpf-operator-system"},
	}
	// PSID is always in the name so different PSIDs sharing a URL basename never collide.
	if got := PldmComponentFilename(bfs, "MT_0000001665", "https://x/fw/dpu-pldm.fwpkg"); got != "dpf-operator-system-bf4-MT_0000001665-dpu-pldm.fwpkg" {
		t.Fatalf("got %q", got)
	}
	if got := PldmComponentFilename(bfs, "MT_0000001775", "https://x/fw/dpu-pldm.fwpkg"); got != "dpf-operator-system-bf4-MT_0000001775-dpu-pldm.fwpkg" {
		t.Fatalf("got %q", got)
	}
	if got := PldmComponentFilename(bfs, "MT_0000001665", "ref-only"); got != PldmTaskName(bfs, "MT_0000001665") {
		t.Fatalf("non-URL should fall back to task name, got %q", got)
	}
}
