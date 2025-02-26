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
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// CharSet defines the alphanumeric set for random string generation.
	// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/util.go#L56
	CharSet = "0123456789abcdefghijklmnopqrstuvwxyz"
)

var (
	// ErrUnstructuredFieldNotFound determines that a field
	// in an unstructured object could not be found.
	// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/util.go#L67
	ErrUnstructuredFieldNotFound = fmt.Errorf("field not found")

	// Copy from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/util.go#L60
	rnd = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
)

// RandomString returns a random alphanumeric string.
// Copied and private from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/util.go#L72
func randomString(n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = CharSet[rnd.Intn(len(CharSet))]
	}
	return string(result)
}

// UnstructuredUnmarshalField is a wrapper around JSON and Unstructured objects to decode and copy a specific field
// value into an object.
// Copied and private from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/util/util.go#L395
func UnstructuredUnmarshalField(obj *unstructured.Unstructured, v interface{}, fields ...string) error {
	if obj == nil || obj.Object == nil {
		return fmt.Errorf("failed to unmarshal unstructured object: object is nil")
	}

	value, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if err != nil {
		return fmt.Errorf("retrieve field %q from %q: %v", strings.Join(fields, "."), obj.GroupVersionKind(), err)
	}
	if !found || value == nil {
		return ErrUnstructuredFieldNotFound
	}
	valueBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("json-encode field %q value from %q: %v", strings.Join(fields, "."), obj.GroupVersionKind(), err)
	}
	if err := json.Unmarshal(valueBytes, v); err != nil {
		return fmt.Errorf("json-decode field %q value from %q: %v", strings.Join(fields, "."), obj.GroupVersionKind(), err)
	}
	return nil
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/util.go#L78
func getReadyCondition(obj client.Object) *metav1.Condition {
	getter := objToGetSet(obj)
	if getter == nil {
		return nil
	}
	return conditions.Get(getter, conditions.TypeReady)
}

// GetOtherConditions returns the other conditions (all the conditions except ready) for an object, if defined.
// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/util.go#L104
func GetOtherConditions(obj client.Object) []*metav1.Condition {
	getter := objToGetSet(obj)
	if getter == nil {
		return nil
	}
	var conds []*metav1.Condition
	for _, c := range getter.GetConditions() {
		if c.Type != string(conditions.TypeReady) {
			conds = append(conds, &c)
		}
	}
	return conds
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/util.go#L139
func setReadyCondition(obj client.Object, ready *metav1.Condition) {
	setter := objToGetSet(obj)
	if setter == nil {
		return
	}
	setter.SetConditions([]metav1.Condition{*ready})
}

// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/util.go#L147
func objToGetSet(obj client.Object) conditions.GetSet {
	if getter, ok := obj.(conditions.GetSet); ok {
		return getter
	}

	objUnstructured, ok := obj.(*unstructured.Unstructured)
	if !ok {
		if err := scheme.Scheme.Convert(obj, &objUnstructured, nil); err != nil {
			return nil
		}
	}
	getter := unstructuredGetSet(objUnstructured)
	return getter
}

// VirtualObject returns a new virtual object.
// Adoption from: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.9/cmd/clusterctl/client/tree/util.go#L174
func VirtualObject(namespace, kind, name string) *unstructured.Unstructured {
	gk := "virtual.dpu.nvidia.com/v1alpha1"
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gk,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
				"annotations": map[string]interface{}{
					VirtualObjectAnnotation: "True",
				},
				"uid": fmt.Sprintf("%s, Kind=%s%s, %s/%s", gk, kind, randomString(10), namespace, name),
			},
			"status": map[string]interface{}{
				"conditions": []metav1.Condition{},
			},
		},
	}
}

func ensurePlural(kind string) string {
	if strings.HasSuffix(kind, "s") {
		return kind
	}
	return kind + "s"
}
