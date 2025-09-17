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

package controllerattacher

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/resourcehelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	corestoragev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ControllerAttacher defines the interface for managing controller attachment operations
type ControllerAttacher interface {
	// ControllerAttach orchestrates the controller attach flow
	ControllerAttach(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
		dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpuVolume *storagev1.DPUVolume, dpu *provisioningv1.DPU) (ControllerAttachResult, error)
	// ControllerDetach deletes the SVVolumeAttachment in the DPU clusters.
	// It returns ControllerDetachResult indicating if detach operation was completed.
	ControllerDetach(ctx context.Context, dpuClustersClients []dpuclusterhelper.ClientForDPUCluster,
		dpuVolumeAttachmentKey client.ObjectKey) (ControllerDetachResult, error)
}

// controllerAttacher provides utilities for managing controller attachment operations
type controllerAttacher struct {
	targetNamespace string
	ownedByHelper   utils.OwnedByHelper
}

// NewControllerAttacher creates a new ControllerAttacher instance
func NewControllerAttacher(targetNamespace string, ownedByHelper utils.OwnedByHelper) ControllerAttacher {
	return &controllerAttacher{
		targetNamespace: targetNamespace,
		ownedByHelper:   ownedByHelper,
	}
}

// ControllerAttachResult represents the result of controller attach operations
type ControllerAttachResult struct {
	// Ready indicates if the SVVolumeAttachment is ready
	Ready bool
	// Reason contains the reason for the result
	Reason string
	// AttachmentMetadata contains the metadata of the attached volume, set only when the attachment is ready
	AttachmentMetadata map[string]string
}

