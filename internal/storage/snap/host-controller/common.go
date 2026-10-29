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

package hostcontroller

import (
	"context"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	requeueIntervalOnDpuClusterNotConnected time.Duration = time.Second * 5
	// dpuNodeNameIndexKey is the field index key for DPU.Spec.DPUNodeName
	dpuNodeNameIndexKey = "spec.dpuNodeName"
	// dpuVolumeNameIndexKey is the field index key for DPUVolumeAttachment.Spec.DPUVolumeName
	dpuVolumeNameIndexKey = "spec.dpuVolumeName"
)

// Options holds common options for controllers
type Options struct {
	// indicated namespace in the host cluster in which controller runs
	Namespace string
	// namespace in the DPU cluster to create Volume and VolumeAttachment objects
	TargetNamespace string
	// DPU cluster to create Volume and VolumeAttachment objects
	DPUCluster types.NamespacedName
}

// cleanupOrphanedObject deletes orphaned objects from the DPU cluster.
// Called when a DPU* object is not found in the host cluster to cleanup the corresponding object in the DPU cluster.

//nolint:unparam
func cleanupOrphanedObject(ctx context.Context, dpuClusterClient client.Client, objectNamespacedName types.NamespacedName, obj client.Object, objectTypeName string) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Checking for orphaned object in DPU cluster", "type", objectTypeName)

	err := dpuClusterClient.Get(ctx, objectNamespacedName, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found in DPU cluster, nothing to clean up
			reqLog.Info("No orphaned object found in DPU cluster", "type", objectTypeName, "name", objectNamespacedName.String())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get %s from DPU cluster: %w", objectTypeName, err)
	}

	// Check if object is already being deleted
	if !obj.GetDeletionTimestamp().IsZero() {
		reqLog.Info("Object in the DPU cluster is already deleting", "type", objectTypeName, "name", objectNamespacedName.String())
		return ctrl.Result{}, nil
	}

	// Object exists, delete it
	reqLog.Info("Deleting orphaned object from DPU cluster", "type", objectTypeName, "name", objectNamespacedName.String())
	err = dpuClusterClient.Delete(ctx, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete orphaned %s from DPU cluster: %w", objectTypeName, err)
	}

	reqLog.Info("Successfully cleaned up orphaned object", "type", objectTypeName, "name", objectNamespacedName.String())
	return ctrl.Result{}, nil
}

// SetupIndexers initializes all field indexers required by the storage host controllers
func SetupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	// Index DPU objects by spec.dpuNodeName
	if err := mgr.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPU{}, dpuNodeNameIndexKey, func(o client.Object) []string {
		d, ok := o.(*provisioningv1.DPU)
		if !ok {
			return nil
		}
		return []string{d.Spec.DPUNodeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPU CR: %w", err)
	}

	// Index DPUVolumeAttachment objects by spec.dpuVolumeName
	if err := mgr.GetFieldIndexer().IndexField(ctx, &storagev1.DPUVolumeAttachment{}, dpuVolumeNameIndexKey, func(o client.Object) []string {
		dpuVolumeAttachment, ok := o.(*storagev1.DPUVolumeAttachment)
		if !ok {
			return nil
		}
		return []string{dpuVolumeAttachment.Spec.DPUVolumeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPUVolumeAttachment CR: %w", err)
	}

	return nil
}

// ConvertDPUSelectionAlgorithmToStorageSelectionAlg converts from DPUStoragePolicy SelectionAlgorithm to StoragePolicy StorageSelectionAlgType
func ConvertDPUSelectionAlgorithmToStorageSelectionAlg(dpuAlgorithm *storagev1.SelectionAlgorithm) storagev1.StorageSelectionAlgType {
	if dpuAlgorithm == nil {
		// Return default value when nil
		return storagev1.LocalNVolumes
	}
	switch *dpuAlgorithm {
	case storagev1.SelectionAlgorithmRandom:
		return storagev1.Random
	case storagev1.SelectionAlgorithmNumberVolumes:
		return storagev1.LocalNVolumes
	default:
		// Return default value for unknown algorithms
		return storagev1.LocalNVolumes
	}
}

// ConvertStorageSelectionAlgToDPUSelectionAlgorithm converts from StoragePolicy StorageSelectionAlgType to DPUStoragePolicy SelectionAlgorithm
func ConvertStorageSelectionAlgToDPUSelectionAlgorithm(storageAlg storagev1.StorageSelectionAlgType) *storagev1.SelectionAlgorithm {
	switch storageAlg {
	case storagev1.Random:
		return ptr.To(storagev1.SelectionAlgorithmRandom)
	case storagev1.LocalNVolumes:
		return ptr.To(storagev1.SelectionAlgorithmNumberVolumes)
	default:
		// Return default value for unknown algorithms
		return ptr.To(storagev1.SelectionAlgorithmNumberVolumes)
	}
}
