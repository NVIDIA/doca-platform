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

package controller

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// PodRestartControllerName is the name of the controller
	PodRestartControllerName = "pod-restart-controller"
)

// PodRestartController reconciles pods on the DPU cluster that need to be restarted due to changes in ServiceChain configuration.
// The controller watches ServiceChains and pods, calculates a digest representing the pod's IPAM and MTU configuration,
// and deletes pods to trigger restarts if the digest on the Pod differs from the calculated one.
type PodRestartController struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles ServiceChain objects to restart pods when network configuration changes
// +kubebuilder:rbac:groups="",resources=pods,verbs=list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=nodeserviceinterfaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=nv-ipam.nvidia.com,resources=ippools,verbs=get;list;watch
// +kubebuilder:rbac:groups=nv-ipam.nvidia.com,resources=cidrpools,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *PodRestartController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Checking ServiceChain for network digest changes")

	serviceChain := &dpuservicev1.ServiceChain{}
	if err := r.Get(ctx, req.NamespacedName, serviceChain); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ServiceChain not found, skipping")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if ServiceChain is being deleted
	if serviceChain.DeletionTimestamp != nil && !serviceChain.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	if err := r.reconcilePods(ctx, serviceChain); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handlePodRestart deletes a pod to trigger a restart.
// The digest annotation has already been updated in reconcilePods, so this function
// only needs to delete the pod to cause the restart.
func (r *PodRestartController) handlePodRestart(ctx context.Context, pod *corev1.Pod) error {
	log := ctrllog.FromContext(ctx)

	// Check if the pod still exists before trying to delete it
	if err := r.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}); err != nil {
		if apierrors.IsNotFound(err) {
			// Pod is already deleted, which is fine
			log.Info("Pod already deleted, skipping", "pod", client.ObjectKeyFromObject(pod))
			return nil
		}
	}

	// The digest annotation has already been updated in reconcilePods
	// Just delete the pod to trigger restart
	if err := r.Delete(ctx, pod); err != nil {
		return fmt.Errorf("error deleting pod for restart: %w", err)
	}

	log.Info("Deleted pod for network digest restart", "pod", client.ObjectKeyFromObject(pod))
	return nil
}

// SetupWithManager sets up the controller with the Manager
// nolint:gocritic // This function is tested via integration tests in suite_test.go
func (r *PodRestartController) SetupWithManager(mgr ctrl.Manager) error {
	// Set up the index for pod lookups by node name
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&corev1.Pod{},
		"spec.nodeName",
		func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName != "" {
				return []string{pod.Spec.NodeName}
			}
			return []string{}
		},
	); err != nil {
		return err
	}

	// The ServiceChain spec.node index is registered by ServiceChainSetReconciler in the same manager.

	// The network digest depends on the ServiceChain spec, the ServiceInterfaces (legacy and NSI entries), the
	// NV-IPAM pools and the Pod. Status writes and resyncs don't change it, so only spec and label changes trigger a
	// reconcile. The Pod watch catches a digest written from a configuration that changed while the write was in flight.
	specOrLabelsChanged := builder.WithPredicates(notInInitialList,
		predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}))
	return ctrl.NewControllerManagedBy(mgr).
		Named(PodRestartControllerName).
		For(&dpuservicev1.ServiceChain{}, builder.WithPredicates(serviceChainPredicate())).
		Watches(&dpuservicev1.ServiceInterface{}, handler.EnqueueRequestsFromMapFunc(r.serviceInterfaceToServiceChains), specOrLabelsChanged).
		Watches(&dpuservicev1.NodeServiceInterfaces{}, handler.EnqueueRequestsFromMapFunc(r.nodeServiceInterfacesToServiceChains),
			builder.WithPredicates(notInInitialList, predicate.GenerationChangedPredicate{})).
		Watches(&nvipamv1.IPPool{}, handler.EnqueueRequestsFromMapFunc(r.poolToServiceChains), specOrLabelsChanged).
		Watches(&nvipamv1.CIDRPool{}, handler.EnqueueRequestsFromMapFunc(r.poolToServiceChains), specOrLabelsChanged).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podToServiceChains), builder.WithPredicates(podDigestInputsChangedPredicate())).
		Complete(r)
}

