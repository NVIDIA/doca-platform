//go:build linux

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
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util/netconfig"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	NumofVFDefaultValue = 16
	condition           = string(provisioningv1.DPUCondHostNetworkReady)
)

type Interface interface {
	// Start starts the network manager
	Start() error
	// GetDevice returns the PCI device by serial number
	GetDevice(serialNumber string) (hostutil.Device, bool)
	// AddNetworkRequest adds a network request for a DPU
	AddNetworkRequest(dpu *provisioningv1.DPU) error
}

type NetworkManager struct {
	sync.RWMutex
	client.Client
	initialized bool
	// devicesBySN is a map of DPU serial number to its PCI device
	devicesBySN map[string]hostutil.Device
	// reqs is a map of DPU CR UID to its network request
	reqs map[string]NetworkRequest
	// netBackend is the network configuration backend (NetworkManager or systemd-networkd)
	netBackend netconfig.Backend
}

type networkOperation struct {
	name string
	f    func(nr NetworkRequest) error
}

func NewNetworkManager(client client.Client) *NetworkManager {
	return &NetworkManager{
		Client:      client,
		devicesBySN: make(map[string]hostutil.Device),
		reqs:        make(map[string]NetworkRequest),
	}
}

func (nm *NetworkManager) Start() error {
	nm.Lock()
	defer nm.Unlock()

	backend, err := netconfig.DetectBackend()
	if err != nil {
		return fmt.Errorf("failed to detect network configuration backend: %w", err)
	}
	nm.netBackend = backend
	klog.Infof("Using network configuration backend: %s", backend.Name())

	devices, err := hostutil.DiscoverDPUs(hostutil.SysFSRoot)
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

func (nm *NetworkManager) GetDevice(serialNumber string) (hostutil.Device, bool) {
	nm.RLock()
	defer nm.RUnlock()
	dev, ok := nm.devicesBySN[serialNumber]
	return dev, ok
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
	nm.RLock()
	reqs := make([]NetworkRequest, 0, len(nm.reqs))
	for _, nr := range nm.reqs {
		reqs = append(reqs, nr)
	}
	nm.RUnlock()

	for _, nr := range reqs {
		if err := nm.processNetworkRequest(nr); err != nil {
			klog.Errorf("failed to process network request, nr: %+v, err: %v", nr, err)
		}
	}
}

func (nm *NetworkManager) removeRequest(uid string) {
	nm.Lock()
	defer nm.Unlock()
	delete(nm.reqs, uid)
}

func (nm *NetworkManager) processNetworkRequest(nr NetworkRequest) error {
	nn := types.NamespacedName{Namespace: nr.DPUNamespace, Name: nr.DpuName}
	dpu := &provisioningv1.DPU{}
	err := nm.Get(context.TODO(), nn, dpu)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get DPU: %w", err)
	}
	if apierrors.IsNotFound(err) || dpu.UID != types.UID(nr.UID) {
		klog.Infof("DPU %s/%s(UID: %s) not found, removing VF and network request for DPU ", nr.DPUNamespace, nr.DpuName, nr.UID)
		if err = hostutil.RemoveVFFromBridge(nr.VFName); err != nil {
			return fmt.Errorf("failed to remove VF: %w", err)
		}
		err = os.Remove(filepath.Join(NetworkRequestDir, nr.UID))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove network request file: %w", err)
		}
		nm.removeRequest(nr.UID)
		klog.Infof("removed VF and network request for DPU %s/%s(UID: %s)", nr.DPUNamespace, nr.DpuName, nr.UID)
		return nil
	}
	cpy := dpu.DeepCopy()

	// Refresh ControlPlaneMTU from DPFOperatorConfig on every tick.
	mtu, err := nm.getControlPlaneMTU()
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToRefreshControlPlaneMTU").Set(&cpy.Status.Conditions)
		if updateErr := nm.Status().Update(context.TODO(), cpy); updateErr != nil {
			return fmt.Errorf("failed to update DPU status: %w, MTU refresh err: %w", updateErr, err)
		}
		return fmt.Errorf("failed to get control plane MTU: %w", err)
	}
	if nr.ControlPlaneMTU != mtu {
		klog.Infof("ControlPlaneMTU changed for DPU %s/%s: %d -> %d", nr.DPUNamespace, nr.DpuName, nr.ControlPlaneMTU, mtu)
		nr.ControlPlaneMTU = mtu
		if err := writeNetworkRequestFile(&nr); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToUpdateNetworkRequestFile").Set(&cpy.Status.Conditions)
			if updateErr := nm.Status().Update(context.TODO(), cpy); updateErr != nil {
				return fmt.Errorf("failed to update DPU status: %w, write err: %w", updateErr, err)
			}
			return fmt.Errorf("failed to update network request file with new MTU: %w", err)
		}
		nm.reqs[nr.UID] = nr
	}

	operations := []networkOperation{
		{
			name: "CreateP0VF",
			f: func(nr NetworkRequest) error {
				return hostutil.NewPCIHelper(nr.PCIAddress).PF(0).SetNumOfVFs(nr.NumOfVFs)
			},
		},
		{
			name: "CreateP1VF",
			f: func(nr NetworkRequest) error {
				isDPU, err := hostutil.NewPCIHelper(nr.PCIAddress).PF(1).IsDPU()
				if err != nil {
					if os.IsNotExist(err) {
						return nil
					}
					return fmt.Errorf("failed to check if device is DPU: %w", err)
				} else if !isDPU {
					return nil
				}
				return hostutil.NewPCIHelper(nr.PCIAddress).PF(1).SetNumOfVFs(nr.NumOfVFs)
			},
		},
		{
			name: "AddVFToBridge",
			f: func(nr NetworkRequest) error {
				vfName, err := hostutil.NewPCIHelper(nr.PCIAddress).PF(0).VF(0).InterfaceName()
				if err != nil {
					return fmt.Errorf("failed to get VF name: %w", err)
				}
				nr.VFName = vfName
				if err := writeNetworkRequestFile(&nr); err != nil {
					return fmt.Errorf("failed to update vf name in network request file: %w", err)
				}
				return hostutil.AddVFToBridge(nr.VFName, hostutil.BridgeName)
			},
		},
		{
			name: "ConfigureNetwork",
			f: func(nr NetworkRequest) error {
				return netconfig.ConfigureNetwork(nm.netBackend, nr.PCIAddress, nr.PortConfigs, nr.ControlPlaneMTU)
			},
		},
	}
	for _, op := range operations {
		klog.V(3).Infof("Setting up host network. operation: %s", op.name)
		if err := op.f(nr); err != nil {
			klog.Errorf("failed to setup host network, network request: %+v, operation: %s, err: %v", nr, op.name, err)
			reason := fmt.Sprintf("FailedTo%s", op.name)
			hostutil.NewCondition(condition).Failure(err, reason).Set(&cpy.Status.Conditions)
			if updateErr := nm.Status().Update(context.TODO(), cpy); updateErr != nil {
				return fmt.Errorf("failed to update DPU status: %w, operation err: %w", updateErr, err)
			}
			return fmt.Errorf("failed to execute operation %s: %w", op.name, err)
		}
	}
	hostutil.NewCondition(condition).Success("").Set(&cpy.Status.Conditions)
	if updateErr := nm.Status().Update(context.TODO(), cpy); updateErr != nil {
		return fmt.Errorf("failed to update DPU status: %w", updateErr)
	}
	return nil
}

