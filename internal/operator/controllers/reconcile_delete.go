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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var reconcileDeleteRequeueDuration = 30 * time.Second

// The DPFOperatorConfig reconciler is responsible for ensuring that the entire DPF system is uninstalled on deletion.
// This deletion follows a specified order - DPUServices must be fully deleted before the dpuservice-controller is deleted.
func (r *DPFOperatorConfigReconciler) reconcileDelete(ctx context.Context, dpfOperatorConfig *operatorv1.DPFOperatorConfig, dpuclusters []*dpucluster.Config) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	log.Info("Ensuring DPF resources are deleted")
	err := r.deleteDPFResources(ctx)
	if err != nil {
		log.Error(err, "Waiting for DPF resources to be deleted")
		conditions.AddFalse(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition, conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(fmt.Sprintf("System DPUServices awaiting deletion: %s", err)))
		return ctrl.Result{RequeueAfter: reconcileDeleteRequeueDuration}, nil
	}

	log.Info("Ensuring DPF system components are deleted")
	var errs []error
	// Delete objects via the inventory.
	for _, component := range r.Inventory.AllComponents() {
		err := r.deleteSystemComponentViaInventory(ctx, component, inventory.VariablesFromDPFOperatorConfig(r.Defaults, dpfOperatorConfig, dpuclusters))
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		log.Error(kerrors.NewAggregate(errs), "Waiting for system components to be deleted")
		conditions.AddFalse(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition, conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(fmt.Sprintf("System components awaiting deletion: %s", kerrors.NewAggregate(errs).Error())))
		return ctrl.Result{}, kerrors.NewAggregate(errs)
	}

	// Delete objects for components deployed to the management cluster.
	// Note: This includes the inventory itself.
	// Note: This also does a fallback cleanup in case the inventory was messed up. We expect this to normally only delete the inventory itself.
	for _, component := range r.Inventory.AllComponents() {
		err := r.deleteSystemComponent(ctx, component, inventory.VariablesFromDPFOperatorConfig(r.Defaults, dpfOperatorConfig, dpuclusters))
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		log.Error(kerrors.NewAggregate(errs), "Waiting for system components to be deleted")
		conditions.AddFalse(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition, conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(fmt.Sprintf("System components awaiting deletion: %s", kerrors.NewAggregate(errs).Error())))
		return ctrl.Result{}, kerrors.NewAggregate(errs)
	}

	conditions.AddTrue(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition)
	// Delete the objects deployed by the controller.
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpfOperatorConfig, operatorv1.DPFOperatorConfigFinalizer)
	// We should have an ownerReference chain in order to delete subordinate objects.
	return ctrl.Result{}, nil
}

// provisioningResources tracks the provisioning resources which need to be deleted by the DPF Operator.
var provisioningResources = []schema.GroupVersionKind{
	provisioningv1.DPUClusterGroupVersionKind,
	provisioningv1.DPUSetGroupVersionKind,
	provisioningv1.BFBGroupVersionKind,
	provisioningv1.DPUFlavorGroupVersionKind,
	provisioningv1.DPUNodeGroupVersionKind,
	provisioningv1.DPUDeviceGroupVersionKind,
	provisioningv1.DPUDiscoveryGroupVersionKind,
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
	dpuservicev1.DPUServiceNADGroupVersionKind,
}

var dpuDeploymentResources = []schema.GroupVersionKind{
	dpuservicev1.DPUDeploymentGroupVersionKind,
	dpuservicev1.DPUServiceTemplateGroupVersionKind,
	dpuservicev1.DPUServiceConfigurationGroupVersionKind,
}

// nodeResourceResources tracks the resources from the noderesources API group which need to be deleted by the DPF Operator.
var nodeResourceResources = []schema.GroupVersionKind{
	noderesourcesv1.NodeSRIOVDevicePluginConfigGroupVersionKind,
}

var orderedDeleteList = []func(ctx context.Context, c client.Client) error{
	// NodeResource can be deleted first
	deleteNodeResourceResources,
	// ServiceChain objects must be deleted before DPUServices as system DPUServices - e.g. the SFC controller - implement their deletion.
	deleteServiceChainResources,
	// DPUDeployments must be deleted after ServiceChains as those objects may depend on the DPUDeployment owned dpusets.
	deleteDpuDeploymentResources,
	// DPUService objects should be deleted before provisioning objects as their deletion depends on the existence of the DPUCluster.
	deleteDpuServiceResources,
	// Provisioning objects can be deleted last.
	deleteProvisioningResources,
}

func deleteServiceChainResources(ctx context.Context, c client.Client) error {
	return deleteResources(ctx, c, serviceChainResources, []string{dpuservicev1.ParentDPUDeploymentNameLabel})
}

func deleteDpuServiceResources(ctx context.Context, c client.Client) error {
	return deleteResources(ctx, c, dpuserviceResources, []string{dpuservicev1.ParentDPUDeploymentNameLabel})
}

func deleteDpuDeploymentResources(ctx context.Context, c client.Client) error {
	return deleteResources(ctx, c, dpuDeploymentResources, []string{})
}

