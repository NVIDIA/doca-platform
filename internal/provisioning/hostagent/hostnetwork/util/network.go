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

	"github.com/vishvananda/netlink"
)

func CreateVF(pciAddress string, numOfVFs int) error {
	err := NewPCIHelper(pciAddress).SetNumOfVFs(numOfVFs)
	if err != nil {
		return fmt.Errorf("failed to set number of VFs: %w", err)
	}
	return nil
}

func AddVFToBridge(vfName, bridgeName string) error {
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge link: %w", err)
	}
	vf, err := netlink.LinkByName(vfName)
	if err != nil {
		return fmt.Errorf("failed to get VF link: %w", err)
	}
	if err := netlink.LinkSetMaster(vf, bridge); err != nil {
		return fmt.Errorf("failed to set VF link master: %w", err)
	}
	return nil
}

func SetLinkMTU(linkName string, mtu int) error {
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return fmt.Errorf("failed to get link %s: %w", linkName, err)
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return fmt.Errorf("failed to set link %s MTU: %w", linkName, err)
	}
	return nil
}

func RemoveVFFromBridge(vfName string) error {
	vf, err := netlink.LinkByName(vfName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to get VF link: %w", err)
	}
	return netlink.LinkSetNoMaster(vf)
}
