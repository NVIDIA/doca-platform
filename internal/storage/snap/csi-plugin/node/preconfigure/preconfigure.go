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

func New(config config.Node, runtimeConfig *config.NodeRuntime, pci utilsPci.Utils) Preconfigure {
	return &preconfigure{
		config:        config,
		runtimeConfig: runtimeConfig,
		pci:           pci,
		started:       make(chan struct{}),
	}
}

type preconfigure struct {
	config        config.Node
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

func (p *preconfigure) run(ctx context.Context) error {
	defer close(p.started)
	klog.V(2).Info("ensure nvme module is loaded")
	if err := p.pci.InsertKernelModule(ctx, utilsCommon.NVMEDriver); err != nil {
		klog.ErrorS(err, "failed to load NVME module")
		return err
	}
	klog.V(2).InfoS("discover SNAP controller", "vendor", utilsCommon.MlxVendor,
		"deviceID", p.config.SnapControllerDeviceID)

	storagePF, err := p.pci.GetPFs(utilsCommon.MlxVendor, []string{p.config.SnapControllerDeviceID})
	if err != nil {
		klog.ErrorS(err, "failed to read PF list")
		return err
	}
	if len(storagePF) == 0 {
		klog.ErrorS(nil, "storage PF not found", "vendor", utilsCommon.MlxVendor,
			"deviceID", p.config.SnapControllerDeviceID)
		return fmt.Errorf("storage PF not found")
	}
	if len(storagePF) > 1 {
		klog.Info("more than one SNAP controller found on node, use first one")
	}
	pfAddress := storagePF[0].Address

	klog.InfoS("SNAP controller selected", "device", pfAddress)

	klog.V(2).Info("load driver for the SNAP controller")
	if err := p.pci.LoadDriver(pfAddress, utilsCommon.NVMEDriver); err != nil {
		klog.ErrorS(err, "failed to load driver for the SNAP controller", "device", pfAddress)
		return err
	}
	maxVfs, err := p.pci.GetSRIOVTotalVFs(pfAddress)
	if err != nil {
		klog.ErrorS(err, "failed to read sriov_totalvfs for device", "device", pfAddress)
		return err
	}
	if maxVfs == 0 {
		klog.ErrorS(nil, "sriov_totalvfs is 0 for the device", "device", pfAddress)
		return fmt.Errorf("sriov_totalvfs is 0 for the device")
	}
	curVfs, err := p.pci.GetSRIOVNumVFs(pfAddress)
	if err != nil {
		klog.ErrorS(err, "failed to read sriov_numvfs for device", "device", pfAddress)
		return err
	}
	if curVfs != 0 {
		if curVfs == maxVfs {
			klog.InfoS("device already has required amount of VFs", "device", pfAddress, "vfCount", maxVfs)
		} else {
			// print scary message and proceed
			klog.ErrorS(nil, "current number of the storage VFs doesn't match expected with value, "+
				"some volumes may fail to attach. reboot the node to fix the issue.", "device", pfAddress, "current", curVfs, "expected", maxVfs)
		}
		p.runtimeConfig.SetMaxVolumesPerNode(int64(curVfs))
		return nil
	}
	klog.V(2).InfoS("create VFs on the device", "device", pfAddress)
	if err := p.pci.DisableSriovVfsDriverAutoprobe(pfAddress); err != nil {
		// print scary message and proceed
		klog.ErrorS(err, "failed to disable driver autoprobe for VFs this may slowdown configuration a lot",
			"device", pfAddress)
	}

	if err := p.pci.SetSriovNumVfs(pfAddress, maxVfs); err != nil {
		klog.ErrorS(err, "failed to create VFs", "device", pfAddress)
		return err
	}
	p.runtimeConfig.SetMaxVolumesPerNode(int64(maxVfs))
	klog.InfoS("device configured", "device", pfAddress, "vfCount", maxVfs)
	return nil
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
