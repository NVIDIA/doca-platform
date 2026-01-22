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
	"strings"

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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type NodeReconciler struct {
	client.Client
	Options dnutil.HostAgentPodOptions
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			if err = r.handleReconcileDelete(ctx, req.NamespacedName.Name); err != nil {
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

	if !node.DeletionTimestamp.IsZero() {
		if err := r.handleReconcileDelete(ctx, node.Name); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !node.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Check if a DPUNode object for this Node already exists.
	dpuNode := &provisioningv1.DPUNode{}
	err := r.Client.Get(ctx, getDPUNodeKey(node.Name), dpuNode)
	if err == nil {
		// Create a new DMS pod for the upgrade scenario and or DMS pod deleted by mistake.
		if !r.hostAgentPodExists(ctx, node) {
			log.Info("Creating a new DMS Pod for Node", "node", node.Name)
			if err := r.deployHostAgent(ctx, node); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	// If not found, create a new DPUNode object using details from the Node.
	if apierrors.IsNotFound(err) {
		log.Info("Creating a new DMS Pod for Node", "node", node.Name)
		if err := r.deployHostAgent(ctx, node); err != nil {
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
func (r *NodeReconciler) handleReconcileDelete(ctx context.Context, nodeName string) error {
	dpuNode := &provisioningv1.DPUNode{}
	err := r.Client.Get(ctx, getDPUNodeKey(nodeName), dpuNode)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get DPUNode: %w", err)
	}

	// Return early, nothing to do here
	if apierrors.IsNotFound(err) {
		return nil
	}

	// Delete the DPUNode to ensure that we set the deletion timestamp.
	// If DPUNode has finalizers, it will remain with DeletionTimestamp set.
	// If DPUNode has no finalizers, it will be deleted immediately.
	if err := r.Client.Delete(ctx, dpuNode); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete DPUNode: %w", err)
	}
	return nil
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.podToNodeReq)).
		Complete(r)
}

func (r *NodeReconciler) podToNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	pod, ok := resource.(*corev1.Pod)
	if !ok {
		log.Error(fmt.Errorf("resource is not a Pod"), "expected Pod resource")
		return nil
	}
	if pod.Spec.NodeName == "" {
		return nil
	}
	if !strings.HasSuffix(pod.Name, "dms") {
		return nil
	}
	log.Info("Mapping Pod to Node", "pod", pod.Name, "node", pod.Spec.NodeName)
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: pod.Spec.NodeName, Namespace: pod.Namespace}},
	}
}

func (r *NodeReconciler) isDPUEnabled(node *corev1.Node) bool {
	if _, ok := node.ObjectMeta.Labels[cutil.NodeSelectorLabel]; ok {
		return true
	}
	return false
}

func (r *NodeReconciler) hostAgentPodExists(ctx context.Context, node *corev1.Node) bool {
	hostAgentPodName := cutil.GenerateHostAgentPodName(node)
	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return false
	}
	namespace := dpfOperatorConfig.Namespace
	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      hostAgentPodName,
	}
	pod := &corev1.Pod{}
	err = r.Client.Get(ctx, nn, pod)
	return err == nil
}

func (r *NodeReconciler) deployHostAgent(ctx context.Context, node *corev1.Node) error {
	// TODO: change GenerateDMSPodName() - it should be based on DPUNode if exist and on Node if DPUNode doesn't exist
	hostAgentName := cutil.GenerateHostAgentPodName(node)
	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return fmt.Errorf("getting DPFOperatorConfig: %w", err)
	}

	namespace := dpfOperatorConfig.Namespace
	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      hostAgentName,
	}
	pod := &corev1.Pod{}
	err = r.Client.Get(ctx, nn, pod)
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) {
		ownerRef := metav1.NewControllerRef(dpfOperatorConfig, operatorv1.DPFOperatorConfigGroupVersionKind)
		ownerRef.BlockOwnerDeletion = ptr.To(false)

		if err := dms.CreateHostAgentPod(ctx, r.Client, node, r.Options, namespace, ownerRef); err != nil {
			return fmt.Errorf("failed to create HostAgent Pod %s: %w", nn, err)
		}
		return nil
	}
	return fmt.Errorf("failed to get HostAgent Pod %s: %w", nn, err)
}
