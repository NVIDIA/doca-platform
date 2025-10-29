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
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// interfaceNameToDPUServiceInterfaceName is a map that has as keys the interface names as defined in the
// DPUServiceConfiguration and as value the DPUServiceInterface name associated with that interface. This type is meant
// to ease readability of the output of constructDPUServiceInterfaceNames() and its consumers.
type interfaceNameToDPUServiceInterfaceName map[string]string

// reconcileDPUServiceInterfaces reconciles the DPUServiceInterfaces created by the DPUDeployment.
func reconcileDPUServiceInterfaces(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dependencies *dpuDeploymentDependencies,
	interfaceNameByServiceName map[string]interfaceNameToDPUServiceInterfaceName,
	dpuNodeLabels map[string]string,
	patchedDPUServices map[string]*dpuservicev1.DPUService,
) (ctrl.Result, bool, error) {
	dpuDeploymentOwnerRef := metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)
	requeue := ctrl.Result{}
	hasDisruptiveChanges := false

	// Get all existing DPUSets
	existingDPUSets := &provisioningv1.DPUSetList{}
	if err := c.List(ctx,
		existingDPUSets,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to list existing DPUSets: %w", err)
	}

	// Get all the existing DPUServices
	existingDPUServices := &dpuservicev1.DPUServiceList{}
	if err := c.List(ctx,
		existingDPUServices,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to list existing DPUService: %w", err)
	}
	mergedDPUServices := mergePatchedAndListedDPUServices(existingDPUServices, patchedDPUServices)

	// Get all the existing DPUServiceInterfaces
	existingDPUServiceInterfaces := &dpuservicev1.DPUServiceInterfaceList{}
	if err := c.List(ctx,
		existingDPUServiceInterfaces,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to list existing DPUServiceInterfaces: %w", err)
	}

	existingDPUServiceInterfacesMap := make(map[string]dpuservicev1.DPUServiceInterface)
	for _, dpuServiceInterface := range existingDPUServiceInterfaces.Items {
		existingDPUServiceInterfacesMap[dpuServiceInterface.Name] = dpuServiceInterface
	}

	var errs []error
	// Create or update DPUServiceInterfaces to match what is defined in the DPUDeployment.
	for serviceName := range dpuDeployment.Spec.Services {
		serviceConfig := dependencies.DPUServiceConfigurations[serviceName]
		serviceTemplate := dependencies.DPUServiceTemplates[serviceName]
		versionDigest := calculateDPUServiceVersionDigest(serviceConfig, serviceTemplate)

		currentDPUServiceClientObj, _ := getCurrentAndStaleDPUServices(serviceName, versionDigest, mergedDPUServices)
		// One case where this can happen is if the DPUService was manually deleted
		if currentDPUServiceClientObj == nil {
			errs = append(errs, fmt.Errorf("failed to find current DPUService for service '%s' as defined in DPUDeployment", serviceName))
			continue
		}
		currentDPUService := currentDPUServiceClientObj.(*dpuservicev1.DPUService)
		dpuServiceLabelKey := getDPUServiceVersionLabelKey(serviceName)
		currentDPUServiceNodeSelector := newObjectLabelSelectorWithOwner(dpuServiceLabelKey, dpuNodeLabels[dpuServiceLabelKey], client.ObjectKeyFromObject(dpuDeployment))
		dpuServiceOwnerRef := metav1.NewControllerRef(currentDPUService, dpuservicev1.DPUServiceGroupVersionKind)
		dpuServiceOwnerRef.Controller = ptr.To(false)
		dpuServiceOwnerRef.BlockOwnerDeletion = ptr.To(false)

		for _, serviceInterface := range dependencies.DPUServiceConfigurations[serviceName].Spec.Interfaces {
			newRevision := generateDPUServiceInterface(
				interfaceNameByServiceName[serviceName][serviceInterface.Name],
				client.ObjectKeyFromObject(dpuDeployment),
				[]metav1.OwnerReference{*dpuDeploymentOwnerRef, *dpuServiceOwnerRef},
				serviceName,
				serviceInterface,
				// We use the version digest of the DPUService in order to ensure that both the DPUService and
				// DPUServiceInterface changes are synced.
				versionDigest,
			)

			// filter the existing DPUServiceInterfaces by name and extract the most current one, if any
			currentRevision, oldRevisions := getCurrentAndStaleDPUServiceInterfaces(serviceInterface.Name, serviceName, versionDigest, existingDPUServiceInterfaces)
			switch {
			case currentRevision != nil:
				// we found the current revision based on the digest, there might be old revisions to handle
				req := reconcileCurrentDPUServiceInterfaceRevision(ctx, c, newRevision, currentRevision, currentDPUService, oldRevisions, serviceConfig, existingDPUServiceInterfacesMap, currentDPUServiceNodeSelector, existingDPUSets.Items)

				if !req.IsZero() {
					requeue = req
				}
				// No new version created, so not disruptive in this reconciliation
			case len(oldRevisions) > 0:
				// we have only previous revisions
				req, isDisruptive := reconcileDPUServiceInterfaceWithOldRevisions(newRevision, oldRevisions, dpuDeployment, serviceConfig, existingDPUServiceInterfacesMap, currentDPUServiceNodeSelector)
				if !req.IsZero() {
					requeue = req
				}
				// Mark if this was a disruptive change
				hasDisruptiveChanges = hasDisruptiveChanges || isDisruptive
			default:
				// no previous version, we are creating a new one
				isDisruptive := reconcileNewDPUServiceInterfaceRevision(newRevision, serviceConfig, currentDPUServiceNodeSelector)
				hasDisruptiveChanges = hasDisruptiveChanges || isDisruptive
			}

			newRevision.SetManagedFields(nil)
			newRevision.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
			if err := c.Patch(ctx, newRevision, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
				errs = append(errs, fmt.Errorf("failed to patch DPUServiceInterface %s: %w", client.ObjectKeyFromObject(newRevision), err))
			}
		}
	}

	// If we have found errors, we don't want to delete any of the existing DPUServiceInterfaces as we might have partially
	// applied the logic of modifying the existingDPUServicesMap.
	if len(errs) > 0 {
		return ctrl.Result{}, false, kerrors.NewAggregate(errs)
	}

	// We don't want to remove stale DPUServiceInterfaces that still have their DPUService around to ensure that they
	// can still function. The reconcileDPUService function takes care of the revision history so we expect that the
	// DPUService that exceeds the history, will be deleted, meaning that the DPUServiceInterfaces that rely on that one,
	// will be removed as well in this function.
	// In addition, when changing the interface name as defined in the DPUServiceConfiguration, the
	// getCurrentAndStaleDPUServiceInterfaces will only return the interfaces with new name since the matcher relies on the
	// name of the interface as defined in the DPUServiceConfiguration. Therefore, in that case, we do an indirect cleanup
	// by checking whether the parent DPUService exists or not, completely relying on the cleanup logic defined in the
	// reconcileDPUService function.
	for _, dpuServiceInterface := range existingDPUServiceInterfacesMap {
		for _, dpuService := range mergedDPUServices.Items {
			isOwner, err := controllerutil.HasOwnerReference(dpuServiceInterface.OwnerReferences, &dpuService, scheme)
			if err != nil {
				return ctrl.Result{}, false, fmt.Errorf("couldn't match owner reference: %w", err)
			}
			// If a service is paused, it should have been as part of the dpuService reconciliation process. This
			// means that it's no longer the most current one, and will be cleaned up after the latest revision is ready.
			// Therefore the interfaces it owns should be preserved until clean up is done.
			if dpuService.IsPaused() && isOwner {
				delete(existingDPUServiceInterfacesMap, dpuServiceInterface.Name)
				requeue = ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}
			}
		}
	}

	// Cleanup the remaining stale DPUServiceInterfaces
	for _, dpuServiceInterface := range existingDPUServiceInterfacesMap {
		if err := c.Delete(ctx, &dpuServiceInterface); err != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to delete DPUServiceInterface %s: %w", client.ObjectKeyFromObject(&dpuServiceInterface), err)
		}
	}

	return requeue, hasDisruptiveChanges, nil
}

