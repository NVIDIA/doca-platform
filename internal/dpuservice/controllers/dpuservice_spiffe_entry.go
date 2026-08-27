/*
Copyright 2026 NVIDIA

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
	"sort"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/spire"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	spirev1alpha1 "github.com/nvidia/doca-platform/third_party/forked/github.com/spiffe/spire-controller-manager/api/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Labels the entry for operators reading SPIRE server state.
	dpuServiceSpiffeEntryHint = "dpu-service"

	// How often to re-check entries that are rendering or being deleted.
	spiffeEntryProgressInterval = 10 * time.Second
)

// clusterStaticEntryGK identifies the optional upstream CRD in a NoKindMatchError.
var clusterStaticEntryGK = spirev1alpha1.SchemeGroupVersion.WithKind("ClusterStaticEntry").GroupKind()

// crdAbsent reports whether spire-controller-manager's CRD is absent, so no entry can
// exist. That is the normal state on non-SPIFFE clusters, hence not an error.
func crdAbsent(err error) bool {
	return meta.IsNoMatchError(err)
}

// entryGone reports whether an entry is already absent, either because the CRD is not
// installed or because the object itself is gone. Only meaningful where a missing entry is
// the outcome being asked for, i.e. deletion.
func entryGone(err error) bool {
	return crdAbsent(err) || apierrors.IsNotFound(err)
}

// uncachedReader returns the reader used to confirm a ClusterStaticEntry is really gone. The
// cache cannot answer that: it still serves an object the delete just removed, and it may not yet
// carry one created moments ago, either of which would release the deregistration finalizer while
// the SPIRE registration still exists.
//
// It falls back to the cached client so a reconciler built without UncachedClient degrades to a
// weaker confirmation instead of panicking. SetupWithManager rejects that for the real
// controller, so only tests that never exercise this path can take the fallback.
func (r *DPUServiceReconciler) uncachedReader() client.Reader {
	if r.UncachedClient != nil {
		return r.UncachedClient
	}
	return r.Client
}

func dpuServiceSpiffeEnabled(dpuService *dpuservicev1.DPUService) bool {
	return dpuService.Spec.Security != nil && dpuService.Spec.Security.SPIFFE != nil
}

// spiffeEntriesProgressing reports whether SPIFFEEntriesReady is waiting on something that
// resolves on its own, i.e. spire-controller-manager rendering an entry or a delete
// completing. The other False reasons need an operator, so they are not worth polling.
//
// Deliberately not gated on the DPUService still opting in: opting back out leaves entries
// awaiting deletion, and those need the same re-reconcile.
func spiffeEntriesProgressing(dpuService *dpuservicev1.DPUService) bool {
	condition := conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady)
	if condition == nil || condition.Status != metav1.ConditionFalse {
		return false
	}
	return condition.Reason == string(conditions.ReasonPending) || condition.Reason == string(conditions.ReasonAwaitingDeletion)
}

// spiffeEntryOwnerLabels selects the ClusterStaticEntries owned by one DPUService. The CR
// is cluster-scoped and cannot own-reference a namespaced DPUService, so ownership is by
// label and this controller does its own garbage collection.
func spiffeEntryOwnerLabels(dpuService *dpuservicev1.DPUService) client.MatchingLabels {
	return client.MatchingLabels{
		dpuservicev1.DPUServiceNameLabelKey:      dpuService.Name,
		dpuservicev1.DPUServiceNamespaceLabelKey: dpuService.Namespace,
	}
}

// spiffeEntryTarget is one desired (DPUService, DPU) SPIRE registration.
type spiffeEntryTarget struct {
	name     string
	spiffeID string
	parentID string
	// dpu identifies the DPU this entry is parented to, for diagnostics.
	dpu string
}

// reconcileSPIFFEEntries syncs the per-DPU ClusterStaticEntries for this DPUService with the
// SPIFFE-mode DPUs it targets and reports the result on SPIFFEEntriesReady.
//
// It deletes every label-owned entry outside the desired set, so one path covers opt-out at
// either level, DPUCluster deselect or delete, and DPU decommission. Safe to call on every
// reconcile, including for DPUServices that never opt in.
func (r *DPUServiceReconciler) reconcileSPIFFEEntries(ctx context.Context, dpuService *dpuservicev1.DPUService, dpuClusterConfigs []*dpucluster.Config, dpfOperatorConfig *operatorv1.DPFOperatorConfig) error {
	targets, invalidDPUs, err := r.desiredSPIFFEEntries(ctx, dpuService, dpuClusterConfigs, dpfOperatorConfig)
	if err != nil {
		return err
	}

	// Take the finalizer BEFORE creating any entry, so a crash cannot leak a SPIRE
	// registration nothing holds a finalizer for. The caller's deferred patcher persists it
	// and that write re-triggers reconcile.
	if len(targets) > 0 && !controllerutil.ContainsFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer) {
		controllerutil.AddFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)
		conditions.AddFalse(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady,
			conditions.ReasonPending, conditions.ConditionMessage("Awaiting the SPIFFE deregistration finalizer to be persisted"))
		return nil
	}

	desired := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		desired[target.name] = struct{}{}
	}

	// Delete before create, so a DPUService retargeted between DPUs never has both live.
	staleRemaining, err := r.deleteUnwantedSPIFFEEntries(ctx, dpuService, desired)
	if err != nil {
		return err
	}

	// Apply all entries: one broken DPU must not withhold identities from the others.
	var errs []error
	var pending, masked []string
	var crdMissing bool
	for _, target := range targets {
		entryMasked, entryReady, entryCRDMissing, err := r.applySPIFFEEntry(ctx, dpuService, target, dpfOperatorConfig)
		switch {
		case err != nil:
			errs = append(errs, err)
		case entryCRDMissing:
			crdMissing = true
		case entryMasked:
			masked = append(masked, target.name)
		case !entryReady:
			pending = append(pending, target.name)
		}
	}
	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}

	if len(targets) == 0 && len(staleRemaining) == 0 {
		controllerutil.RemoveFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)
	}

	switch {
	case len(invalidDPUs) > 0:
		// First: the only state needing an operator to recreate an object.
		conditions.AddFalse(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady, conditions.ReasonFailure,
			spiffeEntriesMessage("The following DPUs cannot form a valid SPIFFE identity for this DPUService", invalidDPUs))
	case len(masked) > 0:
		// Shadowed by another entry and never issued until an operator removes it.
		conditions.AddFalse(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady, conditions.ReasonFailure,
			spiffeEntriesMessage("The following ClusterStaticEntries are masked by another entry", masked))
	case len(staleRemaining) > 0:
		conditions.AddFalse(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady, conditions.ReasonAwaitingDeletion,
			spiffeEntriesMessage("The following stale ClusterStaticEntries are awaiting deletion", staleRemaining))
	case crdMissing:
		// Nothing can be rendered while the CRD is absent, so say that rather than blame
		// spire-controller-manager for not rendering entries that were never created.
		// Pending, not Failure: an in-flight SPIRE install resolves this on its own, and
		// only Pending keeps spiffeEntriesProgressing re-reconciling until it does.
		conditions.AddFalse(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady, conditions.ReasonPending,
			conditions.ConditionMessage("The ClusterStaticEntry CRD is not installed: SPIFFE is enabled but SPIRE is not deployed in the host cluster"))
	case len(pending) > 0:
		conditions.AddFalse(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady, conditions.ReasonPending,
			spiffeEntriesMessage("The following ClusterStaticEntries are awaiting rendering by spire-controller-manager", pending))
	default:
		// Also the no-opt-in case, so this never holds back the Ready summary.
		conditions.AddTrue(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady)
	}
	return nil
}

// spiffeEntriesMessage renders a condition message from a list of entry names, reusing the
// package's shared sort-and-truncate so a DPUService spanning hundreds of DPUs cannot blow up
// its own status.
func spiffeEntriesMessage(message string, entries []string) conditions.ConditionMessage {
	return conditions.ConditionMessage(fmt.Sprintf("%s: %s", message, formatUnreadyItems(entries)))
}

// desiredSPIFFEEntries returns the entries this DPUService should own, one per SPIFFE-mode
// DPU in each targeted DPUCluster, and nothing when SPIFFE is not in play.
//
// DPUs whose serial (or the service ID) cannot form a SPIFFE identifier go to invalid
// instead of failing the call, so one bad serial does not withhold identities from the rest.
func (r *DPUServiceReconciler) desiredSPIFFEEntries(ctx context.Context, dpuService *dpuservicev1.DPUService, dpuClusterConfigs []*dpucluster.Config, dpfOperatorConfig *operatorv1.DPFOperatorConfig) (targets []spiffeEntryTarget, invalid []string, err error) {
	if !dpuServiceSpiffeEnabled(dpuService) || !util.SpiffeEnabled(dpfOperatorConfig) {
		return nil, nil, nil
	}
	// In-cluster DPUServices run on the host, so no per-DPU agent exists to parent to.
	// Admission rejects this; this covers objects predating the rule.
	if ptr.Deref(dpuService.Spec.DeployInCluster, false) {
		return nil, nil, nil
	}

	targetDPUClusterConfigs, err := utils.GetMatchingDPUClusters(dpuClusterConfigs, dpuService.Spec.DPUClusterSelector)
	if err != nil {
		return nil, nil, fmt.Errorf("selecting DPUClusters for DPUService %s: %w", client.ObjectKeyFromObject(dpuService), err)
	}
	targetClusters := make(map[string]struct{}, len(targetDPUClusterConfigs))
	for _, dpuClusterConfig := range targetDPUClusterConfigs {
		// A DPUCluster being torn down keeps no workloads.
		if !dpuClusterConfig.Cluster.DeletionTimestamp.IsZero() {
			continue
		}
		targetClusters[applicationTargetClusterKey(dpuClusterConfig.Cluster.Namespace, dpuClusterConfig.Cluster.Name)] = struct{}{}
	}
	if len(targetClusters) == 0 {
		return nil, nil, nil
	}

	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList); err != nil {
		return nil, nil, fmt.Errorf("listing DPUs for DPUService %s: %w", client.ObjectKeyFromObject(dpuService), err)
	}

	// Parsed once per reconcile: an unusable template is a cluster configuration error, not a
	// per-DPU one, so it fails the reconcile rather than marking every DPU invalid.
	renderer, err := spire.NewDPUServiceIdentityRenderer(dpfOperatorConfig.Spec.Security.SPIFFE)
	if err != nil {
		return nil, nil, fmt.Errorf("building the DPUService identity renderer: %w", err)
	}

	log := ctrllog.FromContext(ctx)
	serviceID := generateServiceID(dpuService)

	targets = make([]spiffeEntryTarget, 0, len(dpuList.Items))
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]
		// Bootstrap-token DPUs have no agent to parent to. Skipped, not an error: mixed
		// identity modes are valid during a rolling migration to SPIFFE.
		if !util.IsSpiffeDPU(dpu) {
			continue
		}
		if _, ok := targetClusters[applicationTargetClusterKey(dpu.Spec.Cluster.Namespace, dpu.Spec.Cluster.Name)]; !ok {
			continue
		}

		target, err := buildSpiffeEntryTarget(renderer, dpuService, serviceID, dpu)
		if err != nil {
			// Terminal for this DPU: only a recreate with a representable serial or
			// serviceID fixes it, so do not fail and requeue forever.
			log.Error(err, "DPU cannot form a valid SPIFFE identity for this DPUService", "dpu", dpu.Name)
			invalid = append(invalid, dpu.Name)
			continue
		}
		targets = append(targets, target)
	}

	// Deterministic order, so condition messages do not churn across reconciles.
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	return targets, invalid, nil
}

// buildSpiffeEntryTarget derives the entry name and spiffeID/parentID for one DPU. Both share
// the spire package's validation, so either failing is terminal for this DPU.
func buildSpiffeEntryTarget(renderer *spire.DPUServiceIdentityRenderer, dpuService *dpuservicev1.DPUService, serviceID string, dpu *provisioningv1.DPU) (spiffeEntryTarget, error) {
	name, err := dpuServiceClusterStaticEntryName(dpuService.Namespace, dpuService.Name, dpu.Spec.SerialNumber)
	if err != nil {
		return spiffeEntryTarget{}, err
	}
	identity, err := renderer.Render(dpuService, dpu, serviceID)
	if err != nil {
		return spiffeEntryTarget{}, err
	}
	return spiffeEntryTarget{name: name, spiffeID: identity.SPIFFEID, parentID: identity.ParentID, dpu: dpu.Name}, nil
}

// applySPIFFEEntry creates or updates one ClusterStaticEntry and reports its rendered state.
// crdMissing separates "SPIRE is not installed" from an entry waiting to be rendered: both
// leave the entry absent, but only the latter resolves without an operator installing SPIRE.
func (r *DPUServiceReconciler) applySPIFFEEntry(ctx context.Context, dpuService *dpuservicev1.DPUService, target spiffeEntryTarget, dpfOperatorConfig *operatorv1.DPFOperatorConfig) (masked, ready, crdMissing bool, err error) {
	className := dpfOperatorConfig.Spec.Security.SPIFFE.SPIREControllerManagerClassName
	selectors := dpuServiceSpiffeSelectors(dpuService)

	cse := &spirev1alpha1.ClusterStaticEntry{ObjectMeta: metav1.ObjectMeta{Name: target.name}}
	if _, err := controllerutil.CreateOrPatch(ctx, r.Client, cse, func() error {
		// Merge rather than replace, so out-of-band labels survive.
		labels := cse.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[dpuservicev1.DPUServiceNameLabelKey] = dpuService.Name
		labels[dpuservicev1.DPUServiceNamespaceLabelKey] = dpuService.Namespace
		cse.SetLabels(labels)
		setDPUServiceSpiffeEntrySpec(cse, target, className, selectors)
		return nil
	}); err != nil {
		if crdAbsent(err) {
			// spire-controller-manager is not installed: report it on the condition rather
			// than erroring, and let the progress poll pick the install up when it lands.
			return false, false, true, nil
		}
		// A NotFound here is not "absent by design": it means the entry was deleted between
		// the read and the patch, so it must be retried rather than reported as pending.
		return false, false, false, fmt.Errorf("applying ClusterStaticEntry %s for DPU %s: %w", target.name, target.dpu, err)
	}

	// CreateOrPatch leaves cse holding the server object, status included.
	if cse.Status.Masked {
		return true, false, false, nil
	}
	return false, cse.Status.Rendered, false, nil
}

// dpuServiceSpiffeSelectors scopes a DPUService's SVID to that service's pods, keyed on the
// destination namespace and the svc.dpu.nvidia.com/service pod label this controller already
// stamps onto the pod templates it manages.
//
// Deliberately not the DPU Agent entry's coarse unix:uid:0: both share a parent SPIRE agent,
// so identical selectors would hand any uid-0 workload on the DPU both identities.
//
// Resolving these needs the on-DPU agent running the Kubernetes workload attestor against the
// DPU cluster's kubelet, which the DPU Agent enables once kubelet has produced usable
// certificates, and the pod reaching the agent's workload API socket.
func dpuServiceSpiffeSelectors(dpuService *dpuservicev1.DPUService) []string {
	return []string{
		fmt.Sprintf("k8s:ns:%s", dpuService.Namespace),
		fmt.Sprintf("k8s:pod-label:%s:%s", dpuservicev1.DPFServiceIDLabelKey, generateServiceID(dpuService)),
	}
}

func setDPUServiceSpiffeEntrySpec(cse *spirev1alpha1.ClusterStaticEntry, target spiffeEntryTarget, className string, selectors []string) {
	cse.Spec.ClassName = className
	cse.Spec.SPIFFEID = target.spiffeID
	cse.Spec.ParentID = target.parentID
	cse.Spec.Selectors = selectors
	cse.Spec.X509SVIDTTL = metav1.Duration{Duration: spire.EntryX509SVIDTTL}
	cse.Spec.JWTSVIDTTL = metav1.Duration{Duration: spire.EntryJWTSVIDTTL}
	cse.Spec.Hint = dpuServiceSpiffeEntryHint
}

// deleteUnwantedSPIFFEEntries deletes every owned entry not in keep and returns those still
// present. A lingering entry keeps the condition pending rather than being ignored.
func (r *DPUServiceReconciler) deleteUnwantedSPIFFEEntries(ctx context.Context, dpuService *dpuservicev1.DPUService, keep map[string]struct{}) ([]string, error) {
	// Uncached: an entry created moments ago may be missing from the cache, and listing
	// nothing here would release the finalizer and leak that entry's SPIRE registration.
	list := &spirev1alpha1.ClusterStaticEntryList{}
	if err := r.uncachedReader().List(ctx, list, spiffeEntryOwnerLabels(dpuService)); err != nil {
		if crdAbsent(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing ClusterStaticEntries for DPUService %s: %w", client.ObjectKeyFromObject(dpuService), err)
	}

	remaining := make([]string, 0, len(list.Items))
	for i := range list.Items {
		entry := &list.Items[i]
		if _, ok := keep[entry.GetName()]; ok {
			continue
		}
		if entry.GetDeletionTimestamp().IsZero() {
			if err := r.Client.Delete(ctx, entry); err != nil {
				if entryGone(err) {
					continue
				}
				return nil, fmt.Errorf("deleting ClusterStaticEntry %s: %w", entry.GetName(), err)
			}
		}
		// Confirm against the API server, not the cache: a cached read still sees the object
		// the delete just removed, and reporting it as remaining would hold the finalizer
		// for a full resync. Reporting gone early would release the finalizer while the
		// SPIRE registration still exists, so this cannot use the cache either way.
		check := &spirev1alpha1.ClusterStaticEntry{}
		if err := r.uncachedReader().Get(ctx, client.ObjectKey{Name: entry.GetName()}, check); err != nil {
			if entryGone(err) {
				continue
			}
			return nil, fmt.Errorf("confirming ClusterStaticEntry %s deletion: %w", entry.GetName(), err)
		}
		remaining = append(remaining, entry.GetName())
	}
	sort.Strings(remaining)
	return remaining, nil
}

// reconcileDeleteSPIFFEEntries removes every owned entry and releases the deregistration
// finalizer once they are gone. false means the caller should requeue.
func (r *DPUServiceReconciler) reconcileDeleteSPIFFEEntries(ctx context.Context, dpuService *dpuservicev1.DPUService) (done bool, err error) {
	remaining, err := r.deleteUnwantedSPIFFEEntries(ctx, dpuService, nil)
	if err != nil {
		return false, err
	}
	if len(remaining) > 0 {
		ctrllog.FromContext(ctx).Info("Awaiting ClusterStaticEntry deletion before releasing the SPIFFE finalizer", "entries", remaining)
		return false, nil
	}
	controllerutil.RemoveFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)
	return true, nil
}

// dpuToSpiffeDPUServices enqueues every SPIFFE-enabled DPUService. It does not filter by the
// DPU's cluster: DPUClusters are selected by label, which needs desiredSPIFFEEntries' own
// evaluation. The set is zero on clusters not using SPIFFE.
func (r *DPUServiceReconciler) dpuToSpiffeDPUServices(ctx context.Context, _ client.Object) []ctrl.Request {
	dpuServices := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServices); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list DPUServices for DPU event")
		return nil
	}
	requests := make([]ctrl.Request, 0, len(dpuServices.Items))
	for i := range dpuServices.Items {
		dpuService := &dpuServices.Items[i]
		if !dpuServiceSpiffeEnabled(dpuService) {
			continue
		}
		requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(dpuService)})
	}
	return requests
}
