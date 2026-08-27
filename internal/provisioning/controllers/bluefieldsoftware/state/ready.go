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
		for _, unit := range missingComponents {
			st.clearComponentStatus(unit)
		}
		return nil
	}

	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondReady)
	return nil
}

// checkMissingComponents returns the download units (per PSID for DPU PLDM bundles) whose
// on-disk file, unpacked output or recorded status no longer reflects the spec, so they can
// be re-downloaded and re-unpacked.
func (st *blueFieldSoftwareReadyState) checkMissingComponents() []componentInfo {
	var missing []componentInfo

	for _, unit := range specComponentUnits(st.bfs) {
		if unit.URL == "" {
			continue
		}
		if isURL(unit.URL) {
			path := componentDestinationPath(unit.ComponentType, componentFileName(st.bfs, unit))
			ok, err := isFileExist(path)
			if err != nil || !ok {
				missing = append(missing, unit)
				continue
			}
		} else if downloadedComponentPath(st.bfs, unit.ComponentType, unit.Key) != unit.URL {
			missing = append(missing, unit)
			continue
		}
		if st.extractedNicFwMissing(unit) {
			missing = append(missing, unit)
		}
	}

	return missing
}

// extractedNicFwMissing reports whether the NIC firmware image unpacked from the platform
// bundle is gone from disk while status still records its path. That image lives in the
// bundle's *-extracted directory, which is removed independently of the bundle file itself
// (Error-phase cleanup, host storage reclamation), and the checks above only see the bundle.
// Left unreported, status keeps a path the DPU agent can never fetch from bfb-registry and
// the version gate in extractedVersionsRecorded suppresses re-extraction for good.
func (st *blueFieldSoftwareReadyState) extractedNicFwMissing(unit componentInfo) bool {
	if unit.ComponentType != butil.ComponentTypePlatformFwBundle {
		return false
	}
	nicFw := st.bfs.Status.DownloadedComponents.NicFw
	if nicFw == "" {
		return false
	}
	ok, err := isFileExist(nicFw)
	return err != nil || !ok
}

// clearComponentStatus drops download paths and the version fields that
// extractedVersionsRecorded uses as its completion gate, so a re-download is
// followed by a fresh unpack instead of being permanently skipped.
func (st *blueFieldSoftwareReadyState) clearComponentStatus(unit componentInfo) {
	switch unit.ComponentType {
	case butil.ComponentTypeFwBundle:
		delete(st.bfs.Status.DownloadedComponents.PldmFwBundle, unit.Key)
		if st.bfs.Status.Versions != nil {
			delete(st.bfs.Status.Versions.BluefieldSoftwareVersions, unit.Key)
		}
	case butil.ComponentTypePlatformFwBundle:
		st.bfs.Status.DownloadedComponents.PlatformPldmFwBundle = ""
		// NicFw path/version come from unpacking the platform bundle.
		st.bfs.Status.DownloadedComponents.NicFw = ""
		if st.bfs.Status.Versions != nil {
			st.bfs.Status.Versions.EWNicFwVersion = ""
		}
	case butil.ComponentTypeNicFw:
		st.bfs.Status.DownloadedComponents.NicFw = ""
	case butil.ComponentTypeOSISO:
		st.bfs.Status.DownloadedComponents.OsIso = ""
	}
}
