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
	"fmt"
	"maps"
	"slices"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/dpuservice/templates"
	"github.com/nvidia/doca-platform/internal/dpuservice/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileDPUServices reconciles the DPUServices created by the DPUDeployment.
func reconcileDPUServices(
	ctx context.Context,
	c client.Client,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dependencies *dpuDeploymentDependencies,
	interfaceNameByServiceName map[string]interfaceNameToDPUServiceInterfaceName,
	dpuNodeLabels map[string]string,
) (ctrl.Result, error) {
	owner := metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)
	requeue := ctrl.Result{}

	// Get all existing DPUSets
	existingDPUSets := &provisioningv1.DPUSetList{}
	if err := c.List(ctx,
		existingDPUSets,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list existing DPUSets: %w", err)
	}

	// Get all existing DPUServices
	existingDPUServices := &dpuservicev1.DPUServiceList{}
	if err := c.List(ctx,
		existingDPUServices,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list existing DPUServices: %w", err)
	}

	existingDPUServicesMap := make(map[string]dpuservicev1.DPUService)
	for _, dpuService := range existingDPUServices.Items {
		existingDPUServicesMap[dpuService.Name] = dpuService
	}

	// get an ordered list of the DPUServices
	sortedServices, err := servicesTopologicalSort(dpuDeployment)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to sort DPUService: %w", err)
	}

	patchedDPUServices := make(map[string]*dpuservicev1.DPUService, len(existingDPUServices.Items))
	var errs []error
	// Create or update DPUServices to match what is defined in the DPUDeployment.
	for _, serviceName := range sortedServices {
		serviceConfig := dependencies.DPUServiceConfigurations[serviceName]
		serviceTemplate := dependencies.DPUServiceTemplates[serviceName]
		if len(dpuDeployment.Spec.Services[serviceName].DependsOn) != 0 {
			// Check dependencies and requeue the reconciliation if the check fails.
			err := checkDPUServiceDependencies(ctx, c, dpuDeployment.Spec.Services[serviceName].DependsOn, patchedDPUServices)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to check dependencies for DPUService %s: %w", serviceName, err)
			}
			rendered, err := templateDPUServiceConfigurationValues(serviceConfig, dpuDeployment.Spec.Services[serviceName].DependsOn, patchedDPUServices)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to template DPUService %s: %w", serviceName, err))
				continue
			}
			serviceConfig.Spec.ServiceConfiguration.HelmChart.Values = rendered
		}
		versionDigest := calculateDPUServiceVersionDigest(serviceConfig, serviceTemplate)
		newRevision, err := generateDPUService(client.ObjectKeyFromObject(dpuDeployment),
			owner,
			serviceName,
			serviceConfig,
			serviceTemplate,
			interfaceNameByServiceName[serviceName],
			versionDigest,
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to generate DPUService %s: %w", serviceName, err))
			continue
		}

		// filter existing services by name
		currentRevision, oldRevisions := getCurrentAndStaleDPUServices(serviceName, versionDigest, existingDPUServices)
		switch {
		case currentRevision != nil:
			// we found the current revision based on the digest, there might be old revisions to handle
			req := reconcileCurrentDPUServiceRevision(ctx, c, newRevision, currentRevision, oldRevisions, serviceName, dpuDeployment, serviceConfig, dpuNodeLabels, existingDPUServicesMap, existingDPUSets.Items)

			if !req.IsZero() {
				requeue = req
			}
		case len(oldRevisions) > 0:
			// we have only previous revisions
			req, err := reconcileDPUServiceWithOldRevisions(ctx, c, newRevision, oldRevisions, serviceName, dpuDeployment, serviceConfig, dpuNodeLabels, existingDPUServicesMap)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if !req.IsZero() {
				requeue = req
			}
		default:
			// no previous version, we are creating a new one
			reconcileNewDPUServiceRevision(newRevision, serviceName, dpuDeployment, serviceConfig, dpuNodeLabels)
		}

		newRevision.SetManagedFields(nil)
		newRevision.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)

		// we have to patch in this loop because other dpuServices might depend on this one
		// further down the line
		if err := c.Patch(ctx, newRevision, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
			errs = append(errs, fmt.Errorf("failed to patch DPUService %s: %w", client.ObjectKeyFromObject(newRevision), err))
			continue
		}

		patchedDPUServices[serviceName] = newRevision
	}

	// If we have found errors, we don't want to delete any of the existing DPUServices as we might have partially applied
	// the logic of modifying the existingDPUServicesMap.
	if len(errs) > 0 {
		return ctrl.Result{}, kerrors.NewAggregate(errs)
	}

	// Cleanup the remaining stale DPUServices
	for _, dpuService := range existingDPUServicesMap {
		if err := c.Delete(ctx, &dpuService); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete DPUService %s: %w", client.ObjectKeyFromObject(&dpuService), err)
		}
	}

	return requeue, nil
}

