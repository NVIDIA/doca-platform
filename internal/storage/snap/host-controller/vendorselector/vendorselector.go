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

package vendorselector

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"sort"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corestoragev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Options contains configuration for the vendor selector
type Options struct {
	// Namespace in the host cluster where the controller runs
	Namespace string
}

// Result is the result of the vendor selection operation
type Result struct {
	// Scheduled indicates if the DPUVolume vendor is selected
	Selected bool
	// Reason is the reason for the vendor selection operation, set only if Selected is false
	Reason string
	// SelectedVendorInfo contains information about the selected vendor
	SelectedVendorInfo *SelectedVendorInfo
}

// SelectedVendorInfo contains information about the selected vendor
type SelectedVendorInfo struct {
	// DPUClusterName is the name of the DPUCluster where the vendor is available and where the volume will be created
	DPUClusterName string
	// DPUClusterNamespace is the namespace of the DPUCluster where the vendor is available and where the volume will be created
	DPUClusterNamespace string
	// SelectedDPUStorageVendorName is the name of the selected vendor
	SelectedDPUStorageVendorName string
	// StorageVendorPluginName is the plugin name of the selected vendor
	StorageVendorPluginName string
	// StorageClassName is the storage class name from the selected vendor
	StorageClassName string
	// CSIDriverName is the CSI driver name
	CSIDriverName string
	// Parameters are the merged parameters from policy and volume
	Parameters map[string]string
}

// VendorSelector is the interface for DPUVolume vendor selection operations
type VendorSelector interface {
	// SelectVendorForDPUVolume selects vendor for the DPUVolume and returns the selection results
	SelectVendorForDPUVolume(ctx context.Context, dpuVolume *storagev1.DPUVolume) (Result, error)
}

// vendorSelector implements the VendorSelector interface
type vendorSelector struct {
	client           client.Client
	dpuClusterHelper dpuclusterhelper.DPUClusterHelper
	options          Options
}

// New creates a new VendorSelector instance
func New(client client.Client, dpuClusterHelper dpuclusterhelper.DPUClusterHelper, options Options) VendorSelector {
	return &vendorSelector{
		client:           client,
		dpuClusterHelper: dpuClusterHelper,
		options:          options,
	}
}

// SelectVendorForDPUVolume selects vendor for the DPUVolume
func (s *vendorSelector) SelectVendorForDPUVolume(ctx context.Context, dpuVolume *storagev1.DPUVolume) (Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Selecting vendor for DPUVolume", "dpuVolume", dpuVolume.Name)
	dpuStoragePolicy := &storagev1.DPUStoragePolicy{}
	dpuStoragePolicyKey := client.ObjectKey{Namespace: s.options.Namespace, Name: dpuVolume.Spec.DPUStoragePolicyName}
	err := s.client.Get(ctx, dpuStoragePolicyKey, dpuStoragePolicy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Result{
				Selected: false,
				Reason:   fmt.Sprintf("DPUStoragePolicy %s not found", dpuStoragePolicyKey),
			}, nil
		}
		reqLog.Error(err, "Failed to get DPUStoragePolicy")
		return Result{}, err
	}

	// Check if the policy is ready
	if !conditions.IsTrue(dpuStoragePolicy, conditions.TypeReady) {
		return Result{
			Selected: false,
			Reason:   fmt.Sprintf("DPUStoragePolicy %s is not ready", dpuStoragePolicyKey),
		}, nil
	}
	// Select storage vendor, based on the selection algorithm
	dpuStorageVendor, err := s.selectDPUStorageVendor(ctx, dpuStoragePolicy)
	if err != nil {
		return Result{}, err
	}
	// Select DPUCluster, based on the list of DPUClusters where the DPUStorageVendor is available
	dpuCluster, err := s.selectDPUCluster(ctx, dpuStorageVendor)
	if err != nil {
		return Result{}, err
	}
	// Resolve additional information about the selected DPUStorageVendor by querying the DPUCluster
	dpuClusterKey := client.ObjectKey{
		Namespace: dpuCluster.Namespace,
		Name:      dpuCluster.Name,
	}
	dpuClusterClient, err := s.dpuClusterHelper.GetClient(ctx, dpuCluster)
	if err != nil {
		return Result{}, err
	}
	sc := &corestoragev1.StorageClass{}
	if err := dpuClusterClient.Client.Get(ctx, client.ObjectKey{Name: dpuStorageVendor.Spec.StorageClassName}, sc); err != nil {
		reqLog.Error(err, "Failed to get StorageClass referenced in DPUStorageVendor", "storageClass",
			dpuStorageVendor.Spec.StorageClassName, "dpuCluster", dpuClusterKey)
		return Result{}, err
	}
	csidriver := &corestoragev1.CSIDriver{}
	if err := dpuClusterClient.Client.Get(ctx, client.ObjectKey{Name: sc.Provisioner}, csidriver); err != nil {
		reqLog.Error(err, "Failed to get CSIDriver referenced in StorageClass", "csidriver",
			sc.Provisioner, "dpuCluster", dpuClusterKey, "storageClass", dpuStorageVendor.Spec.StorageClassName)
		return Result{}, err
	}

	// Merge parameters from DPUStoragePolicy and DPUVolume.Spec.Parameters,
	// parameters from DPUVolume.Spec.Parameters take precedence
	mergedParameters := maps.Clone(dpuStoragePolicy.Spec.Parameters)
	maps.Copy(mergedParameters, dpuVolume.Spec.Parameters)

	selectedVendorInfo := &SelectedVendorInfo{
		DPUClusterName:               dpuCluster.Name,
		DPUClusterNamespace:          dpuCluster.Namespace,
		SelectedDPUStorageVendorName: dpuStorageVendor.Name,
		StorageVendorPluginName:      dpuStorageVendor.Spec.PluginName,
		StorageClassName:             dpuStorageVendor.Spec.StorageClassName,
		CSIDriverName:                csidriver.Name,
		Parameters:                   mergedParameters,
	}

	result := Result{
		Selected:           true,
		SelectedVendorInfo: selectedVendorInfo,
	}

	reqLog.Info("DPUVolume vendor selected",
		"dpuStoragePolicy", dpuStoragePolicy.Name,
		"selectedDPUStorageVendorName", selectedVendorInfo.SelectedDPUStorageVendorName,
		"storageVendorPluginName", selectedVendorInfo.StorageVendorPluginName,
		"dpuCluster", selectedVendorInfo.DPUClusterName+"/"+selectedVendorInfo.DPUClusterNamespace,
		"storageClassName", selectedVendorInfo.StorageClassName,
		"csiDriverName", selectedVendorInfo.CSIDriverName,
		"parameters", selectedVendorInfo.Parameters)
	return result, nil
}

