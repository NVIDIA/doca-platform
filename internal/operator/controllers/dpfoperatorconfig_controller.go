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
	_ "embed"
	"fmt"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	"github.com/fluxcd/pkg/runtime/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultDPFOperatorConfigSingletonName is the default single valid name of the DPFOperatorConfig.
	DefaultDPFOperatorConfigSingletonName = "dpfoperatorconfig"
	// DefaultDPFOperatorConfigSingletonNamespace is the default single valid name of the DPFOperatorConfig.
	DefaultDPFOperatorConfigSingletonNamespace = "dpf-operator-system"
)

const (
	dpfOperatorConfigControllerName = "dpfoperatorconfig-controller"
)

var (
	applyPatchOptions = []client.PatchOption{client.ForceOwnership, client.FieldOwner(dpfOperatorConfigControllerName)}
)

// DPFOperatorConfigReconciler reconciles a DPFOperatorConfig object
type DPFOperatorConfigReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Settings  *DPFOperatorConfigReconcilerSettings
	Inventory *inventory.SystemComponents
	Defaults  *release.Defaults
}

// DPFOperatorConfigReconcilerSettings contains settings related to the DPFOperatorConfig.
type DPFOperatorConfigReconcilerSettings struct {
	// ConfigSingletonNamespaceName restricts reconciliation of the operator to a single DPFOperator Config with a specified namespace and name.
	ConfigSingletonNamespaceName *types.NamespacedName

	// SkipWebhook skips ValidatingWebhookConfiguration/MutatingWebhookConfiguration installing.
	// SkipWebhook is true iff we are running tests in an environment that doesn't have functioning webhook server.
	SkipWebhook bool
}

// Operator objects
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs/finalizers,verbs=update

// DPUService objects
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices;dpudeployments;dpuservicecredentialrequests;dpuserviceconfigurations;dpuservicetemplates,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices/status;dpudeployments/status;dpuservicecredentialrequests/status;dpuservicetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices/finalizers;dpudeployments/finalizers;dpuservicecredentialrequests/finalizers;dpuservicetemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains;serviceinterfaces;dpuserviceinterfaces;dpuservicechains;dpuserviceipams;dpuservicenads,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/status;servicechains/status;dpuservicechains/status;dpuserviceinterfaces/status;dpuserviceipams/status;dpuservicenads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/finalizers;servicechains/finalizers;dpuservicechains/finalizers;dpuserviceinterfaces/finalizers;dpuserviceipams/finalizers;dpuservicenads/finalizers,verbs=update

// Provisioning objects
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets;dpuflavors;bfbs;dpus;dpuclusters;dpudevices;dpunodes;dpudiscoveries;dpunodemaintenances,verbs=create;delete;get;list;watch;patch;update;deletecollection
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets/status;dpus/status;bfbs/status;dpuclusters/status;dpunodes/status;dpudevices/status;dpudiscoveries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets/finalizers;bfbs/finalizers;dpus/finalizers;dpuflavors/finalizers;dpuclusters/finalizers;dpudevices/finalizers;dpunodes/finalizers;dpudiscoveries/finalizers,verbs=update

// VPC objects
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs;dpuvirtualnetworks;isolationclasses,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs/status;dpuvirtualnetworks/status;isolationclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs/finalizers;dpuvirtualnetworks/finalizers;isolationclasses/finalizers,verbs=update

// DPU Storage objects
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes;dpuvolumeattachments;dpustoragepolicies;dpustoragevendors,verbs=get;list;watch

// NodeSRIOVDevicePlugin objects
// +kubebuilder:rbac:groups=noderesources.dpu.nvidia.com,resources=nodesriovdevicepluginconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=noderesources.dpu.nvidia.com,resources=nodesriovdevicepluginconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=noderesources.dpu.nvidia.com,resources=nodesriovdevicepluginconfigs/finalizers,verbs=update

// Kubernetes Objects
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=create
// +kubebuilder:rbac:groups=core,resources=nodes;pods;pods/exec;jobs;serviceaccounts;serviceaccounts/token;persistentvolumeclaims;events,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups=core,resources=configmaps;secrets,verbs=get;list;create;patch;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services;endpoints,verbs=get;list;watch;create;patch;update;delete;deletecollection
// +kubebuilder:rbac:groups=core,resources=nodes;pods;pods/exec;services;serviceaccounts;serviceaccounts/token;configmaps;persistentvolumeclaims;events;,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests,verbs=get;list;create;patch;update;delete
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests/approval;certificatesigningrequests/status,verbs=update
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,resourceNames="kubernetes.io/kube-apiserver-client",verbs=approve

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations;mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch;create;update;patch;delete;deletecollection

