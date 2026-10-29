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

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type bfbInitializingState struct {
	bfb *provisioningv1.BFB
}

func (st *bfbInitializingState) Handle(context.Context, client.Client) (provisioningv1.BFBStatus, error) {
	state := st.bfb.Status.DeepCopy()
	if isDeleting(st.bfb) {
		state.Phase = provisioningv1.BFBDeleting
		return *state, nil
	}

	if st.bfb.Spec.FileName != nil {
		state.FileName = *st.bfb.Spec.FileName
	} else {
		state.FileName = util.DefaultBFBFilename(st.bfb)
	}
	state.Phase = provisioningv1.BFBDownloading
	return *state, nil
}
