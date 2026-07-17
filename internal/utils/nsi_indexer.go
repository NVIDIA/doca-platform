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

package utils

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// NSIObjectsNamespace is the DPF-owned namespace where all NodeServiceInterfaces objects live.
	NSIObjectsNamespace = "dpf-operator-system"

	// NSINodeFieldKey is the cache field-index key for NodeServiceInterfaces.spec.node.
	NSINodeFieldKey = "spec.node"

	// NSITypeFieldKey is the cache field-index key for NodeServiceInterfaces.spec.type.
	NSITypeFieldKey = "spec.type"
)

// NSINodeIndexFunc extracts the node name from a NodeServiceInterfaces object for field indexing.
func NSINodeIndexFunc(o client.Object) []string {
	return []string{o.(*dpuservicev1.NodeServiceInterfaces).Spec.Node}
}

// NSITypeIndexFunc extracts the type from a NodeServiceInterfaces object for field indexing.
func NSITypeIndexFunc(o client.Object) []string {
	return []string{o.(*dpuservicev1.NodeServiceInterfaces).Spec.Type}
}

func ServiceInterfaceNodeIndexFunc(o client.Object) []string {
	si := o.(*dpuservicev1.ServiceInterface)
	if si.Spec.Node == nil {
		return nil
	}
	return []string{*si.Spec.Node}
}

// SetupNSINodeIndexer registers the spec.node and spec.type field indexes for NodeServiceInterfaces.
func SetupNSINodeIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&dpuservicev1.NodeServiceInterfaces{},
		NSINodeFieldKey,
		NSINodeIndexFunc,
	); err != nil {
		return fmt.Errorf("register NSI spec.node index: %w", err)
	}
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&dpuservicev1.NodeServiceInterfaces{},
		NSITypeFieldKey,
		NSITypeIndexFunc,
	); err != nil {
		return fmt.Errorf("register NSI spec.type index: %w", err)
	}
	return nil
}

func SetupServiceInterfaceNodeIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&dpuservicev1.ServiceInterface{},
		ServiceInterfaceNodeFieldKey,
		ServiceInterfaceNodeIndexFunc,
	); err != nil {
		return fmt.Errorf("register ServiceInterface spec.node index: %w", err)
	}
	return nil
}
