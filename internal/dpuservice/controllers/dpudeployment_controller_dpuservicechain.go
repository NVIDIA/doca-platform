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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/pkg/conditions"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func reconcileDPUServiceChain(ctx context.Context,
	c client.Client,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dpuNodeLabels map[string]string) (ctrl.Result, bool, error) {
	owner := metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)
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

	// Grab existing DPUServiceChains
	existingDPUServiceChains := &dpuservicev1.DPUServiceChainList{}
	if err := c.List(ctx,
		existingDPUServiceChains,
		client.MatchingLabels{
			dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(dpuDeployment)),
		},
		client.InNamespace(dpuDeployment.Namespace)); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to list existing DPUServiceChains: %w", err)
	}

	existingDPUServiceChainsMap := make(map[string]dpuservicev1.DPUServiceChain)
	for _, dpuServiceChain := range existingDPUServiceChains.Items {
		existingDPUServiceChainsMap[dpuServiceChain.Name] = dpuServiceChain
	}

	if dpuDeployment.Spec.ServiceChains != nil {
		versionDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)
		newRevision := generateDPUServiceChain(client.ObjectKeyFromObject(dpuDeployment),
			owner,
			versionDigest,
			dpuDeployment.Spec.ServiceChains.Switches,
		)

		currentRevision, oldRevisions := getCurrentAndStaleDPUServiceChains(versionDigest, existingDPUServiceChains)
		switch {
		case currentRevision != nil:
			// we found the current revision based on the digest, there might be old revisions to handle
			isDisruptiveUpgradeOngoing := reconcileCurrentDPUServiceChainRevision(ctx, c, newRevision, currentRevision, oldRevisions, dpuNodeLabels, existingDPUServiceChainsMap, existingDPUSets.Items)

			// If we are in the middle of a disruptive upgrade, we need to enqueue until the upgrade is done
			if isDisruptiveUpgradeOngoing {
				requeue = ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}
			}

			// If we have an ongoing disruptive upgrade, we still apply disruptive changes in the cluster
			hasDisruptiveChanges = hasDisruptiveChanges || isDisruptiveUpgradeOngoing
		case len(oldRevisions) > 0:
			// we have only previous revisions
			req, isDisruptive := reconcileDPUServiceChainWithOldRevisions(newRevision, oldRevisions, dpuDeployment, dpuNodeLabels, existingDPUServiceChainsMap)
			if !req.IsZero() {
				requeue = req
			}
			// Mark if this was a disruptive change
			hasDisruptiveChanges = hasDisruptiveChanges || isDisruptive
		default:
			// no previous revision, we are creating a new one
			isDisruptive := reconcileNewDPUServiceChainRevision(newRevision, dpuDeployment, dpuNodeLabels)
			hasDisruptiveChanges = hasDisruptiveChanges || isDisruptive
		}

		newRevision.SetManagedFields(nil)
		newRevision.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
		if err := c.Patch(ctx, newRevision, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName)); err != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to patch DPUServiceChain %s: %w", client.ObjectKeyFromObject(newRevision), err)
		}
	}

	// Cleanup the remaining stale DPUServiceChains
	for _, dpuServiceChain := range existingDPUServiceChainsMap {
		if err := c.Delete(ctx, &dpuServiceChain); err != nil {
			return ctrl.Result{}, false, fmt.Errorf("failed to delete DPUServiceChain %s: %w", client.ObjectKeyFromObject(&dpuServiceChain), err)
		}
	}

	return requeue, hasDisruptiveChanges, nil
}

// reconcileNewDPUServiceChainRevision reconciles a new revision of the DPUServiceChain.
// It accepts a new DPUServiceChain object.
// It sets the nodeSelector for the DPUServiceChain and updates the DPUNodeLabels.
// Returns true if this is a disruptive service chain.
func reconcileNewDPUServiceChainRevision(newRevision *dpuservicev1.DPUServiceChain,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dpuNodeLabels map[string]string) bool {
	newRevision.SetServiceChainSetLabelSelector(newObjectLabelSelectorWithOwner(dpuServiceChainVersionLabelAnnotationKey, newRevision.Name, client.ObjectKeyFromObject(dpuDeployment)))

	// This is needed in all cases, because we don't know if a dpuSet will be updated or created
	setDPUServiceChainNodeLabelValue(newRevision.Name, dpuNodeLabels)

	// Return true if this is a disruptive service chain
	return dpuDeployment.Spec.ServiceChains.UpgradePolicy.ShouldApplyNodeEffect()
}