// ControllerDetachResult represents the result of controller detach operations
type ControllerDetachResult struct {
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

// ControllerAttach orchestrates the controller attach flow
func (a *controllerAttacher) ControllerAttach(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpuVolume *storagev1.DPUVolume, dpu *provisioningv1.DPU) (ControllerAttachResult, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("dpuVolumeAttachment", dpuVolumeAttachment.Name,
		"dpuCluster", client.ObjectKeyFromObject(dpuClusterClient.DPUCluster))
	if dpuVolume.Status.State.CSIDriverName == nil {
		return ControllerAttachResult{}, fmt.Errorf("CSIDriverName is not set for DPUVolume %s", dpuVolume.Name)
	}
	csiDriverName := *dpuVolume.Status.State.CSIDriverName
	csiDriver, err := a.getCSIDriver(ctx, dpuClusterClient, csiDriverName)
	if err != nil {
		return ControllerAttachResult{}, err
	}
	attachRequired := a.isAttachRequired(csiDriver)
	if !attachRequired {
		reqLog.Info("CSI driver does not require attachment, skipping SVVolumeAttachment creation")
		return ControllerAttachResult{Ready: true}, nil
	}
	result, svVolumeAttachment, err := a.ensureSVVolumeAttachment(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
	if err != nil {
		reqLog.Error(err, "Failed to ensure SVVolumeAttachment")
		return ControllerAttachResult{}, err
	}
	if !result.Ready {
		return ControllerAttachResult{Ready: false, Reason: result.Reason}, nil
	}
	attached := a.isSVVolumeAttachmentAttached(svVolumeAttachment)
	if !attached {
		errorMsg := a.checkAttachErrorMessage(svVolumeAttachment)
		if errorMsg != "" {
			reqLog.Error(nil, "SVVolumeAttachment has attach error", "error", errorMsg)
		} else {
			reqLog.Info("SVVolumeAttachment is not attached yet")
		}
		return ControllerAttachResult{Ready: false, Reason: errorMsg}, nil
	}
	reqLog.Info("SVVolumeAttachment is attached")
	return ControllerAttachResult{Ready: true, AttachmentMetadata: svVolumeAttachment.Status.AttachmentMetadata}, nil
}

// ControllerDetach deletes the SVVolumeAttachment in the DPU clusters.
// It returns ControllerDetachResult indicating if detach operation was completed.
func (a *controllerAttacher) ControllerDetach(ctx context.Context,
	dpuClustersClients []dpuclusterhelper.ClientForDPUCluster, dpuVolumeAttachmentKey client.ObjectKey) (ControllerDetachResult, error) {
	deletionResult, err := resourcehelper.DeleteResourceInDPUClusters(ctx, dpuClustersClients, "SVVolumeAttachment",
		client.ObjectKey{Namespace: a.targetNamespace, Name: dpuVolumeAttachmentKey.Name}, &storagev1.SVVolumeAttachment{}, []string{})
	return ControllerDetachResult{Completed: deletionResult.Completed, Reason: deletionResult.Reason}, err
}

// getCSIDriver queries CSIDriver object from the DPU cluster
func (a *controllerAttacher) getCSIDriver(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster, csiDriverName string) (*corestoragev1.CSIDriver, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("csiDriver", csiDriverName)
	csiDriver := &corestoragev1.CSIDriver{}
	if err := dpuClusterClient.Client.Get(ctx, client.ObjectKey{Name: csiDriverName}, csiDriver); err != nil {
		reqLog.Error(err, "Failed to get CSIDriver")
		return nil, fmt.Errorf("failed to get CSIDriver %s: %w", csiDriverName, err)
	}
	reqLog.Info("Successfully retrieved CSIDriver")
	return csiDriver, nil
}

// isAttachRequired checks if the CSIDriver requires attachment
// Returns true if AttachRequired is nil or true
func (a *controllerAttacher) isAttachRequired(csiDriver *corestoragev1.CSIDriver) bool {
	return csiDriver.Spec.AttachRequired == nil || *csiDriver.Spec.AttachRequired
}

// isSVVolumeAttachmentAttached checks if the SVVolumeAttachment is attached and ready
// Returns true if the SVVolumeAttachment status indicates it is attached
func (a *controllerAttacher) isSVVolumeAttachmentAttached(svVolumeAttachment *storagev1.SVVolumeAttachment) bool {
	return svVolumeAttachment.Status.Attached
}

// ensureSVVolumeAttachment creates or ensures SVVolumeAttachment object in the DPU cluster
func (a *controllerAttacher) ensureSVVolumeAttachment(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpuVolume *storagev1.DPUVolume, dpu *provisioningv1.DPU) (internalResult, *storagev1.SVVolumeAttachment, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("svVolumeAttachment", dpuVolumeAttachment.Name)

	if dpuVolume.Status.State.VolumeInfo == nil ||
		dpuVolume.Status.State.VolumeInfo.VolumeName == nil ||
		*dpuVolume.Status.State.VolumeInfo.VolumeName == "" {
		return internalResult{}, nil, fmt.Errorf("VolumeInfo is not set for DPUVolume %s", dpuVolume.Name)
	}
	desiredSVVolumeAttachment := a.getDesiredSVVolumeAttachment(dpuVolumeAttachment, *dpuVolume.Status.State.VolumeInfo.VolumeName, dpu.Name)
	apiSVVolumeAttachment := &storagev1.SVVolumeAttachment{}
	svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: a.targetNamespace}
	if err := dpuClusterClient.Client.Get(ctx, svVolumeAttachmentKey, apiSVVolumeAttachment); err != nil {
		if !apierrors.IsNotFound(err) {
			reqLog.Error(err, "Failed to get SVVolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
		if err := dpuClusterClient.Client.Create(ctx, desiredSVVolumeAttachment); err != nil {
			reqLog.Error(err, "Failed to create SVVolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
		reqLog.Info("Successfully created SVVolumeAttachment in DPU cluster", "nodeName", dpuVolumeAttachment.Spec.DPUNodeName)
		return internalResult{Ready: true}, desiredSVVolumeAttachment, nil
	}
	if !apiSVVolumeAttachment.DeletionTimestamp.IsZero() {
		reqLog.Info("SVVolumeAttachment is being deleted, wait for it to be deleted")
		return internalResult{Ready: false, Reason: fmt.Sprintf("SVVolumeAttachment %s is being deleted", svVolumeAttachmentKey)}, nil, nil
	}
	if !equality.Semantic.DeepEqual(desiredSVVolumeAttachment.Spec, apiSVVolumeAttachment.Spec) {
		reqLog.Info("SVVolumeAttachment has incorrect spec, remove it")
		if err := dpuClusterClient.Client.Delete(ctx, apiSVVolumeAttachment); err != nil {
			reqLog.Error(err, "Failed to delete SVVolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
		return internalResult{Ready: false, Reason: fmt.Sprintf("SVVolumeAttachment %s is being deleted", svVolumeAttachmentKey)}, nil, nil
	}
	expectedDPUVolumeAttachmentRef := client.ObjectKeyFromObject(dpuVolumeAttachment)
	dpuVolumeAttachmentRef, err := a.ownedByHelper.GetOwnedBy(apiSVVolumeAttachment)
	if err != nil || dpuVolumeAttachmentRef != expectedDPUVolumeAttachmentRef {
		reqLog.Info("SVVolumeAttachment has incorrect DPUVolumeAttachment reference, update it")
		a.ownedByHelper.SetOwnedBy(apiSVVolumeAttachment, expectedDPUVolumeAttachmentRef)
		if err := dpuClusterClient.Client.Update(ctx, apiSVVolumeAttachment); err != nil {
			reqLog.Error(err, "Failed to update SVVolumeAttachment in DPU cluster")
			return internalResult{}, nil, err
		}
	}
	return internalResult{Ready: true}, apiSVVolumeAttachment, nil
}

// checkAttachErrorMessage checks for attach error message in SVVolumeAttachment status
// Returns the error message if AttachError is present, otherwise returns empty string
func (a *controllerAttacher) checkAttachErrorMessage(svVolumeAttachment *storagev1.SVVolumeAttachment) string {
	if svVolumeAttachment.Status.AttachError != nil {
		return svVolumeAttachment.Status.AttachError.Message
	}
	return ""
}

// getDesiredSVVolumeAttachment creates the desired SVVolumeAttachment object
func (a *controllerAttacher) getDesiredSVVolumeAttachment(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, volumeName string, nodeName string) *storagev1.SVVolumeAttachment {
	attachment := &storagev1.SVVolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuVolumeAttachment.Name,
			Namespace: a.targetNamespace,
		},
		Spec: corestoragev1.VolumeAttachmentSpec{
			NodeName: nodeName,
			Source: corestoragev1.VolumeAttachmentSource{
				PersistentVolumeName: ptr.To(volumeName),
			},
		},
	}
	a.ownedByHelper.SetOwnedBy(attachment, client.ObjectKeyFromObject(dpuVolumeAttachment))
	return attachment
}
