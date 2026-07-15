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
	"net"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/argocd"
	"github.com/nvidia/doca-platform/internal/digest"
	dpuservicepredicates "github.com/nvidia/doca-platform/internal/dpuservice/predicates"
	dpuserviceutils "github.com/nvidia/doca-platform/internal/dpuservice/utils"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/dpuselector"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	argohealth "github.com/nvidia/doca-platform/third_party/forked/argoproj/gitops-engine/pkg/health"

	"github.com/fluxcd/pkg/runtime/patch"
	multusTypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUServiceReconciler reconciles a DPUService object
type DPUServiceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	RemoteCache dpucluster.ClusterClientProvider
	controller  controller.Controller
	// privilegedPodEnforcementValidator overrides the post-apply VAP probe and is
	// set only in tests to mock validatePrivilegedPodEnforcement. It is nil in
	// production, where the real probe runs.
	privilegedPodEnforcementValidator func(context.Context, client.Client) error
}

// pauseDPUServiceReconciler pauses the DPUService Reconciler by doing noop reconciliation loops. This is helpful to
// make tests faster and less complex
var pauseDPUServiceReconciler atomic.Bool

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;endpoints,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=argoproj.io,resources=appprojects;applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch

const (
	dpuServiceControllerName = "dpuservice-manager"

	// TODO: These constants don't belong here and should be moved as they're shared with other packages.
	networkAnnotationKey          = "k8s.v1.cni.cncf.io/networks"
	annotationKeyAppSkipReconcile = "argocd.argoproj.io/skip-reconcile"

	// ovnNodeEncapIPsAnnotation is the OVN-Kubernetes node annotation holding the encap IP(s) used by OVN-Kubernetes.
	ovnNodeEncapIPsAnnotation = "k8s.ovn.org/node-encap-ips"
)

// applyPatchOptions contains options which are passed to every `client.Apply` patch.
var applyPatchOptions = []client.PatchOption{
	client.ForceOwnership,
	client.FieldOwner(dpuServiceControllerName),
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUServiceReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {

	bldr := ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUService{}).
		Watches(&argov1.Application{}, handler.EnqueueRequestsFromMapFunc(r.requestsForChangeByLabel)).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.imagePullSecretToDPUServices),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
				_, ok := o.GetLabels()[dpuservicev1.DPFImagePullSecretLabelKey]
				return ok
			})),
		).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.requestsForChangeByLabel)).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(r.requestsForChangeByLabel)).
		Watches(
			&dpuservicev1.DPUServiceInterface{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForDPUServiceInterfaceChange),
			builder.WithPredicates(dpuservicepredicates.DPUServiceInterfaceChangePredicate{}),
		).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.DPUClusterToDPUService)).
		// Watch the DPFOperatorConfig so toggling the privileged-pod enforcement
		// breakglass field (spec.security.privilegedPodEnforcement) promptly
		// re-reconciles all DPUServices instead of waiting for the next resync.
		// The predicate fires only when the resolved enforcement state actually
		// changes, so unrelated DPFOperatorConfig spec edits do not trigger a
		// reconcile of every DPUService.
		Watches(
			&operatorv1.DPFOperatorConfig{},
			handler.EnqueueRequestsFromMapFunc(r.dpfOperatorConfigToDPUServices),
			builder.WithPredicates(privilegedPodEnforcementChangedPredicate()),
		)

	if features.Gates.Enabled(features.ConfigPortsOverHighSpeed) {
		bldr.Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForHostNode),
			builder.WithPredicates(nodeOVNEncapIPPredicate()),
		)
	}

	c, err := bldr.Build(r)
	if err != nil {
		return err
	}

	r.controller = c
	return nil
}

// imagePullSecretToDPUServices enqueues DPUServices in the secret namespace so
// remote mirrored pull secrets are refreshed when the source secret changes.
func (r *DPUServiceReconciler) imagePullSecretToDPUServices(ctx context.Context, obj client.Object) []ctrl.Request {
	dpuServices := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServices, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	out := make([]ctrl.Request, 0, len(dpuServices.Items))
	for i := range dpuServices.Items {
		svc := &dpuServices.Items[i]
		out = append(out, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(svc)})
	}
	return out
}

// Reconcile reconciles changes in a DPUService.
func (r *DPUServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	if pauseDPUServiceReconciler.Load() {
		log.Info("noop reconciliation")
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling")
	dpuService := &dpuservicev1.DPUService{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuService); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	patcher := patch.NewSerialPatcher(dpuService, r.Client)

	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := r.summary(ctx, dpuService); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		if err := r.updateConfigPortsStatus(ctx, dpuService); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}

		if err := patcher.Patch(ctx, dpuService,
			patch.WithFieldOwner(dpuServiceControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.Conditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuService.ObjectMeta.DeletionTimestamp.IsZero() && !dpuService.IsPaused() {
		return r.reconcileDelete(ctx, dpuService)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuService, dpuservicev1.DPUServiceFinalizer) {
		controllerutil.AddFinalizer(dpuService, dpuservicev1.DPUServiceFinalizer)
		return ctrl.Result{}, nil
	}
	if err := r.reconcile(ctx, dpuService); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *DPUServiceReconciler) reconcileDelete(ctx context.Context, dpuService *dpuservicev1.DPUService) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Handling DPUService deletion")
	res, err := r.reconcileDeleteApplications(ctx, dpuService)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !res.IsZero() {
		return res, nil
	}

	//Delete the DPUServiceInterfaces annotations.
	if dpuService.Spec.Interfaces != nil {
		for _, interfaceName := range dpuService.Spec.Interfaces {
			if err := r.deleteInterfaceAnnotation(ctx, interfaceName, dpuService); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	if err := r.reconcileDeleteImagePullSecrets(ctx, dpuService); err != nil {
		return ctrl.Result{}, err
	}
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// On deletion we only remove this DPUService's own privileged allowlist
	// entry; we never change the global VAP/binding state (that is driven by the
	// DPFOperatorConfig breakglass field on the normal reconcile path). This
	// keeps deletion independent of the DPFOperatorConfig and free of
	// cluster-wide side effects.
	if err := r.removePrivilegedPodEnforcementEntries(ctx, dpuClusterConfigs, dpuService); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.cleanupAllConfigPorts(ctx, dpuService); err != nil {
		return ctrl.Result{}, err
	}

	// Add AwaitingDeletion condition to true. Probably this will never going to apply on the DPUService as it will be
	// deleted in the next step anyway.
	conditions.AddTrue(dpuService, dpuservicev1.ConditionApplicationsReconciled)

	// If there are no associated applications remove the finalizer.
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpuService, dpuservicev1.DPUServiceFinalizer)

	return ctrl.Result{}, nil
}

func (r *DPUServiceReconciler) deleteInterfaceAnnotation(ctx context.Context, interfaceName string, dpuService *dpuservicev1.DPUService) error {
	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: interfaceName, Namespace: dpuService.Namespace}, dpuServiceInterface); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	annotations := dpuServiceInterface.GetAnnotations()
	if annotations != nil && annotations[dpuservicev1.DPUServiceInterfaceAnnotationKey] == dpuService.Name {
		delete(annotations, dpuservicev1.DPUServiceInterfaceAnnotationKey)
		dpuServiceInterface.SetManagedFields(nil)
		// Set the GroupVersionKind to the DPUServiceInterface.
		dpuServiceInterface.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
		if err := r.Client.Patch(ctx, dpuServiceInterface, client.Apply, applyPatchOptions...); err != nil {
			return err
		}
	}
	return nil
}

