//go:build linux

// Modifications copyright (C) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sriov

import (
	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
)

// API groups SR-IOV helpers used by dpf-ovs-cni.
type API interface {
	IsOvsHardwareOffloadEnabled(deviceID string) bool
	IsPCIDeviceName(deviceID string) bool
	HasUserspaceDriver(deviceID string) (bool, error)
	GetVFLinkName(deviceID string) (string, error)
	GetAuxLinkName(deviceID string) (string, error)
	SetupSriovInterface(contNetns ns.NetNS, containerID, ifName, mac string, mtu int, deviceID string, userspaceMode bool) (*current.Interface, *current.Interface, error)
	GetNetRepresentor(deviceID string) (string, error)
	ResetOffloadDev(args *skel.CmdArgs, deviceID, origIfName string) error
	ReleaseVF(args *skel.CmdArgs, origIfName string) error
	GetBridgeUplinkNameByDeviceID(deviceID string) ([]string, error)
}
