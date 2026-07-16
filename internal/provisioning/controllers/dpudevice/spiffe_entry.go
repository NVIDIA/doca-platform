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

package dpudevice

import (
	"context"
	"fmt"
	"sort"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/spire"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterStaticEntry is an upstream spire-controller-manager CRD; DPF only creates and
// deletes per-DPU instances, so it is handled as an unstructured object rather than
// vendoring the upstream Go types.
var clusterStaticEntryGVK = schema.GroupVersionKind{
	Group:   "spire.spiffe.io",
	Version: "v1alpha1",
	Kind:    "ClusterStaticEntry",
}

const (
	// Phase A ClusterStaticEntry.spec defaults. selectors is intentionally a
	// coarse uid:0 starter; it is hardened post-bake with unix:path + unix:sha256 selectors.
	spiffeEntrySelectorUID0 = "unix:uid:0"
	spiffeEntryX509SVIDTTL  = "1h"
	spiffeEntryJWTSVIDTTL   = "120s"
	spiffeEntryHint         = "dpu-agent"

	// Labels stamped on each ClusterStaticEntry so a CR watch event can be mapped back to its
	// owning DPUDevice (the CR is cluster-scoped and cannot carry a namespaced ownerReference).
	LabelDPUDeviceName      = cutil.DPUDeviceNameLabel
	LabelDPUDeviceNamespace = cutil.DPUProvisioningPrefix + "dpudevice-namespace"
)

func newClusterStaticEntry() *unstructured.Unstructured {
	cse := &unstructured.Unstructured{}
	cse.SetGroupVersionKind(clusterStaticEntryGVK)
	return cse
}

// emitEvent records a Kubernetes Event when a Recorder is wired.
func (r *DPUDeviceReconciler) emitEvent(dpuDevice *provisioningv1.DPUDevice, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(dpuDevice, eventType, reason, message)
	}
}

// reconcileSPIFFEEntry creates/updates the per-DPU SPIRE ClusterStaticEntry for a SPIFFE-mode
// DPU and mirrors its upstream status into the DPUDevice SPIFFEEntryReady condition. It is a
// no-op (no condition, no CR) for clusters or DPUs that do not use SPIFFE identity, so it is
// safe to call on every DPUDevice reconcile. The condition and finalizer are persisted by the
// caller's deferred patcher, matching the existing BMCCredentialFinalizer handling.
func (r *DPUDeviceReconciler) reconcileSPIFFEEntry(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, cfg *operatorv1.DPFOperatorConfig) error {
	log := log.FromContext(ctx)

	if !cutil.SpiffeEnabled(cfg) {
		return nil
	}

	dpu, err := r.findOwningSpiffeDPU(ctx, dpuDevice)
	if err != nil {
		return fmt.Errorf("finding owning SPIFFE DPU for DPUDevice %s: %w", dpuDevice.Name, err)
	}
	if dpu == nil {
		// No SPIFFE DPU is bound to this device (legacy bootstrap-token DPU, not yet stamped,
		// or not yet attached). Do not create a CR or set the condition.
		return nil
	}

	serial := dpuDevice.Spec.SerialNumber
	trustDomain := cfg.Spec.Security.SPIFFE.SPIRETrustDomain
	className := cfg.Spec.Security.SPIFFE.SPIREControllerManagerClassName

	name, spiffeID, parentID, buildErr := buildSpiffeEntryIdentifiers(trustDomain, serial)
	if buildErr != nil {
		// Terminal: an unrepresentable serial cannot yield a valid identity. Surface it and do
		// not requeue (SerialNumberInvalid) -- it requires DPU delete-recreate.
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady,
			conditions.ReasonError, conditions.ConditionMessage(buildErr.Error()))
		r.emitEvent(dpuDevice, corev1.EventTypeWarning, events.EventSPIFFEEntryRegistrationFailedReason, buildErr.Error())
		log.Error(buildErr, "DPUDevice serial cannot form a valid SPIFFE identity; manual intervention required: recreate the DPUDevice with a serial within the supported charset and length", "serial", serial)
		return nil
	}

	// Persist the deletion-ordering finalizer BEFORE creating the external ClusterStaticEntry. If the
	// CSE were created first and the controller crashed (or the deferred patch failed) before the
	// finalizer became durable, a later DPUDevice delete would skip deregistration and leak a stale
	// SPIRE identity. Add-then-return matches the finalizer gate used across the other controllers
	// (e.g. DPUService): the deferred patcher persists the finalizer and the resulting DPUDevice
	// update re-triggers reconcile, which then creates the CSE with the finalizer already in place.
	if !controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.SPIFFEDeregistrationFinalizer) {
		controllerutil.AddFinalizer(dpuDevice, provisioningv1.SPIFFEDeregistrationFinalizer)
		return nil
	}

	cse := newClusterStaticEntry()
	cse.SetName(name)
	op, err := controllerutil.CreateOrPatch(ctx, r.Client, cse, func() error {
		// Merge our two mapping labels rather than replacing the whole label set, so any
		// labels added out-of-band (e.g. by spire-controller-manager) are preserved.
		labels := cse.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[LabelDPUDeviceName] = dpuDevice.Name
		labels[LabelDPUDeviceNamespace] = dpuDevice.Namespace
		cse.SetLabels(labels)
		return setSpiffeEntrySpec(cse, spiffeID, parentID, className)
	})
	if err != nil {
		// Transient (CRD missing, RBAC denial, API error): surface and requeue (CRSyncFailed).
		wrapped := fmt.Errorf("failed to apply ClusterStaticEntry %s: %w", name, err)
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady,
			conditions.ReasonError, conditions.ConditionMessage(wrapped.Error()))
		r.emitEvent(dpuDevice, corev1.EventTypeWarning, events.EventSPIFFEEntryRegistrationFailedReason, wrapped.Error())
		return wrapped
	}

	switch op {
	case controllerutil.OperationResultCreated:
		r.emitEvent(dpuDevice, corev1.EventTypeNormal, events.EventSPIFFEEntryRegisteredReason,
			fmt.Sprintf("Created ClusterStaticEntry %s", name))
	case controllerutil.OperationResultUpdated:
		r.emitEvent(dpuDevice, corev1.EventTypeWarning, events.EventSPIFFEEntrySpecDriftReconciledReason,
			fmt.Sprintf("Reconciled out-of-band edit to ClusterStaticEntry %s", name))
	}

	// CreateOrPatch leaves cse populated with the server object (including the status owned by
	// spire-controller-manager), so mirror it directly.
	if masked := r.mirrorSpiffeEntryStatus(dpuDevice, cse); masked {
		r.emitEvent(dpuDevice, corev1.EventTypeWarning, events.EventSPIFFEEntryMaskedReason,
			fmt.Sprintf("ClusterStaticEntry %s is masked by another entry", name))
	}
	return nil
}

