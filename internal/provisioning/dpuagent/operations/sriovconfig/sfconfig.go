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

package sriovconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	dpuagentutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
)

const (
	MaxTrustedSfs = 10

	// Poll bound matching mlnx_bf_configure (~L820-830) while mlx5_core.sf.* appears.
	auxDiscoveryRetries         = 20
	defaultAuxDiscoveryInterval = 100 * time.Millisecond
)

// ReconcileSF creates spec.scalableFunctions and the SNAP DMA SF. Must run before ReconcileVF.
type ReconcileSF struct {
	rootFS               string
	runBash              runBashFunc
	auxDiscoveryInterval time.Duration
	dmaSFNum             int
	// dmaSFTargetDevice is the ECPF hosting the DMA SF, or empty if none.
	dmaSFTargetDevice string
}

func (s *ReconcileSF) Name() string {
	return "Reconcile SF"
}

func (s *ReconcileSF) ConditionType() string {
	return "SFReconciled"
}

func (s *ReconcileSF) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipSFConfig
}

func (s *ReconcileSF) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *ReconcileSF) Execute(execCtx context.Context, optCtx *operations.Context) error {
	s.applyDefaults()

	plan, err := s.plan(optCtx)
	if err != nil {
		return err
	}
	if err := s.reconcile(optCtx, plan); err != nil {
		return err
	}
	if err := s.verify(plan); err != nil {
		return err
	}

	return sharedState(optCtx).writeDevicePluginConfig(s.rootFS)
}

func (s *ReconcileSF) applyDefaults() {
	if s.runBash == nil {
		s.runBash = bash.Run
	}
	if s.rootFS == "" {
		s.rootFS = defaultRootFS
	}
	if s.auxDiscoveryInterval == 0 {
		s.auxDiscoveryInterval = defaultAuxDiscoveryInterval
	}
}

// plan resolves SF groups and the DMA ECPF. No mutations.
func (s *ReconcileSF) plan(optCtx *operations.Context) (*sfPlan, error) {
	if err := cutil.ValidatePoolNames(&optCtx.DPUFlavor); err != nil {
		return nil, err
	}
	ports, err := optCtx.NSPorts()
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("target physical port not found")
	}
	if err := s.planDMASF(optCtx, ports); err != nil {
		return nil, err
	}

	plan, err := resolveSFPlan(&optCtx.DPUFlavor, ports, dpuagentutil.IsBlueField4(optCtx.LatestDPU), s.dmaSFTargetDevice)
	if err != nil {
		return nil, err
	}
	sharedState(optCtx).sf = plan
	return plan, nil
}

// planDMASF resolves which ECPF hosts the SNAP DMA SF, if any.
func (s *ReconcileSF) planDMASF(optCtx *operations.Context, ports []pciutil.NICPort) error {
	s.dmaSFNum = snapDMASFNum
	s.dmaSFTargetDevice = ""

	if !optCtx.DPUFlavor.DMAEnabled() || !dpuagentutil.IsBlueField4(optCtx.LatestDPU) {
		return nil
	}
	devices := make([]string, 0, len(ports))
	for _, p := range ports {
		devices = append(devices, p.PCIAddress)
	}
	target, err := selectDMASFTarget(s.rootFS, devices)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("DPUFlavor.spec.dma.enabled is set but no eligible ibdev-less 2nd-link ECPF found: the secondary socket-direct ECPF must be silenced (vendor ENABLE_SD_MERGED_ESWITCH) on a socket-direct BlueField-4")
	}
	s.dmaSFTargetDevice = target
	klog.Infof("DMA SF (sfnum %d) target ECPF: %s", s.dmaSFNum, s.dmaSFTargetDevice)
	return nil
}

