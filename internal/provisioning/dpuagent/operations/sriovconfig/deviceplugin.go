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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/klog/v2"
)

const (
	devicePluginConfigPath     = "var/lib/dpf/sriovdp/config.json"
	devicePluginResourcePrefix = "nvidia.com"
	mellanoxVendorID           = "15b3"
	compatibilitySFPool        = "bf_sf"
	compatibilityTrustedSFPool = "bf_sf_trusted"
)

// devicePluginConfig is the SR-IOV device-plugin configuration file.
type devicePluginConfig struct {
	ResourceList []devicePluginResource `json:"resourceList"`
}

type devicePluginResource struct {
	ResourceName   string                 `json:"resourceName"`
	ResourcePrefix string                 `json:"resourcePrefix"`
	DeviceType     string                 `json:"deviceType"`
	Selectors      []devicePluginSelector `json:"selectors"`
}

type devicePluginSelector struct {
	Vendors      []string `json:"vendors"`
	PFNames      []string `json:"pfNames"`
	IsRdma       bool     `json:"isRdma"`
	AuxTypes     []string `json:"auxTypes,omitempty"`
	NeedVhostNet bool     `json:"needVhostNet"`
}

// writeDevicePluginConfig rewrites the file from whichever plans are set. A nil plan
// (skipped or failed before plan) contributes no pools.
func (s *sriovState) writeDevicePluginConfig(rootFS string) error {
	return writeDevicePluginConfig(rootFS, buildDevicePluginConfig(s.sf, s.vf))
}

// buildDevicePluginConfig advertises only groups with a poolName.
func buildDevicePluginConfig(sf *sfPlan, vf *vfPlan) devicePluginConfig {
	// Collect sfnum / VF-index ranges per pool and port.
	sfNums := map[string]map[string][]int{}
	if sf != nil {
		for _, sf := range sf.sfs {
			if sf.poolName == "" {
				continue
			}
			addPoolNum(sfNums, sf.poolName, sf.netdev, sf.sfNum)
		}
	}

	vfNums := map[string]map[string][]int{}
	if vf != nil {
		for _, vf := range vf.vfs {
			if vf.poolName == "" {
				continue
			}
			for i := vf.index; i < vf.index+vf.count; i++ {
				addPoolNum(vfNums, vf.poolName, vf.netdev, i)
			}
		}
	}

	config := devicePluginConfig{ResourceList: []devicePluginResource{}}
	config.ResourceList = append(config.ResourceList, poolResources(sfNums, "auxNetDevice", []string{"sf"})...)
	config.ResourceList = append(config.ResourceList, poolResources(vfNums, "netDevice", nil)...)
	return config
}

func addPoolNum(nums map[string]map[string][]int, pool, netdev string, num int) {
	if nums[pool] == nil {
		nums[pool] = map[string][]int{}
	}
	nums[pool][netdev] = append(nums[pool][netdev], num)
}

// poolResources is one device-plugin resource per pool, with a selector range per port.
func poolResources(nums map[string]map[string][]int, deviceType string, auxTypes []string) []devicePluginResource {
	out := make([]devicePluginResource, 0, len(nums))
	for _, pool := range sortedKeys(nums) {
		var pfNames []string
		for _, netdev := range sortedKeys(nums[pool]) {
			for _, r := range compressRanges(nums[pool][netdev]) {
				pfNames = append(pfNames, fmt.Sprintf("%s#%d-%d", netdev, r[0], r[1]))
			}
		}
		out = append(out, devicePluginResource{
			ResourceName:   pool,
			ResourcePrefix: devicePluginResourcePrefix,
			DeviceType:     deviceType,
			Selectors: []devicePluginSelector{{
				Vendors:      []string{mellanoxVendorID},
				PFNames:      pfNames,
				IsRdma:       true,
				AuxTypes:     auxTypes,
				NeedVhostNet: true,
			}},
		})
	}
	return out
}

// writeDevicePluginConfig renders the configuration under rootFS, replacing it
// atomically so a device plugin watching the file never reads a partial write.
func writeDevicePluginConfig(rootFS string, config devicePluginConfig) error {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal device plugin config: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(rootFS, devicePluginConfigPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	klog.Infof("Wrote SR-IOV device plugin config to %s: %s", path, strings.Join(resourceNames(config), ", "))
	return nil
}

func resourceNames(config devicePluginConfig) []string {
	names := make([]string, 0, len(config.ResourceList))
	for _, r := range config.ResourceList {
		names = append(names, r.ResourcePrefix+"/"+r.ResourceName)
	}
	return names
}

// compressRanges turns a set of numbers into inclusive [start, end] ranges.
func compressRanges(nums []int) [][2]int {
	if len(nums) == 0 {
		return nil
	}
	sorted := append([]int(nil), nums...)
	sort.Ints(sorted)

	ranges := [][2]int{{sorted[0], sorted[0]}}
	for _, n := range sorted[1:] {
		last := &ranges[len(ranges)-1]
		switch n {
		case last[1]:
		case last[1] + 1:
			last[1] = n
		default:
			ranges = append(ranges, [2]int{n, n})
		}
	}
	return ranges
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