// reconcileNewDPUServiceInterfaceRevision reconciles a new revision of the DPUServiceInterface.
// It accepts a new DPUServiceInterface object, the DPUService name as defined in the DPUDeployment and the name of the
// current DPUService.
// It sets the nodeSelector to the one matching the current DPUService in order for the DPUServiceInterface to match
// the same nodes as the current DPUService.
// Returns true if this is a disruptive service interface.
func reconcileNewDPUServiceInterfaceRevision(newRevision *dpuservicev1.DPUServiceInterface,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	currentDPUServiceNodeSelector *metav1.LabelSelector,
) bool {
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		newRevision.SetServiceInterfaceSetLabelSelector(currentDPUServiceNodeSelector)
	}
	// Return true if this is a disruptive service interface
	return serviceConfig.Spec.UpgradePolicy.ShouldApplyNodeEffect()
}

// reconcileDPUServiceInterfaceWithOldRevisions reconciles a new revision of the DPUServiceInterface and the old revisions.
// It accepts a new DPUServiceInterface object, the DPUService name as defined in the DPUDeployment and the old revisions.
// It sets the nodeSelector to the one matching the current DPUService in order for the DPUServiceInterface to match
// the same nodes as the current DPUService.
// Returns true if a new version is created due to a disruptive upgrade.
func reconcileDPUServiceInterfaceWithOldRevisions(newRevision *dpuservicev1.DPUServiceInterface,
	oldRevisions []client.Object,
	dpuDeployment *dpuservicev1.DPUDeployment,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	existingDPUServiceInterfacesMap map[string]dpuservicev1.DPUServiceInterface,
	currentDPUServiceNodeSelector *metav1.LabelSelector,
) (ctrl.Result, bool) {
	var toRetain []client.Object
	isDisruptive := false
	if serviceConfig.Spec.UpgradePolicy.ShouldApplyNodeEffect() {
		// Mark this as a disruptive change
		isDisruptive = true
		toRetain = getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit-1)
		if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
			newRevision.SetServiceInterfaceSetLabelSelector(currentDPUServiceNodeSelector)
		}
	} else {
		// Patch the youngest existing service
		toRetain = getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit)
		oldDPUServiceInterface := toRetain[0].(*dpuservicev1.DPUServiceInterface)
		newRevision.Name = oldDPUServiceInterface.Name
		if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
			// we are not creating a new DPUServiceInterface, we are updating the existing one
			// so we need to keep the same `version` and `owned-by-dpudeployment` selector to avoid downtime
			newRevision.SetServiceInterfaceSetLabelSelector(&metav1.LabelSelector{
				MatchExpressions: oldDPUServiceInterface.Spec.Template.Spec.NodeSelector.MatchExpressions,
			})
		}
	}

	for _, svcInterface := range toRetain {
		delete(existingDPUServiceInterfacesMap, svcInterface.GetName())
	}

	return ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}, isDisruptive
}