func (r *DPUServiceReconciler) reconcileDeleteApplications(ctx context.Context, dpuService *dpuservicev1.DPUService) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	applications := &argov1.ApplicationList{}
	if err := r.Client.List(ctx, applications, dpuService.MatchLabels()); err != nil {
		return ctrl.Result{}, err
	}

	for _, app := range applications.Items {
		if err := r.Client.Delete(ctx, &app); err != nil {
			// Tolerate if the application is not found and already deleted.
			if !apierrors.IsNotFound(err) {
				conditions.AddFalse(
					dpuService,
					dpuservicev1.ConditionApplicationsReconciled,
					conditions.ReasonAwaitingDeletion,
					conditions.ConditionMessage(fmt.Sprintf("Error occurred: %v", err)),
				)
				return ctrl.Result{}, err
			}
		}

		// unpause the application if it is paused so that it can be deleted.
		if err := r.unpauseApplication(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// List Applications again to verify if it is in deletion.
	// Already deleted Applications will not be listed.
	if err := r.Client.List(ctx, applications, dpuService.MatchLabels()); err != nil {
		return ctrl.Result{}, err
	}

	appsInDeletion := []string{}
	for _, app := range applications.Items {
		if !app.GetDeletionTimestamp().IsZero() {
			argoApp := fmt.Sprintf("%s/%s.%s", app.Namespace, app.Name, app.GroupVersionKind().Group)
			appsInDeletion = append(appsInDeletion, argoApp)
		}
	}

	if len(appsInDeletion) > 0 {
		message := conditions.ReadyConditionMessage("The following applications are awaiting deletion", appsInDeletion)
		conditions.AddFalse(
			dpuService,
			dpuservicev1.ConditionApplicationsReconciled,
			conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage(message),
		)
	}

	if len(applications.Items) > 0 {
		log.Info(fmt.Sprintf("Requeueing: %d applications still managed by DPUService", len(applications.Items)))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileDeleteImagePullSecrets ensures imagePullSecrets are removed from DPUClusters where no more DPUServices with that namespace are deployed.
func (r *DPUServiceReconciler) reconcileDeleteImagePullSecrets(ctx context.Context, dpuService *dpuservicev1.DPUService) error {
	log := ctrllog.FromContext(ctx)

	// List all DPUServices.
	dpuServices := &dpuservicev1.DPUServiceList{}
	if err := r.List(ctx, dpuServices, client.InNamespace(dpuService.GetNamespace())); err != nil {
		return fmt.Errorf("list DPUServices: %w", err)
	}

	// If at least 1 DPUService is not in deleting we cannot clean up the ImagePullSecrets.
	for _, dpuService := range dpuServices.Items {
		if dpuService.DeletionTimestamp.IsZero() {
			return nil
		}
	}

	log.Info("Cleaning up ImagePullSecrets in all DPU Clusters")
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return err
	}

	// Delete the secrets in every DPUCluster.
	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := dpuClusterConfig.Client(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// First get the secrets with the correct label.
		secrets := &corev1.SecretList{}
		err = dpuClusterClient.List(ctx, secrets, client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}, client.InNamespace(dpuService.GetNamespace()))
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// Create list for better logging.
		var secretsNameList []string
		for _, secret := range secrets.Items {
			secretNamespaceName := fmt.Sprintf("%s/%s", secret.GetNamespace(), secret.GetName())
			secretsNameList = append(secretsNameList, secretNamespaceName)
		}

		log.Info("Deleting ImagePullSecrets", "cluster", dpuClusterConfig.Cluster.Name, "secrets", secretsNameList)
		for _, secret := range secrets.Items {
			if err := dpuClusterClient.Delete(ctx, secret.DeepCopy()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return kerrors.NewAggregate(errs)
}

func (r *DPUServiceReconciler) fetchResourceNameFromNAD(ctx context.Context, interfaces []*dpuservicev1.DPUServiceInterface, resourceInjectionMap map[string]int) error {

	predefinedNADs := map[string]struct{}{
		"mybrhbn":   {},
		"iprequest": {},
	}

	for _, dpuServiceInterface := range interfaces {
		nadNamespace, nadName := dpuServiceInterface.Spec.GetTemplateSpec().GetTemplateSpec().Service.GetNetwork()
		if nadNamespace == "" {
			nadNamespace = dpuServiceInterface.Namespace
		}
		if nadName == "" {
			continue
		}
		if _, exists := predefinedNADs[nadName]; exists {
			// do not do anything for predefined NADs
			continue
		}
		dpuserviceNAD := &dpuservicev1.DPUServiceNAD{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: nadName, Namespace: nadNamespace}, dpuserviceNAD); err != nil {
			// NAD could have been deleted, continue to the next one
			if !apierrors.IsNotFound(err) {
				return err
			}
			continue
		}
		resourceName := dpuserviceNAD.GetResourceName()

		if resourceName == "" {
			// do not do anything for veth
			continue
		}
		resourceInjectionMap[resourceName]++
	}
	return nil
}

func (r *DPUServiceReconciler) updateDPUServiceDaemonSetResources(serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, resourceInjectionMap map[string]int) error {
	var resourceList corev1.ResourceList
	if serviceDaemonSet.Resources == nil {
		resourceList = corev1.ResourceList{}
	} else {
		resourceList = serviceDaemonSet.Resources
	}

	for key, value := range resourceInjectionMap {
		quantity, err := resource.ParseQuantity(strconv.Itoa(value))
		if err != nil {
			return err
		}
		resourceList[corev1.ResourceName(key)] = quantity
	}

	serviceDaemonSet.Resources = resourceList
	return nil
}

func (r *DPUServiceReconciler) reconcile(ctx context.Context, dpuService *dpuservicev1.DPUService) error {
	log := ctrllog.FromContext(ctx)

	// Get the list of all DPUClusters. We don't select specific clusters here because we need to reconcile
	// common resources used by multiple DPUServices across all clusters.
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return err
	}

	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return err
	}

	// Return early if the dpuService is paused.
	if dpuService.IsPaused() {
		if err := r.pauseApplication(ctx, dpuClusterConfigs, dpuService, dpfOperatorConfig.GetNamespace()); err != nil {
			return err
		}
		log.Info("reconciliation is paused for the DPUService")
		return nil
	}

	// Ensure the DPUService has a ServiceID.
	serviceID := generateServiceID(dpuService)
	// persist the ServiceID so that user can see it in the status early
	// in case of a failure.
	dpuService.Status.ServiceID = serviceID

	serviceDaemonSet, interfaces, err := r.reconcileInterfaces(ctx, dpuService, serviceID)
	if err != nil {
		message := fmt.Sprintf("Unable to reconcile interfaces for %s", err.Error())
		conditions.AddFalse(
			dpuService,
			dpuservicev1.ConditionDPUServiceInterfaceReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(message),
		)
		return err
	}

	// Dynamic Resource Injection, It is not implemented in this manner but the idea is same https://github.com/k8snetworkplumbingwg/network-resources-injector?tab=readme-ov-file#network-resources-injection-example
	// Reason on why this approach was taken? = https://docs.google.com/document/d/1mr1TjXGemgeBnbeLHU2hA1Ve46spWOCvYb8Xyc2nvcw/edit?tab=t.0#heading=h.o2d5j5205ugw
	resourceInjectionMap := make(map[string]int)
	err = r.fetchResourceNameFromNAD(ctx, interfaces, resourceInjectionMap)
	if err != nil {
		log.Error(err, "unable to fetch resourceName from DPUServiceNAD")
		return err
	}

	if len(resourceInjectionMap) > 0 {
		if err := r.updateDPUServiceDaemonSetResources(serviceDaemonSet, resourceInjectionMap); err != nil {
			log.Error(err, "unable to dynamicInjectResource in DPUServiceHelmchart")
			return err
		}
	}
	conditions.AddTrue(dpuService, dpuservicev1.ConditionDPUServiceInterfaceReconciled)

	// We apply the application prereqs to all clusters regardless of the selector.
	if err := r.reconcileApplicationPrereqs(ctx, dpuService, dpuClusterConfigs, dpfOperatorConfig); err != nil {
		message := fmt.Sprintf("Unable to reconcile application prereq for %s", err.Error())
		conditions.AddFalse(
			dpuService,
			dpuservicev1.ConditionApplicationPrereqsReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(message),
		)
		return err
	}
	conditions.AddTrue(dpuService, dpuservicev1.ConditionApplicationPrereqsReconciled)

	// Update the ArgoApplication for all target clusters.
	if err = r.reconcileApplication(ctx, dpuClusterConfigs, dpuService, serviceDaemonSet, dpfOperatorConfig.GetNamespace(), serviceID); err != nil {
		message := fmt.Sprintf("Unable to reconcile Applications: %v", err)
		conditions.AddFalse(
			dpuService,
			dpuservicev1.ConditionApplicationsReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(message),
		)
		return err
	}
	conditions.AddTrue(dpuService, dpuservicev1.ConditionApplicationsReconciled)

	// TODO: refactor this to support multi DPUCluster
	if err = r.reconcileConfigPorts(ctx, dpuService, dpuClusterConfigs, dpfOperatorConfig.GetNamespace()); err != nil {
		message := fmt.Sprintf("Unable to reconcile Config Ports: %v", err)
		conditions.AddFalse(
			dpuService,
			dpuservicev1.ConditionConfigPortsReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(message),
		)
		return err
	}
	conditions.AddTrue(dpuService, dpuservicev1.ConditionConfigPortsReconciled)

	return nil
}

// reconcileApplicationPrereqs ensures destination namespaces, image-pull
// secrets, and privileged-pod enforcement are ready before ArgoCD Applications
// are created or updated. Whether privileged-pod enforcement is enforcing or in
// audit-only mode is read directly from the DPFOperatorConfig breakglass field.
func (r *DPUServiceReconciler) reconcileApplicationPrereqs(ctx context.Context, dpuService *dpuservicev1.DPUService, dpuClusterConfigs []*dpucluster.Config, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	// Ensure the DPUService namespace exists in target clusters.
	project := getProjectName(dpuService)
	// TODO: think about how to cleanup the namespace in the DPU.
	// TODO: This can be cluster specific, consider adjusting this to target specific clusters
	if err := r.ensureNamespaces(ctx, dpuClusterConfigs, project, dpuService.GetNamespace()); err != nil {
		return fmt.Errorf("cluster namespaces: %v", err)
	}

	// Reconcile the ImagePullSecrets.
	// TODO: Move this functionality to a different controller to make it more generic.
	// TODO: This can be cluster specific, consider adjusting this to target specific clusters
	if err := r.reconcileImagePullSecrets(ctx, dpuClusterConfigs, dpuService); err != nil {
		return fmt.Errorf("ImagePullSecrets: %v", err)
	}

	isPrivilegedPodEnforcementEnabled := dpfOperatorConfig.Spec.Security.PrivilegedPodEnforcementEnabled()
	if err := r.reconcilePrivilegedPodEnforcements(ctx, dpuClusterConfigs, dpuService, isPrivilegedPodEnforcementEnabled); err != nil {
		return fmt.Errorf("PrivilegedPodEnforcement: %w", err)
	}

	return nil
}

// reconcileInterfaces reconciles the DPUServiceInterfaces associated with the DPUService.
func (r *DPUServiceReconciler) reconcileInterfaces(ctx context.Context, dpuService *dpuservicev1.DPUService, dpuServiceID string) (*dpuservicev1.ServiceDaemonSetValues, []*dpuservicev1.DPUServiceInterface, error) {
	log := ctrllog.FromContext(ctx)
	networkSelectionByInterface := map[string]multusTypes.NetworkSelectionElement{}

	if dpuService.Spec.Interfaces == nil {
		return dpuService.Spec.ServiceDaemonSet, nil, nil
	}

	dpuServiceInterfaces := []*dpuservicev1.DPUServiceInterface{}
	for _, interfaceName := range dpuService.Spec.Interfaces {
		dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: interfaceName, Namespace: dpuService.Namespace}, dpuServiceInterface); err != nil {
			return nil, nil, err
		}

		service := dpuServiceInterface.Spec.GetTemplateSpec().GetTemplateSpec().Service
		interfaceType := dpuServiceInterface.Spec.GetTemplateSpec().GetTemplateSpec().InterfaceType
		if service == nil || interfaceType != dpuservicev1.InterfaceTypeService {
			return nil, nil, fmt.Errorf("wrong service definition in DPUServiceInterface %s", interfaceName)
		}

		// report conflicting ServiceID in DPUServiceInterface
		if dpuServiceID != "" && service.ServiceID != dpuServiceID {
			return nil, nil, fmt.Errorf("conflicting ServiceID in DPUServiceInterface %s", interfaceName)
		}

		inUse, exist := isDPUServiceInterfaceInUse(dpuServiceInterface, dpuService)
		if inUse {
			return nil, nil, fmt.Errorf("dpuServiceInterface %s is in use by another DPUService", interfaceName)
		}

		if !exist {
			dpuServiceInterface.SetAnnotations(map[string]string{
				dpuservicev1.DPUServiceInterfaceAnnotationKey: dpuService.Name,
			})

			// remove managed fields from the DPUServiceInterface to avoid conflicts with the DPUService.
			dpuServiceInterface.SetManagedFields(nil)
			// Set the GroupVersionKind to the DPUServiceInterface.
			dpuServiceInterface.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
			if err := r.Client.Patch(ctx, dpuServiceInterface, client.Apply, applyPatchOptions...); err != nil {
				return nil, nil, err
			}
		}

		networkSelection := newNetworkSelectionElement(dpuServiceInterface)
		networkSelectionByInterface[networkSelection.InterfaceRequest] = networkSelection
		dpuServiceInterfaces = append(dpuServiceInterfaces, dpuServiceInterface)
	}

	serviceDaemonSet, err := addNetworkAnnotationToServiceDaemonSet(dpuService, networkSelectionByInterface)
	if err != nil {
		return nil, nil, err
	}

	log.Info("Reconciled DPUServiceInterfaces")
	return serviceDaemonSet, dpuServiceInterfaces, nil
}

