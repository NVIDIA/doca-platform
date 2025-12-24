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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type bfbReadyState struct {
	bfb      *provisioningv1.BFB
	recorder record.EventRecorder
}

func (st *bfbReadyState) Handle(context.Context, client.Client) error {
	if isDeleting(st.bfb) {
		st.bfb.Status.Phase = provisioningv1.BFBDeleting
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondReady,
			conditions.ReasonAwaitingDeletion, "BFB is being deleted")
		return nil
	}

	if exist, err := checkingBFBFile(*st.bfb); !exist {
		st.bfb.Status.Phase = provisioningv1.BFBDownloading
		msg := fmt.Sprintf("BFB file %s not found, triggering re-download", st.bfb.Status.FileName)
		st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventBFBFileNotFoundReason, msg)
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondReady,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("BFB file %s not found", st.bfb.Status.FileName)))
		return err
	}

	conditions.AddTrue(st.bfb, provisioningv1.BFBCondReady)
	return nil
}

// check whether BFB file exist
func checkingBFBFile(bfb provisioningv1.BFB) (bool, error) {
	fullFileName := cutil.GenerateBFBFilePath(bfb.Status.FileName)
	if _, err := os.Stat(fullFileName); err != nil {
		return false, err
	}
	return true, nil
}
