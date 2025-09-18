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

package volumeprovisioner

import (
	"context"
	"fmt"
	"strings"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ensureVolumeCRsInTargetClusters creates/updates Volume CRs across all target clusters
func (p *volumeProvisioner) ensureVolumeCRsInTargetClusters(ctx context.Context,
	targetClusters []dpuclusterhelper.ClientForDPUCluster,
	dpuVolume *storagev1.DPUVolume,
	volumeData *VolumeData) (internalResult, error) {
	reqLog := ctrllog.FromContext(ctx)
	var (
		errs            []error
		notReadyReasons []string
	)
	for _, cluster := range targetClusters {
		result, err := p.ensureVolumeCRInCluster(ctx, cluster, dpuVolume, volumeData)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to ensure Volume in cluster %s: %w",
				client.ObjectKeyFromObject(cluster.DPUCluster), err))
		}
		if !result.Ready {
			notReadyReasons = append(notReadyReasons, fmt.Sprintf("DPUCluster %s: %s", client.ObjectKeyFromObject(cluster.DPUCluster), result.Reason))
		}
	}
	if len(errs) > 0 {
		return internalResult{Ready: false}, kerrors.NewAggregate(errs)
	}
	if len(notReadyReasons) > 0 {
		return internalResult{Ready: false, Reason: strings.Join(notReadyReasons, ",")}, nil
	}
	reqLog.Info("Successfully ensured Volume CRs in all target clusters")
	return internalResult{Ready: true}, nil
}

