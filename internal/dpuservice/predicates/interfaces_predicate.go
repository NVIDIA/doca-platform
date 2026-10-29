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

package predicates

import (
	"fmt"
	"reflect"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	ErrUnstructuredFieldNotFound = fmt.Errorf("field not found")
)

// DPUServiceInterfaceChangePredicate detects changes to a dpuservicev1.DPUServiceInterface object.
type DPUServiceInterfaceChangePredicate struct {
	predicate.Funcs
}

func (DPUServiceInterfaceChangePredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldInterface, ok := e.ObjectOld.(*dpuservicev1.DPUServiceInterface)
	if !ok {
		return false
	}

	newInterface, ok := e.ObjectNew.(*dpuservicev1.DPUServiceInterface)
	if !ok {
		return false
	}

	if oldInterface.Spec.GetTemplateSpec().GetTemplateSpec().Service == nil && newInterface.Spec.GetTemplateSpec().GetTemplateSpec().Service != nil {
		return true
	}

	oldService := oldInterface.Spec.GetTemplateSpec().GetTemplateSpec().Service
	newService := newInterface.Spec.GetTemplateSpec().GetTemplateSpec().Service
	if oldService != nil && newService != nil && !reflect.DeepEqual(oldService, newService) {
		return true
	}

	return false
}

func (DPUServiceInterfaceChangePredicate) Create(e event.CreateEvent) bool {
	return false
}

func (DPUServiceInterfaceChangePredicate) Delete(e event.DeleteEvent) bool {
	return false
}