// reconcileDPUServiceChainWithOldRevisions reconciles a new revision of the DPUServiceChain and the old revisions.
// It accepts a new DPUServiceChain object and the old revisions of the DPUServiceChain.
// It sets the nodeSelector for the DPUServiceChain and updates the DPUNodeLabels.
// Returns true if a new version is created due to a disruptive upgrade.
func reconcileDPUServiceChainWithOldRevisions(newRevision *dpuservicev1.DPUServiceChain,
	oldRevisions []client.Object,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dpuNodeLabels map[string]string,
	existingDPUServiceChainsMap map[string]dpuservicev1.DPUServiceChain) (ctrl.Result, bool) {
	dpuServiceChainNodeSelector := newObjectLabelSelectorWithOwner(dpuServiceChainVersionLabelAnnotationKey, newRevision.Name, client.ObjectKeyFromObject(dpuDeployment))
	nodeLabelValue := newRevision.Name
	var toRetain []client.Object
	isDisruptive := false
	if dpuDeployment.Spec.ServiceChains.UpgradePolicy.ShouldApplyNodeEffect() {
		// Mark this as a disruptive change
		isDisruptive = true
		toRetain = getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit-1)
		newRevision.SetServiceChainSetLabelSelector(dpuServiceChainNodeSelector)
	} else {
		// just patch the youngest existing service
		toRetain = getRevisionHistoryLimitList(oldRevisions, *dpuDeployment.Spec.RevisionHistoryLimit)
		oldSvc := toRetain[0].(*dpuservicev1.DPUServiceChain)
		newRevision.Name = oldSvc.Name
		// we are not creating a new serviceChain, we are updating the existing one
		// so we need to keep the same `version` and `owned-by-dpudeployment` selector to avoid downtime
		newRevision.SetServiceChainSetLabelSelector(&metav1.LabelSelector{
			MatchExpressions: oldSvc.Spec.Template.Spec.NodeSelector.MatchExpressions,
		})
		nodeLabelValue = getLabelSelectorDPUServiceChainVersionValue(oldSvc.Spec.Template.Spec.NodeSelector)
	}

	for _, svcChain := range toRetain {
		delete(existingDPUServiceChainsMap, svcChain.GetName())
	}

	// This is needed in all cases, because we don't know if a dpuSet will be updated or created
	setDPUServiceChainNodeLabelValue(nodeLabelValue, dpuNodeLabels)

	return ctrl.Result{RequeueAfter: time.Duration(reconcileRequeueDuration.Load())}, isDisruptive
}

// reconcileCurrentDPUServiceChainRevision reconciles the current revision of the DPUServiceChain and the old revisions.
// It accepts the current DPUServiceChain objec and  the old revisions of the DPUServiceChain.
// It cleans the old revisions and updates the DPUNodeLabels.
func reconcileCurrentDPUServiceChainRevision(ctx context.Context, c client.Client,
	newRevision *dpuservicev1.DPUServiceChain,
	currentRevision client.Object,
	oldRevisions []client.Object,
	dpuNodeLabels map[string]string,
	existingDPUServiceChainsMap map[string]dpuservicev1.DPUServiceChain,
	existingDPUSets []provisioningv1.DPUSet,
) bool {
	log := ctrllog.FromContext(ctx)
	currentRev, oldRevs := currentRevision.(*dpuservicev1.DPUServiceChain), clientObjectToDPUServiceChainList(oldRevisions)
	isDisruptiveUpgradeOngoing := false

	// We update the name of the newRevision to the currentRevision so that we can patch the existing object
	newRevision.Name = currentRev.Name

	// We update the newRevision nodeSelector to match the currentRevision nodeSelector since it relies on the revision name
	newRevision.SetServiceChainSetLabelSelector(&metav1.LabelSelector{
		MatchExpressions: currentRev.Spec.Template.Spec.NodeSelector.MatchExpressions,
	})

	// We delete the current revision so that it doesn't get cleaned up
	delete(existingDPUServiceChainsMap, currentRev.GetName())

	// This is needed because we don't know if a dpuSet will be updated or created
	setDPUServiceChainNodeLabelValue(getLabelSelectorDPUServiceChainVersionValue(currentRev.Spec.Template.Spec.NodeSelector), dpuNodeLabels)

	// If there are no old revisions, it means that we are not in the middle of a disruptive upgrade operation, so there
	// are no leftovers to handle
	if len(oldRevs) == 0 {
		return isDisruptiveUpgradeOngoing
	}

	// if the current revision is still not ready, keep the eventual old revisions and requeue
	// otherwise, clean old revisions
	if conditions.IsTrue(currentRev, conditions.TypeReady) && len(getNotReadyDPUSets(existingDPUSets)) == 0 {
		err := cleanStaleDPUServiceChains(ctx, c, oldRevs)
		if err != nil {
			log.Error(err, "failed to delete stale DPUServiceChains")
		}
		// If we reached that stage, it means that we have completed the upgrade and now we just need to clean any
		// leftovers. We expect additional reconcilliations to be triggered for leftovers that are getting deleted.
		isDisruptiveUpgradeOngoing = false
	} else {
		// If we found old revisions and either the DPUServiceChain or the DPUSets are not ready, it means that an
		// upgrade is still ongoing
		isDisruptiveUpgradeOngoing = true
	}

	for _, svcChain := range oldRevs {
		delete(existingDPUServiceChainsMap, svcChain.Name)
	}

	return isDisruptiveUpgradeOngoing
}

// setDPUServiceChainNodeLabelValue sets the value of the DPUServiceChain version label.
func setDPUServiceChainNodeLabelValue(value string, nodeLabels map[string]string) {
	nodeLabels[dpuServiceChainVersionLabelAnnotationKey] = value
}