// reconcile creates SFs, confirms they exist, then sets GUIDs. The confirm step
// turns a tolerated per-SF create failure into an error before GUIDs run.
func (s *ReconcileSF) reconcile(optCtx *operations.Context, plan *sfPlan) error {
	if err := s.runHostlessWorkarounds(optCtx); err != nil {
		return err
	}

	createErrs := map[string]map[int]error{}
	for _, port := range plan.ports {
		sfs := plan.sfsOnDevice(port.PCIAddress)
		if len(sfs) == 0 && port.PCIAddress != s.dmaSFTargetDevice {
			continue
		}
		errs, err := s.createSFsOnDevice(port.PCIAddress, sfs)
		if err != nil {
			return fmt.Errorf("failed to configure SFs on device %s: %w", port.PCIAddress, err)
		}
		createErrs[pciutil.NormalizeAddress(port.PCIAddress)] = errs
	}

	// One listing for existence check and GUID pass.
	sfMap, err := s.listSFs()
	if err != nil {
		return fmt.Errorf("SF verification failed: %w", err)
	}
	for _, port := range plan.ports {
		device := pciutil.NormalizeAddress(port.PCIAddress)
		errs, visited := createErrs[device]
		if !visited {
			continue
		}
		if err := s.checkSFsCreated(sfMap, port.PCIAddress, plan.sfsOnDevice(port.PCIAddress), errs); err != nil {
			return err
		}
		if err := s.setGUIDForSFs(sfMap, port.PCIAddress); err != nil {
			return fmt.Errorf("failed to set GUID for SF: %w", err)
		}
	}
	return nil
}

// verify checks planned SFs still exist after the GUID aux rebind. Configures nothing.
//
// TODO: also verify options. mlnx-sf -a show -j already reports trust, hw_addr, and roce.
func (s *ReconcileSF) verify(plan *sfPlan) error {
	sfMap, err := s.listSFs()
	if err != nil {
		return fmt.Errorf("SF verification failed: %w", err)
	}
	for _, port := range plan.ports {
		sfs := plan.sfsOnDevice(port.PCIAddress)
		if len(sfs) == 0 && port.PCIAddress != s.dmaSFTargetDevice {
			continue
		}
		if err := s.checkSFsCreated(sfMap, port.PCIAddress, sfs, nil); err != nil {
			return err
		}
	}
	return nil
}

// runHostlessWorkarounds applies hostless-only prep before SF creation.
func (s *ReconcileSF) runHostlessWorkarounds(optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil || !optCtx.LatestDPU.Status.Hostless {
		return nil
	}
	s.ensureHostlessHugepages()
	return s.ensureHostlessEswitchSwitchdev(optCtx)
}

// ensureHostlessHugepages configures cmx_target hugepages for hostless DPUs.
// Failures are logged only: doca-hugepages is not yet reliably available on
// all hostless images, and must not block SF creation.
func (s *ReconcileSF) ensureHostlessHugepages() {
	for _, cmd := range []string{
		"doca-hugepages config --app cmx_target --size 2048 --num 4096",
		"doca-hugepages reload",
	} {
		klog.Infof("hostless workaround: %s", cmd)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			klog.Errorf("hugepages: %s: %v (stdout=%s stderr=%s)",
				cmd, err, stdout.String(), stderr.String())
		}
	}
}

// ensureHostlessEswitchSwitchdev is a workaround for hostless DPUs where the
// eSwitch can remain in legacy mode after boot. SF creation requires switchdev;
// set it on every N/S ECPF before creating SFs.
//
// Note: consider moving to a shared helper for rare scenarios with Hostless + No SFs + --skip-sf-config.
func (s *ReconcileSF) ensureHostlessEswitchSwitchdev(optCtx *operations.Context) error {
	ports, err := optCtx.NSPorts()
	if err != nil {
		return fmt.Errorf("eswitch switchdev: get N/S ports: %w", err)
	}
	for _, p := range ports {
		if p.PCIAddress == "" {
			continue
		}
		cmd := fmt.Sprintf("devlink dev eswitch set pci/%s mode switchdev", p.PCIAddress)
		klog.Infof("setting eSwitch to switchdev: %s", cmd)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			return fmt.Errorf("eswitch switchdev : %s: %w (stdout=%s stderr=%s)",
				cmd, err, stdout.String(), stderr.String())
		}
	}
	return nil
}

