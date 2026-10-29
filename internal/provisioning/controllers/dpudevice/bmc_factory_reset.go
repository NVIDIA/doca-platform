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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// bmcFactoryResetSettleDelay is how long completion checks are suppressed after the reset was
// submitted. A BMC keeps answering for a few seconds after accepting ResetToDefaults and only then
// reboots, so probing immediately would read the pre-reset BMC as the post-reset one.
const bmcFactoryResetSettleDelay = 60 * time.Second

const bmcFactoryResetPasswordsRejectedMessage = "BMC rejected the factory default password and the password in the credential Secret"

// reconcileBMCFactoryReset runs the one-per-device BMC factory reset step of initialization. It
// reports whether the caller must pause initialization and let the existing requeue drive the next
// pass, following the checkAndUpdateBmcFw convention of never sleeping in the reconcile loop.
//
// The decision to reset comes from the DPUDevice's own spec and nothing else: DPFOperatorConfig is
// not consulted here, because DPUDiscovery already stamped its value into the spec at creation.
func (r *DPUDeviceReconciler) reconcileBMCFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (stop bool, err error) {
	log := log.FromContext(ctx)

	condition := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceBMCFactoryResetReady)
	if condition != nil && condition.Status == metav1.ConditionTrue {
		if condition.ObservedGeneration != dpuDevice.GetGeneration() {
			// Refresh the stale generation in place. meta.SetStatusCondition only re-stamps
			// lastTransitionTime when the status changes, so the recorded completion time survives.
			setBMCFactoryResetCondition(dpuDevice, metav1.ConditionTrue, condition.Reason, condition.Message)
		}
		return false, nil
	}

	if dpuDevice.Status.BMCFactoryResetRequestTime != nil {
		// Persist cleanup protection before a probe can leave a rejection marker on the Secret.
		if !controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.BMCCredentialFinalizer) {
			protected := dpuDevice.DeepCopy()
			patch := client.MergeFromWithOptions(protected.DeepCopy(), client.MergeFromWithOptimisticLock{})
			controllerutil.AddFinalizer(protected, provisioningv1.BMCCredentialFinalizer)
			if err := r.Client.Patch(ctx, protected, patch); err != nil {
				return true, fmt.Errorf("failed to protect BMC factory-reset annotation cleanup: %w", err)
			}
			dpuDevice.Finalizers = protected.Finalizers
			dpuDevice.ResourceVersion = protected.ResourceVersion
		}
		return r.awaitBMCFactoryReset(ctx, dpuDevice)
	}

	if reason := alreadyManagedMarker(dpuDevice); reason != "" {
		log.Info("Skipping BMC factory reset for a device DPF already manages", "marker", reason)
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionTrue, provisioningv1.ReasonFactoryResetSkipped,
			fmt.Sprintf("DPUDevice predates the BMC factory reset feature (%s); the BMC was not reset", reason))
		return false, nil
	}

	if provisioningv1.GetBMCFactoryResetPolicy(dpuDevice.Spec.BMCFactoryResetPolicy) == provisioningv1.BMCFactoryResetPolicyNever {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionTrue, provisioningv1.ReasonFactoryResetSkipped,
			"spec.bmcFactoryResetPolicy is Never; the BMC was not reset")
		return false, nil
	}

	return r.submitBMCFactoryReset(ctx, dpuDevice)
}

// alreadyManagedMarker names the evidence, if any, that DPF already manages this device, and so
// that it was onboarded before this feature existed. Such a device must never be reset by an
// upgrade, even though the new CRD default makes its policy read as OnInitialization. A freshly
// created DPUDevice carries none of these markers on its first reconcile.
func alreadyManagedMarker(dpuDevice *provisioningv1.DPUDevice) string {
	if dpuDevice.Status.BMCCredentialSecretName != nil && *dpuDevice.Status.BMCCredentialSecretName != "" {
		return "status.bmcCredentialSecretName is set"
	}
	if c := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceDiscovered); c != nil && c.Status == metav1.ConditionTrue {
		return "Discovered is True"
	}
	if dpuDevice.Status.DPUType != "" && dpuDevice.Status.DPUType != provisioningv1.DPUTypeUnknown {
		return "status.dpuType is set"
	}
	return ""
}

