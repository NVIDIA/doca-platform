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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dpuDeploymentControllerName = "dpudeploymentcontroller"
	// dpuServiceChainVersionLabelKey is the key for the DPUServiceChain version that is used as labels on DPUSets and NodeSelector in DPUServiceChains.
	// It is also used to discover the most current DPUServiceChain as annotations on DPUServiceChain objects.
	dpuServiceChainVersionLabelAnnotationKey = "svc.dpu.nvidia.com/dpuservicechain-version"

	// ServiceInterfaceInterfaceNameLabel label identifies a specific interface of a DPUService.
	ServiceInterfaceInterfaceNameLabel = "svc.dpu.nvidia.com/interface"
	// dpuServiceVersionAnnotationKey is the key for the version of the DPUService object used to discover the most current DPUService.
	dpuServiceVersionAnnotationKey = "svc.dpu.nvidia.com/dpuservice-version"
	// inClusterDPUServiceVersionLabelKeyPrefix is the prefix of the label key for an in-cluster DPUService version.
	inClusterDPUServiceVersionLabelKeyPrefix = "svc.dpu.nvidia.com/dpuservice-in-cluster-version"

	// resourceNameGeneratedSuffixLength is the length of the random string used to generate unique names
	resourceNameGeneratedSuffixLength = 5
)

// pauseDPUDeploymentReconciler pauses the DPUDeployment Reconciler by doing noop reconciliation loops. This is helpful
// to make tests faster and less complex
var pauseDPUDeploymentReconciler atomic.Bool

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpudeployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpudeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpudeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bfbs;dpuflavors,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bfbs/finalizers;dpuflavors/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceconfigurations;dpuservicetemplates,verbs=get;list;watch;update;patch

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicechains;dpuserviceinterfaces,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices,verbs=get;list;watch;create;update;patch;delete;deletecollection

// DPUDeploymentReconciler reconciles a DPUDeployment object
type DPUDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var reconcileRequeueDuration atomic.Int64

func init() {
	reconcileRequeueDuration.Store(int64(30 * time.Second))
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUDeployment{}).
		// Dependencies
		Watches(&provisioningv1.BFB{}, handler.EnqueueRequestsFromMapFunc(r.BFBToDPUDeployment)).
		Watches(&provisioningv1.DPUFlavor{}, handler.EnqueueRequestsFromMapFunc(r.DPUFlavorToDPUDeployment)).
		Watches(&dpuservicev1.DPUServiceConfiguration{}, handler.EnqueueRequestsFromMapFunc(r.DPUServiceConfigurationToDPUDeployment)).
		Watches(&dpuservicev1.DPUServiceTemplate{}, handler.EnqueueRequestsFromMapFunc(r.DPUServiceTemplateToDPUDeployment)).
		// Child objects
		Owns(&dpuservicev1.DPUService{}).
		Owns(&dpuservicev1.DPUServiceChain{}).
		Owns(&dpuservicev1.DPUServiceInterface{}).
		Owns(&provisioningv1.DPUSet{}).
		Complete(r)
}

// DPUServiceConfigurationToDPUDeployment returns the DPUDeployments associated with a DPUServiceConfiguration
func (r *DPUDeploymentReconciler) DPUServiceConfigurationToDPUDeployment(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuDeploymentList := &dpuservicev1.DPUDeploymentList{}
	if err := r.Client.List(ctx, dpuDeploymentList, client.InNamespace(o.GetNamespace())); err != nil {
		return nil
	}

	for _, dpuDeployment := range dpuDeploymentList.Items {
		for _, opt := range dpuDeployment.Spec.Services {
			if opt.ServiceConfiguration == o.GetName() {
				result = append(result, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&dpuDeployment)})
				break
			}
		}
	}

	return result
}

// DPUServiceTemplateToDPUDeployment returns the DPUDeployments associated with a DPUServiceTemplate
func (r *DPUDeploymentReconciler) DPUServiceTemplateToDPUDeployment(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuDeploymentList := &dpuservicev1.DPUDeploymentList{}
	if err := r.Client.List(ctx, dpuDeploymentList, client.InNamespace(o.GetNamespace())); err != nil {
		return nil
	}

	for _, dpuDeployment := range dpuDeploymentList.Items {
		for _, opt := range dpuDeployment.Spec.Services {
			if opt.ServiceTemplate == o.GetName() {
				result = append(result, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&dpuDeployment)})
				break
			}
		}
	}

	return result
}

// BFBToDPUDeployment returns the DPUDeployments associated with a BFB
func (r *DPUDeploymentReconciler) BFBToDPUDeployment(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuDeploymentList := &dpuservicev1.DPUDeploymentList{}
	if err := r.Client.List(ctx, dpuDeploymentList, client.InNamespace(o.GetNamespace())); err != nil {
		return nil
	}

	for _, dpuDeployment := range dpuDeploymentList.Items {
		if dpuDeployment.Spec.DPUs.BFB == o.GetName() {
			result = append(result, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&dpuDeployment)})
		}
	}

	return result
}

// DPUFlavorToDPUDeployment returns the DPUDeployments associated with a DPUFlavor
func (r *DPUDeploymentReconciler) DPUFlavorToDPUDeployment(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuDeploymentList := &dpuservicev1.DPUDeploymentList{}
	if err := r.Client.List(ctx, dpuDeploymentList, client.InNamespace(o.GetNamespace())); err != nil {
		return nil
	}

	for _, dpuDeployment := range dpuDeploymentList.Items {
		if dpuDeployment.Spec.DPUs.Flavor == o.GetName() {
			result = append(result, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&dpuDeployment)})
		}
	}

	return result
}