// reconcileNewDPUServiceRevision reconciles a new revision of the DPUService.
// It accepts a new DPUService object and the DPUService name as defined in the DPUDeployment.
// It sets the nodeSelector for the DPUService and updates the DPUNodeLabels.
func reconcileNewDPUServiceRevision(newRevision *dpuservicev1.DPUService, dpuServiceName string,
	dpuDeployment *dpuservicev1.DPUDeployment,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	dpuNodeLabels map[string]string,
) {
	dpuServiceNodeSelector := newObjectNodeSelectorWithOwner(getDPUServiceVersionLabelKey(dpuServiceName), newRevision.Name, client.ObjectKeyFromObject(dpuDeployment))
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		newRevision.SetServiceDaemonSetNodeSelector(dpuServiceNodeSelector)
	} else {
		// TODO: we have to update this logic if we want to introduce the support for disruptive upgrades.
		nodeSelector := newInClusterNodeSelectorFromDPUSetSelector(dpuDeployment.Spec.DPUs.DPUSets)
		newRevision.SetServiceDaemonSetNodeSelector(nodeSelector)
	}

	// This is needed in all cases, because we don't know if a dpuSet will be updated or created
	// This node label will be used to patch the dpuSets in the same reconcile loop
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		setDPUServiceNodeLabelValue(dpuServiceName, newRevision.Name, dpuNodeLabels)
	}

	// TODO: By default when creating a new non-disruptive DPUService
	// we should not cause nodeEffect because of labels
}

