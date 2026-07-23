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
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
)

// snapDMASFNum is the default sfnum of the SNAP DMA SF on BlueField-4
// socket-direct systems. It is a discovery ABI: SNAP identifies the DMA SF as
// "SF with sf_num=8000 + DMA caps" (doca_devemu_pci_cap_is_dma_dev), so a
// different value yields an SF SNAP cannot discover. It is the value operators
// set for the DPUFlavor's scalableFunctions.dma.sfNum field.
const snapDMASFNum = 8000

// selectDMASFTarget picks the single ECPF that should host the DMA SF,
// mirroring the vendor's create_snap_dma_sf target selection (Redmine
// #5040591, steps a–f): among the switchdev N/S ECPFs, eliminate any that
// expose an RDMA device (d) or share a PCI link (domain:bus) with one that
// does (e), then take the first remaining, sorted by BDF for determinism (f).
// Returns "" when none qualify (not socket-direct, or the 2nd-link ECPF is not
// silenced). ASTRA/HW-multiplane ECPFs (step a) are excluded upstream: devices
// comes from ctx.NSPorts(), i.e. devlink "physical"-flavour N/S ports.
func selectDMASFTarget(rootFS string, devices []string) (string, error) {
	sorted := append([]string(nil), devices...)
	sort.Strings(sorted)

	hasRDMA := map[string]bool{}
	linksWithRDMA := map[string]bool{}
	for _, dev := range sorted {
		rdma, err := deviceHasRDMA(rootFS, dev)
		if err != nil {
			return "", err
		}
		hasRDMA[dev] = rdma
		if rdma {
			linksWithRDMA[pciLink(dev)] = true
		}
	}
	for _, dev := range sorted {
		if hasRDMA[dev] || linksWithRDMA[pciLink(dev)] {
			continue
		}
		return dev, nil
	}
	return "", nil
}

// pciLink returns the "domain:bus" prefix of a BDF ("0000:03" for
// "0000:03:00.0") — the vendor's `cut -d: -f1-2`.
func pciLink(bdf string) string {
	parts := strings.SplitN(bdf, ":", 3)
	if len(parts) < 3 {
		return bdf
	}
	return parts[0] + ":" + parts[1]
}

// dmaSFExists reports whether the DMA SF (sfnum s.dmaSFNum) already exists on
// device — an agent restart within a boot, or vendor-created.
func (s *CreateSF) dmaSFExists(device string) (bool, error) {
	sfMap, err := s.listSFs()
	if err != nil {
		return false, fmt.Errorf("failed to inspect DMA SF: %w", err)
	}
	for _, info := range sfMap {
		if info.SFNum == s.dmaSFNum && pciutil.NormalizeAddress(info.Device) == pciutil.NormalizeAddress(device) {
			return true, nil
		}
	}
	return false, nil
}

// deviceHasRDMA reports whether the ECPF exposes an RDMA (ibdev) device. The
// silenced secondary ECPF that hosts the DMA SF exposes none.
func deviceHasRDMA(rootFS, device string) (bool, error) {
	ibDir := filepath.Join(rootFS, "sys/bus/pci/devices", device, "infiniband")
	entries, err := os.ReadDir(ibDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read %s: %w", ibDir, err)
	}
	return len(entries) > 0, nil
}

// findDMASF returns the DMA SF entry (sfnum dmaSFNum) on device from mlnx-sf,
// or nil if it does not exist.
func (s *CreateSF) findDMASF(device string, dmaSFNum int) (*SFInfo, error) {
	sfMap, err := s.listSFs()
	if err != nil {
		return nil, err
	}
	for _, info := range sfMap {
		if info.SFNum == dmaSFNum && pciutil.NormalizeAddress(info.Device) == pciutil.NormalizeAddress(device) {
			return &info, nil
		}
	}
	return nil, nil
}

