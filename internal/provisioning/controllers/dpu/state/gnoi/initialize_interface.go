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

package gnoi

import (
	"context"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/openconfig/gnmi/proto/gnmi"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if dpu.Spec.PCIAddress == nil {
		err := fmt.Errorf("PCIAddress is not set")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "InvalidPCIAddress", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
	capacityResult, err := checkCapacity(ctx, dpu, ctrlCtx)
	if err != nil {
		err = fmt.Errorf("failed to get DPU capacity: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToGetDPUCapacity", err.Error()))
		return *state, err
	}
	switch capacityResult {
	case dutil.CapacityUnknown:
		// send a warning in the condition message, but continue the flow
		state.Phase = provisioningv1.DPUConfigFWParameters
		cutil.SetDPUCondition(state, cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized), nil, "UnableToCheckCapacity",
			"WARNING: unable to check DPU CPU/Memory capacity, the DPUFlavor may be unfit for the DPU"))
	case dutil.CapacityInsufficient:
		err := fmt.Errorf("not enough resources for the given DPUFlavor")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "InsufficientResources", err.Error()))
		state.Phase = provisioningv1.DPUError
	default:
		state.Phase = provisioningv1.DPUConfigFWParameters
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), nil, "DMSInitialized", ""))
	}
	return *state, nil
}

// checkCapacity checks the capacity of the DPU using the gNMI interface. It returns error if the caller is expected to retry.
func checkCapacity(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (dutil.CapacityResult, error) {
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		return dutil.CapacityUnknown, err
	}

	conn, err := dutil.CreateGRPCConnection(ctx, ctrlCtx.Client, dpu, ctrlCtx)
	if err != nil {
		return dutil.CapacityUnknown, err
	}
	defer conn.Close() //nolint: errcheck

	gnmiClient := gnmi.NewGNMIClient(conn)
	pciAddress := strings.ReplaceAll(*dpu.Spec.PCIAddress, "-", ":") + ".0"
	log.FromContext(ctx).Info("get DPU capacity", "pciAddress", pciAddress)

	rsp, err := gnmiClient.Get(ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{
			{
				Elem: []*gnmi.PathElem{
					{Name: "nvidia"},
					{
						Name: "command",
						Key: map[string]string{
							"run": fmt.Sprintf("flint -d %s query full", pciAddress),
						},
					},
					{Name: "run"},
				},
			},
		},
	})
	if err != nil {
		return dutil.CapacityUnknown, fmt.Errorf("gNMI Get failed, err: %v", err)
	}
	if len(rsp.Notification) == 0 ||
		len(rsp.Notification[0].Update) == 0 ||
		rsp.Notification[0].Update[0].Val == nil {
		return dutil.CapacityUnknown, fmt.Errorf("failed to get DPU capacity, gNMI rsp: %v", rsp.String())
	}
	output := rsp.GetNotification()[0].GetUpdate()[0].GetVal().GetStringVal()
	for _, line := range strings.Split(output, "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		var bfSpecs *dutil.BlueFieldSpecs
		switch kv[0] {
		case "Part Number":
			bfSpecs = dutil.LookUpPartNumber(strings.TrimSpace(kv[1]))
		case "Description":
			bfSpecs = dutil.ParseDescription(strings.TrimSpace(kv[1]))
		default:
			continue
		}
		if bfSpecs == nil {
			continue
		}
		log.FromContext(ctx).Info("retrieved DPU specs via gNMI", "bfSpecs", bfSpecs)
		if result := bfSpecs.CanSatisfy(flavor.Spec.DPUResources); result != dutil.CapacityUnknown {
			return result, nil
		}
	}
	// it is possible that we can't get the DPU specs from the gNMI interface, in this case we return unknown
	return dutil.CapacityUnknown, nil
}
