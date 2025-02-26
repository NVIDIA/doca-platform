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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/argocd/api/application"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type objectScope struct {
	client client.Client
	tree   ObjectTree
	opts   ObjectTreeOptions
}

func Discover(ctx context.Context, c client.Client, opts ObjectTreeOptions, subCmd string) (*ObjectTree, error) {
	dpfOperatorConfig, err := getDPFOperatorConfig(ctx, c)
	if err != nil {
		return nil, err
	}

	tree := NewObjectTree(dpfOperatorConfig, opts)

	scope := objectScope{
		client: c,
		tree:   *tree,
		opts:   opts,
	}

	skipDPUDeploymentFunc := func(labels map[string]string) bool {
		return labels[dpuservicev1.ParentDPUDeploymentNameLabel] != ""
	}

	switch subCmd {
	case "all":
		tree, err = DiscoverDPUServices(ctx, tree, scope, dpfOperatorConfig, skipDPUDeploymentFunc)
		if err != nil {
			return nil, err
		}
		tree, err = DiscoverDPUDeployments(ctx, tree, scope, dpfOperatorConfig)
		if err != nil {
			return nil, err
		}
		tree, err = DiscoverDPUSets(ctx, tree, scope, dpfOperatorConfig, skipDPUDeploymentFunc)
		if err != nil {
			return nil, err
		}
		return DiscoverDPUClusters(ctx, tree, scope, dpfOperatorConfig)
	case "dpuservices":
		return DiscoverDPUServices(ctx, tree, scope, dpfOperatorConfig, nil)
	case "dpudeployments":
		return DiscoverDPUDeployments(ctx, tree, scope, dpfOperatorConfig)
	case "dpusets":
		return DiscoverDPUSets(ctx, tree, scope, dpfOperatorConfig, skipDPUDeploymentFunc)
	case "dpuclusters":
		return DiscoverDPUClusters(ctx, tree, scope, dpfOperatorConfig)
	}
	return nil, fmt.Errorf("unknown object type %q", subCmd)
}

// DiscoverDPUServices returns a tree of objects representing the DPF status.
func DiscoverDPUServices(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig, skipFunc func(map[string]string) bool) (*ObjectTree, error) {
	if err := addDPUServices(ctx, scope, dpfOperatorConfig, nil, skipFunc); err != nil {
		return nil, err
	}

	if err := addDPUServiceCredentialRequests(ctx, scope, dpfOperatorConfig, nil, nil); err != nil {
		return nil, err
	}

	return tree, nil
}

// DiscoverDPUDeployments returns a tree of objects representing the DPF status.
func DiscoverDPUDeployments(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig) (*ObjectTree, error) {
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

	return tree, nil
}

