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

package nvconfig

import (
	"fmt"
	"sort"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	nvconfigutil "github.com/nvidia/doca-platform/internal/provisioning/utils/nvconfig"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"
)

// EnsureResolved returns the cached ResolvedNVConfig, or resolves once from flavor + NSPorts.
// Errors are not cached so agent retries re-attempt discovery/mapping.
func EnsureResolved(optCtx *operations.Context) (*operations.ResolvedNVConfig, error) {
	if r := optCtx.GetResolvedNVConfig(); r != nil {
		return r, nil
	}
	r, err := resolve(optCtx)
	if err != nil {
		return nil, err
	}
	optCtx.SetResolvedNVConfig(r)
	return r, nil
}

func resolve(optCtx *operations.Context) (*operations.ResolvedNVConfig, error) {
	ports, err := optCtx.NSPorts()
	if err != nil {
		return nil, err
	}
	pciToNetdev := portsToPCINetdev(ports)
	pciToParams := pciToNVConfig(optCtx.DPUFlavor.Spec.NVConfig, pciToNetdev)

	hostOSInitPCIs, required, err := selectHostOSInitPCIs(optCtx.DPUFlavor.Spec.NVConfig, ports)
	if err != nil {
		return nil, err
	}
	return &operations.ResolvedNVConfig{
		PCIToParams:        pciToParams,
		HostOSInitPCIs:     hostOSInitPCIs,
		HostOSInitRequired: required,
	}, nil
}

func portsToPCINetdev(ports []pciutil.NICPort) map[string]string {
	pciToNetdev := make(map[string]string, len(ports))
	for _, port := range ports {
		pciToNetdev[pciutil.NormalizeAddress(port.PCIAddress)] = port.Netdev
	}
	return pciToNetdev
}

func selectHostOSInitPCIs(nvconfigs []provisioningv1.NVConfig, ports []pciutil.NICPort) ([]string, bool, error) {
	pcis := map[string]struct{}{}
	for _, nc := range nvconfigs {
		if !nvconfigutil.ParamsRequestHostOSInitHold(nc.Parameters) {
			continue
		}
		device := normalizeNVDevice(nc.Device)
		if device == "*" {
			if len(ports) == 0 {
				return nil, false, fmt.Errorf("no physical ports discovered for nvconfig device %q", device)
			}
			for _, port := range ports {
				pcis[pciutil.NormalizeAddress(port.PCIAddress)] = struct{}{}
			}
			continue
		}
		pci, err := mapDeviceToPCI(device, ports)
		if err != nil {
			return nil, false, err
		}
		pcis[pci] = struct{}{}
	}
	if len(pcis) == 0 {
		return nil, false, nil
	}
	result := make([]string, 0, len(pcis))
	for pci := range pcis {
		result = append(result, pci)
	}
	sort.Strings(result)
	return result, true, nil
}

func normalizeNVDevice(device *string) string {
	if device == nil {
		return "*"
	}
	return strings.ToLower(strings.TrimSpace(*device))
}

func mapDeviceToPCI(device string, ports []pciutil.NICPort) (string, error) {
	if len(ports) == 0 {
		return "", fmt.Errorf("no physical ports discovered for nvconfig device %q", device)
	}
	if device == "*" {
		return pciutil.NormalizeAddress(ports[0].PCIAddress), nil
	}
	for _, port := range ports {
		if strings.EqualFold(port.Netdev, device) {
			return pciutil.NormalizeAddress(port.PCIAddress), nil
		}
	}
	return "", fmt.Errorf("no PCI device found for nvconfig device %q", device)
}