// submitBMCFactoryReset obtains a privileged session and submits ResetToDefaults, stamping the
// request time. The request time is the only thing standing between a device and a second factory
// reset, so it is persisted as soon as it is set and never cleared afterwards.
func (r *DPUDeviceReconciler) submitBMCFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (bool, error) {
	log := log.FromContext(ctx)

	privilegedClient, err := r.privilegedClientForFactoryReset(ctx, dpuDevice)
	if err != nil {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, err.Error())
		setInitializedPending(dpuDevice, "BMC factory reset cannot start: "+err.Error())
		return true, nil
	}

	defer privilegedClient.CloseIdleConnections()
	log.Info("Resetting BMC to factory defaults")
	resp, _, err := privilegedClient.FactoryResetBMC(ctx)
	if err != nil {
		message := fmt.Sprintf("failed to submit ResetToDefaults to the BMC: %v", err)
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}
	// A rejected submission means the BMC did nothing, so the request time stays nil and the next
	// pass submits again rather than parking the device.
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNoContent {
		message := fmt.Sprintf("BMC rejected ResetToDefaults with status %s", resp.Status())
		if msgs := rfclient.ErrorMessages(string(resp.Body())); len(msgs) > 0 {
			message = fmt.Sprintf("BMC rejected ResetToDefaults: %s", msgs[0])
		}
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}

	dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.Now())
	setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetInProgress,
		"ResetToDefaults accepted by the BMC; waiting for it to come back")
	setInitializedPending(dpuDevice, "BMC factory reset in progress")

	// The request time is written the moment the BMC accepts, rather than being left to the patch
	// deferred to the end of the reconcile: that patch can still fail, and a request time that
	// never reached the API server would send the returning BMC through a second wipe and reboot.
	if err := r.persistBMCFactoryResetRequestTime(ctx, dpuDevice); err != nil {
		log.Error(err, "Failed to record the BMC factory reset request time; the deferred patch is the remaining chance to record it")
	}
	return true, nil
}

// persistBMCFactoryResetRequestTime writes status.bmcFactoryResetRequestTime on its own, touching
// no other field, so that recording the reset does not depend on the rest of the reconcile.
//
// The patch is sent through a copy: the API server answers with the stored object, and letting that
// land on the object in hand would drop everything this pass has changed but not yet patched.
func (r *DPUDeviceReconciler) persistBMCFactoryResetRequestTime(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	stamped := dpuDevice.DeepCopy()
	before := dpuDevice.DeepCopy()
	before.Status.BMCFactoryResetRequestTime = nil
	return r.Client.Status().Patch(ctx, stamped, client.MergeFrom(before))
}

