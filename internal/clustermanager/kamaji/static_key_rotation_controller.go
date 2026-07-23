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

package nvidia

import (
	"context"
	"errors"
	"fmt"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"
	"github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// staticKeyStateIdle reports that no staticKey rotation is in progress.
	staticKeyStateIdle = string(encryptionconfig.PhaseIdle)
	// staticKeyStatePrepared reports that the config contains the active key followed by the new key.
	staticKeyStatePrepared = string(encryptionconfig.PhasePrepared)
	// staticKeyStatePromoted reports that the config contains the new key followed by the active key.
	staticKeyStatePromoted = string(encryptionconfig.PhasePromoted)
	// staticKeyStateMigrating reports that StorageVersionMigration is rewriting encrypted resources.
	staticKeyStateMigrating = "Migrating"
	// staticKeyStateFinalized reports that the config contains only the new key and final reload is pending.
	staticKeyStateFinalized = string(encryptionconfig.PhaseFinalized)
	// staticKeyStateDisabled reports that automatic rotation is disabled at a stable point.
	staticKeyStateDisabled = "Disabled"
	// staticKeyReasonBlocked reports that rotation is not in progress because it is blocked.
	staticKeyReasonBlocked = "Blocked"
	// staticKeyReasonNotBlocked reports that rotation is not blocked.
	staticKeyReasonNotBlocked = "NotBlocked"
)

// defaultStaticKeyRotationRequeueAfter controls how often rotation rechecks asynchronous reload and migration progress.
const defaultStaticKeyRotationRequeueAfter = 30 * time.Second

// reloadVerifier checks whether API servers have loaded a staticKey config.
type reloadVerifier interface {
	VerifyReload(context.Context, *provisioningv1.DPUCluster, encryptionconfig.StaticKey) (bool, error)
}

// staticKeyRotationResult carries condition updates produced by rotation reconciliation.
type staticKeyRotationResult struct {
	conditions []metav1.Condition
}

// staticKeyRotationContext carries the validated staticKey config wrapper for reconciliation.
type staticKeyRotationContext struct {
	config encryptionconfig.StaticKey
}

// staticKeyDesiredSource carries the current desired source key and observed metadata.
type staticKeyDesiredSource struct {
	source encryptionconfig.SourceKey
}

// reconcileStaticKeyRotation advances or reports staticKey rotation for an existing staticKey-encrypted DPUCluster.
func (cm *clusterHandler) reconcileStaticKeyRotation(ctx context.Context, dc *provisioningv1.DPUCluster) (*staticKeyRotationResult, error) {
	// Rotation resumes from state persisted in the encryption config Secret when the cluster becomes Ready again.
	if dc.Status.Phase != provisioningv1.PhaseReady {
		return nil, nil
	}

	rotationContext, result, err := cm.staticKeyRotationContext(ctx, dc)
	if result != nil || err != nil {
		return result, err
	}
	disabled, err := cm.isAutomaticStaticKeyRotationDisabled(ctx)
	if err != nil {
		return nil, err
	}
	if disabled {
		return cm.publishStaticKeyDisabled(dc)
	}

	switch rotationContext.config.Phase() {
	case encryptionconfig.PhaseIdle:
		return cm.reconcileStableStaticKey(ctx, dc, rotationContext)
	case encryptionconfig.PhasePrepared, encryptionconfig.PhasePromoted, encryptionconfig.PhaseFinalized:
		return cm.reconcileInFlightStaticKey(ctx, dc, rotationContext)
	default:
		return cm.publishStaticKeyBlocked(dc, "UnsupportedStaticKeyConfig", fmt.Sprintf("unsupported staticKey rotation state %q", rotationContext.config.Phase()))
	}
}

// staticKeyRotationContext gathers and validates the objects needed to reconcile staticKey rotation.
func (cm *clusterHandler) staticKeyRotationContext(ctx context.Context, dc *provisioningv1.DPUCluster) (*staticKeyRotationContext, *staticKeyRotationResult, error) {
	config, err := cm.encryptionConfigStore().Load(ctx, encryptionConfigSecretName(dc), dc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &staticKeyRotationResult{}, nil
		}
		var ownershipErr *encryptionconfig.OwnershipError
		if errors.As(err, &ownershipErr) {
			result, err := cm.publishStaticKeyBlocked(dc, "EncryptionConfigSecretOwnershipConflict", err.Error())
			return nil, result, err
		}
		var validationErr *encryptionconfig.ValidationError
		if errors.As(err, &validationErr) {
			result, err := cm.publishStaticKeyBlocked(dc, "EncryptionConfigSecretMalformed", err.Error())
			return nil, result, err
		}
		return nil, nil, fmt.Errorf("load staticKey encryption config: %w", err)
	}
	staticKey, ok := config.(encryptionconfig.StaticKey)
	if !ok {
		return nil, &staticKeyRotationResult{}, nil
	}

	ensureStaticKeyStatus(dc)
	activeRef := staticKey.ActiveKeyRef()
	dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef = &activeRef
	return &staticKeyRotationContext{
		config: staticKey,
	}, nil, nil
}

