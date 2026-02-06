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
	"fmt"
	"maps"
	"slices"
	"strings"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"

	"k8s.io/klog/v2"
)

// DevicePluginConfig is the upstream SR-IOV network device plugin configuration format.
// See: https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin
type DevicePluginConfig struct {
	ResourceList []ResourceConfig `json:"resourceList"`
}

// ResourceConfig defines a single resource in the device plugin configuration.
type ResourceConfig struct {
	// ResourceName is the name of the resource (without prefix).
	ResourceName string `json:"resourceName"`
	// ResourcePrefix is the prefix for the resource name (e.g., "nvidia.com").
	ResourcePrefix string `json:"resourcePrefix,omitempty"`
	// Selectors is a list of device selectors. Each selector typically represents
	// devices from a single DPU.
	Selectors []Selector `json:"selectors,omitempty"`
}

// Selector defines device selectors for a group of VFs (typically from one DPU).
type Selector struct {
	// RootDevices is a list of PF PCI addresses with VF range syntax.
	// Format: "<PF_PCI_ADDRESS>#<start>-<end>" (e.g., "0000:b1:00.0#2-10").
	RootDevices []string `json:"rootDevices,omitempty"`
	// IsRdma indicates whether RDMA is enabled.
	IsRdma bool `json:"isRdma,omitempty"`
}

// configBuilder builds DevicePluginConfig from input config and discovered DPU info.
// It groups VF ranges by resource key (prefix/name), then by DPU serial, ensuring
// each DPU's contribution becomes a separate selector entry.
type configBuilder struct {
	defaultPrefix    string
	resourceBuilders map[string]*resourceBuilder // keyed by "prefix/name"
}

// resourceBuilder accumulates selectors for a single resource.
type resourceBuilder struct {
	name             string
	prefix           string
	selectorBuilders map[string]*selectorBuilder // keyed by DPU serial
}

// vfRangeInfo stores start and end indices for a VF range.
type vfRangeInfo struct {
	start int32
	end   int32
}

// selectorBuilder accumulates root devices for a single DPU within a resource.
// Ranges are grouped by PF address to support multi-range syntax.
type selectorBuilder struct {
	// pfRanges maps PF address to list of VF ranges for that PF
	pfRanges map[string][]vfRangeInfo
	// isRdma is a per-selector option
	isRdma bool
}

// newConfigBuilder creates a new config builder.
func newConfigBuilder(defaultPrefix string) *configBuilder {
	return &configBuilder{
		defaultPrefix:    defaultPrefix,
		resourceBuilders: make(map[string]*resourceBuilder),
	}
}

// addResources adds device plugin resources from a specific DPU to the builder.
func (b *configBuilder) addResources(dpuInfo DPUInfo) error {
	for _, res := range dpuInfo.DevicePluginResourcesConfig {
		rb := b.getOrCreateResourceBuilder(res)
		sb := rb.getOrCreateSelectorBuilder(dpuInfo.SerialNumber)

		if res.Options != nil && res.Options.IsRdma != nil && *res.Options.IsRdma {
			sb.isRdma = true
		}

		for _, vfRange := range res.Ranges {
			pf, err := dpuInfo.GetPF(vfRange.PFIndex)
			if err != nil {
				return fmt.Errorf("failed to get PF for DPU %s, pfIndex %d: %w",
					dpuInfo.SerialNumber, vfRange.PFIndex, err)
			}
			sb.addVFRange(pf.Address, resolveVFRange(pf, vfRange))
		}
	}
	return nil
}

// build constructs the final DevicePluginConfig.
func (b *configBuilder) build() *DevicePluginConfig {
	config := &DevicePluginConfig{
		ResourceList: make([]ResourceConfig, 0, len(b.resourceBuilders)),
	}

	for _, key := range slices.Sorted(maps.Keys(b.resourceBuilders)) {
		rb := b.resourceBuilders[key]
		rc := rb.build()
		config.ResourceList = append(config.ResourceList, rc)

		klog.InfoS("Generated resource config",
			"resourceName", rc.ResourceName,
			"resourcePrefix", rc.ResourcePrefix,
			"selectorsCount", len(rc.Selectors))
	}

	return config
}

