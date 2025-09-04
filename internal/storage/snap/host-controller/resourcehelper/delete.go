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

package resourcehelper

import (
	"context"
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// DeletionResult contains the result of the delete operation
type DeletionResult struct {
	// Completed indicates if the deletion operation was completed
	Completed bool
	// Reason contains the reason for the result
	Reason string
}

// handleFinalizerRemoval removes specified finalizers from the object and updates it in the DPU cluster.
func handleFinalizerRemoval(ctx context.Context, reqLog logr.Logger, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	apiObj client.Object, finalizersToRemove []string) error {
	if len(finalizersToRemove) == 0 {
		return nil
	}
	modified := false
	for _, finalizer := range finalizersToRemove {
		if controllerutil.ContainsFinalizer(apiObj, finalizer) {
			controllerutil.RemoveFinalizer(apiObj, finalizer)
			modified = true
		}
	}
	if modified {
		if err := dpuClusterClient.Client.Update(ctx, apiObj); err != nil {
			reqLog.Error(err, "Failed to remove finalizer from resource in DPU cluster")
			return err
		}
		reqLog.Info("Removed finalizers from resource in DPU cluster", "finalizers", finalizersToRemove)
	}
	return nil
}

// handleResourceDeletion initiates deletion of the resource from the DPU cluster.
func handleResourceDeletion(ctx context.Context, reqLog logr.Logger, dpuClusterClient dpuclusterhelper.ClientForDPUCluster,
	apiObj client.Object) error {
	if err := dpuClusterClient.Client.Delete(ctx, apiObj); err != nil {
		reqLog.Error(err, "Failed to delete resource in DPU cluster")
		return err
	}
	reqLog.Info("Resource marked for deletion in DPU cluster")
	return nil
}

// DeleteResourceInDPUClusters attempts to delete the specified resource
// from all provided DPU clusters. For each cluster, it optionally removes
// the given finalizers from the resource, initiates deletion, and tracks
// whether the deletion has fully completed.
//
// The finalizersToRemove parameter allows forced removal of specific
// finalizers from the resource before deletion. This is particularly
// useful for cleaning up resources that may still have finalizers set by
// controllers that are no longer running. In typical scenarios, where
// you want other controllers to properly handle resource deletion, pass
// an empty finalizersToRemove slice.
//
// The function returns a DeletionResult indicating whether deletion is
// complete across all clusters, along with a reason describing any
// incomplete deletions.
func DeleteResourceInDPUClusters(ctx context.Context, dpuClustersClients []dpuclusterhelper.ClientForDPUCluster,
	resourceKind string, key client.ObjectKey, obj client.Object, finalizersToRemove []string) (DeletionResult, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("resourceKind", resourceKind, "name", key)
	reasons := []string{}
	completed := true
	for _, c := range dpuClustersClients {
		clusterKey := client.ObjectKeyFromObject(c.DPUCluster)
		reqLog = reqLog.WithValues("dpuCluster", clusterKey)
		reqLog.Info("Cleaning up resource in DPU cluster")
		err := c.Client.Get(ctx, key, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			reqLog.Error(err, "Failed to get resource in DPU cluster")
			return DeletionResult{}, err
		}
		if apierrors.IsNotFound(err) {
			reqLog.Info("Resource not found")
			continue
		}
		if err := handleFinalizerRemoval(ctx, reqLog, c, obj, finalizersToRemove); err != nil {
			return DeletionResult{}, err
		}
		if !obj.GetDeletionTimestamp().IsZero() {
			reqLog.Info("Resource is already being deleted")
			reasons = append(reasons, fmt.Sprintf("DPUCluster %s: %s %s is not removed yet",
				clusterKey.String(), resourceKind, client.ObjectKeyFromObject(obj).String()))
			completed = false
			continue
		}
		reqLog.Info("Deleting resource in DPU cluster")
		err = handleResourceDeletion(ctx, reqLog, c, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			return DeletionResult{}, err
		}
		if apierrors.IsNotFound(err) {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("DPUCluster %s: %s %s is marked for removal",
			clusterKey.String(), resourceKind, client.ObjectKeyFromObject(obj).String()))
		completed = false
	}
	return DeletionResult{Completed: completed, Reason: strings.Join(reasons, ",")}, nil
}
