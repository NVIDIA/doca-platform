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

package sfconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	dpuagentutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	defaultRootFS = "/"
	MaxTrustedSfs = 10

	// Aux-device discovery after creating an SF is bounded like the vendor
	// script's loop (20 x 0.1s, mlnx_bf_configure ~L820-830) while the kernel
	// instantiates the mlx5_core.sf.* device.
	auxDiscoveryRetries         = 20
	defaultAuxDiscoveryInterval = 100 * time.Millisecond
)

type CreateSF struct {
	// sysClassNet string
	rootFS  string
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	// auxDiscoveryInterval bounds the poll between aux-device lookups after
	// creating an SF; defaults to defaultAuxDiscoveryInterval.
	auxDiscoveryInterval time.Duration
	// dmaSFNum is the sfnum of the SNAP DMA SF, resolved once in Execute.
	dmaSFNum int
	// dmaSFMACOverride is the flavor's scalableFunctions.dma.macAddress, resolved
	// once in Execute. Empty means "derive a deterministic MAC".
	dmaSFMACOverride string
	// dmaSFTargetDevice is the single ECPF chosen to host the DMA SF
	// (selectDMASFTarget), resolved once in Execute. Empty means "no DMA SF to
	// handle" — either the feature is off (not BF4 / dma.sfNum unset) or no ECPF
	// qualifies; a real device BDF is never empty, so per-device checks
	// key off device == dmaSFTargetDevice.
	dmaSFTargetDevice string
}

func (s *CreateSF) Name() string {
	return "Configure SF"
}

func (s *CreateSF) ConditionType() string {
	return "SFCreated"
}

func (s *CreateSF) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipSFConfig
}

func (s *CreateSF) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *CreateSF) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if s.runBash == nil {
		s.runBash = bash.Run
	}
	if s.rootFS == "" {
		s.rootFS = defaultRootFS
	}
	if s.auxDiscoveryInterval == 0 {
		s.auxDiscoveryInterval = defaultAuxDiscoveryInterval
	}
	devices, err := s.targetDevices(optCtx)
	if err != nil {
		return err
	}

	pfTotalSF := getPFTotalSFFromFlavor(&optCtx.DPUFlavor)
	trustedSF := getTrustedSFFromFlavor(&optCtx.DPUFlavor)

	// Resolve the DMA SF parameters from the flavor's scalableFunctions.dma
	// field (the dpu-agent reads no DMA SF config from mlnx-bf.conf). dma.enabled
	// opts into creation; dma.sfNum defaults to the SNAP discovery ABI sfnum.
	s.dmaSFNum = snapDMASFNum
	dma := ptr.Deref(optCtx.DPUFlavor.Spec.ScalableFunctions, provisioningv1.ScalableFunctions{}).DMA
	// The SNAP DMA SF is a BlueField-4 socket-direct feature; gate agent-owned
	// creation on BF4 so a stray scalableFunctions.dma.enabled in a shared flavor
	// never triggers it on another generation.
	dmaSFEnabled := dpuagentutil.IsBlueField4(optCtx.LatestDPU) && dma != nil && ptr.Deref(dma.Enabled, false)
	if dmaSFEnabled {
		s.dmaSFNum = int(ptr.Deref(dma.SFNum, snapDMASFNum))
		s.dmaSFMACOverride = ptr.Deref(dma.MACAddress, "")

		// Pick the single ECPF that hosts the DMA SF up front (Redmine #5040591
		// a–f), so per-device handling is a simple target check rather than an
		// independent decision on every ECPF.
		s.dmaSFTargetDevice, err = selectDMASFTarget(s.rootFS, devices)
		if err != nil {
			return err
		}
		if s.dmaSFTargetDevice == "" {
			// scalableFunctions.dma.enabled is an explicit opt-in, so a missing
			// target is a real misconfiguration (the secondary socket-direct ECPF
			// is not silenced, or this is not a socket-direct BF4) — not something
			// to skip silently, which would leave the DPU Ready with NVMesh at half
			// bandwidth. Fail with a visible condition instead.
			return fmt.Errorf("DPUFlavor.spec.scalableFunctions.dma.enabled is set but no eligible ibdev-less 2nd-link ECPF found: the secondary socket-direct ECPF must be silenced (vendor ENABLE_SD_MERGED_ESWITCH) on a socket-direct BlueField-4")
		}
		klog.Infof("DMA SF (sfnum %d) target ECPF: %s", s.dmaSFNum, s.dmaSFTargetDevice)
	}

	for _, device := range devices {
		if err := s.configureSFsOnDevice(device, pfTotalSF, trustedSF); err != nil {
			return fmt.Errorf("failed to configure SFs on device %s: %w", device, err)
		}
	}
	return nil
}

