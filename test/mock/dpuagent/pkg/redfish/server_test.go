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
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/phasedelay"
)

const (
	testSerial    = "MT2616606M3H"
	testPSID      = "MT_0000001775"
	oldBMCVersion = "BF-24.07-10"
)

type recordingSupervisor struct {
	mu           sync.Mutex
	boots        [][]byte
	reboots      int
	rebootPhases []provisioningv1.DPUPhase
	powerOffs    int
	notified     chan struct{}
	// state, when set, is powered off by PowerOff like the agent supervisor does.
	state *State
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

func (r *recordingSupervisor) Reboot(_ context.Context, phase provisioningv1.DPUPhase) {
	r.mu.Lock()
	r.reboots++
	r.rebootPhases = append(r.rebootPhases, phase)
	r.mu.Unlock()
	r.notified <- struct{}{}
}

func (r *recordingSupervisor) PowerOff() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.powerOffs++
	if r.state != nil {
		r.state.SetPowerOff()
	}
}

func (r *recordingSupervisor) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.notified:
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor was not notified")
	}
}

func startServer(t *testing.T, dpuType config.DPUType, firmware config.Firmware, opts ...ServerOption) (*State, *recordingSupervisor, string) {
	t.Helper()
	state := NewState(Options{
		Personality:  ForType(dpuType),
		SerialNumber: testSerial,
		PSID:         testPSID,
		PF0MAC:       config.PF0MAC(testSerial),
		Firmware:     firmware,
	})
	sup := newRecordingSupervisor()
	sup.state = state
	srv, err := NewServer(state, sup, opts...)
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
	_, root, err := raw.GetRootService(context.Background())
	if err != nil || root.IsBF4() {
		t.Fatalf("root service: %+v %v", root, err)
	}
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF3BMCUser, "whatever")
	if err != nil {
		t.Fatal(err)
	}
	if c.IsBF4 {
		t.Fatal("BF3 personality detected as BF4")
	}
	resp, chassis, err := c.GetChassis(context.Background())
	mustOK(t, "GetChassis", resp.StatusCode(), err)
	if chassis.SerialNumber != testSerial || chassis.GetBlueFieldVersion() != provisioningv1.DPUTypeBlueField3 {
		t.Fatalf("unexpected chassis %+v", chassis)
	}
	psid, err := c.GetPSID(context.Background())
	if err != nil || psid != testPSID {
		t.Fatalf("GetPSID: %q %v", psid, err)
	}
	resp, pf0, err := c.GetNetworkDeviceFunction(context.Background(), "eth0f0")
	mustOK(t, "GetNetworkDeviceFunction", resp.StatusCode(), err)
	if pf0.Ethernet.MACAddress != config.PF0MAC(testSerial) {
		t.Fatalf("unexpected PF0 %+v", pf0)
	}
	resp, desc, err := c.GetProductDescription(context.Background())
	mustOK(t, "GetProductDescription", resp.StatusCode(), err)
	if desc.Mode == nil || *desc.Mode != rfclient.DpuMode {
		t.Fatalf("expected DpuMode, got %+v", desc)
	}
	resp, mgr, err := c.GetBmcManager(context.Background())
	mustOK(t, "GetBmcManager", resp.StatusCode(), err)
	if mgr.FirmwareVersion != config.BMCMinSupportedVersion {
		t.Fatalf("unexpected manager %+v", mgr)
	}
	if _, err := time.Parse(time.RFC3339, mgr.DateTime); err != nil {
		t.Fatal(err)
	}
	// Any password is accepted: VerifyBMCCredential succeeds with the factory default.
	if _, user, err := rfclient.VerifyBMCCredential(context.Background(), url, rfclient.BMCDefaultPassword); err != nil || user != rfclient.BF3BMCUser {
		t.Fatalf("VerifyBMCCredential: %q %v", user, err)
	}
	resp, _, err = c.FactoryResetBMC(context.Background())
	mustOK(t, "FactoryResetBMC", resp.StatusCode(), err)
	resp, _, err = c.SetRedfishUserPassword(context.Background(), rfclient.BF3BMCUser, "new-password")
	mustOK(t, "SetRedfishUserPassword", resp.StatusCode(), err)
	resp, _, err = c.EnableMTLS(context.Background())
	mustOK(t, "EnableMTLS", resp.StatusCode(), err)
}

func TestBF3BMCFirmwareUpgrade(t *testing.T) {
	fw := defaultFirmware()
	fw.BMC = oldBMCVersion
	_, _, url := startServer(t, config.DPUTypeBF3, fw)
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	_, v, err := c.CheckBMCFirmware(context.Background())
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
	resp, task, err := c.UpdateBMCFirmware(context.Background(), fwFile)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("UpdateBMCFirmware: %v %v", resp.Status(), err)
	}
	resp, prog, err := c.CheckTaskProgress(context.Background(), task.ID)
	mustOK(t, "CheckTaskProgress", resp.StatusCode(), err)
	if prog.TaskState != TaskStateCompleted || prog.PercentComplete != 100 {
		t.Fatalf("unexpected task %+v", prog)
	}
	// The version only changes after Manager.Reset, like a real BMC.
	_, v, _ = c.CheckBMCFirmware(context.Background())
	if v.Version != oldBMCVersion {
		t.Fatalf("version changed before reset: %s", v.Version)
	}
	resp, _, err = c.ResetBMC(context.Background())
	mustOK(t, "ResetBMC", resp.StatusCode(), err)
	_, v, _ = c.CheckBMCFirmware(context.Background())
	if v.Version != config.BMCMinSupportedVersion {
		t.Fatalf("version after reset %s", v.Version)
	}
}

