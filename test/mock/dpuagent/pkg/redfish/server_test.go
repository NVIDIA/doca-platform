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

package redfish

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
)

const (
	testSerial    = "MT2616606M3H"
	testPSID      = "MT_0000001775"
	oldBMCVersion = "BF-24.07-10"
)

type recordingSupervisor struct {
	mu       sync.Mutex
	boots    [][]byte
	reboots  int
	notified chan struct{}
}

func newRecordingSupervisor() *recordingSupervisor {
	return &recordingSupervisor{notified: make(chan struct{}, 16)}
}

func (r *recordingSupervisor) Boot(_ context.Context, artifact []byte) {
	r.mu.Lock()
	r.boots = append(r.boots, artifact)
	r.mu.Unlock()
	r.notified <- struct{}{}
}

func (r *recordingSupervisor) Reboot(context.Context) {
	r.mu.Lock()
	r.reboots++
	r.mu.Unlock()
	r.notified <- struct{}{}
}

func (r *recordingSupervisor) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.notified:
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor was not notified")
	}
}

func startServer(t *testing.T, dpuType config.DPUType, firmware config.Firmware) (*State, *recordingSupervisor, string) {
	t.Helper()
	state := NewState(Options{
		Personality:  ForType(dpuType),
		SerialNumber: testSerial,
		PSID:         testPSID,
		PF0MAC:       config.PF0MAC(testSerial),
		Firmware:     firmware,
	})
	sup := newRecordingSupervisor()
	srv, err := NewServer(state, sup)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return state, sup, "https://" + addr.String()
}

func defaultFirmware() config.Firmware {
	return config.Firmware{BMC: config.BMCMinSupportedVersion, ERoT: config.PlaceholderVersion, UEFI: config.PlaceholderVersion, NIC: config.PlaceholderVersion}
}

func mustOK(t *testing.T, what string, statusCode int, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("%s: status %d", what, statusCode)
	}
}

func TestBF3Discovery(t *testing.T) {
	_, _, url := startServer(t, config.DPUTypeBF3, defaultFirmware())
	raw, err := rfclient.NewRawClient(url)
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := raw.GetRootService()
	if err != nil || root.IsBF4() {
		t.Fatalf("root service: %+v %v", root, err)
	}
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF3BMCUser, "whatever")
	if err != nil {
		t.Fatal(err)
	}
	if c.IsBF4 {
		t.Fatal("BF3 personality detected as BF4")
	}
	resp, chassis, err := c.GetChassis()
	mustOK(t, "GetChassis", resp.StatusCode(), err)
	if chassis.SerialNumber != testSerial || chassis.GetBlueFieldVersion() != provisioningv1.DPUTypeBlueField3 {
		t.Fatalf("unexpected chassis %+v", chassis)
	}
	psid, err := c.GetPSID()
	if err != nil || psid != testPSID {
		t.Fatalf("GetPSID: %q %v", psid, err)
	}
	resp, pf0, err := c.GetNetworkDeviceFunction("eth0f0")
	mustOK(t, "GetNetworkDeviceFunction", resp.StatusCode(), err)
	if pf0.Ethernet.MACAddress != config.PF0MAC(testSerial) {
		t.Fatalf("unexpected PF0 %+v", pf0)
	}
	resp, desc, err := c.GetProductDescription()
	mustOK(t, "GetProductDescription", resp.StatusCode(), err)
	if desc.Mode == nil || *desc.Mode != rfclient.DpuMode {
		t.Fatalf("expected DpuMode, got %+v", desc)
	}
	resp, mgr, err := c.GetBmcManager()
	mustOK(t, "GetBmcManager", resp.StatusCode(), err)
	if mgr.FirmwareVersion != config.BMCMinSupportedVersion {
		t.Fatalf("unexpected manager %+v", mgr)
	}
	if _, err := time.Parse(time.RFC3339, mgr.DateTime); err != nil {
		t.Fatal(err)
	}
	// Any password is accepted: VerifyBMCCredential succeeds with the factory default.
	if _, user, err := rfclient.VerifyBMCCredential(url, rfclient.BMCDefaultPassword); err != nil || user != rfclient.BF3BMCUser {
		t.Fatalf("VerifyBMCCredential: %q %v", user, err)
	}
	resp, _, err = c.FactoryResetBMC()
	mustOK(t, "FactoryResetBMC", resp.StatusCode(), err)
	resp, _, err = c.SetRedfishUserPassword(rfclient.BF3BMCUser, "new-password")
	mustOK(t, "SetRedfishUserPassword", resp.StatusCode(), err)
	resp, _, err = c.EnableMTLS()
	mustOK(t, "EnableMTLS", resp.StatusCode(), err)
}

