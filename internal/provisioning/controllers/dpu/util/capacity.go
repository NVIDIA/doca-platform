/*
Copyright 2025 NVIDIA

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

package util

import (
	"fmt"
	"regexp"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// re captures the number of cores and memory size in the product description. The regex is based on the following example:
	// "Arm Cortex-A72 16 cores, 32GB on-board DDR"
	// The regex will capture "16", "32" and "GB"
	re = regexp.MustCompile(`(\d+) Arm cores.*?(\d+)(GB) on-board DDR`)

	PartNumbers = map[string]BlueFieldSpecs{
		"06CMW1":             {8, "16"},
		"0HFWRM":             {16, "32"},
		"0KK4NR":             {8, "16"},
		"0NDP41":             {16, "32"},
		"0WJ9T5":             {16, "32"},
		"0WN6RF":             {16, "32"},
		"0X5DXX":             {16, "32"},
		"0XNWR4":             {16, "32"},
		"8217991":            {16, "32"},
		"8225672":            {16, "32"},
		"900-9D3B4-00CC-EA0": {8, "16"},
		"900-9D3B4-00CV-EA0": {8, "16"},
		"900-9D3B4-00EN-EA0": {8, "16"},
		"900-9D3B4-00PN-EA0": {8, "16"},
		"900-9D3B4-00SC-EA0": {8, "16"},
		"900-9D3B4-00SV-EA0": {8, "16"},
		"900-9D3B6-00CC-EA0": {16, "32"},
		"900-9D3B6-00CN-AB0": {16, "32"},
		"900-9D3B6-00CN-PA0": {16, "32"},
		"900-9D3B6-00CN-PN0": {16, "32"},
		"900-9D3B6-00CV-AA0": {16, "32"},
		"900-9D3B6-00SC-EA0": {16, "32"},
		"900-9D3B6-00SN-AB0": {16, "32"},
		"900-9D3B6-00SV-AA0": {16, "32"},
		"900-9D3B6-F2SC-EA0": {16, "32"},
		"900-9D3B6-F2SV-PA0": {16, "32"},
		"900-9D3B6-H1CN-AB0": {16, "32"},
		"900-9D3B6-H1CN-AB1": {16, "32"},
		"900-9D3C6-00CV-DA0": {16, "48"},
		"900-9D3C6-00SV-DA0": {16, "48"},
		"900-9D3C6-B9SV-DA0": {16, "48"},
		"900-9D3D4-00EN-HA0": {8, "16"},
		"900-9D3D4-00NN-HA0": {8, "16"},
		"900-9D3D4-00NN-HAS": {8, "16"},
		"900-9D3D4-00NN-LA0": {8, "16"},
		"900-9D3L6-00CN-AA0": {16, "32"},
		"P66102-001":         {16, "32"},
		"P66584-001":         {8, "16"},
		"S3K99-63001":        {8, "16"},
		"SN37B36732":         {16, "32"},
		"SN37B82788":         {8, "16"},
	}

	Models = map[string]BlueFieldSpecs{
		// DPUs - All with 16 Arm Cores, 32GB DDR5
		// https://docs.nvidia.com/networking/display/bf3dpu
		"B3240":  {16, "32"},
		"B3220":  {16, "32"},
		"B3210E": {16, "32"},
		"B3210":  {16, "32"},

		// SuperNICs - All with 8 Arm Cores, 16GB DDR5
		// https://docs.nvidia.com/networking/display/bf3dpu
		"B3210L": {8, "16"},
		"B3220L": {8, "16"},
		"B3140L": {8, "16"},
		"B3140H": {8, "16"},

		// DPU Controller - 16 Arm Cores, 48GB DDR5
		// https://docs.nvidia.com/networking/display/bf3dpucontroller
		"B3220SH": {16, "48"},
	}
)

type BlueFieldSpecs struct {
	// CPU is the number of cores
	CPU int
	// Mem is the memory size in GB.
	// Note: GB should be converted to "Gi" or "G" before comparison.
	Mem string
}

type CapacityResult int

const (
	CapacityUnknown CapacityResult = iota
	CapacitySatisfied
	CapacityInsufficient
	CapacityRebootRequired
)

func (spec *BlueFieldSpecs) CanSatisfy(req corev1.ResourceList) CapacityResult {
	if spec == nil {
		return CapacityUnknown
	}

	converted, err := spec.convertToResourceList(req.Memory().Format)
	if err != nil {
		return CapacityUnknown
	}
	if converted.Cpu().Cmp(*req.Cpu()) >= 0 && converted.Memory().Cmp(*req.Memory()) >= 0 {
		return CapacitySatisfied
	}
	return CapacityInsufficient
}

func (spec *BlueFieldSpecs) convertToResourceList(format resource.Format) (corev1.ResourceList, error) {
	cpu, err := resource.ParseQuantity(fmt.Sprintf("%d", spec.CPU))
	if err != nil {
		return nil, fmt.Errorf("invalid CPU amount, get: %q, err: %v", spec.CPU, err)
	}

	var memSuffix string
	switch format {
	case resource.BinarySI, resource.DecimalExponent:
		memSuffix = "Gi"
	case resource.DecimalSI:
		memSuffix = "G"
	default:
		return nil, fmt.Errorf("unsupported quantity suffix %q", format)
	}

	// We assume the suffix is always "GB" in the RedFish reply. Since "GB" is not a valid resource.Quantity, we need to convert it to either "Gi" (binarySI) or "G" (decimalSI).
	// During the conversion, we use the same suffix as the flavor. For example:
	// If the flavor is requesting a "33G" memory, we will parse the "32GB" in the RedFish reply as "32G" to correctly compare them afterward.
	// The consistency of the suffixes is important. In the example above, if we we parse "32GB" as "32Gi", we will get a wrong result that a flavor with "33G" requirement can be installed on a DPU with "32GB" capacity
	// because 32Gi (34359738368) is greater than 33G (33000000000).
	mem, err := resource.ParseQuantity(spec.Mem + memSuffix)
	if err != nil {
		return nil, fmt.Errorf("invalid Mem amount, get: %s%s, err: %v", spec.Mem, memSuffix, err)
	}
	return corev1.ResourceList{
		corev1.ResourceCPU:    cpu,
		corev1.ResourceMemory: mem,
	}, nil
}

// LookUpPartNumber returns the BlueFieldSpecs for the given part number.
func LookUpPartNumber(partNumber string) *BlueFieldSpecs {
	spec, ok := PartNumbers[partNumber]
	if !ok {
		return nil
	}
	specCopy := spec
	return &specCopy
}

// LookUpModel tries to find the model name in the product description and returns the corresponding BlueFieldSpecs.
func LookUpModel(desc string) *BlueFieldSpecs {
	for model, spec := range Models {
		matched, err := regexp.MatchString(`\b`+regexp.QuoteMeta(model)+`\b`, desc)
		if err != nil || !matched {
			continue
		}
		specCopy := spec
		return &specCopy
	}
	return nil
}

// LookUpResource tries to grep the number of cores and memory size from the product description.
func LookUpResource(description string) *BlueFieldSpecs {
	matches := re.FindStringSubmatch(description)
	if len(matches) != 4 {
		return nil
	}
	cpu, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil
	}
	return &BlueFieldSpecs{CPU: cpu, Mem: matches[2]}
}

// ParseDescription parses the product description and returns the corresponding BlueFieldSpecs.
func ParseDescription(description string) *BlueFieldSpecs {
	methods := []func(string) *BlueFieldSpecs{
		LookUpModel,
		LookUpResource,
	}
	for _, method := range methods {
		if spec := method(description); spec != nil {
			return spec
		}
	}
	return nil
}
