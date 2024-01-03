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

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/operator/inventory"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	vars := inventory.VariablesFromDPFOperatorConfig(r.Defaults, dpfOperatorConfig)

	log.Info("Ensuring DPF system DPUServices are deleted")
	var errs []error

	// Delete DPUServices for system components deployed to the DPU cluster.
	for _, dpuService := range r.Inventory.SystemDPUServices() {
		err := r.deleteObjects(ctx, dpuService, vars)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		log.Error(kerrors.NewAggregate(errs), "Waiting for System DPUServices to be deleted")
		conditions.AddFalse(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition, conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(fmt.Sprintf("System DPUServices awaiting deletion: %s", kerrors.NewAggregate(errs))))
		return kerrors.NewAggregate(errs)
	}

	log.Info("Ensuring DPF system components are deleted")
	// Delete objects for components deployed to the management cluster.
	for _, component := range r.Inventory.AllComponents() {
		err := r.deleteObjects(ctx, component, vars)
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

func (r *DPFOperatorConfigReconciler) deleteObjects(ctx context.Context, component inventory.Component, vars inventory.Variables) error {
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
