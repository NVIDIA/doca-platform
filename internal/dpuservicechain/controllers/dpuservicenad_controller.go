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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	dpuServiceNADFinalizer      = "svc.dpu.nvidia.com/dpuservicenad"
	dpuServiceNADControllerName = "dpuservicenadcontroller"
	// ParentDPUServiceNADNameLabel points to the name of the DPUServiceNAD object that owns a resource in the DPU
	// cluster.
	parentDPUServiceNADNameLabel = "dpu.nvidia.com/dpuservicenad-name"
	// ParentDPUServiceNADNamespaceLabel points to the namespace of the DPUServiceNAD object that owns a resource in
	// the DPU cluster.
	parentDPUServiceNADNamespaceLabel = "dpu.nvidia.com/dpuservicenad-namespace"
	nadGroup                          = "k8s.cni.cncf.io"
	nadVersion                        = "v1"
	nadKind                           = "NetworkAttachmentDefinition"
)

// DPUServiceNADReconciler reconciles a DPUServiceNAD object
type DPUServiceNADReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
}

var _ objectsInDPUClustersReconciler = &DPUServiceNADReconciler{}

func (r *DPUServiceNADReconciler) calculateDPUServiceObjectStateBasedOnStatus(_ []*dpucluster.Config, _ dpuservicev1.DPUServiceObject) (bool, error) {
	return false, nil
}

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicenads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicenads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservicenads/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DPUServiceNAD object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.4/pkg/reconcile
func (r *DPUServiceNADReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// take DPUServiceNAD from user and convert it to NAD for DPUClusters
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	DPUServiceNAD := &dpuservicev1.DPUServiceNAD{}
	if err := r.Client.Get(ctx, req.NamespacedName, DPUServiceNAD); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(DPUServiceNAD, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := updateSummary(ctx, r, r.Client, dpuservicev1.ConditionDPUNADObjectReady, DPUServiceNAD); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		if err := patcher.Patch(ctx, DPUServiceNAD,
			patch.WithFieldOwner(dpuServiceNADControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.DPUServiceNADConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(DPUServiceNAD, dpuservicev1.DPUServiceNADConditions)

	// Handle deletion reconciliation loop.
	if !DPUServiceNAD.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, DPUServiceNAD)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(DPUServiceNAD, dpuServiceNADFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(DPUServiceNAD, dpuServiceNADFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, DPUServiceNAD)
}

//nolint:unparam
func (r *DPUServiceNADReconciler) reconcile(ctx context.Context, DPUServiceNAD *dpuservicev1.DPUServiceNAD) (ctrl.Result, error) {
	if err := reconcileObjectsInDPUClusters(ctx, r, r.Client, DPUServiceNAD); err != nil {
		e := &longOperationError{}
		conditions.AddFalse(
			DPUServiceNAD,
			dpuservicev1.ConditionDPUNADObjectReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		// We enqueue without exponential backoff if the error is a longOperationError because this
		// error is returned when we know an operation that was triggered will take time to complete
		if errors.As(err, &e) {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	conditions.AddTrue(
		DPUServiceNAD,
		dpuservicev1.ConditionDPUNADObjectReconciled,
	)

	// start watching the ServiceInterfaceSet in the DPU clusters
	if err := watchObjectsInDPUClusters(ctx, r.Client, r); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles the delete reconciliation loop
//
//nolint:unparam
func (r *DPUServiceNADReconciler) reconcileDelete(ctx context.Context, DPUServiceNAD *dpuservicev1.DPUServiceNAD) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	if err := reconcileObjectDeletionInDPUClusters(ctx, r, r.Client, DPUServiceNAD); err != nil {
		e := &longOperationError{}
		if errors.As(err, &e) {
			log.Info(fmt.Sprintf("Requeueing because %s", err.Error()))
			conditions.AddFalse(
				DPUServiceNAD,
				dpuservicev1.ConditionDPUNADObjectReconciled,
				conditions.ReasonAwaitingDeletion,
				conditions.ConditionMessage(err.Error()),
			)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("error while reconciling deletion of objects in DPU clusters: %w", err)
	}

	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(DPUServiceNAD, dpuServiceNADFinalizer)
	return ctrl.Result{}, nil
}

func getDPUServiceNADAnnotations(dpuServiceNAD *dpuservicev1.DPUServiceNAD) map[string]string {
	resourceAnnotation := map[string]string{
		"k8s.v1.cni.cncf.io/resourceName": dpuServiceNAD.GetResourceName(),
	}
	nadAnnotations := map[string]string{}
	maps.Copy(nadAnnotations, dpuServiceNAD.ObjectMeta.Annotations)
	maps.Copy(nadAnnotations, resourceAnnotation)
	return nadAnnotations
}

func getDPUServiceNADLabels(DPUServiceNAD *dpuservicev1.DPUServiceNAD) map[string]string {
	commonLabels := map[string]string{
		parentDPUServiceNADNameLabel:      DPUServiceNAD.Name,
		parentDPUServiceNADNamespaceLabel: DPUServiceNAD.Namespace,
	}
	nadLabels := map[string]string{}
	maps.Copy(nadLabels, DPUServiceNAD.ObjectMeta.Labels)
	maps.Copy(nadLabels, commonLabels)
	return nadLabels
}

// registerKindToWatcher registers the NetworkAttachmentDefinition kind to the remote cache watcher
// so that the DPUServiceNAD controller can watch for changes in the NetworkAttachmentDefinition objects
// in the DPU clusters. This is used to trigger reconciliation of the DPUServiceNAD
// when a NetworkAttachmentDefinition is created, updated, or deleted in any of the DPU clusters.
func (r *DPUServiceNADReconciler) registerKindToWatcher(ctx context.Context, dpuCluster client.ObjectKey) error {
	nad := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": fmt.Sprintf("%s/%s", nadGroup, nadVersion),
			"kind":       nadKind,
		},
	}
	return r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuservicenad-watch-networkattachmentdefinition",
		Watcher:      r.controller,
		Kind:         nad,
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.networkAttachmentDefinitionToDPUServiceNad),
		Predicates:   []predicate.TypedPredicate[client.Object]{predicates.TypedResourceIsChanged[client.Object]()},
	}))
}

func (r *DPUServiceNADReconciler) networkAttachmentDefinitionToDPUServiceNad(ctx context.Context, o client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	match := o.GetObjectKind().GroupVersionKind().Kind == nadKind &&
		o.GetObjectKind().GroupVersionKind().Group == nadGroup &&
		o.GetObjectKind().GroupVersionKind().Version == nadVersion
	if !match {
		log.Error(fmt.Errorf("expected a NetworkAttachmentDefinition, got %T", o), "failed to convert object")
		return nil
	}
	// Check if the object has the parentDPUServiceNADNameLabel label
	if _, ok := o.GetLabels()[parentDPUServiceNADNameLabel]; ok {
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()}}}
	}
	return nil
}

// getObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
// objects in the DPU cluster related to the given parentObject. The implementation should get the created objects
// in the DPU cluster.
func (r *DPUServiceNADReconciler) getObjectsInDPUCluster(ctx context.Context, c client.Client, dpuObject dpuservicev1.DPUServiceObject) ([]unstructured.Unstructured, error) {
	nad := &unstructured.Unstructured{}
	// Set GroupVersionKind
	nad.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   nadGroup,
		Version: nadVersion,
		Kind:    nadKind,
	})

	key := client.ObjectKey{Namespace: dpuObject.GetNamespace(), Name: dpuObject.GetName()}
	err := c.Get(ctx, key, nad)
	if err != nil {
		return nil, fmt.Errorf("error while getting %s %s: %w", nad.GetObjectKind().GroupVersionKind().String(), key.String(), err)
	}
	return []unstructured.Unstructured{*nad}, nil
}

// createOrUpdateObjectsInDPUCluster is the method called by the reconcileObjectsInDPUClusters function which applies
// changes to the DPU clusters on DPUServiceNAD object updates.
func (r *DPUServiceNADReconciler) createOrUpdateObjectsInDPUCluster(ctx context.Context, c client.Client, _ types.NamespacedName, dpuObject dpuservicev1.DPUServiceObject) error {
	DPUServiceNAD, ok := dpuObject.(*dpuservicev1.DPUServiceNAD)
	if !ok {
		return errors.New("error converting input object to DPUServiceNAD")
	}

	config := make(map[string]interface{})
	config["cniVersion"] = "0.3.1"
	config["name"] = DPUServiceNAD.Name

	// Create OVS plugin configuration
	ovsPlugin := map[string]interface{}{
		"type":           "ovs",
		"mtu":            DPUServiceNAD.Spec.ServiceMTU,
		"bridge":         DPUServiceNAD.Spec.Bridge,
		"interface_type": "dpdk",
	}
	if DPUServiceNAD.Spec.IPAM {
		ipamConfig := map[string]string{
			"type": "nv-ipam",
		}
		ovsPlugin["ipam"] = ipamConfig
	}

	// Check if we need chained CNI format (backward compatible)
	if len(DPUServiceNAD.Spec.ChainedCNIs) == 0 {
		// Use old single-plugin format for backward compatibility
		for k, v := range ovsPlugin {
			config[k] = v
		}
	} else {
		// Use new chained CNI format when additional plugins are specified
		plugins := []map[string]interface{}{
			ovsPlugin,
		}

		// Add additional plugins from Chain
		for _, plugin := range DPUServiceNAD.Spec.ChainedCNIs {
			pluginConfig := map[string]interface{}{
				"type": *plugin.Type,
			}
			if plugin.Config != nil {
				var pluginConfigData map[string]interface{}
				if err := json.Unmarshal(plugin.Config.Raw, &pluginConfigData); err != nil {
					return fmt.Errorf("error while unmarshalling plugin config: %w", err)
				}
				// Merge config into pluginConfig
				for k, v := range pluginConfigData {
					pluginConfig[k] = v
				}
			}
			plugins = append(plugins, pluginConfig)
		}

		config["plugins"] = plugins
	}
	jsonConfig, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// Create the unstructured NetworkAttachmentDefinition object
	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", nadGroup, nadVersion),
			"kind":       nadKind,
			"metadata": map[string]interface{}{
				"name":        DPUServiceNAD.Name,
				"namespace":   DPUServiceNAD.Namespace,
				"labels":      getDPUServiceNADLabels(DPUServiceNAD),
				"annotations": getDPUServiceNADAnnotations(DPUServiceNAD),
			},
			"spec": map[string]interface{}{
				"config": string(jsonConfig),
			},
		},
	}

	// Set GroupVersionKind
	nad.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   nadGroup,
		Version: nadVersion,
		Kind:    nadKind,
	})

	if err := c.Patch(ctx, nad, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceNADControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", nad.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(nad), err)
	}
	return nil
}

// deleteObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
// objects in the DPU cluster related to the deleted DPUServiceNAD object.
func (r *DPUServiceNADReconciler) deleteObjectsInDPUCluster(ctx context.Context, c client.Client, dpuObject dpuservicev1.DPUServiceObject) error {
	dpuServiceNAD, ok := dpuObject.(*dpuservicev1.DPUServiceNAD)
	if !ok {
		return errors.New("error converting input object to DPUServiceNAD")
	}
	p := &unstructured.Unstructured{}
	// Set GroupVersionKind
	p.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   nadGroup,
		Version: nadVersion,
		Kind:    nadKind,
	})

	if err := c.DeleteAllOf(ctx, p, client.InNamespace(dpuServiceNAD.Namespace), client.MatchingLabels{
		parentDPUServiceNADNameLabel:      dpuServiceNAD.Name,
		parentDPUServiceNADNamespaceLabel: dpuServiceNAD.Namespace,
	}); err != nil {
		return fmt.Errorf("error while removing all %s: %w", p.GetObjectKind().GroupVersionKind().String(), err)
	}
	return nil
}

func (r *DPUServiceNADReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	return unreadyObjs, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUServiceNADReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUServiceNAD{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.dpuClusterToDPUServiceNAD)).
		Named(dpuServiceNADControllerName).
		Build(r)

	if err != nil {
		return err
	}

	r.controller = c
	return nil
}

// dpuClusterToDPUServiceNAD ensures all DPUServiceNADs are updated each time there is an update to a DPUCluster.
func (r *DPUServiceNADReconciler) dpuClusterToDPUServiceNAD(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuServiceNADList := &dpuservicev1.DPUServiceNADList{}
	if err := r.Client.List(ctx, dpuServiceNADList); err != nil {
		return nil
	}
	for _, m := range dpuServiceNADList.Items {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}
