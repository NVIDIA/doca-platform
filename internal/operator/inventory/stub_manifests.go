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
	"context"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StubComponent is a type for testing GenerateManifests and ApplySet behavior.
type StubComponent struct {
	objs []*unstructured.Unstructured
	name operatorv1.ComponentName
}

func StubComponentWithObjs(name operatorv1.ComponentName, objs []*unstructured.Unstructured) StubComponent {
	return StubComponent{
		name: name,
		objs: objs,
	}
}

func (s StubComponent) Name() operatorv1.ComponentName {
	return s.name
}

func (s StubComponent) Parse() error {
	return nil
}

func (s StubComponent) GenerateManifests(_ context.Context, vars Variables) ([]client.Object, error) {
	ret := []client.Object{}
	for _, obj := range s.objs {
		ret = append(ret, obj)
	}
	return ret, nil
}

func (s StubComponent) IsReadyForUpgrade(context.Context, client.Client, *operatorv1.DPFOperatorConfig) error {
	return nil
}

func (s StubComponent) IsReady(context.Context, client.Client, string) error {
	return nil
}
