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

package redfish

import (
	"context"
	"encoding/json"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// persistRedfishTaskID writes only status.redfishTaskId immediately after the BMC accepts an
// async install/update task. It does not mutate dpu's ResourceVersion or Status: the reconciler
// still owns the deferred SerialPatcher write from the phase-handler result, and stamping a mid-
// reconcile ResourceVersion would lock that patch and fail with 409 after conditions advance.
//
// On patch failure the error is returned so the phase does not pretend the checkpoint landed.
// Callers keep the accepted task id on the working status for the deferred write.
func persistRedfishTaskID(ctx context.Context, c client.Client, dpu *provisioningv1.DPU, taskID string) error {
	if c == nil {
		return fmt.Errorf("persist redfishTaskId: nil client")
	}
	if dpu == nil {
		return fmt.Errorf("persist redfishTaskId: nil dpu")
	}
	if taskID == "" {
		return fmt.Errorf("persist redfishTaskId: empty task id")
	}
	body, err := json.Marshal(map[string]any{
		"status": map[string]any{
			"redfishTaskId": taskID,
		},
	})
	if err != nil {
		return fmt.Errorf("persist redfishTaskId %q: %w", taskID, err)
	}
	// Patch a copy so the client cannot refresh ResourceVersion on the reconciler's object.
	if err := c.Status().Patch(ctx, dpu.DeepCopy(), client.RawPatch(types.MergePatchType, body)); err != nil {
		return fmt.Errorf("persist redfishTaskId %q: %w", taskID, err)
	}
	return nil
}

// checkpointRedfishTaskID sets state.RedfishTaskID to the accepted id and immediately patches
// that field onto the DPU. Patch failure is returned while the in-memory task id on state is
// retained for the deferred reconcile status write (UpdateDPUStatus applies state onto dpu).
func checkpointRedfishTaskID(ctx context.Context, c client.Client, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("checkpoint redfishTaskId: empty task id")
	}
	id := taskID
	state.RedfishTaskID = &id
	if err := persistRedfishTaskID(ctx, c, dpu, taskID); err != nil {
		log.FromContext(ctx).Error(err, "BMC accepted install task but immediate redfishTaskId status patch failed; retaining in-memory task id for deferred status patch",
			"taskID", taskID)
		return err
	}
	return nil
}