// reconcileCurrentDPUServiceInterfaceRevision reconciles the current revision of the DPUServiceInterface and the old
// revisions.
// It accepts the current DPUServiceInterface object and the old revisions of the DPUServiceInterface.
// It cleans the old revisions if the current object is ready
func reconcileCurrentDPUServiceInterfaceRevision(ctx context.Context,
	c client.Client,
	newRevision *dpuservicev1.DPUServiceInterface,
	currentRevision client.Object,
	currentDPUService *dpuservicev1.DPUService,
	oldRevisions []client.Object,
	serviceConfig *dpuservicev1.DPUServiceConfiguration,
	existingDPUServiceInterfacesMap map[string]dpuservicev1.DPUServiceInterface,
	currentDPUServiceNodeSelector *metav1.LabelSelector,
	existingDPUSets []provisioningv1.DPUSet,
) ctrl.Result {
	log := ctrl.LoggerFrom(ctx)
	requeue := ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}
	oldRevs := clientObjectToDPUServiceInterfaceList(oldRevisions)

	// We update the name of the newRevision to the currentRevision so that we can patch the existing object
	newRevision.Name = currentRevision.GetName()

	// We update the newRevision nodeSelector to match the currentRevision nodeSelector since it relies on the revision name
	if !serviceConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
		newRevision.SetServiceInterfaceSetLabelSelector(currentDPUServiceNodeSelector)
	}

	// We delete the current revision so that it doesn't get cleaned up
	delete(existingDPUServiceInterfacesMap, currentRevision.GetName())

	// If there are no old revisions, we don't need to enqueue as there are no old revisions that need cleanup.
	if len(oldRevs) == 0 {
		return ctrl.Result{}
	}

	// if the current revision is ready, clean old revisions. If not, the stale entries won't be removed in the caller
	// function unless they have their DPUService deleted.
	if conditions.IsTrue(currentDPUService, conditions.TypeReady) && len(getNotReadyDPUSets(existingDPUSets)) == 0 {
		err := cleanStaleDPUServiceInterfaces(ctx, c, oldRevs)
		if err != nil {
			log.Error(err, "failed to clean stale DPUServiceInterfaces")
		}
		// don't requeue if the current revision is ready
		return ctrl.Result{}
	}

	return requeue
}

