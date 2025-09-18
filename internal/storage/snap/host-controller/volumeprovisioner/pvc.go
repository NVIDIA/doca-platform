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
	"encoding/hex"
	"fmt"
	"hash/fnv"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// provisionVolume creates PVC in primary cluster and waits for PV creation
func (p *volumeProvisioner) provisionVolume(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster, dpuVolume *storagev1.DPUVolume) (ProvisionResult, error) {
	ensurePVCResult, pvc, err := p.ensurePVCExist(ctx, dpuClusterClient, dpuVolume)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !ensurePVCResult.Ready {
		return ProvisionResult{Ready: false, Reason: ensurePVCResult.Reason}, nil
	}
	waitForPVResult, pv, err := p.waitForPVCreation(ctx, dpuClusterClient, pvc)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !waitForPVResult.Ready {
		return ProvisionResult{Ready: false, Reason: waitForPVResult.Reason}, nil
	}
	volumeData := p.getVolumeData(pvc, pv)
	return ProvisionResult{Ready: true, Data: volumeData}, nil
}

// getPVCName generates a name for PVC from DPUVolume name
func (p *volumeProvisioner) getPVCName(dpuVolumeKey client.ObjectKey) string {
	// we need to add the suffix to be compatible with the old implementation
	suffix := "-pvc"
	name := dpuVolumeKey.Name + suffix
	if len(name) > validation.DNS1123SubdomainMaxLength {
		// If the raw name is too long, use a FNV hash to create a shorter, deterministic name
		hash := fnv.New128()
		hash.Write([]byte(dpuVolumeKey.Name))
		name = hex.EncodeToString(hash.Sum([]byte{})) + suffix
	}
	return name
}

// getDesiredPVC constructs PVC spec from DPUVolume configuration
func (p *volumeProvisioner) getDesiredPVC(dpuVolume *storagev1.DPUVolume) *corev1.PersistentVolumeClaim {
	desiredPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.getPVCName(client.ObjectKeyFromObject(dpuVolume)),
			Namespace: p.targetNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: dpuVolume.Status.State.StorageClassName,
			VolumeMode:       dpuVolume.Spec.VolumeMode,
			AccessModes:      dpuVolume.Spec.AccessModes,
			Resources:        dpuVolume.Spec.Resources,
		},
	}
	p.ownedByHelper.SetOwnedBy(desiredPVC, client.ObjectKeyFromObject(dpuVolume))
	return desiredPVC
}

// ensurePVCExist creates/validates PVC in DPU cluster, returns readiness status
func (p *volumeProvisioner) ensurePVCExist(ctx context.Context,
	dpuClusterClient dpuclusterhelper.ClientForDPUCluster, dpuVolume *storagev1.DPUVolume) (internalResult, *corev1.PersistentVolumeClaim, error) {
	desiredPVC := p.getDesiredPVC(dpuVolume)
	pvcKey := client.ObjectKeyFromObject(desiredPVC)

	reqLog := ctrllog.FromContext(ctx).WithValues("pvc", pvcKey,
		"dpuCluster", client.ObjectKeyFromObject(dpuClusterClient.DPUCluster))

	apiPVC := &corev1.PersistentVolumeClaim{}
	if err := dpuClusterClient.Client.Get(ctx, pvcKey, apiPVC); err != nil {
		if !apierrors.IsNotFound(err) {
			reqLog.Error(err, "Failed to get PVC in DPU cluster")
			return internalResult{}, nil, err
		}
		// PVC does not exist, create it
		apiPVC = desiredPVC
		if err := dpuClusterClient.Client.Create(ctx, apiPVC); err != nil {
			reqLog.Error(err, "Failed to create PVC in DPU cluster")
			return internalResult{}, nil, err
		}
		reqLog.Info("Successfully created PVC in DPU cluster")
		return internalResult{Ready: true}, apiPVC, nil
	}
	if !apiPVC.DeletionTimestamp.IsZero() {
		reqLog.Info("PVC is being deleted, wait for it to be deleted")
		return internalResult{Ready: false, Reason: fmt.Sprintf("PVC %s is being deleted", pvcKey.String())}, nil, nil
	}
	if !p.comparePVC(desiredPVC, apiPVC) {
		reqLog.Info("PVC has incorrect spec, remove it")
		if err := dpuClusterClient.Client.Delete(ctx, apiPVC); err != nil {
			reqLog.Error(err, "Failed to delete PVC in DPU cluster")
			return internalResult{}, nil, err
		}
		return internalResult{Ready: false, Reason: fmt.Sprintf("PVC %s has incorrect spec, removed it", pvcKey.String())}, nil, nil
	}
	expectedDPUVolumeRef := client.ObjectKeyFromObject(dpuVolume)
	dpuVolumeRef, err := p.ownedByHelper.GetOwnedBy(apiPVC)
	if err != nil || dpuVolumeRef != expectedDPUVolumeRef {
		reqLog.Info("PVC has incorrect DPUVolume reference, update it")
		p.ownedByHelper.SetOwnedBy(apiPVC, expectedDPUVolumeRef)
		if err := dpuClusterClient.Client.Update(ctx, apiPVC); err != nil {
			reqLog.Error(err, "Failed to update PVC in DPU cluster")
			return internalResult{}, nil, err
		}
	}
	return internalResult{Ready: true}, apiPVC, nil
}