// privilegedClientForFactoryReset returns a Redfish client authorized to submit ResetToDefaults.
//
// It prefers the password from the credential Secret. When only the factory default password works,
// it writes the Secret password to the Redfish user and re-authenticates, which is what makes the
// step restartable: a crash between that write and the submission leaves the BMC on a password this
// same helper tries first on the next pass. Only the Redfish user is written, because the reset
// discards it again moments later and full hardening is resolveAndAuthenticateBMC's job.
func (r *DPUDeviceReconciler) privilegedClientForFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (*rfclient.Client, error) {
	log := log.FromContext(ctx)
	readCtx, cancelRead := rfclient.ReadContext(ctx)
	defer cancelRead()

	cred, err := rfclient.ResolveBMCCredential(readCtx, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve BMC credential: %w", err)
	}
	bmcAddress := dpuDevice.BMCAddress()

	if client, _, err := rfclient.VerifyBMCCredential(readCtx, bmcAddress, cred.Password); err == nil {
		// VerifyBMCCredential treats PasswordChangeRequired as success so post-reset probing works,
		// but most Redfish endpoints — including ResetToDefaults — stay blocked in that state.
		// When the Secret still holds 0penBmc, there is no non-default password to write, so the
		// next FactoryResetBMC call would loop on 403. Point the operator at the Secret instead.
		// A factory-default Secret with a normal 200 OK Managers response is fine and can submit.
		if cred.Password == rfclient.BMCDefaultPassword {
			resp, _, managersErr := client.GetManagers(readCtx)
			if rfclient.HasHTTPStatus(managersErr, http.StatusForbidden) && rfclient.PasswordChangeRequired(resp) {
				client.CloseIdleConnections()
				return nil, fmt.Errorf("BMC requires changing the factory default password before access is granted; set a non-default password in the credential Secret")
			}
			if managersErr != nil {
				client.CloseIdleConnections()
				return nil, fmt.Errorf("failed to verify BMC Managers before factory reset: %w", managersErr)
			}
		}
		return client, nil
	} else if !errors.Is(err, rfclient.ErrBMCPasswordRejected) {
		return nil, fmt.Errorf("the BMC at %s is not answering with a usable credential response, so the factory reset cannot start: %w", bmcAddress, err)
	}

	defaultClient, user, err := rfclient.VerifyBMCCredential(readCtx, bmcAddress, rfclient.BMCDefaultPassword)
	if err != nil {
		// Only a BMC that answered and turned both passwords down is a credential problem. A BMC
		// that is still booting, or unreachable, must not send the operator off to edit a Secret
		// that is perfectly correct.
		if !errors.Is(err, rfclient.ErrBMCPasswordRejected) {
			return nil, fmt.Errorf("the BMC at %s is not answering, so the factory reset cannot start: %w", bmcAddress, err)
		}
		return nil, fmt.Errorf("the BMC accepts neither the password in the credential Secret nor the factory default: "+
			"correct the Secret or reset the BMC out of band, then the reset will proceed (%w)", err)
	}
	defer defaultClient.CloseIdleConnections()

	log.Info("BMC still holds the factory default password; setting the Secret password before the reset", "user", user)
	resp, _, err := defaultClient.SetRedfishUserPassword(ctx, user, cred.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to set the BMC password for account %q: %w", user, err)
	}
	if !rfclient.PasswordChangeAccepted(resp) {
		return nil, rfclient.AccountPasswordError(user, resp)
	}

	client, _, err := rfclient.VerifyBMCCredential(ctx, bmcAddress, cred.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to re-authenticate after setting the BMC password: %w", err)
	}
	return client, nil
}

// awaitBMCFactoryReset probes a BMC whose reset is in flight. A BMC that is not answering is waited
// on, because resubmitting ResetToDefaults could wipe a device that has already reset. A BMC that
// answers is tried with the factory default password. If that is rejected but the pre-reset Secret
// password still works, later passes keep using the Secret password until the accepted reset
// starts, and only then try the factory default once. A recorded rejection prevents the Secret
// password from being sent again until that Secret changes or a retry is requested. A retry after
// FactoryResetFailed sends the factory default again to detect an out-of-band reset.
func (r *DPUDeviceReconciler) awaitBMCFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (bool, error) {
	log := log.FromContext(ctx)
	condition := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceBMCFactoryResetReady)
	elapsed := time.Since(dpuDevice.Status.BMCFactoryResetRequestTime.Time)

	// FactoryResetFailed while a request time is set means both passwords were already rejected.
	// Probe again only when the Secret password no longer matches the rejected marker. That retry
	// sends the factory default again; it does not send the rejected Secret password. ResetToDefaults
	// is not submitted again: the request time stays set.
	// A failed marker write requires an explicit retry because the rejected password is unknown.
	if condition != nil && (condition.Reason == provisioningv1.ReasonFactoryResetFailed || condition.Reason == provisioningv1.ReasonFactoryResetCredentialRecordFailed) {
		retry, err := r.factoryResetRetryAllowed(ctx, dpuDevice)
		if err != nil {
			return true, err
		}
		if !retry {
			return true, nil
		}
		log.Info("Retrying BMC factory-reset completion probes after a credential change or explicit retry")
	}

	if elapsed < bmcFactoryResetSettleDelay {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetInProgress,
			"ResetToDefaults accepted by the BMC; waiting for it to come back")
		setInitializedPending(dpuDevice, "BMC factory reset in progress")
		return true, nil
	}

	// The Secret password was already rejected. Retry only the factory default unless the Secret
	// password has changed since that rejection; probing the same Secret again is another failed login.
	// That check and the probe read the Secret separately. The probe compares the credential it
	// actually sends, so a rollback between the two reads cannot log in with the rejected password.
	if condition != nil && condition.Reason == provisioningv1.ReasonFactoryResetCredentialRejected {
		retry, err := r.factoryResetRetryAllowed(ctx, dpuDevice)
		if err != nil {
			return true, err
		}
		if retry {
			return r.probeDelayedBMCFactoryReset(ctx, dpuDevice, elapsed, false)
		}
		return r.awaitFactoryDefaultAfterRejectedSecret(ctx, dpuDevice, elapsed)
	}

	if condition != nil && condition.Reason == provisioningv1.ReasonFactoryResetAwaitingCompletion {
		return r.probeDelayedBMCFactoryReset(ctx, dpuDevice, elapsed, false)
	}

	// After ResetToDefaults a BlueField BMC typically accepts 0penBmc but returns
	// PasswordChangeRequired on Managers until hardening changes it; that still counts as the
	// factory default being in effect (see VerifyBMCCredential).
	bmcAddress := dpuDevice.BMCAddress()
	client, _, err := rfclient.VerifyBMCCredential(ctx, bmcAddress, rfclient.BMCDefaultPassword)
	if err == nil {
		client.CloseIdleConnections()
		return r.completeBMCFactoryReset(ctx, dpuDevice, elapsed)
	}
	if !errors.Is(err, rfclient.ErrBMCPasswordRejected) {
		message := fmt.Sprintf("waiting for the BMC to come back from the factory reset, %s elapsed since the request: %v",
			elapsed.Round(time.Second), err)
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetInProgress, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}

	return r.probeDelayedBMCFactoryReset(ctx, dpuDevice, elapsed, true)
}

