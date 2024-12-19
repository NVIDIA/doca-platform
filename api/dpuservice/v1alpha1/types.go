/*
COPYRIGHT 2024 NVIDIA

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

// Package v1alpha1 contains API Schema definitions for the sfc v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=svc.dpu.nvidia.com
package v1alpha1

// ObjectMeta holds metadata like labels and annotations.
type ObjectMeta struct {
	// Labels is a map of string keys and values.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations is a map of string keys and values.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}