func (r *DPUServiceReconciler) ensureNamespaces(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, project, dpuNamespace string) error {
	var errs []error
	if project == argocd.AppProjectNameDPU {
		for _, dpuClusterConfig := range dpuClusterConfigs {
			dpuClusterClient, err := r.getDPUClusterClient(ctx, dpuClusterConfig.Cluster)
			if err != nil {
				errs = append(errs, fmt.Errorf("get cluster %s: %v", dpuClusterConfig.Cluster.Name, err))
				continue
			}
			if err := r.ensureNamespace(ctx, dpuClusterClient, dpuNamespace); err != nil {
				errs = append(errs, fmt.Errorf("namespace for cluster %s: %v", dpuClusterConfig.Cluster.Name, err))
			}
		}
	} else {
		if err := utils.EnsureNamespace(ctx, r.Client, dpuNamespace); err != nil {
			return fmt.Errorf("in-cluster namespace: %v", err)
		}
	}
	return kerrors.NewAggregate(errs)
}

func (r *DPUServiceReconciler) ensureNamespace(ctx context.Context, dpuClusterClient client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err := controllerutil.CreateOrPatch(ctx, dpuClusterClient, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[NamespaceScopeLabelKey] = namespace
		ns.Labels[DPUServicePrereqLabel] = dpuServicePrereqLabelValue
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or patch namespace %s: %w", namespace, err)
	}
	return nil
}

// summary sets the conditions for the sync status of the ArgoCD applications as well as the overall conditions.
func (r *DPUServiceReconciler) summary(ctx context.Context, dpuService *dpuservicev1.DPUService) error {
	defer conditions.SetSummary(dpuService)

	// Get the list of clusters this DPUService targets.
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return err
	}

	applicationList := &argov1.ApplicationList{}
	// List all of the applications matching this DPUService.
	if err := r.Client.List(ctx, applicationList, dpuService.MatchLabels()); err != nil {
		return err
	}

	unreadyApplications := []string{}
	// Summarize the readiness status of each application linked to the DPUService.
	for _, app := range applicationList.Items {
		syncStatus := app.Status.Sync.Status
		healthStatus := app.Status.Health.Status

		// Skip applications that are synced and healthy.
		if syncStatus == argov1.SyncStatusCodeSynced && healthStatus == argohealth.HealthStatusHealthy {
			continue
		}

		// Handle applications that are not yet deployed.
		if syncStatus == "" && healthStatus == "" {
			unreadyApplications = append(unreadyApplications, "Application is not yet deployed.")
			continue
		}

		// Add a detailed message for applications that are not ready.
		message := fmt.Sprintf(
			"Application is not ready:\n* Sync: %s\n  Health: %s\nRun the following command for more details:\nkubectl describe application %s -n %s",
			syncStatus, healthStatus, app.Name, app.Namespace,
		)
		unreadyApplications = append(unreadyApplications, message)
	}

	// For standard services, we expect the number of applications to equal the number of DPUClusters, whereas for in-cluster
	// we expect a single application targeting the host cluster
	hasExpectedApplications := len(applicationList.Items) == len(dpuClusterConfigs)
	if dpuService.ShouldDeployInCluster() {
		hasExpectedApplications = len(applicationList.Items) == 1
	}
	// Update condition and requeue if there are any errors, or if there are fewer applications than we have clusters.
	if len(unreadyApplications) > 0 || !hasExpectedApplications {
		message := conditions.ReadyConditionMessage("The following applications are not ready", unreadyApplications)
		conditions.AddFalse(
			dpuService,
			dpuservicev1.ConditionApplicationsReady,
			conditions.ReasonPending,
			conditions.ConditionMessage(message),
		)
		return nil
	}

	conditions.AddTrue(dpuService, dpuservicev1.ConditionApplicationsReady)
	return nil
}

func (r *DPUServiceReconciler) reconcileImagePullSecrets(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, service *dpuservicev1.DPUService) error {
	log := ctrllog.FromContext(ctx)
	log.Info("Patching ImagePullSecrets for DPU clusters")

	// First get the secrets with the correct label.
	secrets := &corev1.SecretList{}
	err := r.List(ctx, secrets,
		client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
		client.InNamespace(service.GetNamespace()),
	)
	if err != nil {
		return err
	}

	secretsToPatch := []*corev1.Secret{}
	existingSecrets := map[string]bool{}
	// Copy the spec of the secret to a new secret and set the namespace.
	for _, secret := range secrets.Items {
		secretsToPatch = append(secretsToPatch, &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      secret.Name,
				Namespace: secret.Namespace,
				Labels:    secret.Labels,
			},
			Immutable: secret.Immutable,
			Data:      secret.Data,
			Type:      secret.Type,
		})
		existingSecrets[secret.GetName()] = true
	}

	// Apply the new secret to every DPUCluster.
	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := r.getDPUClusterClient(ctx, dpuClusterConfig.Cluster)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, secret := range secretsToPatch {
			if err := dpuClusterClient.Patch(ctx, secret.DeepCopy(), client.Apply, applyPatchOptions...); err != nil {
				errs = append(errs, err)
			}
		}

		// Cleanup orphaned secrets.
		inClusterSecrets := &corev1.SecretList{}
		if err := dpuClusterClient.List(ctx, inClusterSecrets,
			client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
			client.InNamespace(service.GetNamespace()),
		); err != nil {
			return err
		}
		for _, secret := range inClusterSecrets.Items {
			if _, ok := existingSecrets[secret.GetName()]; ok {
				log.Info("ImagePullSecret still present in cluster", "secret", secret.Name, "cluster", dpuClusterConfig.Cluster.Name)
				continue
			}

			log.Info("Deleting secret in cluster", "secret", secret.Name, "cluster", dpuClusterConfig.Cluster.Name)
			if err := dpuClusterClient.Delete(ctx, &secret); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return kerrors.NewAggregate(errs)
}

func (r *DPUServiceReconciler) reconcileApplication(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpuService *dpuservicev1.DPUService, serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, dpfOperatorConfigNamespace, serviceID string) error {
	project := getProjectName(dpuService)
	if project != argocd.AppProjectNameDPU {
		return r.ensureApplication(ctx, dpuService, serviceDaemonSet, "in-cluster", "", dpfOperatorConfigNamespace, serviceID)
	}
	return r.reconcileStandardApplications(ctx, dpuClusterConfigs, dpuService, serviceDaemonSet, dpfOperatorConfigNamespace, serviceID)
}

