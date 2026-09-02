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

package initcontainer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

// PFInfo contains information about a Physical Function.
type PFInfo struct {
	// Address is the PCI address of the PF (e.g., "0000:b1:00.0").
	Address string
	// TotalVFs is the maximum number of VFs supported by this PF (from sriov_totalvfs).
	TotalVFs int32
}

// DPUInfo contains information about a discovered DPU on the local node.
type DPUInfo struct {
	// SerialNumber is the DPU serial number.
	SerialNumber string
	// BaseAddress is the PCI base address without function number (e.g., "0000:b1:00").
	BaseAddress string
	// PFs is the map of PF index to PF info. Only contains PFs referenced in the config.
	PFs map[int32]*PFInfo
	// DevicePluginResourcesConfig is the list of device plugin resources configuration for this DPU.
	DevicePluginResourcesConfig []noderesourcesv1.DevicePluginResource
}

// GetPF returns the PF info for the PF at the given index.
func (d *DPUInfo) GetPF(pfIndex int32) (*PFInfo, error) {
	pf, ok := d.PFs[pfIndex]
	if !ok {
		return nil, fmt.Errorf("PF%d not found for DPU %s", pfIndex, d.SerialNumber)
	}
	return pf, nil
}

// expectedDPU is an internal representation of a DPU required by the input
// config. It contains the DPU serial, referenced PF indexes, and the DPU's
// device plugin resources config.
type expectedDPU struct {
	serial                      string
	pfIndexes                   []int32
	devicePluginResourcesConfig []noderesourcesv1.DevicePluginResource
}

// discoverDPUsAndWaitForReadiness discovers DPUs and waits for required PFs to
// have VF creation completed for requested VF resources and every SF in each
// configured SF range ready.
// Only PFs explicitly mentioned in the input config are discovered and waited for.
func discoverDPUsAndWaitForReadiness(ctx context.Context,
	clk clock.WithTicker,
	sysFSRoot string,
	inputConfig common.NodeInputConfig,
	devicesReadinessTimeout time.Duration,
	devicesReadinessPollInterval time.Duration,
) ([]DPUInfo, error) {
	expectedDPUs := getExpectedDPUsFromInputConfig(inputConfig)
	klog.InfoS("Waiting for required PFs to be ready", "dpuCount", len(expectedDPUs))

	timeout := clk.After(devicesReadinessTimeout)
	ticker := clk.NewTicker(devicesReadinessPollInterval)
	defer ticker.Stop()

	for {
		dpuInfoList, notReadyReasons, err := tryDiscoverDPUsAndCheckReadiness(sysFSRoot, expectedDPUs)
		if err != nil {
			return nil, fmt.Errorf("failed to discover DPUs and check readiness: %w", err)
		}
		if len(notReadyReasons) == 0 {
			klog.InfoS("all required DPUs are ready")
			return dpuInfoList, nil
		}
		klog.InfoS("not all required DPUs are ready, will retry", "reasons", notReadyReasons)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for required PFs, not ready reasons: %v", notReadyReasons)
		case <-ticker.C():
			// continue to next iteration
		}
	}
}

// getExpectedDPUsFromInputConfig returns DPUs and PF indexes from the input
// config that need to be waited for. The function expects that the input config
// is already validated.
func getExpectedDPUsFromInputConfig(inputConfig common.NodeInputConfig) []expectedDPU {
	dpuPFs := make(map[string]map[int32]struct{})
	for serial, resources := range inputConfig {
		for _, res := range resources {
			for _, fnRange := range res.Ranges {
				if dpuPFs[serial] == nil {
					dpuPFs[serial] = make(map[int32]struct{})
				}
				dpuPFs[serial][fnRange.PFIndex] = struct{}{}
			}
		}
	}
	result := make([]expectedDPU, 0, len(dpuPFs))
	for _, serial := range slices.Sorted(maps.Keys(dpuPFs)) {
		result = append(result, expectedDPU{
			serial:                      serial,
			pfIndexes:                   slices.Sorted(maps.Keys(dpuPFs[serial])),
			devicePluginResourcesConfig: inputConfig[serial],
		})
	}
	return result
}

