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
	"fmt"
	"reflect"
	"strconv"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/argocd"
	"github.com/nvidia/doca-platform/internal/dpuservice/predicates"
	dpuserviceutils "github.com/nvidia/doca-platform/internal/dpuservice/utils"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"

	"github.com/fluxcd/pkg/runtime/patch"
	multusTypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUServiceReconciler reconciles a DPUService object
type DPUServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// pauseDPUServiceReconciler pauses the DPUService Reconciler by doing noop reconciliation loops. This is helpful to
// make tests faster and less complex
var pauseDPUServiceReconciler bool

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps;secrets,verbs=get;list;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=argoproj.io,resources=appprojects;applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch

const (
	dpuServiceControllerName = "dpuservice-manager"

	// TODO: These constants don't belong here and should be moved as they're shared with other packages.
	argoCDSecretLabelKey          = "argocd.argoproj.io/secret-type"
	argoCDSecretLabelValue        = "cluster"
	dpuAppProjectName             = "doca-platform-project-dpu"
	hostAppProjectName            = "doca-platform-project-host"
	networkAnnotationKey          = "k8s.v1.cni.cncf.io/networks"
	annotationKeyAppSkipReconcile = "argocd.argoproj.io/skip-reconcile"
)

// applyPatchOptions contains options which are passed to every `client.Apply` patch.
var applyPatchOptions = []client.PatchOption{
	client.ForceOwnership,
	client.FieldOwner(dpuServiceControllerName),
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUServiceReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &dpuservicev1.DPUService{}, dpuservicev1.InterfaceIndexKey,
		func(o client.Object) []string {
			obj := o.(*dpuservicev1.DPUService)
			namespacedNames, err := getInterfaceNamespacedNames(obj)
			if err != nil {
				return nil
			}
			return namespacedNames
		},
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUService{}).
		Watches(&argov1.Application{}, handler.EnqueueRequestsFromMapFunc(r.requestsForChangeByLabel)).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.requestsForChangeByLabel)).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(r.requestsForChangeByLabel)).
		Watches(
			&dpuservicev1.DPUServiceInterface{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForDPUServiceInterfaceChange),
			builder.WithPredicates(predicates.DPUServiceInterfaceChangePredicate{}),
		).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.DPUClusterToDPUService)).
		Complete(r)
}

