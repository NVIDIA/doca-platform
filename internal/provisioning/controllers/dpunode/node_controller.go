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

package dpunode

import (
	"context"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type NodeReconciler struct {
	client.Client
	Options dnutil.DMSPodOptions
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			// TODO: Consider if adding a finalizer on the node object is the way to go to be able to handle that
			// in a more consistent way
			if err := r.reconcileDelete(ctx, req.Name); err != nil {
				return ctrl.Result{}, err
			}
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !r.isDPUEnabled(node) {
		return ctrl.Result{}, nil
	}

	// Check if a DPUNode object for this Node already exists.
	dpuNode := &provisioningv1.DPUNode{}
	err := r.Client.Get(ctx, getDPUNodeKey(node.Name), dpuNode)
	if err == nil {
		return ctrl.Result{}, nil
	}
	// If not found, create a new DPUNode object using details from the Node.
	if apierrors.IsNotFound(err) {
		log.Info("Creating a new DMS Pod for Node", "node", node.Name)
		if err := r.deployDMS(ctx, node); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, err
}

// getDPUNodeObjectKey returns an ObjectKey for the DPUNode associated with a Node
func getDPUNodeKey(node string) client.ObjectKey {
	// TODO: change to check based on DPUNode List and DPUNode.KubeNodeRef
	return types.NamespacedName{
		Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
		Name:      node,
	}
}

// reconcileDelete ensures proper cleanup of resources when a node has been deleted or cannot be found
func (r *NodeReconciler) reconcileDelete(ctx context.Context, node string) error {
	dpuNode := &provisioningv1.DPUNode{}
	err := r.Client.Get(ctx, getDPUNodeKey(node), dpuNode)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get DPUNode: %w", err)
	}

	// Return early, nothing to do here
	if apierrors.IsNotFound(err) {
		return nil
	}

	// First we delete the DPUNode to ensure that we set the deletion timestamp.
	if err := r.Client.Delete(ctx, dpuNode); err != nil {
		return fmt.Errorf("failed to delete DPUNode: %w", err)
	}

	// Then we remove the finalizer that is set by the DPU object. This is to ensure that the DPUSet controller will
	// delete the DPU associated with that DPUNode. The DPU will get in Deleting phase where it will get stuck because
	// it won't be able to find the DPUNode. Even if it could find the DPUNode, there are subsequent calls to the DMS
	// pod which is no longer running on the node as the node is removed, which means that it would fail there. This means:
	// * Removal of a node that never comes back => Leftover DPU in Deleting phase (bug)
	// * Removal of a node that is readded in the cluster => After the node is added, the DPU will continue the deletion
	//   since DMS and new DPUNode will be there and a new DPU will be created.
	dpuNodeNeedsUpdate := controllerutil.RemoveFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer)
	if !dpuNodeNeedsUpdate {
		return nil
	}

	if err := r.Client.Update(ctx, dpuNode); err != nil {
		return fmt.Errorf("failed to update DPUNode: %w", err)
	}

	return nil
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

func (r *NodeReconciler) isDPUEnabled(node *corev1.Node) bool {
	if _, ok := node.ObjectMeta.Labels[cutil.NodeSelectorLabel]; ok {
		return true
	}
	return false
}

func (r *NodeReconciler) deployDMS(ctx context.Context, node *corev1.Node) error {
	// TODO: change GenerateDMSPodName() - it should be based on DPUNode if exist and on Node if DPUNode doesn't exist
	dmsPodName := cutil.GenerateDMSPodName(node.Name)
	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return fmt.Errorf("getting DPFOperatorConfig: %w", err)
	}

	namespace := dpfOperatorConfig.Namespace
	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      dmsPodName,
	}
	pod := &corev1.Pod{}
	err = r.Client.Get(ctx, nn, pod)
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		ownerRef := metav1.NewControllerRef(dpfOperatorConfig, operatorv1.DPFOperatorConfigGroupVersionKind)
		ownerRef.BlockOwnerDeletion = ptr.To(false)

		if err := dms.CreateDMSPod(ctx, r.Client, node, r.Options, namespace, ownerRef); err != nil {
			return fmt.Errorf("failed to create DMS Pod %s: %w", nn, err)
		}
		return nil
	}
	return fmt.Errorf("failed to get DMS Pod %s: %w", nn, err)
}
