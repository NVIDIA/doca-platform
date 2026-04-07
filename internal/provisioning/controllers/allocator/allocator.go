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

package allocator

import (
	"context"
	"fmt"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type AllocateResult = types.NamespacedName

type Allocator interface {
	// Allocate allocates a DPUCluster for DPU
	Allocate(context.Context, *provisioningv1.DPU) (AllocateResult, error)
	// SaveAssignedDPU stores assigned DPU to cache
	SaveAssignedDPU(*provisioningv1.DPU)
	// SaveCluster updates cached DPUCluster with the given one
	SaveCluster(*provisioningv1.DPUCluster)
	// ReleaseDPU releases the given DPU from its allocated DPUCluster
	ReleaseDPU(*provisioningv1.DPU)
	// RemoveCluster removes given DPUCluster from cache
	RemoveCluster(dc *provisioningv1.DPUCluster)
	// GetDPUsCount returns the number of assigned DPUs to the given DPUCluster
	GetDPUsCount(dc *provisioningv1.DPUCluster) int
}

type allocator struct {
	sync.Mutex
	synced       bool
	client       client.Client
	clusterCache *ClusterCache
}

func NewAllocator(client client.Client) Allocator {
	return &allocator{
		client:       client,
		clusterCache: newCache(),
	}
}

func (a *allocator) Allocate(ctx context.Context, dpu *provisioningv1.DPU) (result AllocateResult, reterr error) {
	a.Lock()
	defer a.Unlock()
	// Before we allocate DPUCluster for any DPU, we must sync with api-server to find out all the assigned DPUs
	// Ideally, we should do this before the processNextWorkItem goroutines are started - see https://github.com/kubernetes-sigs/controller-runtime/blob/release-0.19/pkg/internal/controller/controller.go#L218
	// But we can't do it without modifing the controller-runtime code.
	// TODO: Currently, DPUCLuster are referenced by name and namespace. As DPUClusters may be deleted and recreated with the same name, this syncing may be wrong.
	// I will talk with arch about adding UID to the DPU.spec.cluster field.
	if !a.synced {
		if reterr = a.syncAssignedDPUs(ctx); reterr != nil {
			return result, reterr
		}
	}

	var ci *ClusterInfo
	ci, reterr = a.clusterCache.getFeasibleClusterBinPacking(dpu.Spec.Cluster.Selector)
	if reterr != nil {
		return result, reterr
	} else if ci == nil || ci.cluster == nil {
		return result, fmt.Errorf("ci and ci.cluster should not be nil")
	}

	// it's safe to use patch here. For now, nothing in DPU affects to which DPUCluster it is assigned
	patcher := patch.NewSerialPatcher(dpu, a.client)
	defer func() {
		reterr = patcher.Patch(ctx, dpu)
		if reterr == nil {
			// update the cache so that we don't rely on the informer for the latest number of nodes in the DPUCLuster,
			// meaning that we can start allocating the next DPU object immediately
			ci.addAllocated(dpu)
		}
	}()

	result = cutil.GetNamespacedName(ci.cluster)
	dpu.Spec.Cluster.Name = result.Name
	dpu.Spec.Cluster.Namespace = result.Namespace
	return result, nil
}

func (a *allocator) syncAssignedDPUs(ctx context.Context) (reterr error) {
	defer func() {
		if reterr == nil {
			a.synced = true
		}
	}()
	log.FromContext(ctx).Info("start syncing for assigned DPUs")
	dcList := &provisioningv1.DPUClusterList{}
	if err := a.client.List(ctx, dcList); err != nil {
		return err
	}
	for _, dc := range dcList.Items {
		a.clusterCache.saveCluster(&dc)
	}

	dpuList := &provisioningv1.DPUList{}
	if err := a.client.List(ctx, dpuList); err != nil {
		return err
	}
	for _, dpu := range dpuList.Items {
		a.clusterCache.saveDPU(&dpu)
	}
	return nil
}

func (a *allocator) SaveAssignedDPU(dpu *provisioningv1.DPU) {
	a.Lock()
	defer a.Unlock()
	a.clusterCache.saveDPU(dpu)
}

func (a *allocator) SaveCluster(dc *provisioningv1.DPUCluster) {
	a.Lock()
	defer a.Unlock()
	a.clusterCache.saveCluster(dc)
}

func (a *allocator) ReleaseDPU(dpu *provisioningv1.DPU) {
	a.Lock()
	defer a.Unlock()
	clsName := types.NamespacedName{
		Name:      dpu.Spec.Cluster.Name,
		Namespace: dpu.Spec.Cluster.Namespace,
	}
	ci := a.clusterCache.getByName(clsName)
	if ci == nil {
		return
	}
	delete(ci.dpus, dpu.UID)
}

func (a *allocator) RemoveCluster(dc *provisioningv1.DPUCluster) {
	a.Lock()
	defer a.Unlock()
	a.clusterCache.removeCluster(dc)
}

func (a *allocator) GetDPUsCount(dc *provisioningv1.DPUCluster) int {
	a.Lock()
	defer a.Unlock()
	return a.clusterCache.getDPUsCount(dc)
}

type ClusterInfo struct {
	cluster *provisioningv1.DPUCluster
	dpus    map[types.UID]*provisioningv1.DPU
}

// getFeasibleClusterBinPacking returns the cluster with the highest ratio of assigned DPUs to max nodes
func (c *ClusterCache) getFeasibleClusterBinPacking(labelSelector *metav1.LabelSelector) (*ClusterInfo, error) {
	clusterInfoMap := make(map[types.UID]*ClusterInfo, len(c.clusterInfoMap))
	selector, err := utils.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to parse label selector: %w", err)
	}

	for _, ci := range c.clusterInfoMap {
		if selector.Matches(labels.Set(ci.cluster.Labels)) {
			clusterInfoMap[ci.cluster.UID] = ci
		}
	}

	var bestCluster *ClusterInfo
	var maxRatio = -1.0
	for _, ci := range clusterInfoMap {
		if ci.cluster.DeletionTimestamp != nil {
			continue
		}
		if ci.cluster.Status.Phase != provisioningv1.PhaseReady {
			continue
		}
		if len(ci.dpus) >= ci.cluster.Spec.MaxNodes {
			continue
		}
		assignedDPUsCount := len(ci.dpus)
		ratio := float64(assignedDPUsCount) / float64(ci.cluster.Spec.MaxNodes)
		if ratio > maxRatio {
			maxRatio = ratio
			bestCluster = ci
		}
	}
	if bestCluster != nil {
		return bestCluster, nil
	}
	return nil, fmt.Errorf("no feasible cluster found")
}

