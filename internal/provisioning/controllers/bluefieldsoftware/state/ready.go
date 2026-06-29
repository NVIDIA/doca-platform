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
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type blueFieldSoftwareReadyState struct {
	bfs      *provisioningv1.BlueFieldSoftware
	recorder record.EventRecorder
}

func (st *blueFieldSoftwareReadyState) Handle(context.Context, client.Client) error {
	if isDeleting(st.bfs) {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}

	// Check if any component files are missing
	missingComponents := st.checkMissingComponents()
	if len(missingComponents) > 0 {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDownloading
		msg := fmt.Sprintf("Component files missing: %v, triggering re-download", missingComponents)
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventBFBFileNotFoundReason, msg)
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady,
			conditions.ReasonError,
			conditions.ConditionMessage(msg))

		// Clear the missing components from status to trigger re-download
		for _, componentType := range missingComponents {
			st.clearComponentStatus(componentType)
		}
		return nil
	}

	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondReady)
	return nil
}

func (st *blueFieldSoftwareReadyState) checkMissingComponents() []butil.ComponentType {
	var missing []butil.ComponentType

	type row struct {
		specURL string
		stored  string
		ct      butil.ComponentType
	}
	var rows []row
	rows = append(rows,
		row{ptr.Deref(st.bfs.Spec.PldmFwBundle, ""), st.bfs.Status.DownloadedComponents.PldmFwBundle, butil.ComponentTypeFwBundle},
		row{ptr.Deref(st.bfs.Spec.PlatformPldmFwBundle, ""), st.bfs.Status.DownloadedComponents.PlatformPldmFwBundle, butil.ComponentTypePlatformFwBundle},
		row{st.bfs.Spec.OsIso, st.bfs.Status.DownloadedComponents.OsIso, butil.ComponentTypeOSISO},
	)

	for _, r := range rows {
		if r.specURL == "" {
			continue
		}
		if isURL(r.specURL) {
			path := componentDestinationPath(r.ct, butil.ComponentDownloadFilename(st.bfs, r.ct, r.specURL))
			ok, err := isFileExist(path)
			if err != nil || !ok {
				missing = append(missing, r.ct)
			}
			continue
		}
		if r.stored != r.specURL {
			missing = append(missing, r.ct)
		}
	}

	return missing
}

func (st *blueFieldSoftwareReadyState) clearComponentStatus(componentType butil.ComponentType) {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		st.bfs.Status.DownloadedComponents.PldmFwBundle = ""
	case butil.ComponentTypePlatformFwBundle:
		st.bfs.Status.DownloadedComponents.PlatformPldmFwBundle = ""
	case butil.ComponentTypeOSISO:
		st.bfs.Status.DownloadedComponents.OsIso = ""
	}
}
