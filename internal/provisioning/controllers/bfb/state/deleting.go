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

package state

import (
	"context"
	"fmt"
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type bfbDeletingState struct {
	bfb      *provisioningv1.BFB
	recorder record.EventRecorder
}

func (st *bfbDeletingState) Handle(ctx context.Context, client ctrlclient.Client) error {
	// Ensure any in-flight download task is canceled/cleaned up during deletion.
	taskName := cutil.GenerateBFBTaskName(*st.bfb)
	if cancelFunc, ok := butil.DownloadingTaskMap.Load(taskName + "cancel"); ok {
		cancelFunc.(context.CancelFunc)()
	}
	butil.DownloadingTaskMap.Delete(taskName)
	butil.DownloadingTaskMap.Delete(taskName + "cancel")

	// Check for DPUSet references (scoped to same namespace)
	dpusetList := &provisioningv1.DPUSetList{}
	if err := client.List(ctx, dpusetList, ctrlclient.InNamespace(st.bfb.Namespace)); err != nil {
		err = fmt.Errorf("failed to list DPUSets for reference checking: %w", err)
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDeleted,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
		return err
	}

	// Early exit: check if any DPUSet references this BFB
	for _, ds := range dpusetList.Items {
		if ds.Spec.DPUTemplate.Spec.BFB != nil && ds.Spec.DPUTemplate.Spec.BFB.Name == st.bfb.Name {
			err := fmt.Errorf("BFB %s/%s cannot be deleted, still referenced by DPUSet: %s",
				st.bfb.Namespace, st.bfb.Name, ds.Name)
			conditions.AddFalse(st.bfb, provisioningv1.BFBCondDeleted,
				conditions.ReasonPending, conditions.ConditionMessage(err.Error()))
			return err
		}
	}

	// Check for DPU references (scoped to same namespace)
	dpuList := &provisioningv1.DPUList{}
	if err := client.List(ctx, dpuList, ctrlclient.InNamespace(st.bfb.Namespace)); err != nil {
		err = fmt.Errorf("failed to list DPUs for reference checking: %w", err)
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDeleted,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
		return err
	}

	// Early exit: check if any DPU references this BFB
	for _, dpu := range dpuList.Items {
		if ptr.Deref(dpu.Spec.BFB, "") == st.bfb.Name {
			err := fmt.Errorf("BFB %s/%s cannot be deleted, still referenced by DPU: %s",
				st.bfb.Namespace, st.bfb.Name, dpu.Name)
			conditions.AddFalse(st.bfb, provisioningv1.BFBCondDeleted,
				conditions.ReasonPending, conditions.ConditionMessage(err.Error()))
			return err
		}
	}

	// No references found, proceed with deletion
	// Check if the File name was set during the initialization phase
	if st.bfb.Status.FileName != "" {
		bfbFile := cutil.GenerateBFBFilePath(st.bfb.Status.FileName)
		err := os.Remove(bfbFile)
		if err != nil && !os.IsNotExist(err) {
			msg := fmt.Sprintf("Deleting BFB: (%s/%s) failed", st.bfb.Namespace, st.bfb.Name)
			st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventFailedDeleteBFBReason, msg)
			conditions.AddFalse(st.bfb, provisioningv1.BFBCondDeleted,
				conditions.ReasonError, conditions.ConditionMessage(err.Error()))
			return err
		}
	}

	tempFileName := cutil.GenerateBFBTMPFilePath(string(st.bfb.UID))
	err := os.Remove(tempFileName)
	if err != nil && !os.IsNotExist(err) {
		msg := fmt.Sprintf("Deleting BFB temp file: (%s/%s) failed", st.bfb.Namespace, st.bfb.Name)
		st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventFailedDeleteBFBReason, msg)
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDeleted,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
		return err
	}

	// Set success condition before removing finalizer
	conditions.AddTrue(st.bfb, provisioningv1.BFBCondDeleted)

	// Remove finalizer - patcher will handle the update
	controllerutil.RemoveFinalizer(st.bfb, provisioningv1.BFBFinalizer)

	return nil
}
