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

package hostnetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	networkutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/hostnetwork/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	BridgeName          = "br-dpu"
	NumofVFDefaultValue = 16
)

type NetworkManager struct {
	sync.RWMutex
	client.Client
	initialized bool
	// devicesBySN is a map of DPU serial number to its PCI device
	devicesBySN map[string]networkutil.Device
	// reqs is a map of DPU CR UID to its network request
	reqs map[string]NetworkRequest
	// osType caches the detected operating system type
	osType string
}

func NewNetworkManager(client client.Client) *NetworkManager {
	return &NetworkManager{
		Client:      client,
		devicesBySN: make(map[string]networkutil.Device),
		reqs:        make(map[string]NetworkRequest),
	}
}

func (nm *NetworkManager) Start() error {
	nm.Lock()
	defer nm.Unlock()

	// Detect and cache OS type
	osType, err := networkutil.GetOSType()
	if err != nil {
		return fmt.Errorf("failed to detect OS type: %w", err)
	}
	nm.osType = osType

	devices, err := networkutil.DiscoverDPUs()
	if err != nil {
		return fmt.Errorf("failed to discovery DPUs: %w", err)
	}
	for _, dev := range devices {
		nm.devicesBySN[dev.SerialNumber] = dev
	}

	if err := nm.loadNetworkRequest(); err != nil {
		return err
	}

	nm.initialized = true

	go func() {
		_ = wait.PollUntilContextCancel(context.TODO(), 30*time.Second, true, func(ctx context.Context) (bool, error) {
			klog.V(3).Info("Processing network requests")
			nm.run()
			return false, nil
		})
	}()
	klog.Info("NetworkManager started")
	return nil
}

func (nm *NetworkManager) loadNetworkRequest() error {
	dirEntries, err := os.ReadDir(NetworkRequestDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read network request directory: %w", err)
		}
		return os.MkdirAll(NetworkRequestDir, 0755)
	}

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(NetworkRequestDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			klog.Errorf("failed to read network request file %s: %v", filePath, err)
			continue
		}
		var nr NetworkRequest
		if err := json.Unmarshal(data, &nr); err != nil {
			klog.Errorf("failed to unmarshal network request file %s: %v", filePath, err)
			continue
		}
		klog.Infof("Loaded network request: %+v", nr)
		nm.reqs[nr.UID] = nr
	}
	return nil
}

func (nm *NetworkManager) run() {
	nm.Lock()
	defer nm.Unlock()

	for _, nr := range nm.reqs {
		if err := nm.processNetworkRequest(nr); err != nil {
			klog.Errorf("failed to process network request, nr: %+v, err: %v", nr, err)
		}
	}
}

