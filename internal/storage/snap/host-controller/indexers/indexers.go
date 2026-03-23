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

package indexers

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DPUSpecDPUNodeName is the field index key for DPU.Spec.DPUNodeName
	DPUSpecDPUNodeName = "spec.dpuNodeName"
	// DPUVolumeAttachmentSpecDPUVolumeName is the field index key for DPUVolumeAttachment.Spec.DPUVolumeName
	DPUVolumeAttachmentSpecDPUVolumeName = "spec.dpuVolumeName"
	// DPUVolumeAttachmentSpecDPUNodeName is the field index key for DPUVolumeAttachment.Spec.DPUNodeName
	DPUVolumeAttachmentSpecDPUNodeName = "spec.dpuNodeName"
	// DPUStorageVendorSpecStorageClassName is the field index key for DPUStorageVendor.Spec.StorageClassName
	DPUStorageVendorSpecStorageClassName = "spec.storageClassName"
	// DPUStoragePolicySpecDPUStorageVendors is the field index key for DPUStoragePolicy.Spec.DPUStorageVendors
	DPUStoragePolicySpecDPUStorageVendors = "spec.dpuStorageVendors"
	// DPUVolumeSpecDPUStoragePolicyName is the field index key for DPUVolume.Spec.DPUStoragePolicyName
	DPUVolumeSpecDPUStoragePolicyName = "spec.dpuStoragePolicyName"
	// DPUVolumeStatusStateSelectedDPUStorageVendorName is the field index key for DPUVolume.Status.State.SelectedDPUStorageVendorName
	DPUVolumeStatusStateSelectedDPUStorageVendorName = "status.state.selectedDPUStorageVendorName"
	// DPUVolumeStatusStateVolumeInfoVolumeName is the field index key for DPUVolume.Status.State.VolumeInfo.VolumeName
	DPUVolumeStatusStateVolumeInfoVolumeName = "status.state.volumeInfo.volumeName"
)

// setupDPUSpecDPUNodeNameIndexer sets up indexer for DPU objects by spec.dpuNodeName
func setupDPUSpecDPUNodeNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPU{}, DPUSpecDPUNodeName, func(o client.Object) []string {
		d, ok := o.(*provisioningv1.DPU)
		if !ok {
			return nil
		}
		return []string{d.Spec.DPUNodeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPU CR spec.dpuNodeName field: %w", err)
	}
	return nil
}

// setupDPUVolumeAttachmentSpecDPUVolumeNameIndexer sets up indexer for DPUVolumeAttachment objects by spec.dpuVolumeName
func setupDPUVolumeAttachmentSpecDPUVolumeNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUVolumeAttachment{}, DPUVolumeAttachmentSpecDPUVolumeName, func(o client.Object) []string {
		dpuVolumeAttachment, ok := o.(*storagev1.DPUVolumeAttachment)
		if !ok {
			return nil
		}
		return []string{dpuVolumeAttachment.Spec.DPUVolumeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUVolumeAttachment CR spec.dpuVolumeName field: %w", err)
	}
	return nil
}

// setupDPUVolumeAttachmentSpecDPUNodeNameIndexer sets up indexer for DPUVolumeAttachment objects by spec.dpuNodeName
func setupDPUVolumeAttachmentSpecDPUNodeNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUVolumeAttachment{}, DPUVolumeAttachmentSpecDPUNodeName, func(o client.Object) []string {
		dpuVolumeAttachment, ok := o.(*storagev1.DPUVolumeAttachment)
		if !ok {
			return nil
		}
		return []string{dpuVolumeAttachment.Spec.DPUNodeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUVolumeAttachment CR spec.dpuNodeName field: %w", err)
	}
	return nil
}

// setupDPUStorageVendorSpecStorageClassNameIndexer sets up indexer for DPUStorageVendor objects by spec.storageClassName
func setupDPUStorageVendorSpecStorageClassNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUStorageVendor{}, DPUStorageVendorSpecStorageClassName, func(o client.Object) []string {
		dpuStorageVendor, ok := o.(*storagev1.DPUStorageVendor)
		if !ok {
			return nil
		}
		return []string{dpuStorageVendor.Spec.StorageClassName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUStorageVendor CR spec.storageClassName field: %w", err)
	}
	return nil
}

