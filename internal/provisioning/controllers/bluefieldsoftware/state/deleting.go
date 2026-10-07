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

package state

import (
	"context"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type blueFieldSoftwareDeletingState struct {
	bfs      *provisioningv1.BlueFieldSoftware
	recorder record.EventRecorder
}

// blockDeletion reports the BlueFieldSoftware as still in use, keeping it in Deleting.
func (st *blueFieldSoftwareDeletingState) blockDeletion(msg string) error {
	st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDeleteBlueFieldSoftwareReason, msg)
	conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDeleted,
		conditions.ReasonPending, conditions.ConditionMessage(msg))
	return fmt.Errorf("%s", msg)
}

// blockedByDPUSet returns a non-nil error while any DPUSet still claims this BlueFieldSoftware.
// A DPUSet spec reference alone is enough: the per-DPUSet protection finalizer is added
// asynchronously by the DPUSet controller, so it can still be missing when the BlueFieldSoftware
// is deleted right after the DPUSet is created, and Kubernetes rejects adding finalizers once
// deletionTimestamp is set. A DPUSet that is itself being deleted is not a claim: reconcileDelete
// drops the protection finalizer before the object is garbage-collected, and the leftover spec
// reference must not keep deletion blocked.
func (st *blueFieldSoftwareDeletingState) blockedByDPUSet(ctx context.Context, c client.Client) error {
	dpuSetList := &provisioningv1.DPUSetList{}
	if err := c.List(ctx, dpuSetList, client.InNamespace(st.bfs.Namespace)); err != nil {
		return st.blockDeletion(fmt.Errorf("failed to list DPUSets for reference checking: %w", err).Error())
	}
	for _, ds := range dpuSetList.Items {
		bfsRef := ds.Spec.DPUTemplate.Spec.BlueFieldSoftware
		if bfsRef != nil && bfsRef.Name == st.bfs.Name && ds.DeletionTimestamp.IsZero() {
			return st.blockDeletion(fmt.Sprintf("Cannot delete BlueFieldSoftware %s/%s: still referenced by DPUSet %s",
				st.bfs.Namespace, st.bfs.Name, ds.Name))
		}
	}

	// Also wait for protection finalizers left by DPUSets that are already gone from the API.
	for _, f := range st.bfs.Finalizers {
		if strings.HasPrefix(f, provisioningv1.BlueFieldSoftwareFinalizerPrefix) {
			return st.blockDeletion(fmt.Sprintf("Cannot delete BlueFieldSoftware %s/%s: still protected by DPUSet finalizer %s",
				st.bfs.Namespace, st.bfs.Name, f))
		}
	}
	return nil
}

func (st *blueFieldSoftwareDeletingState) Handle(ctx context.Context, c client.Client) error {
	if err := st.blockedByDPUSet(ctx, c); err != nil {
		return err
	}

	// Delete all downloaded component files (one per PSID for platform bundles).
	var errors []error
	for _, unit := range specComponentUnits(st.bfs) {
		filePath := completedComponentFilePath(st.bfs, unit)
		if filePath == "" {
			continue
		}

		if err := cutil.RemoveFileEx(filePath); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete %s (key %q): %w", unit.ComponentType, unit.Key, err))
		}
	}

	for _, unit := range extractionUnits(st.bfs) {
		extractDir := extractOutputDirForBFS(st.bfs, unit.ComponentType, unit.Key)
		if extractDir == "" {
			continue
		}
		// RemoveAll returns nil when the path does not exist; ignore ignorable NFS errors.
		if err := cutil.RemoveAllEx(extractDir); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete extract output directory %q (component %s key %q): %w", extractDir, unit.ComponentType, unit.Key, err))
		}
	}

	if len(errors) > 0 {
		errMsg := fmt.Sprintf("Errors during cleanup: %v", errors)
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, errMsg)
		// Continue with deletion even if cleanup fails
	}

	// Remove cleanup finalizer - patcher will handle the update
	controllerutil.RemoveFinalizer(st.bfs, provisioningv1.BlueFieldSoftwareFinalizer)

	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondDeleted)
	msg := fmt.Sprintf("BlueFieldSoftware: (%s/%s) deleted successfully", st.bfs.Namespace, st.bfs.Name)
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, msg)

	return nil
}
