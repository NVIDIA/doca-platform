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
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoverDPUVPCs returns a tree of objects representing the DPUVPC status.
func DiscoverDPUVPCs(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, _ func(map[string]string) bool) (*ObjectTree, error) {
	if err := addDPUVPCs(ctx, scope, dpfOperatorConfig); err != nil {
		return nil, err
	}
	return tree, nil
}

// addDPUVPCs adds DPUVPCs and its related objects to the objectScope tree.
func addDPUVPCs(ctx context.Context, o objectScope, root client.Object) error {
	if !showResource(o.opts, vpcv1.DPUVPCKind) {
		return nil
	}

	dpuVPCs, err := listDPUVPCs(ctx, o.client)
	if err != nil {
		return err
	}

	isolationClassMap, err := listIsolationClassByDPUVPC(ctx, o.client)
	if err != nil {
		return err
	}
	dpuVirtualNetworkMap, err := listDPUVirtualNetworksByDPUVPC(ctx, o.client)
	if err != nil {
		return err
	}
	dpuServiceInterfaceMap, err := listDPUServiceInterfaceByDPUVirtualNetwork(ctx, o.client)
	if err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuVPC := range dpuVPCs {
		if !isObjDebug(dpuVPC, o.opts.ShowResources) {
			continue
		}

		dpuVPC.TypeMeta = metav1.TypeMeta{
			Kind:       vpcv1.DPUVPCKind,
			APIVersion: vpcv1.GroupVersion.String(),
		}
		addToTree = append(addToTree, dpuVPC)

		// Add the IsolationClass referenced by this DPUVPC
		addIsolationClassForDPUVPC(o, dpuVPC, isolationClassMap)

		// Add DPUVirtualNetworks that belong to this DPUVPC
		addDPUVirtualNetworksForDPUVPC(o, dpuVPC, dpuVirtualNetworkMap[client.ObjectKeyFromObject(dpuVPC)], dpuServiceInterfaceMap)
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUVPCs")
	return nil
}

// addIsolationClassForDPUVPC adds the IsolationClass referenced by the given DPUVPC to the objectScope tree.
func addIsolationClassForDPUVPC(o objectScope, dpuVPC *vpcv1.DPUVPC, isolationClassMap map[string]*vpcv1.IsolationClass) {
	if !showResource(o.opts, vpcv1.IsolationClassKind) {
		return
	}

	isolationClass, found := isolationClassMap[dpuVPC.Spec.IsolationClassName]
	if !found {
		return
	}

	// Create a virtual object with a Ready condition
	virtIsolationClass := VirtualObjectForVisualization(isolationClass, vpcv1.IsolationClassKind)
	virtIsolationClass.Object["status"] = map[string]interface{}{
		"conditions": []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: isolationClass.GetCreationTimestamp(),
				Reason:             "Available",
				Message:            fmt.Sprintf("Provisioner: %s", isolationClass.Spec.Provisioner),
			},
		},
	}

	o.tree.Add(dpuVPC, virtIsolationClass, ZOrder(1))
}

// addDPUVirtualNetworksForDPUVPC adds DPUVirtualNetworks that belong to the given DPUVPC to the objectScope tree.
func addDPUVirtualNetworksForDPUVPC(
	o objectScope,
	dpuVPC *vpcv1.DPUVPC,
	dpuVirtualNetworks []*vpcv1.DPUVirtualNetwork,
	dpuServiceInterfaceMap map[client.ObjectKey][]*dpuservicev1.DPUServiceInterface,
) {
	if !showResource(o.opts, vpcv1.DPUVirtualNetworkKind) {
		return
	}

	addToTree := []client.Object{}
	for _, dpuVirtualNetwork := range dpuVirtualNetworks {
		if !isObjDebug(dpuVirtualNetwork, o.opts.ShowResources) {
			continue
		}
		addToTree = append(addToTree, dpuVirtualNetwork)

		// Add DPUServiceInterfaces that reference this DPUVirtualNetwork
		addDPUServiceInterfacesForVirtualNetwork(o, dpuVirtualNetwork, dpuServiceInterfaceMap[client.ObjectKeyFromObject(dpuVirtualNetwork)])
	}

	for _, vn := range addToTree {
		o.tree.Add(dpuVPC, vn)
	}
}

// addDPUServiceInterfacesForVirtualNetwork adds DPUServiceInterfaces that reference the given DPUVirtualNetwork to the objectScope tree.
func addDPUServiceInterfacesForVirtualNetwork(
	o objectScope,
	dpuVirtualNetwork *vpcv1.DPUVirtualNetwork,
	dpuServiceInterfaces []*dpuservicev1.DPUServiceInterface,
) {
	if !showResource(o.opts, dpuservicev1.DPUServiceInterfaceKind) {
		return
	}

	addToTree := []client.Object{}
	for _, dpuServiceInterface := range dpuServiceInterfaces {
		if !isObjDebug(dpuServiceInterface, o.opts.ShowResources) {
			continue
		}
		addToTree = append(addToTree, dpuServiceInterface)
	}

	for _, si := range addToTree {
		o.tree.Add(dpuVirtualNetwork, si)
	}
}

