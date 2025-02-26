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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"
	"github.com/nvidia/doca-platform/internal/operator/utils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// namespacedNameInCluster is a struct that points to a particular object within a cluster
type namespacedNameInCluster struct {
	Object  types.NamespacedName
	Cluster provisioningv1.DPUCluster
}

func (n *namespacedNameInCluster) String() string {
	return fmt.Sprintf("{Cluster: %s/%s, Object: %s/%s}", n.Cluster.Namespace, n.Cluster.Name, n.Object.Namespace, n.Object.Name)
}

// objectsInDPUClustersReconciler is an interface that enables host cluster reconcilers to reconcile objects in the DPU
// clusters in a standardized way.
type objectsInDPUClustersReconciler interface {
	// getObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
	// objects in the DPU cluster related to the given parentObject. The implementation should get the created objects
	// in the DPU cluster.
	getObjectsInDPUCluster(ctx context.Context, dpuClusterClient client.Client, parentObject client.Object) ([]unstructured.Unstructured, error)
	// createOrUpdateObjectsInDPUCluster is the method called by the reconcileObjectsInDPUClusters function which applies changes to
	// the DPU clusters based on the given parentObject. The implementation should create and update objects in the DPU
	// cluster.
	createOrUpdateObjectsInDPUCluster(ctx context.Context, dpuClusterClient client.Client, parentObject client.Object) error
	// deleteObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
	// objects in the DPU cluster related to the given parentObject. The implementation should delete objects in the
	// DPU cluster.
	deleteObjectsInDPUCluster(ctx context.Context, dpuClusterClient client.Client, parentObject client.Object) error
	// getUnreadyObjects is the method called by reconcileReadinessOfObjectsInDPUClusters function which returns whether
	// objects in the DPU cluster are ready. The input to the function is a list of objects that exist in a particular
	// cluster.
	getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error)
}

// shouldRequeueError is an error returned by the functions below that indicates that an event should be requeued. This
// error is useful when one wants to requeue after a specific amount of time.
type shouldRequeueError struct {
	err error
}

func (e *shouldRequeueError) Error() string {
	return e.err.Error()
}

// reconcileObjectDeletionInDPUClusters handles the delete reconciliation loop for objects in the DPU clusters. It
// ensures that objects are completely removed from the DPU clusters. It returns ShouldRequeue error if the request
// should be requeued.
//
//nolint:unparam
func reconcileObjectDeletionInDPUClusters(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	dpuServiceObject client.Object,
) error {

	//TODO implement list clusters with label selector
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, k8sClient)
	if err != nil {
		// TODO: Adjust error handling here to do as much work as possible with clusters we managed to rerieve, report
		// error back to the controller logs and update status accordingly
		return err
	}
	var existingObjs int
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := dpuClusterConfig.Client(ctx)
		if err != nil {
			return err
		}
		if err := r.deleteObjectsInDPUCluster(ctx, dpuClusterClient, dpuServiceObject); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		objs, err := r.getObjectsInDPUCluster(ctx, dpuClusterClient, dpuServiceObject)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		existingObjs += len(objs)
	}

	if existingObjs > 0 {
		return &shouldRequeueError{err: fmt.Errorf("%d objects still exist across all DPU clusters", existingObjs)}
	}

	return nil
}

// reconcileObjectsInDPUClusters handles the main reconciliation loop for objects in the DPU clusters
//
//nolint:unparam
func reconcileObjectsInDPUClusters(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	dpuServiceObject client.Object,
) error {

	// Get the list of clusters this DPUServiceObject targets.
	//TODO implement list clusters with label selector
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, k8sClient)
	if err != nil {
		// TODO: Adjust error handling here to do as much work as possible with clusters we managed to rerieve, report
		// error back to the controller logs and update status accordingly
		return err
	}

	for _, dpuClusterConfig := range dpuClusterConfigs {
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
	}
	return nil
}

// reconcileReadinessOfObjectsInDPUClusters handles the readiness reconciliation loop for objects in the DPU clusters
//
//nolint:unparam
func reconcileReadinessOfObjectsInDPUClusters(ctx context.Context,
	r objectsInDPUClustersReconciler,
	k8sClient client.Client,
	dpuServiceObject client.Object,
) ([]namespacedNameInCluster, error) {

	// Get the list of clusters this DPUServiceObject targets.
	//TODO implement list clusters with label selector
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, k8sClient)
	if err != nil {
		// TODO: Adjust error handling here to do as much work as possible with clusters we managed to retrieve, report
		// error back to the controller logs and update status accordingly
		return nil, err
	}

	unreadyObjsNN := []namespacedNameInCluster{}
	for _, dpuClusterConfig := range dpuClusterConfigs {
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
	dpuServiceObject client.Object,
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
		conditions.AddFalse(
			objAsGetSet,
			objReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Objects not ready: %s", strings.Join(func() []string {
				out := []string{}
				for _, o := range unreadyObjs {
					out = append(out, o.String())
				}
				return out
			}(), ","))),
		)
	} else {
		conditions.AddTrue(
			objAsGetSet,
			objReadyCondition,
		)
	}

	return nil
}