// deleteSPIFFEEntry deletes the per-DPU ClusterStaticEntry and reports whether the CR is gone
// from the K8s API. The CR carries no finalizer, so the delete usually takes effect immediately
// and the re-read confirms removal in a single pass; if it lingers (e.g. an externally added
// finalizer) it returns done=false so the caller requeues. done=true means the
// SPIFFEDeregistrationFinalizer may be removed.
func (r *DPUDeviceReconciler) deleteSPIFFEEntry(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (done bool, err error) {
	name, nameErr := spire.DPUAgentClusterStaticEntryName(dpuDevice.Spec.SerialNumber)
	if nameErr != nil {
		// The serial never yielded a valid CR name, so no CR was ever created. Nothing to wait for.
		return true, nil
	}

	entryGone := func(err error) bool {
		// meta.IsNoMatchError means the ClusterStaticEntry CRD was unregistered while the
		// DPUDevice finalizer was held; no CSE of that kind can exist, so deregistration is done.
		return apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
	}

	cse := newClusterStaticEntry()
	if err := r.Get(ctx, types.NamespacedName{Name: name}, cse); err != nil {
		if entryGone(err) {
			return true, nil
		}
		return false, fmt.Errorf("getting ClusterStaticEntry %s for deletion: %w", name, err)
	}

	if cse.GetDeletionTimestamp().IsZero() {
		if err := r.Delete(ctx, cse); err != nil {
			if entryGone(err) {
				return true, nil
			}
			return false, fmt.Errorf("deleting ClusterStaticEntry %s: %w", name, err)
		}
		r.emitEvent(dpuDevice, corev1.EventTypeNormal, events.EventSPIFFEEntryDeleteRequestedReason,
			fmt.Sprintf("Requested deletion of ClusterStaticEntry %s", name))
	}
	// Re-read after issuing the delete: ClusterStaticEntry carries no finalizer, so the common
	// case is immediate removal and the DPUDevice finalizer can be released this pass instead of
	// forcing a 10s requeue. If it lingers (e.g. an externally added finalizer), report not-done.
	if err := r.Get(ctx, types.NamespacedName{Name: name}, cse); err != nil {
		if entryGone(err) {
			return true, nil
		}
		return false, fmt.Errorf("confirming ClusterStaticEntry %s deletion: %w", name, err)
	}
	return false, nil
}

// findOwningSpiffeDPU returns the SPIFFE-mode DPU bound to this DPUDevice, or nil when none is
// (yet) bound or the bound DPU uses bootstrap-token identity.
func (r *DPUDeviceReconciler) findOwningSpiffeDPU(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (*provisioningv1.DPU, error) {
	dpuList := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpuList,
		client.InNamespace(dpuDevice.Namespace),
		client.MatchingFields{dpuByDPUDeviceNameField: dpuDevice.Name},
	); err != nil {
		return nil, fmt.Errorf("listing DPUs by %s=%s in namespace %s: %w", dpuByDPUDeviceNameField, dpuDevice.Name, dpuDevice.Namespace, err)
	}
	// Two-stage filter: the field index narrows to DPUs bound to this device; IsSpiffeDPU then
	// keeps only SPIFFE-mode DPUs.
	var matches []*provisioningv1.DPU
	for i := range dpuList.Items {
		if dpu := &dpuList.Items[i]; cutil.IsSpiffeDPU(dpu) {
			matches = append(matches, dpu)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	// Deterministic selection: do NOT rely on API list order. DPU names are unique within a
	// namespace, so sorting by name alone yields a stable choice across reconciles.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Name < matches[j].Name
	})
	// More than one SPIFFE DPU bound to a single device is an anomaly (stale object not cleaned
	// up). Surface it rather than silently picking one, but still proceed deterministically.
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Name)
		}
		log.FromContext(ctx).Info("multiple SPIFFE-mode DPUs bound to one DPUDevice; selecting deterministically",
			"dpuDevice", dpuDevice.Name, "candidates", names, "selected", matches[0].Name)
		r.emitEvent(dpuDevice, corev1.EventTypeWarning, events.EventSPIFFEDuplicateDPUReason,
			fmt.Sprintf("multiple SPIFFE-mode DPUs %v are bound to DPUDevice %s; selecting %s", names, dpuDevice.Name, matches[0].Name))
	}
	return matches[0], nil
}

