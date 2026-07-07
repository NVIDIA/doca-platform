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
	"sort"
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

	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/patch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	// maxItemsToReportOnValidationMessage is the maximum number of objects reported
	// for validation errors to prevent condition messages from growing unbounded.
	maxItemsToReportOnValidationMessage int = 5
)

const (
	dpfOperatorConfigControllerName = "dpfoperatorconfig-controller"
)

var (
	applyPatchOptions = []client.PatchOption{client.ForceOwnership, client.FieldOwner(dpfOperatorConfigControllerName)}

	// lastDPFReleaseWithoutKubeletSupport is the last DPF release without kubeletVersion support.
	// The kubeletVersion will be added by dpuagent starting with DPF v26.4.
	lastDPFReleaseWithoutKubeletSupport = semver.MustParse("v25.10.1")
)

// DPFOperatorConfigReconciler reconciles a DPFOperatorConfig object
type DPFOperatorConfigReconciler struct {
	client.Client
	UncachedClient client.Reader
	Scheme         *runtime.Scheme
	Settings       *DPFOperatorConfigReconcilerSettings
	Inventory      *inventory.SystemComponents
	Defaults       *release.Defaults
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
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets;dpuflavors;dpuflavortemplates;bfbs;bluefieldsoftwares;dpus;dpuclusters;dpudevices;dpunodes;dpudiscoveries;dpunodemaintenances,verbs=create;delete;get;list;watch;patch;update;deletecollection
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets/status;dpus/status;bfbs/status;bluefieldsoftwares/status;dpuclusters/status;dpunodes/status;dpudevices/status;dpudiscoveries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets/finalizers;bfbs/finalizers;bluefieldsoftwares/finalizers;dpus/finalizers;dpuflavors/finalizers;dpuflavortemplates/finalizers;dpuclusters/finalizers;dpudevices/finalizers;dpunodes/finalizers;dpudiscoveries/finalizers,verbs=update

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
		return fmt.Errorf("DPF version validation: %v", err)
	}

	return r.validateAll(ctx, config, dpuClusters)
}

