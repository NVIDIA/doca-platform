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

package reboot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	failReason = "FailedToReboot"
)

// run fetches DPUs on the node and reboots them. Failed reboots are persisted via updateDPURebootStatus when Client is set.
func (r *Handler) run() []*provisioningv1.DPU {
	timeoutCtx, cancel := context.WithTimeout(context.TODO(), 10*time.Minute)
	defer cancel()

	getDPUNodeFunc := r.getDPUNodeFunc
	if getDPUNodeFunc == nil {
		getDPUNodeFunc = r.getDPUNodeWithClient
	}
	dpuNode, err := getDPUNodeFunc(timeoutCtx)
	if err != nil {
		klog.Errorf("failed to get DPUNode, err: %v", err)
		return nil
	}
	if dpuNode.Spec.NodeRebootMethod != nil && dpuNode.Spec.NodeRebootMethod.GNOI == nil && dpuNode.Spec.NodeRebootMethod.HostAgent == nil { //nolint:staticcheck
		klog.Info("DPUNode is not set to reboot the host via host agent, skipping")
		return nil
	}

	listDPUFunc := r.listDPUFunc
	if r.listDPUFunc == nil {
		listDPUFunc = r.listDPUWithClient
	}
	dpus, err := listDPUFunc(timeoutCtx)
	if err != nil {
		klog.Errorf("failed to list DPUs, err: %v", err)
		return nil
	}
	failedDPUs, err := r.reboot(timeoutCtx, dpuNode, dpus)
	if err != nil {
		klog.Errorf("failed to reboot: %v", err)
		for i := range failedDPUs {
			dpu := &failedDPUs[i]
			klog.Errorf("failed to reboot DPU %s", dpu.Name)
			if r.Client != nil {
				if err2 := r.updateDPURebootStatus(timeoutCtx, dpu, provisioningv1.RebootStatusFailed, failReason, err.Error()); err2 != nil {
					klog.Errorf("failed to update reboot failure status for DPU %s: %v", dpu.Name, err2)
				}
				continue
			}
			klog.Errorf("no reboot failure status update: Client is nil (DPU %s)", dpu.Name)
		}
		return nil
	}
	return nil
}

// reboot reboots the DPUs that need to be rebooted. It returns a list of DPUs that should be rebooted and an error if any.
// Note: Currently, all of the DPUs that should be rebooted either succeed together or fail together.
func (r *Handler) reboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []provisioningv1.DPU) (rebootNow []provisioningv1.DPU, err error) {
	blockers := []client.ObjectKey{}
	for _, dpu := range dpus {
		needReboot, err := r.needRebooting(dpu)
		if err != nil {
			return nil, fmt.Errorf("failed to check if DPU needs to be rebooted, err: %v", err)
		}
		if needReboot {
			rebootNow = append(rebootNow, dpu)
		} else if shouldBlockRebooting(dpu) {
			blockers = append(blockers, client.ObjectKeyFromObject(&dpu))
		}
	}
	if len(rebootNow) == 0 {
		klog.Info("No DPUs to reboot")
		return nil, nil
	}
	if len(blockers) > 0 {
		return rebootNow, fmt.Errorf("waiting for the following DPUs to reach Rebooting phase: %v", blockers)
	}

	// run power cycle if any DPU requires it
	runPowerCycle := false
	for _, dpu := range rebootNow {
		if reboot.PowerCycleRequired(dpu.Annotations) {
			runPowerCycle = true
			break
		}
		rs := dpu.Status.RebootStatus
		if rs != nil && rs.Method != nil &&
			*rs.Method == provisioningv1.RebootMethodPowerCycle &&
			rs.Phase == provisioningv1.RebootStatusPending {
			runPowerCycle = true
			break
		}
	}
	if runPowerCycle {
		if err := r.RunPowerCycle(dpuNode, rebootNow); err != nil {
			return rebootNow, err
		}
		return nil, nil
	}
	if err := r.RunSLR(ctx, rebootNow); err != nil {
		return rebootNow, err
	}
	return nil, nil
}

func (r *Handler) RunPowerCycle(dpuNode *provisioningv1.DPUNode, dpus []provisioningv1.DPU) error {
	powerCycleCommand, err := reboot.PowerCycleCommand(dpuNode)
	if err != nil {
		return fmt.Errorf("failed to get power cycle command: %w", err)
	}
	if err := r.persistDPUBootID(dpus); err != nil {
		return fmt.Errorf("failed to persist DPU boot ID. err: %w", err)
	}
	klog.Infof("run powercycle with command %q", powerCycleCommand)
	_, stderr, err := r.runPowerCycleCmd(powerCycleCommand)
	if err != nil {
		return fmt.Errorf("failed to powercycle. cmd: %s, stderr: %s, err: %w", powerCycleCommand, stderr.String(), err)
	}
	return nil
}