// buildSpiffeEntryIdentifiers derives the ClusterStaticEntry name and the spiffeID/parentID for a
// given trust domain and serial. All three share the single serial/trust-domain policy in the
// spire/identity packages, so any one failing is a terminal validation error for this DPU.
func buildSpiffeEntryIdentifiers(trustDomain, serial string) (name, spiffeID, parentID string, err error) {
	name, err = spire.DPUAgentClusterStaticEntryName(serial)
	if err != nil {
		return "", "", "", err
	}
	spiffeID, parentID, err = spire.SpireDPUAgentIDs(trustDomain, serial)
	if err != nil {
		return "", "", "", err
	}
	return name, spiffeID, parentID, nil
}

func setSpiffeEntrySpec(cse *unstructured.Unstructured, spiffeID, parentID, className string) error {
	if err := unstructured.SetNestedField(cse.Object, className, "spec", "className"); err != nil {
		return err
	}
	if err := unstructured.SetNestedField(cse.Object, spiffeID, "spec", "spiffeID"); err != nil {
		return err
	}
	if err := unstructured.SetNestedField(cse.Object, parentID, "spec", "parentID"); err != nil {
		return err
	}
	if err := unstructured.SetNestedStringSlice(cse.Object, []string{spiffeEntrySelectorUID0}, "spec", "selectors"); err != nil {
		return err
	}
	if err := unstructured.SetNestedField(cse.Object, spiffeEntryX509SVIDTTL, "spec", "x509SVIDTTL"); err != nil {
		return err
	}
	if err := unstructured.SetNestedField(cse.Object, spiffeEntryJWTSVIDTTL, "spec", "jwtSVIDTTL"); err != nil {
		return err
	}
	return unstructured.SetNestedField(cse.Object, spiffeEntryHint, "spec", "hint")
}

// mirrorSpiffeEntryStatus maps the upstream ClusterStaticEntry status triple into the DPUDevice
// SPIFFEEntryReady condition. It returns whether the entry is masked so the caller can
// emit an operator-actionable Event.
func (r *DPUDeviceReconciler) mirrorSpiffeEntryStatus(dpuDevice *provisioningv1.DPUDevice, cse *unstructured.Unstructured) (masked bool) {
	set, _, setErr := unstructured.NestedBool(cse.Object, "status", "set")
	rendered, renderedFound, renderedErr := unstructured.NestedBool(cse.Object, "status", "rendered")
	masked, _, maskedErr := unstructured.NestedBool(cse.Object, "status", "masked")
	if setErr != nil || renderedErr != nil || maskedErr != nil {
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady,
			conditions.ReasonError, conditions.ConditionMessage("ClusterStaticEntry status has invalid field types"))
		return false
	}

	switch {
	case masked:
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady,
			conditions.ReasonError, conditions.ConditionMessage("ClusterStaticEntry is masked by another entry"))
		return true
	case renderedFound && rendered:
		// A rendered entry is ready even when the controller-manager does not set status.set.
		conditions.AddTrue(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
	case set && !rendered:
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady,
			conditions.ReasonPending, conditions.ConditionMessage("ClusterStaticEntry set; rendering pending"))
	default:
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady,
			conditions.ReasonPending, conditions.ConditionMessage("Awaiting spire-controller-manager to observe ClusterStaticEntry"))
	}
	return false
}