// dpuDeploymentDependencies is a struct that holds the parsed dependencies a DPUDeployment has.
type dpuDeploymentDependencies struct {
	BFB                      *provisioningv1.BFB
	DPUFlavor                *provisioningv1.DPUFlavor
	DPUServiceConfigurations map[string]*dpuservicev1.DPUServiceConfiguration
	DPUServiceTemplates      map[string]*dpuservicev1.DPUServiceTemplate
}

// Reconcile reconciles changes in a DPUDeployment object
func (r *DPUDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	if pauseDPUDeploymentReconciler.Load() {
		log.Info("noop reconciliation")
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling")

	dpuDeployment := &dpuservicev1.DPUDeployment{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	patcher := patch.NewSerialPatcher(dpuDeployment, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		if err := r.updateSummary(ctx, dpuDeployment); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}

		if err := patcher.Patch(ctx, dpuDeployment,
			patch.WithFieldOwner(dpuDeploymentControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.DPUDeploymentConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(dpuDeployment, dpuservicev1.DPUDeploymentConditions)

	// Handle deletion reconciliation loop.
	if !dpuDeployment.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuDeployment)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuDeployment, dpuservicev1.DPUDeploymentFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuDeployment, dpuservicev1.DPUDeploymentFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuDeployment)
}

// reconcile handles the main reconciliation loop
// TODO: Remove nolint if we ever return different result
//
//nolint:unparam
func (r *DPUDeploymentReconciler) reconcile(ctx context.Context, dpuDeployment *dpuservicev1.DPUDeployment) (ctrl.Result, error) {
	// dpuNodeLabels is the set of labels that will be applied to the nodes in the DPU cluster that this DPUDeployment
	// adds. This map is supposed to be mutated by the functions that consume it.
	// These labels are needed to perform the upgrade synchronization logic across the child objects the DPUDeployment
	// creates.
	dpuNodeLabels := make(map[string]string)

	deps, err := prepareDependencies(ctx, r.Client, dpuDeployment)
	if err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionPreReqsReady,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while preparing the DPUDeployment dependencies: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionPreReqsReady)

	if err := verifyVersionMatching(deps); err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionVersionMatchingReady,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while verifying that versions are matching: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionVersionMatchingReady)

	if err := verifyResourceFitting(deps); err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionResourceFittingReady,
			// We add failure as state here because we need the user to create new DPUFlavor or DPUServiceTemplate
			// (which are immutable) to fit the resources and update the DPUDeployment accordingly. The system won't
			// be able to recover on its own if it's in that state.
			conditions.ReasonFailure,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while verifying that resources can fit: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionResourceFittingReady)

	interfaceNameByServiceName, err := constructDPUServiceInterfaceNames(ctx, r.Client, dpuDeployment, deps)
	if err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionDPUServiceInterfacesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while constructing the DPUServiceInterfaces names: %w", err)
	}

	requeue, patchedDPUServices, hasDisruptiveServices, err := reconcileDPUServices(ctx, r.Client, dpuDeployment, deps, interfaceNameByServiceName, dpuNodeLabels)
	if err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionDPUServicesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while reconciling the DPUServices: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionDPUServicesReconciled)

	req, hasDisruptiveInterfaces, err := reconcileDPUServiceInterfaces(ctx, r.Client, r.Scheme, dpuDeployment, deps, interfaceNameByServiceName, dpuNodeLabels, patchedDPUServices)
	if err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionDPUServiceInterfacesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while reconciling the DPUServiceInterfaces: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionDPUServiceInterfacesReconciled)

	if !req.IsZero() {
		requeue = req
	}

	req, hasDisruptiveChains, err := reconcileDPUServiceChain(ctx, r.Client, dpuDeployment, dpuNodeLabels)
	if err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionDPUServiceChainsReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while reconciling the DPUServiceChain: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionDPUServiceChainsReconciled)

	if !req.IsZero() {
		requeue = req
	}

	// Aggregate disruptive changes from all object types
	hasDisruptiveChanges := hasDisruptiveServices || hasDisruptiveInterfaces || hasDisruptiveChains

	if err := reconcileDPUSets(ctx, r.Client, dpuDeployment, dpuNodeLabels, hasDisruptiveChanges); err != nil {
		conditions.AddFalse(
			dpuDeployment,
			dpuservicev1.ConditionDPUSetsReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, fmt.Errorf("error while reconciling the DPUSets: %w", err)
	}
	conditions.AddTrue(dpuDeployment, dpuservicev1.ConditionDPUSetsReconciled)

	return requeue, nil
}

// prepareDependencies prepares the DPUDeployment dependencies and returns them for the rest of the reconciliation loop
// to take effect
func prepareDependencies(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment) (*dpuDeploymentDependencies, error) {
	deps, err := getDependencies(ctx, c, dpuDeployment)
	if err != nil {
		return nil, fmt.Errorf("error while getting the DPUDeployment dependencies: %w", err)
	}

	if err := updateDependencies(ctx, c, dpuDeployment, deps); err != nil {
		return nil, fmt.Errorf("error while marking DPUDeployment dependencies: %w", err)
	}

	return deps, nil
}