// Reconcile reconciles changes in a DPUService.
func (r *DPUServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	if pauseDPUServiceReconciler {
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
		if isSkipReconcileAnnotationSet(&app) {
			app.Annotations[annotationKeyAppSkipReconcile] = "false"
			if err := r.Client.Update(ctx, &app); err != nil {
				// Tolerate if the application is not found and already deleted.
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
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
		"mybrsfc":   {},
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

	// Get the list of clusters this DPUService targets.
	// TODO: Add some way to check if the clusters are healthy. Reconciler should retry clusters if they're unready.
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return err
	}

	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := r.Client.List(ctx, &dpfOperatorConfigList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("list DPFOperatorConfigs: %w", err)
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return fmt.Errorf("exactly one DPFOperatorConfig necessary")
	}
	dpfOperatorConfig := &dpfOperatorConfigList.Items[0]

	// Return early if the dpuService is paused.
	if dpuService.IsPaused() {
		if err := r.pauseApplication(ctx, dpuClusterConfigs, dpuService, dpfOperatorConfig.GetNamespace()); err != nil {
			return err
		}
		log.Info("reconciliation is paused for the DPUService")
		return nil
	}

	serviceDaemonSet, interfaces, err := r.reconcileInterfaces(ctx, dpuService)
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
	if err = r.reconcileApplication(ctx, dpuClusterConfigs, dpuService, serviceDaemonSet, dpfOperatorConfig.GetNamespace()); err != nil {
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

	if err = r.reconcileConfigPorts(ctx, dpuService, dpuClusterConfigs); err != nil {
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

func (r *DPUServiceReconciler) reconcileApplicationPrereqs(ctx context.Context, dpuService *dpuservicev1.DPUService, dpuClusterConfigs []*dpucluster.Config, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	// Ensure the DPUService namespace exists in target clusters.
	project := getProjectName(dpuService)
	// TODO: think about how to cleanup the namespace in the DPU.
	if err := r.ensureNamespaces(ctx, dpuClusterConfigs, project, dpuService.GetNamespace()); err != nil {
		return fmt.Errorf("cluster namespaces: %v", err)
	}

	// Ensure the Argo secret for each cluster is up-to-date.
	if err := r.reconcileArgoSecrets(ctx, dpuClusterConfigs, dpfOperatorConfig); err != nil {
		return fmt.Errorf("ArgoSecrets: %v", err)
	}

	//  Ensure the ArgoCD AppProject exists and is up-to-date.
	if err := r.reconcileAppProject(ctx, dpuClusterConfigs, dpfOperatorConfig); err != nil {
		return fmt.Errorf("AppProject: %v", err)
	}

	// Reconcile the ImagePullSecrets.
	if err := r.reconcileImagePullSecrets(ctx, dpuClusterConfigs, dpuService); err != nil {
		return fmt.Errorf("ImagePullSecrets: %v", err)
	}

	return nil
}

// reconcileInterfaces reconciles the DPUServiceInterfaces associated with the DPUService.
func (r *DPUServiceReconciler) reconcileInterfaces(ctx context.Context, dpuService *dpuservicev1.DPUService) (*dpuservicev1.ServiceDaemonSetValues, []*dpuservicev1.DPUServiceInterface, error) {
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
		dpuServiceID := dpuService.Spec.ServiceID
		if dpuServiceID != nil && service.ServiceID != *dpuServiceID {
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
	if project == dpuAppProjectName {
		for _, dpuClusterConfig := range dpuClusterConfigs {
			dpuClusterClient, err := dpuClusterConfig.Client(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("get cluster %s: %v", dpuClusterConfig.Cluster.Name, err))
				continue
			}
			if err := utils.EnsureNamespace(ctx, dpuClusterClient, dpuNamespace); err != nil {
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

// reconcileArgoSecrets reconciles a Secret in the format that ArgoCD expects. It uses data from the control plane secret.
func (r *DPUServiceReconciler) reconcileArgoSecrets(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	log := ctrllog.FromContext(ctx)

	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		// Get the control plane kubeconfig
		adminConfig, err := dpuClusterConfig.Kubeconfig(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// Template an argoSecret using information from the control plane secret.
		argoSecret, err := createArgoSecretFromKubeconfig(adminConfig, dpfOperatorConfig.GetNamespace(), dpuClusterConfig)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// Add owner reference ArgoSecret->DPFOperatorConfig to ensure that the Secret will be deleted with the DPFOperatorConfig.
		// This ensures that we should have no orphaned ArgoCD Secrets.
		owner := metav1.NewControllerRef(dpfOperatorConfig, operatorv1.DPFOperatorConfigGroupVersionKind)
		argoSecret.SetOwnerReferences([]metav1.OwnerReference{*owner})

		// Create or patch
		log.Info("Patching Secrets for DPF clusters")
		if err := r.Client.Patch(ctx, argoSecret, client.Apply, applyPatchOptions...); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	return kerrors.NewAggregate(errs)
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
		if syncStatus == argov1.SyncStatusCodeSynced && healthStatus == argov1.HealthStatusHealthy {
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

	// Update condition and requeue if there are any errors, or if there are fewer applications than we have clusters.
	if len(unreadyApplications) > 0 || len(applicationList.Items) != len(dpuClusterConfigs) {
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
		dpuClusterClient, err := dpuClusterConfig.Client(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, secret := range secretsToPatch {
			if err := dpuClusterClient.Patch(ctx, secret, client.Apply, applyPatchOptions...); err != nil {
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

func (r *DPUServiceReconciler) reconcileAppProject(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	log := ctrllog.FromContext(ctx)

	clusterKeys := []types.NamespacedName{}
	for _, dpuClusterConfig := range dpuClusterConfigs {
		clusterKeys = append(clusterKeys, types.NamespacedName{Namespace: dpuClusterConfig.Cluster.Namespace, Name: dpuClusterConfig.Cluster.Name})
	}
	dpuAppProject := argocd.NewAppProject(dpfOperatorConfig.Namespace, dpuAppProjectName, clusterKeys)
	// Add owner reference AppProject->DPFOperatorConfig to ensure that the AppProject will be deleted with the DPFOperatorConfig.
	// This ensures that we should have no orphaned ArgoCD AppProjects.
	owner := metav1.NewControllerRef(dpfOperatorConfig, operatorv1.DPFOperatorConfigGroupVersionKind)
	dpuAppProject.SetOwnerReferences([]metav1.OwnerReference{*owner})

	log.Info("Patching AppProject for DPU clusters")
	if err := r.Client.Patch(ctx, dpuAppProject, client.Apply, applyPatchOptions...); err != nil {
		return err
	}

	inClusterKey := []types.NamespacedName{{Namespace: "*", Name: "in-cluster"}}
	hostAppProject := argocd.NewAppProject(dpfOperatorConfig.Namespace, hostAppProjectName, inClusterKey)
	// Add owner reference, same as above.
	hostAppProject.SetOwnerReferences([]metav1.OwnerReference{*owner})

	log.Info("Patching AppProject for Host cluster")
	if err := r.Client.Patch(ctx, hostAppProject, client.Apply, applyPatchOptions...); err != nil {
		return err
	}

	return nil
}

func (r *DPUServiceReconciler) reconcileApplication(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpuService *dpuservicev1.DPUService, serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, dpfOperatorConfigNamespace string) error {
	project := getProjectName(dpuService)
	if project == dpuAppProjectName {
		for _, dpuClusterConfig := range dpuClusterConfigs {
			if err := r.ensureApplication(ctx, dpuService, serviceDaemonSet, dpuClusterConfig.Cluster.Name, dpfOperatorConfigNamespace); err != nil {
				return err
			}
		}
	} else {
		return r.ensureApplication(ctx, dpuService, serviceDaemonSet, "in-cluster", dpfOperatorConfigNamespace)
	}
	return nil
}

func (r *DPUServiceReconciler) ensureApplication(ctx context.Context, dpuService *dpuservicev1.DPUService, serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, clusterName, dpfOperatorConfigNamespace string) error {
	log := ctrllog.FromContext(ctx)
	project := getProjectName(dpuService)
	values, err := argoCDValuesFromDPUService(serviceDaemonSet, dpuService)
	if err != nil {
		return err
	}

	argoApplication := argocd.NewApplication(dpfOperatorConfigNamespace, project, dpuService, values, clusterName)
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

func (r *DPUServiceReconciler) pauseApplication(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpuService *dpuservicev1.DPUService, dpfOperatorConfigNamespace string) error {
	project := getProjectName(dpuService)
	if project == dpuAppProjectName {
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
	namespacedName := types.NamespacedName{Namespace: dpfOperatorConfigNamespace, Name: fmt.Sprintf("%v-%v", clustername, name)}
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

func (r *DPUServiceReconciler) reconcileConfigPorts(ctx context.Context, dpuService *dpuservicev1.DPUService, dpuClusterConfigs []*dpucluster.Config) (reterr error) {
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
		if err := r.reconcileConfigPortEndpointSlices(ctx, dpuClusterConfig.Cluster.Name, dpuService, dpuNodePorts); err != nil {
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

	if err := r.Client.DeleteAllOf(ctx, &corev1.Service{},
		client.InNamespace(dpuService.GetNamespace()),
		client.HasLabels{dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey},
		dpuService.MatchLabels(),
	); err != nil {
		return fmt.Errorf("delete services: %w", err)
	}
	return nil
}

func (r *DPUServiceReconciler) reconcileConfigPortEndpointSlices(ctx context.Context, clusterName string, dpuService *dpuservicev1.DPUService, dpuNodePorts map[string]int32) error {
	log := ctrllog.FromContext(ctx)
	// First build the EndpointPorts. If there are no ports to expose, return early.
	endpointPorts := []discoveryv1.EndpointPort{}
	for _, port := range dpuService.Spec.ConfigPorts.Ports {
		// TODO: We have to merge the old and new DPU NodePorts for un-disruptive DPUDeployment upgrades.
		dpuNodePort := dpuNodePorts[port.Name]
		p := discoveryv1.EndpointPort{
			Name:     &port.Name,
			Port:     &dpuNodePort,
			Protocol: &port.Protocol,
		}
		endpointPorts = append(endpointPorts, p)
	}

	endpointSliceName := getConfigPortName(clusterName, dpuService.GetName())
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
	// Get the list of DPUs in the cluster to build the Endpoints.
	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList); err != nil {
		return err
	}
	endpoints := []discoveryv1.Endpoint{}
	for _, dpu := range dpuList.Items {
		var hostname, address string
		for _, a := range dpu.Status.Addresses {
			switch a.Type {
			case corev1.NodeInternalIP:
				address = a.Address
			case corev1.NodeHostName:
				hostname = a.Address
			}
		}
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: []string{address},
			Conditions: discoveryv1.EndpointConditions{
				Ready: ptr.To(true),
			},
			Hostname: &hostname,
			NodeName: &dpu.Spec.DPUNodeName,
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
	endpointSlice.ObjectMeta.Labels[dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey] = clusterName
	endpointSlice.ObjectMeta.Labels[discoveryv1.LabelManagedBy] = dpuServiceControllerName

	// Set the kubernetes.io/service-name to the Service name to label to link the EndpointSlice to the Service.
	// In our case both Service and EndpointSlice have the exact same name.
	endpointSlice.ObjectMeta.Labels[discoveryv1.LabelServiceName] = endpointSliceName

	endpointSlice.AddressType = discoveryv1.AddressTypeIPv4
	endpointSlice.Ports = endpointPorts
	endpointSlice.Endpoints = endpoints

	endpointSlice.SetManagedFields(nil)
	endpointSlice.SetGroupVersionKind(discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"))

	// Add ownerReference to let the EndpointSlice be garbage collected when the DPUService is deleted.
	owner := metav1.NewControllerRef(dpuService, dpuservicev1.DPUServiceGroupVersionKind)
	endpointSlice.SetOwnerReferences([]metav1.OwnerReference{*owner})
	return r.Client.Patch(ctx, endpointSlice, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceControllerName))
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
			len(dpuService.Spec.ConfigPorts.Ports), len(nodePorts))
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

// config is used to marshal the config section of the argoCD secret data.
type config struct {
	TLSClientConfig tlsClientConfig `json:"tlsClientConfig"`
}

// tlsClientConfig is used to marshal the tlsClientConfig section of the argoCD secret data.config.
type tlsClientConfig struct {
	CaData   []byte `json:"caData,omitempty"`
	KeyData  []byte `json:"keyData,omitempty"`
	CertData []byte `json:"certData,omitempty"`
}

// createArgoSecretFromKubeconfig generates an ArgoCD cluster secret from the given kubeconfig.
func createArgoSecretFromKubeconfig(kubeconfig *api.Config, dpfOperatorConfigNamespace string, dpuClusterConfig *dpucluster.Config) (*corev1.Secret, error) {
	name, cluster := getRandomKVPair(kubeconfig.Clusters)
	if name == "" {
		return nil, fmt.Errorf("no clusters found in kubeconfig")
	}
	userName, user := getRandomKVPair(kubeconfig.AuthInfos)
	if userName == "" {
		return nil, fmt.Errorf("no users found in kubeconfig")
	}

	secretConfig, err := json.Marshal(config{TLSClientConfig: tlsClientConfig{
		CaData:   cluster.CertificateAuthorityData,
		KeyData:  user.ClientKeyData,
		CertData: user.ClientCertificateData,
	}})
	if err != nil {
		return nil, err
	}
	return createArgoCDSecret(secretConfig, dpfOperatorConfigNamespace, dpuClusterConfig, cluster.Server), nil
}

// createArgoCDSecret templates an ArgoCD cluster Secret with the passed values.
func createArgoCDSecret(secretConfig []byte, dpfOperatorConfigNamespace string, dpuCluster *dpucluster.Config, clusterConfigServer string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			// The secret name is the dpuCluster name. DPUClusters must have unique names.
			Name:      dpuCluster.ClusterNamespaceName(),
			Namespace: dpfOperatorConfigNamespace,
			Labels: map[string]string{
				argoCDSecretLabelKey:                       argoCDSecretLabelValue,
				provisioningv1.DPUClusterNameLabelKey:      dpuCluster.Cluster.Name,
				provisioningv1.DPUClusterNamespaceLabelKey: dpuCluster.Cluster.Namespace,
				operatorv1.DPFComponentLabelKey:            dpuServiceControllerName,
			},
			OwnerReferences: nil,
			Annotations:     nil,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"name":   []byte(dpuCluster.Cluster.Name),
			"server": []byte(clusterConfigServer),
			"config": secretConfig,
		},
	}
}

// getProjectName returns the correct project name for the DPUService depending on the cluster it's destined for.
func getProjectName(dpuService *dpuservicev1.DPUService) string {
	if dpuService.Spec.DeployInCluster != nil {
		if *dpuService.Spec.DeployInCluster {
			return hostAppProjectName
		}
	}
	return dpuAppProjectName
}

func argoCDValuesFromDPUService(serviceDaemonSet *dpuservicev1.ServiceDaemonSetValues, dpuService *dpuservicev1.DPUService) (*runtime.RawExtension, error) {
	if serviceDaemonSet == nil {
		serviceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{}
	}
	if serviceDaemonSet.Labels == nil {
		serviceDaemonSet.Labels = map[string]string{}
	}
	if dpuService.Spec.ServiceID != nil {
		serviceDaemonSet.Labels[dpuservicev1.DPFServiceIDLabelKey] = *dpuService.Spec.ServiceID
	}

	// Marshal the ServiceDaemonSet and other values to map[string]interface to combine them.
	var otherValues, serviceDaemonSetValues map[string]interface{}
	if dpuService.Spec.HelmChart.Values != nil {
		if err := json.Unmarshal(dpuService.Spec.HelmChart.Values.Raw, &otherValues); err != nil {
			return nil, err
		}
	}

	// Unmarshal the ServiceDaemonSet to get the byte representation.
	dsValuesData, err := json.Marshal(serviceDaemonSet)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(dsValuesData, &serviceDaemonSetValues); err != nil {
		return nil, err
	}

	serviceDaemonSetValuesWithKey := map[string]interface{}{
		"serviceDaemonSet": serviceDaemonSetValues,
	}

	// Combine values
	combinedValues := dpuserviceutils.MergeMaps(serviceDaemonSetValuesWithKey, otherValues)

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

func (r *DPUServiceReconciler) requestsForDPUServiceInterfaceChange(ctx context.Context, o client.Object) []reconcile.Request {
	dsi, ok := o.(*dpuservicev1.DPUServiceInterface)
	if !ok {
		err := fmt.Errorf("expected a DPUServiceInterface but got %T", o)
		ctrl.LoggerFrom(ctx).Error(err, "failed to get requests for DPUServiceInterface change")
		return nil
	}

	var list dpuservicev1.DPUServiceList
	if err := r.List(ctx, &list, client.MatchingFields{
		dpuservicev1.InterfaceIndexKey: client.ObjectKeyFromObject(dsi).String(),
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

func getRandomKVPair[T any](m map[string]T) (string, T) {
	var result T
	for k, v := range m {
		return k, v
	}
	return "", result
}

func isSkipReconcileAnnotationSet(app *argov1.Application) bool {
	return app.Annotations != nil && app.Annotations[annotationKeyAppSkipReconcile] == strconv.FormatBool(true)
}