// reconcileDPUServiceWithOldRevisions reconciles a new revision of the DPUService and the old revisions.
// It accepts a new DPUService object,the DPUService name as defined in the DPUDeployment and the old revisions.
// It sets the nodeSelector for the DPUService and updates the DPUNodeLabels. It also pauses the old revisions.
func reconcileDPUServiceWithOldRevisions(ctx context.Context, c client.Client, newRevision *dpuservicev1.DPUService, oldRevisions []client.Object,
	dpuServiceName string,
	dpuDeployment *dpuservicev1.DPUDeployment,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	dpuNodeLabels map[string]string,
	existingDPUServicesMap map[string]dpuservicev1.DPUService) (ctrl.Result, error) {
	dpuServiceNodeSelector := newObjectNodeSelectorWithOwner(getDPUServiceVersionLabelKey(dpuServiceName), newRevision.Name, client.ObjectKeyFromObject(dpuDeployment))
	nodeLabelValue := newRevision.Name
	var toRetain []client.Object
	if serviceConfig.Spec.UpgradePolicy.ShouldApplyNodeEffect() {
		// the logic regarding the previous revisions is as follows:
		// - pause all previous revisions before creating the new one
		//   - The number of previous are controlled by the revisionHistoryLimit. This limit can be hit when several updates are done in a short period of time
		//     while the previous revisions are still not ready. The previous revisions are always sorted by creation timestamp. If the limit is hit, the oldest
		//     revisions are deleted in order to comply with the limit.
		// - once the youngest revision is ready, we can resume and delete the previous revisions.
		//
		// Hence we never delete old revisions while the current one is not ready, this is to avoid downtime
		toRetain = getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit-1)
		if err := pauseStaleDPUServices(ctx, c, clientObjectToDPUServiceList(toRetain)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to pause stale %s %s: %w", newRevision.GetObjectKind().GroupVersionKind().String(), newRevision.GetName(), err)
		}

		// TODO: implement disruptive upgrades for in-cluster DPUServices with v25.7.
		if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
			newRevision.SetServiceDaemonSetNodeSelector(dpuServiceNodeSelector)
		}
	} else {
		// just patch the youngest existing service
		toRetain = getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit)
		oldSvc := toRetain[0].(*dpuservicev1.DPUService)
		newRevision.Name = oldSvc.Name
		if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
			// we are not creating a new service, we are updating the existing one
			// so we need to keep the same `version` and `owned-by-dpudeployment` selector to avoid downtime
			newRevision.SetServiceDaemonSetNodeSelector(&corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{getNodeSelectorTermForDPUServiceVersion(oldSvc.Spec.ServiceDaemonSet.NodeSelector, dpuServiceName)},
			})
			nodeLabelValue = getNodeSelectorDPUServiceVersionValue(oldSvc.Spec.ServiceDaemonSet.NodeSelector, dpuServiceName)
		} else {
			// TODO: we have to update this logic if we want to introduce the support for disruptive upgrades.
			nodeSelector := newInClusterNodeSelectorFromDPUSetSelector(dpuDeployment.Spec.DPUs.DPUSets)
			newRevision.SetServiceDaemonSetNodeSelector(nodeSelector)
		}
	}

	for _, svc := range toRetain {
		delete(existingDPUServicesMap, svc.GetName())
	}

	// This is needed in all cases, because we don't know if a dpuSet will be updated or created
	// This node label will be used to patch the dpuSets in the same reconcile loop
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		setDPUServiceNodeLabelValue(dpuServiceName, nodeLabelValue, dpuNodeLabels)
	}

	return ctrl.Result{RequeueAfter: reconcileRequeueDuration}, nil
}

// reconcileCurrentDPUServiceRevision reconciles the current revision of the DPUService and the old revisions.
// It accepts the current DPUService object and the old revisions of the DPUService.
// It cleans the old revisions and updates the Node Label value.
func reconcileCurrentDPUServiceRevision(ctx context.Context, c client.Client,
	newRevision *dpuservicev1.DPUService,
	currentRevision client.Object,
	oldRevisions []client.Object,
	dpuServiceName string,
	dpuDeployment *dpuservicev1.DPUDeployment,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	dpuNodeLabels map[string]string,
	existingDPUServicesMap map[string]dpuservicev1.DPUService,
	existingDPUSets []provisioningv1.DPUSet,
) ctrl.Result {
	log := ctrllog.FromContext(ctx)
	requeue := ctrl.Result{RequeueAfter: reconcileRequeueDuration}
	currentRev, oldRevs := currentRevision.(*dpuservicev1.DPUService), clientObjectToDPUServiceList(oldRevisions)

	// We update the name of the newRevision to the currentRevision so that we can patch the existing object
	newRevision.Name = currentRevision.GetName()

	// We update the newRevision nodeSelector to match the currentRevision nodeSelector since it relies on the revision name
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		newRevision.SetServiceDaemonSetNodeSelector(&corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{getNodeSelectorTermForDPUServiceVersion(currentRev.Spec.ServiceDaemonSet.NodeSelector, dpuServiceName)},
		})
	} else {
		// TODO: we have to update this logic if we want to introduce the support for disruptive upgrades.
		nodeSelector := newInClusterNodeSelectorFromDPUSetSelector(dpuDeployment.Spec.DPUs.DPUSets)
		newRevision.SetServiceDaemonSetNodeSelector(nodeSelector)
	}

	// We delete the current revision so that it doesn't get cleaned up
	delete(existingDPUServicesMap, newRevision.GetName())

	// This is needed because we don't know if a dpuSet will be updated or created
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		setDPUServiceNodeLabelValue(dpuServiceName, getNodeSelectorDPUServiceVersionValue(currentRev.Spec.ServiceDaemonSet.NodeSelector, dpuServiceName), dpuNodeLabels)
	}

	// If there are no old revisions, we don't need to enqueue as there are no old revisions that need cleanup.
	if len(oldRevs) == 0 {
		return ctrl.Result{}
	}

	// if the current revision is still not ready, keep the eventual old revisions and requeue
	// otherwise, clean old revisions
	if conditions.IsTrue(currentRev, conditions.TypeReady) && len(getNotReadyDPUSets(existingDPUSets)) == 0 {
		err := cleanStaleDPUServices(ctx, c, oldRevs)
		if err != nil {
			log.Error(err, "failed to resume stale DPUService")
		}
		// don't requeue if the current revision is ready
		requeue = ctrl.Result{}
	}

	for _, svc := range oldRevs {
		delete(existingDPUServicesMap, svc.Name)
	}

	return requeue
}

