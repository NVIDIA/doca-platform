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

package common

// Constants for the CSI context.
// VolumeContext and PublishContext are used to pass data between the different stages of the Volume lifecycle.
// VolumeContext is set by the controller when creating the volume.
// PublishContext is set by the controller when publishing the volume.
const (
	// VolumeCtxStoragePolicyName contains the name of the storage policy of the volume.
	VolumeCtxStoragePolicyName = "storagePolicyName"
	// VolumeCtxStorageVendorName contains the name of the storage vendor of the volume.
	VolumeCtxStorageVendorName = "storageVendorName"
	// VolumeCtxStorageVendorPluginName contains the name of the storage vendor plugin of the volume.
	VolumeCtxStorageVendorPluginName = "storageVendorPluginName"
	// VolumeCtxFunctionType contains the type of the storage function that will be used to attach the volume.
	VolumeCtxFunctionType = "functionType"
	// VolumeCtxHotplugFunction contains the hotplug setting of the storage function that will be used to attach the volume
	VolumeCtxHotplugFunction = "hotplugFunction"

	// VendorPrefix is the prefix used for the vendor specific keys in the PublishContext.
	VendorPrefix = "nv-"
	// PublishCtxNvVolumeName contains the name of the volume
	PublishCtxNvVolumeName = VendorPrefix + "volumeName"
	// PublishCtxNvVolumeAttachmentName contains the name of the volume attachment
	PublishCtxNvVolumeAttachmentName = VendorPrefix + "volumeAttachmentName"
	// PublishCtxDevicePciAddress contains the PCI address of the device to which the volume is attached, must be set for all emulation modes
	PublishCtxDevicePciAddress = VendorPrefix + "pciDeviceAddress"
	// PublishCtxNvmeNsID contains the NVMe namespace ID to which the volume is attached, must be set when the plugin is running with emulationMode=nvme
	PublishCtxNvmeNsID = VendorPrefix + "nvmeNsID"
	// PublishCtxVirtioFsTag contains the tag of the VirtioFS volume, must be set when the plugin is running with emulationMode=virtiofs
	PublishCtxVirtioFsTag = VendorPrefix + "virtioFsTag"
)
