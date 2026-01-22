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
	"github.com/nvidia/doca-platform/internal/release"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
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

	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("get DPFOperatorConfig: %w", err)
	}

	// Delete all DMS pods at startup for upgrade from 25.7 to 25.10
	if err := r.deleteDMSPods(ctx, dpfOperatorConfig); err != nil {
		log.Error(err, "failed to delete DMS pods at startup")
		// Continue with initialization even if DMS pod deletion fails
		return ctrl.Result{}, fmt.Errorf("delete old DMS Pods: %v", err)
	}
	if err := r.deployHostAgent(ctx, dpfOperatorConfig, node); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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

// deleteDMSPods deletes all DMS pods at controller startup for upgrade from 25.7 to 25.10
func (r *NodeReconciler) deleteDMSPods(ctx context.Context, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	log := ctrllog.FromContext(ctx)

	namespace := dpfOperatorConfig.Namespace
	// List DMS and hostagent pods using label selector
	requirement, err := labels.NewRequirement(
		cutil.ProvisioningComponentLabelKey,
		selection.In,
		[]string{"dms", "hostagent"},
	)
	if err != nil {
		log.Error(err, "Failed to create label requirement")
		return err
	}

	podList := &corev1.PodList{}
	if err := r.Client.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{
			Selector: labels.SelectorFromSet(labels.Set{}).Add(*requirement),
		},
	); err != nil {
		return fmt.Errorf("list DMS Pods: %v", err)
	}

	// Delete all DMS Pods that has an old version
	var errs []error
	for _, pod := range podList.Items {
		// TODO: also verify that the pod.Spec didn't change. Will be probably fixed by using DaemonSet for DMS pods.
		// Skip if the Pod has been deployed by the correct DPF version already.
		version, ok := pod.Labels[release.DPFVersionLabelKey]
		if ok && version == release.DPFVersion() {
			continue
		}

		// Skip if the Pod is already in termination.
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}

		log.Info("Deleting DMS pod", "pod", pod.Name, "namespace", pod.Namespace)
		if err := r.Client.Delete(ctx, &pod); err != nil {
			log.Error(err, "Failed to delete DMS pod", "pod", pod.Name, "namespace", pod.Namespace)
			errs = append(errs, err)
			continue
		}
	}

	return kerrors.NewAggregate(errs)
}

func (r *NodeReconciler) deployHostAgent(ctx context.Context, dpfOperatorConfig *operatorv1.DPFOperatorConfig, node *corev1.Node) error {
	log := ctrllog.FromContext(ctx)
	// TODO: change GenerateDMSPodName() - it should be based on DPUNode if exist and on Node if DPUNode doesn't exist
	hostAgentName := cutil.GenerateHostAgentPodName(node)

	namespace := dpfOperatorConfig.Namespace
	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      hostAgentName,
	}
	pod := &corev1.Pod{}
	err := r.Client.Get(ctx, nn, pod)
	if err == nil {
		if !pod.DeletionTimestamp.IsZero() {
			return fmt.Errorf("HostAgent Pod %s/%s is terminating", nn.Namespace, nn.Name)
		}
		return nil
	}
	if apierrors.IsNotFound(err) {
		log.Info("Creating a new DMS Pod for Node", "node", node.Name)
		ownerRef := metav1.NewControllerRef(dpfOperatorConfig, operatorv1.DPFOperatorConfigGroupVersionKind)
		ownerRef.BlockOwnerDeletion = ptr.To(false)

		if err := dms.CreateHostAgentPod(ctx, r.Client, node, r.Options, namespace, ownerRef); err != nil {
			return fmt.Errorf("failed to create HostAgent Pod %s: %w", nn, err)
		}
		return nil
	}
	return fmt.Errorf("failed to get HostAgent Pod %s: %w", nn, err)
}