func TestBF3BMCFirmwareUpgrade(t *testing.T) {
	fw := defaultFirmware()
	fw.BMC = oldBMCVersion
	_, _, url := startServer(t, config.DPUTypeBF3, fw)
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	_, v, err := c.CheckBMCFirmware()
	if err != nil || v.Version != oldBMCVersion {
		t.Fatalf("CheckBMCFirmware: %+v %v", v, err)
	}
	fwFile, err := os.CreateTemp(t.TempDir(), "bmc-*.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fwFile.Write(bytes.Repeat([]byte{0xaa}, 4096)); err != nil {
		t.Fatal(err)
	}
	if _, err := fwFile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	resp, task, err := c.UpdateBMCFirmware(fwFile)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("UpdateBMCFirmware: %v %v", resp.Status(), err)
	}
	resp, prog, err := c.CheckTaskProgress(task.ID)
	mustOK(t, "CheckTaskProgress", resp.StatusCode(), err)
	if prog.TaskState != TaskStateCompleted || prog.PercentComplete != 100 {
		t.Fatalf("unexpected task %+v", prog)
	}
	// The version only changes after Manager.Reset, like a real BMC.
	_, v, _ = c.CheckBMCFirmware()
	if v.Version != oldBMCVersion {
		t.Fatalf("version changed before reset: %s", v.Version)
	}
	resp, _, err = c.ResetBMC()
	mustOK(t, "ResetBMC", resp.StatusCode(), err)
	_, v, _ = c.CheckBMCFirmware()
	if v.Version != config.BMCMinSupportedVersion {
		t.Fatalf("version after reset %s", v.Version)
	}
}

