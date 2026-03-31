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

package hostagent

// This file has no //go:build linux constraint so that controller-gen can parse the RBAC markers
// on all platforms (e.g. macOS). All other files in this package are linux-only.

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes;dpudevices,verbs=create;delete;get;list;watch;patch;update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bfbs;dpuclusters;dpudiscoveries;dpus;dpusets,verbs=get;list;patch;update;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status;dpunodes/status;dpudevices/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuflavors,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=nodes;services,verbs=get;list;patch;watch
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests,verbs=create;get;list;watch
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
