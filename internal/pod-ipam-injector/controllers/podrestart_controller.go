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

	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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

	// Create a predicate that only processes ServiceChains with a node specified
	serviceChainPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			serviceChain := e.Object.(*dpuservicev1.ServiceChain)
			return serviceChain.Spec.Node != nil
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldServiceChain := e.ObjectOld.(*dpuservicev1.ServiceChain)
			newServiceChain := e.ObjectNew.(*dpuservicev1.ServiceChain)

			// Process if both old and new have a node specified
			// This is to avoid processing ServiceChains that are not part of a node.
			oldHasNode := oldServiceChain.Spec.Node != nil
			newHasNode := newServiceChain.Spec.Node != nil

			return oldHasNode && newHasNode
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			serviceChain := e.Object.(*dpuservicev1.ServiceChain)
			return serviceChain.Spec.Node != nil
		},
		GenericFunc: func(e event.GenericEvent) bool {
			serviceChain := e.Object.(*dpuservicev1.ServiceChain)
			return serviceChain.Spec.Node != nil
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(PodRestartControllerName).
		For(&dpuservicev1.ServiceChain{}, builder.WithPredicates(serviceChainPredicate)).
		Complete(r)
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
	if pod.Status.Phase == corev1.PodPending || markedForDeletion || !hasStoredDigest || HasInvalidNetwork(networks) {
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
