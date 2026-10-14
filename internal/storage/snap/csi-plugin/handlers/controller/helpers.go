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

package controller

import (
	"encoding/hex"
	"hash/fnv"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// waitPoolInterval is the interval for polling the cache
	waitPoolInterval = time.Millisecond * 250
	// waitHardTimeout is the hard timeout for the wait operations, the context can be canceled earlier by the caller
	waitHardTimeout = time.Minute * 2
)

// generateDPUVolumeAttachmentName generates a name for DPUVolumeAttachment from DPU node name and volume name
// If the raw name is too long, it uses a FNV hash to create a shorter, deterministic name
func generateDPUVolumeAttachmentName(dpuNodeName, volumeName string) string {
	name := dpuNodeName + "-" + volumeName
	if len(name) > validation.DNS1123SubdomainMaxLength {
		return generateID("va-", dpuNodeName, volumeName)
	}
	return name
}

// GenerateID generates unique predictable ID: prefix + FNV hash of the identifiers
func generateID(prefix string, identifiers ...string) string {
	hash := fnv.New128()
	for _, i := range identifiers {
		hash.Write([]byte(i))
	}
	return prefix + hex.EncodeToString(hash.Sum([]byte{}))
}

// getCSIVolumeCtx constructs a volume context map by combining information from the CreateVolumeRequest.Parameters and DPUVolume object.
func getCSIVolumeCtx(createParams map[string]string, vol *storagev1.DPUVolume) (map[string]string, error) {
	volumeCtx := map[string]string{
		common.VolumeCtxStoragePolicyName: vol.Spec.DPUStoragePolicyName,
	}
	if vol.Status.State != nil {
		if vol.Status.State.SelectedDPUStorageVendorName != nil {
			volumeCtx[common.VolumeCtxStorageVendorName] = *vol.Status.State.SelectedDPUStorageVendorName
		}
		if vol.Status.State.StorageVendorPluginName != nil {
			volumeCtx[common.VolumeCtxStorageVendorPluginName] = *vol.Status.State.StorageVendorPluginName
		}
	}
	// if functionType or hotplugFunction is set in the createParams,
	// run through the validation and defaulting logic and add to the volumeCtx
	if createParams[common.StorageClassFunctionTypeKey] != "" || createParams[common.StorageClassHotplugFunctionKey] != "" {
		functionTypeConfig, err := common.FunctionTypeConfigFromStrings(
			createParams[common.StorageClassFunctionTypeKey],
			createParams[common.StorageClassHotplugFunctionKey])
		if err != nil {
			return nil, err
		}
		functionType, hotplugFunction := common.FunctionTypeConfigAsStrings(functionTypeConfig)
		volumeCtx[common.VolumeCtxFunctionType] = functionType
		volumeCtx[common.VolumeCtxHotplugFunction] = hotplugFunction
	}
	return volumeCtx, nil
}

func convertCSIVolumeMode(volCaps []*csi.VolumeCapability) *corev1.PersistentVolumeMode {
	typeFS := corev1.PersistentVolumeFilesystem
	typeBlock := corev1.PersistentVolumeBlock
	for _, c := range volCaps {
		switch c.GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			return &typeBlock
		case *csi.VolumeCapability_Mount:
			return &typeFS
		}
	}
	return &typeBlock
}

// convert access modes from CSI request to DCM storageAPI access modes
func convertCSIAccessModesToStorageAPIAccessModes(volCaps []*csi.VolumeCapability) []corev1.PersistentVolumeAccessMode {
	accessMode := []corev1.PersistentVolumeAccessMode{}
	for _, c := range volCaps {
		switch c.AccessMode.Mode {
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			accessMode = append(accessMode, corev1.ReadWriteMany)
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			accessMode = append(accessMode, corev1.ReadOnlyMany)
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER, csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER:
			accessMode = append(accessMode, corev1.ReadWriteOnce)
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER:
			accessMode = append(accessMode, corev1.ReadWriteOncePod)
		}
	}
	return accessMode
}