func (nm *NetworkManager) processNetworkRequest(nr NetworkRequest) error {
	nn := types.NamespacedName{Namespace: nr.DPUNamespace, Name: nr.DpuName}
	dpu := &provisioningv1.DPU{}
	if err := nm.Get(context.TODO(), nn, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			klog.Infof("DPU %s/%s(UID: %s) not found, removing VF and network request for DPU ", nr.DPUNamespace, nr.DpuName, nr.UID)
			if err = networkutil.RemoveVFFromBridge(nr.VFName); err != nil {
				return fmt.Errorf("failed to remove VF: %w", err)
			}
			err = os.Remove(filepath.Join(NetworkRequestDir, nr.UID))
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove network request file: %w", err)
			}
			delete(nm.reqs, nr.UID)
			klog.Infof("removed VF and network request for DPU %s/%s(UID: %s)", nr.DPUNamespace, nr.DpuName, nr.UID)
			return nil
		}
		return fmt.Errorf("failed to get DPU: %w", err)
	}
	operations := map[string]func(networkReq NetworkRequest) error{
		"CreateP0VF": func(nr NetworkRequest) error {
			return networkutil.CreateVF(networkutil.NewPCIHelper(nr.PCIAddress).PF(0).Path(), nr.NumOfVFs)
		},
		"CreateP1VF": func(nr NetworkRequest) error {
			isDPU, err := networkutil.NewPCIHelper(nr.PCIAddress).PF(1).IsDPU()
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("failed to check if device is DPU: %w", err)
			} else if !isDPU {
				return nil
			}
			return networkutil.CreateVF(networkutil.NewPCIHelper(nr.PCIAddress).PF(1).Path(), nr.NumOfVFs)
		},
		"AddVFToBridge": func(nr NetworkRequest) error {
			return networkutil.AddVFToBridge(nr.VFName, BridgeName)
		},
		"SetControlPlaneMTU": func(nr NetworkRequest) error {
			return networkutil.SetLinkMTU(BridgeName, nr.ControlPlaneMTU)
		},
		"ConfigurePFNetplan": func(nr NetworkRequest) error {
			// Only configure netplan on Ubuntu systems
			if nr.OSType != "ubuntu" {
				klog.Infof("Skipping netplan configuration for OS type: %s (only supported on Ubuntu)", nr.OSType)
				return nil
			}
			return networkutil.ConfigurePFNetplan(nr.PCIAddress, nr.PF0MTU, nr.PF0DHCP, nr.PF1MTU, nr.PF1DHCP)
		},
	}
	cpy := dpu.DeepCopy()
	for opName, op := range operations {
		if err := op(nr); err != nil {
			reason := fmt.Sprintf("FailedTo%s", opName)
			cutil.SetDPUCondition(&cpy.Status, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, reason, err.Error()))
			if updateErr := nm.Status().Update(context.TODO(), cpy); updateErr != nil {
				return fmt.Errorf("failed to update DPU status: %w, operation err: %w", updateErr, err)
			}
			return fmt.Errorf("failed to execute operation %s: %w", opName, err)
		}
	}
	cutil.SetDPUCondition(&cpy.Status, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), nil, "", ""))
	if updateErr := nm.Status().Update(context.TODO(), cpy); updateErr != nil {
		return fmt.Errorf("failed to update DPU status: %w", updateErr)
	}
	return nil
}

func (nm *NetworkManager) AddNetworkRequest(dpu *provisioningv1.DPU) error {
	nm.Lock()
	defer nm.Unlock()
	if !nm.initialized {
		return fmt.Errorf("network manager is not initialized")
	}
	if _, ok := nm.reqs[string(dpu.UID)]; ok {
		return nil
	}

	nr := NetworkRequest{
		DpuName:      dpu.Name,
		DPUNamespace: dpu.Namespace,
		UID:          string(dpu.UID),
		SerialNumber: dpu.Spec.SerialNumber,
	}

	// use the PCI address collected locally, so that it's not affected by PCI address changes
	dev, ok := nm.devicesBySN[nr.SerialNumber]
	if !ok {
		return fmt.Errorf("PCI address of device %s not found", nr.SerialNumber)
	}
	nr.PCIAddress = dev.Address

	vfName, err := networkutil.NewPCIHelper(dev.Address).PF(0).VF(0).InterfaceName()
	if err != nil {
		return fmt.Errorf("failed to get VF name: %w", err)
	}
	nr.VFName = vfName

	numOfVFs, err := nm.getNumOfVFs(dpu)
	if err != nil {
		return fmt.Errorf("failed to get number of VFs: %w", err)
	}
	nr.NumOfVFs = numOfVFs

	controlPlaneMTU, err := nm.getControlPlaneMTU()
	if err != nil {
		return fmt.Errorf("failed to get control plane MTU: %w", err)
	}
	nr.ControlPlaneMTU = controlPlaneMTU

	// Get PF network configuration for both P0 and P1
	pf0MTU, pf0DHCP, pf1MTU, pf1DHCP, err := nm.getPFNetworkConfig(dpu, dev.Address)
	if err != nil {
		return fmt.Errorf("failed to get PF network configuration: %w", err)
	}
	nr.PF0MTU = pf0MTU
	nr.PF0DHCP = pf0DHCP
	nr.PF1MTU = pf1MTU
	nr.PF1DHCP = pf1DHCP

	// Use cached OS type
	nr.OSType = nm.osType

	if err := writeNetworkRequestFile(&nr); err != nil {
		return fmt.Errorf("failed to write network request file: %w", err)
	}
	nm.reqs[nr.UID] = nr
	return nil
}