func (s *CreateSF) configureSFsOnDevice(device string, pfTotalSF, trustedSF int) error {
	var dmaCreate, dmaSFExpected bool
	if device == s.dmaSFTargetDevice {
		// This is the single ECPF chosen to host the DMA SF. One is expected
		// here; create it unless it already exists (agent restart within a
		// boot, or vendor-created).
		exists, err := s.dmaSFExists(device)
		if err != nil {
			return err
		}
		dmaSFExpected = true
		dmaCreate = !exists
	}

	// Reserve a PF_TOTAL_SF slot for the DMA SF on that ECPF, so one fewer
	// workload SF is created there.
	dmaSFs := 0
	if dmaSFExpected {
		dmaSFs = 1
	}

	normalSFCount := pfTotalSF - trustedSF - dmaSFs
	if normalSFCount < 0 {
		// The flavor asks for more trusted SFs (plus the DMA SF) than the
		// PF_TOTAL_SF budget can hold. Fail with the root cause instead of
		// proceeding to a doomed trusted-SF create whose error would only name
		// the last SF, not the over-capacity misconfiguration.
		return fmt.Errorf("insufficient SF capacity on device %s: PF_TOTAL_SF=%d cannot fit %d trusted SF(s) and %d DMA SF(s)",
			device, pfTotalSF, trustedSF, dmaSFs)
	}
	if dmaSFs > 0 {
		klog.Infof("One slot is reserved for the DMA SF (sfnum %d) on device %s, reducing created SF count to %d", s.dmaSFNum, device, normalSFCount)
	}

	createErrBySF := map[int]error{}
	expectedSFNums := make([]int, 0, pfTotalSF)

	// System SF(index 0) has been removed, so DPF will create SF from index 0
	for i := 0; i < normalSFCount; i++ {
		expectedSFNums = append(expectedSFNums, i)
		// Create SFs with random mac, kernel will allocate random MAC for SF netdev
		cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d", device, i)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			// Continue on error (like "|| true" in bash)
			klog.Warningf("Failed to create SF %d on device %s: stdout=%s, stderr=%s, err=%v", i, device, stdout.String(), stderr.String(), err)
			createErrBySF[i] = fmt.Errorf("failed to create SF %d on device %s: stdout=%s, stderr=%s, err=%w", i, device, stdout.String(), stderr.String(), err)
		}
	}

	// Create trusted SFs starting from index 101
	for i := 101; i <= 100+trustedSF; i++ {
		expectedSFNums = append(expectedSFNums, i)
		cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d -t", device, i)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			// Continue on error (like "|| true" in bash)
			klog.Warningf("Failed to create trusted SF %d on device %s: stdout=%s, stderr=%s, err=%v", i, device, stdout.String(), stderr.String(), err)
			createErrBySF[i] = fmt.Errorf("failed to create trusted SF %d on device %s: stdout=%s, stderr=%s, err=%w", i, device, stdout.String(), stderr.String(), err)
		}
	}
	// Create the DMA SF on this silenced ECPF (fresh-create path only — a
	// pre-existing SF must not be re-created, its aux reload would bounce the
	// ibdev while a consumer may be using it).
	if dmaCreate {
		if err := s.createDMASF(device, s.dmaSFNum); err != nil {
			return err
		}
		// Note: not adding the dma sf to expectedSFNums because verification is handled separately (inside verifyExpectedSFs).
	}
	if dmaSFExpected {
		// Ensure the representor is up.
		s.ensureDMASFRepresentorUp(device, s.dmaSFNum)
	}

	if err := s.verifyExpectedSFs(device, expectedSFNums, createErrBySF, dmaSFExpected); err != nil {
		return err
	}

	// Set GUID for SF
	if err := s.setGUIDForSF(device); err != nil {
		return fmt.Errorf("failed to set GUID for SF: %w", err)
	}
	return nil
}

