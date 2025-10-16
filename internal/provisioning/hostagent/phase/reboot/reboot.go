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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BootIDDir         = "/var/lib/dpf/hostagent/boot_id"
	SystemdBootIDFile = "/proc/sys/kernel/random/boot_id"
	condition         = string(provisioningv1.DPUCondRebooted)
)

type RebootRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	UID          string `json:"uid"`
	RebootID     string `json:"rebootID"`
}

type Handler struct {
	sync.Mutex
	client.Client
	readyToReboot           map[types.UID]*provisioningv1.DPU
	getNode                 func() string
	getDeviceBySerialNumber func(string) (hostutil.Device, bool)
}

func NewHandler(client client.Client, nodeFunc func() string, snFunc func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		readyToReboot:           make(map[types.UID]*provisioningv1.DPU),
		Client:                  client,
		getNode:                 nodeFunc,
		getDeviceBySerialNumber: snFunc,
	}
}

func (r *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if r.getNode == nil {
		err := fmt.Errorf("node manager is not set")
		hostutil.NewCondition(condition).Failure(err, "NodeManagerNotSet").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	} else if r.getDeviceBySerialNumber == nil {
		err := fmt.Errorf("network manager is not set")
		hostutil.NewCondition(condition).Failure(err, "NetworkManagerNotSet").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: r.getNode()}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			return dpu.Status, ctrl.Result{}, nil
		}
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUNode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	if dpuNode.Spec.NodeRebootMethod != nil && dpuNode.Spec.NodeRebootMethod.GNOI == nil && dpuNode.Spec.NodeRebootMethod.HostAgent == nil { //nolint:staticcheck
		return dpu.Status, ctrl.Result{}, nil
	}
	finished, err := r.rebootFinished(dpu)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToCheckRebootProgress").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	if finished {
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, nil
	}

	// Requeue if any other DPU is being provisioned
	canReboot, err := r.canReboot(ctx, dpu)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToCheckRebootProgress").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	} else if !canReboot {
		hostutil.NewCondition(condition).Failure(fmt.Errorf("pending reboot"), "PendingReboot").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	patcher := patch.NewSerialPatcher(dpuNode, r.Client)
	dpuNode.Status.RebootInProgress = ptr.To(true)
	if err := patcher.Patch(ctx, dpuNode); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToSetRebootInProgress").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}

	rebootCommand, rebootType, err := reboot.GenerateCmd(dpuNode.Annotations, dpu.Annotations)
	if err != nil {
		err = fmt.Errorf("invalid reboot annotation on DPUNode or DPU. err: %w", err)
		hostutil.NewCondition(condition).Failure(err, "FailedToGenerateRebootCommand").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	// Return early and set node to ready if we should skip the powercycle/reboot command.
	// Note: This is mainly for testing. Skipping the powercycle/reboot may cause issues with
	// the firmware installation and configuration.
	if rebootCommand == reboot.Skip {
		logger.Info("Warning not rebooting: this may cause issues with DPU firmware installation and configuration")
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, nil
	}
	switch rebootType {
	case reboot.PowerCycle:
		logger.Info(fmt.Sprintf("DPU %s powercycle command %q", dpu.Name, rebootCommand))
		_, stderr, err := hostutil.RunBash(rebootCommand)
		if err != nil {
			hostutil.NewCondition(condition).
				Failure(fmt.Errorf("failed to powercycle. cmd: %s, stderr: %s, err: %w", rebootCommand, stderr.String(), err), "FailedToPowerCycle").
				Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		}
	case reboot.WarmReboot:
		logger.Info(fmt.Sprintf("DPU %s Bluefield System-Level-Reset", dpu.Name))
		inProgress, err := r.slr(ctx, dpu)
		if err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToSLR").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		} else if inProgress {
			return dpu.Status, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	default:
		err := fmt.Errorf("invalid reboot type: %s", rebootType)
		hostutil.NewCondition(condition).Failure(err, "InvalidRebootType").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	// we won't reach here
	return dpu.Status, ctrl.Result{}, nil
}

func (r *Handler) rebootFinished(dpu *provisioningv1.DPU) (bool, error) {
	dpuBootID, err := readDPUBootID(dpu)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return false, writeDPUBootIDFile(dpu)
	}
	currentBootID, err := os.ReadFile(SystemdBootIDFile)
	if err != nil {
		return false, fmt.Errorf("failed to read current boot ID, err: %v", err)
	}
	return dpuBootID != string(currentBootID), nil
}

func (r *Handler) sync(readyDPU *provisioningv1.DPU, allDPUs ...provisioningv1.DPU) bool {
	r.Lock()
	defer r.Unlock()
	r.readyToReboot[readyDPU.UID] = readyDPU
	// check if all being provisioned DPUs are ready to reboot
	dpuSet := make(map[types.UID]struct{})
	canReboot := true
	for _, dpu := range allDPUs {
		dpuSet[dpu.UID] = struct{}{}
		if isBeingProvisioned(dpu.Status.Phase) {
			if _, ok := r.readyToReboot[dpu.UID]; !ok {
				canReboot = false
			}
		}
	}
	// delete ready DPUs that no longer exist
	for _, dpu := range r.readyToReboot {
		if _, ok := dpuSet[dpu.UID]; !ok {
			delete(r.readyToReboot, dpu.UID)
		}
	}
	return canReboot
}

