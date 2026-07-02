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

package dpuflavortemplate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
)

// hashPrefixLen is the number of hex characters kept from a sha256 sum when used as a
// short label value.
const hashPrefixLen = 12

// Hash returns a short, stable hash of the render inputs: the template body plus the
// two structured resource-fitting fields. A change to any of these means the generated
// DPUFlavor may differ and drift detection must re-evaluate.
func Hash(spec provisioningv1.DPUFlavorTemplateSpec) (string, error) {
	res, err := canonical(spec.DPUResources)
	if err != nil {
		return "", err
	}
	sys, err := canonical(spec.SystemReservedResources)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(spec.Template + "\x00" + res + "\x00" + sys))
	return fmt.Sprintf("%x", sum)[:hashPrefixLen], nil
}

// ValuesHash returns a short, stable hash of DPUDevice.spec.values.
func ValuesHash(values *runtime.RawExtension) (string, error) {
	data, err := decodeValues(values)
	if err != nil {
		return "", err
	}
	c, err := canonical(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(c))
	return fmt.Sprintf("%x", sum)[:hashPrefixLen], nil
}

// canonical marshals v to JSON. Go's json encoder sorts map keys, giving a stable
// representation for maps such as ResourceList and decoded values.
func canonical(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize value: %w", err)
	}
	return string(b), nil
}