func (s *CreateSF) targetDevices(ctx *operations.Context) ([]string, error) {
	ports, err := ctx.NSPorts()
	if err != nil {
		return nil, err
	}

	devices := []string{}
	if dpuagentutil.IsBlueField4(ctx.LatestDPU) {
		for _, p := range ports {
			devices = append(devices, p.PCIAddress)
		}
	} else {
		// SFs are created on p0 for non-BF4 BlueField generations.
		for _, p := range ports {
			if p.Netdev == "p0" {
				devices = append(devices, p.PCIAddress)
				break
			}
		}
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("target physical port not found")
	}
	return devices, nil
}

// SFInfo represents fields parsed from mlnx-sf -a show -j.
// Example output:
//
//	{
//	    "pci/0000:03:00.0/229376": {
//	        "type": "eth",
//	        "netdev": "en3f0pf0sf0",
//	        "flavour": "pcisf",
//	        "controller": 0,
//	        "pfnum": 0,
//	        "sfnum": 0,
//	        "splittable": false,
//	        "function": {
//	            "hw_addr": "42:f9:a8:cf:b6:1e",
//	            "state": "active",
//	            "opstate": "attached",
//	            "roce": "enable",
//	            "trust": "off",
//	            "max_uc_macs": 128,
//	            "max_io_eqs": 8,
//	            "eswitch": "NA"
//	        },
//	        "device": "0000:03:00.0",
//	        "sfindex": "pci/0000:03:00.0/229376",
//	        "aux_dev": "mlx5_core.sf.2",
//	        "sf_netdev": "enp3s0f0s0",
//	        "rdma_dev": "mlx5_0"
//	    }
//	}
//
//nolint:misspell // Keep original field names from mlnx-sf output example.
type SFInfo struct {
	// SFNetdev is the SF's own function netdev (absent when its eth netdev is
	// disabled, as for the DMA SF); distinct from the representor Netdev.
	SFNetdev string `json:"sf_netdev"`
	AuxDev   string `json:"aux_dev"`
	Device   string `json:"device"`
	SFNum    int    `json:"sfnum"`
	// Netdev is the DPU-side SF representor netdev.
	Netdev string `json:"netdev"`
	// RDMADev is the SF's RDMA device; absent when the SF exposes no ibdev.
	RDMADev string `json:"rdma_dev"`
}

// verifyExpectedSFs checks that every expected workload SF exists on device
// and, when dmaSFExpected, that the DMA SF is consumable (see
// verifyDMASFConsumable).
func (s *CreateSF) verifyExpectedSFs(device string, expectedSFNums []int, createErrBySF map[int]error, dmaSFExpected bool) error {
	sfMap, err := s.listSFs()
	if err != nil {
		return fmt.Errorf("SF verification failed: %w", err)
	}

	existingSF := map[int]struct{}{}
	var dmaSFInfo *SFInfo
	for _, info := range sfMap {
		if pciutil.NormalizeAddress(info.Device) != pciutil.NormalizeAddress(device) {
			continue
		}
		existingSF[info.SFNum] = struct{}{}
		if info.SFNum == s.dmaSFNum {
			dmaSFInfo = &info
		}
	}

	for _, sfnum := range expectedSFNums {
		if _, found := existingSF[sfnum]; found {
			continue
		}
		if createErr, hasCreateErr := createErrBySF[sfnum]; hasCreateErr {
			return createErr
		}
		return fmt.Errorf("sf %d was not found on device %s after creation", sfnum, device)
	}

	if dmaSFExpected {
		if err := verifyDMASFConsumable(dmaSFInfo, device, s.dmaSFNum); err != nil {
			return err
		}
	}

	return nil
}

