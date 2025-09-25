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

package controllers

import provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

// GetServiceVersionKeyToBFBVersionValue returns a map that defines the supported version matching in DPUDeployment
// Controller. In addition, it returns a boolean to indicate if the values are set correctly or are dummy.
// The key is the annotation we expect the chart author to define in the Chart.yaml -> annotations
// The value is the version found in a BFB object given as input that corresponds to the aforementioned key
func GetServiceVersionKeyToBFBVersionValue() map[string]func(*provisioningv1.BFB) string {
	return map[string]func(*provisioningv1.BFB) string{
		"dpu.nvidia.com/doca-version": func(bfb *provisioningv1.BFB) string { return bfb.Status.Versions.DOCA },
	}
}