// selectVendorByNumberVolumes selects vendor with minimum number of volumes
func (s *vendorSelector) selectVendorByNumberVolumes(ctx context.Context, vendors []string) (string, error) {
	reqLog := ctrllog.FromContext(ctx)
	vendorVolumesCount := make(map[string]int)
	for _, dpuStorageVendor := range vendors {
		dpuVolumeList := &storagev1.DPUVolumeList{}
		if err := s.client.List(ctx, dpuVolumeList, client.InNamespace(s.options.Namespace),
			client.MatchingFields{indexers.DPUVolumeStatusStateSelectedDPUStorageVendorName: dpuStorageVendor}); err != nil {
			reqLog.Error(err, "Failed to list DPUVolumes for vendor", "dpuStorageVendor", dpuStorageVendor)
			return "", err
		}
		vendorVolumesCount[dpuStorageVendor] = len(dpuVolumeList.Items)
	}
	minCount := -1
	var candidateVendors []string
	for vendor, count := range vendorVolumesCount {
		if minCount == -1 || count < minCount {
			minCount = count
			candidateVendors = []string{vendor}
		} else if count == minCount {
			candidateVendors = append(candidateVendors, vendor)
		}
	}
	// Sort alphabetically for deterministic selection when counts are equal
	sort.Strings(candidateVendors)
	return candidateVendors[0], nil
}

// selectVendorRandom selects a random vendor from the list
func (s *vendorSelector) selectVendorRandom(vendors []string) string {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	return vendors[rnd.Intn(len(vendors))]
}

// selectDPUStorageVendor selects the storage vendor for the DPUVolume based on the selection algorithm
func (s *vendorSelector) selectDPUStorageVendor(ctx context.Context, dpuStoragePolicy *storagev1.DPUStoragePolicy) (*storagev1.DPUStorageVendor, error) {
	reqLog := ctrllog.FromContext(ctx)
	if dpuStoragePolicy.Spec.SelectionAlgorithm == nil {
		dpuStoragePolicy.Spec.SelectionAlgorithm = ptr.To(storagev1.SelectionAlgorithmNumberVolumes)
	}
	if len(dpuStoragePolicy.Spec.DPUStorageVendors) == 0 {
		err := errors.New("no storage vendors specified")
		reqLog.Error(err, "Invalid DPUStoragePolicy", "dpuStoragePolicy", dpuStoragePolicy.Name)
		return nil, err
	}
	var selectedVendor string
	var err error
	if *dpuStoragePolicy.Spec.SelectionAlgorithm == storagev1.SelectionAlgorithmNumberVolumes {
		selectedVendor, err = s.selectVendorByNumberVolumes(ctx, dpuStoragePolicy.Spec.DPUStorageVendors)
		if err != nil {
			return nil, err
		}
	} else {
		selectedVendor = s.selectVendorRandom(dpuStoragePolicy.Spec.DPUStorageVendors)
	}
	dpuStorageVendor := &storagev1.DPUStorageVendor{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: s.options.Namespace, Name: selectedVendor}, dpuStorageVendor); err != nil {
		reqLog.Error(err, "Failed to get DPUStorageVendor", "dpuStorageVendor", selectedVendor)
		return nil, err
	}
	reqLog.Info("Selected storage vendor", "algorithm", dpuStoragePolicy.Spec.SelectionAlgorithm,
		"vendor", dpuStorageVendor.Name)
	return dpuStorageVendor, nil
}

// selectDPUCluster selects the DPUCluster for the DPUVolume based on the list of DPUClusters where the DPUStorageVendor is available
func (s *vendorSelector) selectDPUCluster(ctx context.Context, dpuStorageVendor *storagev1.DPUStorageVendor) (*provisioningv1.DPUCluster, error) {
	reqLog := ctrllog.FromContext(ctx)
	if len(dpuStorageVendor.Status.DPUClusters) == 0 {
		return nil, fmt.Errorf("no DPUClusters available in DPUStorageVendor %s", dpuStorageVendor.Name)
	}
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	selectedDPUCluster := dpuStorageVendor.Status.DPUClusters[rnd.Intn(len(dpuStorageVendor.Status.DPUClusters))]
	dpuCluster, err := s.dpuClusterHelper.GetDPUCluster(ctx,
		client.ObjectKey{Namespace: selectedDPUCluster.Namespace, Name: selectedDPUCluster.Name})
	if err != nil {
		reqLog.Error(err, "Failed to get DPUCluster", "dpuCluster", selectedDPUCluster)
		return nil, err
	}
	return dpuCluster, nil
}