// External API objects
// +kubebuilder:rbac:groups=argoproj.io,resources=appprojects;applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=maintenance.nvidia.com,resources=nodemaintenances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=maintenance.nvidia.com,resources=nodemaintenances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificaterequests;certificates;issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nv-ipam.nvidia.com,resources=ippools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *DPFOperatorConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1.DPFOperatorConfig{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.ResourceToDPFOperatorConfig)).
		Watches(&provisioningv1.DPU{}, handler.EnqueueRequestsFromMapFunc(r.ResourceToDPFOperatorConfig)).
		Watches(&dpuservicev1.DPUService{}, handler.EnqueueRequestsFromMapFunc(r.DPUServiceToDPFOperatorConfig)).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(r.DeploymentToDPFOperatorConfig)).
		Watches(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(r.DaemonSetToDPFOperatorConfig)).
		Complete(r)
}

// Reconcile reconciles changes in a DPFOperatorConfig.
func (r *DPFOperatorConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")
	// TODO: Validate that the DPF Operator Config only exists with the correct name and namespace at creation time.
	if r.Settings.ConfigSingletonNamespaceName != nil {
		if req.Namespace != r.Settings.ConfigSingletonNamespaceName.Name && req.Name != r.Settings.ConfigSingletonNamespaceName.Name {
			return ctrl.Result{}, fmt.Errorf("invalid config: only one object with name %s in namespace %s is allowed",
				r.Settings.ConfigSingletonNamespaceName.Name, r.Settings.ConfigSingletonNamespaceName.Namespace)
		}
	}

	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpfOperatorConfig); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// If the DPFOperatorConfig is paused take no further action.
	if isPaused(dpfOperatorConfig) {
		log.Info("Reconciling Paused")
		return ctrl.Result{}, nil
	}

	dpuClusters, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpfOperatorConfig, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.updateSystemComponentStatus(ctx, dpfOperatorConfig, dpuClusters)

		// Set the summary condition for the DPFOperatorConfig.
		conditions.SetSummary(dpfOperatorConfig)

		log.Info("Patching")
		if err := patcher.Patch(ctx, dpfOperatorConfig,
			patch.WithFieldOwner(dpfOperatorConfigControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(operatorv1.Conditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// If this is a new config we have to set the version to the current DPF version.
	if dpfOperatorConfig.IsNewConfig() {
		dpfOperatorConfig.Status.Version = ptr.To(release.DPFVersion())
	}

	conditions.EnsureConditions(dpfOperatorConfig, operatorv1.Conditions)

	// Handle deletion reconciliation loop.
	if !dpfOperatorConfig.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpfOperatorConfig, dpuClusters)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpfOperatorConfig, operatorv1.DPFOperatorConfigFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpfOperatorConfig, operatorv1.DPFOperatorConfigFinalizer)
		return ctrl.Result{}, nil
	}
	return r.reconcile(ctx, dpfOperatorConfig, dpuClusters)
}

func isPaused(config *operatorv1.DPFOperatorConfig) bool {
	if config.Spec.Overrides == nil {
		return false
	}
	if config.Spec.Overrides.Paused == nil {
		return false
	}

	return *config.Spec.Overrides.Paused
}

