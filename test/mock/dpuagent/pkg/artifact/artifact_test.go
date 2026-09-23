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

package artifact

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	bfbutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	trimmedBFB  = "../../testdata/bf3-trimmed.bfb"
	trimmedPLDM = "../../testdata/bf4-trimmed.fwpkg"
)

// testUserData renders the production cloud-init user-data for a fake DPU.
func testUserData(t *testing.T) string {
	t.Helper()
	flavor := &provisioningv1.DPUFlavor{ObjectMeta: metav1.ObjectMeta{Name: "flavor", Namespace: "dpf-operator-system"}}
	params := cloudinit.Params{
		DPUHostName:            "dpu-1",
		KubeadmSecretName:      "dpu-1-kubeadm-join",
		KubeadmSecretNamespace: "dpf-operator-system",
		BootstrapKubeconfig:    "apiVersion: v1\nkind: Config\nclusters: []\n",
		ControlPlaneMTU:        1500,
		DPUName:                "dpu-1",
		DPUNamespace:           "dpf-operator-system",
		DPUUID:                 "0f1e2d3c-0000-4000-8000-000000000001",
		DPUType:                provisioningv1.DPUTypeBlueField3,
		DPUAgentRepoURL:        "https://registry.example/repo",
		RedfishInterface:       true,
		CATrustBundle:          "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
	}
	if err := params.ApplyFlavor(flavor); err != nil {
		t.Fatalf("ApplyFlavor: %v", err)
	}
	file, err := cloudinit.GenerateUserData(params)
	if err != nil {
		t.Fatalf("GenerateUserData: %v", err)
	}
	return file.Content
}

// testBFCFG wraps user-data the way pkg/bfcfg's default template does inside bfb_modify_os().
func testBFCFG(userData string) []byte {
	var b strings.Builder
	b.WriteString("ubuntu_PASSWORD='x'\n\nBMC_PASSWORD=\"abcd\"\n\nbfb_modify_os()\n{\n")
	b.WriteString("cat << \\EOF > /mnt/etc/cloud/cloud.cfg.d/dpf.cfg\nnetwork:\n  config: disabled\nEOF\n")
	b.WriteString("cat << \\EOF > /mnt/var/lib/cloud/seed/nocloud-net/user-data\n")
	b.WriteString(strings.TrimSuffix(userData, "\n"))
	b.WriteString("\nEOF\n}\n")
	return []byte(b.String())
}

func TestBFBSplitterAndInventory(t *testing.T) {
	bfb, err := os.ReadFile(trimmedBFB)
	if err != nil {
		t.Fatal(err)
	}
	bfcfg := testBFCFG(testUserData(t))
	stream := append(append([]byte{}, bfb...), bfcfg...)

	for _, chunk := range []int{1, 7, 24, 4096, len(stream)} {
		s := NewBFBSplitter()
		for i := 0; i < len(stream); i += chunk {
			end := i + chunk
			if end > len(stream) {
				end = len(stream)
			}
			if _, err := s.Write(stream[i:end]); err != nil {
				t.Fatalf("chunk %d: Write: %v", chunk, err)
			}
		}
		if err := s.Finish(); err != nil {
			t.Fatalf("chunk %d: Finish: %v", chunk, err)
		}
		if s.Images() != 49 || s.BFBSize() != int64(len(bfb)) {
			t.Fatalf("chunk %d: images=%d bfbSize=%d", chunk, s.Images(), s.BFBSize())
		}
		if !bytes.Equal(s.BFCFG(), bfcfg) {
			t.Fatalf("chunk %d: bf.cfg trailer differs", chunk)
		}
		got, err := s.Inventory().Versions()
		if err != nil {
			t.Fatalf("chunk %d: Versions: %v", chunk, err)
		}
		want, err := bfbutil.VersionFromBFBFile(trimmedBFB)
		if err != nil {
			t.Fatalf("VersionFromBFBFile: %v", err)
		}
		if *got != *want {
			t.Fatalf("chunk %d: versions %+v differ from controller %+v", chunk, *got, *want)
		}
		if got.UEFI != "4.15.0-19-g37c6f5adb2" || got.BSP != "4.15.0.13977" || got.ATF != "4.15.0-4-g419fbf393" || got.DOCA != "3.4.0" {
			t.Fatalf("unexpected versions %+v", *got)
		}
	}
}

func TestBFBSplitterWithoutTrailer(t *testing.T) {
	bfb, err := os.ReadFile(trimmedBFB)
	if err != nil {
		t.Fatal(err)
	}
	s := NewBFBSplitter()
	if _, err := s.Write(bfb); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	if len(s.BFCFG()) != 0 {
		t.Fatalf("expected empty trailer, got %d bytes", len(s.BFCFG()))
	}
	truncated := NewBFBSplitter()
	if _, err := truncated.Write(bfb[:len(bfb)-3]); err != nil {
		t.Fatal(err)
	}
	if err := truncated.Finish(); err == nil {
		t.Fatal("expected truncated stream to fail")
	}
}

