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

package controllers

import (
	"context"
	"fmt"
	"strconv"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// dpuNodeNameField is the field indexer for DPU resources by DPUNode name
	dpuNodeNameField = "spec.dpuNodeName"
	// dpuNodeMaintenanceDPUNodeNameField is the field indexer for DPUNodeMaintenance resources by DPUNode name
	dpuNodeMaintenanceDPUNodeNameField = "spec.dpuNodeName"
	// dpuNodeKubeNodeRefField is the field indexer for DPUNode resources by KubeNodeRef
	dpuNodeKubeNodeRefField = "status.kubeNodeRef"
	// dpuServiceDeployInClusterField is the field indexer for DPUService resources by deployment location (true for in cluster, false for DPU)
	dpuServiceDeployInClusterField = "spec.deployInCluster"
	// dpuServiceConfigPortsField is the field indexer for DPUService resources by whether ConfigPorts is set
	dpuServiceConfigPortsField = "spec.configPorts"
	// dpuServiceInterfacesField is the field indexer for DPUService resources by interfaces
	dpuServiceInterfacesField = ".metadata.interfaces"
)

// SetupIndexers initializes all field indexers required by the DPU service controllers
func SetupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	// Set up the index for DPU lookups by dpuNodeName
	if err := mgr.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPU{}, dpuNodeNameField, func(obj client.Object) []string {
		dpu := obj.(*provisioningv1.DPU)
		return []string{dpu.Spec.DPUNodeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPU CR: %w", err)
	}

	// Set up the index for DPUNodeMaintenance lookups by dpuNodeName
	if err := mgr.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPUNodeMaintenance{}, dpuNodeMaintenanceDPUNodeNameField, func(obj client.Object) []string {
		dpuNodeMaintenance := obj.(*provisioningv1.DPUNodeMaintenance)
		return []string{dpuNodeMaintenance.Spec.DPUNodeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUNodeMaintenance CR: %w", err)
	}

	// Set up the index for DPUNode lookups by KubeNodeRef
	if err := mgr.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPUNode{}, dpuNodeKubeNodeRefField, func(obj client.Object) []string {
		dpuNode := obj.(*provisioningv1.DPUNode)
		if dpuNode.Status.KubeNodeRef == nil {
			return nil
		}
		return []string{*dpuNode.Status.KubeNodeRef}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUNode KubeNodeRef: %w", err)
	}

	// Set up the index for DPUService lookups by deployInCluster
	if err := mgr.GetFieldIndexer().IndexField(ctx, &dpuservicev1.DPUService{}, dpuServiceDeployInClusterField, func(obj client.Object) []string {
		dpuService := obj.(*dpuservicev1.DPUService)
		return []string{strconv.FormatBool(ptr.Deref(dpuService.Spec.DeployInCluster, false))}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUService CR: %w", err)
	}

	// Set up the index for DPUService lookups by interfaces
	if err := mgr.GetFieldIndexer().IndexField(ctx, &dpuservicev1.DPUService{}, dpuServiceInterfacesField, func(obj client.Object) []string {
		dpuService := obj.(*dpuservicev1.DPUService)
		namespacedNames, err := getInterfaceNamespacedNames(dpuService)
		if err != nil {
			return nil
		}
		return namespacedNames
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUService Interfaces field: %w", err)
	}

	// Set up the index for DPUService lookups by whether ConfigPorts is set
	if err := mgr.GetFieldIndexer().IndexField(ctx, &dpuservicev1.DPUService{}, dpuServiceConfigPortsField, func(obj client.Object) []string {
		dpuService := obj.(*dpuservicev1.DPUService)
		return []string{strconv.FormatBool(dpuService.Spec.ConfigPorts != nil)}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUService ConfigPorts field: %w", err)
	}

	return nil
}