//nolint:unparam
func (r *DPFOperatorConfigReconciler) reconcile(ctx context.Context, dpfOperatorConfig *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) (ctrl.Result, error) {
	if err := r.reconcilePreUpgradeValidations(ctx, dpfOperatorConfig, dpuClusters); err != nil {
		// We don't have to join the errors here as we are already formatting the error message.
		message := fmt.Sprintf("Validation must pass for DPF upgrade to continue:\n%v",
			conditions.JoinErrors(err, 1))
		conditions.AddFalse(
			dpfOperatorConfig,
			operatorv1.PreUpgradeValidationReadyCondition,
			conditions.ReasonError,
			conditions.ConditionMessage(message))
		return ctrl.Result{}, err
	}
	conditions.AddTrue(dpfOperatorConfig, operatorv1.PreUpgradeValidationReadyCondition)

	if err := r.reconcileImagePullSecrets(ctx, dpfOperatorConfig); err != nil {
		message := fmt.Sprintf("ImagePullSecrets must be reconciled for DPF Operator to continue:\n%v",
			conditions.JoinErrors(err, 1))
		conditions.AddFalse(
			dpfOperatorConfig,
			operatorv1.ImagePullSecretsReconciledCondition,
			conditions.ReasonError,
			conditions.ConditionMessage(message))
		return ctrl.Result{}, err
	}
	conditions.AddTrue(dpfOperatorConfig, operatorv1.ImagePullSecretsReconciledCondition)

	if err := r.reconcileSystemComponents(ctx, dpfOperatorConfig, dpuClusters); err != nil {
		message := fmt.Sprintf("System components must be reconciled for DPF Operator to continue:\n%v",
			conditions.JoinErrors(err, 1))
		conditions.AddFalse(
			dpfOperatorConfig,
			operatorv1.SystemComponentsReconciledCondition,
			conditions.ReasonError,
			conditions.ConditionMessage(message))
		return ctrl.Result{}, err
	}
	conditions.AddTrue(dpfOperatorConfig, operatorv1.SystemComponentsReconciledCondition)

	// Update the DPF version in the status of the DPFOperatorConfig after a successful reconciliation.
	dpfOperatorConfig.Status.Version = ptr.To(release.DPFVersion())

	return ctrl.Result{}, nil
}

func (r *DPFOperatorConfigReconciler) reconcilePreUpgradeValidations(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error {
	// Return early if no version change is needed.
	if !config.UpgradeInProgress() || config.IsNewConfig() {
		return nil
	}

	// Validate the DPF version in the DPFOperatorConfig against the build version of the operator.
	if err := utils.ValidateDPFVersion(config.Status.Version); err != nil {
		return fmt.Errorf("DPF version validation:\n%v", err)
	}

	return r.validateAll(ctx, config, dpuClusters)
}

type validator struct {
	name string
	fn   func(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error
}

func (r *DPFOperatorConfigReconciler) validators() []validator {
	return []validator{
		{name: "System Components", fn: r.validateSystemComponentsReadiness},
		{name: "DPU State", fn: r.validateDPUState},
	}
}

func (r *DPFOperatorConfigReconciler) validateAll(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error {
	log := ctrllog.FromContext(ctx)
	var errs []error
	for _, v := range r.validators() {
		if err := v.fn(ctx, config, dpuClusters); err != nil {
			log.Error(err, "Validation failed", "validator", v.name)
			formattedErr := conditions.JoinErrors(err, 2)
			errs = append(errs, fmt.Errorf("%s:\n%s", v.name, formattedErr.Error()))
		}
	}
	return conditions.JoinErrors(kerrors.NewAggregate(errs), 1)
}

// validateDPUState checks the state of all DPUs in the cluster as well as their DPF versions.
func (r *DPFOperatorConfigReconciler) validateDPUState(ctx context.Context, config *operatorv1.DPFOperatorConfig, _ []*dpucluster.Config) error {
	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList, client.InNamespace(config.Namespace)); err != nil {
		return fmt.Errorf("failed to list DPUs in namespace %s: %w", config.Namespace, err)
	}

	var errs []error
	for _, dpu := range dpuList.Items {
		if err := utils.ValidateDPFVersion(dpu.Status.DPFVersion); err != nil {
			errs = append(errs, fmt.Errorf("DPU %s: %w", dpu.Name, err))
		}

		// TODO: Replace with conditions.IsTrue() when DPU implements ObservedGeneration in the conditions.
		_, readyCondition := util.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondReady.String())
		if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
			errs = append(errs, fmt.Errorf("DPU %s is not ready", dpu.Name))
		}
	}

	return kerrors.NewAggregate(errs)
}

