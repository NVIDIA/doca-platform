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

package sriovconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

// ReconcileVF creates spec.virtualFunctions. Must run after ReconcileSF.
type ReconcileVF struct {
	rootFS  string
	runBash runBashFunc
}

func (v *ReconcileVF) Name() string {
	return "Reconcile VF"
}

func (v *ReconcileVF) ConditionType() string {
	return "VFReconciled"
}

// ShouldSkip honors --skip-vf-config only. An empty virtualFunctions list still runs
// so leftover VFs on selected devices can be converged.
func (v *ReconcileVF) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipVFConfig
}

func (v *ReconcileVF) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (v *ReconcileVF) Execute(execCtx context.Context, optCtx *operations.Context) error {
	v.applyDefaults()

	plan, err := v.plan(optCtx)
	if err != nil {
		return err
	}
	if err := v.reconcile(plan); err != nil {
		return err
	}
	v.verify(plan)

	return sharedState(optCtx).writeDevicePluginConfig(v.rootFS)
}

func (v *ReconcileVF) applyDefaults() {
	if v.runBash == nil {
		v.runBash = bash.Run
	}
	if v.rootFS == "" {
		v.rootFS = defaultRootFS
	}
}

// plan resolves VF groups. No side effects.
func (v *ReconcileVF) plan(optCtx *operations.Context) (*vfPlan, error) {
	if err := cutil.ValidatePoolNames(&optCtx.DPUFlavor); err != nil {
		return nil, err
	}
	ports, err := optCtx.NSPorts()
	if err != nil {
		return nil, err
	}
	plan, err := resolveVFPlan(&optCtx.DPUFlavor, ports)
	if err != nil {
		return nil, err
	}
	sharedState(optCtx).vf = plan
	return plan, nil
}

// reconcile writes sriov_numvfs per ECPF, then applies each run's MAC. MAC is set
// at VF create, so a changed flavor relies on DPU reprovision (fresh OS, no VFs).
// An agent restart within a boot is a no-op if the count matches.
//
// TODO: if flavor edits stop triggering reprovision, detect in-place option changes
// (count can stay the same). Previously a hash of spec.virtualFunctions under
// /var/lib/dpf/dpuagent forced sriov_numvfs to 0 on mismatch.
func (v *ReconcileVF) reconcile(plan *vfPlan) error {
	for _, port := range plan.ports {
		total, declared := plan.totals[port.PCIAddress]
		if !declared {
			continue
		}
		if err := v.setVFCount(port.Netdev, total); err != nil {
			return err
		}
		for _, vf := range plan.vfsOnDevice(port.PCIAddress) {
			if err := v.applyVFOptions(vf); err != nil {
				return err
			}
		}
	}
	return nil
}

// setVFCount writes sriov_numvfs. Firmware NUM_OF_VFS must already allow `total`.
func (v *ReconcileVF) setVFCount(netdev string, total int) error {
	numVFsPath := filepath.Join(v.rootFS, "sys/class/net", netdev, "device/sriov_numvfs")
	current, err := readIntFile(numVFsPath)
	if err != nil {
		return fmt.Errorf("failed to read VF count of %s: %w", netdev, err)
	}
	if current == total {
		return nil
	}
	// Kernel rejects non-zero → different non-zero; drop VFs first.
	if current != 0 {
		if err := os.WriteFile(numVFsPath, []byte("0"), 0644); err != nil {
			return fmt.Errorf("failed to remove the %d existing VFs of %s: %w", current, netdev, err)
		}
	}
	if total > 0 {
		if err := os.WriteFile(numVFsPath, []byte(strconv.Itoa(total)), 0644); err != nil {
			return fmt.Errorf("failed to create %d VFs on %s: %w", total, netdev, err)
		}
	}
	klog.Infof("Set VF count of %s to %d", netdev, total)
	return nil
}

// applyVFOptions sets the MAC on this run's VF indices. There is no trust option:
// setting a DPU VF trusted is not supported.
func (v *ReconcileVF) applyVFOptions(vf plannedVF) error {
	if vf.mac != "" {
		cmd := fmt.Sprintf("ip link set %s vf %d mac %s", vf.netdev, vf.index, vf.mac)
		if stdout, stderr, err := v.runBash(cmd); err != nil {
			return fmt.Errorf("failed to set MAC of VF %d on %s: stdout=%s, stderr=%s, err=%w", vf.index, vf.netdev, stdout.String(), stderr.String(), err)
		}
	}
	return nil
}

// verify logs sriov_numvfs mismatches. It does not fail the operation or configure anything.
//
// TODO: fail on per-VF MAC mismatch (`ip -j -d link show`).
func (v *ReconcileVF) verify(plan *vfPlan) {
	for _, port := range plan.ports {
		total, declared := plan.totals[port.PCIAddress]
		if !declared {
			continue
		}
		numVFsPath := filepath.Join(v.rootFS, "sys/class/net", port.Netdev, "device/sriov_numvfs")
		current, err := readIntFile(numVFsPath)
		if err != nil {
			klog.Warningf("VF verification: failed to read VF count of %s: %v", port.Netdev, err)
			continue
		}
		if current != total {
			klog.Warningf("VF verification: %s has %d VFs, the flavor declares %d", port.Netdev, current, total)
		}
	}
}