// lookupDevice checks preconditions and returns the PCI device for the DPU.
// Returns found=false when a network request for this DPU already exists.
func (nm *NetworkManager) lookupDevice(dpu *provisioningv1.DPU) (dev hostutil.Device, found bool, err error) {
	nm.RLock()
	defer nm.RUnlock()
	if !nm.initialized {
		return hostutil.Device{}, false, fmt.Errorf("network manager is not initialized")
	}
	if _, ok := nm.reqs[string(dpu.UID)]; ok {
		return hostutil.Device{}, false, nil
	}
	dev, ok := nm.devicesBySN[dpu.Spec.SerialNumber]
	if !ok {
		return hostutil.Device{}, false, fmt.Errorf("PCI address of device %s not found", dpu.Spec.SerialNumber)
	}
	return dev, true, nil
}

func (nm *NetworkManager) AddNetworkRequest(dpu *provisioningv1.DPU) error {
	if dpu == nil {
		return fmt.Errorf("DPU is nil")
	}

	dev, found, err := nm.lookupDevice(dpu)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	nr := &NetworkRequest{
		SerialNumber: dpu.Spec.SerialNumber,
	}
	nr.SetDPUObjectMeta(*dpu)

	// use the PCI address collected locally, so that it's not affected by PCI address changes
	nr.PCIAddress = dev.Address

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

	// Get PF network configuration for all ports
	portConfigs, err := nm.getPFNetworkConfig(dpu, dev.Address)
	if err != nil {
		return fmt.Errorf("failed to get PF network configuration: %w", err)
	}
	nr.PortConfigs = portConfigs

	if err := writeNetworkRequestFile(nr); err != nil {
		return fmt.Errorf("failed to write network request file: %w", err)
	}

	nm.addRequest(nr)
	return nil
}

