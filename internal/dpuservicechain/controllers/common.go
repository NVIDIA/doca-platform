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

package controllers

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// namespacedNameInCluster is a struct that points to a particular object within a cluster
type namespacedNameInCluster struct {
	Object  types.NamespacedName
	Cluster provisioningv1.DPUCluster
}

func (n *namespacedNameInCluster) String() string {
	return fmt.Sprintf("Cluster: %s/%s\n  Object: %s/%s", n.Cluster.Namespace, n.Cluster.Name, n.Object.Namespace, n.Object.Name)
}

// objectsInDPUClustersReconciler is an interface that enables host cluster reconcilers to reconcile objects in the DPU
// clusters in a standardized way.
type objectsInDPUClustersReconciler interface {
	// getObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
	// objects in the DPU cluster related to the given parentObject. The implementation should get the created objects
	// in the DPU cluster.
	getObjectsInDPUCluster(ctx context.Context, dpuClusterClient client.Client, parentObject dpuservicev1.DPUServiceObject) ([]unstructured.Unstructured, error)
	// createOrUpdateObjectsInDPUCluster is the method called by the reconcileObjectsInDPUClusters function which applies changes to
	// the DPU clusters based on the given parentObject. The implementation should create and update objects in the DPU
	// cluster.
	createOrUpdateObjectsInDPUCluster(ctx context.Context, dpuClusterClient client.Client, parentObject dpuservicev1.DPUServiceObject) error
	// deleteObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
	// objects in the DPU cluster related to the given parentObject. The implementation should delete objects in the
	// DPU cluster.
	deleteObjectsInDPUCluster(ctx context.Context, dpuClusterClient client.Client, parentObject dpuservicev1.DPUServiceObject) error
	// getUnreadyObjects is the method called by reconcileReadinessOfObjectsInDPUClusters function which returns whether
	// objects in the DPU cluster are ready. The input to the function is a list of objects that exist in a particular
	// cluster.
	getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error)
	// registerKindToWatcher is the method which starts watching the objects in the DPU clusters.
	registerKindToWatcher(ctx context.Context, dpuClusterKey types.NamespacedName) error
}

// longOperationError is an error returned by the functions below that indicates that an event should be requeued. This
// error is returned when we know an operation that was triggered will take time to complete.
type longOperationError struct {
	err error
}

func (e *longOperationError) Error() string {
	return e.err.Error()
}

// watchObjectsInDPUClusters watches objects in the DPU clusters. It is called by the reconciler to start
// watching the objects in the DPU clusters.
func watchObjectsInDPUClusters(ctx context.Context, k8sClient client.Client, r objectsInDPUClustersReconciler) error {
	dpuClusters := &provisioningv1.DPUClusterList{}
	err := k8sClient.List(ctx, dpuClusters)
	if err != nil {
		return fmt.Errorf("error while listing DPU clusters: %w", err)
	}

	var errs []error
	for _, dpuCluster := range dpuClusters.Items {
		// This is a no-op if the watch is already registered
		if err := r.registerKindToWatcher(ctx, client.ObjectKey{Namespace: dpuCluster.Namespace, Name: dpuCluster.Name}); err != nil {
			// if err happens, most likely the connection is unhealthy and we should requeue
			errs = append(errs, err)
		}
	}
	return kerrors.NewAggregate(errs)
}

// reconcileObjectDeletionInDPUClusters handles the delete reconciliation loop for objects in the DPU clusters. It
// ensures that objects are completely removed from the DPU clusters. It returns ShouldRequeue error if the request
// should be requeued.
//
//nolint:unparam
func reconcileObjectDeletionInDPUClusters(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	dpuServiceObject dpuservicev1.DPUServiceObject,
) error {

	// Get the list of all DPUClusters as we need to ensure that no leftover resource across any cluster
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, k8sClient)
	if err != nil {
		// TODO: Adjust error handling here to do as much work as possible with clusters we managed to rerieve, report
		// error back to the controller logs and update status accordingly
		return err
	}
	var existingObjs int
	for _, dpuClusterConfig := range dpuClusterConfigs {
		objs, err := deleteObjectsInDPUCluster(ctx, dpuClusterConfig, r, dpuServiceObject)
		if err != nil {
			return err
		}
		existingObjs += len(objs)
	}

	if existingObjs > 0 {
		return &longOperationError{err: fmt.Errorf("%d objects still exist across all DPU clusters", existingObjs)}
	}

	return nil
}

