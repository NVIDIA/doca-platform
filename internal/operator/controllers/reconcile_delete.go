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

package controller

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/operator/inventory"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// The DPFOperatorConfig reconciler is responsible for ensuring that the entire DPF system is uninstalled on deletion.
// This deletion follows a specified order - DPUServices must be fully deleted before the dpuservice-controller is deleted.
func (r *DPFOperatorConfigReconciler) reconcileDelete(ctx context.Context, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	log.Info("Ensuring DPF resources are deleted")
	err := r.deleteDPFResources(ctx)
	if err != nil {
		log.Error(err, "Waiting for DPF resources to be deleted")
		conditions.AddFalse(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition, conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(fmt.Sprintf("System DPUServices awaiting deletion: %s", err)))
		return err
	}

	log.Info("Ensuring DPF system components are deleted")
	var errs []error
	// Delete objects for components deployed to the management cluster.
	for _, component := range r.Inventory.AllComponents() {
		err := r.deleteSystemComponent(ctx, component, inventory.VariablesFromDPFOperatorConfig(r.Defaults, dpfOperatorConfig))
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		log.Error(kerrors.NewAggregate(errs), "Waiting for system components to be deleted")
		conditions.AddFalse(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition, conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(fmt.Sprintf("System components awaiting deletion: %s", kerrors.NewAggregate(errs).Error())))
		return kerrors.NewAggregate(errs)
	}

	conditions.AddTrue(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition)
	// Delete the objects deployed by the controller.
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpfOperatorConfig, operatorv1.DPFOperatorConfigFinalizer)
	// We should have an ownerReference chain in order to delete subordinate objects.
	return nil
}

// provisioningResources tracks the provisioning resources which need to be deleted by the DPF Operator.
var provisioningResources = []schema.GroupVersionKind{
	provisioningv1.DPUClusterGroupVersionKind,
	provisioningv1.DPUSetGroupVersionKind,
	provisioningv1.BFBGroupVersionKind,
	provisioningv1.DPUFlavorGroupVersionKind,
}

// serviceChainResources tracks the DPUService resources which need to be deleted by the DPF Operator.
var dpuserviceResources = []schema.GroupVersionKind{
	dpuservicev1.DPUServiceGroupVersionKind,
	dpuservicev1.DPUServiceCredentialRequestGroupVersionKind,
}

// serviceChainResources tracks the ServiceChain resources which need to be deleted by the DPF Operator.
var serviceChainResources = []schema.GroupVersionKind{
	dpuservicev1.DPUServiceChainGroupVersionKind,
	dpuservicev1.DPUServiceInterfaceGroupVersionKind,
	dpuservicev1.DPUServiceIPAMGroupVersionKind,
}

var dpuDeploymentResources = []schema.GroupVersionKind{
	dpuservicev1.DPUDeploymentGroupVersionKind,
}
var orderedDeleteList = [][]schema.GroupVersionKind{
	// DPUDeployments must be deleted first to ensure the DPUDeployment controller does not recreate objects.
	dpuDeploymentResources,
	// ServiceChain objects must be deleted before DPUServices as system DPUServices - e.g. the SFC controller - implement their deletion.
	serviceChainResources,
	// DPUService objects should be deleted before provisioning objects as their deletion depends on the existence of the DPUCluster.
	dpuserviceResources,
	// Provisioning objects can be deleted last.
	provisioningResources,
}

func (r *DPFOperatorConfigReconciler) deleteDPFResources(ctx context.Context) error {

	// For each kind of resource controlled by the DPF Operator.
	// The groups of resources must be deleted following this order to ensure deletion is not blocked either by removing
	// controllers required for deletion or recreating objects while deletion is ongoing.
	for _, gvkList := range orderedDeleteList {
		var errs []error
		for _, resource := range gvkList {
			// Check if any objects of that type remain in the cluster.
			objListKind := fmt.Sprintf("%vList", resource.Kind)
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(resource.GroupVersion().WithKind(objListKind))
			if err := r.Client.List(ctx, list); err != nil {
				return err
			}

			// If no items exist all are deleted.
			if len(list.Items) == 0 {
				continue
			}
			for _, obj := range list.Items {
				errs = append(errs, fmt.Errorf("%d instances of resource Kind %v still exist. ", len(list.Items), resource))
				if err := r.Client.Delete(ctx, &obj); client.IgnoreNotFound(err) != nil {
					errs = append(errs, err)
				}
			}
		}
		// If there are any errors return before proceeding to the next stage of deletion.
		if len(errs) > 0 {
			return kerrors.NewAggregate(errs)
		}
	}
	return nil
}

func (r *DPFOperatorConfigReconciler) deleteSystemComponent(ctx context.Context, component inventory.Component, vars inventory.Variables) error {
	objs, err := component.GenerateManifests(vars)
	if err != nil {
		return fmt.Errorf("%s error generating manifest while attempting deletion: %v", component.Name(), err)
	}
	var errs []error
	for _, obj := range objs {
		if err := r.Client.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("%s: error deleting %s %s: %w", component.Name(), obj.GetObjectKind().GroupVersionKind().Kind, klog.KObj(obj), err))
			continue
		}
		uns := &unstructured.Unstructured{}
		uns.SetKind(obj.GetObjectKind().GroupVersionKind().Kind)
		uns.SetAPIVersion(obj.GetObjectKind().GroupVersionKind().GroupVersion().String())
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(obj), uns)

		// If result is anything other than StatusReasonNotFound return an error even if the error is nil.
		if !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("%s: %s/%s pending deletion", component.Name(), obj.GetObjectKind().GroupVersionKind().Kind, klog.KObj(obj)))
		}
	}
	return kerrors.NewAggregate(errs)
}