// tryDiscoverDPUsAndCheckReadiness scans for the expected DPUs and verifies
// the readiness of the required PFs.
//
// Terminal errors: DPU missing from node, PF discovery
// failure, or SR-IOV not enabled (sriov_totalvfs == 0) when VFs are requested.
//
// Retryable conditions (returned as not-ready reasons): requested VFs or SFs
// have not been created yet.
func tryDiscoverDPUsAndCheckReadiness(sysFSRoot string, expectedDPUs []expectedDPU) ([]DPUInfo, []string, error) {
	devices, err := util.DiscoverDPUs(sysFSRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to discover DPUs: %w", err)
	}
	serialToDiscoveredDPU := make(map[string]util.Device)
	for _, dev := range devices {
		if dev.SerialNumber != "" {
			serialToDiscoveredDPU[dev.SerialNumber] = dev
		}
	}
	dpuInfoList := make([]DPUInfo, 0, len(expectedDPUs))
	var notReadyReasons []string
	var errs []error

	for _, dpu := range expectedDPUs {
		dev, ok := serialToDiscoveredDPU[dpu.serial]
		if !ok {
			errs = append(errs, fmt.Errorf("DPU %s: not found on node", dpu.serial))
			continue
		}
		dpuInfo := &DPUInfo{
			SerialNumber:                dpu.serial,
			BaseAddress:                 dev.Address,
			PFs:                         make(map[int32]*PFInfo),
			DevicePluginResourcesConfig: dpu.devicePluginResourcesConfig,
		}
		for _, pfIndex := range dpu.pfIndexes {
			pfInfo, err := discoverPF(sysFSRoot, dev.Address, pfIndex)
			if err != nil {
				errs = append(errs, fmt.Errorf("DPU %s: failed to discover PF%d: %w",
					dpu.serial, pfIndex, err))
				continue
			}
			needVF, needSF := functionNeedsForPF(dpu.devicePluginResourcesConfig, pfIndex)
			if needVF && pfInfo.TotalVFs == 0 {
				errs = append(errs, fmt.Errorf("DPU %s: PF%d (%s): has SR-IOV disabled",
					dpu.serial, pfIndex, pfInfo.Address))
				continue
			}
			if needSF {
				pfPath := util.NewPCIHelper(pfInfo.Address).SetSysFS(sysFSRoot).Path()
				present, err := listDiscoveredSFNums(pfPath)
				if err != nil {
					errs = append(errs, fmt.Errorf("DPU %s: PF%d (%s): failed to discover SFs: %w",
						dpu.serial, pfIndex, pfInfo.Address, err))
					continue
				}
				for _, r := range configuredSFRangesForPF(dpu.devicePluginResourcesConfig, pfIndex) {
					if !sfRangeFullyPresent(*r.Start, *r.End, present) {
						notReadyReasons = append(notReadyReasons,
							fmt.Sprintf("DPU %s PF%d (%s): SF range %d-%d is not fully present",
								dpu.serial, pfIndex, pfInfo.Address, *r.Start, *r.End))
					}
				}
			}
			dpuInfo.PFs[pfIndex] = pfInfo

			if needVF && !pfHasVFs(sysFSRoot, pfInfo.Address) {
				notReadyReasons = append(notReadyReasons,
					fmt.Sprintf("DPU %s PF%d (%s): no VFs created yet", dpu.serial, pfIndex, pfInfo.Address))
			}
		}
		dpuInfoList = append(dpuInfoList, *dpuInfo)
	}
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return dpuInfoList, notReadyReasons, nil
}