// updateDependencies marks the dependencies with a label and a finalizer to ensure that they are not deleted until the
// DPUDeployment is deleted. It also takes care of cleaning up stale dependencies so that they can be removed.
func updateDependencies(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment, deps *dpuDeploymentDependencies) error {
	if err := markAllCurrentDependencies(ctx, c, dpuDeployment, deps); err != nil {
		return fmt.Errorf("error while marking all current DPUDeployment dependencies: %w", err)
	}

	if err := cleanAllStaleDependencies(ctx, c, dpuDeployment, deps); err != nil {
		return fmt.Errorf("error while cleaning up identifiers from all stale DPUDeployment dependencies: %w", err)
	}

	return nil
}

// markAllCurrentDependencies marks all the current dependencies with the correct identifiers
func markAllCurrentDependencies(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment, deps *dpuDeploymentDependencies) error {
	patcher := patch.NewSerialPatcher(deps.BFB, c)
	markDependency(deps.BFB, dpuDeployment)

	if err := patcher.Patch(ctx, deps.BFB, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", deps.BFB.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(deps.BFB), err)
	}

	patcher = patch.NewSerialPatcher(deps.DPUFlavor, c)
	markDependency(deps.DPUFlavor, dpuDeployment)
	if err := patcher.Patch(ctx, deps.DPUFlavor, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", deps.DPUFlavor.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(deps.DPUFlavor), err)
	}

	for _, serviceConfig := range deps.DPUServiceConfigurations {
		patcher = patch.NewSerialPatcher(serviceConfig, c)
		markDependency(serviceConfig, dpuDeployment)

		if err := patcher.Patch(ctx, serviceConfig, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
			return fmt.Errorf("error while patching %s %s: %w", serviceConfig.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(serviceConfig), err)
		}
	}
	for _, serviceTemplate := range deps.DPUServiceTemplates {
		patcher = patch.NewSerialPatcher(serviceTemplate, c)
		markDependency(serviceTemplate, dpuDeployment)

		if err := patcher.Patch(ctx, serviceTemplate, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
			return fmt.Errorf("error while patching %s %s: %w", serviceTemplate.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(serviceTemplate), err)
		}
	}
	return nil
}

// cleanAllStaleDependencies cleans all the identifiers from the stale DPUDeployment dependencies
func cleanAllStaleDependencies(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment, deps *dpuDeploymentDependencies) error {
	for _, obj := range []client.ObjectList{
		&dpuservicev1.DPUServiceConfigurationList{},
		&dpuservicev1.DPUServiceTemplateList{},
		&provisioningv1.BFBList{},
		&provisioningv1.DPUFlavorList{},
	} {
		if err := c.List(ctx,
			obj,
			client.InNamespace(dpuDeployment.Namespace),
			client.MatchingLabels{
				dpuDeployment.GetDependentLabelKey(): dpuservicev1.DependentDPUDeploymentLabelValue,
			},
		); err != nil {
			return fmt.Errorf("error while listing %T: %w", obj, err)
		}
		switch t := obj.(type) {
		case *dpuservicev1.DPUServiceConfigurationList:
			objs := obj.(*dpuservicev1.DPUServiceConfigurationList).Items
			for _, dpuServiceConfiguration := range objs {
				if currentDPUServiceConfigurationDependency, ok := deps.DPUServiceConfigurations[dpuServiceConfiguration.Spec.DeploymentServiceName]; ok {
					if currentDPUServiceConfigurationDependency.Name == dpuServiceConfiguration.Name {
						continue
					}
				}
				patcher := patch.NewSerialPatcher(&dpuServiceConfiguration, c)
				unmarkDependency(dpuDeployment, &dpuServiceConfiguration)

				if err := patcher.Patch(ctx, &dpuServiceConfiguration, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", dpuServiceConfiguration.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuServiceConfiguration), err)
				}
			}
		case *dpuservicev1.DPUServiceTemplateList:
			objs := obj.(*dpuservicev1.DPUServiceTemplateList).Items
			for _, dpuServiceTemplate := range objs {
				if currentDPUServiceTemplateDependency, ok := deps.DPUServiceTemplates[dpuServiceTemplate.Spec.DeploymentServiceName]; ok {
					if currentDPUServiceTemplateDependency.Name == dpuServiceTemplate.Name {
						continue
					}
				}
				patcher := patch.NewSerialPatcher(&dpuServiceTemplate, c)
				unmarkDependency(dpuDeployment, &dpuServiceTemplate)

				if err := patcher.Patch(ctx, &dpuServiceTemplate, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", dpuServiceTemplate.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuServiceTemplate), err)
				}
			}
		case *provisioningv1.BFBList:
			objs := obj.(*provisioningv1.BFBList).Items
			for _, bfb := range objs {
				if bfb.Name == deps.BFB.Name {
					continue
				}
				patcher := patch.NewSerialPatcher(&bfb, c)
				unmarkDependency(dpuDeployment, &bfb)

				if err := patcher.Patch(ctx, &bfb, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", bfb.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&bfb), err)
				}
			}
		case *provisioningv1.DPUFlavorList:
			objs := obj.(*provisioningv1.DPUFlavorList).Items
			for _, dpuFlavor := range objs {
				if dpuFlavor.Name == deps.DPUFlavor.Name {
					continue
				}
				patcher := patch.NewSerialPatcher(&dpuFlavor, c)
				unmarkDependency(dpuDeployment, &dpuFlavor)

				if err := patcher.Patch(ctx, &dpuFlavor, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", dpuFlavor.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuFlavor), err)
				}
			}
		default:
			panic(fmt.Sprintf("type %v not handled", t))
		}
	}
	return nil
}

