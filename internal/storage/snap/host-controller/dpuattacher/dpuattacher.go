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

package dpuattacher

import (
	"context"
	"fmt"
	"maps"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/resourcehelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// snapControllerFinalizer is the finalizer used by the snap controller to protect the VolumeAttachment from deletion,
	// it is not used anymore, but we need to make sure that it is removed from VolumeAttachment CR while we are removing the volume attachment
	snapControllerFinalizer = "storage.nvidia.com/attachment-protection"
)

// DPUAttacher provides utilities for managing attachment operations for DPUs
type DPUAttacher interface {
	// DPUAttach manages VolumeAttachment lifecycle and waits for DPU attachment completion
	DPUAttach(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
		dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpu *provisioningv1.DPU) (DPUAttachResult, error)
	// DPUDetach deletes VolumeAttachments across DPU clusters
	DPUDetach(ctx context.Context, dpuClustersClients []dpuclusterhelper.ClientForDPUCluster,
		dpuVolumeAttachmentKey client.ObjectKey) (DPUDetachResult, error)
}

// dpuAttacher provides utilities for managing attachment operations for DPUs
type dpuAttacher struct {
	targetNamespace string
	ownedByHelper   utils.OwnedByHelper
}

// NewDPUAttacher creates a new DPUAttacher instance
func NewDPUAttacher(targetNamespace string, ownedByHelper utils.OwnedByHelper) DPUAttacher {
	return &dpuAttacher{
		targetNamespace: targetNamespace,
		ownedByHelper:   ownedByHelper,
	}
}

// DPUAttachResult is the result of the DPUAttach function
type DPUAttachResult struct {
	// Ready indicates if the DPU is attached
	Ready bool
	// Reason contains the reason for the result
	Reason string
	// Data contains the data of the DPU attachment
	Data *DPUAttachData
}

// DPUAttachData contains the data of the DPU attachment
type DPUAttachData struct {
	// PCIAddress is the PCI address of the DPU
	PCIAddress string
	// FuncVUID is the VUID of the emulated function
	FuncVUID string
	// DeviceName is the name of the device that was created by the storage vendor plugin
	DeviceName string
	// NVMEAttrs is the NVME attributes of the DPU
	NVMEAttrs *storagev1.NVMEAttrs
	// VirtioFSAttrs is the VirtioFS attributes of the DPU
	VirtioFSAttrs *storagev1.VirtioFSAttrs
}

// DPUDetachResult is the result of the DPUDetach function
type DPUDetachResult struct {
	// Completed indicates if the detach operation was completed
	Completed bool
	// Reason contains the reason for the result
	Reason string
}

// internalResult represents internal operation results with readiness status
type internalResult struct {
	Ready  bool
	Reason string
}

// getDesiredVolumeAttachment creates the desired VolumeAttachment object for a DPUVolumeAttachment
func (a *dpuAttacher) getDesiredVolumeAttachment(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpu *provisioningv1.DPU) *storagev1.VolumeAttachment {
	attachment := &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuVolumeAttachment.Name,
			Namespace: a.targetNamespace,
		},
		Spec: storagev1.VolumeAttachmentSpec{
			NodeName: dpu.Name,
			Source: storagev1.VolumeSource{
				VolumeRef: &storagev1.ObjectRef{
					Name:      dpuVolumeAttachment.Spec.DPUVolumeName,
					Namespace: a.targetNamespace,
				},
			},
			Parameters:         maps.Clone(dpuVolumeAttachment.Status.AttachmentMetadata),
			FunctionTypeConfig: dpuVolumeAttachment.Spec.FunctionTypeConfig,
		},
	}
	a.ownedByHelper.SetOwnedBy(attachment, client.ObjectKeyFromObject(dpuVolumeAttachment))
	return attachment
}

// DPUAttach manages VolumeAttachment lifecycle and waits for DPU attachment completion
func (a *dpuAttacher) DPUAttach(ctx context.Context,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpu *provisioningv1.DPU) (DPUAttachResult, error) {
	reqLog := ctrllog.FromContext(ctx)
	result, apiVolumeAttachment, err := a.ensureVolumeAttachment(ctx, dpuClusterClient, dpuVolumeAttachment, dpu)
	if err != nil {
		return DPUAttachResult{}, err
	}
	if !result.Ready {
		return DPUAttachResult{Ready: false, Reason: result.Reason}, nil
	}
	apiVolumeAttachment, err = a.ensureVolumeAttachmentStatus(ctx, dpuClusterClient, apiVolumeAttachment)
	if err != nil {
		return DPUAttachResult{}, err
	}
	if !apiVolumeAttachment.Status.DPU.Attached {
		reqLog.Info("VolumeAttachment is not attached to the DPU, wait for it to be attached", "message", apiVolumeAttachment.Status.Message)
		return DPUAttachResult{Ready: false, Reason: apiVolumeAttachment.Status.Message}, nil
	}
	return DPUAttachResult{Ready: true, Data: a.getDPUAttachData(apiVolumeAttachment)}, nil
}