func (r *Handler) RunSLR(ctx context.Context, toBeRebooted []provisioningv1.DPU) error {
	devs := make([]hostutil.Device, len(toBeRebooted))
	for i, dpu := range toBeRebooted {
		dev, ok := r.getDeviceBySerialNumberFunc(dpu.Spec.SerialNumber)
		if !ok {
			return fmt.Errorf("failed to get device by serial number: %s", dpu.Spec.SerialNumber)
		}
		devs[i] = dev
	}
	// shut down ARMs in parallel
	results := sync.Map{}
	group := sync.WaitGroup{}
	for i, dpu := range toBeRebooted {
		group.Add(1)
		go func(index int, rebootingDPU provisioningv1.DPU, dev hostutil.Device) {
			defer group.Done()
			results.Store(index, r.shutDownARM(ctx, rebootingDPU, dev))
		}(i, dpu, devs[i])

	}
	group.Wait()

	failures := []error{}
	results.Range(func(key any, value any) bool {
		i := key.(int)
		if value != nil {
			failures = append(failures, fmt.Errorf("dpu: %s, err: %w", toBeRebooted[i].Name, value.(error)))
		}
		return true
	})
	if len(failures) > 0 {
		return fmt.Errorf("failed to shutdown ARM(s). failures: %w", errors.Join(failures...))
	}

	if err := r.persistDPUBootID(toBeRebooted); err != nil {
		return fmt.Errorf("failed to persist DPU boot ID. err: %w", err)
	}
	// run reboot host against ANY DPU
	klog.Info("Attempting to reboot host")
	pciAddr := filepath.Base(hostutil.NewPCIHelper(devs[0].Address).PF(0).Path())
	cmd := fmt.Sprintf("mlxfwreset -d %s -l 4 r -y", pciAddr)
	stdout, stderr, err := r.runRebootHost(cmd)
	if err != nil {
		// mlxfwreset outputs error messages to stdout
		return fmt.Errorf("failed to reboot host. This error can be ignored as long as the host is automatically rebooted later. cmd: %s, stderr: %s, stdout: %s", cmd, stderr.String(), stdout.String())
	}
	return nil
}

func (r *Handler) shutDownARM(ctx context.Context, dpu provisioningv1.DPU, dev hostutil.Device) error {
	// Use test injection if available (for unit tests)
	rshimFunc := r.getRshimNameByPCIFunc
	if rshimFunc == nil {
		// Production: Use QueryRshimByPCI for efficient rshim discovery
		rshimFunc = func(pciAddr string) (string, error) {
			info, err := hostutil.QueryRshimByPCI(pciAddr)
			if err != nil {
				return "", err
			}
			return info.RshimName, nil
		}
	}

	rshim, err := rshimFunc(dev.Address)
	if err != nil {
		return fmt.Errorf("failed to find DPU rshim for PCI address: %s. err: %v", dev.Address, err)
	}
	klog.Infof("Found DPU rshim %s for PCI address %s, dpu: %s", rshim, dev.Address, dpu.Name)

	isDPUOffFunc := r.isDPUOffFunc
	if isDPUOffFunc == nil {
		isDPUOffFunc = r.isDPUOff
	}
	pciAddr := filepath.Base(hostutil.NewPCIHelper(dev.Address).PF(0).Path())
	off, lastLine, err := isDPUOffFunc(rshim)
	if err != nil {
		return fmt.Errorf("failed to check if DPU is off. dpu: %s, err: %v", dpu.Name, err)
	}
	if off {
		return nil
	}

	klog.Infof("DPU is not off, attempt to stop ARM, last line: %s, pci address: %s, dpu: %s", lastLine, pciAddr, dpu.Name)
	// the mlxfwreset command returns error if the ARM is already being stopped, so ideally we should not run it again.
	// However, it's not a trivial task to determine if the ARM is already being stopped.
	// To make things simpler, we simply run the cmd and return the error until we get "System Off" from rshim/misc.
	// The downside is that an error message will appear in the DPU.status.condition.
	cmd := fmt.Sprintf("mlxfwreset -d %s -l 1 -t 4 reset -y --sync 0", pciAddr)
	stdout, stderr, err := r.runShutdownARMCmd(cmd)
	if err == nil {
		return nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// the exit code of mlxfwreset can be confusing. Sometimes, it is 1 even if the ARM is successfully stopped.
	// So we will continue to reboot the host if we see "System Off" from rshim/misc.
	rshimErr := wait.PollUntilContextCancel(timeoutCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		off, lastLine, err := isDPUOffFunc(rshim)
		if err != nil {
			return false, fmt.Errorf("failed to check if DPU is off. dpu: %s, err: %v", dpu.Name, err)
		} else if !off {
			klog.Errorf("DPU is not off, last line: %s, dpu: %s, pci address: %s, rshim: %s", lastLine, dpu.Name, pciAddr, rshim)
			return false, nil
		}
		return true, nil
	})
	if rshimErr != nil {
		// NOTE: mlxfwreset outputs error messages to stdout rather than stderr.
		return fmt.Errorf("failed ARM shutdown for dpu: %s. mlxfwreset stdout: %s, mlxfwreset stderr: %s, rshim err: %v",
			dpu.Name, stdout.String(), stderr.String(), rshimErr)
	}
	return nil
}