// markDependency marks the object as a dependency to the given DPUDeployment
func markDependency(o client.Object, dpuDeployment *dpuservicev1.DPUDeployment) {
	controllerutil.AddFinalizer(o, dpuservicev1.DPUDeploymentFinalizer)
	labels := o.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[dpuDeployment.GetDependentLabelKey()] = dpuservicev1.DependentDPUDeploymentLabelValue
	o.SetLabels(labels)
}

// unmarkDependency removes the identifiers for a dependency that is no longer referenced in the DPUDeployment
func unmarkDependency(dpuDeployment *dpuservicev1.DPUDeployment, o client.Object) {
	labels := o.GetLabels()
	delete(labels, dpuDeployment.GetDependentLabelKey())
	o.SetLabels(labels)

	for k := range labels {
		if strings.HasPrefix(k, dpuservicev1.DependentDPUDeploymentLabelKeyPrefix) {
			return
		}
	}
	controllerutil.RemoveFinalizer(o, dpuservicev1.DPUDeploymentFinalizer)
}

// verifyResourceFitting verifies that the user provided resources for DPUServices can fit the resources defined in the
// DPUFlavor
func verifyResourceFitting(dependencies *dpuDeploymentDependencies) error {
	availableResources, err := dpfutils.GetAllocatableResources(dependencies.DPUFlavor.Spec.DPUResources, dependencies.DPUFlavor.Spec.SystemReservedResources)
	if err != nil {
		return fmt.Errorf("can't calculate available DPU resources: %w", err)
	}

	if availableResources == nil {
		return nil
	}

	requestedResources := make(corev1.ResourceList)
	for _, dpuServiceTemplate := range dependencies.DPUServiceTemplates {
		for resourceName, requiredQuantity := range dpuServiceTemplate.Spec.ResourceRequirements {
			requestedResource := resource.Quantity{}
			if resource, ok := requestedResources[resourceName]; ok {
				requestedResource = resource
			}

			requestedResource.Add(requiredQuantity)
			requestedResources[resourceName] = requestedResource
		}
	}

	_, err = dpfutils.GetAllocatableResources(availableResources, requestedResources)
	if err != nil {
		e := &dpfutils.ResourcesExceedError{}
		if errors.As(err, &e) {
			return fmt.Errorf("there are not enough resources for DPUServices to fit in this DPUDeployment:  Additional resources needed: %v", e.AdditionalResourcesRequired)
		}
		return fmt.Errorf("can't calculate if requested resources fit: %w", err)
	}

	return nil
}

// verifyVersionMatching verifies that the user provided BFB and DPUServices have versions that match
func verifyVersionMatching(dependencies *dpuDeploymentDependencies) error {
	var errs []error
	for _, dpuServiceTemplate := range dependencies.DPUServiceTemplates {
		for dpuServiceVersionKey, bfbVersionFunc := range GetServiceVersionKeyToBFBVersionValue() {
			bfbValue, err := semver.NewVersion(bfbVersionFunc(dependencies.BFB))
			if err != nil {
				errs = append(errs, fmt.Errorf("version '%s' found in bfb is not parsable: %w", bfbVersionFunc(dependencies.BFB), err))
				continue
			}

			if dpuServiceTemplate.Status.Versions == nil {
				continue
			}

			serviceVersionConstraint, ok := dpuServiceTemplate.Status.Versions[dpuServiceVersionKey]
			if !ok {
				continue
			}

			dpuServiceTemplateValue, err := semver.NewConstraint(serviceVersionConstraint)
			if err != nil {
				errs = append(errs, fmt.Errorf("version constraint '%s' found in DPUServiceTemplate '%s' is not parsable: %w", serviceVersionConstraint, dpuServiceTemplate.Name, err))
				continue
			}

			if !dpuServiceTemplateValue.Check(bfbValue) {
				errs = append(errs, fmt.Errorf("version constraint for '%s' found in DPUServiceTemplate '%s' is not satisfied by the version '%s' found in the given BFB", dpuServiceVersionKey, dpuServiceTemplate.Name, bfbVersionFunc(dependencies.BFB)))
				continue
			}
		}
	}

	return kerrors.NewAggregate(errs)
}