// waitForPVCreation waits for PVC to bind and retrieves corresponding PV
func (p *volumeProvisioner) waitForPVCreation(ctx context.Context, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	pvc *corev1.PersistentVolumeClaim) (internalResult, *corev1.PersistentVolume, error) {
	pvcKey := client.ObjectKeyFromObject(pvc)
	reqLog := ctrllog.FromContext(ctx).WithValues("pvc", pvcKey, "dpuCluster", client.ObjectKeyFromObject(dpuClusterClient.DPUCluster))

	if pvc.Status.Phase != corev1.ClaimBound {
		reqLog.Info("PVC is not bound, wait for it to be bound")
		return internalResult{Ready: false, Reason: fmt.Sprintf("PVC %s is not bound", pvcKey.String())}, nil, nil
	}
	if pvc.Spec.VolumeName == "" {
		err := fmt.Errorf("PVC is bound but has no volume name")
		reqLog.Error(err, "PVC object is missing required field")
		return internalResult{}, nil, err
	}
	pv := &corev1.PersistentVolume{}
	if err := dpuClusterClient.Client.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, pv); err != nil {
		reqLog.Error(err, "Failed to get PV", "pv", pvc.Spec.VolumeName)
		return internalResult{}, nil, err
	}
	return internalResult{Ready: true}, pv, nil
}

// comparePVC compares PVC specs for equality on DPUVolume-relevant fields
func (p *volumeProvisioner) comparePVC(pvc1 *corev1.PersistentVolumeClaim, pvc2 *corev1.PersistentVolumeClaim) bool {
	return equality.Semantic.DeepEqual(pvc1.Spec.StorageClassName, pvc2.Spec.StorageClassName) &&
		equality.Semantic.DeepEqual(pvc1.Spec.VolumeMode, pvc2.Spec.VolumeMode) &&
		equality.Semantic.DeepEqual(pvc1.Spec.AccessModes, pvc2.Spec.AccessModes) &&
		equality.Semantic.DeepEqual(pvc1.Spec.Resources, pvc2.Spec.Resources)
}

// getVolumeData extracts volume information from PVC and PV for Volume CRs
func (p *volumeProvisioner) getVolumeData(pvc *corev1.PersistentVolumeClaim, pv *corev1.PersistentVolume) *VolumeData {
	volumeData := &VolumeData{
		PVCName:      pvc.Name,
		PVCNamespace: pvc.Namespace,
		VolumeName:   pv.Name,
		Capacity:     pv.Spec.Capacity,
		AccessModes:  pv.Spec.AccessModes,
		VolumeMode:   pv.Spec.VolumeMode,
	}
	if pv.Spec.PersistentVolumeSource.CSI != nil {
		volumeData.VolumeAttributes = pv.Spec.PersistentVolumeSource.CSI.VolumeAttributes
	}
	return volumeData
}