func (r *Handler) getDPUNodeWithClient(ctx context.Context) (*provisioningv1.DPUNode, error) {
	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: hostutil.DPFNamespace, Name: r.getNodeNameFunc()}, dpuNode); err != nil {
		return nil, fmt.Errorf("failed to get DPUNode, err: %v", err)
	}
	return dpuNode, nil
}

func (r *Handler) listDPUWithClient(ctx context.Context) ([]provisioningv1.DPU, error) {
	dpus := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpus, client.MatchingLabels{
		provisioningv1.DPUNodeNameLabel: r.getNodeNameFunc(),
	}); err != nil {
		return nil, fmt.Errorf("failed to list DPUs, err: %v", err)
	}
	return dpus.Items, nil
}

func (r *Handler) persistDPUBootID(dpus []provisioningv1.DPU) error {
	for _, dpu := range dpus {
		if err := r.bootIDStore.PersistBootID(&dpu); err != nil {
			return fmt.Errorf("failed to write DPU boot ID file. dpu: %s, err: %w", dpu.Name, err)
		}
	}
	return nil
}

func (r *Handler) runPowerCycleCmd(cmd string) (stdout, stderr bytes.Buffer, err error) {
	f := r.runPowerCycleCmdFunc
	if f == nil {
		f = hostutil.RunBash
	}
	return f(cmd)
}

func (r *Handler) runShutdownARMCmd(cmd string) (stdout, stderr bytes.Buffer, err error) {
	f := r.runShutdownARMFunc
	if f == nil {
		f = hostutil.RunBash
	}
	return f(cmd)
}

func (r *Handler) runRebootHost(cmd string) (stdout, stderr bytes.Buffer, err error) {
	f := r.runRebootHostFunc
	if f == nil {
		f = hostutil.RunBash
	}
	return f(cmd)
}

func (r *Handler) isDPUOff(rshim string) (bool, string, error) {
	cmd := fmt.Sprintf("echo 'DISPLAY_LEVEL 2' > /dev/%s/misc && cat /dev/%s/misc", rshim, rshim)
	stdout, stderr, err := hostutil.RunBash(cmd)
	if err != nil {
		return false, "", fmt.Errorf("failed to run command: %v, stderr: %v, error: %v", cmd, stderr, err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) == 0 {
		return false, "", nil
	}
	lastLine := lines[len(lines)-1]
	return strings.Contains(lastLine, "System Off"), lastLine, nil
}

func (r *Handler) needRebooting(dpu provisioningv1.DPU) (bool, error) {
	finished, err := r.bootIDStore.IsRebootFinished(&dpu)
	if err != nil {
		return false, fmt.Errorf("failed to check if reboot is finished. dpu: %s, err: %v", dpu.Name, err)
	}
	return dpu.Status.Phase == provisioningv1.DPURebooting && dpu.DeletionTimestamp.IsZero() && !finished, nil
}

func shouldBlockRebooting(dpu provisioningv1.DPU) bool {
	switch dpu.Status.Phase {
	case provisioningv1.DPUInitializeInterface,
		provisioningv1.DPUConfigFWParameters,
		provisioningv1.DPUPrepareBFB,
		provisioningv1.DPUOSInstalling,
		provisioningv1.DPUConfig:
		return true
	default:
		return false
	}
}
