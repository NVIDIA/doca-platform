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
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	yamlv3 "gopkg.in/yaml.v3"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const (
	// privilegedAllowlistConfigMapName is the ConfigMap that backs the VAP's
	// paramRef in the DPU cluster.
	privilegedAllowlistConfigMapName = "dpf-deny-privileged-pods-allowlist"
	// privilegedAllowlistConfigMapNamespace is the namespace where the
	// privileged DPUService ConfigMap lives in the DPU cluster.
	privilegedAllowlistConfigMapNamespace = "dpf-operator-system"
	// NamespaceScopeLabelKey is the label set on destination namespaces that the
	// VAP's namespaceSelector matches.
	NamespaceScopeLabelKey = "svc.dpu.nvidia.com/dpuservice-namespace"
	// DPUServicePrereqLabel is the label set on objects created as
	// prerequisites for DPUServices.
	DPUServicePrereqLabel = "svc.dpu.nvidia.com/dpuservice-prereq"
	// dpuServicePrereqLabelValue is the canonical value applied to
	// DPUServicePrereqLabel-labeled objects.
	dpuServicePrereqLabelValue = "true"

	privilegedPodDeniedMessage = "Privileged containers are not allowed for this DPUService unless security.privileged is set to true."
)

// privilegedDefaultForUnsetSecurity is the value applied when a
// DPUService (or DPUServiceTemplate during DPUDeployment generation) does
// not set spec.security.privileged. Iteration 1 keeps this true so legacy
// privileged workloads survive the upgrade; iteration 2 flips it to false
// once existing DPUServices have been migrated to the explicit opt-in.
//
// Both the privileged-pod enforcement reconciler and the DPUDeployment
// generator read this constant; there must be exactly one source of truth.
//
// TODO: change to false after v26.7.
const privilegedDefaultForUnsetSecurity = true

//go:embed manifests/privileged-pod-enforcement-vap.yaml
var rawVAPManifest []byte

var (
	privilegedVAPName        string
	privilegedPodsVAP        *admissionregistrationv1.ValidatingAdmissionPolicy
	privilegedPodsVAPBinding *admissionregistrationv1.ValidatingAdmissionPolicyBinding
)

func init() {
	err := parseVAPManifest()
	if err != nil {
		panic(fmt.Errorf("decode embedded privileged-pod-enforcement-vap.yaml: %w", err))
	}
}

// parseVAPManifest decodes the embedded VAP + binding YAML into typed
// objects ready for server-side apply.
func parseVAPManifest() error {
	docs, err := readYAMLDocuments(rawVAPManifest)
	if err != nil {
		return fmt.Errorf("read privileged-pod-enforcement-vap.yaml documents: %w", err)
	}

	if len(docs) != 2 {
		return fmt.Errorf("expected 2 documents in privileged-pod-enforcement-vap.yaml, got %d", len(docs))
	}

	if err := decodeYAMLDocument(docs[0], &privilegedPodsVAP); err != nil {
		return fmt.Errorf("decode ValidatingAdmissionPolicy: %w", err)
	}
	if err := decodeYAMLDocument(docs[1], &privilegedPodsVAPBinding); err != nil {
		return fmt.Errorf("decode ValidatingAdmissionPolicyBinding: %w", err)
	}

	privilegedVAPName = privilegedPodsVAP.GetName()

	// Ensure the DPUServicePrereqLabel is set.
	existingLabels := privilegedPodsVAP.GetLabels()
	if existingLabels == nil {
		existingLabels = map[string]string{}
	}
	existingLabels[DPUServicePrereqLabel] = dpuServicePrereqLabelValue
	privilegedPodsVAP.SetLabels(existingLabels)

	existingLabels = privilegedPodsVAPBinding.GetLabels()
	if existingLabels == nil {
		existingLabels = map[string]string{}
	}
	existingLabels[DPUServicePrereqLabel] = dpuServicePrereqLabelValue
	privilegedPodsVAPBinding.SetLabels(existingLabels)

	return nil
}

func readYAMLDocuments(raw []byte) ([]map[string]any, error) {
	dec := yamlv3.NewDecoder(bytes.NewReader(raw))
	var docs []map[string]any
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(doc) == 0 {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func decodeYAMLDocument(doc map[string]any, out any) error {
	raw, err := yamlv3.Marshal(doc)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(raw, out)
}

// buildPrivilegedDPUServiceConfigMap returns the ConfigMap that the VAP's
// binding references. Each data entry is keyed by DPUService service ID and
// stores the DPUService namespaced name as its value.
func buildPrivilegedDPUServiceConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      privilegedAllowlistConfigMapName,
			Namespace: privilegedAllowlistConfigMapNamespace,
			Labels: map[string]string{
				// Set the DPUServicePrereqLabel label to ensure we cache the configmap.
				DPUServicePrereqLabel: dpuServicePrereqLabelValue,
			},
		},
		Data: map[string]string{},
	}
}