// getDependencies gets the DPUDeployment dependencies from the Kubernetes API Server.
func getDependencies(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment) (*dpuDeploymentDependencies, error) {
	deps := &dpuDeploymentDependencies{
		DPUServiceConfigurations: make(map[string]*dpuservicev1.DPUServiceConfiguration),
		DPUServiceTemplates:      make(map[string]*dpuservicev1.DPUServiceTemplate),
	}

	bfb := &provisioningv1.BFB{}
	key := client.ObjectKey{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Spec.DPUs.BFB}
	if err := c.Get(ctx, key, bfb); err != nil {
		return deps, fmt.Errorf("error while getting %s %s: %w", provisioningv1.BFBGroupVersionKind.String(), key.String(), err)
	}
	if bfb.Status.Phase != provisioningv1.BFBReady {
		return deps, errors.New("BFB is not ready yet")
	}
	deps.BFB = bfb

	dpuFlavor := &provisioningv1.DPUFlavor{}
	key = client.ObjectKey{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Spec.DPUs.Flavor}
	if err := c.Get(ctx, key, dpuFlavor); err != nil {
		return deps, fmt.Errorf("error while getting %s %s: %w", provisioningv1.DPUFlavorGroupVersionKind.String(), key.String(), err)
	}
	deps.DPUFlavor = dpuFlavor

	for service, config := range dpuDeployment.Spec.Services {
		serviceTemplate := &dpuservicev1.DPUServiceTemplate{}
		key := client.ObjectKey{Namespace: dpuDeployment.Namespace, Name: config.ServiceTemplate}
		if err := c.Get(ctx, key, serviceTemplate); err != nil {
			return deps, fmt.Errorf("error while getting %s %s: %w", dpuservicev1.DPUServiceTemplateGroupVersionKind.String(), key.String(), err)
		}
		if serviceTemplate.Spec.DeploymentServiceName != service {
			return deps, fmt.Errorf("service in DPUServiceTemplate %s doesn't match requested service in DPUDeployment", key.String())
		}
		// We can't check against the ObservedGeneration of the individual Ready condition because flux patcher patches
		// first the status conditions, then the spec and then the status of an object, meaning that we may have a
		// race condition where the conditions are updated, the versions in status field is outdated => version matching
		// passes on the first reconcile => service is updated => disruption.
		if !conditions.IsTrue(serviceTemplate, conditions.TypeReady) || serviceTemplate.Generation != serviceTemplate.Status.ObservedGeneration {
			return deps, fmt.Errorf("DPUServiceTemplate %s is not ready yet", key.String())
		}
		deps.DPUServiceTemplates[service] = serviceTemplate

		serviceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
		key = client.ObjectKey{Namespace: dpuDeployment.Namespace, Name: config.ServiceConfiguration}
		if err := c.Get(ctx, key, serviceConfiguration); err != nil {
			return deps, fmt.Errorf("error while getting %s %s: %w", dpuservicev1.DPUServiceConfigurationGroupVersionKind.String(), key.String(), err)
		}
		if serviceConfiguration.Spec.DeploymentServiceName != service {
			return deps, fmt.Errorf("service in DPUServiceConfiguration %s doesn't match requested service in DPUDeployment", key.String())
		}
		deps.DPUServiceConfigurations[service] = serviceConfiguration
	}

	return deps, nil
}

// reconcileDPUSets reconciles the DPUSets created by the DPUDeployment
// As part of this flow, we try to find existing DPUSets and update them to match the DPUDeployment spec instead of
// creating new ones. The reason behind that is so that we can minimize the mutations on DPU objects that impose infra
// changes (provisioning of BFB) that may be disruptive and take a lot of time.
func reconcileDPUSets(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment, dpuNodeLabels map[string]string, hasDisruptiveChanges bool) error {
	owner := metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)

	// Grab existing DPUSets
	existingDPUSets := &provisioningv1.DPUSetList{}
	if err := c.List(ctx,
		existingDPUSets,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return fmt.Errorf("error while listing dpusets : %w", err)
	}

	dpuSetsToBeCreated := make([]*provisioningv1.DPUSet, 0)
	for _, dpuSetOption := range dpuDeployment.Spec.DPUs.DPUSets {
		newDPUSet := generateDPUSet(client.ObjectKeyFromObject(dpuDeployment),
			owner,
			&dpuSetOption,
			dpuDeployment,
			dpuNodeLabels,
			hasDisruptiveChanges)

		if i := matchDPUSetIndex(newDPUSet, existingDPUSets.Items); i >= 0 {
			// patch the existing dpuset
			if err := c.Patch(ctx, newDPUSet, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
				return fmt.Errorf("error while patching %s %s: %w", newDPUSet.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(newDPUSet), err)
			}
			existingDPUSets.Items = deleteElementOrNil(existingDPUSets.Items, i, i+1)
			continue
		}
		dpuSetsToBeCreated = append(dpuSetsToBeCreated, newDPUSet)
	}

	// Create DPUSets to match what is defined in the DPUDeployment
	for _, dpuSet := range dpuSetsToBeCreated {
		if err := c.Patch(ctx, dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
			return fmt.Errorf("error while patching %s %s: %w", dpuSet.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(dpuSet), err)
		}
	}

	// Cleanup the remaining stale DPUSets
	for _, dpuSet := range existingDPUSets.Items {
		if err := c.Delete(ctx, &dpuSet); err != nil {
			return fmt.Errorf("error while deleting %s %s: %w", dpuSet.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuSet), err)
		}
	}

	return nil
}

// matchDPUSetIndex tries to find a DPUSet that matches the expected input from a list and returns the index
func matchDPUSetIndex(expected *provisioningv1.DPUSet, existing []provisioningv1.DPUSet) int {
	// If the name is the same, we can assume that the DPUSet is the same
	// no need to check the spec
	for i, existingDPUSet := range existing {
		if existingDPUSet.Name == expected.Name {
			return i
		}
	}

	return -1
}

