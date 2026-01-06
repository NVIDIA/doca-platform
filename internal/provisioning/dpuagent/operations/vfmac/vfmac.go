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

// Manage virtual function (VF) MAC addresses.
// It maintains persistent MAC address mappings in a TOML config file (/etc/mellanox/dpf-vf-mac-mapping.toml)
// and handles MAC address assignment for VFs on physical interfaces p0 and p1.

// The module provides functions to:
// - Query maximum VF count from /sys/class/net/<uplink>/smart_nic
// - Read and write VF MAC addresses from/to sysfs (/sys/class/net/<uplink>/smart_nic/<vf>/mac)
// - Load and save MAC address mappings from/to config (/etc/mellanox/dpf-vf-mac-mapping.toml)
// - Process VFs to either generate random MAC addresses or assign existing MAC addresses
//   if already present from the config file.

package vfmac

import (
	"fmt"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"
	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"
	"github.com/nvidia/doca-platform/pkg/vfmac"
)

type VFMACOperation struct {
}

func (v *VFMACOperation) Name() string {
	return "vfmac"
}

func (v *VFMACOperation) Type() operations.OperationType {
	return operations.RunOnEachReboot
}

func (v *VFMACOperation) Execute(ctx operations.Context) error {
	// Create a new VFMAC instance with default configuration
	vfmacInstance, err := vfmac.NewVFMAC(filesystem.DefaultFileSystem, networkhelper.New(), "", "")
	if err != nil {
		return fmt.Errorf("error creating VFMAC instance: %v", err)
	}

	// Process VFs using the new instance
	if err := vfmacInstance.ProcessVFs(); err != nil {
		return fmt.Errorf("error processing VFs: %v", err)
	}
	return nil
}