// reconcileStableStaticKey handles staticKey configs that contain exactly one key.
func (cm *clusterHandler) reconcileStableStaticKey(ctx context.Context, dc *provisioningv1.DPUCluster, rotationContext *staticKeyRotationContext) (*staticKeyRotationResult, error) {
	desired, result, err := cm.staticKeyDesiredSource(ctx, dc)
	if result != nil || err != nil {
		return result, err
	}
	next, err := rotationContext.config.TransitionToPrepared(desired.source)
	if err != nil {
		return cm.publishStaticKeyBlocked(dc, "InvalidStaticKey", err.Error())
	}
	return cm.saveStaticKeyConfig(ctx, dc, next)
}

// reconcileInFlightStaticKey handles staticKey configs that are already rotating.
func (cm *clusterHandler) reconcileInFlightStaticKey(ctx context.Context, dc *provisioningv1.DPUCluster, rotationContext *staticKeyRotationContext) (*staticKeyRotationResult, error) {
	config := rotationContext.config
	switch config.Phase() {
	case encryptionconfig.PhasePrepared:
		verified, err := cm.verifyStaticKeyReload(ctx, dc, config)
		if err != nil {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStatePrepared,
				fmt.Sprintf("waiting for all kube-apiserver instances to reload encryption config: %v", err))
		}
		if !verified {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStatePrepared, "waiting for all kube-apiserver instances to reload encryption config")
		}
		next, err := config.TransitionToPromoted()
		if err != nil {
			return nil, err
		}
		return cm.saveStaticKeyConfig(ctx, dc, next)
	case encryptionconfig.PhasePromoted:
		verified, err := cm.verifyStaticKeyReload(ctx, dc, config)
		if err != nil {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStatePromoted,
				fmt.Sprintf("waiting for all kube-apiserver instances to reload promoted encryption config: %v", err))
		}
		if !verified {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStatePromoted, "waiting for all kube-apiserver instances to reload promoted encryption config")
		}
		rotationID, err := config.RotationID()
		if err != nil {
			return nil, err
		}
		migrated, blocked, message, err := cm.reconcileStaticKeyStorageMigration(ctx, dc, rotationID)
		if err != nil {
			return nil, err
		}
		if blocked {
			return cm.publishStaticKeyBlocked(dc, "StorageVersionMigrationFailed", message)
		}
		if !migrated {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStateMigrating, message)
		}
		next, err := config.TransitionToFinalized()
		if err != nil {
			return nil, err
		}
		return cm.saveStaticKeyConfig(ctx, dc, next)
	case encryptionconfig.PhaseFinalized:
		verified, err := cm.verifyStaticKeyReload(ctx, dc, config)
		if err != nil {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStateFinalized,
				fmt.Sprintf("waiting for all kube-apiserver instances to reload finalized encryption config: %v", err))
		}
		if !verified {
			cm.scheduleStaticKeyRotation(dc)
			return cm.publishStaticKeyInProgress(dc, staticKeyStateFinalized, "waiting for all kube-apiserver instances to reload finalized encryption config")
		}
		next, err := config.TransitionToIdle()
		if err != nil {
			return nil, err
		}
		result, err := cm.saveStaticKeyConfig(ctx, dc, next)
		if err != nil {
			return nil, err
		}
		return result, nil
	default:
		return cm.publishStaticKeyBlocked(dc, "UnsupportedStaticKeyConfig", fmt.Sprintf("unsupported in-flight staticKey state %q", config.Phase()))
	}
}

// staticKeyDesiredSource resolves the current desired staticKey source only for idle configs.
func (cm *clusterHandler) staticKeyDesiredSource(ctx context.Context, dc *provisioningv1.DPUCluster) (*staticKeyDesiredSource, *staticKeyRotationResult, error) {
	cfg, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return nil, nil, fmt.Errorf("get DPFOperatorConfig for static key rotation: %w", err)
	}
	staticKey, ok := staticKeyConfiguration(cfg)
	if !ok {
		result, err := cm.publishStaticKeyBlocked(dc, "StaticKeySourceUnavailable", "current DPFOperatorConfig does not configure staticKey encryption")
		return nil, result, err
	}
	sourceSecret, keyBytes, err := cm.readSecretKey(ctx, cfg.Namespace, staticKey.KeySecretRef)
	if err != nil {
		var missingKeyErr *secretKeyNotFoundError
		if apierrors.IsNotFound(err) || errors.As(err, &missingKeyErr) {
			result, err := cm.publishStaticKeyBlocked(dc, "StaticKeySourceUnavailable", err.Error())
			return nil, result, err
		}
		return nil, nil, fmt.Errorf("read staticKey source Secret: %w", err)
	}
	return &staticKeyDesiredSource{
		source: encryptionconfig.SourceKey{
			Key: keyBytes,
			Ref: observedSecretKeyRef(sourceSecret, staticKey.KeySecretRef.Key),
		},
	}, nil, nil
}