// awaitFactoryDefaultAfterRejectedSecret retries only the factory default password. The Secret
// password was already rejected, so a timeout must leave that fact in place for the next pass.
func (r *DPUDeviceReconciler) awaitFactoryDefaultAfterRejectedSecret(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, elapsed time.Duration) (bool, error) {
	client, _, err := rfclient.VerifyBMCCredential(ctx, dpuDevice.BMCAddress(), rfclient.BMCDefaultPassword)
	if err == nil {
		client.CloseIdleConnections()
		return r.completeBMCFactoryReset(ctx, dpuDevice, elapsed)
	}
	if errors.Is(err, rfclient.ErrBMCPasswordRejected) {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, bmcFactoryResetPasswordsRejectedMessage)
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
			conditions.ReasonError, conditions.ConditionMessage(bmcFactoryResetPasswordsRejectedMessage))
		return true, nil
	}
	message := fmt.Sprintf("waiting for the BMC to come back from the delayed factory reset, %s elapsed since the request: %v",
		elapsed.Round(time.Second), err)
	setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetCredentialRejected, message)
	setInitializedPending(dpuDevice, message)
	return true, nil
}

// probeDelayedBMCFactoryReset checks the Secret credential and only tries the factory default if it has not already been rejected.
func (r *DPUDeviceReconciler) probeDelayedBMCFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, elapsed time.Duration, defaultAlreadyRejected bool) (bool, error) {
	bmcAddress := dpuDevice.BMCAddress()
	cred, err := rfclient.ResolveBMCCredential(ctx, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
	if err != nil {
		message := fmt.Sprintf("waiting for the delayed BMC factory reset; failed to read the credential Secret: %v", err)
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetAwaitingCompletion, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}
	// A Secret that holds 0penBmc is the factory-default probe. Fail only after this pass already
	// had that password rejected; otherwise probe it once. Sending it again would be a second
	// failed login. Record the password already read so a later Secret edit is not mistaken for
	// the rejected password.
	if cred.Password == rfclient.BMCDefaultPassword {
		if err := r.rememberRejectedFactoryResetSecret(ctx, dpuDevice, cred); err != nil {
			setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetCredentialRecordFailed, err.Error())
			setInitializedPending(dpuDevice, err.Error())
			return true, err
		}
		if defaultAlreadyRejected {
			message := "BMC rejected the factory default password and the credential Secret holds that same password"
			setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, message)
			conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
				conditions.ReasonError, conditions.ConditionMessage(message))
			return true, nil
		}
		return r.awaitFactoryDefaultAfterRejectedSecret(ctx, dpuDevice, elapsed)
	}
	// factoryResetRetryAllowed and this read are separate. A Secret change that is rolled
	// back in between restores the rejected password; compare the credential about to be sent and
	// skip it. Another failed login with that password can lock the BMC account.
	if rejectedFactoryResetCredential(dpuDevice, cred) {
		if defaultAlreadyRejected {
			setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, bmcFactoryResetPasswordsRejectedMessage)
			conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
				conditions.ReasonError, conditions.ConditionMessage(bmcFactoryResetPasswordsRejectedMessage))
			return true, nil
		}
		return r.awaitFactoryDefaultAfterRejectedSecret(ctx, dpuDevice, elapsed)
	}
	client, _, err := rfclient.VerifyBMCCredential(ctx, bmcAddress, cred.Password)
	if err == nil {
		client.CloseIdleConnections()
		message := fmt.Sprintf("ResetToDefaults was accepted but the credential Secret password still works after %s; waiting for the reset to start",
			elapsed.Round(time.Second))
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetAwaitingCompletion, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}
	if !errors.Is(err, rfclient.ErrBMCPasswordRejected) {
		message := fmt.Sprintf("waiting for the delayed BMC factory reset, %s elapsed since the request: %v",
			elapsed.Round(time.Second), err)
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetAwaitingCompletion, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}

	if err := r.rememberRejectedFactoryResetSecret(ctx, dpuDevice, cred); err != nil {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetCredentialRecordFailed, err.Error())
		setInitializedPending(dpuDevice, err.Error())
		return true, err
	}
	if defaultAlreadyRejected {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, bmcFactoryResetPasswordsRejectedMessage)
		conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
			conditions.ReasonError, conditions.ConditionMessage(bmcFactoryResetPasswordsRejectedMessage))
		return true, nil
	}

	return r.awaitFactoryDefaultAfterRejectedSecret(ctx, dpuDevice, elapsed)
}