// reconcileStandardApplications reconciles ArgoCD Applications across DPU clusters.
// It creates/updates applications in clusters that match the DPUService's cluster selector,
// and deletes applications from clusters that no longer match or no longer exist.
func (r *DPUServiceReconciler) reconcileStandardApplications(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpuService *dpuservicev1.DPUService, serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, dpfOperatorConfigNamespace, serviceID string) error {
	// Get the list of the DPUClusters the DPUService is targeting
	targetDPUClusterConfigs, err := utils.GetMatchingDPUClusters(dpuClusterConfigs, dpuService.Spec.DPUClusterSelector)
	if err != nil {
		return err
	}

	// Create and update applications in each targeted cluster
	targetClusterNames := make(map[string]struct{}, len(targetDPUClusterConfigs))
	for _, dpuClusterConfig := range targetDPUClusterConfigs {
		// Skip DPUClusters that are being deleted. Instead existing applications will be deleted below.
		if !dpuClusterConfig.Cluster.DeletionTimestamp.IsZero() {
			continue
		}

		if err := r.ensureApplication(ctx, dpuService, serviceDaemonSet, dpuClusterConfig.Cluster.Name, dpuClusterConfig.Cluster.Namespace, dpfOperatorConfigNamespace, serviceID); err != nil {
			return fmt.Errorf("ensure application for DPUCluster %s: %w", dpuClusterConfig.ClusterNamespaceName(), err)
		}
		targetClusterNames[applicationTargetClusterKey(dpuClusterConfig.Cluster.Namespace, dpuClusterConfig.Cluster.Name)] = struct{}{}
	}

	// List all existing Applications for this DPUService, including those for DPUClusters that
	// no longer exist. This ensures orphaned applications are cleaned up when a DPUCluster is deleted.
	existingApplications := &argov1.ApplicationList{}
	if err := r.Client.List(ctx, existingApplications, client.InNamespace(dpfOperatorConfigNamespace), dpuService.MatchLabels()); err != nil {
		return fmt.Errorf("listing applications for DPUService %s: %w", client.ObjectKeyFromObject(dpuService), err)
	}

	// Delete leftover applications in clusters that are no longer selected or no longer exist
	var staleApplications []string
	for _, app := range existingApplications.Items {
		clusterName, hasDPUClusterNameLabel := app.Labels[provisioningv1.DPUClusterNameLabelKey]
		// Ignore applications that don't have the DPUClusterNameLabel. They have not been created by the DPUService controller.
		if !hasDPUClusterNameLabel {
			continue
		}
		clusterNamespace, hasDPUClusterNamespaceLabel := app.Labels[provisioningv1.DPUClusterNamespaceLabelKey]
		// Ignore applications that don't have the DPUClusterNamespaceLabel, this is the case for e.g. in-cluster services.
		if !hasDPUClusterNamespaceLabel {
			continue
		}

		// Ignore applications we expect to exist.
		if _, ok := targetClusterNames[applicationTargetClusterKey(clusterNamespace, clusterName)]; ok {
			continue
		}
		// Try to trigger deletion of the application.
		isAppDeleted, err := r.deleteApplication(ctx, dpuService, clusterName, dpfOperatorConfigNamespace)
		if err != nil {
			return fmt.Errorf("deleting application for DPUCluster %s: %w", klog.KRef(clusterNamespace, clusterName), err)
		}
		if !isAppDeleted {
			staleApplications = append(staleApplications, fmt.Sprintf("%s/%s", dpfOperatorConfigNamespace, app.Name))
		}
	}

	if len(staleApplications) > 0 {
		message := conditions.ReadyConditionMessage("Stale applications still exist for non matching DPUClusters", staleApplications)
		return fmt.Errorf("%s", message)
	}

	return nil
}

func applicationTargetClusterKey(clusterNamespace, clusterName string) string {
	return fmt.Sprintf("%s-%s", clusterNamespace, clusterName)
}

func (r *DPUServiceReconciler) ensureApplication(ctx context.Context, dpuService *dpuservicev1.DPUService, serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, clusterName, clusterNamespace, dpfOperatorConfigNamespace, serviceID string) error {
	log := ctrllog.FromContext(ctx)
	project := getProjectName(dpuService)
	values, err := argoCDValuesFromDPUService(serviceDaemonSet, dpuService, clusterName, serviceID)
	if err != nil {
		return err
	}

	argoApplication := argocd.NewApplication(dpfOperatorConfigNamespace, project, dpuService, values, clusterName, clusterNamespace)
	gotArgoApplication := &argov1.Application{}

	// If Application does not exist, create it. Otherwise, patch the object.
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(argoApplication), gotArgoApplication); apierrors.IsNotFound(err) {
		log.Info("Creating Application", "Application", klog.KObj(argoApplication))
		if err := r.Client.Create(ctx, argoApplication); err != nil {
			return err
		}
	} else {
		// Return early if the spec has not changed.
		if reflect.DeepEqual(gotArgoApplication.Spec, argoApplication.Spec) && !isSkipReconcileAnnotationSet(gotArgoApplication) {
			return nil
		}

		// Copy spec to the original object and update it.
		gotArgoApplication.Spec = argoApplication.Spec

		// Remove any skip reconcile annotations.
		delete(gotArgoApplication.Annotations, annotationKeyAppSkipReconcile)

		log.Info("Updating Application", "Application", klog.KObj(gotArgoApplication))
		if err := r.Client.Update(ctx, gotArgoApplication); err != nil {
			return err
		}
	}
	return nil
}

// deleteApplication deletes an Application for a specific cluster and returns true if it was deleted, false if it still exists.
func (r *DPUServiceReconciler) deleteApplication(ctx context.Context, dpuService *dpuservicev1.DPUService, clusterName, dpfOperatorConfigNamespace string) (bool, error) {
	namespacedName := types.NamespacedName{Namespace: dpfOperatorConfigNamespace, Name: argocd.GetApplicationName(clusterName, dpuService.Name)}
	application := &argov1.Application{}
	// Get the application and exit early if it is already deleted
	if err := r.Client.Get(ctx, namespacedName, application); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	// Delete the application and exit early if it is already deleted
	if err := r.Client.Delete(ctx, application); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	// Unpause the application if it is paused so that it can be deleted
	if err := r.unpauseApplication(ctx, application); err != nil {
		return false, err
	}

	// Check if the application still exists (might be in deletion)
	if err := r.Client.Get(ctx, namespacedName, application); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	// Application still exists
	return false, nil
}

func (r *DPUServiceReconciler) pauseApplication(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpuService *dpuservicev1.DPUService, dpfOperatorConfigNamespace string) error {
	project := getProjectName(dpuService)
	if project == argocd.AppProjectNameDPU {
		for _, dpuClusterConfig := range dpuClusterConfigs {
			if err := r.skipApplicationReconcile(ctx, dpfOperatorConfigNamespace, dpuClusterConfig.Cluster.Name, dpuService.Name); err != nil {
				return err
			}
		}
	} else {
		if err := r.skipApplicationReconcile(ctx, dpfOperatorConfigNamespace, "in-cluster", dpuService.Name); err != nil {
			return err
		}
	}
	return nil
}

func (r *DPUServiceReconciler) skipApplicationReconcile(ctx context.Context, dpfOperatorConfigNamespace string, clustername, name string) error {
	namespacedName := types.NamespacedName{Namespace: dpfOperatorConfigNamespace, Name: argocd.GetApplicationName(clustername, name)}
	gotArgoApplication := &argov1.Application{}
	if err := r.Client.Get(ctx, namespacedName, gotArgoApplication); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if gotArgoApplication.Annotations == nil {
		gotArgoApplication.Annotations = map[string]string{}
	}
	gotArgoApplication.Annotations[annotationKeyAppSkipReconcile] = strconv.FormatBool(true)
	if err := r.Client.Update(ctx, gotArgoApplication); err != nil {
		return err
	}
	return nil
}