// getCurrentAndStaleDPUServiceInterfaces returns the current and stale DPUServiceInterface objects filtered by the name
// of the interface as defined in the DPUServiceConfiguration.
func getCurrentAndStaleDPUServiceInterfaces(serviceInterfaceName string, serviceName string, currentVersionDigest string, existingDPUServiceInterfaces *dpuservicev1.DPUServiceInterfaceList) (client.Object, []client.Object) {
	match := []client.Object{}
	var current client.Object
	for _, dpuServiceInterface := range existingDPUServiceInterfaces.Items {
		if strings.HasPrefix(dpuServiceInterface.Name, fmt.Sprintf("%s-%s", serviceName, strings.ReplaceAll(serviceInterfaceName, "_", "-"))) {
			if dpuServiceInterface.Annotations[dpuServiceVersionAnnotationKey] == currentVersionDigest {
				current = &dpuServiceInterface
			} else {
				match = append(match, &dpuServiceInterface)
			}
		}
	}
	return current, match
}

// generateDPUServiceInterface generates a DPUServiceInterface according to the DPUServiceConfiguration.
func generateDPUServiceInterface(name string,
	dpuDeploymentNamespacedName types.NamespacedName,
	owners []metav1.OwnerReference,
	dpuServiceName string,
	serviceInterface dpuservicev1.ServiceInterfaceTemplate,
	dpuServiceVersionDigest string,
) *dpuservicev1.DPUServiceInterface {
	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpuDeploymentNamespacedName.Namespace,
			// we save the DPUService version in the annotations, to match a given dpuService revision to a
			// corresponding DPUServiceInterface. This will permit retrieving the current version.
			Annotations: map[string]string{
				dpuServiceVersionAnnotationKey: dpuServiceVersionDigest,
			},
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName),
			},
		},
		Spec: dpuservicev1.DPUServiceInterfaceSpec{
			Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
				Spec: dpuservicev1.ServiceInterfaceSetSpec{
					Template: dpuservicev1.ServiceInterfaceSpecTemplate{
						ObjectMeta: dpuservicev1.ObjectMeta{
							Labels: map[string]string{
								dpuservicev1.DPFServiceIDLabelKey:  getServiceID(dpuDeploymentNamespacedName, dpuServiceName),
								ServiceInterfaceInterfaceNameLabel: serviceInterface.Name,
							},
						},
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeService,
							Service: &dpuservicev1.ServiceDef{
								ServiceID:      getServiceID(dpuDeploymentNamespacedName, dpuServiceName),
								Network:        serviceInterface.Network,
								InterfaceName:  serviceInterface.Name,
								VirtualNetwork: serviceInterface.VirtualNetwork,
							},
						},
					},
				},
			},
		},
	}

	dpuServiceInterface.SetOwnerReferences(owners)

	return dpuServiceInterface
}

// cleanStaleDPUServiceInterfaces deletes all the stale DPUServiceInterface objects that are not needed anymore.
func cleanStaleDPUServiceInterfaces(ctx context.Context, c client.Client, oldRevisions []dpuservicev1.DPUServiceInterface) error {
	for _, dpuServiceInterface := range oldRevisions {
		if err := c.Delete(ctx, &dpuServiceInterface); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("error while deleting %s %s: %w", dpuServiceInterface.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&dpuServiceInterface), err)
			}
		}
	}
	return nil
}

func clientObjectToDPUServiceInterfaceList(oldRevisions []client.Object) []dpuservicev1.DPUServiceInterface {
	oldRevs := make([]dpuservicev1.DPUServiceInterface, 0, len(oldRevisions))
	for _, svcInterface := range oldRevisions {
		oldRevs = append(oldRevs, *svcInterface.(*dpuservicev1.DPUServiceInterface))
	}
	return oldRevs
}