// rejectedFactoryResetCredential reports whether cred is the Secret password the BMC already refused.
func rejectedFactoryResetCredential(dpuDevice *provisioningv1.DPUDevice, cred *rfclient.BMCCredentialResult) bool {
	if dpuDevice == nil || cred == nil || dpuDevice.UID == "" {
		return false
	}
	stored := cred.Annotations[rejectedPasswordAnnotationKey(dpuDevice.UID)]
	return stored != "" && stored == passwordDigest(cred.Password)
}

// factoryResetRetryAllowed accepts a changed password or consumes an explicit retry request.
// After a failed marker write, only an explicit retry is safe: the old digest may be stale.
func (r *DPUDeviceReconciler) factoryResetRetryAllowed(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (bool, error) {
	cred, err := rfclient.ResolveBMCCredential(ctx, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
	if err != nil {
		return false, fmt.Errorf("failed to check BMC factory-reset retry: %w", err)
	}
	marker := cred.Annotations[rejectedPasswordAnnotationKey(dpuDevice.UID)]
	if marker == provisioningv1.BMCFactoryResetRetryAnnotationValue {
		if err := r.clearRejectedFactoryResetSecret(ctx, dpuDevice); err != nil {
			return false, fmt.Errorf("failed to consume BMC factory-reset retry: %w", err)
		}
		return true, nil
	}
	condition := conditions.Get(dpuDevice, provisioningv1.ConditionDpuDeviceBMCFactoryResetReady)
	if condition != nil && condition.Reason == provisioningv1.ReasonFactoryResetCredentialRecordFailed {
		return false, nil
	}
	return marker != "" && !rejectedFactoryResetCredential(dpuDevice, cred), nil
}

// rememberRejectedFactoryResetSecret writes the rejected-password annotation before this pass returns.
// The value is the digest of the password that was probed, not of whatever the Secret holds by the
// time the patch is built: an edit after the probe must not be recorded as a password the BMC refused.
func (r *DPUDeviceReconciler) rememberRejectedFactoryResetSecret(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, cred *rfclient.BMCCredentialResult) error {
	if cred == nil || cred.SecretName == "" || dpuDevice.UID == "" {
		return fmt.Errorf("cannot record a rejected BMC password without the credential Secret and the DPUDevice UID")
	}
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: cred.SecretName, Namespace: dpuDevice.Namespace}, secret); err != nil {
		return fmt.Errorf("failed to get credential secret %q: %w", cred.SecretName, err)
	}
	key := rejectedPasswordAnnotationKey(dpuDevice.UID)
	digest := passwordDigest(cred.Password)
	if secret.Annotations[key] == digest {
		return nil
	}
	patch := client.MergeFrom(secret.DeepCopy())
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[key] = digest
	if err := r.Client.Patch(ctx, secret, patch); err != nil {
		return fmt.Errorf("failed to record the rejected BMC password on secret %q: %w", cred.SecretName, err)
	}
	return nil
}