// ensureVolumeCRInCluster creates/updates Volume CR in a single cluster
func (p *volumeProvisioner) ensureVolumeCRInCluster(ctx context.Context,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	dpuVolume *storagev1.DPUVolume,
	volumeData *VolumeData) (internalResult, error) {

	reqLog := ctrllog.FromContext(ctx).WithValues("volume", dpuVolume.Name, "dpuCluster", client.ObjectKeyFromObject(dpuClusterClient.DPUCluster))

	desiredVolume := p.buildDesiredVolumeCR(dpuVolume, volumeData)
	volumeKey := client.ObjectKeyFromObject(desiredVolume)
	expectedDPUVolumeRef := client.ObjectKeyFromObject(dpuVolume)

	existingVolume := &storagev1.Volume{}
	err := dpuClusterClient.Client.Get(ctx, volumeKey, existingVolume)
	if err != nil && !apierrors.IsNotFound(err) {
		reqLog.Error(err, "Failed to get Volume CR in DPU cluster")
		return internalResult{}, fmt.Errorf("failed to get Volume CR in DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		existingVolume, err = p.createVolumeInCluster(ctx, reqLog, dpuClusterClient, desiredVolume)
		if err != nil {
			return internalResult{}, err
		}
	} else {
		if !existingVolume.DeletionTimestamp.IsZero() {
			reqLog.Info("Volume CR is being deleted")
			return internalResult{Ready: false, Reason: fmt.Sprintf("volume CR %s is being deleted", volumeKey.Name)}, nil
		}
		if err := p.updateVolumeSpecInCluster(ctx, reqLog, dpuClusterClient, desiredVolume, existingVolume); err != nil {
			return internalResult{}, err
		}
	}
	if err := p.ensureVolumeOwnershipInCluster(ctx, reqLog, dpuClusterClient, existingVolume, expectedDPUVolumeRef); err != nil {
		return internalResult{}, err
	}
	if err := p.ensureVolumeStatusAvailable(ctx, reqLog, dpuClusterClient, existingVolume); err != nil {
		return internalResult{}, err
	}
	return internalResult{Ready: true}, nil
}

// createVolumeInCluster creates a new Volume CR in the DPU cluster
func (p *volumeProvisioner) createVolumeInCluster(ctx context.Context,
	reqLog logr.Logger,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	desiredVolume *storagev1.Volume) (*storagev1.Volume, error) {
	existingVolume := desiredVolume.DeepCopy()
	reqLog.Info("Creating Volume CR in DPU cluster")
	if err := dpuClusterClient.Client.Create(ctx, existingVolume); err != nil {
		reqLog.Error(err, "Failed to create Volume CR in DPU cluster")
		return nil, fmt.Errorf("failed to create Volume CR in DPU cluster: %w", err)
	}
	reqLog.Info("Successfully created Volume CR in DPU cluster")
	return existingVolume, nil
}

// updateVolumeSpecInCluster updates the Volume spec if it differs from desired
func (p *volumeProvisioner) updateVolumeSpecInCluster(ctx context.Context,
	reqLog logr.Logger,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	desiredVolume *storagev1.Volume,
	existingVolume *storagev1.Volume) error {
	// currently nothing is directly attached to the lifecycle of the Volume CR in the DPU cluster,
	// so it is safe to directly update it if needed
	if !equality.Semantic.DeepEqual(desiredVolume.Spec, existingVolume.Spec) {
		reqLog.Info("Volume spec does not match desired, updating")
		existingVolume.Spec = *desiredVolume.Spec.DeepCopy()
		if err := dpuClusterClient.Client.Update(ctx, existingVolume); err != nil {
			reqLog.Error(err, "Failed to update Volume CR in DPU cluster")
			return fmt.Errorf("failed to update Volume CR in DPU cluster: %w", err)
		}
		reqLog.Info("Successfully updated Volume CR spec in DPU cluster")
	}
	return nil
}

// ensureVolumeOwnershipInCluster ensures the Volume has the correct DPUVolume reference
func (p *volumeProvisioner) ensureVolumeOwnershipInCluster(ctx context.Context,
	reqLog logr.Logger,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	existingVolume *storagev1.Volume,
	expectedDPUVolumeRef client.ObjectKey) error {
	existingDpuVolumeRef, err := p.ownedByHelper.GetOwnedBy(existingVolume)
	if err != nil || existingDpuVolumeRef != expectedDPUVolumeRef {
		reqLog.Info("Volume CR has incorrect DPUVolume reference, update it")
		p.ownedByHelper.SetOwnedBy(existingVolume, expectedDPUVolumeRef)
		if err := dpuClusterClient.Client.Update(ctx, existingVolume); err != nil {
			reqLog.Error(err, "Failed to update Volume CR in DPU cluster")
			return fmt.Errorf("failed to update Volume CR in DPU cluster: %w", err)
		}
	}
	return nil
}

// ensureVolumeStatusAvailable ensures the Volume status is set to Available
func (p *volumeProvisioner) ensureVolumeStatusAvailable(ctx context.Context,
	reqLog logr.Logger,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	existingVolume *storagev1.Volume) error {
	if existingVolume.Status.State != storagev1.VolumeStateAvailable {
		reqLog.Info("Updating Volume status to Available")
		existingVolume.Status.State = storagev1.VolumeStateAvailable
		if err := dpuClusterClient.Client.Status().Update(ctx, existingVolume); err != nil {
			reqLog.Error(err, "Failed to update Volume CR status in DPU cluster")
			return fmt.Errorf("failed to update Volume status in DPU cluster: %w", err)
		}
		reqLog.Info("Successfully updated Volume CR status to Available")
	}
	return nil
}

// buildDesiredVolumeCR constructs Volume CR spec from DPUVolume and volume data
func (p *volumeProvisioner) buildDesiredVolumeCR(dpuVolume *storagev1.DPUVolume, volumeData *VolumeData) *storagev1.Volume {
	var capacityRange storagev1.CapacityRange
	if dpuVolume.Spec.Resources.Requests != nil {
		if storage, ok := dpuVolume.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			capacityRange.Request = storage
		}
	}
	if dpuVolume.Spec.Resources.Limits != nil {
		if storage, ok := dpuVolume.Spec.Resources.Limits[corev1.ResourceStorage]; ok {
			capacityRange.Limit = storage
		}
	}
	desired := &storagev1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuVolume.Name,
			Namespace: p.targetNamespace,
		},
		Spec: storagev1.VolumeSpec{
			StorageParameters: dpuVolume.Spec.Parameters,
			Request: storagev1.VolumeRequest{
				CapacityRange: capacityRange,
				AccessModes:   dpuVolume.Spec.AccessModes,
				VolumeMode:    dpuVolume.Spec.VolumeMode,
			},
			StoragePolicyRef: &storagev1.ObjectRef{
				Kind:       storagev1.StoragePolicyKind,
				APIVersion: storagev1.GroupVersion.String(),
				Name:       dpuVolume.Spec.DPUStoragePolicyName,
				Namespace:  p.targetNamespace,
			},
			StoragePolicyParameters: dpuVolume.Spec.Parameters,
			VolumeSpecDPU: storagev1.VolumeSpecDPU{
				ID:            dpuVolume.Name,
				AccessModes:   dpuVolume.Spec.AccessModes,
				ReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			},
		},
	}
	if dpuVolume.Status.State != nil {
		state := dpuVolume.Status.State
		if state.SelectedDPUStorageVendorName != nil {
			desired.Spec.VolumeSpecDPU.StorageVendorName = *state.SelectedDPUStorageVendorName
		}
		if state.StorageVendorPluginName != nil {
			desired.Spec.VolumeSpecDPU.StorageVendorPluginName = *state.StorageVendorPluginName
		}
		if state.CSIDriverName != nil {
			desired.Spec.VolumeSpecDPU.CSIReference.CSIDriverName = *state.CSIDriverName
		}
		if state.StorageClassName != nil {
			desired.Spec.VolumeSpecDPU.CSIReference.StorageClassName = *state.StorageClassName
		}
	}
	if volumeData != nil {
		desired.Spec.VolumeSpecDPU.CSIReference.PVCRef = &storagev1.ObjectRef{
			Name:      volumeData.PVCName,
			Namespace: volumeData.PVCNamespace,
		}
		if volumeData.VolumeAttributes != nil {
			desired.Spec.VolumeSpecDPU.VolumeAttributes = volumeData.VolumeAttributes
		}
		if volumeData.Capacity != nil {
			if storage, ok := volumeData.Capacity[corev1.ResourceStorage]; ok {
				desired.Spec.VolumeSpecDPU.Capacity = storage
			}
		}
	}
	p.ownedByHelper.SetOwnedBy(desired, client.ObjectKeyFromObject(dpuVolume))
	return desired
}