// generateDPUSet generates a DPUSet according to the DPUDeployment
func generateDPUSet(dpuDeploymentNamespacedName types.NamespacedName,
	owner *metav1.OwnerReference,
	dpuSetSettings *dpuservicev1.DPUSet,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dpuNodeLabels map[string]string,
	hasDisruptiveChanges bool,
) *provisioningv1.DPUSet {
	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", dpuDeploymentNamespacedName.Name, dpuSetSettings.NameSuffix),
			Namespace: dpuDeploymentNamespacedName.Namespace,
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName),
			},
		},
		Spec: provisioningv1.DPUSetSpec{
			Strategy: &provisioningv1.DPUSetStrategy{
				// TODO: Update to OnDelete when this is implemented
				Type: provisioningv1.RollingUpdateStrategyType,
			},
			DPUTemplate: provisioningv1.DPUTemplate{
				Annotations: dpuSetSettings.DPUAnnotations,
				Spec: provisioningv1.DPUTemplateSpec{
					BFB: provisioningv1.BFBReference{
						Name: dpuDeployment.Spec.DPUs.BFB,
					},
					DPUFlavor: dpuDeployment.Spec.DPUs.Flavor,
				},
			},
		},
	}

	// Set DPUNodeSelector and DPUDeviceSelector accordingly preferring the new field over the deprecated one
	dpuSet.Spec.DPUNodeSelector = dpuSetSettings.DPUNodeSelector
	if dpuSet.Spec.DPUNodeSelector == nil {
		//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
		dpuSet.Spec.DPUNodeSelector = dpuSetSettings.NodeSelector
	}

	dpuSet.Spec.DPUDeviceSelector = dpuSetSettings.DPUDeviceSelector
	//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
	if dpuSet.Spec.DPUDeviceSelector == nil && dpuSetSettings.DPUSelector != nil && len(dpuSetSettings.DPUSelector) > 0 {
		// Convert deprecated map[string]string to *metav1.LabelSelector
		//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
		dpuSet.Spec.DPUDeviceSelector = &metav1.LabelSelector{
			MatchLabels: dpuSetSettings.DPUSelector,
		}
	}

	if dpuDeployment.Spec.DPUs.NodeEffect != nil {
		dpuSet.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{Action: *dpuDeployment.Spec.DPUs.NodeEffect}
	} else {
		// We need to patch with the default which is drain in order to avoid issues with updating the nodeEffect on
		// subsequent edits.
		dpuSet.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}}
	}

	// Create a slice of NodeMaintenanceAdditionalRequestors
	// Each DPUService and DPUServiceChain has a NodeMaintenanceAdditionalRequestor.
	// This is needed because the DPUReadyController and the DPUDeployment Node Controller will need to remove those
	// requestors when those DPUServices and DPUServiceChains are ready.
	// Always populate this list regardless of whether changes are disruptive
	dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = make([]string, 0)
	for _, revision := range dpuNodeLabels {
		requestor := getRequestorForDPUObjectVersion(getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName), revision)
		if !slices.Contains(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors, requestor) {
			dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = append(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors, requestor)
		}
	}
	slices.Sort(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors)

	// Only set ApplyOnLabelChange=true if there are disruptive changes
	// This prevents non-disruptive label updates from triggering node effect
	dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(hasDisruptiveChanges)

	nodeLabels := map[string]string{
		dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName),
	}

	maps.Copy(nodeLabels, dpuNodeLabels)

	dpuSet.Spec.DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{
		NodeLabels: nodeLabels,
	}

	dpuSet.SetOwnerReferences([]metav1.OwnerReference{*owner})
	dpuSet.ObjectMeta.ManagedFields = nil
	dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)

	return dpuSet
}

// getRequestorForDPUObjectVersion returns the requestor that should be added in the DPUNodeMaintenance for the given
// DPU* version.
func getRequestorForDPUObjectVersion(parentDPUDeploymentNameLabelValue string, version string) string {
	return fmt.Sprintf("%s_%s", parentDPUDeploymentNameLabelValue, version)
}

