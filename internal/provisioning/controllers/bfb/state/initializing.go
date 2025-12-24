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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type bfbInitializingState struct {
	bfb *provisioningv1.BFB
}

func (st *bfbInitializingState) Handle(context.Context, client.Client) error {
	if isDeleting(st.bfb) {
		st.bfb.Status.Phase = provisioningv1.BFBDeleting
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondInitialized,
			conditions.ReasonAwaitingDeletion, "BFB is being deleted")
		return nil
	}

	if st.bfb.Spec.FileName != nil {
		st.bfb.Status.FileName = *st.bfb.Spec.FileName
	} else {
		st.bfb.Status.FileName = util.DefaultBFBFilename(st.bfb)
	}

	conditions.AddTrue(st.bfb, provisioningv1.BFBCondInitialized)
	st.bfb.Status.Phase = provisioningv1.BFBDownloading
	return nil
}
