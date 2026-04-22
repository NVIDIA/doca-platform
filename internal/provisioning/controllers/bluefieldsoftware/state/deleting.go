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
	stderrors "errors"
	"fmt"
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
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

func (st *blueFieldSoftwareDeletingState) Handle(ctx context.Context, c client.Client) error {
	// Check if any DPU is using this BlueFieldSoftware
	dpuList := &provisioningv1.DPUList{}
	if err := c.List(ctx, dpuList); err != nil {
		return fmt.Errorf("failed to list DPUs: %w", err)
	}

	var usingDPUs []string
	for _, dpu := range dpuList.Items {
		if dpu.Spec.BlueFieldSoftware == st.bfs.Name {
			usingDPUs = append(usingDPUs, fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name))
		}
	}

	if len(usingDPUs) > 0 {
		errMsg := fmt.Sprintf("Cannot delete BlueFieldSoftware %s/%s: still being used by DPUs: %v",
			st.bfs.Namespace, st.bfs.Name, usingDPUs)
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, errMsg)
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDeleted,
			conditions.ReasonPending, conditions.ConditionMessage(errMsg))
		return fmt.Errorf("cannot delete BlueFieldSoftware %s/%s: still being used by DPUs: %v",
			st.bfs.Namespace, st.bfs.Name, usingDPUs)
	}

	// Delete all downloaded component files
	componentsToDelete := []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypeOSISO,
		butil.ComponentTypeBMCEROT,
		butil.ComponentTypeBMC,
		butil.ComponentTypeNIC,
		butil.ComponentTypeGRACEEROT,
		butil.ComponentTypeGRACEFW,
	}

	var errors []error
	for _, componentType := range componentsToDelete {
		fileName := butil.DefaultComponentFilename(st.bfs, componentType)
		filePath := componentDestinationPath(componentType, fileName)

		if exists, err := isFileExist(filePath); err != nil {
			errors = append(errors, fmt.Errorf("failed to check %s: %w", componentType, err))
		} else if exists {
			if err := os.Remove(filePath); err != nil {
				errors = append(errors, fmt.Errorf("failed to delete %s: %w", componentType, err))
			}
		}
	}

	extractDir := extractOutputDirForBFS(st.bfs)
	if extractDir != "" {
		// RemoveAll returns nil when the path does not exist; ignore ErrNotExist if wrapped.
		if err := os.RemoveAll(extractDir); err != nil && !stderrors.Is(err, os.ErrNotExist) {
			errors = append(errors, fmt.Errorf("failed to delete extract output directory %q: %w", extractDir, err))
		}
	}

	if len(errors) > 0 {
		errMsg := fmt.Sprintf("Errors during cleanup: %v", errors)
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, errMsg)
		// Continue with deletion even if cleanup fails
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(st.bfs, provisioningv1.BlueFieldSoftwareFinalizer)
	if err := c.Update(ctx, st.bfs); err != nil {
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}

	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondDeleted)
	msg := fmt.Sprintf("BlueFieldSoftware: (%s/%s) deleted successfully", st.bfs.Namespace, st.bfs.Name)
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, msg)

	return nil
}
