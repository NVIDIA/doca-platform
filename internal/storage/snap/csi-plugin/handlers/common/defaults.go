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
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
)

const (
	// DefaultMaxVolumesPerNode defines the fallback value for maximum number of volumes that can be published to a node.
	DefaultMaxVolumesPerNode = 30
)

const (
	// DefaultFunctionType defines the default attachment function type for the volume.
	DefaultFunctionType = storagev1.FunctionTypeVF
	// DefaultHotplugFunction defines the default attachment hotplug function for the volume.
	DefaultHotplugFunction = false
)