// ensureStaticKeyStatus initializes the staticKey status section before writing observed key metadata.
func ensureStaticKeyStatus(dc *provisioningv1.DPUCluster) {
	if dc.Status.EtcdEncryptionAtRest == nil {
		dc.Status.EtcdEncryptionAtRest = &provisioningv1.DPUClusterEtcdEncryptionAtRestStatus{}
	}
	dc.Status.EtcdEncryptionAtRest.Provider = string(operatorv1.EtcdEncryptionProviderStaticKey)
	if dc.Status.EtcdEncryptionAtRest.StaticKey == nil {
		dc.Status.EtcdEncryptionAtRest.StaticKey = &provisioningv1.DPUClusterStaticKeyEncryptionStatus{}
	}
}

// staticKeyConfiguration returns the configured staticKey settings when staticKey EAR is selected.
func staticKeyConfiguration(cfg *operatorv1.DPFOperatorConfig) (*operatorv1.StaticKeyConfiguration, bool) {
	if cfg.Spec.KamajiClusterManager == nil ||
		cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest == nil ||
		cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest.Provider != operatorv1.EtcdEncryptionProviderStaticKey ||
		cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest.StaticKey == nil {
		return nil, false
	}
	return cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest.StaticKey, true
}

// saveStaticKeyConfig persists a transition and projects the reloaded active key metadata into status.
func (cm *clusterHandler) saveStaticKeyConfig(ctx context.Context, dc *provisioningv1.DPUCluster, next encryptionconfig.StaticKey) (*staticKeyRotationResult, error) {
	saved, err := cm.encryptionConfigStore().Save(ctx, next)
	if err != nil {
		return nil, err
	}
	staticKey, ok := saved.(encryptionconfig.StaticKey)
	if !ok {
		return nil, fmt.Errorf("saved staticKey config reloaded as %s", saved.Provider())
	}
	ensureStaticKeyStatus(dc)
	activeRef := staticKey.ActiveKeyRef()
	dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef = &activeRef
	log.FromContext(ctx).V(2).Info("advanced staticKey rotation",
		"dpuCluster", klog.KObj(dc),
		"toState", staticKey.Phase())
	if staticKey.Phase() == encryptionconfig.PhaseIdle {
		return cm.publishStaticKeyIdle(dc)
	}
	return cm.publishStaticKeyInProgress(dc, string(staticKey.Phase()), fmt.Sprintf("staticKey rotation advanced to %s", staticKey.Phase()))
}

// encryptionConfigStore returns the per-cluster encryption config Secret store.
func (cm *clusterHandler) encryptionConfigStore() *encryptionconfig.Store {
	return encryptionconfig.NewStore(cm.Client, cm.Scheme)
}

// verifyStaticKeyReload checks whether kube-apiserver instances loaded the latest encryption config write.
func (cm *clusterHandler) verifyStaticKeyReload(ctx context.Context, dc *provisioningv1.DPUCluster, config encryptionconfig.StaticKey) (bool, error) {
	if cm.reloadVerifier == nil {
		return false, nil
	}
	return cm.reloadVerifier.VerifyReload(ctx, dc, config)
}

// scheduleStaticKeyRotation requests a delayed reconcile for asynchronous rotation progress checks.
func (cm *clusterHandler) scheduleStaticKeyRotation(dc *provisioningv1.DPUCluster) {
	if cm.requeueScheduler != nil {
		cm.requeueScheduler.Schedule(types.NamespacedName{Namespace: dc.Namespace, Name: dc.Name}, cm.requeueAfter)
	}
}

// isAutomaticStaticKeyRotationDisabled reports whether automatic staticKey rotation is disabled.
// Rotation is enabled only when DPFOperatorConfig still selects staticKey and does not explicitly
// disable automatic rotation. Missing or non-staticKey settings disable rotation for existing
// staticKey-encrypted clusters without changing their persisted encryption config phase.
func (cm *clusterHandler) isAutomaticStaticKeyRotationDisabled(ctx context.Context) (bool, error) {
	cfg, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return false, fmt.Errorf("get DPFOperatorConfig for staticKey rotation disable flag: %w", err)
	}
	staticKey, ok := staticKeyConfiguration(cfg)
	if !ok {
		return true, nil
	}
	return ptr.Deref(staticKey.AutomaticRotationDisabled, false), nil
}