// discoverPF discovers a specific PF and reads its totalVFs.
func discoverPF(sysFSRoot, baseAddress string, pfIndex int32) (*PFInfo, error) {
	helper := util.NewPCIHelper(baseAddress).SetSysFS(sysFSRoot).PF(int(pfIndex))
	pfPath := helper.Path()

	if _, err := os.Stat(pfPath); err != nil {
		return nil, fmt.Errorf("PF%d not found: %w", pfIndex, err)
	}

	pfAddr := filepath.Base(pfPath)
	totalVFs, err := readTotalVFs(pfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read totalVFs: %w, check if SR-IOV is enabled", err)
	}

	return &PFInfo{
		Address:  pfAddr,
		TotalVFs: totalVFs,
	}, nil
}

// pfHasVFs reports whether VF creation has completed for a PF. A nonzero
// sriov_numvfs value indicates that the kernel has added the complete VF batch.
// Read failures leave the PF not ready and are retried by the polling loop.
func pfHasVFs(sysFSRoot, pfAddr string) bool {
	pfPath := util.NewPCIHelper(pfAddr).SetSysFS(sysFSRoot).Path()
	data, err := os.ReadFile(filepath.Join(pfPath, "sriov_numvfs"))
	if err != nil {
		return false
	}
	numVFs, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return err == nil && numVFs > 0
}

// functionNeedsForPF reports whether the input config requests VFs and/or SFs
// on the given PF.
func functionNeedsForPF(resources []noderesourcesv1.DevicePluginResource, pfIndex int32) (needVF, needSF bool) {
	for _, res := range resources {
		for _, r := range res.Ranges {
			if r.PFIndex != pfIndex {
				continue
			}
			switch res.Type {
			case noderesourcesv1.DevicePluginResourceTypeVF:
				needVF = true
			case noderesourcesv1.DevicePluginResourceTypeSF:
				needSF = true
			}
		}
	}
	return needVF, needSF
}

// configuredSFRangesForPF returns SF ranges that apply to the given PF.
// The input config is expected to already require start and end for type sf.
func configuredSFRangesForPF(resources []noderesourcesv1.DevicePluginResource, pfIndex int32) []noderesourcesv1.FunctionRange {
	var ranges []noderesourcesv1.FunctionRange
	for _, res := range resources {
		if res.Type != noderesourcesv1.DevicePluginResourceTypeSF {
			continue
		}
		for _, r := range res.Ranges {
			if r.PFIndex == pfIndex {
				ranges = append(ranges, r)
			}
		}
	}
	return ranges
}

// listDiscoveredSFNums returns the set of sfnum values found under the PF
// (mlx5_core.sf.*/sfnum). Entries with unreadable sfnum files are skipped.
func listDiscoveredSFNums(pfPath string) (map[int32]struct{}, error) {
	matches, err := filepath.Glob(filepath.Join(pfPath, "mlx5_core.sf.*"))
	if err != nil {
		return nil, fmt.Errorf("failed to list SFs: %w", err)
	}
	present := make(map[int32]struct{}, len(matches))
	for _, match := range matches {
		data, err := os.ReadFile(filepath.Join(match, "sfnum"))
		if err != nil {
			continue
		}
		sfnum, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		present[int32(sfnum)] = struct{}{}
	}
	return present, nil
}

// sfRangeFullyPresent reports whether every integer index in [start, end] is
// present. SF ranges are contiguous; holes are treated as not ready.
// start must be <= end. The loop exits on i == end to avoid int32 overflow.
func sfRangeFullyPresent(start, end int32, present map[int32]struct{}) bool {
	for i := start; ; {
		if _, ok := present[i]; !ok {
			return false
		}
		if i == end {
			return true
		}
		i++
	}
}

// readTotalVFs reads sriov_totalvfs for a PF given its sysfs path.
func readTotalVFs(pfPath string) (int32, error) {
	totalVFsPath := filepath.Join(pfPath, "sriov_totalvfs")
	data, err := os.ReadFile(totalVFsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	totalVFs, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse sriov_totalvfs: %w", err)
	}
	return int32(totalVFs), nil
}