// createDMASF creates the DMA SF (sfnum dmaSFNum) on the silenced ECPF with
// RoCE disabled, disables its netdev (the SF is consumed as an ibdev only), and
// brings its representor up (best-effort, never added to a bridge). Mirrors the
// vendor script's create_snap_dma_sf (mlnx_bf_configure ~L800-847, mlnx-tools
// v2604.0.17).
func (s *CreateSF) createDMASF(device string, dmaSFNum int) error {
	if dmaSFNum != snapDMASFNum {
		klog.Warningf("scalableFunctions.dma.sfNum=%d differs from the SNAP discovery ABI sfnum %d: SNAP will not discover the DMA SF",
			dmaSFNum, snapDMASFNum)
	}
	mac, err := dmaSFMAC(s.dmaSFMACOverride, device, dmaSFNum)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d --hwaddr %s --disable-roce", device, dmaSFNum, mac)
	if stdout, stderr, err := s.runBash(cmd); err != nil {
		return fmt.Errorf("failed to create DMA SF on %s: stdout=%s, stderr=%s, err=%w", device, stdout.String(), stderr.String(), err)
	}

	aux, err := findAuxDevice(s.rootFS, s.auxDiscoveryInterval, device, dmaSFNum)
	if err != nil {
		return err
	}
	for _, c := range []string{
		fmt.Sprintf("devlink dev param set auxiliary/%s name enable_eth value false cmode driverinit", aux),
		fmt.Sprintf("devlink dev reload auxiliary/%s", aux),
	} {
		if stdout, stderr, err := s.runBash(c); err != nil {
			return fmt.Errorf("failed to disable DMA SF netdev: cmd=%s, stdout=%s, stderr=%s, err=%w", c, stdout.String(), stderr.String(), err)
		}
	}

	klog.InfoS("DMA SF created", "device", device, "sfnum", dmaSFNum, "mac", mac, "aux", aux)

	return nil
}

// ensureDMASFRepresentorUp brings the DMA SF's representor netdev up,
// best-effort and idempotently. It runs on every reconcile — both after a fresh
// create and for a pre-existing DMA SF (agent restart / vendor-created), which
// would otherwise never (re)assert it. Non-disruptive: `ip link set up` touches
// only the representor and is a no-op when it is already up — no aux reload, so
// no ibdev bounce for an in-use consumer. The representor must never be attached
// to br-sfc — self-enforcing, since no ServiceInterface references it.
func (s *CreateSF) ensureDMASFRepresentorUp(device string, dmaSFNum int) {
	sf, err := s.findDMASF(device, dmaSFNum)
	if err != nil || sf == nil || sf.Netdev == "" {
		return
	}
	if _, stderr, err := s.runBash(fmt.Sprintf("ip link set %s up", sf.Netdev)); err != nil {
		klog.Warningf("Failed to bring up DMA SF representor %s: %v (stderr: %s)", sf.Netdev, err, stderr.String())
	}
}

// dmaSFMAC returns the MAC for the DMA SF: the flavor's
// scalableFunctions.dma.macAddress override if set, otherwise the
// script-compatible derivation over the SF's sfnum.
func dmaSFMAC(override, device string, dmaSFNum int) (string, error) {
	if override != "" {
		hw, err := net.ParseMAC(override)
		if err != nil {
			return "", fmt.Errorf("invalid scalableFunctions.dma.macAddress %q: %w", override, err)
		}
		// net.ParseMAC also accepts EUI-64 and InfiniBand addresses and various
		// separators; require a 48-bit Ethernet MAC and return the canonical
		// colon-separated form for mlnx-sf --hwaddr rather than the raw value.
		if len(hw) != 6 {
			return "", fmt.Errorf("invalid scalableFunctions.dma.macAddress %q: must be a 48-bit Ethernet MAC", override)
		}
		return hw.String(), nil
	}
	return deriveDMASFMAC(device, dmaSFNum), nil
}

// deriveDMASFMAC derives the SF MAC exactly like the vendor script
// (mlnx_bf_configure ~L797, mlnx-tools v2604.0.17): "02:" followed by the first
// 5 bytes of md5("<bdf>:<sfnum>"). Byte-identical to the vendor path.
func deriveDMASFMAC(device string, dmaSFNum int) string {
	sum := md5.Sum(fmt.Appendf(nil, "%s:%d", device, dmaSFNum))
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", sum[0], sum[1], sum[2], sum[3], sum[4])
}

// verifyDMASFConsumable errors unless the DMA SF exposes an RDMA device and its
// own netdev is absent (really disabled) — the closest boot-time proxy for "a
// consumer's discovery of sfnum + DMA caps will succeed".
func verifyDMASFConsumable(sf *SFInfo, device string, dmaSFNum int) error {
	if sf == nil {
		return fmt.Errorf("DMA SF (sfnum %d) not found on %s after creation", dmaSFNum, device)
	}
	if sf.RDMADev == "" {
		return fmt.Errorf("DMA SF on %s (aux %s) exposes no RDMA device; consumer discovery would fail", device, sf.AuxDev)
	}
	if sf.SFNetdev != "" {
		return fmt.Errorf("DMA SF netdev %s on %s is still present; disabling it did not take effect", sf.SFNetdev, device)
	}
	return nil
}
