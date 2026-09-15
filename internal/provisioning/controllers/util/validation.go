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

package util

import (
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/utils/ptr"
)

// The SR-IOV validations below are pure functions of the DPUFlavor spec, so the
// validating webhook and the dpu-agent share them. Running them at admission reports a
// misconfiguration as a rejected API request, instead of leaving the dpu-agent to retry
// a spec it will never satisfy. The agent keeps its own call for flavors that were
// admitted before these checks existed.

// ValidatePoolNames rejects a pool name used by both a scalableFunctions and a
// virtualFunctions group. The device plugin resolves deviceType per resource rather
// than per selector, so a pool holding both device types cannot be expressed.
func ValidatePoolNames(flavor *provisioningv1.DPUFlavor) error {
	sfPools := map[string]bool{}
	for _, sf := range flavor.Spec.ScalableFunctions {
		if pool := ptr.Deref(sf.PoolName, ""); pool != "" {
			sfPools[pool] = true
		}
	}
	for _, vf := range flavor.Spec.VirtualFunctions {
		if pool := ptr.Deref(vf.PoolName, ""); pool != "" && sfPools[pool] {
			return fmt.Errorf("poolName %q is used by both a scalableFunctions and a virtualFunctions group; a device-plugin pool holds one device type", pool)
		}
	}
	return nil
}

// ValidateScalableFunctions rejects pinned sfnum runs that collide whatever ports the
// DPU turns out to have: two groups sharing a device selector whose
// [sfNumStart, sfNumStart+count) runs overlap. Groups without options.sfNumStart are
// numbered by the agent around the pinned runs, so they cannot collide. An overlap
// across different selectors ("p0" against "*" or a PCI address) depends on the
// discovered ports, which only the agent knows, and it rejects those per port.
func ValidateScalableFunctions(flavor *provisioningv1.DPUFlavor) error {
	// pinnedRun is one group's reserved sfnums as the half-open interval [start, end).
	type pinnedRun struct {
		index    int
		selector string
		start    int
		end      int
	}

	runs := make([]pinnedRun, 0, len(flavor.Spec.ScalableFunctions))
	for i, sf := range flavor.Spec.ScalableFunctions {
		count := int(ptr.Deref(sf.Count, 0))
		if count <= 0 || sf.Options == nil || sf.Options.SFNumStart == nil {
			continue
		}
		run := pinnedRun{
			index:    i,
			selector: normalizeDeviceSelector(sf.Device),
			start:    int(*sf.Options.SFNumStart),
		}
		run.end = run.start + count

		for _, earlier := range runs {
			if earlier.selector != run.selector || earlier.end <= run.start || run.end <= earlier.start {
				continue
			}
			return fmt.Errorf("scalableFunctions[%d] pins sfnums %d-%d on device %q, overlapping the %d-%d pinned by scalableFunctions[%d]",
				run.index, run.start, run.end-1, run.selector, earlier.start, earlier.end-1, earlier.index)
		}
		runs = append(runs, run)
	}
	return nil
}

// normalizeDeviceSelector is a scalableFunctions or virtualFunctions device selector in
// the form two groups can be compared in. An unset selector is "*", matching how the
// agent resolves it, and the CRD pattern keeps a PCI address in its full canonical form,
// so only case has to be folded.
func normalizeDeviceSelector(device *string) string {
	selector := strings.ToLower(strings.TrimSpace(ptr.Deref(device, "*")))
	if selector == "" {
		return "*"
	}
	return selector
}