// createSFsOnDevice creates planned SFs on one ECPF, plus the DMA SF if this is its
// target. Counts are not checked against PF_TOTAL_SF. Per-SF create failures are
// collected; checkSFsCreated turns missing ones into the condition error.
func (s *ReconcileSF) createSFsOnDevice(device string, sfs []plannedSF) (map[int]error, error) {
	var dmaCreate, dmaSFExpected bool
	if device == s.dmaSFTargetDevice {
		// This is the single ECPF chosen to host the DMA SF. One is expected
		// here; create it unless it already exists (agent restart within a
		// boot, or vendor-created).
		exists, err := s.dmaSFExists(device)
		if err != nil {
			return nil, err
		}
		dmaSFExpected = true
		dmaCreate = !exists
	}

	createErrBySF := map[int]error{}
	for _, sf := range sfs {
		if err := s.createSF(sf); err != nil {
			klog.Warningf("Failed to create SF %d on device %s: %v", sf.sfNum, device, err)
			createErrBySF[sf.sfNum] = err
		}
	}

	// Create DMA only if missing; recreating would bounce the ibdev.
	if dmaCreate {
		if err := s.createDMASF(device, s.dmaSFNum); err != nil {
			return nil, err
		}
	}
	if dmaSFExpected {
		// Ensure the representor is up.
		s.ensureDMASFRepresentorUp(device, s.dmaSFNum)
	}
	return createErrBySF, nil
}

// createSF creates one SF. Without --hwaddr the kernel picks a random MAC.
func (s *ReconcileSF) createSF(sf plannedSF) error {
	cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d", sf.device, sf.sfNum)
	if sf.trusted {
		cmd += " -t"
	}
	if sf.controller != nil {
		cmd += fmt.Sprintf(" -C %d", *sf.controller)
	}
	if sf.cpuList != "" {
		cmd += " --cpu-list " + sf.cpuList
	}
	if sf.mac != "" {
		cmd += " --hwaddr " + sf.mac
	}
	if sf.disableRoCE {
		cmd += " --disable-roce"
	}
	stdout, stderr, err := s.runBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to create SF %d on device %s: stdout=%s, stderr=%s, err=%w", sf.sfNum, sf.device, stdout.String(), stderr.String(), err)
	}
	if sf.disableNetdev {
		return s.disableSFNetdev(sf.device, sf.sfNum)
	}
	return nil
}

// disableSFNetdev drops the SF ethernet netdev. enable_eth needs aux reload.
func (s *ReconcileSF) disableSFNetdev(device string, sfNum int) error {
	aux, err := findAuxDevice(s.rootFS, s.auxDiscoveryInterval, device, sfNum)
	if err != nil {
		return err
	}
	for _, c := range []string{
		fmt.Sprintf("devlink dev param set auxiliary/%s name enable_eth value false cmode driverinit", aux),
		fmt.Sprintf("devlink dev reload auxiliary/%s", aux),
	} {
		if stdout, stderr, err := s.runBash(c); err != nil {
			return fmt.Errorf("failed to disable SF netdev: cmd=%s, stdout=%s, stderr=%s, err=%w", c, stdout.String(), stderr.String(), err)
		}
	}
	return nil
}

// SFInfo is a subset of `mlnx-sf -a show -j`.
type SFInfo struct {
	// SFNetdev is the SF function netdev, empty when eth is disabled (DMA SF).
	SFNetdev string `json:"sf_netdev"`
	AuxDev   string `json:"aux_dev"`
	Device   string `json:"device"`
	SFNum    int    `json:"sfnum"`
	// Netdev is the DPU-side representor.
	Netdev  string `json:"netdev"`
	RDMADev string `json:"rdma_dev"`
}

// checkSFsCreated confirms planned SFs exist. createErrBySF, if set, is attached to missing-SF errors.
func (s *ReconcileSF) checkSFsCreated(sfMap map[string]SFInfo, device string, sfs []plannedSF, createErrBySF map[int]error) error {
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

	for _, sf := range sfs {
		if _, found := existingSF[sf.sfNum]; found {
			continue
		}
		if createErr, hasCreateErr := createErrBySF[sf.sfNum]; hasCreateErr {
			return createErr
		}
		return fmt.Errorf("sf %d was not found on device %s after creation", sf.sfNum, device)
	}

	if device == s.dmaSFTargetDevice {
		return verifyDMASFConsumable(dmaSFInfo, device, s.dmaSFNum)
	}
	return nil
}

// setGUIDForSFs sets each SF GUID from its netdev MAC and rebinds the aux device.
func (s *ReconcileSF) setGUIDForSFs(sfMap map[string]SFInfo, device string) error {
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
func (s *ReconcileSF) listSFs() (map[string]SFInfo, error) {
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

// findAuxDevice waits briefly for mlx5_core.sf.* of the given sfnum on the ECPF.
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
