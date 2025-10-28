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

package networkmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/hostagent/options"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	VFConfigFile      = "/var/lib/dpf/dms/vf-config"
	NetworkRequestDir = options.HostAgentDir + "/network_req"
)

type NetworkRequest struct {
	DpuName         string `json:"dpuName"`
	DPUNamespace    string `json:"dpuNamespace"`
	UID             string `json:"uid"`
	VFName          string `json:"vfName"`
	SerialNumber    string `json:"serialNumber"`
	PCIAddress      string `json:"pciAddress"`
	NumOfVFs        int    `json:"numOfVFs"`
	ControlPlaneMTU int    `json:"controlPlaneMTU"`

	// PortConfigs holds the network interface configuration for each port
	PortConfigs []hostutil.PortConfig `json:"portConfigs,omitempty"`
}

func (nr *NetworkRequest) SetDPUObjectMeta(dpu provisioningv1.DPU) {
	nr.DPUNamespace = dpu.Namespace
	nr.DpuName = dpu.Name
	nr.UID = string(dpu.UID)
}

func ConvertVFConfigToNetworkRequest(c client.Client) error {
	vfFile, err := os.Open(VFConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			klog.Infof("vf config file %s not found, skip converting to network request", VFConfigFile)
			return nil
		}
		return fmt.Errorf("failed to open vf config file: %w", err)
	}
	defer func() {
		_ = vfFile.Close()
	}()

	nr := &NetworkRequest{}
	scanner := bufio.NewScanner(vfFile)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid line: %s", line)
		}
		value := strings.TrimSpace(parts[1])
		switch parts[0] {
		case "serial_number":
			nr.SerialNumber = value
		case "device_pci_address":
			nr.PCIAddress = value
		case "num_of_vfs":
			numOfVFs, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid num_of_vfs: %s", value)
			}
			nr.NumOfVFs = numOfVFs
		case "control_plane_mtu":
			controlPlaneMTU, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid control_plane_mtu: %s", value)
			}
			nr.ControlPlaneMTU = controlPlaneMTU
		}
	}
	nr.VFName, err = hostutil.NewPCIHelper(nr.PCIAddress).PF(0).VF(0).InterfaceName()
	if err != nil {
		if os.IsNotExist(err) {
			klog.Infof("VF device not found, remove vf config file %s", VFConfigFile)
			return os.Remove(VFConfigFile)
		}
		return fmt.Errorf("failed to find VF device: %w", err)
	}

	dpuList := &provisioningv1.DPUList{}
	err = c.List(context.TODO(), dpuList, client.MatchingLabels{
		cutil.DPUDeviceNameLabel: strings.ToLower(nr.SerialNumber),
	})
	if err != nil {
		return fmt.Errorf("failed to find DPU with PCI address %s, error: %w", nr.PCIAddress, err)
	}
	if len(dpuList.Items) > 0 {
		dpu := dpuList.Items[0]
		nr.SetDPUObjectMeta(dpu)
		err = writeNetworkRequestFile(nr)
		if err != nil {
			return err
		}
	} else {
		klog.Infof("DPU with serial number %s not found, remove vf config file %+v", nr.SerialNumber, nr)
	}
	return os.Remove(VFConfigFile)
}

func writeNetworkRequestFile(nr *NetworkRequest) error {
	err := os.MkdirAll(NetworkRequestDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create network request directory: %w", err)
	}
	filePath := filepath.Join(NetworkRequestDir, nr.UID)
	jsonData, err := json.Marshal(nr)
	if err != nil {
		return fmt.Errorf("failed to marshal network request: %w", err)
	}
	err = hostutil.AtomicWrite(filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write network request file %s: %w", filePath, err)
	}
	klog.Infof("wrote network request file %s", filePath)
	return nil
}