// setupDPUStoragePolicySpecDPUStorageVendorsIndexer sets up indexer for DPUStoragePolicy objects by spec.dpuStorageVendors
func setupDPUStoragePolicySpecDPUStorageVendorsIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUStoragePolicy{}, DPUStoragePolicySpecDPUStorageVendors, func(o client.Object) []string {
		dpuStoragePolicy, ok := o.(*storagev1.DPUStoragePolicy)
		if !ok {
			return nil
		}
		// Return the vendor names directly; the indexer supports multi-valued keys
		return dpuStoragePolicy.Spec.DPUStorageVendors
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUStoragePolicy CR spec.dpuStorageVendors field: %w", err)
	}
	return nil
}

// setupDPUVolumeSpecDPUStoragePolicyNameIndexer sets up indexer for DPUVolume objects by spec.dpuStoragePolicyName
func setupDPUVolumeSpecDPUStoragePolicyNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUVolume{}, DPUVolumeSpecDPUStoragePolicyName, func(o client.Object) []string {
		dpuVolume, ok := o.(*storagev1.DPUVolume)
		if !ok {
			return nil
		}
		return []string{dpuVolume.Spec.DPUStoragePolicyName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUVolume CR spec.dpuStoragePolicyName field: %w", err)
	}
	return nil
}

// setupDPUVolumeStatusStateSelectedDPUStorageVendorNameIndexer sets up indexer for DPUVolume objects by selectedDPUStorageVendorName
func setupDPUVolumeStatusStateSelectedDPUStorageVendorNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUVolume{}, DPUVolumeStatusStateSelectedDPUStorageVendorName, func(o client.Object) []string {
		dpuVolume, ok := o.(*storagev1.DPUVolume)
		if !ok {
			return nil
		}
		if dpuVolume.Status.State == nil ||
			dpuVolume.Status.State.SelectedDPUStorageVendorName == nil ||
			*dpuVolume.Status.State.SelectedDPUStorageVendorName == "" {
			return nil
		}
		return []string{*dpuVolume.Status.State.SelectedDPUStorageVendorName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUVolume CR status.state.selectedDPUStorageVendorName field: %w", err)
	}
	return nil
}

// setupDPUVolumeStatusStateVolumeInfoVolumeNameIndexer sets up indexer for DPUVolume objects by status.state.volumeInfo.volumeName
func setupDPUVolumeStatusStateVolumeInfoVolumeNameIndexer(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUVolume{}, DPUVolumeStatusStateVolumeInfoVolumeName, func(o client.Object) []string {
		dpuVolume, ok := o.(*storagev1.DPUVolume)
		if !ok {
			return nil
		}
		if dpuVolume.Status.State == nil ||
			dpuVolume.Status.State.VolumeInfo == nil ||
			dpuVolume.Status.State.VolumeInfo.VolumeName == nil ||
			*dpuVolume.Status.State.VolumeInfo.VolumeName == "" {
			return nil
		}
		return []string{*dpuVolume.Status.State.VolumeInfo.VolumeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUVolume CR status.state.volumeInfo.volumeName field: %w", err)
	}
	return nil
}

// SetupIndexers initializes all field indexers required by the storage host controllers
func SetupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	indexers := []func(context.Context, ctrl.Manager) error{
		setupDPUSpecDPUNodeNameIndexer,
		setupDPUVolumeAttachmentSpecDPUVolumeNameIndexer,
		setupDPUVolumeAttachmentSpecDPUNodeNameIndexer,
		setupDPUStorageVendorSpecStorageClassNameIndexer,
		setupDPUStoragePolicySpecDPUStorageVendorsIndexer,
		setupDPUVolumeSpecDPUStoragePolicyNameIndexer,
		setupDPUVolumeStatusStateSelectedDPUStorageVendorNameIndexer,
		setupDPUVolumeStatusStateVolumeInfoVolumeNameIndexer,
	}
	for _, setupIndexer := range indexers {
		if err := setupIndexer(ctx, mgr); err != nil {
			return err
		}
	}
	return nil
}