// setDPUServiceNodeLabelValue sets the value of the node label for the DPUService.
func setDPUServiceNodeLabelValue(dpuServiceName, value string, nodeLabels map[string]string) {
	nodeLabels[getDPUServiceVersionLabelKey(dpuServiceName)] = value
}

// getCurrentAndStaleDPUServices returns the current and stale DPUService objects.
func getCurrentAndStaleDPUServices(dpuServiceName, currentVersionDigest string, existingDPUServices *dpuservicev1.DPUServiceList) (client.Object, []client.Object) {
	match := []client.Object{}
	var current client.Object
	for _, dpuService := range existingDPUServices.Items {
		if matchDPUServiceName(dpuService.Name, dpuServiceName) {
			if dpuService.Annotations[dpuServiceVersionAnnotationKey] == currentVersionDigest {
				current = &dpuService
			} else {
				match = append(match, &dpuService)
			}
		}
	}
	return current, match
}

// matchDPUServiceName checks if a given name matches the DPUService name.
// We expect the given name to belong to a dpuService that is created by the DPUDeployment.
func matchDPUServiceName(name, dpuServiceName string) bool {
	// There are two cases:
	// 1. The name is in legacy format `<dpudeploymentName>-<dpuServiceName>`
	// 2. The name is in the new format `<dpuServiceName>-<randomLengthSuffix>`
	// Those two formats are mutually exclusive and come from the function that
	// generates the DPUServices.

	// The legacy format is used for DPUService created before the introduction of the new format.
	// We keep the legacy format for backward compatibility. We just check if the name ends with the dpuServiceName.
	if strings.HasSuffix(name, dpuServiceName) {
		return true
	}

	// Otherwise, we check if the name is in the new format.
	index := strings.LastIndex(name, "-")
	if index == -1 {
		return false
	}
	if len(name[index:]) != resourceNameGeneratedSuffixLength+1 {
		return false
	}
	return name[:index] == dpuServiceName
}

// CalculateDPUServiceVersionDigest calculates the digest of the DPUService.
func calculateDPUServiceVersionDigest(configuration *dpuservicev1.DPUServiceConfiguration, template *dpuservicev1.DPUServiceTemplate) string {
	config := configuration.DeepCopy()
	// The upgrade change should not affect the digest, always set it to the default value
	config.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{}
	return digest.Short(digest.FromObjects(config.Spec, template.Spec), 10)
}

func getDPUServiceVersionLabelKey(name string) string {
	return fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-%s-version", name)
}