func (r *DPUServiceReconciler) reconcileConfigPorts(
	ctx context.Context,
	dpuService *dpuservicev1.DPUService,
	dpuClusterConfigs []*dpucluster.Config,
	dpfOperatorConfigNamespace string,
) (reterr error) {
	// If there are no ConfigPorts, we can cleanup the ConfigPorts completely.
	if dpuService.Spec.ConfigPorts == nil || len(dpuClusterConfigs) == 0 {
		if err := r.cleanupAllConfigPorts(ctx, dpuService); err != nil {
			return fmt.Errorf("cleanup config ports: %w", err)
		}
		return nil
	}

	// TODO: Add support for multiple clusters. Currently this is not supported and must be implemented.
	// This is especially for the config ports feature an issue as we cannot pass in the correct service name.
	if len(dpuClusterConfigs) > 1 {
		return fmt.Errorf("multiple clusters are currently not supported")
	}

	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := dpuClusterConfig.Client(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("get client for cluster %q: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}

		serviceList := &corev1.ServiceList{}
		err = dpuClusterClient.List(ctx, serviceList, dpuService.MatchLabels())
		if err != nil {
			errs = append(errs, fmt.Errorf("list services for cluster %q: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}

		dpuNodePorts, err := dpuNodePortsToMap(dpuService, serviceList)
		if err != nil {
			errs = append(errs, fmt.Errorf("config ports not ready yet for cluster %q: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}
		if err := r.reconcileConfigPortEndpointSlices(ctx, dpuClusterConfig, dpuService, dpuNodePorts, dpfOperatorConfigNamespace); err != nil {
			errs = append(errs, fmt.Errorf("reconcile endpoint slices for cluster %q: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}
		if err := r.reconcileConfigPortServices(ctx, dpuClusterConfig.Cluster.Name, dpuService, dpuNodePorts); err != nil {
			errs = append(errs, fmt.Errorf("reconcile services for cluster %q: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}
	}

	return kerrors.NewAggregate(errs)
}

func (r *DPUServiceReconciler) cleanupAllConfigPorts(ctx context.Context, dpuService *dpuservicev1.DPUService) error {
	// TODO: rethink if we want to use DeleteAllOf or a List and Delete.
	// With List and Delete we are able to add proper logging.
	if err := r.Client.DeleteAllOf(ctx, &discoveryv1.EndpointSlice{},
		client.InNamespace(dpuService.GetNamespace()),
		client.HasLabels{dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey},
		dpuService.MatchLabels(),
	); err != nil {
		return fmt.Errorf("delete endpoint slices: %w", err)
	}

	if err := r.Client.DeleteAllOf(ctx, &corev1.Endpoints{},
		client.InNamespace(dpuService.GetNamespace()),
		client.HasLabels{dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey},
		dpuService.MatchLabels(),
	); err != nil {
		return fmt.Errorf("delete endpoints: %w", err)
	}

	if err := r.Client.DeleteAllOf(ctx, &corev1.Service{},
		client.InNamespace(dpuService.GetNamespace()),
		client.HasLabels{dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey},
		dpuService.MatchLabels(),
	); err != nil {
		return fmt.Errorf("delete services: %w", err)
	}
	return nil
}

// ovnEncapIPFromNode returns the first valid IPv4 address from the OVN-Kubernetes node-encap-ips
// annotation on a main-cluster Node. Returns ("", false) when the annotation is absent,
// (ip, true) when the annotation is present and valid, ("", true) when the annotation is present but invalid.
func ovnEncapIPFromNode(ctx context.Context, node *corev1.Node) (string, bool) {
	log := ctrllog.FromContext(ctx)

	value, ok := node.Annotations[ovnNodeEncapIPsAnnotation]
	if !ok {
		return "", false
	}

	var ips []string
	if err := json.Unmarshal([]byte(value), &ips); err != nil {
		log.Info("Invalid OVN node encap IPs annotation",
			"Node", klog.KObj(node),
			"annotation", ovnNodeEncapIPsAnnotation,
			"value", value,
			"error", err,
		)
		return "", true
	}

	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return ip, true
		}
	}

	log.Info("Invalid OVN node encap IPs annotation",
		"Node", klog.KObj(node),
		"annotation", ovnNodeEncapIPsAnnotation,
		"value", value,
	)
	return "", true
}

func nodeInternalIP(node *corev1.Node) string {
	var address string
	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			address = a.Address
		}
	}
	return address
}

// nodeEndpointAddress returns the IP address to use for a config port EndpointSlice endpoint
// for the given DPU-cluster and host cluster nodes. It prefers the OVN-Kubernetes node-encap-ips annotation
// on the host cluster Node when present, otherwise falls back to
// the DPU-cluster node's NodeInternalIP.
func nodeEndpointAddress(ctx context.Context, dpuClusterNode *corev1.Node, hostClusterNode *corev1.Node) string {
	if features.Gates.Enabled(features.ConfigPortsOverHighSpeed) && hostClusterNode != nil {
		if ip, hasAnnotation := ovnEncapIPFromNode(ctx, hostClusterNode); hasAnnotation {
			// NOTE: if annotation is present, we always use it (even if it ends up being invalid).
			return ip
		}
	}

	return nodeInternalIP(dpuClusterNode)
}

func (r *DPUServiceReconciler) reconcileConfigPortEndpointSlices(
	ctx context.Context,
	dpuClusterConfig *dpucluster.Config,
	dpuService *dpuservicev1.DPUService,
	dpuNodePorts map[string]int32,
	dpfOperatorConfigNamespace string,
) error {
	log := ctrllog.FromContext(ctx)
	// First build the EndpointPorts. If there are no ports to expose, return early.
	endpointPorts := []discoveryv1.EndpointPort{}
	for _, port := range dpuService.Spec.ConfigPorts.Ports {
		// TODO: We have to merge the old and new DPU NodePorts for disruptive DPUDeployment upgrades.
		dpuNodePort := dpuNodePorts[port.Name]
		p := discoveryv1.EndpointPort{
			Name:     &port.Name,
			Port:     &dpuNodePort,
			Protocol: &port.Protocol,
		}
		endpointPorts = append(endpointPorts, p)
	}

	endpointSliceName := getConfigPortName(dpuClusterConfig.Cluster.Name, dpuService.GetName())
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointSliceName,
			Namespace: dpuService.GetNamespace(),
		},
	}

	// Cleanup and return early if nothing is to do.
	if len(endpointPorts) == 0 {
		if err := r.Client.Delete(ctx, endpointSlice); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete endpoint slice: %w", err)
			}
			return nil
		}
		log.Info("Deleted EndpointSlice", "Service", klog.KObj(endpointSlice))
		return nil
	}

	log.Info("Reconciling EndpointSlice", "Service", klog.KObj(endpointSlice))
	remoteClient, err := r.getDPUClusterClient(ctx, dpuClusterConfig.Cluster)
	if err != nil {
		return err
	}
	nodeSelectorTerms := []corev1.NodeSelectorTerm{}
	if dpuService.Spec.ServiceDaemonSet != nil && dpuService.Spec.ServiceDaemonSet.NodeSelector != nil {
		nodeSelectorTerms = dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms
	}
	dpuClusterNodes, err := listNodesByNodeAffinity(ctx, remoteClient, nodeSelectorTerms)
	if err != nil {
		return fmt.Errorf("list nodes by node affinity: %w", err)
	}

	endpoints := []discoveryv1.Endpoint{}
	for _, dpuClusterNode := range dpuClusterNodes {
		dpuNodeRef, ok := extractDPUNodeFromNodeLabels(&dpuClusterNode, dpfOperatorConfigNamespace)
		if !ok {
			log.Error(fmt.Errorf("DPUNode reference to which the node %s in the DPUCluster belongs to not found", dpuClusterNode.Name), "Failed to get DPUNode reference for node in DPUCluster")
			continue
		}

		// Get DPUNode from host cluster
		dpuNode := &provisioningv1.DPUNode{}
		if err := r.Client.Get(ctx, dpuNodeRef, dpuNode); err != nil {
			return fmt.Errorf("failed to get DPUNode %s. %w", dpuNodeRef.String(), err)
		}

		// Get k8s Node for the retrieved DPUNode from host cluster
		var hostClusterNode *corev1.Node
		if dpuNode.Status.KubeNodeRef != nil {
			hostClusterNode = &corev1.Node{}
			if err := r.Client.Get(ctx, client.ObjectKey{Name: *dpuNode.Status.KubeNodeRef}, hostClusterNode); err != nil {
				return fmt.Errorf("failed to get k8s Node %s. %w", *dpuNode.Status.KubeNodeRef, err)
			}
		}

		address := nodeEndpointAddress(ctx, &dpuClusterNode, hostClusterNode)
		if address == "" {
			log.Info("Skipping Node because it has no address",
				"Node", klog.KObj(&dpuClusterNode),
				"address", address,
				"dpuNodeName", dpuNode.Name,
			)
			continue
		}

		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: []string{address},
			Conditions: discoveryv1.EndpointConditions{
				Ready: ptr.To(true),
			},
			NodeName: &dpuNode.Name,
		})
	}

	// Check if the EndpointSlice already exists. If not, create it, otherwise use the patcher.
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(endpointSlice), endpointSlice); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	endpointSlice.ObjectMeta.Name = endpointSliceName
	endpointSlice.ObjectMeta.Namespace = dpuService.GetNamespace()
	if endpointSlice.ObjectMeta.Labels == nil {
		endpointSlice.ObjectMeta.Labels = map[string]string{}
	}
	endpointSlice.ObjectMeta.Labels[dpuservicev1.DPUServiceNameLabelKey] = dpuService.GetName()
	endpointSlice.ObjectMeta.Labels[dpuservicev1.DPUServiceNamespaceLabelKey] = dpuService.GetNamespace()
	endpointSlice.ObjectMeta.Labels[dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey] = dpuClusterConfig.Cluster.Name
	endpointSlice.ObjectMeta.Labels[discoveryv1.LabelManagedBy] = dpuServiceControllerName

	// Set the kubernetes.io/service-name to the Service name to label to link the EndpointSlice to the Service.
	// In our case both Service and EndpointSlice have the exact same name.
	endpointSlice.ObjectMeta.Labels[discoveryv1.LabelServiceName] = endpointSliceName

	// Add all dpuService labels to the endpointSlice.
	// This helps select the endpoint slice when the DPUService is created by a DPUSet.
	for k, v := range dpuService.GetLabels() {
		endpointSlice.ObjectMeta.Labels[k] = v
	}

	endpointSlice.AddressType = discoveryv1.AddressTypeIPv4
	endpointSlice.Ports = endpointPorts

	// We only set endpoints if we have any. This enables the DPUService to be ready with 0 scheduled Pods, which
	// is the behavior we want to achieve.
	if len(endpoints) > 0 {
		endpointSlice.Endpoints = endpoints
	}

	endpointSlice.SetManagedFields(nil)
	endpointSlice.SetGroupVersionKind(discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"))

	// Add ownerReference to let the EndpointSlice be garbage collected when the DPUService is deleted.
	owner := metav1.NewControllerRef(dpuService, dpuservicev1.DPUServiceGroupVersionKind)
	endpointSlice.SetOwnerReferences([]metav1.OwnerReference{*owner})
	if err := r.Client.Patch(ctx, endpointSlice, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceControllerName)); err != nil {
		return err
	}
	// Create an Endpoints object for the Service. Both Endpoints and EndpointSlice are needed. Some Kubernetes
	// controllers - e.g. the Prometheus Operator - rely on the older Endpoints API.
	if err := r.reconcileEndpointsFromEndpointsSlice(ctx, endpointSlice, dpuService); err != nil {
		return fmt.Errorf("reconcile endpoints: %w", err)
	}
	return nil
}

func (r *DPUServiceReconciler) reconcileEndpointsFromEndpointsSlice(ctx context.Context, endpointSlice *discoveryv1.EndpointSlice, dpuService *dpuservicev1.DPUService) error {
	log := ctrllog.FromContext(ctx)

	// Create the Endpoints object with the same name and namespace as the EndpointSlice
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointSlice.Name,
			Namespace: endpointSlice.Namespace,
		},
	}

	// Check if the Endpoints already exists
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(endpoints), endpoints); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	// Set metadata
	endpoints.ObjectMeta.Name = endpointSlice.Name
	endpoints.ObjectMeta.Namespace = endpointSlice.Namespace
	if endpoints.ObjectMeta.Labels == nil {
		endpoints.ObjectMeta.Labels = map[string]string{}
	}

	// Copy relevant labels from EndpointSlice
	for key, value := range endpointSlice.Labels {
		endpoints.ObjectMeta.Labels[key] = value
	}

	// Add skip-mirror label to signal this Endpoints object is manually managed
	// and should not be mirrored to EndpointSlices.
	endpoints.ObjectMeta.Labels[discoveryv1.LabelSkipMirror] = "true" //nolint:goconst

	// Convert EndpointSlice to Endpoints format
	endpoints.Subsets = r.convertEndpointSliceToSubsets(endpointSlice)

	endpoints.SetManagedFields(nil)
	endpoints.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Endpoints"))

	// Add ownerReference to let the Endpoints be garbage collected when the DPUService is deleted
	owner := metav1.NewControllerRef(dpuService, dpuservicev1.DPUServiceGroupVersionKind)
	endpoints.SetOwnerReferences([]metav1.OwnerReference{*owner})

	log.Info("Reconciling Endpoints", "Endpoints", klog.KObj(endpoints))
	if err := r.Client.Patch(ctx, endpoints, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceControllerName)); err != nil {
		return err
	}

	return nil
}

