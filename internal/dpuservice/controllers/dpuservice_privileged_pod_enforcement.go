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
	"maps"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	yamlv3 "gopkg.in/yaml.v3"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
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

func (r *DPUServiceReconciler) reconcilePrivilegedPodEnforcements(ctx context.Context, dpuClusterConfigs []*dpucluster.Config, currentDPUService *dpuservicev1.DPUService) error {
	var errs []error
	for _, dpuClusterConfig := range dpuClusterConfigs {
		dpuClusterClient, err := r.getDPUClusterClient(ctx, dpuClusterConfig.Cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("get client for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}

		targetsCluster, err := dpuServiceTargetsCluster(currentDPUService, dpuClusterConfig.Cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("evaluate target for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
			continue
		}

		if err := r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, currentDPUService, targetsCluster); err != nil {
			errs = append(errs, fmt.Errorf("reconcile PrivilegedPodEnforcement for DPUCluster %s: %w", dpuClusterConfig.Cluster.Name, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

// reconcilePrivilegedPodEnforcement reconciles the privileged pod enforcement for a given DPUService.
// Note: The cleanup, apply and removeOrphaned operations are okay as is as long as we don't have
// concurrent reconciles of different DPUServices. As soon as we have concurrent reconciles, we may
// need to add a locking mechanism so at most one reconcile at a time handles these parts.
func (r *DPUServiceReconciler) reconcilePrivilegedPodEnforcement(ctx context.Context, dpuClusterClient client.Client, currentDPUService *dpuservicev1.DPUService, targetsCluster bool) error {
	// Cleanup the VAP and Configmap if the feature gate is disabled.
	if !features.Gates.Enabled(features.PrivilegedPodEnforcement) {
		return r.cleanupPrivilegedPodEnforcement(ctx, dpuClusterClient)
	}

	// In any case, apply the VAP and Binding.
	if err := r.applyPrivilegedPodPolicyVAP(ctx, dpuClusterClient); err != nil {
		return err
	}

	// Remove orphaned privileged DPUService entries from the allowlist ConfigMap.
	if err := r.removeOrphanedPrivilegedDPUServiceEntries(ctx, dpuClusterClient); err != nil {
		return err
	}

	allowed, fellBack := resolvePrivileged(currentDPUService)
	if fellBack && allowed && targetsCluster && currentDPUService.DeletionTimestamp.IsZero() {
		ctrllog.FromContext(ctx).V(4).Info(
			"DPUService allowlisted for privileged workloads via legacy fallback; set spec.security.privileged explicitly before upgrading to the next release",
			"dpuService", client.ObjectKeyFromObject(currentDPUService),
		)
	}
	shouldAllowPrivilegedPods := targetsCluster && currentDPUService.DeletionTimestamp.IsZero() && allowed
	return r.updatePrivilegedDPUServiceEntry(ctx, dpuClusterClient, currentDPUService, shouldAllowPrivilegedPods)
}

func (r *DPUServiceReconciler) applyPrivilegedPodPolicyVAP(ctx context.Context, dpuClusterClient client.Client) error {
	if err := utils.EnsureNamespace(ctx, dpuClusterClient, privilegedAllowlistConfigMapNamespace); err != nil {
		return fmt.Errorf("ensure %s namespace: %w", privilegedAllowlistConfigMapNamespace, err)
	}

	// Ensure the allowlist ConfigMap exists before applying the binding.
	// The binding's paramRef has parameterNotFoundAction: Deny, so a workload
	// admission between binding rollout and the first updatePrivilegedDPUServiceEntry
	// call would otherwise be denied. We create it empty here; per-service entries
	// are written by updatePrivilegedDPUServiceEntry and removed by the sweep.
	desiredCM := buildPrivilegedDPUServiceConfigMap()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredCM.Name,
			Namespace: desiredCM.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrPatch(ctx, dpuClusterClient, cm, func() error {
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		maps.Copy(cm.Labels, desiredCM.Labels)
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("create or patch allowlist ConfigMap: %w", err)
	}

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

	// Create or Patch ValidatingAdmissionPolicyBinding
	desiredBinding := privilegedPodsVAPBinding.DeepCopy()
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

	return nil
}

// removeOrphanedPrivilegedDPUServiceEntries removes entries from the allowlist
// ConfigMap whose owning DPUService no longer exists anywhere in the
// management cluster. It is called once per per-cluster reconcile (not from
// inside updatePrivilegedDPUServiceEntry's mutator) so concurrent reconciles
// of different DPUServices do not delete each other's just-added entries.
func (r *DPUServiceReconciler) removeOrphanedPrivilegedDPUServiceEntries(ctx context.Context, dpuClusterClient client.Client) error {
	allDPUServices := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, allDPUServices); err != nil {
		return fmt.Errorf("list all DPUServices: %w", err)
	}
	knownIDs := make(map[string]struct{}, len(allDPUServices.Items))
	for i := range allDPUServices.Items {
		if id := getDPUServiceID(&allDPUServices.Items[i]); id != "" {
			knownIDs[id] = struct{}{}
		}
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
		for id := range cm.Data {
			if _, ok := knownIDs[id]; !ok {
				delete(cm.Data, id)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("sweep orphaned allowlist entries: %w", err)
	}
	return nil
}

func (r *DPUServiceReconciler) updatePrivilegedDPUServiceEntry(ctx context.Context, dpuClusterClient client.Client, dpuService *dpuservicev1.DPUService, shouldAllow bool) error {
	serviceID := getDPUServiceID(dpuService)
	if serviceID == "" {
		if shouldAllow {
			return fmt.Errorf("DPUService %s has no service ID", client.ObjectKeyFromObject(dpuService))
		}
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
		// Either add or remove the entry from the ConfigMap.
		if shouldAllow {
			cm.Data[serviceID] = client.ObjectKeyFromObject(dpuService).String()
		} else {
			delete(cm.Data, serviceID)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create or patch ConfigMap: %w", err)
	}

	return nil
}

// cleanupPrivilegedPodEnforcement removes DPUService prerequisite resources from
// the DPU cluster by label.
func (r *DPUServiceReconciler) cleanupPrivilegedPodEnforcement(ctx context.Context, dpuClusterClient client.Client) error {
	errs := []error{}
	cleanupLabels := client.MatchingLabels{DPUServicePrereqLabel: dpuServicePrereqLabelValue}
	if err := dpuClusterClient.DeleteAllOf(ctx, &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}, cleanupLabels); err != nil {
		errs = append(errs, fmt.Errorf("delete ValidatingAdmissionPolicyBindings: %w", err))
	}
	if err := dpuClusterClient.DeleteAllOf(ctx, &admissionregistrationv1.ValidatingAdmissionPolicy{}, cleanupLabels); err != nil {
		errs = append(errs, fmt.Errorf("delete ValidatingAdmissionPolicies: %w", err))
	}
	if err := dpuClusterClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, cleanupLabels, client.InNamespace(privilegedAllowlistConfigMapNamespace)); err != nil {
		errs = append(errs, fmt.Errorf("delete ConfigMaps: %w", err))
	}
	return kerrors.NewAggregate(errs)
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