func (nm *NetworkManager) addRequest(nr *NetworkRequest) {
	nm.Lock()
	defer nm.Unlock()
	nm.reqs[nr.UID] = *nr
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

func (nm *NetworkManager) getPFNetworkConfig(dpu *provisioningv1.DPU, pciAddress string) ([]hostutil.PortConfig, error) {
	// Get desired configuration from DPUFlavor for all ports
	desiredConfigs, err := nm.getDesiredPFConfig(dpu)
	if err != nil {
		return nil, fmt.Errorf("failed to get desired PF config from DPUFlavor: %w", err)
	}

	if len(desiredConfigs) == 0 {
		return nil, nil // No configuration to apply
	}

	// Only read current config if we need to configure something
	pciHelper := hostutil.NewPCIHelper(pciAddress)
	portConfigs := make([]hostutil.PortConfig, 0, len(desiredConfigs))

	for _, desiredConfig := range desiredConfigs {
		portNumber := desiredConfig.PortNumber
		pf := pciHelper.PF(int(portNumber))
		_, err := pf.InterfaceName()
		if err != nil {
			return nil, fmt.Errorf("PF%d interface not available but configuration requested: %v", portNumber, err)
		}

		portConfig := hostutil.PortConfig{
			PortNumber: portNumber,
		}

		if desiredConfig.MTU != nil {
			portConfig.MTU = desiredConfig.MTU
		}
		if desiredConfig.DHCP != nil {
			portConfig.DHCP = desiredConfig.DHCP
		}

		portConfigs = append(portConfigs, portConfig)
	}

	return portConfigs, nil
}

func (nm *NetworkManager) getDesiredPFConfig(dpu *provisioningv1.DPU) ([]hostutil.PortConfig, error) {
	flavor := &provisioningv1.DPUFlavor{}
	if err := nm.Get(context.TODO(), types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		return nil, fmt.Errorf("failed to get DPU flavor: %w", err)
	}

	portConfigs := make([]hostutil.PortConfig, 0, len(flavor.Spec.HostNetworkInterfaceConfigs))

	// Validate and collect port configurations
	for _, config := range flavor.Spec.HostNetworkInterfaceConfigs {
		portNumber := config.PortNumber

		portConfig := hostutil.PortConfig{
			PortNumber: portNumber,
			MTU:        config.MTU,
			DHCP:       config.DHCP,
		}

		portConfigs = append(portConfigs, portConfig)
	}

	return portConfigs, nil
}