// notInInitialList drops the create events of an informer's initial list. Workers start once all watches have
// synced, and the ServiceChain watch alone already enqueues every ServiceChain at that point.
var notInInitialList = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return !e.IsInInitialList
	},
}

// podDigestInputsChangedPredicate accepts Pod updates that change the digest annotation or another Pod input of the
// digest. PodIpam sets the digest with a server-side apply on the existing Pod, which arrives as an update, so creates
// are ignored.
func podDigestInputsChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod, newPod := e.ObjectOld.(*corev1.Pod), e.ObjectNew.(*corev1.Pod)
			if newPod.Spec.NodeName == "" {
				return false
			}
			return oldPod.Annotations[NetworkDigestAnnotation] != newPod.Annotations[NetworkDigestAnnotation] ||
				oldPod.Annotations[NetworkAttachmentAnnot] != newPod.Annotations[NetworkAttachmentAnnot] ||
				oldPod.Labels[dpuservicev1.DPFServiceIDLabelKey] != newPod.Labels[dpuservicev1.DPFServiceIDLabelKey]
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

// serviceChainPredicate accepts ServiceChains that have a node, and on update only spec changes.
func serviceChainPredicate() predicate.Predicate {
	hasNode := func(o client.Object) bool {
		return o.(*dpuservicev1.ServiceChain).Spec.Node != nil
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasNode(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasNode(e.ObjectOld) && hasNode(e.ObjectNew) &&
				e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasNode(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasNode(e.Object)
		},
	}
}

// serviceChainsOnNode returns a request for every ServiceChain on nodeName, in namespace if it isn't empty.
func (r *PodRestartController) serviceChainsOnNode(ctx context.Context, nodeName, namespace string) []reconcile.Request {
	opts := []client.ListOption{client.MatchingFields{utils.ServiceChainNodeFieldKey: nodeName}, client.UnsafeDisableDeepCopy}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	serviceChains := &dpuservicev1.ServiceChainList{}
	if err := r.List(ctx, serviceChains, opts...); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list ServiceChains", "node", nodeName, "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(serviceChains.Items))
	for i := range serviceChains.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&serviceChains.Items[i])})
	}
	return requests
}

// podToServiceChains maps a Pod to the ServiceChains in its namespace on its node.
func (r *PodRestartController) podToServiceChains(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*corev1.Pod)
	if pod.Spec.NodeName == "" {
		return nil
	}
	return r.serviceChainsOnNode(ctx, pod.Spec.NodeName, pod.Namespace)
}

// serviceInterfaceToServiceChains maps a ServiceInterface to the ServiceChains in its namespace on its node.
func (r *PodRestartController) serviceInterfaceToServiceChains(ctx context.Context, obj client.Object) []reconcile.Request {
	si := obj.(*dpuservicev1.ServiceInterface)
	if si.Spec.Node == nil {
		return nil
	}
	return r.serviceChainsOnNode(ctx, *si.Spec.Node, si.Namespace)
}

// nodeServiceInterfacesToServiceChains maps a NodeServiceInterfaces shard to all ServiceChains on its node. The
// shard lives in the DPF namespace while its entries belong to ServiceChains in any namespace.
func (r *PodRestartController) nodeServiceInterfacesToServiceChains(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.serviceChainsOnNode(ctx, obj.(*dpuservicev1.NodeServiceInterfaces).Spec.Node, "")
}

// poolToServiceChains maps an IPPool or CIDRPool to the ServiceChains with a node and a port whose IPAM matchLabels
// select the pool.
func (r *PodRestartController) poolToServiceChains(ctx context.Context, obj client.Object) []reconcile.Request {
	poolLabels := labels.Set(obj.GetLabels())
	serviceChains := &dpuservicev1.ServiceChainList{}
	if err := r.List(ctx, serviceChains, client.UnsafeDisableDeepCopy); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list ServiceChains")
		return nil
	}
	var requests []reconcile.Request
	for i := range serviceChains.Items {
		sc := &serviceChains.Items[i]
		if sc.Spec.Node != nil && serviceChainSelectsPool(sc, poolLabels) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(sc)})
		}
	}
	return requests
}

