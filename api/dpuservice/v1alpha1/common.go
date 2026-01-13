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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const SvcDpuGroupName = "svc.dpu.nvidia.com"

// DPUServiceObject is an interface implemented by common DPUService* related objects enabling common functions in the
// reconcilers
// +kubebuilder:object:generate=false
type DPUServiceObject interface {
	client.Object
	// GetDPUClusterSelector returns the DPUCluster selector of the object
	GetDPUClusterSelector() *metav1.LabelSelector
}