// clearRejectedFactoryResetSecret removes the annotation after completion, on deletion, or to consume an explicit retry.
func (r *DPUDeviceReconciler) clearRejectedFactoryResetSecret(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	if dpuDevice.UID == "" {
		return nil
	}
	secretName := rfclient.BMCPasswordSecret
	if dpuDevice.Spec.BMCCredentialSecretName != nil && *dpuDevice.Spec.BMCCredentialSecretName != "" {
		secretName = *dpuDevice.Spec.BMCCredentialSecretName
	}
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: dpuDevice.Namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	key := rejectedPasswordAnnotationKey(dpuDevice.UID)
	if _, ok := secret.Annotations[key]; !ok {
		return nil
	}
	patch := client.MergeFrom(secret.DeepCopy())
	delete(secret.Annotations, key)
	return r.Client.Patch(ctx, secret, patch)
}

// rejectedPasswordAnnotationKey scopes the rejection marker to one DPUDevice.
func rejectedPasswordAnnotationKey(deviceUID types.UID) string {
	return provisioningv1.BMCFactoryResetRejectedPasswordAnnotationPrefix + string(deviceUID)
}

// passwordDigest returns the hex SHA-256 digest stored on the credential Secret.
func passwordDigest(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// completeBMCFactoryReset records that the factory default password is in effect.
func (r *DPUDeviceReconciler) completeBMCFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice, elapsed time.Duration) (bool, error) {
	log := log.FromContext(ctx)
	// The reset wiped the BMC's truststore and server certificate, so every cached client built
	// against the pre-reset BMC is obsolete regardless of which family or device UID it was keyed under.
	r.RedfishClients.RetireEndpoint(dpuDevice.BMCAddress())
	// The reset dropped the BMC's pending CSR key pair, so an existing server CertificateRequest is
	// unusable. setUpMTLS recovers from that on its own, but deleting it here avoids a
	// guaranteed-failing install round trip.
	if err := r.deleteServerCertRequest(ctx, dpuDevice); err != nil {
		return true, err
	}

	if err := r.clearRejectedFactoryResetSecret(ctx, dpuDevice); err != nil {
		log.Error(err, "Failed to clear the rejected BMC password annotation")
	}
	log.Info("BMC came back from the factory reset", "elapsed", elapsed.Round(time.Second))
	setBMCFactoryResetCondition(dpuDevice, metav1.ConditionTrue, provisioningv1.ReasonFactoryResetCompleted,
		"BMC was reset to factory defaults and is reachable again")
	return false, nil
}

// deleteServerCertRequest removes the device's server CertificateRequest, if any.
func (r *DPUDeviceReconciler) deleteServerCertRequest(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	cr := newServerCertRequest(dpuDevice)
	if err := r.Client.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete server CertificateRequest after the BMC factory reset: %w", err)
	}
	return nil
}

// setBMCFactoryResetCondition writes the BMCFactoryResetReady condition. meta.SetStatusCondition
// re-stamps lastTransitionTime only when the status changes, so a True condition refreshed for a
// stale generation keeps the completion time it already recorded.
func setBMCFactoryResetCondition(dpuDevice *provisioningv1.DPUDevice, status metav1.ConditionStatus, reason, message string) {
	conditionsList := dpuDevice.GetConditions()
	meta.SetStatusCondition(&conditionsList, metav1.Condition{
		Type:               string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady),
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dpuDevice.GetGeneration(),
	})
	dpuDevice.SetConditions(conditionsList)
}

// setInitializedPending reports that initialization is paused on a step that is still running,
// matching the "BMC firmware update in progress" pattern in checkAndUpdateBmcFw.
func setInitializedPending(dpuDevice *provisioningv1.DPUDevice, message string) {
	conditions.AddFalse(dpuDevice, provisioningv1.ConditionDpuDeviceInitialized,
		conditions.ReasonPending, conditions.ConditionMessage(message))
}