// reconcileDelete handles the deletion reconciliation loop.
func (r *DPUDeploymentReconciler) reconcileDelete(ctx context.Context, dpuDeployment *dpuservicev1.DPUDeployment) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	// We first remove the DPUServiceChain, DPUServiceInterface and DPUService and then the DPUSet to ensure that the
	// controllers running on the DPU cluster are able to run the deletion reconciliation loop and remove the
	// finalizers. If we don't do so, there is a deadlock if all the DPUs in the DPU Cluster are added by this very
	// same DPUDeployment.
	for _, obj := range []client.Object{
		&dpuservicev1.DPUServiceChain{},
		&dpuservicev1.DPUServiceInterface{},
		&dpuservicev1.DPUService{},
	} {
		if err := r.Client.DeleteAllOf(ctx,
			obj,
			client.InNamespace(dpuDeployment.Namespace),
			client.MatchingLabels{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
			},
		); err != nil {
			return ctrl.Result{}, fmt.Errorf("error while removing %T: %w", obj, err)
		}
	}

	var dpuServiceChainItems, dpuServiceInterfaceItems, dpuServiceItems, dpuSetItems int
	for obj, conditionType := range map[client.ObjectList]conditions.ConditionType{
		&dpuservicev1.DPUServiceChainList{}:     dpuservicev1.ConditionDPUServiceChainsReconciled,
		&dpuservicev1.DPUServiceInterfaceList{}: dpuservicev1.ConditionDPUServiceInterfacesReconciled,
		&dpuservicev1.DPUServiceList{}:          dpuservicev1.ConditionDPUServicesReconciled,
		&provisioningv1.DPUSetList{}:            dpuservicev1.ConditionDPUSetsReconciled,
	} {
		if err := r.Client.List(ctx,
			obj,
			client.InNamespace(dpuDeployment.Namespace),
			client.MatchingLabels{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
			},
		); err != nil {
			return ctrl.Result{}, fmt.Errorf("error while listing %T: %w", obj, err)
		}
		var msg string
		switch t := obj.(type) {
		case *dpuservicev1.DPUServiceChainList:
			dpuServiceChainItems += len(obj.(*dpuservicev1.DPUServiceChainList).Items)
			if dpuServiceChainItems > 0 {
				msg = fmt.Sprintf("There are still %d DPUServiceChains that are not completely deleted", dpuServiceChainItems)
				break
			}
			msg = "All DPUServiceChains are deleted"

		case *dpuservicev1.DPUServiceInterfaceList:
			dpuServiceInterfaceItems += len(obj.(*dpuservicev1.DPUServiceInterfaceList).Items)
			if dpuServiceInterfaceItems > 0 {
				msg = fmt.Sprintf("There are still %d DPUServiceInterfaces that are not completely deleted", dpuServiceInterfaceItems)
				break
			}
			msg = "All DPUServiceInterfaces are deleted"
		case *dpuservicev1.DPUServiceList:
			dpuServiceList := obj.(*dpuservicev1.DPUServiceList)
			dpuServiceItems += len(dpuServiceList.Items)

			// We need to unpause all the DPUServices because there might be stale DPUServices if the DPUDeployment
			// was deleted in the middle of a disruptive upgrade.
			// DPUService controller won't remove the finalizer if the DPUService is paused.
			for _, dpuService := range dpuServiceList.Items {
				if !dpuService.IsPaused() {
					continue
				}

				dpuService.Spec.Paused = ptr.To(false)
				dpuService.SetManagedFields(nil)
				dpuService.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
				if err := r.Client.Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
					if !apierrors.IsNotFound(err) {
						return ctrl.Result{}, fmt.Errorf("error while patching %s %s: %w", dpuService.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuService), err)
					}
				}
			}

			if dpuServiceItems > 0 {
				msg = fmt.Sprintf("There are still %d DPUServices that are not completely deleted", dpuServiceItems)
				break
			}
			msg = "All DPUServices are deleted"
		case *provisioningv1.DPUSetList:
			dpuSetItems += len(obj.(*provisioningv1.DPUSetList).Items)
			if dpuSetItems > 0 {
				msg = fmt.Sprintf("There are still %d DPUSets that are not completely deleted", dpuSetItems)
				break
			}
			msg = "All DPUSets are deleted"
		default:
			panic(fmt.Sprintf("type %v not handled", t))
		}
		conditions.AddFalse(
			dpuDeployment,
			conditionType,
			conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(msg),
		)
	}

	if dpuServiceChainItems > 0 || dpuServiceInterfaceItems > 0 || dpuServiceItems > 0 {
		existingObjs := dpuServiceChainItems + dpuServiceInterfaceItems + dpuServiceItems + dpuSetItems
		log.Info(fmt.Sprintf("There are still %d child objects that are not completely deleted, requeueing before removing the finalizer.", existingObjs),
			"dpuservicechains", dpuServiceChainItems,
			"dpuserviceinterfaces", dpuServiceInterfaceItems,
			"dpuservices", dpuServiceItems,
			"dpusets", dpuSetItems,
		)
		return ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}, nil
	}

	if err := r.Client.DeleteAllOf(ctx,
		&provisioningv1.DPUSet{},
		client.InNamespace(dpuDeployment.Namespace),
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("error while removing %T: %w", &provisioningv1.DPUSet{}, err)
	}

	dpuSetList := &provisioningv1.DPUSetList{}
	if err := r.Client.List(ctx,
		dpuSetList,
		client.InNamespace(dpuDeployment.Namespace),
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("error while listing %T: %w", dpuSetList, err)
	}
	dpuSetItems = len(dpuSetList.Items)
	var msg string
	if dpuSetItems > 0 {
		msg = fmt.Sprintf("There are still %d DPUSets that are not completely deleted", dpuSetItems)
	} else {
		msg = "All DPUSets are deleted"
	}
	conditions.AddFalse(
		dpuDeployment,
		dpuservicev1.ConditionDPUSetsReconciled,
		conditions.ReasonAwaitingDeletion,
		conditions.ConditionMessage(msg),
	)

	if dpuSetItems > 0 {
		log.Info(fmt.Sprintf("There are still %d child objects that are not completely deleted, requeueing before removing the finalizer.", dpuSetItems),
			"dpuservicechains", dpuServiceChainItems,
			"dpuserviceinterfaces", dpuServiceInterfaceItems,
			"dpuservices", dpuServiceItems,
			"dpusets", dpuSetItems,
		)
		return ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}, nil
	}

	if err := releaseAllDependencies(ctx, r.Client, dpuDeployment); err != nil {
		return ctrl.Result{}, fmt.Errorf("error while releasing dependencies: %w", err)
	}

	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpuDeployment, dpuservicev1.DPUDeploymentFinalizer)
	return ctrl.Result{}, nil
}

