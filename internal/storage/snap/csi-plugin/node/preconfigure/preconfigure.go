/*
Copyright 2024 NVIDIA

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

package preconfigure

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	utilsCommon "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/common"
	utilsPci "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"

	"k8s.io/klog/v2"
)

type Preconfigure interface {
	runner.Runnable
}

func New(commonConfig config.Common, nodeConfig config.Node, runtimeConfig *config.NodeRuntime, pci utilsPci.Utils) Preconfigure {
	return &preconfigure{
		commonConfig:  commonConfig,
		nodeConfig:    nodeConfig,
		runtimeConfig: runtimeConfig,
		pci:           pci,
		started:       make(chan struct{}),
	}
}

type preconfigure struct {
	commonConfig  config.Common
	nodeConfig    config.Node
	runtimeConfig *config.NodeRuntime
	pci           utilsPci.Utils
	started       chan struct{}
}

// Run blocks until context is canceled or an error occurred
func (p *preconfigure) Run(ctx context.Context) error {
	if err := p.run(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// runs mode specific preconfiguration
func (p *preconfigure) run(ctx context.Context) error {
	defer close(p.started)
	switch p.commonConfig.EmulationMode {
	case config.EmulationModeNVMe:
		klog.V(2).Info("run preconfiguration for NVMe emulation mode")
		return p.nvmeRun(ctx)
	case config.EmulationModeVirtiofs:
		klog.V(2).Info("run preconfiguration for Virtiofs emulation mode")
		return p.virtiofsRun(ctx)
	default:
		return fmt.Errorf("unknown emulation mode: %s", p.commonConfig.EmulationMode)
	}
}

// the run function currently supports VF on top of static PF and hotplugged PF scenarios
// The function inserts the NVME kernel module.
// Then the function iterates over all storage PFs and binds them to the NVME driver.
// If the storage function supports SR-IOV, the function creates max possible amount of VFs on top of it.
func (p *preconfigure) nvmeRun(ctx context.Context) error {
	if p.nodeConfig.NVMeLoadDriver {
		klog.V(2).Info("ensure nvme module is loaded")
		if err := p.pci.InsertKernelModule(ctx, utilsCommon.NVMEDriver); err != nil {
			klog.ErrorS(err, "failed to load NVME module")
			return err
		}
	} else {
		klog.V(2).Info("skip NVMe driver loading, as requested by the configuration")
	}

	if p.nodeConfig.NVMeCreateVFs {
		klog.V(2).InfoS("discover SNAP controller", "vendor", utilsCommon.MlxVendor,
			"deviceID", p.nodeConfig.SnapControllerDeviceID)

		// this function returns only NVME PFs, but without distinction between static and hotplugged PFs
		storagePF, err := p.pci.GetPFs(utilsCommon.MlxVendor, []string{p.nodeConfig.SnapControllerDeviceID})
		if err != nil {
			klog.ErrorS(err, "failed to read PF list")
			return err
		}
		totalVfs := 0
		for _, pf := range storagePF {
			sriovEnabled, err := p.pci.IsSRIOVEnabled(pf.Address)
			if err != nil {
				klog.ErrorS(err, "failed to read sriov_totalvfs for device", "device", pf.Address)
				return err
			}
			if !sriovEnabled {
				klog.InfoS("SR-IOV is not enabled for the device, skip creating VFs", "device", pf.Address)
				continue
			}
			if err := p.pci.LoadDriver(pf.Address, utilsCommon.NVMEDriver); err != nil {
				klog.ErrorS(err, "failed to load driver for the SNAP controller", "device", pf.Address)
				return err
			}
			createdVfs, err := p.createVFs(pf.Address)
			if err != nil {
				return err
			}
			totalVfs += createdVfs
		}
		if totalVfs > 0 {
			p.runtimeConfig.SetMaxVolumesPerNode(int64(totalVfs))
		}
	} else {
		klog.V(2).Info("skip NVMe VFs creation, as requested by the configuration")
	}
	klog.InfoS("host preconfiguration completed", "discoveredMaxVolumesPerNode", p.runtimeConfig.GetMaxVolumesPerNode())
	return nil
}

// virtiofs specific preconfiguration, ensures that virtiofs driver is loaded
func (p *preconfigure) virtiofsRun(ctx context.Context) error {
	if p.nodeConfig.VirtiofsLoadDriver {
		klog.V(2).InfoS("ensure virtio-pci driver is loaded", "driver", utilsCommon.VirtioPCIDriver)
		if err := p.pci.InsertKernelModule(ctx, utilsCommon.VirtioPCIDriver); err != nil {
			klog.ErrorS(err, "failed to load virtio-pci driver")
			return err
		}
	} else {
		klog.V(2).Info("skip virtio-pci driver loading, as requested by the configuration")
	}
	return nil
}

// createVFs creates VFs on the device if needed
// returns the number of VFs created and error if any
func (p *preconfigure) createVFs(pfAddress string) (int, error) {
	maxVfs, err := p.pci.GetSRIOVTotalVFs(pfAddress)
	if err != nil {
		klog.ErrorS(err, "failed to read sriov_totalvfs for device", "device", pfAddress)
		return 0, err
	}
	curVfs, err := p.pci.GetSRIOVNumVFs(pfAddress)
	if err != nil {
		klog.ErrorS(err, "failed to read sriov_numvfs for device", "device", pfAddress)
		return 0, err
	}
	if curVfs != 0 {
		if curVfs == maxVfs {
			klog.InfoS("device already has required amount of VFs", "device", pfAddress, "vfCount", maxVfs)
		} else {
			// print scary message and proceed
			klog.ErrorS(nil, "current number of the storage VFs doesn't match expected with value, "+
				"some volumes may fail to attach. reboot the node to fix the issue.", "device", pfAddress, "current", curVfs, "expected", maxVfs)
		}
		return curVfs, nil
	}
	klog.V(2).InfoS("create VFs on the device", "device", pfAddress)
	if err := p.pci.DisableSriovVfsDriverAutoprobe(pfAddress); err != nil {
		// print scary message and proceed
		klog.ErrorS(err, "failed to disable driver autoprobe for VFs this may slowdown configuration a lot",
			"device", pfAddress)
	}
	if err := p.pci.SetSriovNumVfs(pfAddress, maxVfs); err != nil {
		klog.ErrorS(err, "failed to create VFs", "device", pfAddress)
		return 0, err
	}
	klog.V(2).InfoS("VF created", "device", pfAddress, "vfCount", maxVfs)
	return maxVfs, nil
}

// Wait blocks until context is canceled or service is ready
func (p *preconfigure) Wait(ctx context.Context) error {
	select {
	case <-p.started:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