func (r *DPUServiceReconciler) convertEndpointSliceToSubsets(endpointSlice *discoveryv1.EndpointSlice) []corev1.EndpointSubset {
	if len(endpointSlice.Ports) == 0 || len(endpointSlice.Endpoints) == 0 {
		return nil
	}

	// Convert ports from EndpointSlice format to Endpoints format
	endpointPorts := []corev1.EndpointPort{}
	for _, port := range endpointSlice.Ports {
		endpointPort := corev1.EndpointPort{
			Port:     *port.Port,
			Protocol: *port.Protocol,
		}
		if port.Name != nil {
			endpointPort.Name = *port.Name
		}
		endpointPorts = append(endpointPorts, endpointPort)
	}

	var readyAddresses []corev1.EndpointAddress
	var notReadyAddresses []corev1.EndpointAddress

	// Convert endpoints from EndpointSlice format to Endpoints format
	for _, endpoint := range endpointSlice.Endpoints {
		for _, address := range endpoint.Addresses {
			endpointAddress := corev1.EndpointAddress{
				IP: address,
			}
			if endpoint.NodeName != nil {
				endpointAddress.NodeName = endpoint.NodeName
			}

			// Check if endpoint is ready
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				readyAddresses = append(readyAddresses, endpointAddress)
			} else {
				notReadyAddresses = append(notReadyAddresses, endpointAddress)
			}
		}
	}

	// Create the subset
	subset := corev1.EndpointSubset{
		Addresses:         readyAddresses,
		NotReadyAddresses: notReadyAddresses,
		Ports:             endpointPorts,
	}

	return []corev1.EndpointSubset{subset}
}

func (r *DPUServiceReconciler) reconcileConfigPortServices(ctx context.Context, clusterName string, dpuService *dpuservicev1.DPUService, dpuNodePorts map[string]int32) error {
	log := ctrllog.FromContext(ctx)
	// First build the ServicePorts. If there are no ports to expose, return early.
	servicePorts := []corev1.ServicePort{}
	for _, port := range dpuService.Spec.ConfigPorts.Ports {
		dpuNodePort := dpuNodePorts[port.Name]
		p := corev1.ServicePort{
			Name:       port.Name,
			TargetPort: intstr.IntOrString{IntVal: dpuNodePort},
			Protocol:   port.Protocol,
			Port:       int32(port.Port),
		}
		if dpuService.Spec.ConfigPorts.ServiceType == corev1.ServiceTypeNodePort {
			// If the NodePort is spec'd, use that. Otherwise, try to get the NodePort from the status.
			// Using the NodePort from the status is useful if the Service gets deleted without the DPUService being deleted.
			if port.NodePort != nil {
				p.NodePort = int32(*port.NodePort)
			} else {
				nodePort := getNodePortFromStatus(dpuService, port.Name, clusterName)
				if nodePort != 0 {
					p.NodePort = int32(nodePort)
				}
			}
		}
		servicePorts = append(servicePorts, p)
	}

	serviceName := getConfigPortName(clusterName, dpuService.GetName())
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: dpuService.GetNamespace(),
		},
	}

	// Cleanup and return early if nothing is to do.
	if len(servicePorts) == 0 {
		if err := r.Client.Delete(ctx, service); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete endpoint slice: %w", err)
			}
			return nil
		}
		log.Info("Deleted Service", "Service", klog.KObj(service))
		return nil
	}

	log.Info("Reconciling Service", "Service", klog.KObj(service))
	// Check if the Service already exists. If not, create it, otherwise use the patcher.
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	service.ObjectMeta.Name = serviceName
	service.ObjectMeta.Namespace = dpuService.GetNamespace()
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = map[string]string{}
	}
	service.ObjectMeta.Labels[dpuservicev1.DPUServiceNameLabelKey] = dpuService.GetName()
	service.ObjectMeta.Labels[dpuservicev1.DPUServiceNamespaceLabelKey] = dpuService.GetNamespace()
	service.ObjectMeta.Labels[dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey] = clusterName

	service.Spec.InternalTrafficPolicy = ptr.To(corev1.ServiceInternalTrafficPolicyLocal)
	if dpuService.Spec.ConfigPorts.ServiceType == corev1.ServiceTypeNodePort {
		service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	}
	service.Spec.Ports = servicePorts

	// Set the Service type to NodePort or ClusterIP. If it's a headless service set the ClusterIP to None.
	switch dpuService.Spec.ConfigPorts.ServiceType {
	case corev1.ServiceTypeNodePort, corev1.ServiceTypeClusterIP:
		service.Spec.Type = dpuService.Spec.ConfigPorts.ServiceType
	case corev1.ClusterIPNone:
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	service.SetManagedFields(nil)
	service.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))

	// Add ownerReference to let the Serviceb be garbage collected when the DPUService is deleted.
	owner := metav1.NewControllerRef(dpuService, dpuservicev1.DPUServiceGroupVersionKind)
	service.SetOwnerReferences([]metav1.OwnerReference{*owner})
	return r.Client.Patch(ctx, service, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceControllerName))
}

func (r *DPUServiceReconciler) updateConfigPortsStatus(ctx context.Context, dpuService *dpuservicev1.DPUService) error {
	// Create a map of the service type and port names that should be exposed.
	configPorts := map[string]bool{}
	if dpuService.Spec.ConfigPorts != nil {
		for _, portSpec := range dpuService.Spec.ConfigPorts.Ports {
			configPorts[portSpec.Name] = true
		}
	}

	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return err
	}

	// Create a map of the cluster names to check if all clusters are present.
	// If not we have to cleanup the ConfigPorts status for the missing clusters.
	dpuClusters := map[string]bool{}
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusters[dpuClusterConfig.Cluster.Name] = true
	}

	serviceList := &corev1.ServiceList{}
	if err := r.Client.List(context.Background(), serviceList,
		client.HasLabels{dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey},
		dpuService.MatchLabels(),
	); err != nil {
		return err
	}

	// Loop over all the services and set the status.
	dpuService.Status.ConfigPorts = map[string][]dpuservicev1.ConfigPort{}
	for _, s := range serviceList.Items {
		dpuCluster := s.Labels[dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey]
		if !dpuClusters[dpuCluster] {
			ctrllog.FromContext(ctx).Info("DPUCluster to expose service not found", "cluster", dpuCluster, "service", s.Name, "namespace", s.Namespace)
			continue
		}
		// If it's a headless service, set the service type to None.
		serviceType := s.Spec.Type
		if s.Spec.ClusterIP == corev1.ClusterIPNone {
			serviceType = corev1.ClusterIPNone
		}

		for _, port := range s.Spec.Ports {
			// If the port is not in the configPorts, skip it.
			if _, ok := configPorts[port.Name]; !ok {
				continue
			}
			if serviceType == corev1.ServiceTypeNodePort && port.NodePort == 0 {
				ctrllog.FromContext(ctx).Error(fmt.Errorf("NodePort is not set for port %q", port.Name), "service", s.Name, "namespace", s.Namespace)
				continue
			}

			p := dpuservicev1.ConfigPort{
				Name:     port.Name,
				Port:     uint16(port.Port),
				Protocol: port.Protocol,
			}
			if serviceType == corev1.ServiceTypeNodePort {
				p.NodePort = ptr.To(uint16(port.NodePort))
			}

			dpuService.Status.ConfigPorts[dpuCluster] = append(dpuService.Status.ConfigPorts[dpuCluster], p)
		}
	}

	for clusterName, exists := range dpuClusters {
		if exists {
			continue
		}
		delete(dpuService.Status.ConfigPorts, clusterName)
	}
	return nil
}