// releaseAllDependencies unmarks all the dependencies so that they can be deleted if needed
func releaseAllDependencies(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment) error {
	for _, obj := range []client.ObjectList{
		&dpuservicev1.DPUServiceConfigurationList{},
		&dpuservicev1.DPUServiceTemplateList{},
		&provisioningv1.BFBList{},
		&provisioningv1.DPUFlavorList{},
	} {
		if err := c.List(ctx,
			obj,
			client.InNamespace(dpuDeployment.Namespace),
			client.MatchingLabels{
				dpuDeployment.GetDependentLabelKey(): dpuservicev1.DependentDPUDeploymentLabelValue,
			},
		); err != nil {
			return fmt.Errorf("error while listing %T: %w", obj, err)
		}
		switch t := obj.(type) {
		case *dpuservicev1.DPUServiceConfigurationList:
			objs := obj.(*dpuservicev1.DPUServiceConfigurationList).Items
			for _, o := range objs {
				patcher := patch.NewSerialPatcher(&o, c)
				unmarkDependency(dpuDeployment, &o)

				if err := patcher.Patch(ctx, &o, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", o.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&o), err)
				}
			}
		case *dpuservicev1.DPUServiceTemplateList:
			objs := obj.(*dpuservicev1.DPUServiceTemplateList).Items
			for _, o := range objs {
				patcher := patch.NewSerialPatcher(&o, c)
				unmarkDependency(dpuDeployment, &o)

				if err := patcher.Patch(ctx, &o, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", o.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&o), err)
				}
			}
		case *provisioningv1.BFBList:
			objs := obj.(*provisioningv1.BFBList).Items
			for _, o := range objs {
				patcher := patch.NewSerialPatcher(&o, c)
				unmarkDependency(dpuDeployment, &o)

				if err := patcher.Patch(ctx, &o, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", o.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&o), err)
				}
			}
		case *provisioningv1.DPUFlavorList:
			objs := obj.(*provisioningv1.DPUFlavorList).Items
			for _, o := range objs {
				patcher := patch.NewSerialPatcher(&o, c)
				unmarkDependency(dpuDeployment, &o)

				if err := patcher.Patch(ctx, &o, patch.WithFieldOwner(dpuDeploymentControllerName)); err != nil {
					return fmt.Errorf("error while patching %s %s: %w", o.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&o), err)
				}
			}
		default:
			panic(fmt.Sprintf("type %v not handled", t))
		}
	}
	return nil
}

// updateSummary updates the status field of the DPUDeployment
func (r *DPUDeploymentReconciler) updateSummary(ctx context.Context, dpuDeployment *dpuservicev1.DPUDeployment) error {
	defer conditions.SetSummary(dpuDeployment)

	for objGVK, conditionType := range map[schema.GroupVersionKind]conditions.ConditionType{
		provisioningv1.GroupVersion.WithKind(provisioningv1.DPUSetListKind):          dpuservicev1.ConditionDPUSetsReady,
		dpuservicev1.GroupVersion.WithKind(dpuservicev1.DPUServiceChainListKind):     dpuservicev1.ConditionDPUServiceChainsReady,
		dpuservicev1.GroupVersion.WithKind(dpuservicev1.DPUServiceInterfaceListKind): dpuservicev1.ConditionDPUServiceInterfacesReady,
		dpuservicev1.GroupVersion.WithKind(dpuservicev1.DPUServiceListKind):          dpuservicev1.ConditionDPUServicesReady,
	} {
		objs := &unstructured.UnstructuredList{}
		objs.SetGroupVersionKind(objGVK)
		if err := r.Client.List(ctx,
			objs,
			client.MatchingLabels{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
			},
			client.InNamespace(dpuDeployment.Namespace),
		); err != nil {
			return fmt.Errorf("error while listing objects: %w", err)
		}

		var unreadyObjs []types.NamespacedName
		var err error

		unreadyObjs, err = getNotReadyUnstructuredObjects(objs)
		if err != nil {
			conditions.AddFalse(
				dpuDeployment,
				conditionType,
				conditions.ReasonPending,
				conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
			)
			return err
		}

		if len(unreadyObjs) > 0 {
			unreadyMessages := []string{}
			for _, unreadyObj := range unreadyObjs {
				unreadyMessages = append(unreadyMessages, unreadyObj.String())
			}
			message := conditions.ReadyConditionMessage("Objects are not ready", unreadyMessages)
			conditions.AddFalse(
				dpuDeployment,
				conditionType,
				conditions.ReasonPending,
				conditions.ConditionMessage(message),
			)
		} else {
			conditions.AddTrue(dpuDeployment, conditionType)
		}
	}

	return nil
}

// getNotReadyUnstructuredObjects returns a list of objects from a given list that are not in Ready state. This function
// works under the assumption that these objects implement the standard DPF conditions.
func getNotReadyUnstructuredObjects(objs *unstructured.UnstructuredList) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objs.Items {
		conds, exists, err := unstructured.NestedSlice(o.Object, "status", "conditions")
		if err != nil {
			return nil, err
		}

		if !exists || len(conds) == 0 {
			unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()})
			continue
		}

		var isReady bool
		for _, condition := range conds {
			c := condition.(map[string]interface{})
			conditionType, exists, err := unstructured.NestedString(c, "type")
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
			if conditionType != string(conditions.TypeReady) {
				continue
			}

			conditionStatus, exists, err := unstructured.NestedString(c, "status")
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}

			isReady = conditionStatus == string(metav1.ConditionTrue)
		}

		if !isReady {
			unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()})
		}
	}

	return unreadyObjs, nil
}

// getNotReadyDPUSets returns a list of DPUSets from a given list that are not in Ready state.
func getNotReadyDPUSets(dpuSets []provisioningv1.DPUSet) []types.NamespacedName {
	unreadyObjs := []types.NamespacedName{}
	for _, dpuSet := range dpuSets {
		if conditions.IsTrue(&dpuSet, conditions.TypeReady) {
			continue
		}

		unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: dpuSet.GetName(), Namespace: dpuSet.GetNamespace()})
	}

	return unreadyObjs
}

func getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName types.NamespacedName) string {
	return fmt.Sprintf("%s_%s", dpuDeploymentNamespacedName.Namespace, dpuDeploymentNamespacedName.Name)
}

// deleteElementOrNil deletes an element from a slice or returns nil if this is the last element in the slice
func deleteElementOrNil[S ~[]E, E any](s S, i, j int) S {
	if len(s) == 1 {
		s = nil
	} else {
		s = slices.Delete(s, i, j)
	}
	return s
}