// getOrCreateResourceBuilder returns existing or creates new resource builder.
// Resources are keyed by "prefix/name" since the combination uniquely identifies a resource.
func (b *configBuilder) getOrCreateResourceBuilder(
	res noderesourcesv1.DevicePluginResource,
) *resourceBuilder {
	prefix := b.defaultPrefix
	if res.ResourcePrefix != nil && *res.ResourcePrefix != "" {
		prefix = *res.ResourcePrefix
	}

	key := resourceKey(prefix, res.Name)
	rb, exists := b.resourceBuilders[key]
	if !exists {
		rb = &resourceBuilder{
			name:             res.Name,
			prefix:           prefix,
			selectorBuilders: make(map[string]*selectorBuilder),
		}
		b.resourceBuilders[key] = rb
	}
	return rb
}

// resourceKey creates a unique key for a resource from its prefix and name.
func resourceKey(prefix, name string) string {
	return prefix + "/" + name
}

// getOrCreateSelectorBuilder returns existing or creates new selector builder for a DPU.
func (rb *resourceBuilder) getOrCreateSelectorBuilder(serial string) *selectorBuilder {
	sb, exists := rb.selectorBuilders[serial]
	if !exists {
		sb = &selectorBuilder{
			pfRanges: make(map[string][]vfRangeInfo),
		}
		rb.selectorBuilders[serial] = sb
	}
	return sb
}

// build constructs the ResourceConfig from accumulated data.
func (rb *resourceBuilder) build() ResourceConfig {
	rc := ResourceConfig{
		ResourceName:   rb.name,
		ResourcePrefix: rb.prefix,
		Selectors:      make([]Selector, 0, len(rb.selectorBuilders)),
	}

	for _, serial := range slices.Sorted(maps.Keys(rb.selectorBuilders)) {
		sb := rb.selectorBuilders[serial]
		selector := sb.build()
		rc.Selectors = append(rc.Selectors, selector)
	}

	return rc
}

// addVFRange adds a VF range for a specific PF address.
func (sb *selectorBuilder) addVFRange(pfAddr string, vfRange vfRangeInfo) {
	sb.pfRanges[pfAddr] = append(sb.pfRanges[pfAddr], vfRange)
}

// build constructs the Selector from accumulated root devices.
// Multiple ranges for the same PF are combined using comma-separated syntax.
func (sb *selectorBuilder) build() Selector {
	rootDevices := make([]string, 0, len(sb.pfRanges))
	for _, pfAddr := range slices.Sorted(maps.Keys(sb.pfRanges)) {
		rootDevices = append(rootDevices, formatRootDevice(pfAddr, sb.pfRanges[pfAddr]))
	}
	return Selector{
		RootDevices: rootDevices,
		IsRdma:      sb.isRdma,
	}
}

// buildDevicePluginConfig transforms the DPF input config format to upstream
// device plugin config. It creates separate selectors for each DPU's contribution
// to a resource.
//
// Rules:
//   - Ranges from different DPUs go to separate selectors
//   - Ranges from the same DPU within the same resource are merged into one selector
//   - If start is not set, 0 is used
//   - If end is not set, totalVFs-1 is used
func buildDevicePluginConfig(defaultResourcePrefix string, dpuInfoList []DPUInfo) (*DevicePluginConfig, error) {
	builder := newConfigBuilder(defaultResourcePrefix)
	for _, dpuInfo := range dpuInfoList {
		if err := builder.addResources(dpuInfo); err != nil {
			return nil, err
		}
	}
	return builder.build(), nil
}

// resolveVFRange resolves optional start/end values in a VFRange to concrete values.
// If start is nil, 0 is used. If end is nil, totalVFs-1 is used.
func resolveVFRange(pf *PFInfo, vfRange noderesourcesv1.VFRange) vfRangeInfo {
	start := int32(0)
	if vfRange.Start != nil {
		start = *vfRange.Start
	}
	// at this point it is guaranteed that pf.TotalVFs > 0
	end := pf.TotalVFs - 1
	if vfRange.End != nil {
		end = *vfRange.End
	}
	return vfRangeInfo{start: start, end: end}
}

// formatRootDevice formats a root device selector string with VF range syntax.
// Multiple ranges are combined using comma-separated syntax.
// Format: "<PF_PCI_ADDRESS>#<range1>,<range2>,..." (e.g., "0000:b1:00.0#2-10,15-20").
func formatRootDevice(pfAddr string, ranges []vfRangeInfo) string {
	// Sort ranges by start index for deterministic output
	slices.SortFunc(ranges, func(a, b vfRangeInfo) int {
		return int(a.start - b.start)
	})

	rangeStrs := make([]string, len(ranges))
	for i, r := range ranges {
		rangeStrs[i] = fmt.Sprintf("%d-%d", r.start, r.end)
	}

	return pfAddr + "#" + strings.Join(rangeStrs, ",")
}
