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

package dpfctl

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/third_party/api/argocd/api/application"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const all = "all"

type objectScope struct {
	client client.Client
	tree   ObjectTree
	opts   ObjectTreeOptions
}

type funcs func(context.Context, *ObjectTree, objectScope, *operatorv1.DPFOperatorConfig, func(map[string]string) bool) (*ObjectTree, error)

var funcMap = map[string][]funcs{
	"all": {
		DiscoverDPUServices,
		DiscoverDPUDeployments,
		DiscoverDPUSets,
		DiscoverDPUClusters,
		DiscoverDPUVPCs,
		DiscoverStorage,
	},
	"dpuservices": {
		DiscoverDPUServices,
	},
	"dpudeployments": {
		DiscoverDPUDeployments,
	},
	"dpusets": {
		DiscoverDPUSets,
	},
	"dpuclusters": {
		DiscoverDPUClusters,
	},
	"dpuvpcs": {
		DiscoverDPUVPCs,
	},
	"storage": {
		DiscoverStorage,
	},
}

func Discover(ctx context.Context, c client.Client, opts ObjectTreeOptions, subCmd string) (*ObjectTree, error) {
	dpfOperatorConfig, err := getDPFOperatorConfig(ctx, c)
	if err != nil {
		return nil, err
	}

	tree := NewObjectTree(dpfOperatorConfig, opts)

	// We have to default to all to show all resources.
	// This is necessary for our unit tests as well as for the CI output.
	if opts.ShowResources == "" {
		opts.ShowResources = all
	}
	// We need to alwaysshow storage resources if the subCmd is "storage".
	if subCmd == "storage" {
		opts.ShowStorage = true
	}

	scope := objectScope{
		client: c,
		tree:   *tree,
		opts:   opts,
	}

	// Only use skipFunc when showing all resources
	var skipFunc func(map[string]string) bool
	if subCmd == "all" {
		skipFunc = func(labels map[string]string) bool {
			return labels[dpuservicev1.ParentDPUDeploymentNameLabel] != ""
		}
	}

	if fs, ok := funcMap[subCmd]; ok {
		for _, f := range fs {
			if tree, err = f(ctx, tree, scope, dpfOperatorConfig, skipFunc); err != nil {
				return nil, err
			}
		}
		return tree, nil
	}
	return nil, fmt.Errorf("unknown object type %q", subCmd)
}

// DiscoverDPUServices returns a tree of objects representing the DPF status.
func DiscoverDPUServices(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, skipFunc func(map[string]string) bool) (*ObjectTree, error) {
	// Add system component DPUServices first
	if err := addSystemComponentDPUServices(ctx, scope, dpfOperatorConfig); err != nil {
		return nil, err
	}

	// Add regular DPUServices
	if err := addDPUServices(ctx, scope, dpfOperatorConfig, nil, skipFunc); err != nil {
		return nil, err
	}

	if err := addDPUServiceCredentialRequests(ctx, scope, dpfOperatorConfig, nil, nil); err != nil {
		return nil, err
	}

	return tree, nil
}

// addSystemComponentDPUServices adds system component DPUServices to the tree.
func addSystemComponentDPUServices(ctx context.Context, o objectScope, root client.Object) error {
	if !showResource(o.opts, dpuservicev1.DPUServiceKind) {
		return nil
	}

	// Get system component DPUServices
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := o.client.List(ctx, dpuServiceList, client.HasLabels{
		operatorv1.DPFComponentLabelKey,
	}); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuService := range dpuServiceList.Items {
		if !isObjDebug(&dpuService, o.opts.ShowResources) {
			continue
		}

		dpuService.TypeMeta = metav1.TypeMeta{
			Kind:       dpuservicev1.DPUServiceKind,
			APIVersion: dpuservicev1.GroupVersion.String(),
		}
		addToTree = append(addToTree, &dpuService)

		// Return early if we should not expand DPUServices.
		if meta.IsStatusConditionTrue(dpuService.GetConditions(), string(conditions.TypeReady)) && !isObjDebug(&dpuService, o.opts.ExpandResources) {
			continue
		}
		if err := addArgoApplication(ctx, o, dpuService); err != nil {
			return fmt.Errorf("get application information: %w", err)
		}
	}

	if len(addToTree) == 0 {
		return nil
	}

	// Create a virtual object for system components
	systemComponents := VirtualObject(root.GetNamespace(), "SystemComponents", "System Components")
	addAnnotation(systemComponents, ObjectMetaNameAnnotation, "System Components")
	o.tree.Add(root, systemComponents)

	o.tree.AddMultipleWithHeader(systemComponents, addToTree, "DPUServices", GroupingObject(true))
	return nil
}