func (r *DPUServiceReconciler) reconcilePrivilegedPodEnforcements(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, currentDPUService *dpuservicev1.DPUService, enforce bool) error {
	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := r.getDPUClusterClient(ctx, dpuClusterConfig.Cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("get client for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}

		if err := r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, currentDPUService, dpuClusterConfig.Cluster, enforce); err != nil {
			errs = append(errs, fmt.Errorf("reconcile PrivilegedPodEnforcement for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

// removePrivilegedPodEnforcementEntries removes the given DPUService's entry
// from the privileged allowlist ConfigMap in every DPUCluster. It is used on
// deletion: it only cleans up this service's own entry and never changes the
// global VAP/binding state, so deleting a single DPUService can never affect
// enforcement for other services or depend on the DPFOperatorConfig.
func (r *DPUServiceReconciler) removePrivilegedPodEnforcementEntries(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, dpuService *dpuservicev1.DPUService) error {
	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := r.getDPUClusterClient(ctx, dpuClusterConfig.Cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("get client for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}
		if err := r.removePrivilegedDPUServiceEntry(ctx, dpuClusterClient, dpuService); err != nil {
			errs = append(errs, fmt.Errorf("remove privileged allowlist entry for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

// reconcilePrivilegedPodEnforcement reconciles the privileged pod enforcement for a given DPUService.
// Note: The apply and allowlist-rebuild operations are okay as is as long as we don't have
// concurrent reconciles of different DPUServices. As soon as we have concurrent reconciles, we may
// need to add a locking mechanism so at most one reconcile at a time handles these parts.
func (r *DPUServiceReconciler) reconcilePrivilegedPodEnforcement(ctx context.Context, dpuClusterClient client.Client, currentDPUService *dpuservicev1.DPUService, cluster *provisioningv1.DPUCluster, enforce bool) error {
	// Rebuild the authoritative allowlist from all DPUServices before applying
	// the binding; this also ensures the namespace and ConfigMap exist so the
	// binding's paramRef always resolves. The allowlist stays populated in both
	// modes so Audit-mode violations remain meaningful.
	if err := r.reconcilePrivilegedAllowlist(ctx, dpuClusterClient, cluster); err != nil {
		return err
	}

	// Deny when enforcing, Audit when disabled. In both modes we keep the VAP
	// and its binding and only flip the action — we never delete them (deleting
	// a binding triggers a K8s paramRef informer bug:
	// https://github.com/kubernetes/kubernetes/issues/133827).
	validationAction := admissionregistrationv1.Deny
	if !enforce {
		validationAction = admissionregistrationv1.Audit
	}

	// Apply the VAP and binding once the allowlist ConfigMap is up to date. In
	// Deny mode applyPrivilegedPodPolicyVAP probes that privileged pods are
	// denied before returning; in Audit mode that probe is skipped.
	if err := r.applyPrivilegedPodPolicyVAP(ctx, dpuClusterClient, validationAction); err != nil {
		return err
	}

	// While enforcing, confirm this DPUService's own privileged pods are admitted
	// when it is expected to run them. Nothing is denied in Audit mode, so skip.
	if !enforce {
		return nil
	}
	return validateServicePrivilegedPodAdmission(ctx, dpuClusterClient, currentDPUService, cluster)
}

// applyPrivilegedPodPolicyVAP creates or updates the ValidatingAdmissionPolicy
// and its binding. validationAction selects the binding's validationActions:
// Deny when the feature is enabled (privileged workloads are rejected) and
// Audit when it is disabled (admission is only recorded to the audit log, never
// denied). The post-apply dry-run probes assert that privileged pods are
// rejected, which only holds for the enforcing (Deny) configuration, so they
// are skipped in any non-Deny mode.
func (r *DPUServiceReconciler) applyPrivilegedPodPolicyVAP(ctx context.Context, dpuClusterClient client.Client, validationAction admissionregistrationv1.ValidationAction) error {
	// Create or Patch ValidatingAdmissionPolicy
	desiredPolicy := privilegedPodsVAP.DeepCopy()
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: desiredPolicy.GetName(),
		},
	}

	if _, err := controllerutil.CreateOrPatch(ctx, dpuClusterClient, policy, func() error {
		policy.Spec = desiredPolicy.Spec
		policy.Labels = desiredPolicy.Labels
		policy.Annotations = desiredPolicy.Annotations
		return nil
	}); err != nil {
		return fmt.Errorf("create or patch ValidatingAdmissionPolicy: %w", err)
	}

	// Create or Patch ValidatingAdmissionPolicyBinding, overriding the manifest's
	// validationActions with the requested action (Deny when enforcing, Audit
	// when the feature is disabled).
	desiredBinding := privilegedPodsVAPBinding.DeepCopy()
	desiredBinding.Spec.ValidationActions = []admissionregistrationv1.ValidationAction{validationAction}
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: desiredBinding.GetName(),
		},
	}
	if _, err := controllerutil.CreateOrPatch(ctx, dpuClusterClient, binding, func() error {
		binding.Spec = desiredBinding.Spec
		binding.Labels = desiredBinding.Labels
		binding.Annotations = desiredBinding.Annotations
		return nil
	}); err != nil {
		return fmt.Errorf("create or patch ValidatingAdmissionPolicyBinding: %w", err)
	}

	// The dry-run probes assert that privileged pods are denied, which is only
	// true for the enforcing configuration. In Audit mode nothing is denied, so
	// skip them.
	if validationAction != admissionregistrationv1.Deny {
		return nil
	}

	// Note: overriding is only used in tests.
	if r.privilegedPodEnforcementValidator != nil {
		return r.privilegedPodEnforcementValidator(ctx, dpuClusterClient)
	}
	return validatePrivilegedPodEnforcement(ctx, dpuClusterClient)
}

// paramRefNotSyncedMarker is the substring the API server returns when a VAP
// binding with parameterNotFoundAction=Deny is evaluated before its paramRef
// informer has observed the allowlist ConfigMap. It is a transient condition
// that clears once the informer syncs.
const paramRefNotSyncedMarker = "no params found for policy binding with `Deny` parameterNotFoundAction"

// isParamRefNotSyncedErr reports whether err is the transient "paramRef informer
// not yet synced" signal (see paramRefNotSyncedMarker). It self-heals once the
// informer syncs, so probes surface it as a distinct, retry-worthy error rather
// than conflating it with an unexpected rejection.
func isParamRefNotSyncedErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), paramRefNotSyncedMarker)
}

// isPrivilegedPodDeniedErr reports whether err is our PrivilegedPodEnforcement
// VAP denying a privileged pod (Invalid + privilegedPodDeniedMessage), as
// opposed to a denial originating from a different admission stage (e.g.
// PodSecurity or another webhook), which the probes must not mistake for the
// VAP's own decision.
func isPrivilegedPodDeniedErr(err error) bool {
	return apierrors.IsInvalid(err) && strings.Contains(err.Error(), privilegedPodDeniedMessage)
}

// validateServicePrivilegedPodAdmission runs a dry-run probe confirming that the
// given DPUService's own privileged pods are admitted, but only when the service
// is expected to run them: it targets this cluster, is not being deleted, and
// resolves to privileged=true. For any other service it is a no-op.
func validateServicePrivilegedPodAdmission(ctx context.Context, dpuClusterClient client.Client, currentDPUService *dpuservicev1.DPUService, cluster *provisioningv1.DPUCluster) error {
	targetsCluster, err := dpuServiceTargetsCluster(currentDPUService, cluster)
	if err != nil {
		return fmt.Errorf("evaluate target for DPUCluster %s: %w", cluster.Name, err)
	}
	allowed, fellBack := resolvePrivileged(currentDPUService)
	if fellBack && allowed && targetsCluster && currentDPUService.DeletionTimestamp.IsZero() {
		ctrllog.FromContext(ctx).V(4).Info(
			"DPUService allowlisted for privileged workloads via legacy fallback; set spec.security.privileged explicitly before upgrading to the next release",
			"dpuService", client.ObjectKeyFromObject(currentDPUService),
		)
	}
	shouldAllowPrivilegedPods := targetsCluster && currentDPUService.DeletionTimestamp.IsZero() && allowed
	if !shouldAllowPrivilegedPods {
		return nil
	}
	return validateAllowlistedPrivilegedPodAdmission(ctx, dpuClusterClient, getDPUServiceID(currentDPUService))
}

// validateAllowlistedPrivilegedPodAdmission runs a single dry-run probe
// confirming that a privileged pod labeled with the given allowlisted DPUService
// ID is admitted. It is the positive-direction counterpart to
// validatePrivilegedPodEnforcement (which confirms an unknown service's
// privileged pod is denied): together they prove the allowlist both denies what
// it should and admits what it should. This one is run per-DPUService, only for
// services expected to run privileged pods on the cluster.
func validateAllowlistedPrivilegedPodAdmission(ctx context.Context, c client.Client, dpuServiceID string) error {
	if dpuServiceID == "" {
		return fmt.Errorf("VAP probe: allowlisted privileged pod requires a DPUService ID")
	}
	err := c.Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpf-vap-allowlisted-probe",
			Namespace: privilegedAllowlistConfigMapNamespace,
			Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: dpuServiceID},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "probe",
					Image:           "scratch",
					SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)},
				},
			},
		},
	}, client.DryRunAll)
	if err != nil {
		switch {
		case isPrivilegedPodDeniedErr(err):
			return fmt.Errorf("VAP probe: allowlisted privileged pod rejected: %w", err)
		case isParamRefNotSyncedErr(err):
			return fmt.Errorf("VAP probe: waiting for the VAP paramRef informer to catch up: %w", err)
		default:
			return fmt.Errorf("VAP probe: allowlisted privileged pod got unexpected error: %w", err)
		}
	}
	return nil
}