// DiscoverDPUClusters returns a tree of objects representing the DPF status.
func DiscoverDPUClusters(ctx context.Context, tree *ObjectTree, scope objectScope, dpfOperatorConfig *operatorv1.DPFOperatorConfig) (*ObjectTree, error) {
	if err := addDPUClusters(ctx, scope, dpfOperatorConfig); err != nil {
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
	if !showResource(o.opts.ShowResources, provisioningv1.DPUClusterKind) {
		return nil
	}

	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := o.client.List(ctx, dpuClusterList); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuCluster := range dpuClusterList.Items {
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
	if !showResource(o.opts.ShowResources, provisioningv1.DPUSetKind) {
		return nil
	}

	// Override the ShowResources option to show all the resources recursively.
	// If the ShowResources option is set to show DPUDeployment, then we should not skip resources owned by DPUDeployment.
	showDPUDeployment := showResource(o.opts.ShowResources, dpuservicev1.DPUDeploymentKind)
	o.opts.ShowResources = ""

	dpuSetList := &provisioningv1.DPUSetList{}
	if err := o.client.List(ctx, dpuSetList, matchLabels); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuSet := range dpuSetList.Items {
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
			if err := o.client.Get(ctx, client.ObjectKeyFromObject(bfb), bfb); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
			}
			bfb.TypeMeta = metav1.TypeMeta{
				Kind:       provisioningv1.BFBKind,
				APIVersion: provisioningv1.GroupVersion.String(),
			}
			o.tree.Add(dpuSet.DeepCopy(), bfb)
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
	if !showResource(o.opts.ShowResources, provisioningv1.DPUKind) {
		return nil
	}

	// If the ShowResources option is set to show DPUDeployment, then we should not skip resources owned by DPUDeployment.
	showDPUSets := showResource(o.opts.ShowResources, provisioningv1.DPUSetKind)

	dpuList := &provisioningv1.DPUList{}
	if err := o.client.List(ctx, dpuList, matchLabels); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpu := range dpuList.Items {
		if showDPUSets && skipFunc != nil && skipFunc(dpu.Labels) {
			continue
		}
		dpu.TypeMeta = metav1.TypeMeta{
			Kind:       provisioningv1.DPUKind,
			APIVersion: provisioningv1.GroupVersion.String(),
		}

		conds := dpu.GetConditions()

		// TODO: remove this workaround as soon as all conditions gets initialized.
		// get ready condition
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

		// find newest lastTransitionTime in conditions
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

	// If matchLabels is nil, it means that the DPUs are not part of a DPUSet.
	if matchLabels == nil {
		o.tree.AddMultipleWithHeader(root, addToTree, "DPUs", GroupingObject(true))
		return nil
	}

	for _, dpu := range addToTree {
		o.tree.Add(root, dpu)
	}

	return nil
}

func addDPUServices(ctx context.Context, o objectScope, root client.Object, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	if !showResource(o.opts.ShowResources, dpuservicev1.DPUServiceKind) {
		return nil
	}

	// Override the ShowResources option to show all the resources recursively.
	// If the ShowResources option is set to show DPUDeployment, then we should not skip resources owned by DPUDeployment.
	showDPUDeployment := showResource(o.opts.ShowResources, dpuservicev1.DPUDeploymentKind)
	o.opts.ShowResources = ""

	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := o.client.List(ctx, dpuServiceList, matchLabels); err != nil {
		return err
	}

	addToTree := []client.Object{}
	for _, dpuService := range dpuServiceList.Items {
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
		if !isObjDebug(&dpuService, o.opts.ExpandResources) {
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
	if !showResource(o.opts.ShowResources, application.ApplicationKind) {
		return nil
	}

	applications := argov1.ApplicationList{}
	if err := o.client.List(ctx, &applications, client.MatchingLabels{
		dpuservicev1.DPUServiceNameLabelKey:      dpuService.Name,
		dpuservicev1.DPUServiceNamespaceLabelKey: dpuService.Namespace,
	}); err != nil {
		return err
	}
	for _, appObj := range applications.Items {
		virtApp := VirtualObject(appObj.GetNamespace(), application.ApplicationKind, appObj.GetName())
		virtApp.SetAnnotations(nil)
		conditions := argoStatusResourcesToConditions(appObj.Status)
		// add conditions to unstructured object under .status.conditions
		virtApp.Object["status"] = map[string]interface{}{
			"conditions": conditions,
		}
		o.tree.Add(dpuService.DeepCopy(), virtApp)
	}
	return nil
}

// argoStatusResourcesToConditions converts the argo status resources to metav1 conditions.
func argoStatusResourcesToConditions(status argov1.ApplicationStatus) []metav1.Condition {
	conditions := []metav1.Condition{}
	lastTransitionTime := status.ReconciledAt
	if lastTransitionTime == nil {
		lastTransitionTime = &metav1.Time{Time: time.Now()}
	}
	for _, c := range status.Resources {
		if !isWorkloadKind(c.Kind) {
			continue
		}
		var cStatus, message string
		if c.Health != nil {
			cStatus = string(c.Health.Status)
			message = c.Health.Message
		}
		conditions = append(conditions, metav1.Condition{
			Type:               fmt.Sprintf("%s/%s", c.Kind, c.Name),
			Status:             metav1.ConditionStatus(c.Status),
			LastTransitionTime: *lastTransitionTime,
			Reason:             cStatus,
			Message:            message,
		})
	}

	// Add ready condition
	cond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: *lastTransitionTime,
		Reason:             "Success",
	}
	if status.Health.Status != argov1.HealthStatusHealthy {
		cond.Status = metav1.ConditionFalse
		cond.Reason = string(status.Health.Status)
	}
	conditions = append(conditions, cond)
	return conditions
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

func addResourceByGVK(ctx context.Context, o objectScope, root client.Object, gvk schema.GroupVersionKind, matchLabels client.MatchingLabels, skipFunc func(map[string]string) bool) error {
	if !showResource(o.opts.ShowResources, gvk.Kind) {
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

	o.tree.AddMultipleWithHeader(root, addToTree, gvk.Kind)
	return nil
}

func addDPUDeployments(ctx context.Context, o objectScope, root client.Object) error {
	if !showResource(o.opts.ShowResources, dpuservicev1.DPUDeploymentKind) {
		return nil
	}

	// Override the ShowResources option to show all the resources recursively.
	o.opts.ShowResources = ""

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

		if err := addDPUServices(ctx, o, &dpuDeployment, client.MatchingLabels{
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
	}

	o.tree.AddMultipleWithHeader(root, addToTree, "DPUDeployments", GroupingObject(true))
	return nil
}
