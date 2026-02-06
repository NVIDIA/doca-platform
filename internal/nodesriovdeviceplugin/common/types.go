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

package common

import (
	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
)

// NodeInputConfig is the per-node input configuration passed to the init container.
// Key is the DPU serial number, value is the list of device plugin resources.
type NodeInputConfig map[string][]noderesourcesv1.DevicePluginResource