// validatePrivilegedPodEnforcement runs two dry-run pod-creation probes against
// the DPU cluster to confirm that the VAP setup is correct after apply:
//  1. A non-privileged pod must be admitted — this confirms the ConfigMap
//     paramRef is resolvable (guards against the "no params found" race).
//  2. A privileged pod with an unknown DPUService ID must be denied — this
//     confirms the allowlist enforcement logic is active.
//
// It runs once after every Deny-mode apply and validates the policy globally; it
// does not depend on any particular DPUService.
func validatePrivilegedPodEnforcement(ctx context.Context, c client.Client) error {
	probeMeta := metav1.ObjectMeta{
		Name:      "dpf-vap-probe",
		Namespace: privilegedAllowlistConfigMapNamespace,
		// An ID that is never added to the allowlist ConfigMap.
		Labels: map[string]string{dpuservicev1.DPFServiceIDLabelKey: "dpf-vap-probe"},
	}
	probeContainer := corev1.Container{Name: "probe", Image: "scratch"}

	// Probe 1: non-privileged pod must be allowed. The only expected failure is
	// the transient paramRef-not-synced race (with parameterNotFoundAction=Deny,
	// an unobserved allowlist ConfigMap denies even non-privileged pods); any
	// other rejection comes from a different admission stage and is unexpected.
	if err := c.Create(ctx, &corev1.Pod{
		ObjectMeta: probeMeta,
		Spec:       corev1.PodSpec{Containers: []corev1.Container{probeContainer}},
	}, client.DryRunAll); err != nil {
		if isParamRefNotSyncedErr(err) {
			return fmt.Errorf("VAP probe: waiting for the VAP paramRef informer to catch up (non-privileged pod transiently denied): %w", err)
		}
		return fmt.Errorf("VAP probe: non-privileged pod unexpectedly rejected: %w", err)
	}

	// Probe 2: privileged pod must be denied by our VAP.
	privileged := true
	privilegedContainer := probeContainer
	privilegedContainer.SecurityContext = &corev1.SecurityContext{Privileged: &privileged}
	err := c.Create(ctx, &corev1.Pod{
		ObjectMeta: probeMeta,
		Spec:       corev1.PodSpec{Containers: []corev1.Container{privilegedContainer}},
	}, client.DryRunAll)
	switch {
	case err == nil:
		return fmt.Errorf("VAP probe: privileged pod was unexpectedly allowed — allowlist check may be broken")
	case isPrivilegedPodDeniedErr(err):
		// Expected result.
		return nil
	case isParamRefNotSyncedErr(err):
		return fmt.Errorf("VAP probe: waiting for the VAP paramRef informer to catch up: %w", err)
	default:
		return fmt.Errorf("VAP probe: privileged pod got unexpected error: %w", err)
	}
}

