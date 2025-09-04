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

package utils

import (
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OwnedByHelper is the interface for setting and getting owned by references.
type OwnedByHelper interface {
	// SetOwnedBy sets the owned by annotation on the given object.
	SetOwnedBy(obj client.Object, owner client.ObjectKey)
	// GetOwnedBy gets the owned by annotation from the given object.
	GetOwnedBy(obj client.Object) (client.ObjectKey, error)
}

// ownedBy implements the OwnedByHelper interface.
type ownedBy struct {
	annotationKey string
}

// New creates a new OwnedByHelper with the given annotation key.
func New(ownedByAnnotation string) OwnedByHelper {
	return &ownedBy{
		annotationKey: ownedByAnnotation,
	}
}

// SetOwnedBy sets the owned by annotation on the given object.
func (h *ownedBy) SetOwnedBy(obj client.Object, owner client.ObjectKey) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[h.annotationKey] = owner.String()
	obj.SetAnnotations(annotations)
}

// GetOwnedBy gets the owned by annotation from the given object.
// Returns the client.ObjectKey, or an error if not found or if the format is invalid.
func (h *ownedBy) GetOwnedBy(obj client.Object) (client.ObjectKey, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return client.ObjectKey{}, fmt.Errorf("annotations not set on object")
	}
	val, ok := annotations[h.annotationKey]
	if !ok || val == "" {
		return client.ObjectKey{}, fmt.Errorf("owned by annotation not found")
	}
	// Expecting format: "namespace/name", or "/name" for cluster-scoped resources.
	parts := strings.Split(val, "/")
	if len(parts) != 2 || parts[1] == "" {
		return client.ObjectKey{}, fmt.Errorf("invalid owned by annotation format: %q", val)
	}
	return client.ObjectKey{Namespace: parts[0], Name: parts[1]}, nil
}
