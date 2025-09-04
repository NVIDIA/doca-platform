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

package dpuclusterhelper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ErrDPUClusterClientNotAvailable indicates that a client for a DPUCluster is not available for any reason.
var ErrDPUClusterClientNotAvailable = errors.New("client for DPUCluster is not available")

// ClientForDPUCluster is a struct that contains a client instance and a pointer to the DPUCluster.
type ClientForDPUCluster struct {
	Client     client.Client
	DPUCluster *provisioningv1.DPUCluster
}

// DPUClusterHelper is the interface for the DPUClusterHelper.
type DPUClusterHelper interface {
	// GetDPUCluster returns the DPUCluster object for the specified DPUCluster
	GetDPUCluster(ctx context.Context, clusterKey client.ObjectKey) (*provisioningv1.DPUCluster, error)
	// GetTargetDPUClusters returns the list of DPUClusters that should be used for "all DPUClusters" operations.
	// The function returns the list of DPUClusters that are ready.
	// If the requiredDPUClusters are provided, they are included in the list of target DPUClusters even if they are not ready.
	GetTargetDPUClusters(ctx context.Context, requiredDPUClusters []client.ObjectKey) ([]provisioningv1.DPUCluster, error)
	// GetDPUClusterClients returns a list of clients for the DPUClusters
	GetDPUClusterClients(ctx context.Context, clusters []provisioningv1.DPUCluster) ([]ClientForDPUCluster, error)
	// GetClient returns a client for the specified DPUCluster
	GetClient(ctx context.Context, cluster *provisioningv1.DPUCluster) (ClientForDPUCluster, error)
}

// New creates a new instance of DPUClusterHelper.
func New(hostClusterClient client.Client, clusterClientProvider dpucluster.ClusterClientProvider) DPUClusterHelper {
	return &dpuClusterHelper{
		hostClusterClient:     hostClusterClient,
		clusterClientProvider: clusterClientProvider,
	}
}

// dpuClusterHelper is the implementation of DPUClusterHelper.
type dpuClusterHelper struct {
	hostClusterClient     client.Client
	clusterClientProvider dpucluster.ClusterClientProvider
}

// GetDPUCluster returns the DPUCluster object for the specified DPUCluster
func (h *dpuClusterHelper) GetDPUCluster(ctx context.Context, clusterKey client.ObjectKey) (*provisioningv1.DPUCluster, error) {
	dpuCluster := &provisioningv1.DPUCluster{}
	err := h.hostClusterClient.Get(ctx, clusterKey, dpuCluster)
	if err != nil {
		return nil, err
	}
	return dpuCluster, nil
}

// GetTargetDPUClusters returns the list of DPUClusters that should be used for "all DPUClusters" operations. The function returns the list of DPUClusters that are ready.
// If the requiredDPUClusters are provided, they are included in the list of target DPUClusters even if they are not ready.
func (h *dpuClusterHelper) GetTargetDPUClusters(ctx context.Context, requiredDPUClusters []client.ObjectKey) ([]provisioningv1.DPUCluster, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Getting target DPUClusters", "requiredDPUClusters", requiredDPUClusters)
	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := h.hostClusterClient.List(ctx, dpuClusterList); err != nil {
		reqLog.Error(err, "Failed to list DPUClusters")
		return nil, err
	}
	result := []provisioningv1.DPUCluster{}
	resultKeys := []client.ObjectKey{}
	for _, dpuCluster := range dpuClusterList.Items {
		isRequired := slices.Contains(requiredDPUClusters, client.ObjectKeyFromObject(&dpuCluster))
		if isRequired || dpuCluster.Status.Phase == provisioningv1.PhaseReady {
			result = append(result, dpuCluster)
			resultKeys = append(resultKeys, client.ObjectKeyFromObject(&dpuCluster))
		}
	}
	reqLog.Info("Target DPUClusters", "targetDPUClusters", resultKeys)
	return result, nil
}

// GetClient returns a client for the specified DPUCluster
func (h *dpuClusterHelper) GetClient(ctx context.Context, cluster *provisioningv1.DPUCluster) (ClientForDPUCluster, error) {
	c, err := h.clusterClientProvider.GetClient(client.ObjectKeyFromObject(cluster))
	if err != nil {
		return ClientForDPUCluster{}, fmt.Errorf("%w: %v", ErrDPUClusterClientNotAvailable, err)
	}
	return ClientForDPUCluster{
		Client:     c,
		DPUCluster: cluster,
	}, nil
}

// GetDPUClusterClients returns a list of clients for the DPUClusters
func (h *dpuClusterHelper) GetDPUClusterClients(ctx context.Context, clusters []provisioningv1.DPUCluster) ([]ClientForDPUCluster, error) {
	clients := []ClientForDPUCluster{}
	var errs []error
	for _, cluster := range clusters {
		c, err := h.GetClient(ctx, &cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get client for DPUCluster %s: %w", client.ObjectKeyFromObject(&cluster), err))
			continue
		}
		clients = append(clients, c)
	}
	if len(errs) > 0 {
		return nil, kerrors.NewAggregate(errs)
	}
	return clients, nil
}