func dpuNodePortsToMap(dpuService *dpuservicev1.DPUService, serviceList *corev1.ServiceList) (map[string]int32, error) {
	if len(serviceList.Items) == 0 || len(serviceList.Items) > 1 {
		return nil, fmt.Errorf("expected 1 service, got %d", len(serviceList.Items))
	}

	// Create a map of the port names that should be exposed.
	configPorts := map[string]bool{}
	for _, portSpec := range dpuService.Spec.ConfigPorts.Ports {
		configPorts[portSpec.Name] = true
	}

	// Loop over all the services and get the nodePort for each port.
	nodePorts := map[string]int32{}
	for _, s := range serviceList.Items {
		for _, port := range s.Spec.Ports {
			if _, ok := configPorts[port.Name]; !ok {
				continue
			}
			if port.NodePort == 0 {
				return nil, fmt.Errorf("nodePort is not set for service %q", s.Name)
			}
			// If it's a match, we add it to the nodePorts map.
			nodePorts[port.Name] = port.NodePort
		}
	}

	// Check if the service has the correct number of ports.
	// This can happen if Kubernetes has not assigned a NodePort yet.
	if len(nodePorts) != len(configPorts) {
		return nil, fmt.Errorf("creation of Service on DPU not ready yet, expected %d nodePorts, got %d",
			len(configPorts), len(nodePorts))
	}

	return nodePorts, nil
}

func getNodePortFromStatus(dpuService *dpuservicev1.DPUService, portName, clusterName string) uint16 {
	for _, configPort := range dpuService.Status.ConfigPorts[clusterName] {
		if configPort.Name == portName && configPort.NodePort != nil {
			return *configPort.NodePort
		}
	}
	return 0
}

// TODO: take care of long names, as the service name is limited to 63 characters.
// TODO: currently we use the dpuService name as the config ports name to simplify the usage of the service
// as an annotation on the Pod using the Downward API.
func getConfigPortName(_, dpuService string) string {
	return dpuService
}

// getProjectName returns the correct project name for the DPUService depending on the cluster it's destined for.
func getProjectName(dpuService *dpuservicev1.DPUService) string {
	if dpuService.Spec.DeployInCluster != nil {
		if *dpuService.Spec.DeployInCluster {
			return argocd.AppProjectNameHost
		}
	}
	return argocd.AppProjectNameDPU
}

func argoCDValuesFromDPUService(serviceDaemonSetValues *dpuservicev1.ServiceDaemonSetValues, dpuService *dpuservicev1.DPUService, clusterName, serviceID string) (*runtime.RawExtension, error) {
	if serviceDaemonSetValues == nil {
		serviceDaemonSetValues = &dpuservicev1.ServiceDaemonSetValues{}
	}
	if serviceDaemonSetValues.Labels == nil {
		serviceDaemonSetValues.Labels = map[string]string{}
	}
	serviceDaemonSetValues.Labels[dpuservicev1.DPFServiceIDLabelKey] = serviceID

	// Convert the typedServiceDaemonSetValues to untyped map[string]interface,
	// so it can be merged with the additional values we want to set.

	rawServiceDaemonSetValues, err := json.Marshal(serviceDaemonSetValues)
	if err != nil {
		return nil, err
	}

	var untypedServiceDaemonSetValues map[string]interface{}
	if err := json.Unmarshal(rawServiceDaemonSetValues, &untypedServiceDaemonSetValues); err != nil {
		return nil, err
	}

	var serviceDaemonSetHelmValues map[string]interface{}

	// System components are implemented as Helm subcharts and can only access
	// values within their own scope or under .Values.global. Parent chart values
	// are not directly accessible, so expose serviceDaemonSet under .global only.
	// Additionally, only system components need the cluster annotations.
	if _, exists := dpuService.GetLabels()[operatorv1.DPFComponentLabelKey]; exists {
		// For System components, we annotate the DPUCluster name and namespace so it can be used as information in pods.
		// E.g. for annotating logs and metrics with the DPUCluster name and Namespace via open-telemetry-collector.
		annotations, _, _ := unstructured.NestedStringMap(untypedServiceDaemonSetValues, "annotations")
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[provisioningv1.DPUClusterNameLabelKey] = clusterName
		annotations[provisioningv1.DPUClusterNamespaceLabelKey] = dpuService.GetNamespace()
		if err := unstructured.SetNestedStringMap(untypedServiceDaemonSetValues, annotations, "annotations"); err != nil {
			return nil, err
		}

		// Move the additional values to the global level.
		// System components are implemented as a single chart with subcharts.
		// Values across the components must be exposed at `.Values.global`, otherwise
		// they can only access values within their own scope. Other parent chart values
		// are not directly accessible.
		serviceDaemonSetHelmValues = map[string]interface{}{
			"global": map[string]interface{}{
				"serviceDaemonSet": untypedServiceDaemonSetValues,
			},
		}
	} else {
		serviceDaemonSetHelmValues = map[string]interface{}{
			"serviceDaemonSet": untypedServiceDaemonSetValues,
		}
	}

	// Unmarshal the values from the DPUService.
	var dpuServiceHelmValues map[string]interface{}
	if dpuService.Spec.HelmChart.Values != nil {
		if err := json.Unmarshal(dpuService.Spec.HelmChart.Values.Raw, &dpuServiceHelmValues); err != nil {
			return nil, err
		}
	}

	// Combine values
	combinedValues := dpuserviceutils.MergeMaps(serviceDaemonSetHelmValues, dpuServiceHelmValues)

	// Override with values to expose config ports.
	if dpuService.Spec.ConfigPorts != nil {
		configPortsValues := argoCDValuesForConfigPorts(dpuService)
		combinedValues = dpuserviceutils.MergeMaps(combinedValues, configPortsValues)
	}

	data, err := json.Marshal(combinedValues)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{Raw: data}, nil
}

func argoCDValuesForConfigPorts(dpuService *dpuservicev1.DPUService) map[string]interface{} {
	ports := map[string]interface{}{}
	for _, p := range dpuService.Spec.ConfigPorts.Ports {
		ports[p.Name] = true
	}

	values := map[string]interface{}{
		"exposedPorts": map[string]interface{}{
			"labels": dpuService.MatchLabels(),
			"ports":  ports,
		},
	}
	return values
}

// DPUClusterToDPUService ensures all DPUServices are updated each time there is an update to a DPUCluster.
func (r *DPUServiceReconciler) DPUClusterToDPUService(ctx context.Context, _ client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServiceList); err != nil {
		return nil
	}
	for _, m := range dpuServiceList.Items {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// dpfOperatorConfigToDPUServices enqueues all DPUServices when the
// DPFOperatorConfig changes, so breakglass toggles (e.g. privileged-pod
// enforcement) take effect without waiting for the next resync.
func (r *DPUServiceReconciler) dpfOperatorConfigToDPUServices(ctx context.Context, _ client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServiceList); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list DPUServices for DPFOperatorConfig watch")
		return nil
	}
	for _, m := range dpuServiceList.Items {
		result = append(result, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: m.Namespace, Name: m.Name}})
	}
	return result
}

// requestsForChangeByLabel ensures a DPUService is updated each time there is an update to the linked Argo Application.
func (r *DPUServiceReconciler) requestsForChangeByLabel(_ context.Context, o client.Object) []ctrl.Request {
	namespace, ok := o.GetLabels()[dpuservicev1.DPUServiceNamespaceLabelKey]
	if !ok {
		return nil
	}
	name, ok := o.GetLabels()[dpuservicev1.DPUServiceNameLabelKey]
	if !ok {
		return nil
	}
	return []ctrl.Request{{
		NamespacedName: client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		},
	}}
}

// generateServiceID generates a ServiceID for the given DPUService.
// If the DPUService has a ServiceID in spec, it returns that value.
// Otherwise, it generates a serviceID with dpuService name, namespace and group kind.
// The digest is shortened to 10 characters to keep the ServiceID short.
func generateServiceID(dpuService *dpuservicev1.DPUService) string {
	if dpuService.Spec.ServiceID != nil {
		return *dpuService.Spec.ServiceID
	}
	serviceDigest := digest.FromObjects(dpuService.GroupVersionKind().GroupKind().String(), dpuService.Name, dpuService.Namespace)
	return digest.Short(serviceDigest, 10)
}

func (r *DPUServiceReconciler) requestsForDPUServiceInterfaceChange(ctx context.Context, o client.Object) []reconcile.Request {
	dsi, ok := o.(*dpuservicev1.DPUServiceInterface)
	if !ok {
		err := fmt.Errorf("expected a DPUServiceInterface but got %T", o)
		ctrl.LoggerFrom(ctx).Error(err, "failed to get requests for DPUServiceInterface change")
		return nil
	}

	var list dpuservicev1.DPUServiceList
	if err := r.List(ctx, &list, client.MatchingFields{
		dpuServiceInterfacesField: client.ObjectKeyFromObject(dsi).String(),
	}); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list DPUService objects for DPUServiceInterface change")
		return nil
	}

	reqs := []reconcile.Request{}
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return reqs
}

func getInterfaceNamespacedNames(dpuservice *dpuservicev1.DPUService) ([]string, error) {
	if dpuservice.Spec.Interfaces == nil {
		return nil, fmt.Errorf("no interfaces in DPUService")
	}
	namespacedNames := []string{}
	for _, interfaceName := range dpuservice.Spec.Interfaces {
		namespacedNames = append(namespacedNames, types.NamespacedName{
			Name:      interfaceName,
			Namespace: dpuservice.Namespace}.String())
	}
	return namespacedNames, nil
}

// isDPUServiceInterfaceInUse checks if the DPUServiceInterface is in use by another DPUService.
// Returns true if the DPUServiceInterface is in use, and true if the DPUServiceInterface is found.
func isDPUServiceInterfaceInUse(dpuServiceInterface *dpuservicev1.DPUServiceInterface, dpuService *dpuservicev1.DPUService) (inUse bool, exist bool) {
	annotations := dpuServiceInterface.GetAnnotations()
	if annotations != nil {
		_, ok := annotations[dpuservicev1.DPUServiceInterfaceAnnotationKey]
		if ok {
			exist = true
		}
		if ok && annotations[dpuservicev1.DPUServiceInterfaceAnnotationKey] != dpuService.Name {
			inUse = true
			return
		}
	}
	return
}

