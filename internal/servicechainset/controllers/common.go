/*
COPYRIGHT 2024 NVIDIA

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
	"context"
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type serviceSetReconciler interface {
	// getObjectsInDPUCluster is the method called by the reconcileReadinessOfObjectsInDPUCluster function which deletes
	// objects in the DPU cluster related to the given parentObject. The implementation should get the created objects
	// in the DPU cluster.
	getObjectsInDPUCluster(ctx context.Context, k8sClient client.Client, dpuObject client.Object) ([]unstructured.Unstructured, error)
	// getChildMap creates a map of ServiceChains for every node inside the cluster called by reconcileSet
	// and reconcileDelete.
	getChildMap(context.Context, client.Object) (map[string]client.Object, error)
	// createOrUpdateChild is the method called by the reconcileSet function which applies changes to
	// the DPU clusters based on the given parentObject. The implementation should create and update objects in the DPU
	// cluster.
	createOrUpdateChild(context.Context, client.Object, string) error
	// getUnreadyObjects is the method called by reconcileReadinessOfObjectsInDPUCluster function which returns whether
	// objects in the DPU cluster are ready. The input to the function is a list of objects that exist in a particular
	// cluster.
	getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error)
	// setReadyStatus sets the status for ready objects called by reconcileReadinessOfObjectsInDPUCluster function
	// which mutates that status ob the serviceSet.
	setReadyStatus(serviceSet client.Object, numberApplied, numberReady int32)
}

//nolint:unparam
func reconcileSet(
	ctx context.Context,
	set client.Object,
	k8sClient client.Client,
	selector *metav1.LabelSelector,
	reconciler serviceSetReconciler,
) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Get node list by nodeSelector
	nodeList, err := getNodeList(ctx, k8sClient, selector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Node list: %w", err)
	}

	// Get Childs map (node->child) which are owned by Set
	childMap, err := reconciler.getChildMap(ctx, set)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Child list: %w", err)
	}

	// create or update Child for the node
	for _, node := range nodeList.Items {
		if err = reconciler.createOrUpdateChild(ctx, set, node.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create or update Child: %w", err)
		}
		delete(childMap, node.Name)
	}

	// delete Child if node does not exist
	for nodeName, child := range childMap {
		if err := k8sClient.Delete(ctx, child); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete child : %w", err)
		}
		log.Info("Child is deleted", "nodename", nodeName, "child", child)
	}

	return ctrl.Result{}, nil
}

func deleteSet(ctx context.Context, set client.Object, k8sClient client.Client,
	reconciler serviceSetReconciler) error {
	log := ctrllog.FromContext(ctx)

	// Get Childs map (node->child) which are owned by Set
	childMap, err := reconciler.getChildMap(ctx, set)
	if err != nil {
		return fmt.Errorf("failed to get Child list: %w", err)
	}

	// delete Child if node does not exist
	for nodeName, child := range childMap {
		if err := k8sClient.Delete(ctx, child); err != nil {
			return fmt.Errorf("failed to delete child : %w", err)
		}
		log.Info("Child is deleted", "nodename", nodeName, "child", child)
	}

	return nil
}

func getNodeList(ctx context.Context, k8sClient client.Client, selector *metav1.LabelSelector) (*corev1.NodeList, error) {
	nodeList := &corev1.NodeList{}
	nodeSelector, err := utils.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	listOptions := client.ListOptions{
		LabelSelector: nodeSelector,
	}

	if err := k8sClient.List(ctx, nodeList, &listOptions); err != nil {
		return nil, err
	}

	return nodeList, nil
}

//nolint:unparam
func reconcileDelete(ctx context.Context, set client.Object, k8sClient client.Client,
	reconciler serviceSetReconciler, finalizerStr string) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")
	if err := deleteSet(ctx, set, k8sClient, reconciler); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(set, finalizerStr)

	return ctrl.Result{}, nil
}

// reconcileReadinessOfObjectsInDPUCluster handles the readiness reconciliation loop for objects in a specific DPU cluster.
func reconcileReadinessOfObjectsInDPUCluster(
	ctx context.Context,
	r serviceSetReconciler,
	k8sClient client.Client,
	serviceSet client.Object,
) ([]types.NamespacedName, error) {
	objs, err := r.getObjectsInDPUCluster(ctx, k8sClient, serviceSet)
	if err != nil {
		return nil, err
	}
	unreadyObjs, err := r.getUnreadyObjects(objs)
	if err != nil {
		return nil, err
	}

	unreadyObjsNN := []types.NamespacedName{}
	for _, unreadyObj := range unreadyObjs {
		unreadyObjsNN = append(unreadyObjsNN, types.NamespacedName{
			Namespace: unreadyObj.Namespace,
			Name:      unreadyObj.Name,
		})
	}

	// Set the status of applied and ready objects.
	numberApplied := int32(len(objs))
	numberReady := numberApplied - int32(len(unreadyObjsNN))
	r.setReadyStatus(serviceSet, numberApplied, numberReady)

	return unreadyObjsNN, nil
}

// updateSummary updates the status conditions in the object.
func updateSummary(
	ctx context.Context,
	r serviceSetReconciler,
	k8sClient client.Client,
	objReadyCondition conditions.ConditionType,
	serviceSet client.Object,
) error {
	objAsGetSet, ok := serviceSet.(conditions.GetSet)
	if !ok {
		return fmt.Errorf("error while converting object to conditions.GetSet")
	}

	defer conditions.SetSummary(objAsGetSet)
	unreadyObjs, err := reconcileReadinessOfObjectsInDPUCluster(ctx, r, k8sClient, serviceSet)
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
		return nil
	}

	conditions.AddTrue(
		objAsGetSet,
		objReadyCondition,
	)
	return nil
}