func (r *Handler) canReboot(ctx context.Context, dpu *provisioningv1.DPU) (bool, error) {
	dpuList := &provisioningv1.DPUList{}
	err := r.List(ctx, dpuList, client.MatchingLabels{
		cutil.DPUNodeNameLabel: r.getNode(),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list DPUs, err: %v", err)
	}
	canReboot := r.sync(dpu, dpuList.Items...)
	if !canReboot {
		return false, nil
	}
	return true, nil
}

func (r *Handler) slr(ctx context.Context, dpu *provisioningv1.DPU) (inProgress bool, err error) {
	log := log.FromContext(ctx)
	dev, ok := r.getDeviceBySerialNumber(dpu.Spec.SerialNumber)
	if !ok {
		return false, fmt.Errorf("failed to get device by serial number: %s", dpu.Spec.SerialNumber)
	}
	// find DPU rshim
	rshim, err := RshimNameByPCI(dev.Address)
	if err != nil {
		return false, fmt.Errorf("failed to find DPU rshim for PCI address: %s. err: %v", dev.Address, err)
	}
	log.Info("Found DPU rshim", "Rshim", rshim, "PCI address", dev.Address)

	pciAddr := filepath.Base(hostutil.NewPCIHelper(dev.Address).PF(0).Path())
	off, lastLine, err := IsDPUOff(rshim)
	if err != nil {
		return false, fmt.Errorf("failed to check if DPU is off. err: %v", err)
	} else if !off {
		log.Info("DPU is not off, attempt to stop ARM", "Last line", lastLine, "PCI address", pciAddr)
		// the mlxfwreset command returns error if the ARM is already being stopped, so ideally we should not run it again.
		// However, it's not a trivial task to determine if the ARM is already being stopped.
		// To make things simpler, we simply run the cmd and return the error until we get "System Off" from rshim/misc.
		// The downside is that an error message will appear in the DPU.status.condition.
		// TODO: run SLR only once for each DPU and sync all SLRs
		cmd := fmt.Sprintf("mlxfwreset -d %s -l 1 -t 4 reset -y --sync 0", pciAddr)
		stdout, stderr, err := hostutil.RunBash(cmd)
		if err != nil {
			// the exit code of mlxfwreset can be confusing. Sometimes, it is 1 even if the ARM is successfully stopped.
			// So we will continue to reboot the host when we see "System Off" from rshim/misc.
			// NOTE: mlxfwreset outputs error messages to stdout rather than stderr.
			return false, fmt.Errorf("failed to stop ARM. This error can be ignored as long as the host is automatically rebooted later. cmd: %s, stderr: %s, stdout: %s", cmd, stderr.String(), stdout.String())
		}
		return true, nil
	}

	log.Info("Attempting to reboot host", "PCI address", pciAddr)
	cmd := fmt.Sprintf("mlxfwreset -d %s -l 4 r -y", pciAddr)
	stdout, stderr, err := hostutil.RunBash(cmd)
	if err != nil {
		// mlxfwreset outputs error messages to stdout
		return false, fmt.Errorf("failed to reboot host. This error can be ignored as long as the host is automatically rebooted later. cmd: %s, stderr: %s, stdout: %s", cmd, stderr.String(), stdout.String())
	}
	return false, nil
}

func rebootRequestFileName(dpu *provisioningv1.DPU) string {
	return filepath.Join(BootIDDir, string(dpu.UID))
}

func readDPUBootID(dpu *provisioningv1.DPU) (string, error) {
	data, err := os.ReadFile(rebootRequestFileName(dpu))
	if err != nil {
		return "", err
	}
	request := &RebootRequest{}
	err = json.Unmarshal(data, request)
	if err != nil {
		return "", err
	}
	return request.RebootID, nil
}

func writeDPUBootIDFile(dpu *provisioningv1.DPU) error {
	bootID, err := os.ReadFile(SystemdBootIDFile)
	if err != nil {
		return err
	}
	request := &RebootRequest{
		DPUName:      dpu.Name,
		DPUNamespace: dpu.Namespace,
		UID:          string(dpu.UID),
		RebootID:     string(bootID),
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return hostutil.AtomicWrite(rebootRequestFileName(dpu), requestBytes, 0644)
}

func isBeingProvisioned(phase provisioningv1.DPUPhase) bool {
	switch phase {
	case provisioningv1.DPUInitializeInterface,
		provisioningv1.DPUConfigFWParameters,
		provisioningv1.DPUPrepareBFB,
		provisioningv1.DPUOSInstalling,
		provisioningv1.DPUCheckingHostRebootNeed,
		provisioningv1.DPURebooting:
		return true
	default:
		return false
	}
}

// RshimNameByPCI finds the rshim appropriate to the given PCI address.
// Iterate over all the rshim devices and searches for the one that contains the given target PCI.
func RshimNameByPCI(PCIAddress string) (string, error) {
	cmd := "ls /dev | egrep 'rshim.*[0-9]+' | while read line ; do echo $(" +
		"echo 'DISPLAY_LEVEL 1' > /dev/$line/misc && " +
		"cat /dev/$line/misc | grep " + PCIAddress + " | xargs -r echo $line | awk 'END {print $1}') ; done | tr -d '[:space:]'"
	out, stderr, err := hostutil.RunBash(cmd)
	if err != nil || len(stderr.String()) > 0 || len(out.String()) == 0 {
		return "", fmt.Errorf("can't find rshim address on device: %v, stderr: %v, error: %v", PCIAddress, stderr, err)
	}
	return out.String(), nil
}

func IsDPUOff(rshim string) (bool, string, error) {
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