// reconcileObjectsInDPUClusters handles the main reconciliation loop for objects in the DPU clusters
//
//nolint:unparam
func reconcileObjectsInDPUClusters(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	dpuServiceObject dpuservicev1.DPUServiceObject,
) error {

	// Get the list of all DPUClusters
	allDPUClusterConfigs, err := dpucluster.GetConfigs(ctx, k8sClient)
	if err != nil {
		// TODO: Adjust error handling here to do as much work as possible with clusters we managed to rerieve, report
		// error back to the controller logs and update status accordingly
		return err
	}

	// Create a map for all DPUClusters that need to be processed
	unprocessedDPUClusters := make(map[string]*dpucluster.Config, len(allDPUClusterConfigs))
	for i, dpuClusterConfig := range allDPUClusterConfigs {
		unprocessedDPUClusters[client.ObjectKeyFromObject(dpuClusterConfig.Cluster).String()] = allDPUClusterConfigs[i]
	}

	// Get the list of the DPUClusters the resource is targeting
	targetDPUClusterConfigs, err := utils.GetMatchingDPUClusters(allDPUClusterConfigs, dpuServiceObject.GetDPUClusterSelector())
	if err != nil {
		return err
	}

	// Create and update objects in each targeted cluster
	for _, dpuClusterConfig := range targetDPUClusterConfigs {
		dpuClusterClient, err := dpuClusterConfig.Client(ctx)
		if err != nil {
			return err
		}
		if err := utils.EnsureNamespace(ctx, dpuClusterClient, dpuServiceObject.GetNamespace()); err != nil {
			return err
		}
		if err := r.createOrUpdateObjectsInDPUCluster(ctx, dpuClusterClient, dpuServiceObject); err != nil {
			return err
		}

		// Remove the cluster from the map to keep only clusters where resources need to be deleted
		delete(unprocessedDPUClusters, client.ObjectKeyFromObject(dpuClusterConfig.Cluster).String())
	}

	// Delete leftover resources in clusters that are no longer selected
	var existingObjsNN []namespacedNameInCluster
	for _, dpuClusterConfig := range unprocessedDPUClusters {
		objs, err := deleteObjectsInDPUCluster(ctx, dpuClusterConfig, r, dpuServiceObject)
		if err != nil {
			return err
		}
		for _, obj := range objs {
			existingObjsNN = append(existingObjsNN, namespacedNameInCluster{
				Object:  client.ObjectKeyFromObject(&obj),
				Cluster: *dpuClusterConfig.Cluster,
			})
		}
	}

	if len(existingObjsNN) > 0 {
		objMessages := []string{}
		for _, objNN := range existingObjsNN {
			objMessages = append(objMessages, objNN.String())
		}
		message := conditions.ReadyConditionMessage("Stale objects still exist across non matching DPUClusters", objMessages)
		return &longOperationError{err: fmt.Errorf("%s", message)}
	}

	return nil
}

// reconcileReadinessOfObjectsInDPUClusters handles the readiness reconciliation loop for objects in the DPU clusters
//
//nolint:unparam
func reconcileReadinessOfObjectsInDPUClusters(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	dpuServiceObject dpuservicev1.DPUServiceObject,
) ([]namespacedNameInCluster, error) {

	// Get the list of clusters this DPUServiceObject targets.
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, k8sClient)
	if err != nil {
		// TODO: Adjust error handling here to do as much work as possible with clusters we managed to retrieve, report
		// error back to the controller logs and update status accordingly
		return nil, err
	}

	// Get the list of the DPUClusters the resource is targeting
	targetDPUClusterConfigs, err := utils.GetMatchingDPUClusters(dpuClusterConfigs, dpuServiceObject.GetDPUClusterSelector())
	if err != nil {
		return nil, err
	}

	unreadyObjsNN := []namespacedNameInCluster{}
	for _, dpuClusterConfig := range targetDPUClusterConfigs {
		dpuClusterClient, err := dpuClusterConfig.Client(ctx)
		if err != nil {
			return nil, err
		}
		objs, err := r.getObjectsInDPUCluster(ctx, dpuClusterClient, dpuServiceObject)
		if err != nil {
			return nil, err
		}
		unreadyObjs, err := r.getUnreadyObjects(objs)
		if err != nil {
			return nil, err
		}
		for _, unreadyObj := range unreadyObjs {
			unreadyObjsNN = append(unreadyObjsNN, namespacedNameInCluster{Object: unreadyObj, Cluster: *dpuClusterConfig.Cluster})
		}

	}
	return unreadyObjsNN, nil
}

// updateSummary updates the status conditions in the object.
//
//nolint:unparam
func updateSummary(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	objReadyCondition conditions.ConditionType,
	dpuServiceObject dpuservicev1.DPUServiceObject,
) error {

	objAsGetSet, ok := dpuServiceObject.(conditions.GetSet)
	if !ok {
		return fmt.Errorf("error while converting object to conditions.GetSet")
	}

	defer conditions.SetSummary(objAsGetSet)
	unreadyObjs, err := reconcileReadinessOfObjectsInDPUClusters(ctx, r, k8sClient, dpuServiceObject)
	if err != nil {
		conditions.AddFalse(
			objAsGetSet,
			objReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return err
	}

	if len(unreadyObjs) > 0 {
		unreadyMessages := []string{}
		for _, unreadyObj := range unreadyObjs {
			unreadyMessages = append(unreadyMessages, unreadyObj.String())
		}
		message := conditions.ReadyConditionMessage("Objects are not ready", unreadyMessages)
		conditions.AddFalse(
			objAsGetSet,
			objReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(message),
		)
	} else {
		conditions.AddTrue(
			objAsGetSet,
			objReadyCondition,
		)
	}

	return nil
}

// deleteObjectsInDPUCluster deletes objects in the given DPUCluster and returns the amount of objects that still exist
// in that cluster but should not exist.
func deleteObjectsInDPUCluster(ctx context.Context, dpuClusterConfig *dpucluster.Config, r objectsInDPUClustersReconciler, dpuServiceObject dpuservicev1.DPUServiceObject) ([]unstructured.Unstructured, error) {
	dpuClusterClient, err := dpuClusterConfig.Client(ctx)
	if err != nil {
		return nil, err
	}
	if err := r.deleteObjectsInDPUCluster(ctx, dpuClusterClient, dpuServiceObject); err != nil && !isObjectUnavailable(err) {
		return nil, err
	}
	objs, err := r.getObjectsInDPUCluster(ctx, dpuClusterClient, dpuServiceObject)
	if err != nil && !isObjectUnavailable(err) {
		return nil, err
	}
	return objs, nil
}

// isObjectUnavailable returns true when an object type cannot be addressed in the API server.
// NoMatch means the API server does not serve this GVK (for example CRD not installed), so
// there cannot be objects of this type to delete.
func isObjectUnavailable(err error) bool {
	return apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
}
