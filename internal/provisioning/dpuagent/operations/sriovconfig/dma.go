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
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
)

// snapDMASFNum is the SNAP discovery ABI (sf_num=8000 + DMA caps). Owned by the agent,
// not a scalableFunctions group.
const snapDMASFNum = 8000

// selectDMASFTarget picks the DMA ECPF (Redmine #5040591 a–f): among N/S switchdev
// ports, drop those with RDMA or sharing a PCI link with one, then first remaining BDF.
// Empty if none qualify. devices come from ctx.NSPorts() (physical N/S only).
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

// pciLink is the domain:bus of a BDF.
func pciLink(bdf string) string {
	parts := strings.SplitN(bdf, ":", 3)
	if len(parts) < 3 {
		return bdf
	}
	return parts[0] + ":" + parts[1]
}

// dmaSFExists is true if the DMA SF is already on device.
func (s *ReconcileSF) dmaSFExists(device string) (bool, error) {
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

// deviceHasRDMA is true if the ECPF exposes an ibdev.
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

func (s *ReconcileSF) findDMASF(device string, dmaSFNum int) (*SFInfo, error) {
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

// createDMASF creates the DMA SF (RoCE off, no netdev). Mirrors create_snap_dma_sf
// (mlnx_bf_configure ~L800-847).
func (s *ReconcileSF) createDMASF(device string, dmaSFNum int) error {
	mac := deriveDMASFMAC(device, dmaSFNum)

	cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d --hwaddr %s --disable-roce", device, dmaSFNum, mac)
	if stdout, stderr, err := s.runBash(cmd); err != nil {
		return fmt.Errorf("failed to create DMA SF on %s: stdout=%s, stderr=%s, err=%w", device, stdout.String(), stderr.String(), err)
	}

	if err := s.disableSFNetdev(device, dmaSFNum); err != nil {
		return err
	}

	klog.InfoS("DMA SF created", "device", device, "sfnum", dmaSFNum, "mac", mac)

	return nil
}

// ensureDMASFRepresentorUp brings the representor up, best-effort, every reconcile.
func (s *ReconcileSF) ensureDMASFRepresentorUp(device string, dmaSFNum int) {
	sf, err := s.findDMASF(device, dmaSFNum)
	if err != nil || sf == nil || sf.Netdev == "" {
		return
	}
	if _, stderr, err := s.runBash(fmt.Sprintf("ip link set %s up", sf.Netdev)); err != nil {
		klog.Warningf("Failed to bring up DMA SF representor %s: %v (stderr: %s)", sf.Netdev, err, stderr.String())
	}
}

// deriveDMASFMAC matches mlnx_bf_configure ~L797: "02:" + first 5 bytes of md5(bdf:sfnum).
func deriveDMASFMAC(device string, dmaSFNum int) string {
	sum := md5.Sum(fmt.Appendf(nil, "%s:%d", device, dmaSFNum))
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", sum[0], sum[1], sum[2], sum[3], sum[4])
}

// verifyDMASFConsumable requires an RDMA device and no SF netdev.
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