func deleteProvisioningResources(ctx context.Context, c client.Client) error {
	return deleteResources(ctx, c, provisioningResources, []string{})
}

func deleteNodeResourceResources(ctx context.Context, c client.Client) error {
	return deleteResources(ctx, c, nodeResourceResources, []string{})
}

func deleteResources(ctx context.Context, c client.Client, gvkList []schema.GroupVersionKind, labelExclusionList []string) error {
	var errs []error
	for _, resource := range gvkList {
		// Check if any objects of that type remain in the cluster.
		objListKind := fmt.Sprintf("%vList", resource.Kind)
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(resource.GroupVersion().WithKind(objListKind))
		if err := c.List(ctx, list); err != nil {
			return err
		}

		// If no items exist all are deleted.
		if len(list.Items) == 0 {
			continue
		}

		awaitingDeletion := 0
		for _, obj := range list.Items {
			// Skip objects with labels in the exclusion list.
			if matchLabelExclusionList(obj.GetLabels(), labelExclusionList) {
				continue
			}
			awaitingDeletion++
			if err := c.Delete(ctx, &obj); client.IgnoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}

		if awaitingDeletion > 0 {
			errs = append(errs, fmt.Errorf("%d instances of resource Kind %v still exist. ", awaitingDeletion, resource))
		}
	}
	return kerrors.NewAggregate(errs)
}

func (r *DPFOperatorConfigReconciler) deleteDPFResources(ctx context.Context) error {
	// For each kind of resource controlled by the DPF Operator.
	// The groups of resources must be deleted following this order to ensure deletion is not blocked either by removing
	// controllers required for deletion or recreating objects while deletion is ongoing.
	for _, deleteFunc := range orderedDeleteList {
		err := deleteFunc(ctx, r.Client)
		if err != nil {
			// If there are any errors return before proceeding to the next stage of deletion.
			return err
		}
	}
	return nil
}

func (r *DPFOperatorConfigReconciler) deleteSystemComponentViaInventory(ctx context.Context, component inventory.Component, vars inventory.Variables) error {
	objs, err := r.getCurrentInventoryObjects(ctx, vars.Namespace, component)
	if err != nil {
		return err
	}

	return r.deleteObjsAndWait(ctx, component.Name(), objs)
}

func (r *DPFOperatorConfigReconciler) deleteSystemComponent(ctx context.Context, component inventory.Component, vars inventory.Variables) error {
	objs, err := component.GenerateManifests(ctx, vars)
	if err != nil {
		return fmt.Errorf("%s error generating manifest while attempting deletion: %v", component.Name(), err)
	}

	return r.deleteObjsAndWait(ctx, component.Name(), objs)
}

func (r *DPFOperatorConfigReconciler) deleteObjsAndWait(ctx context.Context, componentName operatorv1.ComponentName, objs []client.Object) error {
	var errs []error
	for _, obj := range objs {
		if err := r.Client.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("%s: error deleting %s %s: %w", componentName, obj.GetObjectKind().GroupVersionKind().Kind, klog.KObj(obj), err))
			continue
		}
		uns := &unstructured.Unstructured{}
		uns.SetKind(obj.GetObjectKind().GroupVersionKind().Kind)
		uns.SetAPIVersion(obj.GetObjectKind().GroupVersionKind().GroupVersion().String())
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(obj), uns)

		// If result is anything other than StatusReasonNotFound return an error even if the error is nil.
		if !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("%s: %s/%s pending deletion", componentName, obj.GetObjectKind().GroupVersionKind().Kind, klog.KObj(obj)))
		}
	}
	return kerrors.NewAggregate(errs)
}

// deleteInventoryObjects deletes all objects which are in the inventory.
func (r *DPFOperatorConfigReconciler) getCurrentInventoryObjects(ctx context.Context, namespace string, component inventory.Component) ([]client.Object, error) {
	currentInventory, err := r.currentInventoryForComponent(ctx, namespace, component)
	if err != nil {
		return nil, err
	}

	objs := []client.Object{}

	for _, obj := range currentInventory {
		gknn, err := inventory.ParseGroupKindNamespaceName(obj)
		if err != nil {
			return nil, err
		}

		mapping, err := r.Client.RESTMapper().RESTMapping(schema.GroupKind{Group: gknn.Group, Kind: gknn.Kind})
		if err != nil {
			return nil, fmt.Errorf("could not find kind for resource from inventory in schema: %w", err)
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(mapping.GroupVersionKind)
		obj.SetNamespace(gknn.Namespace)
		obj.SetName(gknn.Name)

		objs = append(objs, obj)
	}
	return objs, nil
}

// matchLabelExclusionList returns true if any of the labels in the exclusion list are present in the object.
func matchLabelExclusionList(labels map[string]string, labelExclusionList []string) bool {
	if labels == nil {
		return false
	}

	for _, exclude := range labelExclusionList {
		if _, ok := labels[exclude]; ok {
			return true
		}
	}
	return false
}