// getCurrentAndStaleDPUServiceChains returns the current and stale DPUServiceChain objects.
func getCurrentAndStaleDPUServiceChains(currentVersionDigest string, existingDPUServiceChains *dpuservicev1.DPUServiceChainList) (client.Object, []client.Object) {
	match := []client.Object{}
	var current client.Object
	for _, dpuServiceChain := range existingDPUServiceChains.Items {
		if dpuServiceChain.Annotations[dpuServiceChainVersionLabelAnnotationKey] == currentVersionDigest {
			current = &dpuServiceChain
		} else {
			match = append(match, &dpuServiceChain)
		}
	}
	return current, match
}

// getLabelSelectorDPUServiceChainVersionValue returns the NodeSelectorTerm for the given DPUService version label key.
func getLabelSelectorDPUServiceChainVersionValue(labelSelector *metav1.LabelSelector) string {
	var version string
	for _, req := range labelSelector.MatchExpressions {
		if req.Key == dpuServiceChainVersionLabelAnnotationKey {
			version = req.Values[0]
			break
		}
	}
	return version
}

// generateDPUServiceChain generates a DPUServiceChain according to the DPUDeployment.
func generateDPUServiceChain(dpuDeploymentNamespacedName types.NamespacedName, owner *metav1.OwnerReference, versionDigest string, switches []dpuservicev1.DPUDeploymentSwitch) *dpuservicev1.DPUServiceChain {
	sw := make([]dpuservicev1.Switch, 0, len(switches))

	for _, s := range switches {
		sw = append(sw, convertToSFCSwitch(dpuDeploymentNamespacedName, s))
	}

	dpuServiceChain := &dpuservicev1.DPUServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", dpuDeploymentNamespacedName.Name, utilrand.String(resourceNameGeneratedSuffixLength)),
			Namespace: dpuDeploymentNamespacedName.Namespace,
			// we save the version in the annotations, as this will permit us to retrieve
			// a dpuServiceChain by its version
			Annotations: map[string]string{
				dpuServiceChainVersionLabelAnnotationKey: versionDigest,
			},
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: getParentDPUDeploymentLabelValue(dpuDeploymentNamespacedName),
			},
		},
		Spec: dpuservicev1.DPUServiceChainSpec{
			// TODO: Derive and add cluster selector
			Template: dpuservicev1.ServiceChainSetSpecTemplate{
				Spec: dpuservicev1.ServiceChainSetSpec{
					Template: dpuservicev1.ServiceChainSpecTemplate{
						Spec: dpuservicev1.ServiceChainSpec{
							Switches: sw,
						},
					},
				},
			},
		},
	}
	dpuServiceChain.SetOwnerReferences([]metav1.OwnerReference{*owner})

	return dpuServiceChain
}

func cleanStaleDPUServiceChains(ctx context.Context, c client.Client, oldObjects []dpuservicev1.DPUServiceChain) error {
	for _, obj := range oldObjects {
		if err := c.Delete(ctx, &obj); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("error while deleting %s %s: %w", obj.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(&obj), err)
			}
		}
	}
	return nil
}

// convertToSFCSwitch converts a dpuservicev1.DPUDeploymentSwitch to a dpuservicev1.DPUDeploymentSwitch.
func convertToSFCSwitch(dpuDeploymentNamespacedName types.NamespacedName, sw dpuservicev1.DPUDeploymentSwitch) dpuservicev1.Switch {
	o := dpuservicev1.Switch{
		Ports:      make([]dpuservicev1.Port, 0, len(sw.Ports)),
		ServiceMTU: sw.ServiceMTU,
	}

	for _, inPort := range sw.Ports {
		outPort := dpuservicev1.Port{}

		if inPort.Service != nil {
			// construct ServiceIfc that references serviceInterface for the service
			if inPort.Service.IPAM != nil {
				outPort.ServiceInterface.IPAM = inPort.Service.IPAM.DeepCopy()
			}
			outPort.ServiceInterface.MatchLabels = map[string]string{
				dpuservicev1.DPFServiceIDLabelKey:  getServiceID(dpuDeploymentNamespacedName, inPort.Service.Name),
				ServiceInterfaceInterfaceNameLabel: inPort.Service.InterfaceName,
			}
		}

		if inPort.ServiceInterface != nil {
			outPort.ServiceInterface = *inPort.ServiceInterface.DeepCopy()
		}

		o.Ports = append(o.Ports, outPort)

	}
	return o
}

func clientObjectToDPUServiceChainList(oldRevisions []client.Object) []dpuservicev1.DPUServiceChain {
	oldRevs := make([]dpuservicev1.DPUServiceChain, 0, len(oldRevisions))
	for _, svc := range oldRevisions {
		oldRevs = append(oldRevs, *svc.(*dpuservicev1.DPUServiceChain))
	}
	return oldRevs
}

func calculateDPUServiceChainVersionDigest(switches []dpuservicev1.DPUDeploymentSwitch) string {
	return digest.Short(digest.FromObjects(switches), 10)
}