// constructDPUServiceInterfaceNames constructs a map that contains the DPUServiceInterface name associated with a service
// interface as defined in the DPUServiceConfiguration, by service name.
func constructDPUServiceInterfaceNames(ctx context.Context,
	c client.Client,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dependencies *dpuDeploymentDependencies,
) (map[string]interfaceNameToDPUServiceInterfaceName, error) {

	// Get all the existing DPUServiceInterfaces.
	existingDPUServiceInterfaces := &dpuservicev1.DPUServiceInterfaceList{}
	if err := c.List(ctx,
		existingDPUServiceInterfaces,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list existing DPUServiceInterfaces: %w", err)
	}

	interfaceNameByServiceName := make(map[string]interfaceNameToDPUServiceInterfaceName)
	// Create or update DPUServices to match what is defined in the DPUDeployment.
	for dpuServiceName := range dpuDeployment.Spec.Services {
		serviceConfig := dependencies.DPUServiceConfigurations[dpuServiceName]
		serviceTemplate := dependencies.DPUServiceTemplates[dpuServiceName]
		versionDigest := calculateDPUServiceVersionDigest(serviceConfig, serviceTemplate)
		interfaceNameByServiceName[dpuServiceName] = constructCurrentDPUServiceInterfaceNamesForService(dpuDeployment,
			dpuServiceName,
			dependencies,
			versionDigest,
			existingDPUServiceInterfaces)
	}
	return interfaceNameByServiceName, nil
}

// constructCurrentDPUServiceInterfaceNamesForService constructs a map that has as key the name of the service interface
// as defined in the DPUServiceConfiguration and as value the actual name of the current DPUServiceInterface to be
// consumed by the current DPUService.
// This function must simulate the update logic we apply for the DPUServiceInterface resources which is:
// * use existing resource if non disruptive upgrade, if not exists, create
// * use new resource if disruptive upgrade
func constructCurrentDPUServiceInterfaceNamesForService(dpuDeployment *dpuservicev1.DPUDeployment,
	dpuServiceName string,
	dependencies *dpuDeploymentDependencies,
	dpuServiceVersionDigest string,
	existingDPUServiceInterfaces *dpuservicev1.DPUServiceInterfaceList) interfaceNameToDPUServiceInterfaceName {

	serviceConfig := dependencies.DPUServiceConfigurations[dpuServiceName]
	interfacesFromDependencies := serviceConfig.Spec.Interfaces

	if len(interfacesFromDependencies) == 0 {
		return nil
	}

	m := make(map[string]string)
	for _, serviceInterface := range interfacesFromDependencies {
		generatedServiceInterfaceName := fmt.Sprintf("%s-%s-%s", dpuServiceName, strings.ReplaceAll(serviceInterface.Name, "_", "-"), utilrand.String(resourceNameGeneratedSuffixLength))
		// filter the existing DPUServiceInterfaces by name and extract the most current one, if any
		currentRevision, oldRevisions := getCurrentAndStaleDPUServiceInterfaces(serviceInterface.Name, dpuServiceName, dpuServiceVersionDigest, existingDPUServiceInterfaces)
		switch {
		case currentRevision != nil:
			// found current, we use its name
			m[serviceInterface.Name] = currentRevision.GetName()
		case len(oldRevisions) > 0:
			// If disruptive, use a newly generated name
			if serviceConfig.Spec.UpgradePolicy.ShouldApplyNodeEffect() {
				m[serviceInterface.Name] = generatedServiceInterfaceName
				continue
			}

			// If not disruptive, use the name of the latest stale dpuserviceinterface
			toRetain := getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit)
			m[serviceInterface.Name] = toRetain[0].GetName()
		default:
			// if no dpuserviceinterface is found at all, use a newly generated one
			m[serviceInterface.Name] = generatedServiceInterfaceName
		}
	}
	return m
}

// mergePatchedAndListedDPUServices merges the DPUServices that were just created/updated with the ones listed from cache.
// This is needed because the cache may not have synced yet with the latest changes when reconciling DPUServiceInterfaces.
// Patched services always take precedence over cached services.
func mergePatchedAndListedDPUServices(
	existingDPUServices *dpuservicev1.DPUServiceList,
	patchedDPUServices map[string]*dpuservicev1.DPUService,
) *dpuservicev1.DPUServiceList {
	merged := existingDPUServices.DeepCopy()

	for _, patched := range patchedDPUServices {
		found := false
		for i := range merged.Items {
			if merged.Items[i].Name == patched.Name {
				// Replace the cached version with the patched version
				merged.Items[i] = *patched
				found = true
				break
			}
		}
		if !found {
			// Service doesn't exist in cache yet, add it
			merged.Items = append(merged.Items, *patched)
		}
	}

	return merged
}