// serviceChainSelectsPool reports whether a port of sc has IPAM matchLabels that select a pool with poolLabels.
func serviceChainSelectsPool(sc *dpuservicev1.ServiceChain, poolLabels labels.Set) bool {
	for _, sw := range sc.Spec.Switches {
		for _, port := range sw.Ports {
			ipam := port.ServiceInterface.IPAM
			if ipam != nil && labels.SelectorFromSet(ipam.MatchLabels).Matches(poolLabels) {
				return true
			}
		}
	}
	return false
}

// reconcilePods finds and reconciles all pods in the namespace that need to be restarted due to network digest changes.
// For each pod, it calculates the current network digest and deletes the pod if the digest has changed from the previously stored value.
func (r *PodRestartController) reconcilePods(ctx context.Context, serviceChain *dpuservicev1.ServiceChain) error {
	namespace := serviceChain.Namespace
	nodeName := *serviceChain.Spec.Node

	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingFields{"spec.nodeName": nodeName},
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		return fmt.Errorf("failed to list pods in namespace %s that are assigned to node %s: %w", namespace, nodeName, err)
	}

	for _, pod := range podList.Items {
		needsRestart, err := r.needsRestartDueToDigestChange(ctx, &pod)
		if err != nil {
			return fmt.Errorf("error checking if pod %s needs restart: %w", client.ObjectKeyFromObject(&pod), err)
		}

		if !needsRestart {
			continue
		}

		if err := r.handlePodRestart(ctx, &pod); err != nil {
			return fmt.Errorf("error handling pod restart: %w", err)
		}
	}

	return nil
}

// shouldProcessPod determines if a pod should be considered for restarting in case of network digest annotation mismatch.
// Returns false if the pod is not running, marked for deletion, or has an invalid network annotation.
func shouldProcessPod(pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) bool {
	_, hasStoredDigest := pod.Annotations[NetworkDigestAnnotation]
	markedForDeletion := pod.DeletionTimestamp != nil && !pod.DeletionTimestamp.IsZero()
	// If the Pod is in Pending state and the invalid network is not removed, it means it's processed by the Pod IPAM injector
	// TODO: Adjust this check once Pod IPAM Injector reads DPUServiceNAD to understand whether IPAM
	// should be used.
	if (pod.Status.Phase == corev1.PodPending && HasInvalidNetwork(networks)) ||
		// If the Pod is marked for deletion, the pod will be removed anyway, we don't need to process
		markedForDeletion ||
		// If the Pod doesn't have a digest, it means the Pod IPAM injector didn't inject one yet or should not inject
		// at all
		!hasStoredDigest ||
		// If the Pod has the invalid network, it means it's processed by the Pod IPAM Injector
		HasInvalidNetwork(networks) {
		return false
	}

	return true
}

// needsRestartDueToDigestChange determines if a pod needs to be restarted due to digest changes. The digest is calculated
// by the podipam controller for ServiceChain related config and is stored in the pod's annotations.
// Returns true if the pod has a stored digest annotation and it differs from the current calculated digest.
// Returns false if the pod has no stored digest (indicating it hasn't been processed by the podipam controller yet)
// or the digest is the same as the calculated one.
func (r *PodRestartController) needsRestartDueToDigestChange(ctx context.Context, pod *corev1.Pod) (bool, error) {
	// Get pod networks for validation
	networks, err := GetPodNetworks(pod)
	if err != nil {
		return false, fmt.Errorf("error getting pod networks: %w", err)
	}

	shouldProcess := shouldProcessPod(pod, networks)
	if !shouldProcess {
		return false, nil
	}

	storedDigest := pod.Annotations[NetworkDigestAnnotation]

	// Calculate the current expected digest for this pod
	currentDigest, err := CalculatePodNetworkDigest(ctx, r.Client, pod, networks)
	if err != nil {
		return false, fmt.Errorf("failed to calculate pod network digest: %w", err)
	}

	// Compare the current digest with the stored digest
	return currentDigest != storedDigest, nil
}