func isSkipReconcileAnnotationSet(app *argov1.Application) bool {
	return app.Annotations != nil && app.Annotations[annotationKeyAppSkipReconcile] == strconv.FormatBool(true)
}

// unpauseApplication unpauses an ArgoCD Application by removing the skip-reconcile annotation.
// Returns nil if the application is successfully unpaused or if it's not found (already deleted).
func (r *DPUServiceReconciler) unpauseApplication(ctx context.Context, app *argov1.Application) error {
	if !isSkipReconcileAnnotationSet(app) {
		return nil
	}

	if app.Annotations == nil {
		app.Annotations = make(map[string]string)
	}
	app.Annotations[annotationKeyAppSkipReconcile] = "false"

	if err := r.Client.Update(ctx, app); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("unpause ArgoCD application: %w", err)
	}
	return nil
}

// requestsForHostNode enqueues DPUServices with config ports which target a DPU associated with the host-cluster node.
func (r *DPUServiceReconciler) requestsForHostNode(ctx context.Context, obj client.Object) []ctrl.Request {
	log := ctrllog.FromContext(ctx)
	hostNode, ok := obj.(*corev1.Node)
	if !ok {
		log.Error(fmt.Errorf("event expected a Node but got a %T", obj), "Failed to convert object")
		return nil
	}

	// lookup DPUNode by KubeNodeRef
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.Client.List(ctx, dpuNodeList, client.MatchingFields{dpuNodeKubeNodeRefField: hostNode.Name}); err != nil {
		log.Error(err, "failed to list DPUNodes for host Node change", "node", hostNode.Name)
		return nil
	}
	if len(dpuNodeList.Items) == 0 {
		return nil
	}

	if len(dpuNodeList.Items) > 1 {
		log.Error(fmt.Errorf("multiple DPUNodes found for host Node %s", hostNode.Name), "Failed to get DPU node for host Node")
		return nil
	}

	dpuNode := &dpuNodeList.Items[0]
	// get dpus of the DPU node
	selector := dpuselector.New(dpuselector.WithIndexerField{FieldName: dpuNodeNameField})
	dpus, err := selector.ListDPUsForNode(ctx, r.Client, dpuNode)
	if err != nil {
		log.Error(err, "failed to list DPUs for DPU node", "dpuNode", dpuNode.Name)
		return nil
	}

	// get matching k8s nodes in the DPU cluster
	dpuK8sNodes := []*corev1.Node{}
	for _, dpu := range dpus {
		clusterKey := client.ObjectKey{Namespace: dpu.Spec.Cluster.Namespace, Name: dpu.Spec.Cluster.Name}
		dpuClusterClient, err := r.RemoteCache.GetClient(clusterKey)
		if err != nil {
			log.Error(err, "failed to get DPU cluster client, skipping DPU", "cluster", clusterKey.String(), "dpu", dpu.Name)
			continue
		}

		// DPU name is also the name of the K8s node in the DPU cluster
		dpuK8sNode := &corev1.Node{}
		err = dpuClusterClient.Get(ctx, client.ObjectKey{Name: dpu.Name}, dpuK8sNode)
		if err != nil {
			log.Error(err, "failed to get K8s node for DPU", "cluster", clusterKey.String(), "dpu", dpu.Name)
			continue
		}

		dpuK8sNodes = append(dpuK8sNodes, dpuK8sNode)
	}

	if len(dpuK8sNodes) == 0 {
		return nil
	}

	// list all DPUServices with config ports
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServiceList, client.MatchingFields{dpuServiceConfigPortsField: "true"}); err != nil {
		log.Error(err, "failed to list DPUServices for host Node change", "node", hostNode.Name)
		return nil
	}

	// enqueue requests for services which target the DPUs associated with the host-cluster node
	var requests []reconcile.Request
	for _, dpuService := range dpuServiceList.Items {
		var nodeSelector *corev1.NodeSelector
		if dpuService.Spec.ServiceDaemonSet != nil && dpuService.Spec.ServiceDaemonSet.NodeSelector != nil {
			nodeSelector = dpuService.Spec.ServiceDaemonSet.NodeSelector
		}

		for _, dpuK8sNode := range dpuK8sNodes {
			matches, err := nodeMatchesNodeSelector(dpuK8sNode, nodeSelector)
			if err != nil {
				log.Error(err, "failed to match DPU K8s Node against NodeSelector",
					"Node", dpuK8sNode.Name,
					"DPUService", client.ObjectKeyFromObject(&dpuService).String(),
					"NodeSelector", nodeSelector)
				continue
			}

			if matches {
				log.V(3).Info("DPU K8s Node matches DPUService NodeSelector, triggering reconciliation",
					"Node", dpuK8sNode.Name,
					"DPUService", client.ObjectKeyFromObject(&dpuService).String())
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&dpuService)})
				// continue to the next DPUService
				break
			}
		}
	}

	return requests
}

// WatchNodes sets up a watcher for corev1.Node objects in the DPU clusters.
func (r *DPUServiceReconciler) WatchNodes(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuservice-node-watcher",
		Kind:         &corev1.Node{},
		EventHandler: &nodeEventHandler{hostClient: r.Client},
		Predicates: []predicate.Predicate{
			nodeAddressPredicate(),
		},
		Watcher: r.controller,
	}), nil
}

// getDPUClusterClient returns a client for the given DPUCluster.
// It first tries to get a client from the remote cache.
// If that fails because the remote cache has not yet bootstrapped for this
// cluster (no accessor, or accessor present but not yet connected), it falls
// back to a direct client. This is the case when prereqs were never
// reconciled yet, since the remote cache needs ServiceChain/ServiceInterface
// CRDs to be provisioned before it can connect.
func (r *DPUServiceReconciler) getDPUClusterClient(ctx context.Context, dpuCluster *provisioningv1.DPUCluster) (client.Client, error) {
	log := ctrllog.FromContext(ctx)
	clusterKey := client.ObjectKeyFromObject(dpuCluster)

	dpuClusterClient, err := r.RemoteCache.GetClient(clusterKey)
	if err != nil {
		if errors.Is(err, dpucluster.ErrDPUClusterNoConnectionAvailable) ||
			errors.Is(err, dpucluster.ErrDPUClusterNotConnected) {
			log.V(1).Info("Using direct DPU cluster client for prerequisites", "dpuCluster", clusterKey, "remoteCacheError", err.Error())
			return dpucluster.NewConfig(r.Client, dpuCluster).Client(ctx)
		}
		return nil, err
	}

	return dpuClusterClient, nil
}

// nodeEventHandler is a handler for node events
type nodeEventHandler struct {
	hostClient client.Client
}

// handleNodeEventHelper verifies if the node matches any DPUService NodeSelector and enqueues a reconcile request.
func (p *nodeEventHandler) handleNodeEventHelper(ctx context.Context, node *corev1.Node, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	log := ctrllog.FromContext(ctx)

	// Check for new DPUNodeNameLabel first, fall back to deprecated HostNameDPULabelKey for backward compatibility
	if _, exists := node.Labels[provisioningv1.DPUNodeNameLabel]; !exists {
		if _, exists := node.Labels[util.HostNameDPULabelKey]; !exists {
			log.Error(fmt.Errorf("DPUNode reference label not found for node %s", node.Name), "Failed to get DPUNode reference for node")
			return
		}
	}

	if len(node.Status.Addresses) == 0 {
		log.V(1).Info("Node has no addresses, skipping", "node", node.Name)
		return
	}

	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := p.hostClient.List(ctx, dpuServiceList, client.MatchingFields{dpuServiceConfigPortsField: "true"}); err != nil {
		log.Error(err, "failed to list DPUServices for DPUCluster Node change", "node", node.Name)
		return
	}

	var requests []reconcile.Request
	for _, dpuService := range dpuServiceList.Items {
		// Check if the DPUService has a ServiceDaemonSet with NodeSelector.
		// If no NodeSelector is defined, the default behavior is to target all DPU nodes.
		var nodeSelector *corev1.NodeSelector
		if dpuService.Spec.ServiceDaemonSet != nil && dpuService.Spec.ServiceDaemonSet.NodeSelector != nil {
			nodeSelector = dpuService.Spec.ServiceDaemonSet.NodeSelector
		}

		matches, err := nodeMatchesNodeSelector(node, nodeSelector)
		if err != nil {
			log.Error(err, "failed to match node against NodeSelector",
				"Node", node.Name,
				"DPUService", dpuService.GetName(),
				"NodeSelector", &nodeSelector)
			continue
		}

		if matches {
			log.V(3).Info("Node matches DPUService NodeSelector, triggering reconciliation",
				"Node", node.Name,
				"DPUService", fmt.Sprintf("%s/%s", dpuService.GetNamespace(), dpuService.GetName()))
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Namespace: dpuService.GetNamespace(),
					Name:      dpuService.GetName(),
				},
			})
		}
	}

	enqueueRequests(requests, q)
}

func (p *nodeEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	node, ok := e.Object.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.Object), "Failed to convert object")
		return
	}

	p.handleNodeEventHelper(ctx, node, q)
}

// Update finds the host node name from the node labels and enqueues a request for the host node
func (p *nodeEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	node, ok := e.ObjectNew.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.ObjectNew), "Failed to convert object")
		return
	}

	p.handleNodeEventHelper(ctx, node, q)
}

// Delete finds the host node name from the node labels and enqueues a request for the host node
func (p *nodeEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	node, ok := e.Object.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.Object), "Failed to convert object")
		return
	}

	p.handleNodeEventHelper(ctx, node, q)
}

func (p *nodeEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	// Generic is not implemented
	// because we are not interested in generic events and the predicate filters out all generic events
}