func (a *dpuAttacher) ensureVolumeAttachment(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpu *provisioningv1.DPU) (internalResult, *storagev1.VolumeAttachment, error) {
	desiredVolumeAttachment := a.getDesiredVolumeAttachment(dpuVolumeAttachment, dpu)
	volumeAttachmentKey := client.ObjectKeyFromObject(desiredVolumeAttachment)

	reqLog := ctrllog.FromContext(ctx).WithValues("volumeAttachment", volumeAttachmentKey)

	apiVolumeAttachment := &storagev1.VolumeAttachment{}
	if err := dpuClusterClient.Client.Get(ctx, volumeAttachmentKey, apiVolumeAttachment); err != nil {
		if !apierrors.IsNotFound(err) {
			reqLog.Error(err, "Failed to get VolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
		if err := dpuClusterClient.Client.Create(ctx, desiredVolumeAttachment); err != nil {
			reqLog.Error(err, "Failed to create VolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
		reqLog.Info("Successfully created VolumeAttachment in DPU cluster")
		return internalResult{Ready: true}, desiredVolumeAttachment, nil
	}
	if !apiVolumeAttachment.DeletionTimestamp.IsZero() {
		reqLog.Info("VolumeAttachment is being deleted, wait for it to be deleted")
		return internalResult{Ready: false, Reason: fmt.Sprintf("VolumeAttachment %s is being deleted", dpuVolumeAttachment.Name)}, nil, nil
	}
	if !equality.Semantic.DeepEqual(desiredVolumeAttachment.Spec, apiVolumeAttachment.Spec) {
		reqLog.Info("VolumeAttachment has incorrect spec, removing it for recreation")
		if err := dpuClusterClient.Client.Delete(ctx, apiVolumeAttachment); err != nil {
			reqLog.Error(err, "Failed to delete VolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
		return internalResult{Ready: false, Reason: fmt.Sprintf("VolumeAttachment %s has incorrect spec, removed it", dpuVolumeAttachment.Name)}, nil, nil
	}
	expectedDPUVolumeAttachmentRef := client.ObjectKeyFromObject(dpuVolumeAttachment)
	dpuVolumeAttachmentRef, err := a.ownedByHelper.GetOwnedBy(apiVolumeAttachment)
	if err != nil || dpuVolumeAttachmentRef != expectedDPUVolumeAttachmentRef {
		reqLog.Info("VolumeAttachment has incorrect DPUVolumeAttachment reference, update it")
		a.ownedByHelper.SetOwnedBy(apiVolumeAttachment, expectedDPUVolumeAttachmentRef)
		if err := dpuClusterClient.Client.Update(ctx, apiVolumeAttachment); err != nil {
			reqLog.Error(err, "Failed to update VolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
	}
	return internalResult{Ready: true}, apiVolumeAttachment, nil
}

func (a *dpuAttacher) ensureVolumeAttachmentStatus(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	volumeAttachment *storagev1.VolumeAttachment) (*storagev1.VolumeAttachment, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("volumeAttachment", client.ObjectKeyFromObject(volumeAttachment))
	if !volumeAttachment.Status.StorageAttached {
		volumeAttachment.Status.StorageAttached = true
		if err := dpuClusterClient.Client.Status().Update(ctx, volumeAttachment); err != nil {
			reqLog.Error(err, "Failed to update VolumeAttachment status in DPU cluster")
			return nil, err
		}
		reqLog.Info("Successfully updated VolumeAttachment status to StorageAttached")
	}
	return volumeAttachment, nil
}

// getDPUAttachData creates DPUAttachData based on the
// VolumeAttachment status from the DPU cluster.
func (a *dpuAttacher) getDPUAttachData(volumeAttachment *storagev1.VolumeAttachment) *DPUAttachData {
	data := DPUAttachData{}
	if volumeAttachment.Status.DPU.PCIDeviceAddress != "" {
		data.PCIAddress = volumeAttachment.Status.DPU.PCIDeviceAddress
	}
	if volumeAttachment.Status.DPU.FuncVUID != "" {
		data.FuncVUID = volumeAttachment.Status.DPU.FuncVUID
	}
	if volumeAttachment.Status.DPU.DeviceName != "" {
		data.DeviceName = volumeAttachment.Status.DPU.DeviceName
	}
	if volumeAttachment.Status.DPU.BdevAttrs.NVMeNsID > 0 || volumeAttachment.Status.DPU.BdevAttrs.NVMeUUID != "" {
		data.NVMEAttrs = &storagev1.NVMEAttrs{
			NamespaceID:   &volumeAttachment.Status.DPU.BdevAttrs.NVMeNsID,
			NamespaceUUID: &volumeAttachment.Status.DPU.BdevAttrs.NVMeUUID,
		}
	}
	if volumeAttachment.Status.DPU.FSdevAttrs.FilesystemTag != "" {
		data.VirtioFSAttrs = &storagev1.VirtioFSAttrs{
			FilesystemTag: &volumeAttachment.Status.DPU.FSdevAttrs.FilesystemTag,
		}
	}
	return &data
}

// DPUDetach deletes VolumeAttachments across DPU clusters
func (a *dpuAttacher) DPUDetach(ctx context.Context, dpuClustersClients []dpuclusterhelper.ClientForDPUCluster,
	dpuVolumeAttachmentKey client.ObjectKey) (DPUDetachResult, error) {
	result, err := resourcehelper.DeleteResourceInDPUClusters(ctx, dpuClustersClients, "VolumeAttachment",
		client.ObjectKey{Namespace: a.targetNamespace, Name: dpuVolumeAttachmentKey.Name}, &storagev1.VolumeAttachment{}, []string{snapControllerFinalizer})
	return DPUDetachResult{Completed: result.Completed, Reason: result.Reason}, err
}