// publishStaticKeyIdle publishes the public conditions for a healthy idle rotation state.
func (cm *clusterHandler) publishStaticKeyIdle(dc *provisioningv1.DPUCluster) (*staticKeyRotationResult, error) {
	return cm.publishStaticKeyState(dc, "", staticKeyStateIdle, ""), nil
}

// publishStaticKeyDisabled publishes the public conditions for disabled automatic rotation.
func (cm *clusterHandler) publishStaticKeyDisabled(dc *provisioningv1.DPUCluster) (*staticKeyRotationResult, error) {
	return cm.publishStaticKeyBlocked(dc, staticKeyStateDisabled, "automatic staticKey rotation is disabled")
}

// publishStaticKeyBlocked publishes the public conditions for a blocked rotation state.
func (cm *clusterHandler) publishStaticKeyBlocked(dc *provisioningv1.DPUCluster, reason, message string) (*staticKeyRotationResult, error) {
	return cm.publishStaticKeyState(dc, provisioningv1.ConditionEtcdEncryptionRotationBlocked, reason, message), nil
}

// publishStaticKeyInProgress publishes the public conditions for an active rotation state.
func (cm *clusterHandler) publishStaticKeyInProgress(dc *provisioningv1.DPUCluster, reason, message string) (*staticKeyRotationResult, error) {
	return cm.publishStaticKeyState(dc, provisioningv1.ConditionEtcdEncryptionRotationInProgress, reason, message), nil
}

// publishStaticKeyState builds the rotation conditions and emits events for state changes.
func (cm *clusterHandler) publishStaticKeyState(dc *provisioningv1.DPUCluster, activeCondition provisioningv1.ConditionType, reason, message string) *staticKeyRotationResult {
	conditions := staticKeyConditions(activeCondition, reason, message)
	if dc != nil && staticKeyStateChanged(dc.Status.Conditions, conditions) {
		eventType := corev1.EventTypeNormal
		eventReason := "EtcdEncryptionRotation" + reason
		if activeCondition == provisioningv1.ConditionEtcdEncryptionRotationBlocked {
			eventType = corev1.EventTypeWarning
			eventReason = "EtcdEncryptionRotationBlocked"
		}
		cm.emitStaticKeyRotationEvent(dc, eventType, eventReason, message)
	}
	return &staticKeyRotationResult{conditions: conditions}
}

// staticKeyConditions returns the public staticKey rotation conditions with independent reasons and messages.
func staticKeyConditions(activeCondition provisioningv1.ConditionType, reason, message string) []metav1.Condition {
	inProgressCondition := metav1.Condition{
		Type:    string(provisioningv1.ConditionEtcdEncryptionRotationInProgress),
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
	blockedCondition := metav1.Condition{
		Type:   string(provisioningv1.ConditionEtcdEncryptionRotationBlocked),
		Status: metav1.ConditionFalse,
		Reason: staticKeyReasonNotBlocked,
	}

	switch activeCondition {
	case provisioningv1.ConditionEtcdEncryptionRotationInProgress:
		inProgressCondition.Status = metav1.ConditionTrue
		inProgressCondition.Reason = reason
		inProgressCondition.Message = message
	case provisioningv1.ConditionEtcdEncryptionRotationBlocked:
		inProgressCondition.Reason = staticKeyReasonBlocked
		inProgressCondition.Message = ""
		blockedCondition.Status = metav1.ConditionTrue
		blockedCondition.Reason = reason
		blockedCondition.Message = message
	}
	return []metav1.Condition{inProgressCondition, blockedCondition}
}

// staticKeyStateChanged reports whether the public rotation condition state changed.
func staticKeyStateChanged(current []metav1.Condition, desired []metav1.Condition) bool {
	for _, desiredCondition := range desired {
		currentCondition := meta.FindStatusCondition(current, desiredCondition.Type)
		if currentCondition == nil ||
			currentCondition.Status != desiredCondition.Status ||
			currentCondition.Reason != desiredCondition.Reason {
			return true
		}
	}
	return false
}

// emitStaticKeyRotationEvent emits a DPUCluster event when an event recorder is configured.
func (cm *clusterHandler) emitStaticKeyRotationEvent(dc *provisioningv1.DPUCluster, eventType, reason, message string) {
	if cm.recorder == nil {
		return
	}
	cm.recorder.Event(dc, eventType, reason, message)
}