func TestBF3ConfigFWParametersAndInstall(t *testing.T) {
	state, sup, url := startServer(t, config.DPUTypeBF3, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	resp, _, err := c.DisableHostRshim(context.Background())
	mustOK(t, "DisableHostRshim", resp.StatusCode(), err)
	enabled, _, err := c.GetBMCRShimEnabled(context.Background())
	if err != nil || enabled {
		t.Fatalf("rshim before enable: %v %v", enabled, err)
	}
	resp, _, err = c.EnableBMCRShim(context.Background())
	mustOK(t, "EnableBMCRShim", resp.StatusCode(), err)
	enabled, _, err = c.GetBMCRShimEnabled(context.Background())
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

	resp, task, err := c.InstallBFB(context.Background(), imageURI)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("InstallBFB: %v %v", resp.Status(), err)
	}
	sup.wait(t)
	resp, prog, err := c.CheckTaskProgress(context.Background(), task.ID)
	mustOK(t, "CheckTaskProgress", resp.StatusCode(), err)
	if prog.TaskState != TaskStateCompleted || prog.PercentComplete != 100 {
		t.Fatalf("unexpected task %+v", prog)
	}
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if len(sup.boots) != 1 || !bytes.Equal(sup.boots[0], bfcfg) {
		t.Fatalf("supervisor boots %d", len(sup.boots))
	}
	_, uefi, _ := c.CheckDPUUEFI(context.Background())
	_, bsp, _ := c.CheckDPUBSP(context.Background())
	_, os, _ := c.CheckDPUOS(context.Background())
	if uefi.Version != "4.15.0-19-g37c6f5adb2" || bsp.Version != "4.15.0.13977" || os.Version != "3.4.0" {
		t.Fatalf("installed versions uefi=%s bsp=%s os=%s", uefi.Version, bsp.Version, os.Version)
	}
	if v, _ := state.FirmwareVersion("DPU_BOARD"); v != testPSID {
		t.Fatalf("DPU_BOARD %s", v)
	}

	// A broken download ends in an Exception task with a message.
	resp, task, err = c.InstallBFB(context.Background(), strings.TrimPrefix(registry.URL, "https://")+"/missing")
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("InstallBFB: %v %v", resp.Status(), err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, prog, err = c.CheckTaskProgress(context.Background(), task.ID)
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
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ForceRestartDPUArm(context.Background()); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	if _, err := c.ForceResetSOC(context.Background()); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	sup.mu.Lock()
	reboots, phases := sup.reboots, sup.rebootPhases
	sup.mu.Unlock()
	if reboots != 2 || phases[0] != provisioningv1.DPUPerformArmForceRestart || phases[1] != provisioningv1.DPURebooting {
		t.Fatalf("reboots %d phases %v", reboots, phases)
	}
	state.SetPowerOff()
	_, system, err := c.GetSystem(context.Background())
	if err != nil || system.PowerState != "Off" || system.Status.State != "StandbyOffline" {
		t.Fatalf("system after power off %+v %v", system, err)
	}
	state.SetPowerOn()
	_, system, _ = c.GetSystem(context.Background())
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
	if _, root, err := raw.GetRootService(context.Background()); err != nil || !root.IsBF4() {
		t.Fatalf("root service: %+v %v", root, err)
	}
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsBF4 {
		t.Fatal("BF4 personality not detected")
	}
	resp, chassis, err := c.GetChassis(context.Background())
	mustOK(t, "GetChassis", resp.StatusCode(), err)
	if chassis.GetBlueFieldVersion() != provisioningv1.DPUTypeBlueField4 {
		t.Fatalf("unexpected chassis %+v", chassis)
	}
	if psid, err := c.GetPSID(context.Background()); err != nil || psid != testPSID {
		t.Fatalf("GetPSID: %q %v", psid, err)
	}
	resp, pf0, err := c.GetNetworkDeviceFunction(context.Background(), "0")
	mustOK(t, "GetNetworkDeviceFunction", resp.StatusCode(), err)
	if pf0.Ethernet.PermanentMACAddress != config.PF0MAC(testSerial) {
		t.Fatalf("unexpected PF0 %+v", pf0)
	}
	resp, desc, err := c.GetProductDescription(context.Background())
	mustOK(t, "GetProductDescription", resp.StatusCode(), err)
	if desc.Mode != nil {
		t.Fatalf("BF4 must not expose Mode: %+v", desc)
	}
	resp, _, err = c.SetHostPrivilegeRestricted(context.Background())
	mustOK(t, "SetHostPrivilegeRestricted", resp.StatusCode(), err)
	if state.HostPrivilegeMode() != "Restricted" {
		t.Fatalf("host privilege %s", state.HostPrivilegeMode())
	}
}

func TestBF4FirmwareUpdate(t *testing.T) {
	state, sup, url := startServer(t, config.DPUTypeBF4, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	// Firmware update: versions differ from the bundle, upload the trimmed PLDM package.
	resp, erot, err := c.GetErotChassis(context.Background())
	mustOK(t, "GetErotChassis", resp.StatusCode(), err)
	if erot.Oem["Nvidia"].(map[string]interface{})["BackgroundCopyStatus"] != "Completed" {
		t.Fatalf("unexpected ERoT chassis %+v", erot)
	}
	pldm, err := os.Open("../../testdata/bf4-trimmed.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pldm.Close() }()
	resp, task, err := c.UpdateBluefieldFirmwareMultipart(context.Background(), pldm, true)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("UpdateBluefieldFirmwareMultipart: %v %v", resp.Status(), err)
	}
	_, prog, err := c.CheckTaskProgress(context.Background(), task.ID)
	if err != nil || prog.TaskState != TaskStateCompleted {
		t.Fatalf("task %+v %v", prog, err)
	}
	if _, err := c.ArmShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, system, _ := c.GetSystem(context.Background())
	if system.PowerState != "Paused" {
		t.Fatalf("expected Paused after ArmShutdown, got %s", system.PowerState)
	}
	sup.mu.Lock()
	powerOffs := sup.powerOffs
	sup.mu.Unlock()
	if powerOffs != 1 {
		t.Fatalf("ArmShutdown must go through the supervisor, PowerOff calls %d", powerOffs)
	}
	resp, err = c.ActivatePendingBundle(context.Background())
	mustOK(t, "ActivatePendingBundle", resp.StatusCode(), err)
	// Versions switch only when the host reboots.
	if _, info, err := c.CheckBMCFirmware(context.Background()); err != nil || info.Version != config.BMCMinSupportedVersion {
		t.Fatalf("BMC version changed before reboot: %+v %v", info, err)
	}
	if !state.ApplyActivatedBundle() {
		t.Fatal("no activated bundle")
	}
	_, bmc, _ := c.CheckBMCFirmware(context.Background())
	_, erotFW, _ := c.CheckBMCEROTFW(context.Background())
	_, sbios, _ := c.CheckDPUUEFI(context.Background())
	_, nic, _ := c.CheckDPUNIC(context.Background())
	if bmc.Version != "BF4-26.07-0005" || erotFW.Version != "02.00.0044.0000_n05" || sbios.Version != "26.08-0007" || nic.Version != "82.48.4004" {
		t.Fatalf("bundle versions bmc=%s erot=%s sbios=%s nic=%s", bmc.Version, erotFW.Version, sbios.Version, nic.Version)
	}
}

func TestBF4OSInstall(t *testing.T) {
	_, sup, url := startServer(t, config.DPUTypeBF4, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF4BMCUser, "x")
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
	if _, err := c.CheckOSImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckConfigImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		install func(string) (interface{}, *rfclient.TaskInfo, error)
		uri     string
	}{
		{func(u string) (interface{}, *rfclient.TaskInfo, error) {
			r, t, e := c.InstallBluefieldArmImage(context.Background(), u)
			return r, t, e
		}, host + "/bfb/bf4/os.iso"},
		{func(u string) (interface{}, *rfclient.TaskInfo, error) {
			r, t, e := c.InstallBluefieldArmConfig(context.Background(), u)
			return r, t, e
		}, host + "/bfb/user-data/ns_dpu_uid/seed.iso"},
	} {
		_, task, err := step.install(step.uri)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			_, prog, err := c.CheckTaskProgress(context.Background(), task.ID)
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
	if _, err := c.InsertVirtualMediaImage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InsertVirtualMediaConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetBootTarget(context.Background(), "None", false); err != nil {
		t.Fatal(err)
	}
	if _, settings, err := c.GetSettings(context.Background()); err != nil || settings.Boot.BootSourceOverrideTarget != "None" {
		t.Fatalf("settings %+v %v", settings, err)
	}
	if _, err := c.SetBootTarget(context.Background(), "Usb", true); err != nil {
		t.Fatal(err)
	}
	if _, settings, err := c.GetSettings(context.Background()); err != nil || settings.Boot.BootSourceOverrideTarget != "Usb" || settings.Boot.BootSourceOverrideEnabled != "Once" {
		t.Fatalf("settings %+v %v", settings, err)
	}
	if _, err := c.ChassisReset(context.Background()); err != nil {
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
	if _, err := c.ChassisReset(context.Background()); err != nil {
		t.Fatal(err)
	}
	sup.wait(t)
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if sup.reboots != 1 || sup.rebootPhases[0] != provisioningv1.DPUPerformArmForceRestart {
		t.Fatalf("reboots %d phases %v", sup.reboots, sup.rebootPhases)
	}
}

// TestCertificateLifecycle walks the controller's mTLS bootstrap: truststore reconcile,
// GenerateCSR, install of the CA-signed certificate, and then a verified connection that pins the
// CA and the BMC IP like the provisioning controller's verified client does.
func TestCertificateLifecycle(t *testing.T) {
	_, _, url := startServer(t, config.DPUTypeBF3, defaultFirmware())
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	caKey, caCert, caPEM := newTestCA(t)

	// Truststore reconcile by fingerprint.
	certs, err := c.ListTruststoreCerts(context.Background())
	if err != nil || len(certs) != 0 {
		t.Fatalf("initial truststore %+v %v", certs, err)
	}
	resp, _, err := c.InstallCert(context.Background(), string(caPEM))
	mustOK(t, "InstallCert", resp.StatusCode(), err)
	certs, err = c.ListTruststoreCerts(context.Background())
	if err != nil || len(certs) != 1 {
		t.Fatalf("truststore after install %+v %v", certs, err)
	}
	resp, _, err = c.DeleteTruststoreCert(context.Background(), certs[0].URI)
	mustOK(t, "DeleteTruststoreCert", resp.StatusCode(), err)
	if certs, _ = c.ListTruststoreCerts(context.Background()); len(certs) != 0 {
		t.Fatalf("truststore after delete %+v", certs)
	}

	// GenerateCSR must be called on the server directly: the client hard-codes port 443.
	bmcIP := "127.0.0.1"
	csrResp, err := c.R().SetHeader("Content-Type", "application/json").SetBody(map[string]interface{}{
		"CommonName":       bmcIP,
		"AlternativeNames": []string{"IP: " + bmcIP, "DNS: localhost", "IP: 127.0.0.1"},
	}).Post(rfclient.RedfishAPIs.Path("APIGenerateCSR"))
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
	resp, _, err = c.ReplaceServerCert(context.Background(), string(signCSR(t, caKey, caCert, otherCSR)))
	if err != nil || resp.StatusCode() != http.StatusInternalServerError {
		t.Fatalf("ReplaceServerCert with foreign key: %v %v", resp.Status(), err)
	}
	resp, _, err = c.ReplaceServerCert(context.Background(), string(leafPEM))
	mustOK(t, "ReplaceServerCert", resp.StatusCode(), err)
	resp, served, err := c.GetServerCert(context.Background())
	mustOK(t, "GetServerCert", resp.StatusCode(), err)
	if strings.TrimSpace(served.CertificateString) != strings.TrimSpace(string(leafPEM)) {
		t.Fatal("served certificate is not the installed one")
	}

	// Verified connection: chain to the CA and pin the BMC IP, as the provisioning controller's verified client does.
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

func TestTaskPacePercent(t *testing.T) {
	p := taskPace{start: time.Now().Add(-time.Second), hold: 4 * time.Second}
	if got := p.percent(100); got < 24 || got > 26 {
		t.Fatalf("a quarter of the hold reported %d%%", got)
	}
	if got := p.percent(10); got != 10 {
		t.Fatalf("a slower download must win, got %d%%", got)
	}
	if got := (taskPace{}).percent(100); got != 99 {
		t.Fatalf("no hold: got %d%%, want 99 until completion", got)
	}
}

func TestTransferPhaseDelay(t *testing.T) {
	const hold = 800 * time.Millisecond // 0.40 of OS Installing for the BF4 ISO
	delays := phasedelay.New(map[provisioningv1.DPUPhase]phasedelay.Range{
		provisioningv1.DPUOSInstalling: {Min: 2 * time.Second, Max: 2 * time.Second},
	})
	_, _, url := startServer(t, config.DPUTypeBF4, defaultFirmware(), WithPhaseDelays(delays))
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("iso"), 1000))
	}))
	t.Cleanup(registry.Close)
	start := time.Now()
	_, task, err := c.InstallBluefieldArmImage(context.Background(), strings.TrimPrefix(registry.URL, "https://")+"/bfb/bf4/os.iso")
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, prog, err := c.CheckTaskProgress(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		if prog.TaskState == TaskStateCompleted {
			if elapsed < hold {
				t.Fatalf("task completed after %v, want at least %v", elapsed, hold)
			}
			return
		}
		if prog.TaskState != TaskStateRunning {
			t.Fatalf("task %+v", prog)
		}
		if limit := int(elapsed*100/hold) + 1; prog.PercentComplete > limit {
			t.Fatalf("%d%% complete after %v, more than the elapsed share", prog.PercentComplete, elapsed)
		}
		if elapsed > 10*time.Second {
			t.Fatalf("task not completed: %+v", prog)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFirmwareTaskPhaseDelay(t *testing.T) {
	const hold = 500 * time.Millisecond
	delays := phasedelay.New(map[provisioningv1.DPUPhase]phasedelay.Range{
		provisioningv1.DPUUpdateFirmware: {Min: hold, Max: hold},
	})
	_, _, url := startServer(t, config.DPUTypeBF4, defaultFirmware(), WithPhaseDelays(delays))
	c, err := rfclient.NewBasicAuthClient(context.Background(), url, rfclient.BF4BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	pldm, err := os.Open("../../testdata/bf4-trimmed.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pldm.Close() })
	start := time.Now()
	resp, task, err := c.UpdateBluefieldFirmwareMultipart(context.Background(), pldm, true)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("UpdateBluefieldFirmwareMultipart: %v %v", resp.Status(), err)
	}
	if _, prog, err := c.CheckTaskProgress(context.Background(), task.ID); err != nil || prog.TaskState != TaskStateRunning {
		t.Fatalf("task right after upload %+v %v", prog, err)
	}
	for {
		_, prog, err := c.CheckTaskProgress(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if prog.TaskState == TaskStateCompleted {
			if elapsed := time.Since(start); elapsed < hold {
				t.Fatalf("task completed after %v, want at least %v", elapsed, hold)
			}
			return
		}
		if time.Since(start) > 10*time.Second {
			t.Fatalf("task not completed: %+v", prog)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// redfishHTTP is an HTTP client that trusts the mock BMC's certificate.
func redfishHTTP() *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
}

// getRedfish fetches a Redfish resource and decodes the JSON object.
func getRedfish(t *testing.T, client *http.Client, url string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("GET %s: %v body %s", url, err, body)
	}
	return resp.StatusCode, decoded
}

// postRedfish posts a JSON body and returns the status code.
func postRedfish(t *testing.T, client *http.Client, url, body string) int {
	t.Helper()
	resp, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// odataMemberIDs returns the last path segment of each Members[].@odata.id.
func odataMemberIDs(t *testing.T, body map[string]interface{}) []string {
	t.Helper()
	raw, ok := body["Members"].([]interface{})
	if !ok {
		t.Fatalf("Members: %#v", body["Members"])
	}
	ids := make([]string, 0, len(raw))
	for _, member := range raw {
		odataID, _ := member.(map[string]interface{})["@odata.id"].(string)
		ids = append(ids, odataID[strings.LastIndex(odataID, "/")+1:])
	}
	return ids
}

// inactiveSlotVersion returns Oem.Nvidia.InactiveFirmwareSlot.Version.
func inactiveSlotVersion(t *testing.T, fw map[string]interface{}) (string, string) {
	t.Helper()
	nvidia, ok := fw["Oem"].(map[string]interface{})["Nvidia"].(map[string]interface{})
	if !ok {
		t.Fatalf("firmware has no Oem.Nvidia: %#v", fw)
	}
	inactive, ok := nvidia["InactiveFirmwareSlot"].(map[string]interface{})
	if !ok {
		t.Fatalf("firmware has no InactiveFirmwareSlot: %#v", fw)
	}
	version, _ := inactive["Version"].(string)
	state, _ := inactive["FirmwareState"].(string)
	return version, state
}

// TestBF4CMXPersonality checks the discovery tree of the Vera BMC from the v0.2 CMX mockup: the
// product string, the system, manager and chassis collections, and the two CX9s with their adapters
// and ports, none of which has a MAC address.
func TestBF4CMXPersonality(t *testing.T) {
	_, _, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	base := rawURL + "/redfish/v1"

	status, root := getRedfish(t, httpClient, base)
	if status != http.StatusOK || root["Product"] != "CMX" {
		t.Fatalf("root status %d product %#v", status, root["Product"])
	}
	c, err := rfclient.NewBasicAuthClient(context.Background(), rawURL, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}
	if c.IsBF4 {
		t.Fatal("CMX product string must not be detected as BlueField-4")
	}

	_, systems := getRedfish(t, httpClient, base+"/Systems")
	if ids := odataMemberIDs(t, systems); len(ids) != 1 || ids[0] != "System_0" {
		t.Fatalf("systems %v", ids)
	}
	_, system := getRedfish(t, httpClient, base+"/Systems/System_0")
	links, _ := system["Links"].(map[string]interface{})
	chassisLinks, _ := links["Chassis"].([]interface{})
	var link map[string]interface{}
	if len(chassisLinks) == 1 {
		link, _ = chassisLinks[0].(map[string]interface{})
	}
	if link["@odata.id"] != "/redfish/v1/Chassis/BMC_0" {
		t.Fatalf("system chassis link %#v, want BMC_0", links["Chassis"])
	}
	_, managers := getRedfish(t, httpClient, base+"/Managers")
	managerIDs := strings.Join(odataMemberIDs(t, managers), " ")
	for _, id := range []string{"BMC_0", "SMM_0", "StorageBackplane_0"} {
		if !strings.Contains(managerIDs, id) {
			t.Fatalf("managers %s missing %s", managerIDs, id)
		}
	}
	_, chassis := getRedfish(t, httpClient, base+"/Chassis")
	for _, id := range []string{"Chassis_0", "BMC_0", "ERoT_BMC_0", "CX_0", "CX_1", "OSFP_Board_0", "CPLD_0"} {
		if !strings.Contains(strings.Join(odataMemberIDs(t, chassis), " "), id) {
			t.Fatalf("chassis collection %v missing %s", odataMemberIDs(t, chassis), id)
		}
	}
	_, node := getRedfish(t, httpClient, base+"/Chassis/Chassis_0")
	if node["Model"] != "CMX" || node["ChassisType"] != "Zone" || node["SerialNumber"] != testSerial || node["AssetTag"] != testPSID {
		t.Fatalf("identity chassis %#v", node)
	}
	for _, id := range []string{"CX_0", "CX_1"} {
		_, nic := getRedfish(t, httpClient, base+"/Chassis/"+id)
		if nic["Model"] != "ConnectX-9 800GE 2P NIC" {
			t.Fatalf("%s %#v", id, nic)
		}
	}
	_, adapter := getRedfish(t, httpClient, base+"/Chassis/CX_0/NetworkAdapters/CX_NIC_0")
	if adapter["Model"] != "ConnectX-9 800GE 2P NIC" || adapter["NetworkDeviceFunctions"] != nil || adapter["Controllers"] == nil {
		t.Fatalf("CX_NIC_0 %#v", adapter)
	}
	if status, _ := getRedfish(t, httpClient, base+"/Chassis/CX_0/NetworkAdapters/CX_NIC_0/NetworkDeviceFunctions/0"); status != http.StatusNotFound {
		t.Fatalf("NetworkDeviceFunctions status %d, want 404", status)
	}
	_, ports := getRedfish(t, httpClient, base+"/Chassis/CX_0/NetworkAdapters/CX_NIC_0/Ports")
	if ids := odataMemberIDs(t, ports); len(ids) != 2 || ids[0] != "Port_0" || ids[1] != "Port_1" {
		t.Fatalf("CX_NIC_0 ports %v", ids)
	}
	_, port0 := getRedfish(t, httpClient, base+"/Chassis/CX_0/NetworkAdapters/CX_NIC_0/Ports/Port_0")
	if _, ok := port0["MACAddress"]; ok {
		t.Fatalf("Port_0 has a MAC: %#v", port0)
	}
}

// TestBF4CMXInventoryAndMedia checks the CMX firmware inventory before an update, the virtual media
// slots and the BlueField-only resources that must be absent.
func TestBF4CMXInventoryAndMedia(t *testing.T) {
	_, _, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	base := rawURL + "/redfish/v1"

	_, inventory := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory")
	members := strings.Join(odataMemberIDs(t, inventory), " ")
	for _, id := range []string{"FW_BMC_0", "FW_ERoT_BMC_0", "SBIOS_FW_0", "SBIOS_FMC_0", "FW_CX_0", "FW_CX_1", "FW_CPLD_0", "FW_MCU_OSFP_0", "FW_NVMe_M2_0", "FW_NVMe_E3S_SSD0", "FW_NVMe_E3S_SSD11"} {
		if !strings.Contains(" "+members+" ", " "+id+" ") {
			t.Fatalf("firmware inventory %s missing %s", members, id)
		}
	}
	_, cx0 := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_CX_0")
	related, _ := cx0["RelatedItem"].([]interface{})
	if cx0["Version"] != config.PlaceholderVersion || cx0["Oem"] != nil || len(related) != 1 || related[0].(map[string]interface{})["@odata.id"] != "/redfish/v1/Chassis/CX_0" {
		t.Fatalf("FW_CX_0 %#v", cx0)
	}
	_, bmcFW := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	if version, state := inactiveSlotVersion(t, bmcFW); version != "" || state != "Unknown" {
		t.Fatalf("FW_BMC_0 inactive before an update: version %q state %q", version, state)
	}
	if status, media := getRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia"); status != http.StatusOK {
		t.Fatalf("VirtualMedia collection status %d", status)
	} else if ids := strings.Join(odataMemberIDs(t, media), " "); !strings.Contains(ids, "USB1") || !strings.Contains(ids, "USB2") || !strings.Contains(ids, "Slot_0") {
		t.Fatalf("VirtualMedia members %s", ids)
	}
	if status, usb1 := getRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia/USB1"); status != http.StatusOK {
		t.Fatalf("USB1 status %d", status)
	} else if actions, _ := usb1["Actions"].(map[string]interface{}); actions["#VirtualMedia.InsertMedia"] == nil {
		t.Fatalf("USB1 actions %#v", usb1["Actions"])
	}
	if status, slot0 := getRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia/Slot_0"); status != http.StatusOK {
		t.Fatalf("Slot_0 status %d", status)
	} else if actions, _ := slot0["Actions"].(map[string]interface{}); actions["#VirtualMedia.InsertMedia"] != nil {
		t.Fatalf("Slot_0 accepts InsertMedia: %#v", actions)
	}
	if status := postRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia/Slot_0/Actions/VirtualMedia.InsertMedia", "{}"); status != http.StatusNotFound {
		t.Fatalf("Slot_0 InsertMedia status %d, want 404", status)
	}
	if status, _ := getRedfish(t, httpClient, base+"/Systems/System_0/Oem/Nvidia"); status != http.StatusNotFound {
		t.Fatalf("Oem/Nvidia status %d, want 404", status)
	}
	if status := postRedfish(t, httpClient, base+"/Systems/System_0/Oem/Nvidia/SOC.ForceReset", "{}"); status != http.StatusNotFound {
		t.Fatalf("SOC.ForceReset status %d, want 404", status)
	}
	if status := postRedfish(t, httpClient, base+"/UpdateService/Actions/UpdateService.Activate", "{}"); status != http.StatusNotFound {
		t.Fatalf("Activate status %d, want 404", status)
	}

}

// requireCX9Firmware fails the test unless both CX9 inventory members report version and have no
// inactive firmware slot. base is the Redfish root URL, when says which step of the test checks.
func requireCX9Firmware(t *testing.T, httpClient *http.Client, base, version, when string) {
	t.Helper()
	for _, id := range []string{"FW_CX_0", "FW_CX_1"} {
		_, fw := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/"+id)
		if fw["Version"] != version || fw["Oem"] != nil {
			t.Fatalf("%s %s %#v", id, when, fw)
		}
	}
}

// TestBF4CMXFirmwareUpdate stages a PLDM bundle through the multipart update, commits it and walks
// it through a host reset and a Node AC cycle.
func TestBF4CMXFirmwareUpdate(t *testing.T) {
	_, sup, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	base := rawURL + "/redfish/v1"
	c, err := rfclient.NewBasicAuthClient(context.Background(), rawURL, rfclient.BF3BMCUser, "x")
	if err != nil {
		t.Fatal(err)
	}

	pldm, err := os.Open("../../testdata/bf4-trimmed.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pldm.Close() }()
	resp, task, err := c.UpdateBluefieldFirmwareMultipart(context.Background(), pldm, true)
	if err != nil || resp.StatusCode() != http.StatusAccepted {
		t.Fatalf("multipart update: %v %v", resp.Status(), err)
	}
	if _, prog, err := c.CheckTaskProgress(context.Background(), task.ID); err != nil || prog.TaskState != TaskStateCompleted {
		t.Fatalf("task %+v %v", prog, err)
	}
	requireCX9Firmware(t, httpClient, base, config.PlaceholderVersion, "after stage")
	_, bmc := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	if bmc["Version"] != config.BMCMinSupportedVersion {
		t.Fatalf("BMC changed before a reset: %#v", bmc)
	}
	if version, state := inactiveSlotVersion(t, bmc); version != "BF4-26.07-0005" || state != "Inactive" {
		t.Fatalf("FW_BMC_0 inactive after stage: version %q state %q", version, state)
	}
	if status := postRedfish(t, httpClient, base+"/UpdateService/Actions/Oem/NvidiaUpdateService.CommitImage", "{}"); status != http.StatusOK {
		t.Fatalf("CommitImage status %d", status)
	}
	_, bmc = getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	if version, state := inactiveSlotVersion(t, bmc); version != "BF4-26.07-0005" || state != "PendingActivation" || bmc["Version"] != config.BMCMinSupportedVersion {
		t.Fatalf("FW_BMC_0 after CommitImage version %#v inactive %q %q", bmc["Version"], version, state)
	}

	if status := postRedfish(t, httpClient, base+"/Systems/System_0/Actions/ComputerSystem.Reset", `{"ResetType":"PowerCycle"}`); status != http.StatusOK {
		t.Fatalf("PowerCycle status %d", status)
	}
	requireCX9Firmware(t, httpClient, base, "82.48.4004", "after host reset")
	_, sbios := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/SBIOS_FW_0")
	if sbios["Version"] != "26.08-0007" {
		t.Fatalf("SBIOS after host reset %#v", sbios)
	}
	if version, _ := inactiveSlotVersion(t, sbios); version != "" {
		t.Fatalf("SBIOS inactive slot still staged after host reset: %q", version)
	}
	_, bmc = getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	if bmc["Version"] != config.BMCMinSupportedVersion {
		t.Fatalf("host reset activated BMC firmware: %#v", bmc)
	}
	if version, state := inactiveSlotVersion(t, bmc); version != "BF4-26.07-0005" || state != "PendingActivation" {
		t.Fatalf("host reset cleared BMC inactive slot: version %q state %q", version, state)
	}

	if status := postRedfish(t, httpClient, base+"/Chassis/BMC_0/Actions/Oem/NvidiaChassis.AuxPowerReset", `{"ResetType":"AuxPowerCycle"}`); status != http.StatusOK {
		t.Fatalf("AuxPowerCycle status %d", status)
	}
	_, bmc = getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	_, erot := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_ERoT_BMC_0")
	bmcInactive, _ := inactiveSlotVersion(t, bmc)
	if bmc["Version"] != "BF4-26.07-0005" || erot["Version"] != "02.00.0044.0000_n05" || bmcInactive != "" {
		t.Fatalf("after Node AC cycle bmc %#v erot %#v inactive %q", bmc["Version"], erot["Version"], bmcInactive)
	}
	sup.mu.Lock()
	reboots := sup.reboots
	sup.mu.Unlock()
	if reboots != 2 {
		t.Fatalf("host reset and Node AC cycle must both reboot, got %d", reboots)
	}
}

// TestBF4CMXBootProgress checks that the CMX System reports BootProgress through the standard
// LastState, as the v0.2 mockup does, while BlueField keeps its OEM state.
func TestBF4CMXBootProgress(t *testing.T) {
	state, _, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	bootProgress := func(base string) map[string]interface{} {
		t.Helper()
		_, system := getRedfish(t, httpClient, base)
		progress, _ := system["BootProgress"].(map[string]interface{})
		if progress == nil {
			t.Fatalf("%s has no BootProgress: %#v", base, system)
		}
		return progress
	}
	system := rawURL + "/redfish/v1/Systems/System_0"
	if progress := bootProgress(system); progress["LastState"] != "OSRunning" || progress["OemLastState"] != nil {
		t.Fatalf("running CMX BootProgress %#v", progress)
	}
	state.SetOSStarting()
	if progress := bootProgress(system); progress["LastState"] != "OSBootStarted" || progress["OemLastState"] != nil {
		t.Fatalf("booting CMX BootProgress %#v", progress)
	}
	state.SetPowerOff()
	if progress := bootProgress(system); progress["LastState"] != "None" {
		t.Fatalf("powered off CMX BootProgress %#v", progress)
	}

	_, _, bf4URL := startServer(t, config.DPUTypeBF4, defaultFirmware())
	if progress := bootProgress(bf4URL + "/redfish/v1/Systems/BlueField_0"); progress["LastState"] != "OEM" || progress["OemLastState"] != OemLastStateOSUp {
		t.Fatalf("BF4 BootProgress changed: %#v", progress)
	}
}

// TestBF4CMXSystemReset checks every ResetType in the Vera ResetActionInfo: the shutdown types
// power the host off, On and ForceOn boot it only when it is off, the restart types boot it, and
// a BMC-unknown type is rejected.
func TestBF4CMXSystemReset(t *testing.T) {
	state, sup, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	reset := rawURL + "/redfish/v1/Systems/System_0/Actions/ComputerSystem.Reset"
	counts := func() (reboots, powerOffs int) {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		return sup.reboots, sup.powerOffs
	}
	post := func(resetType string, want int) {
		t.Helper()
		if status := postRedfish(t, httpClient, reset, fmt.Sprintf(`{"ResetType":%q}`, resetType)); status != want {
			t.Fatalf("ResetType %s status %d, want %d", resetType, status, want)
		}
	}

	post("Nmi", http.StatusBadRequest)
	if reboots, powerOffs := counts(); reboots != 0 || powerOffs != 0 {
		t.Fatalf("an unsupported ResetType acted: reboots %d powerOffs %d", reboots, powerOffs)
	}
	post("On", http.StatusOK)
	post("ForceOn", http.StatusOK)
	if reboots, _ := counts(); reboots != 0 {
		t.Fatalf("On of a running host rebooted it %d times", reboots)
	}

	for _, resetType := range []string{"GracefulShutdown", "ForceOff"} {
		state.SetPowerOn()
		post(resetType, http.StatusOK)
		if power, _, _ := state.Power(); power != "Off" {
			t.Fatalf("PowerState after %s is %s, want Off", resetType, power)
		}
	}
	if _, powerOffs := counts(); powerOffs != 2 {
		t.Fatalf("shutdown types powered off %d times, want 2", powerOffs)
	}

	for _, resetType := range []string{"On", "ForceOn"} {
		state.SetPowerOff()
		post(resetType, http.StatusOK)
		sup.wait(t)
	}
	for _, resetType := range []string{"ForceRestart", "GracefulRestart", "PowerCycle"} {
		post(resetType, http.StatusOK)
		sup.wait(t)
	}
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if sup.reboots != 5 {
		t.Fatalf("boots %d, want 5", sup.reboots)
	}
	for _, phase := range sup.rebootPhases {
		if phase != provisioningv1.DPURebooting {
			t.Fatalf("CMX reset is attributed to %s, want %s", phase, provisioningv1.DPURebooting)
		}
	}
}

// TestBF4CMXOSInstall walks the CMX OS install: InsertMedia on USB1 (OS ISO) and USB2 (config ISO),
// a one-time Usb boot override and ComputerSystem.Reset. The config ISO is downloaded at insert
// and the reset boots it, and a second reset only restarts the OS.
func TestBF4CMXOSInstall(t *testing.T) {
	_, sup, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	base := rawURL + "/redfish/v1"
	seedISO := []byte("cidata-seed-iso-content")
	var osISOReads int
	var mu sync.Mutex
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/seed.iso"):
			_, _ = w.Write(seedISO)
		case strings.HasSuffix(r.URL.Path, "/os.iso"):
			mu.Lock()
			osISOReads++
			mu.Unlock()
			_, _ = w.Write([]byte("os"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer registry.Close()
	insert := func(slot, body string) int {
		t.Helper()
		return postRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia/"+slot+"/Actions/VirtualMedia.InsertMedia", body)
	}
	media := func(slot string) map[string]interface{} {
		t.Helper()
		_, body := getRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia/"+slot)
		return body
	}

	if status := insert("USB2", "{}"); status != http.StatusBadRequest {
		t.Fatalf("InsertMedia without Image status %d, want 400", status)
	}
	if status := insert("USB2", fmt.Sprintf(`{"Image":"%s/missing.iso","TransferMethod":"Stream"}`, registry.URL)); status != http.StatusBadRequest {
		t.Fatalf("InsertMedia of an unreadable image status %d, want 400", status)
	}
	if usb2 := media("USB2"); usb2["Inserted"] != false || usb2["Image"] != nil {
		t.Fatalf("USB2 after failed inserts %#v", usb2)
	}
	osImage := registry.URL + "/bfb/os.iso"
	seedImage := registry.URL + "/bfb/user-data/ns_dpu_uid/seed.iso"
	if status := insert("USB1", fmt.Sprintf(`{"Image":%q,"TransferMethod":"Stream"}`, osImage)); status != http.StatusOK {
		t.Fatalf("InsertMedia USB1 status %d", status)
	}
	if status := insert("USB2", fmt.Sprintf(`{"Image":%q,"TransferMethod":"Stream"}`, seedImage)); status != http.StatusOK {
		t.Fatalf("InsertMedia USB2 status %d", status)
	}
	if usb1 := media("USB1"); usb1["Inserted"] != true || usb1["Image"] != osImage || usb1["ConnectedVia"] != "URI" {
		t.Fatalf("USB1 after insert %#v", usb1)
	}
	if usb2 := media("USB2"); usb2["Inserted"] != true || usb2["Image"] != seedImage {
		t.Fatalf("USB2 after insert %#v", usb2)
	}
	mu.Lock()
	reads := osISOReads
	mu.Unlock()
	if reads != 0 {
		t.Fatalf("the OS ISO was read %d times, a BMC only streams it to the host", reads)
	}

	patch, err := http.NewRequest(http.MethodPatch, base+"/Systems/System_0/Settings", strings.NewReader(`{"Boot":{"BootSourceOverrideTarget":"Usb","BootSourceOverrideEnabled":"Once"}}`))
	if err != nil {
		t.Fatal(err)
	}
	patch.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(patch)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("boot override status %d", resp.StatusCode)
	}

	if status := postRedfish(t, httpClient, base+"/Systems/System_0/Actions/ComputerSystem.Reset", `{"ResetType":"ForceRestart"}`); status != http.StatusOK {
		t.Fatalf("Reset status %d", status)
	}
	sup.wait(t)
	sup.mu.Lock()
	boots, reboots := sup.boots, sup.reboots
	sup.mu.Unlock()
	if len(boots) != 1 || !bytes.Equal(boots[0], seedISO) || reboots != 0 {
		t.Fatalf("boots=%d reboots=%d", len(boots), reboots)
	}
	if status := postRedfish(t, httpClient, base+"/Systems/System_0/Actions/ComputerSystem.Reset", `{"ResetType":"PowerCycle"}`); status != http.StatusOK {
		t.Fatalf("second Reset status %d", status)
	}
	sup.wait(t)
	sup.mu.Lock()
	boots, reboots = sup.boots, sup.reboots
	sup.mu.Unlock()
	if len(boots) != 1 || reboots != 1 {
		t.Fatalf("a reset without a pending seed must only restart: boots=%d reboots=%d", len(boots), reboots)
	}

	if status := postRedfish(t, httpClient, base+"/Managers/BMC_0/VirtualMedia/USB2/Actions/VirtualMedia.EjectMedia", "{}"); status != http.StatusOK {
		t.Fatalf("EjectMedia status %d", status)
	}
	if usb2 := media("USB2"); usb2["Inserted"] != false || usb2["Image"] != nil || usb2["ConnectedVia"] != "NotConnected" {
		t.Fatalf("USB2 after eject %#v", usb2)
	}
}

// TestBF4CMXEjectSeed checks that ejecting USB2 drops the downloaded config ISO, so the next
// ComputerSystem.Reset restarts the host instead of booting that seed.
func TestBF4CMXEjectSeed(t *testing.T) {
	_, sup, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	base := rawURL + "/redfish/v1"
	seedISO := []byte("cidata-seed-iso-content")
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/seed.iso") {
			_, _ = w.Write(seedISO)
			return
		}
		http.NotFound(w, r)
	}))
	defer registry.Close()

	seedImage := registry.URL + "/bfb/user-data/ns_dpu_uid/seed.iso"
	insert := base + "/Managers/BMC_0/VirtualMedia/USB2/Actions/VirtualMedia.InsertMedia"
	if status := postRedfish(t, httpClient, insert, fmt.Sprintf(`{"Image":%q,"TransferMethod":"Stream"}`, seedImage)); status != http.StatusOK {
		t.Fatalf("InsertMedia USB2 status %d", status)
	}
	eject := base + "/Managers/BMC_0/VirtualMedia/USB2/Actions/VirtualMedia.EjectMedia"
	if status := postRedfish(t, httpClient, eject, "{}"); status != http.StatusOK {
		t.Fatalf("EjectMedia status %d", status)
	}
	if status := postRedfish(t, httpClient, base+"/Systems/System_0/Actions/ComputerSystem.Reset", `{"ResetType":"ForceRestart"}`); status != http.StatusOK {
		t.Fatalf("Reset after eject status %d", status)
	}
	sup.wait(t)
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if len(sup.boots) != 0 || sup.reboots != 1 {
		t.Fatalf("a reset after ejecting the seed must not boot it: boots=%d reboots=%d", len(sup.boots), sup.reboots)
	}
}