func TestUserDataFromBFCFGAndAgentFiles(t *testing.T) {
	userData := testUserData(t)
	got, err := UserDataFromBFCFG(testBFCFG(userData))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.TrimSuffix(userData, "\n") {
		t.Fatalf("user-data mismatch:\n%s", got)
	}
	files, err := AgentFilesFromUserData(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files.AgentConf, "--dpu-name=dpu-1") || !strings.Contains(files.AgentConf, "--dpu-uid=0f1e2d3c-0000-4000-8000-000000000001") {
		t.Fatalf("unexpected dpuagent.conf:\n%s", files.AgentConf)
	}
	if !strings.HasPrefix(files.BootstrapKubeconfig, "apiVersion: v1") {
		t.Fatalf("unexpected bootstrap kubeconfig:\n%s", files.BootstrapKubeconfig)
	}
	if !strings.Contains(files.CATrustBundle, "BEGIN CERTIFICATE") {
		t.Fatalf("unexpected CA bundle:\n%s", files.CATrustBundle)
	}
	if _, err := UserDataFromBFCFG([]byte("echo no heredoc\n")); err == nil {
		t.Fatal("expected error without heredoc")
	}
}

func TestUserDataFromSeedISO(t *testing.T) {
	userData := testUserData(t)
	dir := t.TempDir()
	isoPath, err := dutil.MkIso(filepath.Join(dir, "seed"), "cidata", []dutil.IsoRootFile{
		{Name: "user-data", Data: []byte(userData)},
		{Name: "meta-data", Data: []byte("instance-id: dpu-1\n")},
		{Name: "network-config", Data: []byte("network:\n  version: 2\n")},
	})
	if err != nil {
		t.Fatalf("MkIso: %v", err)
	}
	iso, err := os.ReadFile(isoPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UserDataFromSeedISO(iso, filepath.Join(dir, "staged.iso"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != userData {
		t.Fatalf("user-data mismatch:\n%s", got)
	}
	if _, err := AgentFilesFromUserData(got); err != nil {
		t.Fatal(err)
	}
}

func TestParsePLDM(t *testing.T) {
	f, err := os.Open(trimmedPLDM)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	pkg, err := ParsePLDM(f)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.FormatRevision != 1 || pkg.HeaderSize != 582 || pkg.PackageVersion != "BF4-DPU_0000_260901.11024501.0_custom" {
		t.Fatalf("unexpected header %+v", pkg)
	}
	wantImages := []string{
		"ERoT_02.00.0044.0000_n05_image.bin",
		"BMC_BF4-BMC_BF4-26.07-0005_image.bin",
		"SBIOS_SKU_903_26.08-0007_image.bin",
		"CX9_MT_0000001775_82.48.4004_image.bin",
	}
	if len(pkg.Components) != len(wantImages) {
		t.Fatalf("got %d components", len(pkg.Components))
	}
	for i, c := range pkg.Components {
		if c.ImageName != wantImages[i] {
			t.Errorf("component %d image %q, want %q", i, c.ImageName, wantImages[i])
		}
		if c.Offset != uint32(582+16*i) || c.Size != 16 {
			t.Errorf("component %d offset/size %d/%d", i, c.Offset, c.Size)
		}
	}
	want := PLDMVersions{BMC: "BF4-26.07-0005", ERoT: "02.00.0044.0000_n05", SBIOS: "26.08-0007", NIC: "82.48.4004", NICPSID: "MT_0000001775"}
	if pkg.Versions != want {
		t.Fatalf("versions %+v, want %+v", pkg.Versions, want)
	}
	if _, err := ParsePLDM(bytes.NewReader([]byte("not a pldm package at all, definitely"))); err == nil {
		t.Fatal("expected error for bad identifier")
	}
}

func TestDownload(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 3<<20)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "?a.bfb,bfcfg/b.cfg?/bfb-to-install" {
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	var last, total int64
	buf := NewLimitedBuffer(len(payload))
	n, err := Download(context.Background(), host+"/bfb/??a.bfb,bfcfg/b.cfg?/bfb-to-install", buf, func(d, tot int64) { last, total = d, tot })
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || last != n || total != n || !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("n=%d last=%d total=%d", n, last, total)
	}
	if _, err := Download(context.Background(), host+"/missing", NewLimitedBuffer(10), nil); err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestPLDMReaderSkip(t *testing.T) {
	p := &pldmReader{r: bufio.NewReader(bytes.NewReader([]byte{1, 2, 3, 4, 5}))}
	if err := p.skip(3); err != nil {
		t.Fatal(err)
	}
	if p.pos != 3 {
		t.Fatalf("pos = %d, want 3", p.pos)
	}
	b, err := p.u8()
	if err != nil || b != 4 {
		t.Fatalf("u8 after skip = %d, %v; want 4", b, err)
	}
	if err := p.skip(10); err == nil {
		t.Fatal("skipping past the end of the package must fail")
	}
}
