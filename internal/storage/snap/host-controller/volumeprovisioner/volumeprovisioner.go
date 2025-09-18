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

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/resourcehelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// snapControllerLegacyFinalizer is the finalizer used by the snap controller to protect the volume from deletion,
	// it is not used anymore, but we need to make sure that it is removed from Volume CR while we are removing the volume
	snapControllerLegacyFinalizer = "storage.nvidia.com/volume-protection"
)

// ProvisionResult represents the result of a volume provisioning operation
type ProvisionResult struct {
	Ready  bool        `json:"ready"`
	Reason string      `json:"reason"`
	Data   *VolumeData `json:"data"`
}

// VolumeData contains volume information from PVC and PV for updating DPUVolume CR
type VolumeData struct {
	// PVC information
	PVCName      string `json:"pvcName"`
	PVCNamespace string `json:"pvcNamespace"`

	// PV information from DPU cluster
	VolumeName       string                              `json:"volumeName"`
	Capacity         corev1.ResourceList                 `json:"capacity"`
	AccessModes      []corev1.PersistentVolumeAccessMode `json:"accessModes"`
	VolumeMode       *corev1.PersistentVolumeMode        `json:"volumeMode"`
	VolumeAttributes map[string]string                   `json:"volumeAttributes"`
}

// RemoveResult represents the result of a volume removal operation
type RemoveResult struct {
	Completed bool   `json:"completed"`
	Reason    string `json:"reason"`
}

// VolumeProvisioner manages volume provisioning and removal across DPU clusters
type VolumeProvisioner interface {
	Provision(ctx context.Context,
		clientForPrimary dpuclusterhelper.ClientForDPUCluster,
		targetClusters []dpuclusterhelper.ClientForDPUCluster,
		dpuVolume *storagev1.DPUVolume) (ProvisionResult, error)
	Remove(ctx context.Context,
		targetClusters []dpuclusterhelper.ClientForDPUCluster,
		dpuVolumeKey client.ObjectKey) (RemoveResult, error)
}

// New creates a new VolumeProvisioner instance
func New(targetNamespace string, ownedByHelper utils.OwnedByHelper) VolumeProvisioner {
	return &volumeProvisioner{
		targetNamespace: targetNamespace,
		ownedByHelper:   ownedByHelper,
	}
}

// internalResult represents internal operation results with readiness status
type internalResult struct {
	Ready  bool
	Reason string
}

// volumeProvisioner implements VolumeProvisioner interface for managing volumes
type volumeProvisioner struct {
	targetNamespace string
	ownedByHelper   utils.OwnedByHelper
}

// Provision creates PVC in primary cluster and Volume CRs in target clusters
func (p *volumeProvisioner) Provision(ctx context.Context, clientForPrimary dpuclusterhelper.ClientForDPUCluster,
	targetClusters []dpuclusterhelper.ClientForDPUCluster,
	dpuVolume *storagev1.DPUVolume) (ProvisionResult, error) {

	reqLog := ctrllog.FromContext(ctx).WithValues("dpuVolume", client.ObjectKeyFromObject(dpuVolume))
	provisionResult, err := p.provisionVolume(ctx, clientForPrimary, dpuVolume)
	if err != nil {
		return ProvisionResult{Ready: false}, err
	}
	if !provisionResult.Ready {
		return ProvisionResult{Ready: false, Reason: provisionResult.Reason}, nil
	}
	volumeCrResult, err := p.ensureVolumeCRsInTargetClusters(ctx, targetClusters, dpuVolume, provisionResult.Data)
	if err != nil {
		return ProvisionResult{Ready: false}, err
	}
	if !volumeCrResult.Ready {
		return ProvisionResult{Ready: false, Reason: volumeCrResult.Reason}, nil
	}
	reqLog.Info("successfully provisioned volume", "result", provisionResult)
	return provisionResult, nil
}

// Remove deletes Volume CRs and PVCs from target clusters
func (p *volumeProvisioner) Remove(ctx context.Context,
	targetClusters []dpuclusterhelper.ClientForDPUCluster,
	dpuVolumeKey client.ObjectKey) (RemoveResult, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("dpuVolume", dpuVolumeKey)
	reqLog.Info("removing volume")

	volumeCRsDeletionResult, err := resourcehelper.DeleteResourceInDPUClusters(ctx, targetClusters, storagev1.VolumeKind,
		client.ObjectKey{Namespace: p.targetNamespace, Name: dpuVolumeKey.Name},
		&storagev1.Volume{}, []string{snapControllerLegacyFinalizer})
	if err != nil {
		return RemoveResult{Completed: false}, err
	}
	if !volumeCRsDeletionResult.Completed {
		reqLog.Info("volume CRs in removal state", "result", volumeCRsDeletionResult)
		return RemoveResult{Completed: false, Reason: volumeCRsDeletionResult.Reason}, nil
	}

	pvcsDeletionResult, err := resourcehelper.DeleteResourceInDPUClusters(ctx, targetClusters, "PersistentVolumeClaim",
		client.ObjectKey{Namespace: p.targetNamespace, Name: p.getPVCName(dpuVolumeKey)},
		&corev1.PersistentVolumeClaim{}, []string{})
	if err != nil {
		return RemoveResult{Completed: false}, err
	}
	if !pvcsDeletionResult.Completed {
		reqLog.Info("PVCs in removal state", "result", pvcsDeletionResult)
		return RemoveResult{Completed: false, Reason: pvcsDeletionResult.Reason}, nil
	}
	reqLog.Info("successfully removed volume")
	return RemoveResult{Completed: true}, nil
}
