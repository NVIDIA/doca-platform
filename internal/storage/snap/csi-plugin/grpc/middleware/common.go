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

package middleware

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
)

// These fields names are used for logging and
// as a prefixes for locks which prevent concurrent requests with same
// volumeID, volumeName, snapshotName and snapshotID
const (
	VolumeIDFieldName     = "volumeID"
	VolumeNameFieldName   = "volumeName"
	SnapshotNameFieldName = "snapshotName"
	SnapshotIDFiledName   = "snapshotID"
)

// Return field name and field value which can be used to identify target object of the request/
// Can be used to serialize access to the object or to inject object's identifier to the logger.
// Return empty strings if request doesn't have such field.
func getIdentifierFieldForReq(req interface{}) (string, string) {
	switch r := req.(type) {
	case *csi.CreateVolumeRequest:
		return VolumeNameFieldName, r.GetName()
	case *csi.CreateSnapshotRequest:
		return SnapshotNameFieldName, r.GetName()
	case *csi.DeleteVolumeRequest,
		*csi.ControllerPublishVolumeRequest,
		*csi.ControllerUnpublishVolumeRequest,
		*csi.ControllerExpandVolumeRequest,
		*csi.NodeStageVolumeRequest,
		*csi.NodeUnstageVolumeRequest,
		*csi.NodePublishVolumeRequest,
		*csi.NodeUnpublishVolumeRequest,
		*csi.NodeExpandVolumeRequest:
		volumeIDReader, ok := r.(interface{ GetVolumeId() string })
		if !ok {
			return "", ""
		}
		return VolumeIDFieldName, volumeIDReader.GetVolumeId()
	case *csi.DeleteSnapshotRequest:
		return SnapshotIDFiledName, r.GetSnapshotId()
	default:
		return "", ""
	}
}
