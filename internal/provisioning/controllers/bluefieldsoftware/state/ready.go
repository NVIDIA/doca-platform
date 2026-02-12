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

	// Define component mappings
	components := []struct {
		url           string
		componentType butil.ComponentType
	}{
		{st.bfs.Status.DownloadedComponents.PldmFwBundle, butil.ComponentTypeFwBundle},
		{st.bfs.Status.DownloadedComponents.OsIso, butil.ComponentTypeOSISO},
		{st.bfs.Status.DownloadedComponents.BmcErot, butil.ComponentTypeBMCEROT},
		{st.bfs.Status.DownloadedComponents.BmcFw, butil.ComponentTypeBMC},
		{st.bfs.Status.DownloadedComponents.AstraNicFw, butil.ComponentTypeNIC},
		{st.bfs.Status.DownloadedComponents.GraceErot, butil.ComponentTypeGRACEEROT},
		{st.bfs.Status.DownloadedComponents.GraceFw, butil.ComponentTypeGRACEFW},
	}

	// Check each component
	for _, comp := range components {
		if st.isComponentMissing(comp.url, comp.componentType) {
			missing = append(missing, comp.componentType)
		}
	}

	return missing
}

func (st *blueFieldSoftwareReadyState) isComponentMissing(url string, componentType butil.ComponentType) bool {
	if url == "" || !isURL(url) {
		return false
	}

	fileName := butil.DefaultComponentFilename(st.bfs, componentType)
	exists, _ := isFileExist(generateComponentFilePath(fileName))
	return !exists
}

func (st *blueFieldSoftwareReadyState) clearComponentStatus(componentType butil.ComponentType) {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		st.bfs.Status.DownloadedComponents.PldmFwBundle = ""
	case butil.ComponentTypeOSISO:
		st.bfs.Status.DownloadedComponents.OsIso = ""
	case butil.ComponentTypeBMCEROT:
		st.bfs.Status.DownloadedComponents.BmcErot = ""
	case butil.ComponentTypeBMC:
		st.bfs.Status.DownloadedComponents.BmcFw = ""
	case butil.ComponentTypeNIC:
		st.bfs.Status.DownloadedComponents.AstraNicFw = ""
	case butil.ComponentTypeGRACEEROT:
		st.bfs.Status.DownloadedComponents.GraceErot = ""
	case butil.ComponentTypeGRACEFW:
		st.bfs.Status.DownloadedComponents.GraceFw = ""
	}
}