// generateDPUService generates a DPUService according to the DPUDeployment.
func generateDPUService(dpuDeploymentNamespacedName types.NamespacedName,
	owner *metav1.OwnerReference,
	name string,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	serviceTemplate *dpuservicev1.DPUServiceTemplate,
	interfaces map[string]string,
	versionDigest string,
) (*dpuservicev1.DPUService, error) {

	var serviceConfigValues, serviceTemplateValues map[string]interface{}
	if serviceConfig.Spec.ServiceConfiguration.HelmChart.Values != nil {
		if err := json.Unmarshal(serviceConfig.Spec.ServiceConfiguration.HelmChart.Values.Raw, &serviceConfigValues); err != nil {
			return nil, fmt.Errorf("error while unmarshaling serviceConfig values: %w", err)
		}
	}
	if serviceTemplate.Spec.HelmChart.Values != nil {
		if err := json.Unmarshal(serviceTemplate.Spec.HelmChart.Values.Raw, &serviceTemplateValues); err != nil {
			return nil, fmt.Errorf("error while unmarshaling serviceTemplate values: %w", err)
		}
	}

	mergedValues := utils.MergeMaps(serviceConfigValues, serviceTemplateValues)
	var mergedValuesRawExtension *runtime.RawExtension
	if mergedValues != nil {
		mergedValuesRaw, err := json.Marshal(mergedValues)
		if err != nil {
			return nil, fmt.Errorf("error while marshaling merged ServiceTemplate and ServiceConfig values: %w", err)
		}
		mergedValuesRawExtension = &runtime.RawExtension{Raw: mergedValuesRaw}
	}

	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", name, utilrand.String(resourceNameGeneratedSuffixLength)),
			Namespace: dpuDeploymentNamespacedName.Namespace,
			// we save the version in the annotations, as this will permit us to retrieve
			// a dpuService by its version
			Annotations: map[string]string{
				dpuServiceVersionAnnotationKey: versionDigest,
			},
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName),
			},
		},
		Spec: dpuservicev1.DPUServiceSpec{
			HelmChart: dpuservicev1.HelmChart{
				Source: serviceTemplate.Spec.HelmChart.Source,
				Values: mergedValuesRawExtension,
			},
			ServiceID:       ptr.To(getServiceID(dpuDeploymentNamespacedName, name)),
			DeployInCluster: serviceConfig.Spec.ServiceConfiguration.DeployInCluster,
			Interfaces:      slices.Sorted(maps.Values(interfaces)),
		},
	}

	dpuService.Spec.ServiceDaemonSet = generateDPUServiceDaemonSetValues(serviceConfig.Spec.ServiceConfiguration.ServiceDaemonSet)

	if serviceConfig.Spec.ServiceConfiguration.ConfigPorts != nil {
		dpuService.Spec.ConfigPorts = serviceConfig.Spec.ServiceConfiguration.ConfigPorts
	}

	dpuService.SetOwnerReferences([]metav1.OwnerReference{*owner})

	return dpuService, nil
}

func generateDPUServiceDaemonSetValues(serviceDaemonSet dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues) *dpuservicev1.ServiceDaemonSetValues {
	var serviceDaemonSetValues *dpuservicev1.ServiceDaemonSetValues
	if serviceDaemonSet.Labels != nil || serviceDaemonSet.Annotations != nil || serviceDaemonSet.UpdateStrategy != nil || serviceDaemonSet.Resources != nil {
		serviceDaemonSetValues = &dpuservicev1.ServiceDaemonSetValues{
			Labels:         serviceDaemonSet.Labels,
			Annotations:    serviceDaemonSet.Annotations,
			UpdateStrategy: serviceDaemonSet.UpdateStrategy,
			Resources:      serviceDaemonSet.Resources,
		}
	}
	return serviceDaemonSetValues
}

