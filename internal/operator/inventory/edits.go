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

package inventory

import (
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnstructuredEdit edits Unstructured in place, returns error if occurred
type UnstructuredEdit func(un *unstructured.Unstructured) error

// StructuredEdit edits Structured object in place, returns error if occurred
type StructuredEdit func(obj client.Object) error

// Edits facilitates editing kubernetes objects
type Edits struct {
	allEdits      []UnstructuredEdit
	perKindEdits  map[ObjectKind][]UnstructuredEdit
	perKindSEdits map[ObjectKind][]StructuredEdit
}

// NewEdits creates new edit.
func NewEdits() *Edits {
	return &Edits{
		perKindEdits:  make(map[ObjectKind][]UnstructuredEdit),
		perKindSEdits: make(map[ObjectKind][]StructuredEdit),
	}
}

// AddForAll addds UnstructuredEdits that will be called on all objects during Apply call
func (e *Edits) AddForAll(edits ...UnstructuredEdit) *Edits {
	e.allEdits = append(e.allEdits, edits...)
	return e
}

// AddForKind adds UnstructuredEdits that will be called on all objects of the specified kind during Apply call
func (e *Edits) AddForKind(kind ObjectKind, edits ...UnstructuredEdit) *Edits {
	e.perKindEdits[kind] = append(e.perKindEdits[kind], edits...)
	return e
}

// AddForKindS addds StructuredEdits that will be called on objects with the specified type during Apply call
// objects are converted to concrete type before calling UnstructuredEdit
func (e *Edits) AddForKindS(kind ObjectKind, edits ...StructuredEdit) *Edits {
	e.perKindSEdits[kind] = append(e.perKindSEdits[kind], edits...)
	return e
}

// Apply applies in place Edits for objs, returns first error if occurred
func (e *Edits) Apply(objs []*unstructured.Unstructured) error {
	if err := e.applyAllEdits(objs); err != nil {
		return fmt.Errorf("Edits failed to applyAllEdits. %w", err)
	}

	if err := e.applyPerKindEdits(objs); err != nil {
		return fmt.Errorf("Edits failed to applyPerKindEdits. %w", err)
	}

	if err := e.applyPerKindSEdits(objs); err != nil {
		return fmt.Errorf("Edits failed to applyPerTypeEdits. %w", err)
	}

	return nil
}

func (e *Edits) applyAllEdits(objs []*unstructured.Unstructured) error {
	for i := range objs {
		for _, edit := range e.allEdits {
			if err := edit(objs[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Edits) applyPerKindEdits(objs []*unstructured.Unstructured) error {
	for i := range objs {
		if edits, ok := e.perKindEdits[ObjectKind(objs[i].GetKind())]; ok {
			for _, edit := range edits {
				if err := edit(objs[i]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *Edits) applyPerKindSEdits(objs []*unstructured.Unstructured) error {
	for i := range objs {
		edits, ok := e.perKindSEdits[ObjectKind(objs[i].GetKind())]
		// no edits
		if !ok {
			continue
		}

		// convert to concrete type
		concrete, err := e.toConcreteType(objs[i])
		if err != nil {
			return err
		}

		if concrete == nil {
			// NOTE(adrianc): we should actually panic here
			return fmt.Errorf("missing conversion for %s", objs[i].GetKind())
		}

		// run edits
		for _, edit := range edits {
			if err := edit(concrete); err != nil {
				return err
			}
		}

		// convert back to unstructured
		uns, err := runtime.DefaultUnstructuredConverter.ToUnstructured(concrete)
		if err != nil {
			return fmt.Errorf("failed to convert to unstructured. %w", err)
		}
		// update in-place
		objs[i].Object = uns
	}

	return nil
}

func (e *Edits) toConcreteType(obj *unstructured.Unstructured) (client.Object, error) {
	// add more object conversion as needed
	switch ObjectKind(obj.GetKind()) {
	case DeploymentKind:
		concrete := &appsv1.Deployment{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), concrete)
		if err != nil {
			return nil, fmt.Errorf("error while converting data to objects: %w", err)
		}
		return concrete, nil
	case StatefulSetKind:
		concrete := &appsv1.StatefulSet{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), concrete)
		if err != nil {
			return nil, fmt.Errorf("error while converting data to objects: %w", err)
		}
		return concrete, nil
	case DPUServiceKind:
		concrete := &dpuservicev1.DPUService{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), concrete)
		if err != nil {
			return nil, fmt.Errorf("error while converting data to objects: %w", err)
		}
		return concrete, nil
	case DaemonSetKind:
		concrete := &appsv1.DaemonSet{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), concrete)
		if err != nil {
			return nil, fmt.Errorf("error while converting data to objects: %w", err)
		}
		return concrete, nil
	}
	return nil, nil
}