func (nm *NetworkManager) getNumOfVFs(dpu *provisioningv1.DPU) (int, error) {
	flavor := &provisioningv1.DPUFlavor{}
	if err := nm.Get(context.TODO(), types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		return -1, fmt.Errorf("failed to get DPU flavor: %w", err)
	}
	regex := regexp.MustCompile(`^NUM_OF_VFS=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				return strconv.Atoi(matches[1])
			}
		}
	}
	return NumofVFDefaultValue, nil
}

func (nm *NetworkManager) getControlPlaneMTU() (int, error) {
	// Get control-plane MTU to pass it to the hostnetwork.sh.
	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := nm.List(context.TODO(), &dpfOperatorConfigList); err != nil {
		return -1, fmt.Errorf("list DPFOperatorConfigs: %w", err)
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return -1, fmt.Errorf("exactly one DPFOperatorConfig necessary")
	}
	if dpfOperatorConfigList.Items[0].Spec.Networking == nil {
		return -1, fmt.Errorf("DPFOperatorConfig networking section is missing")
	}
	if dpfOperatorConfigList.Items[0].Spec.Networking.ControlPlaneMTU != nil {
		return *dpfOperatorConfigList.Items[0].Spec.Networking.ControlPlaneMTU, nil
	}
	return 1500, nil
}

func (nm *NetworkManager) getPFNetworkConfig(dpu *provisioningv1.DPU, pciAddress string) (int32, *bool, int32, *bool, error) {
	// Get desired configuration from DPUFlavor for both P0 and P1
	p0DesiredMTU, p0DesiredDHCP, p1DesiredMTU, p1DesiredDHCP, err := nm.getDesiredPFConfig(dpu)
	if err != nil {
		return 0, nil, 0, nil, fmt.Errorf("failed to get desired PF config from DPUFlavor: %w", err)
	}

	// Check if any configuration is requested
	needP0Config := p0DesiredMTU != nil || p0DesiredDHCP != nil
	needP1Config := p1DesiredMTU != nil || p1DesiredDHCP != nil

	if !needP0Config && !needP1Config {
		return 0, nil, 0, nil, nil // No configuration to apply
	}

	// Only read current config if we need to configure something
	pciHelper := networkutil.NewPCIHelper(pciAddress)
	var pf0MTU, pf1MTU int32 = 0, 0
	var pf0DHCP, pf1DHCP *bool = nil, nil

	if needP0Config {
		pf0 := pciHelper.PF(0)
		_, err := pf0.InterfaceName()
		if err != nil {
			return 0, nil, 0, nil, fmt.Errorf("PF0 interface not available but configuration requested: %v", err)
		}
		if p0DesiredMTU != nil {
			pf0MTU = *p0DesiredMTU
		}
		if p0DesiredDHCP != nil {
			pf0DHCP = p0DesiredDHCP
		}
	}

	if needP1Config {
		pf1 := pciHelper.PF(1)
		_, err := pf1.InterfaceName()
		if err != nil {
			return 0, nil, 0, nil, fmt.Errorf("PF1 interface not available but configuration requested: %v", err)
		}
		if p1DesiredMTU != nil {
			pf1MTU = *p1DesiredMTU
		}
		if p1DesiredDHCP != nil {
			pf1DHCP = p1DesiredDHCP
		}
	}

	return pf0MTU, pf0DHCP, pf1MTU, pf1DHCP, nil
}

func (nm *NetworkManager) getDesiredPFConfig(dpu *provisioningv1.DPU) (*int32, *bool, *int32, *bool, error) {
	flavor := &provisioningv1.DPUFlavor{}
	if err := nm.Get(context.TODO(), types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get DPU flavor: %w", err)
	}

	var p0MTU, p1MTU *int32
	var p0DHCP, p1DHCP *bool

	// Get P0 configuration
	if flavor.Spec.P0NetworkInterfaceConfig != nil {
		p0MTU = flavor.Spec.P0NetworkInterfaceConfig.MTU
		p0DHCP = flavor.Spec.P0NetworkInterfaceConfig.DHCP
	}

	// Get P1 configuration
	if flavor.Spec.P1NetworkInterfaceConfig != nil {
		p1MTU = flavor.Spec.P1NetworkInterfaceConfig.MTU
		p1DHCP = flavor.Spec.P1NetworkInterfaceConfig.DHCP
	}

	return p0MTU, p0DHCP, p1MTU, p1DHCP, nil
}
