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
	// dpuServiceDeployInClusterField is the field indexer for DPUService resources by deployment location (true for in cluster, false for DPU)
	dpuServiceDeployInClusterField = "spec.deployInCluster"
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

	// Set up the index for DPUService lookups by deployInCluster
	if err := mgr.GetFieldIndexer().IndexField(ctx, &dpuservicev1.DPUService{}, dpuServiceDeployInClusterField, func(obj client.Object) []string {
		dpuService := obj.(*dpuservicev1.DPUService)
		return []string{strconv.FormatBool(ptr.Deref(dpuService.Spec.DeployInCluster, false))}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUService CR: %w", err)
	}

	return nil
}
