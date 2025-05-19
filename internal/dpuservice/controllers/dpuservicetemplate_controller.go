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

package controllers

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/dpuservice/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dpuServiceTemplateControllerName = "dpuservicetemplatecontroller"
)

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicetemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicetemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicetemplates/status,verbs=get;update;patch

// DPUServiceTemplateReconciler reconciles a DPUServiceTemplate object
type DPUServiceTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// ChartHelper is used to do relevant operations on a chart
	ChartHelper utils.ChartHelper
	// versionsForChart holds the versions for a given chart. The key is a hash of the chart reference in the
	// DPUServiceTemplate. The value is the set of versions for that particular chart.
	versionsForChart map[string]map[string]string
	// namespacedNameToHash holds the hash of the DPUServiceTemplate associated with a particular DPUServiceTemplate.
	// Used so that the versionsForChart is kept up to date.
	namespacedNameToHash map[types.NamespacedName]string
}

// pauseDPUServiceTemplateReconciler pauses the DPUServiceTemplate Reconciler by doing noop reconciliation loops. This
// is helpful to make tests faster and less complex
var pauseDPUServiceTemplateReconciler bool

// SetupWithManager sets up the controller with the Manager.
func (r *DPUServiceTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUServiceTemplate{}).
		Complete(r)
}

// Reconcile reconciles changes in a DPUServiceTemplate object
func (r *DPUServiceTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	if pauseDPUServiceTemplateReconciler {
		log.Info("noop reconciliation")
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling")
	dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuServiceTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	patcher := patch.NewSerialPatcher(dpuServiceTemplate, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		conditions.SetSummary(dpuServiceTemplate)

		if err := patcher.Patch(ctx, dpuServiceTemplate,
			patch.WithFieldOwner(dpuServiceTemplateControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.DPUServiceTemplateConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(dpuServiceTemplate, dpuservicev1.DPUServiceTemplateConditions)

	// Handle deletion reconciliation loop.
	if !dpuServiceTemplate.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuServiceTemplate)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuServiceTemplate, dpuservicev1.DPUServiceTemplateFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuServiceTemplate, dpuservicev1.DPUServiceTemplateFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuServiceTemplate)
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUServiceTemplateReconciler) reconcile(ctx context.Context, dpuServiceTemplate *dpuservicev1.DPUServiceTemplate) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	if r.versionsForChart == nil {
		r.versionsForChart = make(map[string]map[string]string)
	}
	if r.namespacedNameToHash == nil {
		r.namespacedNameToHash = make(map[types.NamespacedName]string)
	}

	versions, found := getVersionsFromMemory(r.namespacedNameToHash, r.versionsForChart, dpuServiceTemplate)
	if !found {
		log.Info("Pulling chart locally")
		annotations, err := r.ChartHelper.GetAnnotationsFromChart(ctx, r.Client, dpuServiceTemplate.Spec.HelmChart.Source)
		if err != nil {
			conditions.AddTrue(dpuServiceTemplate, dpuservicev1.ConditionDPUServiceTemplateReconciled)
			conditions.AddFalse(
				dpuServiceTemplate,
				dpuservicev1.ConditionDPUServiceTemplateReconciled,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
			)
			return ctrl.Result{}, err
		}

		versions = filterVersions(annotations)
	}

	setVersionsInMemory(r.namespacedNameToHash, r.versionsForChart, dpuServiceTemplate, versions)
	dpuServiceTemplate.Status.Versions = versions
	conditions.AddTrue(dpuServiceTemplate, dpuservicev1.ConditionDPUServiceTemplateReconciled)

	return ctrl.Result{}, nil
}

// filterVersions returns the filtered versions from the given unfiltered versions
func filterVersions(unfilteredVersions map[string]string) map[string]string {
	if len(unfilteredVersions) == 0 {
		return unfilteredVersions
	}
	versions := make(map[string]string)
	serviceVersionKeyToBFBVersionValue := GetServiceVersionKeyToBFBVersionValue()
	for annKey, annValue := range unfilteredVersions {
		if _, ok := serviceVersionKeyToBFBVersionValue[annKey]; ok {
			versions[annKey] = annValue
		}
	}
	return versions
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUServiceTemplateReconciler) reconcileDelete(ctx context.Context, dpuServiceTemplate *dpuservicev1.DPUServiceTemplate) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	deleteVersionsFromMemory(r.namespacedNameToHash, r.versionsForChart, dpuServiceTemplate)

	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpuServiceTemplate, dpuservicev1.DPUServiceTemplateFinalizer)
	return ctrl.Result{}, nil
}

// getVersionsFromMemory returns the versions from memory if they are fetched before. The boolean indicates whether they
// were found in memory.
func getVersionsFromMemory(namespacedNameToHash map[types.NamespacedName]string, versionsForChart map[string]map[string]string, dpuServiceTemplate *dpuservicev1.DPUServiceTemplate) (map[string]string, bool) {
	hash, ok := namespacedNameToHash[types.NamespacedName{Name: dpuServiceTemplate.Name, Namespace: dpuServiceTemplate.Namespace}]
	if !ok {
		return nil, false
	}

	newHash := digest.FromObjects(dpuServiceTemplate.Spec.HelmChart.Source)
	if newHash.String() != hash {
		return nil, false
	}

	versions, ok := versionsForChart[hash]
	if !ok {
		return nil, false
	}
	return versions, true
}

// setVersionsInMemory sets the found versions in memory for further reconciliations. It also cleans up the maps from
// stale entries.
func setVersionsInMemory(namespacedNameToHash map[types.NamespacedName]string, versionsForChart map[string]map[string]string, dpuServiceTemplate *dpuservicev1.DPUServiceTemplate, versions map[string]string) {
	namespacedName := types.NamespacedName{Name: dpuServiceTemplate.Name, Namespace: dpuServiceTemplate.Namespace}
	if hash, ok := namespacedNameToHash[namespacedName]; ok {
		delete(versionsForChart, hash)
	}
	hash := digest.FromObjects(dpuServiceTemplate.Spec.HelmChart.Source)
	namespacedNameToHash[namespacedName] = hash.String()
	versionsForChart[hash.String()] = versions
}

// deleteVersionsFromMemory deletes the versions from memory for a resource.
func deleteVersionsFromMemory(namespacedNameToHash map[types.NamespacedName]string, versionsForChart map[string]map[string]string, dpuServiceTemplate *dpuservicev1.DPUServiceTemplate) {
	namespacedName := types.NamespacedName{Name: dpuServiceTemplate.Name, Namespace: dpuServiceTemplate.Namespace}
	if hash, ok := namespacedNameToHash[namespacedName]; ok {
		delete(namespacedNameToHash, namespacedName)
		// If we have another DPUServiceTemplate relying on the same hash, we force the pull of the chart again on the
		// next reconciliation.
		delete(versionsForChart, hash)
	}
}
