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

package dpfctl

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type unstructuredWrapper struct {
	*unstructured.Unstructured
}

func unstructuredGetSet(u *unstructured.Unstructured) *unstructuredWrapper {
	return &unstructuredWrapper{Unstructured: u}
}

// GetConditions gets the conditions from unstructured object.
//
// NOTE: Due to the constraints of JSON-unmarshal, this operation is to be considered best effort.
// In more details:
//   - Errors during JSON-unmarshal are ignored and a empty collection list is returned.
//   - It's not possible to detect if the object has an empty condition list or if it does not implement conditions;
//     in both cases the operation returns an empty slice is returned.
//   - If the object doesn't implement conditions on under status as defined in DPF,
//     JSON-unmarshal matches incoming object keys to the keys; this can lead to conditions values partially set.
//
// Adopted from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/conditions/unstructured.go#L53
func (c *unstructuredWrapper) GetConditions() []metav1.Condition {
	conditions := []metav1.Condition{}
	if err := UnstructuredUnmarshalField(c.Unstructured, &conditions, "status", "conditions"); err != nil {
		return nil
	}
	return conditions
}

// SetConditions sets the conditions from unstructured object.
// Adopted from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/conditions/unstructured.go#L68
func (c *unstructuredWrapper) SetConditions(conditions []metav1.Condition) {
	v := make([]interface{}, 0, len(conditions))
	for i := range conditions {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&conditions[i])
		if err != nil {
			log.Log.Error(err, "Failed to convert Condition to unstructured map. This error shouldn't have occurred, please file an issue.", "groupVersionKind", c.GroupVersionKind(), "name", c.GetName(), "namespace", c.GetNamespace())
			continue
		}
		v = append(v, m)
	}
	// unstructured.SetNestedField returns an error only if value cannot be set because one of
	// the nesting levels is not a map[string]interface{}; this is not the case so the error should never happen here.
	err := unstructured.SetNestedField(c.Unstructured.Object, v, "status", "conditions")
	if err != nil {
		log.Log.Error(err, "Failed to set Conditions on unstructured object. This error shouldn't have occurred, please file an issue.", "groupVersionKind", c.GroupVersionKind(), "name", c.GetName(), "namespace", c.GetNamespace())
	}
}
