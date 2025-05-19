/*
Copyright 2024 NVIDIA

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

package controller

import (
	"cmp"
	"context"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"slices"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func generateVolumeAttachmentName(namespace, dpuNodeName, volumeID string) string {
	return generateID("pva-", namespace, dpuNodeName, volumeID)
}

// GenerateID generates unique predictable ID: prefix + FNV hash of the identifiers
func generateID(prefix string, identifiers ...string) string {
	hash := fnv.New128()
	for _, i := range identifiers {
		hash.Write([]byte(i))
	}
	return prefix + hex.EncodeToString(hash.Sum([]byte{}))
}

// kubernetes API doesn't allow to query CR by UID.
// getVolumeByID uses list operator with selector that filters by the UID field that is indexed in the informer.
// the function returns nil if the required CR not found.
func getVolumeByID(ctx context.Context, c client.Client, uid string) (*storagev1.Volume, error) {
	volumeList := &storagev1.VolumeList{}
	if err := c.List(ctx, volumeList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector("metadata.uid", uid)}); err != nil {
		return nil, fmt.Errorf("failed to list volumes in the API: %v", err)
	}
	if len(volumeList.Items) == 0 {
		return nil, nil
	}
	if string(volumeList.Items[0].GetUID()) != uid {
		// this should never happen
		return nil, fmt.Errorf("bug: getVolumeByID returned volume with wrong ID")
	}
	return &volumeList.Items[0], nil
}

// getDPUNameByNodeName lists DPU objects in the host cluster and checks the nodeName field to understand node-DPU mapping.
// Returns the name of the matching DPU object (it represents the nodeName of the DPU in the DPU Cluster).
// The nodeName field is indexed by the informer.
// If multiple DPUs are found for a single node, use the first one after sorting them lexically.
// returns empty string if the required CR not found.
func getDPUNameByNodeName(ctx context.Context, c client.Client, nodeName string) (string, error) {
	dpuList := &provisioningv1.DPUList{}
	if err := c.List(ctx, dpuList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector("spec.dpuNodeName", nodeName)}); err != nil {
		return "", fmt.Errorf("failed to list DPUs in the API: %v", err)
	}
	if len(dpuList.Items) == 0 {
		return "", nil
	}
	slices.SortFunc(dpuList.Items, func(a, b provisioningv1.DPU) int {
		return cmp.Compare(
			types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}.String(),
			types.NamespacedName{Name: b.GetName(), Namespace: b.GetNamespace()}.String(),
		)
	})
	if dpuList.Items[0].Spec.DPUNodeName != nodeName {
		// this should never happen
		return "", fmt.Errorf("bug: getDPUNameByNodeName returned DPU that doesn't math the node")
	}
	return dpuList.Items[0].Name, nil
}

// build volume context map from the volume object
func getVolumeCtx(vol *storagev1.Volume) map[string]string {
	volumeCtx := map[string]string{
		common.VolumeCtxStoragePolicyName:       vol.Spec.StorageParameters["policy"],
		common.VolumeCtxStorageVendorName:       vol.Spec.VolumeSpecDPU.StorageVendorName,
		common.VolumeCtxStorageVendorPluginName: vol.Spec.VolumeSpecDPU.StorageVendorPluginName,
	}
	for k, v := range vol.Spec.VolumeSpecDPU.VolumeAttributes {
		volumeCtx[k] = v
	}
	return volumeCtx
}

func convertCSIVolumeMode(volCaps []*csi.VolumeCapability) *corev1.PersistentVolumeMode {
	typeFS := corev1.PersistentVolumeFilesystem
	typeBlock := corev1.PersistentVolumeBlock
	for _, c := range volCaps {
		switch c.GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			return &typeBlock
		case *csi.VolumeCapability_Mount:
			return &typeFS
		}
	}
	return &typeBlock
}

// convert access modes from CSI request to DCM storageAPI access modes
func convertCSIAccessModesToStorageAPIAccessModes(volCaps []*csi.VolumeCapability) []corev1.PersistentVolumeAccessMode {
	accessMode := []corev1.PersistentVolumeAccessMode{}
	for _, c := range volCaps {
		switch c.AccessMode.Mode {
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			accessMode = append(accessMode, corev1.ReadWriteMany)
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			accessMode = append(accessMode, corev1.ReadOnlyMany)
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER, csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER:
			accessMode = append(accessMode, corev1.ReadWriteOnce)
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER:
			accessMode = append(accessMode, corev1.ReadWriteOncePod)
		}
	}
	return accessMode
}