func (ci *ClusterInfo) addAllocated(dpu *provisioningv1.DPU) {
	if ci.dpus == nil {
		ci.dpus = make(map[types.UID]*provisioningv1.DPU)
	}
	ci.dpus[dpu.UID] = dpu.DeepCopy()
}

// ClusterCache caches DPUClusters and to which DPUs they are allocated.
type ClusterCache struct {
	clusterInfoMap map[types.UID]*ClusterInfo
	idxByName      map[types.NamespacedName]*ClusterInfo
}

func newCache() *ClusterCache {
	return &ClusterCache{
		clusterInfoMap: make(map[types.UID]*ClusterInfo),
		idxByName:      make(map[types.NamespacedName]*ClusterInfo),
	}
}

func (c *ClusterCache) saveDPU(dpu *provisioningv1.DPU) {
	if dpu == nil {
		return
	}
	cls := dpu.Spec.Cluster
	if !dpu.DeletionTimestamp.IsZero() || cls.Name == "" {
		return
	}
	nn := types.NamespacedName{Name: cls.Name, Namespace: cls.Namespace}
	ci := c.getByName(nn)
	if ci == nil {
		return
	}
	ci.addAllocated(dpu)
}

func (c *ClusterCache) saveCluster(dc *provisioningv1.DPUCluster) {
	if dc == nil {
		return
	}

	ci, ok := c.clusterInfoMap[dc.UID]
	if ok {
		*ci.cluster = *(dc.DeepCopy())
		return
	}
	ci = &ClusterInfo{
		cluster: dc.DeepCopy(),
		dpus:    make(map[types.UID]*provisioningv1.DPU),
	}
	c.clusterInfoMap[dc.UID] = ci
	c.idxByName[cutil.GetNamespacedName(dc)] = ci
}

func (c *ClusterCache) removeCluster(dc *provisioningv1.DPUCluster) {
	if dc == nil {
		return
	}

	delete(c.clusterInfoMap, dc.UID)
	delete(c.idxByName, cutil.GetNamespacedName(dc))
}

func (c *ClusterCache) getByName(name types.NamespacedName) *ClusterInfo {
	return c.idxByName[name]
}

func (c *ClusterCache) getDPUsCount(dc *provisioningv1.DPUCluster) int {
	ci, ok := c.clusterInfoMap[dc.UID]
	if !ok {
		return 0
	}
	return len(ci.dpus)
}