// cleanStaleDPUServices deletes all the stale DPUService objects that are not needed anymore.
// It resumes the reconciliation as well in order for the deletion to take effect since DPUService controller won't
// remove the finalizer if the DPUService is paused.
func cleanStaleDPUServices(ctx context.Context, c client.Client, oldRevisions []dpuservicev1.DPUService) error {
	// deletion happens before resume here because we want deletion to take effect immediately.
	// This is the case if the object are marked while the controller is ignoring them
	for _, dpuService := range oldRevisions {
		if err := c.Delete(ctx, &dpuService); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("error while deleting %s %s: %w", dpuService.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuService), err)
			}
		}
	}
	for _, dpuService := range oldRevisions {
		dpuService.Spec.Paused = ptr.To(false)
		dpuService.SetManagedFields(nil)
		dpuService.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
		if err := c.Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("error while patching %s %s: %w", dpuService.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuService), err)
			}
		}
	}
	return nil
}

// getNodeSelectorDPUServiceVersionValue returns the NodeSelectorTerm for the given DPUService version label key.
func getNodeSelectorDPUServiceVersionValue(nodeSelector *corev1.NodeSelector, dpuServiceName string) string {
	var version string
	for _, term := range nodeSelector.NodeSelectorTerms {
		for _, req := range term.MatchExpressions {
			if req.Key == getDPUServiceVersionLabelKey(dpuServiceName) {
				version = req.Values[0]
				break
			}
		}
	}
	return version
}

// pauseStaleDPUServices disable reconciliation on previous versions of a DPUService up to the revisionHistoryLimit.
// All older versions will be deleted.
func pauseStaleDPUServices(ctx context.Context, c client.Client, dpuServices []dpuservicev1.DPUService) error {
	for _, dpuService := range dpuServices {
		dpuService.Spec.Paused = ptr.To(true)
		dpuService.SetManagedFields(nil)
		if err := c.Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
			return fmt.Errorf("failed to patch DPUService %s: %w", client.ObjectKeyFromObject(&dpuService), err)
		}
	}

	return nil
}

// getNodeSelectorTermForDPUServiceVersion returns the NodeSelectorTerm for the given DPUService version label key.
func getNodeSelectorTermForDPUServiceVersion(nodeSelector *corev1.NodeSelector, dpuServiceName string) corev1.NodeSelectorTerm {
	var target corev1.NodeSelectorTerm
	for _, term := range nodeSelector.NodeSelectorTerms {
		for _, req := range term.MatchExpressions {
			if req.Key == getDPUServiceVersionLabelKey(dpuServiceName) {
				target = term
				break
			}
		}
	}
	return target
}

// generateServiceID generates the serviceID for the child resources of a DPUDeployment.
func getServiceID(dpuDeploymentNamespacedName types.NamespacedName, serviceName string) string {
	return fmt.Sprintf("dpudeployment_%s_%s", dpuDeploymentNamespacedName.Name, serviceName)
}

func clientObjectToDPUServiceList(oldRevisions []client.Object) []dpuservicev1.DPUService {
	oldRevs := make([]dpuservicev1.DPUService, 0, len(oldRevisions))
	for _, svc := range oldRevisions {
		oldRevs = append(oldRevs, *svc.(*dpuservicev1.DPUService))
	}
	return oldRevs
}

// servicesTopologicalSort sorts the services in a DPUDeployment based on their dependencies.
// It returns a slice of service names in the order they should be deployed.
// It returns an error if there are circular dependencies or if a dependency is not found.
func servicesTopologicalSort(dpuDeployment *dpuservicev1.DPUDeployment) ([]string, error) {
	graph := simple.NewDirectedGraph()

	// Create a map to store service names to node IDs
	serviceToNode := make(map[string]int64)
	nodeToService := make(map[int64]string)
	var nodeID int64
	for name := range dpuDeployment.Spec.Services {
		serviceToNode[name] = nodeID
		nodeToService[nodeID] = name
		if graph.Node(nodeID) == nil {
			// This is important because otherwise it panics on collision
			// when the same node ID is added twice. Should not happen in practice
			// but we want to be safe.
			graph.AddNode(simple.Node(nodeID))
		}
		nodeID++
	}

	for serviceName, config := range dpuDeployment.Spec.Services {
		if len(config.DependsOn) == 0 {
			continue
		}

		for _, dependency := range config.DependsOn {
			if _, exists := serviceToNode[dependency.Name]; !exists {
				return nil, fmt.Errorf("service %s depends on %s which is not defined in the DPUDeployment", serviceName, dependency.Name)
			}
			if dependency.Name == serviceName {
				return nil, fmt.Errorf("service %s cannot depend on itself", serviceName)
			}
			// Add edge from dependency to dependent service
			graph.SetEdge(graph.NewEdge(
				simple.Node(serviceToNode[dependency.Name]),
				simple.Node(serviceToNode[serviceName]),
			))
		}
	}
	// Perform topological sort to get services in dependency order
	// If the graph has cycles, topo.Sort will return an error
	sorted, err := topo.Sort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to sort Service: %w", err)
	}
	// Convert node IDs back to service names
	sortedNames := make([]string, len(sorted))
	for i, node := range sorted {
		sortedNames[i] = nodeToService[node.ID()]
	}
	return sortedNames, nil
}