// getEnabledSystemComponents returns a map of system component names that can be DPUServices.
func getEnabledSystemComponents() map[string]bool {
	components := map[string]bool{}
	for _, component := range inventory.New().SystemDPUServices() {
		components[component.Name()] = true
	}
	// Add servicechainset-rbac-and-crds as separate component.
	// It is deployed via the servicechainset-controller helm chart.
	components[operatorv1.ServiceChainSetCRDsName] = true
	return components
}

// DiscoverDPUDeployments returns a tree of objects representing the DPF status.
func DiscoverDPUDeployments(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, _ func(map[string]string) bool) (*ObjectTree, error) {
	if err := addDPUDeployments(ctx, scope, dpfOperatorConfig); err != nil {
		return nil, err
	}

	return tree, nil
}

// DiscoverDPUSets returns a tree of objects representing the DPF status.
func DiscoverDPUSets(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, skipFunc func(map[string]string) bool) (*ObjectTree, error) {
	if err := addDPUSets(ctx, scope, dpfOperatorConfig, nil, skipFunc); err != nil {
		return nil, err
	}

	skipDPUSetFunc := func(labels map[string]string) bool {
		return labels[util.DPUSetNameLabel] != ""
	}

	if err := addDPUs(ctx, scope, dpfOperatorConfig, nil, skipDPUSetFunc); err != nil {
		return nil, err
	}

	if err := addDPUServiceChains(ctx, scope, dpfOperatorConfig, nil, skipFunc); err != nil {
		return nil, err
	}

	if err := addDPUServiceInterfaces(ctx, scope, dpfOperatorConfig, nil, skipFunc); err != nil {
		return nil, err
	}

	if err := addDPUServiceIPAMs(ctx, scope, dpfOperatorConfig, nil, nil); err != nil {
		return nil, err
	}

	if err := addDPUServiceNADs(ctx, scope, dpfOperatorConfig, nil, nil); err != nil {
		return nil, err
	}

	return tree, nil
}

// DiscoverDPUClusters returns a tree of objects representing the DPF status.
func DiscoverDPUClusters(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, _ func(map[string]string) bool) (*ObjectTree, error) {
	if err := addDPUClusters(ctx, scope, dpfOperatorConfig); err != nil {
		return nil, err
	}

	return tree, nil
}

// DiscoverStorage returns a tree of objects representing the storage resources status.
func DiscoverStorage(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, _ func(map[string]string) bool) (*ObjectTree, error) {
	if !scope.opts.ShowStorage {
		return tree, nil
	}

	storageResources := VirtualObject(dpfOperatorConfig.GetNamespace(), "StorageResources", "Storage Resources")
	scope.tree.Add(dpfOperatorConfig, storageResources)

	if err := addDPUVolumes(ctx, scope, storageResources, nil, nil); err != nil {
		return nil, err
	}

	if err := addDPUVolumeAttachments(ctx, scope, storageResources, nil, nil); err != nil {
		return nil, err
	}

	if err := addDPUStoragePolicies(ctx, scope, storageResources, nil, nil); err != nil {
		return nil, err
	}

	if err := addDPUStorageVendors(ctx, scope, storageResources, nil, nil); err != nil {
		return nil, err
	}

	return tree, nil
}

