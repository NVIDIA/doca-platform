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
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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
	if dpuNode.Spec.NodeRebootMethod == nil || dpuNode.Spec.NodeRebootMethod.GNOI == nil {
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

	if err := r.slr(ctx, dpu); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToSLR").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	// todo: power cycle
	hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
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

func (r *Handler) slr(ctx context.Context, dpu *provisioningv1.DPU) error {
	log := ctrllog.FromContext(ctx)
	dev, ok := r.getDeviceBySerialNumber(dpu.Spec.SerialNumber)
	if !ok {
		return fmt.Errorf("failed to get device by serial number: %s", dpu.Spec.SerialNumber)
	}
	pciAddr := filepath.Base(hostutil.NewPCIHelper(dev.Address).PF(0).Path())

	log.Info("Attempting to stop ARM", "PCI address", pciAddr)
	cmd := fmt.Sprintf("mlxfwreset -d %s -l 1 -t 4 reset -y --sync 0", pciAddr)
	_, stderr, err := hostutil.RunBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to stop ARM. cmd: %s, stderr: %s", cmd, stderr.String())
	}

	log.Info("Attempting to reboot host", "PCI address", pciAddr)
	cmd = fmt.Sprintf("mlxfwreset -d %s -l 4 r -y", pciAddr)
	_, stderr, err = hostutil.RunBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to reboot host. cmd: %s, stderr: %s", cmd, stderr.String())
	}
	return nil
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