// postMultipartUpdate posts the trimmed PLDM bundle to the multipart update URI with the given
// UpdateParameters JSON and returns the status code.
func postMultipartUpdate(t *testing.T, httpClient *http.Client, url, params string) int {
	t.Helper()
	pldm, err := os.ReadFile("../../testdata/bf4-trimmed.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("UpdateFile", "bundle.fwpkg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(pldm)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="UpdateParameters"`)
	header.Set("Content-Type", "application/json")
	field, err := form.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = field.Write([]byte(params))
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err := httpClient.Post(url, form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestBF4CMXUpdateTargets checks that a multipart update with Targets stages only the named
// inventory members: a bundle for FW_CX_1 leaves FW_CX_0 and the dual-slot firmware alone. A target
// that is not a full FirmwareInventory URI, a member the bundle has no version for, and Targets that
// is not a list are rejected and stage nothing. A replacement image staged after CommitImage is
// Inactive until the next commit.
func TestBF4CMXUpdateTargets(t *testing.T) {
	_, _, rawURL := startServer(t, config.DPUTypeBF4CMX, defaultFirmware())
	httpClient := redfishHTTP()
	base := rawURL + "/redfish/v1"
	update := base + "/UpdateService/update-multipart"
	fwURI := func(id string) string { return `"/redfish/v1/UpdateService/FirmwareInventory/` + id + `"` }
	inactive := func(id string) string {
		t.Helper()
		_, fw := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/"+id)
		version, _ := inactiveSlotVersion(t, fw)
		return version
	}

	if status := postMultipartUpdate(t, httpClient, update, `{"Targets":[`+fwURI("FW_NOPE")+`],"ForceUpdate":true}`); status != http.StatusBadRequest {
		t.Fatalf("unknown target status %d, want 400", status)
	}
	if status := postMultipartUpdate(t, httpClient, update, `{"Targets":"FW_CX_1"}`); status != http.StatusBadRequest {
		t.Fatalf("Targets that is not a list status %d, want 400", status)
	}
	if status := postMultipartUpdate(t, httpClient, update, `{"Targets":["/arbitrary/FW_BMC_0"]}`); status != http.StatusBadRequest {
		t.Fatalf("target that is not a FirmwareInventory URI status %d, want 400", status)
	}
	for _, id := range []string{"SBIOS_FMC_0", "FW_CPLD_0"} {
		if status := postMultipartUpdate(t, httpClient, update, `{"Targets":[`+fwURI(id)+`]}`); status != http.StatusBadRequest {
			t.Fatalf("%s has no version in the bundle, status %d, want 400", id, status)
		}
	}
	if version := inactive("FW_BMC_0"); version != "" {
		t.Fatalf("a rejected update staged FW_BMC_0 %q", version)
	}

	if status := postMultipartUpdate(t, httpClient, update, `{"Targets":[`+fwURI("FW_CX_1")+`]}`); status != http.StatusAccepted {
		t.Fatalf("FW_CX_1 update status %d", status)
	}
	for _, id := range []string{"FW_BMC_0", "FW_ERoT_BMC_0", "SBIOS_FW_0"} {
		if version := inactive(id); version != "" {
			t.Fatalf("an update targeting FW_CX_1 staged %s %q", id, version)
		}
	}
	if status := postRedfish(t, httpClient, base+"/Systems/System_0/Actions/ComputerSystem.Reset", `{"ResetType":"ForceRestart"}`); status != http.StatusOK {
		t.Fatalf("Reset status %d", status)
	}
	_, cx0 := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_CX_0")
	_, cx1 := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_CX_1")
	if cx0["Version"] != config.PlaceholderVersion || cx1["Version"] != "82.48.4004" {
		t.Fatalf("after the FW_CX_1 update FW_CX_0 %v FW_CX_1 %v", cx0["Version"], cx1["Version"])
	}

	if status := postMultipartUpdate(t, httpClient, update, `{"Targets":[`+fwURI("FW_BMC_0")+`,`+fwURI("SBIOS_FW_0")+`]}`); status != http.StatusAccepted {
		t.Fatalf("FW_BMC_0 and SBIOS_FW_0 update status %d", status)
	}
	if bmc, sbios, erot := inactive("FW_BMC_0"), inactive("SBIOS_FW_0"), inactive("FW_ERoT_BMC_0"); bmc != "BF4-26.07-0005" || sbios != "26.08-0007" || erot != "" {
		t.Fatalf("staged bmc %q sbios %q erot %q", bmc, sbios, erot)
	}
	if status := postRedfish(t, httpClient, base+"/UpdateService/Actions/Oem/NvidiaUpdateService.CommitImage", "{}"); status != http.StatusOK {
		t.Fatalf("CommitImage status %d", status)
	}
	_, bmcFW := getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	if _, state := inactiveSlotVersion(t, bmcFW); state != "PendingActivation" {
		t.Fatalf("FW_BMC_0 after CommitImage state %q", state)
	}
	if status := postMultipartUpdate(t, httpClient, update, `{"Targets":[`+fwURI("FW_BMC_0")+`]}`); status != http.StatusAccepted {
		t.Fatalf("replacement FW_BMC_0 update status %d", status)
	}
	_, bmcFW = getRedfish(t, httpClient, base+"/UpdateService/FirmwareInventory/FW_BMC_0")
	if version, state := inactiveSlotVersion(t, bmcFW); version != "BF4-26.07-0005" || state != "Inactive" {
		t.Fatalf("replacement FW_BMC_0 version %q state %q, want Inactive", version, state)
	}
}