func (r *DPFOperatorConfigReconciler) reconcileImagePullSecrets(ctx context.Context, config *operatorv1.DPFOperatorConfig) error {
	imagePullSecrets := map[string]bool{}
	for _, imagePullSecret := range config.Spec.ImagePullSecrets {
		imagePullSecrets[imagePullSecret] = true
	}
	// label imagePullSecrets listed in the DPFOperatorConfig
	for name := range imagePullSecrets {
		secret := &corev1.Secret{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: name}, secret); err != nil {
			return err
		}
		labels := secret.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
		secret.SetManagedFields(nil)
		labels[dpuservicev1.DPFImagePullSecretLabelKey] = ""
		secret.SetLabels(labels)
		if err := r.Client.Patch(ctx, secret, client.Apply, applyPatchOptions...); err != nil {
			return err
		}
	}

	// remove labels from pull secrets which are no longer in the DPFOperatorConfig.
	secretList := &corev1.SecretList{}
	if err := r.Client.List(ctx, secretList,
		client.HasLabels([]string{dpuservicev1.DPFImagePullSecretLabelKey}),
		client.InNamespace(config.GetNamespace()),
	); err != nil {
		return err
	}
	for _, secret := range secretList.Items {
		if _, ok := imagePullSecrets[secret.Name]; !ok {
			labels := secret.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
			secret.SetManagedFields(nil)
			secret.SetLabels(labels)

			delete(secret.Labels, dpuservicev1.DPFImagePullSecretLabelKey)
			if err := r.Client.Patch(ctx, &secret, client.Apply, applyPatchOptions...); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconcileSystemComponents applies manifests for components which form the DPF system.
// It deploys the following:
// 1. DPUService controller
// 2. DPU provisioning controller (and bfb-registry if DPUs are to be installed via RedFish)
// 3. ServiceFunctionChainSet controller DPUService
// 4. SR-IOV device plugin DPUService
// 5. MultusConfiguration DPUService
// 6. FlannelConfiguration DPUService
// 7. NVIDIA Kubernetes IPAM
// 8. OVS CNI
// 9. SFC Controller
// 10. CNI Installer
func (r *DPFOperatorConfigReconciler) reconcileSystemComponents(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error {
	var errs []error
	vars := inventory.VariablesFromDPFOperatorConfig(r.Defaults, config, dpuClusters)
	// Create objects for system components.
	for _, component := range r.Inventory.AllComponents() {
		if err := r.generateAndPatchObjects(ctx, component, vars); err != nil {
			errs = append(errs, err)
		}
	}
	return kerrors.NewAggregate(errs)
}

func (r *DPFOperatorConfigReconciler) updateSystemComponentStatus(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) {
	var errs []error
	log := ctrllog.FromContext(ctx)
	log.Info("Updating status of DPF OperatorConfig")
	for _, component := range r.Inventory.EnabledComponents(inventory.VariablesFromDPFOperatorConfig(r.Defaults, config, dpuClusters)) {
		err := component.IsReady(ctx, r.Client, config.Namespace)
		if err != nil {
			log.Error(err, "Component not ready", "component", component)
			errs = append(errs, fmt.Errorf("%s: %v", component.Name(), err))
		}
	}

	if len(errs) > 0 {
		message := fmt.Sprintf("System components must be ready for DPFOperatorConfig to become Ready:\n%v",
			conditions.JoinErrors(kerrors.NewAggregate(errs), 1))
		conditions.AddFalse(
			config,
			operatorv1.SystemComponentsReadyCondition,
			conditions.ReasonError,
			conditions.ConditionMessage(message),
		)
		return
	}

	conditions.AddTrue(config, operatorv1.SystemComponentsReadyCondition)
}

func (r *DPFOperatorConfigReconciler) validateSystemComponentsReadiness(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error {
	var errs []error
	log := ctrllog.FromContext(ctx)
	log.Info("Checking ready state of system components")
	for _, component := range r.Inventory.EnabledComponents(inventory.VariablesFromDPFOperatorConfig(r.Defaults, config, dpuClusters)) {
		err := component.IsReadyForUpgrade(ctx, r.Client, config)
		if err != nil {
			log.Error(err, "Component not ready", "component", component)
			errs = append(errs, fmt.Errorf("%s: %v", component.Name(), err))
		}
	}

	return kerrors.NewAggregate(errs)
}

func (r *DPFOperatorConfigReconciler) generateAndPatchObjects(ctx context.Context, component inventory.Component, vars inventory.Variables) error {
	objs, err := component.GenerateManifests(vars)
	if err != nil {
		return fmt.Errorf("error while generating manifests for object, err: %v", err)
	}

	var desiredApplySet client.Object
	for _, obj := range objs {
		// If the ApplySetParentIDLabel is present and has the correct value this is the ApplySet parent for the component.
		// Hold back on updating this object until all operations are complete.
		if id, ok := obj.GetLabels()[inventory.ApplySetParentIDLabel]; ok && (id == inventory.ApplySetID(vars.Namespace, component)) {
			desiredApplySet = obj
			continue
		}

		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if r.Settings.SkipWebhook && (kind == "ValidatingWebhookConfiguration" || kind == "MutatingWebhookConfiguration") {
			continue
		}

		if err = r.Client.Patch(ctx, obj, client.Apply, applyPatchOptions...); err != nil {
			return fmt.Errorf("error patching %v %v: %w", obj.GetObjectKind().GroupVersionKind().Kind, klog.KObj(obj), err)
		}
	}

	// Delete objects that were previously part of the component but have been removed.
	if err = r.deleteStaleObjects(ctx, vars.Namespace, component, objs); err != nil {
		return err
	}

	// Update the apply set with the current state of the component.
	if err = r.updateApplySet(ctx, vars.Namespace, desiredApplySet, component); err != nil {
		return err
	}

	return nil
}

// deleteStaleObjects compares the current ApplySet to the desired list of objects. It deletes all objects which are in the inventory but not in the desiredObjects.
func (r *DPFOperatorConfigReconciler) deleteStaleObjects(ctx context.Context, namespace string, component inventory.Component, desiredObjects []client.Object) error {
	currentInventory, err := r.currentInventoryForComponent(ctx, namespace, component)
	if err != nil {
		return err
	}

	desiredInventory := gknnSetForObjects(desiredObjects)
	for _, obj := range currentInventory {
		// If the object doesn't exist in the desired inventory delete it.
		if _, ok := desiredInventory[obj]; !ok {
			gknn, err := inventory.ParseGroupKindNamespaceName(obj)
			if err != nil {
				return err
			}
			if err := r.deleteByGKNN(ctx, gknn); err != nil {
				return err
			}
		}
	}
	return nil
}

// updateApplySet patches the ApplySet parent with an up to date inventory annotation.
// If desiredApplySet is nil it ensures the ApplySet parent object is deleted.
func (r *DPFOperatorConfigReconciler) updateApplySet(ctx context.Context, namespace string, desiredApplySet client.Object, component inventory.Component) error {
	log := ctrllog.FromContext(ctx)
	// Delete the ApplySet if it is not part of the inventory.
	if desiredApplySet == nil {
		currentApplySet, err := r.getApplySetForComponent(ctx, namespace, component)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
		log.Info("Deleting empty ApplySet parent", "object", klog.KObj(currentApplySet))
		return client.IgnoreNotFound(r.Client.Delete(ctx, currentApplySet))
	}

	// Update the applySet to reflect the new inventory.
	if err := r.Client.Patch(ctx, desiredApplySet, client.Apply, applyPatchOptions...); err != nil {
		return err
	}
	return nil
}

// currentInventoryForComponent returns a list of all GKNNs which are currently in the ApplySet inventory for the component.
// If the ApplySet doesn't exist it returns an empty list.
func (r *DPFOperatorConfigReconciler) currentInventoryForComponent(ctx context.Context, namespace string, component inventory.Component) ([]string, error) {
	applySet, err := r.getApplySetForComponent(ctx, namespace, component)
	if err != nil {
		// If the applySet is not found return an empty list.
		// This means that the ApplySet is not tracked by the operator because it was never created or has been deleted.
		if apierrors.IsNotFound(err) {
			return []string{}, nil
		}
		return nil, err
	}
	gknnList, ok := applySet.GetAnnotations()[inventory.ApplySetInventoryAnnotationKey]
	if !ok {
		return nil, fmt.Errorf("no inventory annotation found on applyset for component %s in namespace %s", component.Name(), namespace)
	}
	return strings.Split(gknnList, ","), nil
}

// getApplySetForComponent returns the ApplySet parent for the passed component.
func (r *DPFOperatorConfigReconciler) getApplySetForComponent(ctx context.Context, namespace string, component inventory.Component) (client.Object, error) {
	labels := map[string]string{
		inventory.ApplySetParentIDLabel: inventory.ApplySetID(namespace, component),
	}
	secrets := &corev1.SecretList{}
	if err := r.List(ctx, secrets, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	// If there is no secret with the matching label return a NotFound error.
	if len(secrets.Items) == 0 {
		return nil, apierrors.NewNotFound(schema.GroupResource{
			Group:    "",
			Resource: "Secret",
		}, inventory.ApplySetName(component))
	}

	if len(secrets.Items) > 1 {
		return nil, fmt.Errorf("found multiple ApplySets for component %s in namespace %s: only one allowed", component.Name(), namespace)
	}

	// Check that the applySet parent contains the correct tooling annotation.
	if tool, ok := secrets.Items[0].GetAnnotations()[inventory.ApplySetToolingAnnotation]; !ok || tool != inventory.ApplySetToolingAnnotationValue {
		return nil, fmt.Errorf("applySet must contain the tooling annotation \"%s: %s\". Skipping component %s in namespace %s",
			inventory.ApplySetToolingAnnotation, inventory.ApplySetToolingAnnotationValue, component.Name(), namespace)
	}
	return &secrets.Items[0], nil
}

// gknnSetForObjects returns a set containing each object in the inventory.
func gknnSetForObjects(objs []client.Object) map[string]bool {
	out := map[string]bool{}
	for _, obj := range objs {
		s := inventory.GroupKindNamespaceName{
			Group:     obj.GetObjectKind().GroupVersionKind().Group,
			Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}.String()
		out[s] = true
	}
	return out
}

// deleteByGKNN deletes the object represented by its GKNN. It ignores NotFound errors.
func (r *DPFOperatorConfigReconciler) deleteByGKNN(ctx context.Context, gknn inventory.GroupKindNamespaceName) error {
	log := ctrllog.FromContext(ctx)
	mapping, err := r.Client.RESTMapper().RESTMapping(schema.GroupKind{Group: gknn.Group, Kind: gknn.Kind})
	if err != nil {
		return fmt.Errorf("could not find kind for resource in annotation: %w", err)
	}
	uns := &unstructured.Unstructured{}
	uns.SetGroupVersionKind(mapping.GroupVersionKind)
	uns.SetNamespace(gknn.Namespace)
	uns.SetName(gknn.Name)

	log.Info("Deleting object", "object", klog.KObj(uns))
	if err = r.Client.Delete(ctx, uns); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

// ResourceToDPFOperatorConfig enqueues a reconcile when an event occurs for system DPUServices.
func (r *DPFOperatorConfigReconciler) ResourceToDPFOperatorConfig(_ context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	// Ignore this enqueue function if the singletonNamespaceName is not set. This is done to enable easier testing.
	if r.Settings.ConfigSingletonNamespaceName == nil {
		return result
	}
	result = append(result, ctrl.Request{NamespacedName: *r.Settings.ConfigSingletonNamespaceName})
	return result
}

// DPUServiceToDPFOperatorConfig enqueues a reconcile when an event occurs for system DPUServices.
func (r *DPFOperatorConfigReconciler) DPUServiceToDPFOperatorConfig(_ context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuService, ok := o.(*dpuservicev1.DPUService)
	if !ok {
		return result
	}
	// Ignore this enqueue function if the singletonNamespaceName is not set. This is done to enable easier testing.
	if r.Settings.ConfigSingletonNamespaceName == nil {
		return result
	}
	if _, ok = dpuService.GetLabels()[operatorv1.DPFComponentLabelKey]; ok {
		result = append(result, ctrl.Request{NamespacedName: *r.Settings.ConfigSingletonNamespaceName})
	}
	return result
}

// DaemonSetToDPFOperatorConfig enqueues a reconcile when an event occurs for system DaemonSets.
func (r *DPFOperatorConfigReconciler) DaemonSetToDPFOperatorConfig(_ context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	daemonSet, ok := o.(*appsv1.DaemonSet)
	if !ok {
		return result
	}
	// Ignore this enqueue function if the singletonNamespaceName is not set. This is done to enable easier testing.
	if r.Settings.ConfigSingletonNamespaceName == nil {
		return result
	}
	if _, ok = daemonSet.GetLabels()[operatorv1.DPFComponentLabelKey]; ok {
		result = append(result, ctrl.Request{NamespacedName: *r.Settings.ConfigSingletonNamespaceName})
	}
	return result
}

// DeploymentToDPFOperatorConfig enqueues a reconcile when an event occurs for system Deployments.
func (r *DPFOperatorConfigReconciler) DeploymentToDPFOperatorConfig(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	deployment, ok := o.(*appsv1.Deployment)
	if !ok {
		return result
	}
	// Ignore this enqueue function if the singletonNamespaceName is not set. This is done to enable easier testing.
	if r.Settings.ConfigSingletonNamespaceName == nil {
		return result
	}
	if _, ok = deployment.GetLabels()[operatorv1.DPFComponentLabelKey]; ok {
		result = append(result, ctrl.Request{NamespacedName: *r.Settings.ConfigSingletonNamespaceName})
	}
	return result
}