type validator struct {
	name string
	fn   func(ctx context.Context, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error
}

func (r *DPFOperatorConfigReconciler) validators() []validator {
	return []validator{
		{name: "Kubernetes Version Skew", fn: r.validateKubernetesVersionSkew},
		{name: "System Components", fn: r.validateSystemComponentsReadiness},
		{name: "DPU State", fn: r.validateDPUState},
		{name: "Object Schema Validation", fn: r.validateObjectSchemas},
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

	dpus := []string{}
	for _, dpu := range dpuList.Items {
		// Skip if the DPU has reached a terminal state (Ready or Error).
		if dpu.Status.Phase == provisioningv1.DPUError || dpu.Status.Phase == provisioningv1.DPUReady {
			continue
		}
		dpus = append(dpus, dpu.Name)
	}
	sort.Strings(dpus)

	errs := []error{}
	for i, dpu := range dpus {
		if i >= maxItemsToReportOnValidationMessage {
			errs = append(errs, fmt.Errorf("... and %d more", len(dpus)-i))
			break
		}
		errs = append(errs, fmt.Errorf("DPU %s is not ready", dpu))
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

// validateKubernetesVersionSkew enforces the Kubernetes version skew policy
// (https://kubernetes.io/releases/version-skew-policy/) across all DPU clusters
// as a pre-upgrade validation.
//
// For each cluster, it determines the kube-apiserver version to validate against:
//   - Kamaji clusters: the KubernetesVersion constant (the version being upgraded to)
//   - Static clusters: the actual version from DPUCluster.Status.Version
//
// It then checks every ready DPU assigned to that cluster:
//   - Kubelet major version must match the kube-apiserver major version
//   - Kubelet minor version must not be newer than the kube-apiserver
//   - Kubelet minor version must be at most 3 versions behind the kube-apiserver
//
// DPUs provisioned by operator versions that predate KubeletVersion reporting
// (DPFVersion == lastDPFReleaseWithoutKubeletSupport major.minor) are skipped.
// DPUs with a recent DPFVersion that fail to report KubeletVersion produce a validation error.
//
// All errors across clusters and DPUs are aggregated and returned together.
func (r *DPFOperatorConfigReconciler) validateKubernetesVersionSkew(ctx context.Context, _ *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) error {
	var errs []error
	log := ctrllog.FromContext(ctx)
	log.Info("Validating Kubernetes version skew policy")

	// Parse the target Kubernetes version for kamaji clusters (we control the upgrade).
	kamajiAPIServerVersion, err := semver.NewVersion(util.KubernetesVersion)
	if err != nil {
		return fmt.Errorf("invalid KubernetesVersion constant %s: %v", util.KubernetesVersion, err)
	}

	for _, dpuCluster := range dpuClusters {
		// Determine the kube-apiserver version to validate against.
		// For kamaji clusters: the KubernetesVersion constant (the version we're upgrading to).
		// For other clusters (e.g. static): the actual kube-apiserver version from Status.Version.
		var apiserverVersion *semver.Version
		if dpuCluster.Cluster.Spec.Type == string(provisioningv1.KamajiCluster) {
			apiserverVersion = kamajiAPIServerVersion
		} else {
			if dpuCluster.Cluster.Status.Version == "" {
				errs = append(errs, fmt.Errorf("cluster %s has no version", dpuCluster.Cluster.Name))
				continue
			}
			apiserverVersion, err = semver.NewVersion(dpuCluster.Cluster.Status.Version)
			if err != nil {
				errs = append(errs, fmt.Errorf("cluster %s/%s has invalid kube-apiserver version %s: %v",
					dpuCluster.Cluster.Namespace, dpuCluster.Cluster.Name, dpuCluster.Cluster.Status.Version, err))
				continue
			}
		}

		// Query all DPUs assigned to this cluster
		dpus, err := r.getDPUsForCluster(ctx, dpuCluster.Cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to query DPUs for cluster %s/%s: %v",
				dpuCluster.Cluster.Namespace, dpuCluster.Cluster.Name, err))
			continue
		}

		// Validate each DPU's kubelet version
		for _, dpu := range dpus {
			// Skip DPUs that are in Error state, we only care about working DPUs with the kubelet version set.
			if dpu.Status.Phase == provisioningv1.DPUError {
				continue
			}
			if err := r.validateDPUKubeletVersion(&dpu, apiserverVersion); err != nil {
				errs = append(errs, fmt.Errorf("cluster %s/%s: %v",
					dpuCluster.Cluster.Namespace, dpuCluster.Cluster.Name, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("kubernetes version skew violated: %w", kerrors.NewAggregate(errs))
	}
	return nil
}

func (r *DPFOperatorConfigReconciler) getDPUsForCluster(ctx context.Context, cluster *provisioningv1.DPUCluster) ([]provisioningv1.DPU, error) {
	dpuList := &provisioningv1.DPUList{}

	// List all DPUs in the same namespace as the cluster
	if err := r.Client.List(ctx, dpuList, client.InNamespace(cluster.Namespace)); err != nil {
		return nil, err
	}

	// Filter DPUs assigned to this cluster
	var assignedDPUs []provisioningv1.DPU
	for _, dpu := range dpuList.Items {
		if dpu.Spec.Cluster.Name == cluster.Name &&
			dpu.Spec.Cluster.Namespace == cluster.Namespace {
			assignedDPUs = append(assignedDPUs, dpu)
		}
	}

	return assignedDPUs, nil
}

func (r *DPFOperatorConfigReconciler) validateDPUKubeletVersion(dpu *provisioningv1.DPU, apiserverVersion *semver.Version) error {
	kubeletVersionStr, err := getDPUKubeletVersion(dpu)
	if err != nil {
		return fmt.Errorf("DPU %s/%s: %w", dpu.Namespace, dpu.Name, err)
	}
	if kubeletVersionStr == "" {
		// Legacy DPU, which predates KubeletVersion reporting.
		return nil
	}

	// Parse kubelet version
	kubeletVersion, err := semver.NewVersion(kubeletVersionStr)
	if err != nil {
		return fmt.Errorf("DPU %s/%s has invalid kubelet version %s: %v",
			dpu.Namespace, dpu.Name, kubeletVersionStr, err)
	}

	// Check major version match
	if kubeletVersion.Major() != apiserverVersion.Major() {
		return fmt.Errorf("DPU %s/%s: kubelet major version (%d) must match kube-apiserver major version (%d)",
			dpu.Namespace, dpu.Name, kubeletVersion.Major(), apiserverVersion.Major())
	}

	// Check kubelet not newer than apiserver
	if kubeletVersion.Minor() > apiserverVersion.Minor() {
		return fmt.Errorf("DPU %s/%s: kubelet version (%s) cannot be newer than kube-apiserver version (%s)",
			dpu.Namespace, dpu.Name, kubeletVersionStr, apiserverVersion.Original())
	}

	// Check kubelet not more than 3 minor versions older
	// Per Kubernetes version skew policy: kubelet can be at most 3 minor versions behind kube-apiserver
	// https://kubernetes.io/releases/version-skew-policy/
	if apiserverVersion.Minor()-kubeletVersion.Minor() > 3 {
		return fmt.Errorf("DPU %s/%s: kubelet version (%s) is more than 3 minor versions behind kube-apiserver version (%s)",
			dpu.Namespace, dpu.Name, kubeletVersionStr, apiserverVersion.Original())
	}

	return nil
}

// getDPUKubeletVersion returns the kubelet version string for a DPU.
// Returns ("", nil) for legacy DPUs that predate KubeletVersion reporting.
// Returns ("", error) for DPUs that should have reported KubeletVersion but didn't.
func getDPUKubeletVersion(dpu *provisioningv1.DPU) (string, error) {
	// If KubeletVersion is present, return it directly.
	if dpu.Status.AgentStatus != nil && dpu.Status.AgentStatus.KubeletVersion != nil {
		return *dpu.Status.AgentStatus.KubeletVersion, nil
	}

	if dpu.Status.DPFVersion == nil {
		return "", fmt.Errorf("no KubeletVersion")
	}
	dpfVer, err := semver.NewVersion(*dpu.Status.DPFVersion)
	if err != nil {
		return "", fmt.Errorf("failed to parse DPF version %s: %v", *dpu.Status.DPFVersion, err)
	}
	// DPF v25.10.x DPUs do not report KubeletVersion.
	// BFB LTS (3.2) DPUs need reprovisioning with DPF v26.4+ to report it.
	// Skip validation for these DPUs but error for anything older.
	if dpfVer.Major() == lastDPFReleaseWithoutKubeletSupport.Major() && dpfVer.Minor() == lastDPFReleaseWithoutKubeletSupport.Minor() {
		return "", nil
	}
	if !dpfVer.GreaterThan(lastDPFReleaseWithoutKubeletSupport) {
		return "", fmt.Errorf("unsupported DPF version %s (minimum supported: v25.10.x)", *dpu.Status.DPFVersion)
	}

	// DPU has a recent DPFVersion but no KubeletVersion. This is unexpected.
	return "", fmt.Errorf("kubelet version not reported (DPFVersion %s should support it)", *dpu.Status.DPFVersion)
}

// generateAndPatchObjects generates a component's manifests, creates or patches the objects, deletes stale objects
// and handles the ApplySet for the component in a re-entrant way.
func (r *DPFOperatorConfigReconciler) generateAndPatchObjects(ctx context.Context, component inventory.Component, vars inventory.Variables) error {
	desiredObjects, err := component.GenerateManifests(ctx, vars)
	if err != nil {
		return fmt.Errorf("error while generating manifests for object, err: %v", err)
	}

	// Compute desired inventory directly from the filtered objects.
	var desiredInventory []string
	if len(desiredObjects) > 0 {
		desiredInventory = strings.Split(inventory.InventoryStringFromObjects(desiredObjects...), ",")
	}

	currentInventory, err := r.currentInventoryForComponent(ctx, vars.Namespace, component)
	if err != nil {
		return err
	}

	// 1. Update ApplySet to contain existing + desired.
	// Note: if the desiredInventory is nil, then no update is required. The ApplySet will be deleted in step 4.
	if desiredInventory != nil {
		if err = r.updateApplySet(ctx, vars.Namespace, component, joinInventory(currentInventory, desiredInventory)); err != nil {
			return err
		}
	}

	// 2. Create and update desired objects.
	for _, obj := range desiredObjects {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if r.Settings.SkipWebhook && (kind == "ValidatingWebhookConfiguration" || kind == "MutatingWebhookConfiguration") {
			continue
		}
		if err = r.Client.Patch(ctx, obj, client.Apply, applyPatchOptions...); err != nil {
			return fmt.Errorf("error patching %v %v: %w", kind, klog.KObj(obj), err)
		}
	}

	// 3. Delete objects that were previously part of the component but have been removed.
	if err = r.deleteStaleObjects(ctx, currentInventory, desiredInventory); err != nil {
		return err
	}

	// 4. Update ApplySet to contain only desired objects, or delete it if there are none.
	return r.updateApplySet(ctx, vars.Namespace, component, desiredInventory)
}

func joinInventory(currentInventory, desiredInventory []string) []string {
	joinedInventory := []string{}
	gotObjs := map[string]bool{}
	for _, obj := range append(currentInventory, desiredInventory...) {
		if _, got := gotObjs[obj]; !got {
			joinedInventory = append(joinedInventory, obj)
			gotObjs[obj] = true
		}
	}
	sort.Strings(joinedInventory)
	return joinedInventory
}

// deleteStaleObjects compares the current ApplySet to the desired list of objects. It deletes all objects which are in the inventory but not in the desiredObjects.
func (r *DPFOperatorConfigReconciler) deleteStaleObjects(ctx context.Context, currentInventory, desiredInventory []string) error {
	desiredMap := map[string]bool{}
	for _, obj := range desiredInventory {
		desiredMap[obj] = true
	}
	for _, obj := range currentInventory {
		// If the object doesn't exist in the desired inventory delete it.
		if _, ok := desiredMap[obj]; !ok {
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

// updateApplySet patches the ApplySet parent with an up-to-date inventory annotation.
// If inv is nil it ensures the existing ApplySet is deleted.
func (r *DPFOperatorConfigReconciler) updateApplySet(ctx context.Context, namespace string, component inventory.Component, inv []string) error {
	if inv == nil {
		currentApplySet, err := r.getApplySetForComponent(ctx, namespace, component)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
		log := ctrllog.FromContext(ctx)
		log.Info("Deleting empty ApplySet parent", "object", klog.KObj(currentApplySet))
		return client.IgnoreNotFound(r.Client.Delete(ctx, currentApplySet))
	}
	applySet := buildApplySetParent(
		component,
		inventory.ApplySetID(namespace, component),
		namespace,
		strings.Join(inv, ","),
	)
	// managedFields must be nil for server-side apply.
	applySet.SetManagedFields(nil)
	return r.Client.Patch(ctx, applySet, client.Apply, applyPatchOptions...)
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

	if applySet == nil {
		return []string{}, nil
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

// buildApplySetParent returns a Secret object which is the parent for a given component.
func buildApplySetParent(component inventory.Component, id string, namespace string, inv string) *unstructured.Unstructured {
	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})
	parent.SetName(inventory.ApplySetName(component))
	parent.SetNamespace(namespace)
	parent.SetLabels(map[string]string{
		inventory.ApplySetParentIDLabel: id,
		operatorv1.DPFComponentLabelKey: component.Name().String(),
	})
	parent.SetAnnotations(map[string]string{
		inventory.ApplySetInventoryAnnotationKey: inv,
		inventory.ApplySetToolingAnnotation:      inventory.ApplySetToolingAnnotationValue,
	})
	return parent
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
