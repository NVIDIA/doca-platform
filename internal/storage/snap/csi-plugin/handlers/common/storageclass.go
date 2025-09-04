/*
Copyright 2025 NVIDIA

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

package common

import (
	"maps"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
)

// Name of the Keys in StorageClass that supported by the plugin.
// These parameters are passed to the plugin in the CreateVolumeRequest.Parameters field.
const (
	// StorageClassPolicyKey defines the StorageClass parameter key that determines
	// which policy to use when creating volumes.
	// This fields translates to DPUVolume.Spec.DPUStoragePolicyName field.
	StorageClassPolicyKey = "policy"
	// StorageClassFunctionTypeKey defines the StorageClass parameter key that determines
	// which function type to use when creating volumes.
	// This fields translates to DPUVolumeAttachment.Spec.FunctionType field.
	StorageClassFunctionTypeKey = "functionType"
	// StorageClassHotplugFunctionKey defines the StorageClass parameter key that determines
	// if the volume is hotplugged.
	// This fields translates to DPUVolumeAttachment.Spec.HotplugFunction field.
	StorageClassHotplugFunctionKey = "hotplugFunction"
)

// ValidateCreateVolumeParameters validates the parameters for the CreateVolumeRequest.Parameters field.
func ValidateCreateVolumeParameters(commonConfig config.Common, params map[string]string) error {
	if params[StorageClassPolicyKey] == "" {
		return FieldIsRequiredError("Parameters.policy")
	}
	_, err := FunctionTypeConfigFromStrings(commonConfig, params[StorageClassFunctionTypeKey], params[StorageClassHotplugFunctionKey])
	if err != nil {
		return FieldIsInvalidError("Parameters", err.Error())
	}
	return nil
}

// ExcludeCSIPluginStorageClassParameters excludes the parameters that are used by the SNAP CSI plugin
// and should not be passed to other storage components.
// StorageClass parameters that are not excluded by this function are passed to the DPUVolume.Spec.Parameters field.
func ExcludeCSIPluginStorageClassParameters(params map[string]string) map[string]string {
	result := maps.Clone(params)
	delete(result, StorageClassPolicyKey)
	delete(result, StorageClassFunctionTypeKey)
	delete(result, StorageClassHotplugFunctionKey)
	return result
}