func TestBF3ConfigFWParametersAndInstall(t *testing.T) {
	state, sup, url := startServer(t, config.DPUTypeBF3, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	resp, _, err := c.DisableHostRshim()
	mustOK(t, "DisableHostRshim", resp.StatusCode(), err)
	enabled, _, err := c.GetBMCRShimEnabled()
	if err != nil || enabled {
		t.Fatalf("rshim before enable: %v %v", enabled, err)
	}
	resp, _, err = c.EnableBMCRShim()
	mustOK(t, "EnableBMCRShim", resp.StatusCode(), err)
	enabled, _, err = c.GetBMCRShimEnabled()
	if err != nil || !enabled {
		t.Fatalf("rshim after enable: %v %v", enabled, err)
	}

	// Registry serving the BFB + bf.cfg concat stream.
	bfb, err := os.ReadFile("../../testdata/bf3-trimmed.bfb")
	if err != nil {
		t.Fatal(err)
	}
	bfcfg := []byte("ubuntu_PASSWORD='x'\nbfb_modify_os()\n{\ncat << \\EOF > /mnt/var/lib/cloud/seed/nocloud-net/user-data\n#cloud-config\nwrite_files: []\nEOF\n}\n")
	stream := append(append([]byte{}, bfb...), bfcfg...)
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bfb/" || !strings.Contains(r.URL.RawQuery, "bfb-to-install") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(stream)))
		_, _ = w.Write(stream)
	}))
	defer registry.Close()
	imageURI := strings.TrimPrefix(registry.URL, "https://") + "/bfb/??bf-bundle.bfb,bfcfg/dpu-1.cfg?/bfb-to-install"

	resp, task, err := c.InstallBFB(imageURI)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("InstallBFB: %v %v", resp.Status(), err)
	}
	sup.wait(t)
	resp, prog, err := c.CheckTaskProgress(task.ID)
	mustOK(t, "CheckTaskProgress", resp.StatusCode(), err)
	if prog.TaskState != TaskStateCompleted || prog.PercentComplete != 100 {
		t.Fatalf("unexpected task %+v", prog)
	}
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if len(sup.boots) != 1 || !bytes.Equal(sup.boots[0], bfcfg) {
		t.Fatalf("supervisor boots %d", len(sup.boots))
	}
	_, uefi, _ := c.CheckDPUUEFI()
	_, bsp, _ := c.CheckDPUBSP()
	_, os, _ := c.CheckDPUOS()
	if uefi.Version != "4.15.0-19-g37c6f5adb2" || bsp.Version != "4.15.0.13977" || os.Version != "3.4.0" {
		t.Fatalf("installed versions uefi=%s bsp=%s os=%s", uefi.Version, bsp.Version, os.Version)
	}
	if v, _ := state.FirmwareVersion("DPU_BOARD"); v != testPSID {
		t.Fatalf("DPU_BOARD %s", v)
	}

	// A broken download ends in an Exception task with a message.
	resp, task, err = c.InstallBFB(strings.TrimPrefix(registry.URL, "https://") + "/missing")
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("InstallBFB: %v %v", resp.Status(), err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, prog, err = c.CheckTaskProgress(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if prog.TaskState == TaskStateException {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not fail: %+v", prog)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(prog.Messages) == 0 {
		t.Fatal("exception task without messages")
	}
}

func TestBF3ResetPaths(t *testing.T) {
	state, sup, url := startServer(t, config.DPUTypeBF3, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ForceRestartDPUArm(); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	if _, err := c.ForceResetSOC(); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	sup.mu.Lock()
	reboots := sup.reboots
	sup.mu.Unlock()
	if reboots != 2 {
		t.Fatalf("reboots %d", reboots)
	}
	state.SetPowerOff()
	_, system, err := c.GetSystem()
	if err != nil || system.PowerState != "Off" || system.Status.State != "StandbyOffline" {
		t.Fatalf("system after power off %+v %v", system, err)
	}
	state.SetPowerOn()
	_, system, _ = c.GetSystem()
	if system.PowerState != "On" || system.BootProgress.OemLastState != OemLastStateOSUp {
		t.Fatalf("system after power on %+v", system)
	}
	resp, sel, err := c.GetSELEntries(context.Background())
	mustOK(t, "GetSELEntries", resp.StatusCode(), err)
	if len(sel.Members) != 0 {
		t.Fatalf("unexpected SEL %+v", sel)
	}
}

func TestBF4Discovery(t *testing.T) {
	state, _, url := startServer(t, config.DPUTypeBF4, defaultFirmware())
	raw, err := rfclient.NewRawClient(url)
	if err != nil {
		t.Fatal(err)
	}
	if _, root, err := raw.GetRootService(); err != nil || !root.IsBF4() {
		t.Fatalf("root service: %+v %v", root, err)
	}
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsBF4 {
		t.Fatal("BF4 personality not detected")
	}
	resp, chassis, err := c.GetChassis()
	mustOK(t, "GetChassis", resp.StatusCode(), err)
	if chassis.GetBlueFieldVersion() != provisioningv1.DPUTypeBlueField4 {
		t.Fatalf("unexpected chassis %+v", chassis)
	}
	if psid, err := c.GetPSID(); err != nil || psid != testPSID {
		t.Fatalf("GetPSID: %q %v", psid, err)
	}
	resp, pf0, err := c.GetNetworkDeviceFunction("0")
	mustOK(t, "GetNetworkDeviceFunction", resp.StatusCode(), err)
	if pf0.Ethernet.PermanentMACAddress != config.PF0MAC(testSerial) {
		t.Fatalf("unexpected PF0 %+v", pf0)
	}
	resp, desc, err := c.GetProductDescription()
	mustOK(t, "GetProductDescription", resp.StatusCode(), err)
	if desc.Mode != nil {
		t.Fatalf("BF4 must not expose Mode: %+v", desc)
	}
	resp, _, err = c.SetHostPrivilegeRestricted()
	mustOK(t, "SetHostPrivilegeRestricted", resp.StatusCode(), err)
	if state.HostPrivilegeMode() != "Restricted" {
		t.Fatalf("host privilege %s", state.HostPrivilegeMode())
	}
}

func TestBF4FirmwareUpdate(t *testing.T) {
	state, _, url := startServer(t, config.DPUTypeBF4, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	// Firmware update: versions differ from the bundle, upload the trimmed PLDM package.
	resp, erot, err := c.GetErotChassis()
	mustOK(t, "GetErotChassis", resp.StatusCode(), err)
	if erot.Oem["Nvidia"].(map[string]interface{})["BackgroundCopyStatus"] != "Completed" {
		t.Fatalf("unexpected ERoT chassis %+v", erot)
	}
	pldm, err := os.Open("../../testdata/bf4-trimmed.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pldm.Close() }()
	resp, task, err := c.UpdateBluefieldFirmwareMultipart(pldm, true)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("UpdateBluefieldFirmwareMultipart: %v %v", resp.Status(), err)
	}
	_, prog, err := c.CheckTaskProgress(task.ID)
	if err != nil || prog.TaskState != TaskStateCompleted {
		t.Fatalf("task %+v %v", prog, err)
	}
	if _, err := c.ArmShutdown(); err != nil {
		t.Fatal(err)
	}
	_, system, _ := c.GetSystem()
	if system.PowerState != "Paused" {
		t.Fatalf("expected Paused after ArmShutdown, got %s", system.PowerState)
	}
	resp, err = c.ActivatePendingBundle()
	mustOK(t, "ActivatePendingBundle", resp.StatusCode(), err)
	// Versions switch only when the host reboots.
	if _, info, err := c.CheckBMCFirmware(); err != nil || info.Version != config.BMCMinSupportedVersion {
		t.Fatalf("BMC version changed before reboot: %+v %v", info, err)
	}
	if !state.ApplyActivatedBundle() {
		t.Fatal("no activated bundle")
	}
	_, bmc, _ := c.CheckBMCFirmware()
	_, erotFW, _ := c.CheckBMCEROTFW()
	_, sbios, _ := c.CheckDPUUEFI()
	_, nic, _ := c.CheckDPUNIC()
	if bmc.Version != "BF4-26.07-0005" || erotFW.Version != "02.00.0044.0000_n05" || sbios.Version != "26.08-0007" || nic.Version != "82.48.4004" {
		t.Fatalf("bundle versions bmc=%s erot=%s sbios=%s nic=%s", bmc.Version, erotFW.Version, sbios.Version, nic.Version)
	}
}

func TestBF4OSInstall(t *testing.T) {
	_, sup, url := startServer(t, config.DPUTypeBF4, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	// OS install: ISO, seed.iso, virtual media, boot target, ArmReset -> Boot(seed.iso).
	osISO := bytes.Repeat([]byte("iso"), 100000)
	seedISO := []byte("cidata-seed-iso-content")
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/os.iso"):
			_, _ = w.Write(osISO)
		case strings.HasSuffix(r.URL.Path, "/seed.iso"):
			_, _ = w.Write(seedISO)
		default:
			http.NotFound(w, r)
		}
	}))
	defer registry.Close()
	host := strings.TrimPrefix(registry.URL, "https://")
	if _, err := c.CheckOSImage(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckConfigImage(); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		install func(string) (interface{}, *rfclient.TaskInfo, error)
		uri     string
	}{
		{func(u string) (interface{}, *rfclient.TaskInfo, error) {
			r, t, e := c.InstallBluefieldArmImage(u)
			return r, t, e
		}, host + "/bfb/bf4/os.iso"},
		{func(u string) (interface{}, *rfclient.TaskInfo, error) {
			r, t, e := c.InstallBluefieldArmConfig(u)
			return r, t, e
		}, host + "/bfb/user-data/ns_dpu_uid/seed.iso"},
	} {
		_, task, err := step.install(step.uri)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			_, prog, err := c.CheckTaskProgress(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if prog.TaskState == TaskStateException {
				t.Fatalf("task failed: %+v", prog.Messages)
			}
			if prog.TaskState == TaskStateCompleted {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("task not completed: %+v", prog)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if _, err := c.InsertVirtualMediaImage(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InsertVirtualMediaConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetBootTarget("None", false); err != nil {
		t.Fatal(err)
	}
	if _, settings, err := c.GetSettings(); err != nil || settings.Boot.BootSourceOverrideTarget != "None" {
		t.Fatalf("settings %+v %v", settings, err)
	}
	if _, err := c.SetBootTarget("Usb", true); err != nil {
		t.Fatal(err)
	}
	if _, settings, err := c.GetSettings(); err != nil || settings.Boot.BootSourceOverrideTarget != "Usb" || settings.Boot.BootSourceOverrideEnabled != "Once" {
		t.Fatalf("settings %+v %v", settings, err)
	}
	if _, err := c.ChassisReset(); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	sup.mu.Lock()
	boots, reboots := sup.boots, sup.reboots
	sup.mu.Unlock()
	if len(boots) != 1 || !bytes.Equal(boots[0], seedISO) || reboots != 0 {
		t.Fatalf("boots=%d reboots=%d", len(boots), reboots)
	}
	// A second ArmReset with no pending seed.iso is a plain reboot.
	if _, err := c.ChassisReset(); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if sup.reboots != 1 {
		t.Fatalf("reboots %d", sup.reboots)
	}
}

// TestCertificateLifecycle walks the controller's mTLS bootstrap: truststore reconcile,
// GenerateCSR, install of the CA-signed certificate, and then a verified connection that pins the
// CA and the BMC IP like rfclient.NewTLSClient does.
func TestCertificateLifecycle(t *testing.T) {
	_, _, url := startServer(t, config.DPUTypeBF3, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	caKey, caCert, caPEM := newTestCA(t)

	// Truststore reconcile by fingerprint.
	certs, err := c.ListTruststoreCerts()
	if err != nil || len(certs) != 0 {
		t.Fatalf("initial truststore %+v %v", certs, err)
	}
	resp, _, err := c.InstallCert(string(caPEM))
	mustOK(t, "InstallCert", resp.StatusCode(), err)
	certs, err = c.ListTruststoreCerts()
	if err != nil || len(certs) != 1 {
		t.Fatalf("truststore after install %+v %v", certs, err)
	}
	resp, _, err = c.DeleteTruststoreCert(certs[0].URI)
	mustOK(t, "DeleteTruststoreCert", resp.StatusCode(), err)
	if certs, _ = c.ListTruststoreCerts(); len(certs) != 0 {
		t.Fatalf("truststore after delete %+v", certs)
	}

	// GenerateCSR must be called on the server directly: the client hard-codes port 443.
	bmcIP := "127.0.0.1"
	csrResp, err := c.R().SetHeader("Content-Type", "application/json").SetBody(map[string]interface{}{
		"CommonName":       bmcIP,
		"AlternativeNames": []string{"IP: " + bmcIP, "DNS: localhost", "IP: 127.0.0.1"},
	}).Post(rfclient.APIGenerateCSR)
	if err != nil || csrResp.StatusCode() != http.StatusOK {
		t.Fatalf("GenerateCSR: %v %v", csrResp.Status(), err)
	}
	var csrInfo rfclient.CSRInfo
	if err := jsonUnmarshal(csrResp.Body(), &csrInfo); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(csrInfo.CSRString))
	if block == nil {
		t.Fatal("no CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if csr.Subject.CommonName != bmcIP || len(csr.IPAddresses) == 0 || !csr.IPAddresses[0].Equal(net.ParseIP(bmcIP)) {
		t.Fatalf("unexpected CSR subject %+v SANs %v", csr.Subject, csr.IPAddresses)
	}
	leafPEM := signCSR(t, caKey, caCert, csr)

	// A certificate for a different key is rejected.
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherCSRDER, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: bmcIP}}, otherKey)
	otherCSR, _ := x509.ParseCertificateRequest(otherCSRDER)
	resp, _, err = c.ReplaceServerCert(string(signCSR(t, caKey, caCert, otherCSR)))
	if err != nil || resp.StatusCode() != http.StatusInternalServerError {
		t.Fatalf("ReplaceServerCert with foreign key: %v %v", resp.Status(), err)
	}
	resp, _, err = c.ReplaceServerCert(string(leafPEM))
	mustOK(t, "ReplaceServerCert", resp.StatusCode(), err)
	resp, served, err := c.GetServerCert()
	mustOK(t, "GetServerCert", resp.StatusCode(), err)
	if strings.TrimSpace(served.CertificateString) != strings.TrimSpace(string(leafPEM)) {
		t.Fatal("served certificate is not the installed one")
	}

	// Verified connection: chain to the CA and pin the BMC IP, as NewTLSClient does.
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	conn, err := tls.Dial("tcp", strings.TrimPrefix(url, "https://"), &tls.Config{RootCAs: pool, ServerName: bmcIP, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("verified TLS dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := rfclient.VerifyBMCIdentity(conn.ConnectionState().PeerCertificates[0], bmcIP); err != nil {
		t.Fatal(err)
	}
}

func newTestCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dpf-provisioning-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return key, cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func signCSR(t *testing.T, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, csr *x509.CertificateRequest) []byte {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  csr.IPAddresses,
		DNSNames:     csr.DNSNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestResponseDelay(t *testing.T) {
	const minDelay, maxDelay = 200 * time.Millisecond, 400 * time.Millisecond
	state := NewState(Options{Personality: ForType(config.DPUTypeBF3), SerialNumber: testSerial, PSID: testPSID, PF0MAC: config.PF0MAC(testSerial), Firmware: defaultFirmware()})
	srv, err := NewServer(state, newRecordingSupervisor(), WithResponseDelay(minDelay, maxDelay))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	start := time.Now()
	srv.httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/redfish/v1", nil))
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if elapsed < minDelay || elapsed > maxDelay+time.Second {
		t.Fatalf("response took %v, want between %v and %v", elapsed, minDelay, maxDelay)
	}
}

func TestResponseDelayOverrides(t *testing.T) {
	const minDelay, maxDelay = 200 * time.Millisecond, 400 * time.Millisecond
	newState := func() *State {
		return NewState(Options{Personality: ForType(config.DPUTypeBF3), SerialNumber: testSerial, PSID: testPSID, PF0MAC: config.PF0MAC(testSerial), Firmware: defaultFirmware()})
	}
	timed := func(t *testing.T, srv *Server, method, path string) (int, time.Duration) {
		t.Helper()
		rec := httptest.NewRecorder()
		start := time.Now()
		srv.httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code, time.Since(start)
	}

	t.Run("override and default", func(t *testing.T) {
		srv, err := NewServer(newState(), newRecordingSupervisor(),
			WithResponseDelay(minDelay, maxDelay),
			WithResponseDelayOverrides([]config.ResponseDelayOverride{
				// Task: no delay at all, although the default says otherwise.
				{Name: "Task"},
				// SecureBoot: only GET is delayed longer; PATCH keeps the default.
				{Name: "SecureBoot", Methods: []string{"GET"}, ResponseDelay: config.ResponseDelay{MinSeconds: 1, MaxSeconds: 1}},
			}))
		if err != nil {
			t.Fatal(err)
		}
		if code, elapsed := timed(t, srv, http.MethodGet, "/redfish/v1/TaskService/Tasks/0"); code != http.StatusNotFound || elapsed >= minDelay {
			t.Fatalf("Task override: status %d, took %v, want no delay", code, elapsed)
		}
		if code, elapsed := timed(t, srv, http.MethodGet, "/redfish/v1/Systems/Bluefield/SecureBoot"); code != http.StatusOK || elapsed < time.Second {
			t.Fatalf("SecureBoot GET override: status %d, took %v, want at least 1s", code, elapsed)
		}
		if code, elapsed := timed(t, srv, http.MethodGet, "/redfish/v1"); code != http.StatusOK || elapsed < minDelay || elapsed >= time.Second {
			t.Fatalf("default delay: status %d, took %v, want between %v and 1s", code, elapsed, minDelay)
		}
	})

	t.Run("star covers every method", func(t *testing.T) {
		srv, err := NewServer(newState(), newRecordingSupervisor(),
			WithResponseDelayOverrides([]config.ResponseDelayOverride{
				{Name: "SecureBoot", Methods: []string{"*"}, ResponseDelay: config.ResponseDelay{MinSeconds: 1, MaxSeconds: 1}},
			}))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{"GET /redfish/v1/Systems/{system}/SecureBoot", "PATCH /redfish/v1/Systems/{system}/SecureBoot"} {
			if d := srv.delayFor(p); d.min != time.Second || d.max != time.Second {
				t.Errorf("%s: got %+v", p, d)
			}
		}
		if d := srv.delayFor("GET /redfish/v1"); d.max != 0 {
			t.Errorf("ServiceRoot must keep the (empty) default, got %+v", d)
		}
	})

	t.Run("rejected at construction", func(t *testing.T) {
		for name, overrides := range map[string][]config.ResponseDelayOverride{
			"unknown operation": {{Name: "Nope"}},
			"method not served": {{Name: "Task", Methods: []string{"POST"}}},
			"overlap explicit":  {{Name: "SecureBoot", Methods: []string{"GET"}}, {Name: "SecureBoot", Methods: []string{"GET", "PATCH"}}},
			"overlap star":      {{Name: "SecureBoot"}, {Name: "SecureBoot", Methods: []string{"PATCH"}}},
		} {
			if _, err := NewServer(newState(), newRecordingSupervisor(), WithResponseDelayOverrides(overrides)); err == nil {
				t.Errorf("%s: expected an error", name)
			} else {
				t.Logf("%s: %v", name, err)
			}
		}
	})
}

// TestRouteTableNames checks the naming rule of the route table: every name maps to one path
// family, and a path (ignoring the method) has exactly one name.
func TestRouteTableNames(t *testing.T) {
	nameByPath := map[string]string{}
	for _, rt := range routes {
		if rt.name == "" || rt.method == "" || rt.path == "" || rt.handler == nil {
			t.Fatalf("incomplete route %+v", rt)
		}
		if prev, ok := nameByPath[rt.path]; ok && prev != rt.name {
			t.Errorf("%s is named both %s and %s", rt.path, prev, rt.name)
		}
		nameByPath[rt.path] = rt.name
	}
}
