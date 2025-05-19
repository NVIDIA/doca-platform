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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StubComponent is a type for testing GenerateManifests and ApplySet behavior.
type StubComponent struct {
	objs []*unstructured.Unstructured
	name string
}

func StubComponentWithObjs(name string, objs []*unstructured.Unstructured) StubComponent {
	return StubComponent{
		name: name,
		objs: objs,
	}
}

func (s StubComponent) Name() string {
	return s.name
}

func (s StubComponent) Parse() error {
	return nil
}

func (s StubComponent) GenerateManifests(vars Variables, _ ...GenerateManifestOption) ([]client.Object, error) {
	ret := []client.Object{}
	if len(s.objs) > 0 {
		ret = append(ret, applySetParentForComponent(s, ApplySetID(vars.Namespace, s), vars, applySetInventoryString(s.objs...)))
	}
	for _, obj := range s.objs {
		ret = append(ret, obj)
	}
	return ret, nil
}

func (s StubComponent) IsReady(context.Context, client.Client, string) error {
	return nil
}