// reconcilePrivilegedAllowlist rebuilds the allowlist ConfigMap from the full
// set of DPUServices in the management cluster, so the allowlist is
// authoritative on every enforcing reconcile and independent of which
// DPUService triggered it. An entry is present if its DPUService still exists,
// targets this cluster, and resolves to privileged=true. It also ensures the
// ConfigMap's namespace exists and creates the ConfigMap if absent.
//
// Rebuilding the whole map is required to reliably switch on and off the feature.
func (r *DPUServiceReconciler) reconcilePrivilegedAllowlist(ctx context.Context, dpuClusterClient client.Client, cluster *provisioningv1.DPUCluster) error {
	allDPUServices := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, allDPUServices); err != nil {
		return fmt.Errorf("list all DPUServices: %w", err)
	}

	desired := make(map[string]string, len(allDPUServices.Items))
	for i := range allDPUServices.Items {
		svc := &allDPUServices.Items[i]
		serviceID := getDPUServiceID(svc)
		if serviceID == "" {
			continue
		}
		targetsCluster, err := dpuServiceTargetsCluster(svc, cluster)
		if err != nil {
			return fmt.Errorf("evaluate target for DPUService %s on DPUCluster %s: %w", client.ObjectKeyFromObject(svc), cluster.Name, err)
		}
		if allowed, _ := resolvePrivileged(svc); targetsCluster && allowed {
			desired[serviceID] = client.ObjectKeyFromObject(svc).String()
		}
	}

	// Ensure the namespace exists before patching the ConfigMap into it. The
	// binding's paramRef has parameterNotFoundAction: Deny, so the ConfigMap must
	// exist before the binding is applied; seeding it here is what lets callers
	// apply the binding straight after.
	if err := r.ensureNamespace(ctx, dpuClusterClient, privilegedAllowlistConfigMapNamespace); err != nil {
		return fmt.Errorf("ensure %s namespace: %w", privilegedAllowlistConfigMapNamespace, err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      privilegedAllowlistConfigMapName,
			Namespace: privilegedAllowlistConfigMapNamespace,
		},
	}
	if _, err := controllerutil.CreateOrPatch(ctx, dpuClusterClient, cm, func() error {
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels[DPUServicePrereqLabel] = dpuServicePrereqLabelValue
		cm.Data = desired
		return nil
	}); err != nil {
		return fmt.Errorf("rebuild allowlist ConfigMap: %w", err)
	}
	return nil
}

