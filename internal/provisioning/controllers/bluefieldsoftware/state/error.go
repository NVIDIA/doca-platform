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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type blueFieldSoftwareErrorState struct {
	bfs *provisioningv1.BlueFieldSoftware
}

func (st *blueFieldSoftwareErrorState) Handle(ctx context.Context, _ client.Client) error {
	cleanupErr := cleanupInFlightComponentArtifacts(st.bfs)
	if isDeleting(st.bfs) {
		if cleanupErr != nil {
			log.FromContext(ctx).Error(cleanupErr, "failed to cleanup in-flight component artifacts during deletion")
		}
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondError,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}
	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondError)
	if cleanupErr != nil {
		return fmt.Errorf("cleanup in-flight component artifacts: %w", cleanupErr)
	}
	return nil
}