func getDPFOperatorConfig(ctx context.Context, c client.Client) (*operatorv1.DPFOperatorConfig, error) {
	dpfOperatorConfigList := &operatorv1.DPFOperatorConfigList{}
	if err := c.List(ctx, dpfOperatorConfigList); err != nil {
		return nil, err
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return nil, apierrors.NewNotFound(schema.GroupResource{
			Group:    operatorv1.DPFOperatorConfigGroupVersionKind.Group,
			Resource: operatorv1.DPFOperatorConfigGroupVersionKind.Kind,
		}, operatorcontroller.DefaultDPFOperatorConfigSingletonName)
	}
	dpfOperatorConfig := dpfOperatorConfigList.Items[0].DeepCopy()
	dpfOperatorConfig.TypeMeta = metav1.TypeMeta{
		Kind:       operatorv1.DPFOperatorConfigKind,
		APIVersion: operatorv1.GroupVersion.String(),
	}
	return dpfOperatorConfig, nil
}

func addDPUClusters(ctx context.Context, o objectScope, root client.Object) error {
	if !showResource(o.opts, provisioningv1.DPUClusterKind) {
		return nil
	}

	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := o.client.List(ctx, dpuClusterList); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuCluster := range dpuClusterList.Items {
		if !isObjDebug(&dpuCluster, o.opts.ShowResources) {
			continue
		}

		dpuCluster.TypeMeta = metav1.TypeMeta{
			Kind:       provisioningv1.DPUClusterKind,
			APIVersion: provisioningv1.GroupVersion.String(),
		}
		addToTree = append(addToTree, &dpuCluster)
		// TODO: add KamajiControlPlane to the loop, enabled via feature flag
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUClusters")
	return nil
}

func addDPUSets(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	if !showResource(o.opts, provisioningv1.DPUSetKind) {
		return nil
	}

	// Override the ShowResources option to show all the resources recursively.
	// If the ShowResources option is set to show DPUDeployment, then we should not skip resources owned by DPUDeployment.
	showDPUDeployment := showResource(o.opts, dpuservicev1.DPUDeploymentKind)
	o.opts.ShowChildResources = true

	dpuSetList := &provisioningv1.DPUSetList{}
	if err := o.client.List(ctx, dpuSetList, matchLabels); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuSet := range dpuSetList.Items {
		if !isObjDebug(&dpuSet, o.opts.ShowResources) {
			continue
		}

		// Continue if the resource is a child of a DPUDeployment and the matchLabels are nil.
		if showDPUDeployment && skipFunc != nil && skipFunc(dpuSet.GetLabels()) {
			continue
		}
		dpuSet.TypeMeta = metav1.TypeMeta{
			Kind:       provisioningv1.DPUSetKind,
			APIVersion: provisioningv1.GroupVersion.String(),
		}
		addToTree = append(addToTree, &dpuSet)

		if dpuSet.Spec.DPUTemplate.Spec.BFB.Name != "" {
			// Add BFB to the tree.
			bfb := &provisioningv1.BFB{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuSet.Spec.DPUTemplate.Spec.BFB.Name,
					Namespace: dpuSet.Namespace,
				},
			}
			bfbExists := true
			if err := o.client.Get(ctx, client.ObjectKeyFromObject(bfb), bfb); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				bfbExists = false
			}

			if bfbExists {
				virtBfb := VirtualObjectForVisualization(bfb, provisioningv1.BFBKind)
				readyCondition := metav1.Condition{
					Type:               string(provisioningv1.BFBReady),
					Status:             metav1.ConditionFalse,
					LastTransitionTime: bfb.ObjectMeta.GetCreationTimestamp(),
					Reason:             string(bfb.Status.Phase),
				}
				if bfb.Status.Phase == provisioningv1.BFBReady {
					readyCondition = metav1.Condition{
						Type:               string(provisioningv1.BFBReady),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: bfb.ObjectMeta.GetCreationTimestamp(),
						Reason:             string(bfb.Status.Phase),
						Message:            fmt.Sprintf("File: %s", filepath.Base(bfb.Spec.URL)),
					}
					if bfb.Status.Versions.DOCA != "" {
						readyCondition.Message += fmt.Sprintf(", DOCA: %s", bfb.Status.Versions.DOCA)
					}
				}

				virtBfb.Object["status"] = map[string]interface{}{
					"conditions": []metav1.Condition{readyCondition},
				}

				o.tree.Add(dpuSet.DeepCopy(), virtBfb)
			}
		}

		if err := addDPUs(ctx, o, dpuSet.DeepCopy(), client.MatchingLabels{
			util.DPUSetNameLabel:      dpuSet.Name,
			util.DPUSetNamespaceLabel: dpuSet.Namespace,
		}, nil); err != nil {
			return err
		}
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUSets")
	return nil
}