// removePrivilegedDPUServiceEntry removes the given DPUService's entry from the
// privileged allowlist ConfigMap. It is a no-op when the service has no service
// ID (it can never have had an entry). Adding entries is handled by the
// authoritative rebuild in reconcilePrivilegedAllowlist, so this is removal-only.
func (r *DPUServiceReconciler) removePrivilegedDPUServiceEntry(ctx context.Context, dpuClusterClient client.Client, dpuService *dpuservicev1.DPUService) error {
	serviceID := getDPUServiceID(dpuService)
	if serviceID == "" {
		return nil
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      privilegedAllowlistConfigMapName,
			Namespace: privilegedAllowlistConfigMapNamespace,
		},
	}
	_, err := controllerutil.CreateOrPatch(ctx, dpuClusterClient, cm, func() error {
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels[DPUServicePrereqLabel] = dpuServicePrereqLabelValue
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		delete(cm.Data, serviceID)
		return nil
	})
	if err != nil {
		return fmt.Errorf("create or patch ConfigMap: %w", err)
	}

	return nil
}

// dpuServiceTargetsCluster reports whether the given DPUService targets the
// given DPUCluster (selector matches and the service is not deployed in the
// management cluster). It returns an error if the DPUClusterSelector cannot
// be parsed; that's a user-facing configuration problem and the caller
// should surface it rather than silently treat the service as not targeting
// any cluster.
func dpuServiceTargetsCluster(dpuService *dpuservicev1.DPUService, c *provisioningv1.DPUCluster) (bool, error) {
	if dpuService.ShouldDeployInCluster() {
		return false, nil
	}
	if !dpuService.DeletionTimestamp.IsZero() {
		return false, nil
	}
	if dpuService.Spec.DPUClusterSelector == nil {
		return true, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(dpuService.Spec.DPUClusterSelector)
	if err != nil {
		return false, fmt.Errorf("parse DPUClusterSelector: %w", err)
	}
	return selector.Matches(labels.Set(c.Labels)), nil
}

// resolvePrivileged returns (allowed, fellBackToDefault). allowed is the
// value the controller should treat as the DPUService's effective
// security.privileged. fellBackToDefault is true when the caller's
// Security/Privileged field is unset and the legacy default was used.
// Callers can use the second return value to emit a one-off log line
// during the migration window.
func resolvePrivileged(svc *dpuservicev1.DPUService) (bool, bool) {
	if svc.Spec.Security != nil && svc.Spec.Security.Privileged != nil {
		return *svc.Spec.Security.Privileged, false
	}
	return privilegedDefaultForUnsetSecurity, true
}

func getDPUServiceID(svc *dpuservicev1.DPUService) string {
	if svc.Spec.ServiceID != nil {
		return *svc.Spec.ServiceID
	}
	if svc.Status.ServiceID != "" {
		return svc.Status.ServiceID
	}
	return ""
}