// listIsolationClassByDPUVPC returns a map of IsolationClass objects keyed by their name.
// GVK is set for IsolationClass objects.
func listIsolationClassByDPUVPC(ctx context.Context, c client.Client) (map[string]*vpcv1.IsolationClass, error) {
	isolationClassList := &vpcv1.IsolationClassList{}
	if err := c.List(ctx, isolationClassList); err != nil {
		return nil, fmt.Errorf("failed to list IsolationClass objects: %w", err)
	}

	isolationClassMap := make(map[string]*vpcv1.IsolationClass)
	for _, isolationClass := range isolationClassList.Items {
		// set GVK
		isolationClass.SetGroupVersionKind(vpcv1.GroupVersion.WithKind(vpcv1.IsolationClassKind))
		isolationClassMap[isolationClass.Name] = &isolationClass
	}

	return isolationClassMap, nil
}

// listDPUVirtualNetworksByDPUVPC returns a map of DPUVirtualNetwork objects keyed by their DPUVPC object key.
// GVK is set for DPUVirtualNetwork objects.
func listDPUVirtualNetworksByDPUVPC(ctx context.Context, c client.Client) (map[client.ObjectKey][]*vpcv1.DPUVirtualNetwork, error) {
	dpuVirtualNetworkList := &vpcv1.DPUVirtualNetworkList{}
	if err := c.List(ctx, dpuVirtualNetworkList); err != nil {
		return nil, fmt.Errorf("failed to list DPUVirtualNetwork objects: %w", err)
	}

	dpuVirtualNetworkMap := make(map[client.ObjectKey][]*vpcv1.DPUVirtualNetwork)
	for _, dpuVirtualNetwork := range dpuVirtualNetworkList.Items {
		vpcObjKey := client.ObjectKey{
			Namespace: dpuVirtualNetwork.Namespace,
			Name:      dpuVirtualNetwork.Spec.VPCName,
		}
		// set GVK
		dpuVirtualNetwork.SetGroupVersionKind(vpcv1.GroupVersion.WithKind(vpcv1.DPUVirtualNetworkKind))
		dpuVirtualNetworkMap[vpcObjKey] = append(dpuVirtualNetworkMap[vpcObjKey], &dpuVirtualNetwork)
	}

	return dpuVirtualNetworkMap, nil
}

// listDPUServiceInterfaceByDPUVirtualNetwork returns a map of DPUServiceInterface objects keyed by their DPUVirtualNetwork object key.
// GVK is set for DPUServiceInterface objects.
func listDPUServiceInterfaceByDPUVirtualNetwork(ctx context.Context, c client.Client) (map[client.ObjectKey][]*dpuservicev1.DPUServiceInterface, error) {
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	if err := c.List(ctx, dpuServiceInterfaceList); err != nil {
		return nil, fmt.Errorf("failed to list DPUServiceInterface objects: %w", err)
	}

	dpuServiceInterfaceMap := make(map[client.ObjectKey][]*dpuservicev1.DPUServiceInterface)
	for _, dpuServiceInterface := range dpuServiceInterfaceList.Items {
		if dpuServiceInterface.GetVirtualNetworkName() == "" {
			continue
		}

		vnObjKey := client.ObjectKey{
			Namespace: dpuServiceInterface.Namespace,
			Name:      dpuServiceInterface.GetVirtualNetworkName(),
		}
		// set GVK
		dpuServiceInterface.SetGroupVersionKind(dpuservicev1.GroupVersion.WithKind(dpuservicev1.DPUServiceInterfaceKind))
		dpuServiceInterfaceMap[vnObjKey] = append(dpuServiceInterfaceMap[vnObjKey], &dpuServiceInterface)
	}

	return dpuServiceInterfaceMap, nil
}

// listDPUVPCs returns a list of DPUVPC objects adding GVK to each object.
func listDPUVPCs(ctx context.Context, c client.Client) ([]*vpcv1.DPUVPC, error) {
	dpuVPCList := &vpcv1.DPUVPCList{}
	if err := c.List(ctx, dpuVPCList); err != nil {
		return nil, fmt.Errorf("failed to list DPUVPC objects: %w", err)
	}

	dpuVPCs := []*vpcv1.DPUVPC{}
	for _, dpuVPC := range dpuVPCList.Items {
		dpuVPC.SetGroupVersionKind(vpcv1.GroupVersion.WithKind(vpcv1.DPUVPCKind))
		dpuVPCs = append(dpuVPCs, &dpuVPC)
	}

	return dpuVPCs, nil
}