// checkDPUServiceDependencies checks if the dependencies of a DPUService exist.
func checkDPUServiceDependencies(ctx context.Context, c client.Client, dependencies []dpuservicev1.LocalObjectDependency, dpuServices map[string]*dpuservicev1.DPUService) error {
	var errs []error
	for _, dependency := range dependencies {
		dependentService, exists := dpuServices[dependency.Name]
		if !exists {
			errs = append(errs, fmt.Errorf("DPUService %s not found in patched services", dependency.Name))
			continue
		}
		currentService := &dpuservicev1.DPUService{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(dependentService), currentService); err != nil {
			errs = append(errs, fmt.Errorf("failed to get DPUService %s: %w", dependency.Name, err))
			continue
		}
		// We do not check the status of the DPUService, we just check if it exists.
		// Checking for their readiness implies that the DPUService is already created and
		// deployed on a dpuset nodes. This implies a full reconciliation for a subset
		// of the DPUServices. This is not supported here.
	}
	return kerrors.NewAggregate(errs)
}

// TemplateDPUServiceConfigurationValuesParams are the values that are made available to the template
// for the DPUService configuration.
type TemplateDPUServiceConfigurationValuesParams struct {
	// Name of the generated DPUService.
	Name string

	// Namespace of the generated DPUService.
	Namespace string
}

// templateDPUServiceConfigurationValues templates the DPUServiceConfiguration helm chart values
// using the provided dependencies and the DPUServices map.
// It returns a runtime.RawExtension containing the templated values or an error if the templating fails.
func templateDPUServiceConfigurationValues(serviceConfig *dpuservicev1.DPUServiceConfiguration, dependencies []dpuservicev1.LocalObjectDependency, dpuServices map[string]*dpuservicev1.DPUService,
) (*runtime.RawExtension, error) {
	if serviceConfig.Spec.ServiceConfiguration.HelmChart.Values == nil {
		return nil, nil
	}
	// params is a map of template parameters that will be used to render the service configuration.
	params := make(map[string]map[string]TemplateDPUServiceConfigurationValuesParams)
	// the "Services" key will contain the DPUService parameters to be used in the template.
	params["Services"] = make(map[string]TemplateDPUServiceConfigurationValuesParams, len(dependencies))
	for _, dependency := range dependencies {
		if dpuService, exists := dpuServices[dependency.Name]; exists {
			// We use only the parameters of dependencies that exists.
			params["Services"][dependency.Name] = TemplateDPUServiceConfigurationValuesParams{
				Name:      dpuService.Name,
				Namespace: dpuService.Namespace,
			}
		} else {
			return nil, fmt.Errorf("DPUService %s not found in patched services", dependency.Name)
		}
	}
	// template the service configuration
	rendered, err := templates.Render(serviceConfig.Spec.ServiceConfiguration.HelmChart.Values, params, serviceConfig.Annotations)
	if err != nil {
		return nil, err
	}
	return rendered, nil
}