func (s *CreateSF) setGUIDForSF(device string) error {
	sfMap, err := s.listSFs()
	if err != nil {
		return err
	}

	if s.rootFS == "" {
		s.rootFS = defaultRootFS
	}
	// Iterate over each SF
	for key, info := range sfMap {
		if pciutil.NormalizeAddress(info.Device) != pciutil.NormalizeAddress(device) {
			continue
		}
		// Skip SFs with no netdev: there is no MAC to read for the GUID. This
		// also covers the DMA SF, whose netdev is deliberately disabled
		// (enable_eth=false) and asserted absent by verifyDMASFConsumable before
		// this runs — so its aux device is never rebound, which would otherwise
		// resurrect that netdev.
		if info.SFNetdev == "" {
			klog.Infof("Skipping GUID setup for SF %s: it has no netdev", key)
			continue
		}
		// Read the MAC address from the system file
		macPath := filepath.Join(s.rootFS, "sys/class/net", info.SFNetdev, "address")
		macBytes, err := os.ReadFile(macPath)
		if err != nil {
			klog.Warningf("Failed to read MAC address for %s: %v", info.SFNetdev, err)
			continue
		}
		macAddress := strings.TrimSpace(string(macBytes))

		// Update the MAC address using mlxdevm
		cmd := fmt.Sprintf("/opt/mellanox/iproute2/sbin/mlxdevm port function set %s hw_addr %s", key, macAddress)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			klog.Warningf("Failed to set hw_addr for %s: stdout=%s, stderr=%s, err=%v", key, stdout.String(), stderr.String(), err)
			continue
		}

		// Unbind the auxiliary device
		unbindPath := filepath.Join(s.rootFS, "sys/bus/auxiliary/devices", info.AuxDev, "driver/unbind")
		if err := os.WriteFile(unbindPath, []byte(info.AuxDev), 0644); err != nil {
			return fmt.Errorf("failed to unbind aux device %s: %w", info.AuxDev, err)
		}

		// Bind the auxiliary device
		bindPath := filepath.Join(s.rootFS, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")
		if err := os.WriteFile(bindPath, []byte(info.AuxDev), 0644); err != nil {
			return fmt.Errorf("failed to bind aux device %s: %w", info.AuxDev, err)
		}

		klog.Infof("Successfully set GUID for SF %s", key)
	}
	return nil
}

// listSFs runs "mlnx-sf -a show -j" and returns the parsed SF map.
func (s *CreateSF) listSFs() (map[string]SFInfo, error) {
	stdout, stderr, err := s.runBash("mlnx-sf -a show -j")
	if err != nil {
		return nil, fmt.Errorf("failed to run mlnx-sf: stdout=%s, stderr=%s, err=%w", stdout.String(), stderr.String(), err)
	}
	var sfMap map[string]SFInfo
	if err := json.Unmarshal(stdout.Bytes(), &sfMap); err != nil {
		return nil, fmt.Errorf("failed to parse mlnx-sf output: %w", err)
	}
	return sfMap, nil
}

// findAuxDevice locates the mlx5_core.sf.* auxiliary device of the SF with the
// given sfnum under the target ECPF's PCI device, retrying briefly while the
// kernel instantiates it.
func findAuxDevice(rootFS string, interval time.Duration, device string, sfNum int) (string, error) {
	pattern := filepath.Join(rootFS, "sys/bus/pci/devices", device, "mlx5_core.sf.*")
	want := strconv.Itoa(sfNum)
	for attempt := 0; attempt < auxDiscoveryRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(interval)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", fmt.Errorf("failed to glob %s: %w", pattern, err)
		}
		for _, match := range matches {
			data, err := os.ReadFile(filepath.Join(match, "sfnum"))
			if err != nil {
				continue
			}
			if strings.TrimSpace(string(data)) == want {
				return filepath.Base(match), nil
			}
		}
	}
	return "", fmt.Errorf("auxiliary device of the SF (sfnum %d) not found under %s", sfNum, pattern)
}

func getPFTotalSFFromFlavor(flavor *provisioningv1.DPUFlavor) int {
	regex := regexp.MustCompile(`^PF_TOTAL_SF=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					return num
				}
			}
		}
	}
	return 0
}

func getTrustedSFFromFlavor(flavor *provisioningv1.DPUFlavor) int {
	if flavor.Annotations != nil {
		trustedSFCountFromAnnotation, found := flavor.Annotations[cutil.TrustedSFCount]
		if found {
			trustedSFCount, err := strconv.Atoi(trustedSFCountFromAnnotation)
			if err == nil && trustedSFCount > 0 && trustedSFCount <= MaxTrustedSfs {
				return trustedSFCount
			}

		}
	}
	return 0
}