// TODO: add servicechainsets and servicechains from DPU cluster
// TODO: add serviceinterfacesets and serviceinterfaces from DPU cluster
// TODO: add cidrpools and ippools from DPU cluster
func addDPUs(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	if !showResource(o.opts, provisioningv1.DPUKind) && !showResource(o.opts, provisioningv1.DPUSetKind) {
		return nil
	}

	// If the ShowResources option is set to show DPUDeployment, then we should not skip resources owned by DPUDeployment.
	showDPUSets := showResource(o.opts, provisioningv1.DPUSetKind)

	dpuList := &provisioningv1.DPUList{}
	if err := o.client.List(ctx, dpuList, matchLabels); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpu := range dpuList.Items {
		if !isObjDebug(&dpu, o.opts.ShowResources) && !isObjDebug(root, o.opts.ShowResources) {
			continue
		}
		if showDPUSets && skipFunc != nil && skipFunc(dpu.Labels) {
			continue
		}

		dpu.TypeMeta = metav1.TypeMeta{
			Kind:       provisioningv1.DPUKind,
			APIVersion: provisioningv1.GroupVersion.String(),
		}

		conds := dpu.GetConditions()

		// TODO: remove this workaround as soon as all conditions gets initialized.
		_, readyCondition := util.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUReady))
		if readyCondition != nil {
			addToTree = append(addToTree, &dpu)
			continue
		}
		// Set a fake Ready condition based on the status.Phase.
		dpuStatus := metav1.ConditionFalse
		if dpu.Status.Phase == provisioningv1.DPUReady {
			dpuStatus = metav1.ConditionTrue
		}

		// Find the most recent lastTransitionTime in conditions to set the Age.
		newestLastTransitionTime := metav1.NewTime(time.Time{})
		for _, c := range conds {
			if c.LastTransitionTime.After(newestLastTransitionTime.Time) {
				newestLastTransitionTime = c.LastTransitionTime
			}
		}
		if !dpu.DeletionTimestamp.IsZero() && dpu.DeletionTimestamp.Time.After(newestLastTransitionTime.Time) {
			newestLastTransitionTime = *dpu.DeletionTimestamp
		}
		conds = append(conds, metav1.Condition{
			Type:               string(provisioningv1.DPUReady),
			Status:             dpuStatus,
			LastTransitionTime: newestLastTransitionTime,
			Reason:             string(dpu.Status.Phase),
		})
		dpu.SetConditions(conds)
		addToTree = append(addToTree, &dpu)
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUs", GroupingObject(true))

	return nil
}

func addDPUServices(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	if !showResource(o.opts, dpuservicev1.DPUServiceKind) {
		return nil
	}

	// Override the ShowResources option to show all the resources recursively.
	// If the ShowResources option is set to show DPUDeployment, then we should not skip resources owned by DPUDeployment.
	showDPUDeployment := showResource(o.opts, dpuservicev1.DPUDeploymentKind)
	o.opts.ShowChildResources = true

	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := o.client.List(ctx, dpuServiceList, matchLabels); err != nil {
		return err
	}

	// Get enabled system components to skip them
	enabledSystemComponents := getEnabledSystemComponents()

	addToTree := []client.Object{}
	for _, dpuService := range dpuServiceList.Items {
		if !isObjDebug(&dpuService, o.opts.ShowResources) {
			continue
		}

		// Skip enabled system components only if we're dealing with DPFOperatorConfig
		if enabledSystemComponents != nil {
			componentName, ok := dpuService.GetLabels()[operatorv1.DPFComponentLabelKey]
			if ok && enabledSystemComponents[componentName] {
				continue
			}
		}

		// Continue if the resource is a child of a DPUDeployment and the matchLabels are nil.
		if showDPUDeployment && skipFunc != nil && skipFunc(dpuService.GetLabels()) {
			continue
		}

		dpuService.TypeMeta = metav1.TypeMeta{
			Kind:       dpuservicev1.DPUServiceKind,
			APIVersion: dpuservicev1.GroupVersion.String(),
		}
		addToTree = append(addToTree, &dpuService)

		// Return early if we should not expand DPUServices.
		if meta.IsStatusConditionTrue(dpuService.GetConditions(), string(conditions.TypeReady)) && !isObjDebug(&dpuService, o.opts.ExpandResources) {
			continue
		}
		if err := addArgoApplication(ctx, o, dpuService); err != nil {
			return fmt.Errorf("get application information: %w", err)
		}
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUServices", GroupingObject(true))
	return nil
}

func addArgoApplication(ctx context.Context, o objectScope, dpuService dpuservicev1.DPUService) error {
	if !showResource(o.opts, application.ApplicationKind) {
		return nil
	}

	applications := argov1.ApplicationList{}
	if err := o.client.List(ctx, &applications, dpuService.MatchLabels()); err != nil {
		return err
	}
	for _, appObj := range applications.Items {
		virtApp := VirtualObjectForVisualization(&appObj, application.ApplicationKind)
		conditions := argoStatusResourcesToConditions(appObj.Status)
		// The conditions are not part of the ArgoCD Application object.
		// We need this workaround to be able to add conditions to the tree.
		virtApp.Object["status"] = map[string]interface{}{
			"conditions": conditions,
		}
		o.tree.Add(dpuService.DeepCopy(), virtApp)
	}
	return nil
}

// argoStatusResourcesToConditions converts the argo status resources to metav1 conditions.
func argoStatusResourcesToConditions(status argov1.ApplicationStatus) []metav1.Condition {
	conds := []metav1.Condition{}
	lastTransitionTime := status.ReconciledAt
	if lastTransitionTime == nil {
		lastTransitionTime = &metav1.Time{Time: time.Now()}
	}

	// Add ArgoCD's own conditions
	notReadyConditions := []string{}
	for _, c := range status.Conditions {
		// ArgoCD conditions only have Type, Message, and LastTransitionTime
		// We'll set Status to False since any condition indicates a problem
		// and use the Type as the Reason
		conds = append(conds, metav1.Condition{
			Type:               c.Type,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: *lastTransitionTime,
			Reason:             c.Type,
			Message:            c.Message,
		})
		notReadyConditions = append(notReadyConditions, c.Type)
	}

	// Add resource conditions
	for _, c := range status.Resources {
		if !isWorkloadKind(c.Kind) {
			continue
		}
		var cStatus, message string
		if c.Health != nil {
			cStatus = string(c.Health.Status)
			message = c.Health.Message
		}
		conds = append(conds, metav1.Condition{
			Type:               fmt.Sprintf("%s/%s", c.Kind, c.Name),
			Status:             metav1.ConditionStatus(c.Status),
			LastTransitionTime: *lastTransitionTime,
			Reason:             cStatus,
			Message:            message,
		})
	}

	// Add ready condition - false if there are any conditions or if health status is not healthy
	cond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: *lastTransitionTime,
		Reason:             "Success",
	}
	if status.Health.Status != argov1.HealthStatusHealthy || len(status.Conditions) > 0 {
		cond.Status = metav1.ConditionFalse
		cond.Reason = string(status.Health.Status)
		if len(status.Conditions) > 0 {
			cond.Message = conditions.ReadyConditionMessage(conditions.MessageNotReady, notReadyConditions)
		}
	}
	conds = append(conds, cond)
	return conds
}

func addDPUServiceChains(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, dpuservicev1.DPUServiceChainGroupVersionKind, matchLabels, skipFunc)
}

func addDPUServiceInterfaces(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, dpuservicev1.DPUServiceInterfaceGroupVersionKind, matchLabels, skipFunc)
}

func addDPUServiceIPAMs(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, dpuservicev1.DPUServiceIPAMGroupVersionKind, matchLabels, skipFunc)
}

func addDPUServiceCredentialRequests(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, dpuservicev1.DPUServiceCredentialRequestGroupVersionKind, matchLabels, skipFunc)
}

func addDPUServiceTemplates(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, dpuservicev1.DPUServiceTemplateGroupVersionKind, matchLabels, skipFunc)
}

func addDPUServiceNADs(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, dpuservicev1.DPUServiceNADGroupVersionKind, matchLabels, skipFunc)
}

func addResourceByGVK(ctx context.Context, o objectScope, root client.Object, gvk schema.GroupVersionKind, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	if !showResource(o.opts, gvk.Kind) {
		return nil
	}

	resourceList := &unstructured.UnstructuredList{}
	resourceList.SetGroupVersionKind(gvk)
	if err := o.client.List(ctx, resourceList, matchLabels); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, resource := range resourceList.Items {
		if skipFunc != nil && skipFunc(resource.GetLabels()) {
			continue
		}
		addToTree = append(addToTree, &resource)
	}

	o.tree.AddMultipleWithHeader(root, addToTree, gvk.Kind, GroupingObject(true))
	return nil
}

func addDPUDeployments(ctx context.Context, o objectScope, root client.Object) error {
	if !showResource(o.opts, dpuservicev1.DPUDeploymentKind) {
		return nil
	}

	// Override the ShowResources option to show all the resources recursively.
	o.opts.ShowChildResources = true

	dpuDeploymentsList := &dpuservicev1.DPUDeploymentList{}
	if err := o.client.List(ctx, dpuDeploymentsList); err != nil {
		return err
	}
	if len(dpuDeploymentsList.Items) == 0 {
		return nil
	}

	addToTree := []client.Object{}
	for _, dpuDeployment := range dpuDeploymentsList.Items {
		dpuDeployment.TypeMeta = metav1.TypeMeta{
			Kind:       dpuservicev1.DPUDeploymentKind,
			APIVersion: dpuservicev1.GroupVersion.String(),
		}

		// If it is requested to show all the conditions for the root, add
		// the ShowObjectConditionsAnnotation to signal this to the presentation layer.
		if isObjDebug(root, o.opts.ShowOtherConditions) {
			addAnnotation(root, ShowObjectConditionsAnnotation, "True")
		}

		addToTree = append(addToTree, &dpuDeployment)
		parentDPUName := fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName())

		if err := addDPUSets(ctx, o, &dpuDeployment, client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: parentDPUName,
		}, nil); err != nil {
			return err
		}

		if err := addDPUServiceChains(ctx, o, &dpuDeployment, client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: parentDPUName,
		}, nil); err != nil {
			return err
		}

		if err := addDPUServiceInterfaces(ctx, o, &dpuDeployment, client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: parentDPUName,
		}, nil); err != nil {
			return err
		}

		headerName := "Services"
		servicesObj := VirtualObject("", headerName, headerName)
		o.tree.Add(&dpuDeployment, servicesObj)

		if err := addDPUServiceTemplates(ctx, o, servicesObj, client.MatchingLabels{
			dpuDeployment.GetDependentLabelKey(): dpuservicev1.DependentDPUDeploymentLabelValue,
		}, nil); err != nil {
			return err
		}

		if err := addDPUServices(ctx, o, servicesObj, client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: parentDPUName,
		}, nil); err != nil {
			return err
		}
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUDeployments", GroupingObject(true))
	return nil
}

// addDPUVolumes adds DPUVolumes to the tree.
func addDPUVolumes(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, storagev1.DPUVolumeGroupVersionKind, matchLabels, skipFunc)
}

// addDPUVolumeAttachments adds DPUVolumeAttachments to the tree.
func addDPUVolumeAttachments(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, storagev1.DPUVolumeAttachmentGroupVersionKind, matchLabels, skipFunc)
}

// addDPUStoragePolicies adds DPUStoragePolicies to the tree.
func addDPUStoragePolicies(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, storagev1.DPUStoragePolicyGroupVersionKind, matchLabels, skipFunc)
}

// addDPUStorageVendors adds DPUStorageVendors to the tree.
func addDPUStorageVendors(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	return addResourceByGVK(ctx, o, root, storagev1.DPUStorageVendorGroupVersionKind, matchLabels, skipFunc)
}
