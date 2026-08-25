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
	"errors"
	"fmt"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/pkg/conditions"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// bmcFactoryResetSettleDelay is how long completion checks are suppressed after the reset was
// submitted. A BMC keeps answering for a few seconds after accepting ResetToDefaults and only then
// reboots, so probing immediately would read the pre-reset BMC as the post-reset one.
const bmcFactoryResetSettleDelay = 60 * time.Second

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

	log.Info("Resetting BMC to factory defaults")
	resp, _, err := privilegedClient.FactoryResetBMC()
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

	cred, err := rfclient.ResolveBMCCredential(ctx, dpuDevice.Namespace, dpuDevice.Spec.BMCCredentialSecretName, r.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve BMC credential: %w", err)
	}
	bmcAddress := dpuDevice.BMCAddress()

	if client, _, err := rfclient.VerifyBMCCredential(bmcAddress, cred.Password); err == nil {
		// VerifyBMCCredential treats PasswordChangeRequired as success so post-reset probing works,
		// but most Redfish endpoints — including ResetToDefaults — stay blocked in that state.
		// When the Secret still holds 0penBmc, there is no non-default password to write, so the
		// next FactoryResetBMC call would loop on 403. Point the operator at the Secret instead.
		// A factory-default Secret with a normal 200 OK Managers response is fine and can submit.
		if cred.Password == rfclient.BMCDefaultPassword {
			if resp, _, managersErr := client.GetManagers(); managersErr == nil && rfclient.PasswordChangeRequired(resp) {
				return nil, fmt.Errorf("BMC requires changing the factory default password before access is granted; set a non-default password in the credential Secret")
			}
		}
		return client, nil
	}

	defaultClient, user, err := rfclient.VerifyBMCCredential(bmcAddress, rfclient.BMCDefaultPassword)
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

	log.Info("BMC still holds the factory default password; setting the Secret password before the reset", "user", user)
	resp, _, err := defaultClient.SetRedfishUserPassword(user, cred.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to set the BMC password for account %q: %w", user, err)
	}
	if !rfclient.PasswordChangeAccepted(resp) {
		return nil, rfclient.AccountPasswordError(user, resp)
	}

	client, _, err := rfclient.VerifyBMCCredential(bmcAddress, cred.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to re-authenticate after setting the BMC password: %w", err)
	}
	return client, nil
}

// awaitBMCFactoryReset probes a BMC whose reset is in flight. There is no deadline: a BMC that has
// not come back is reported rather than given up on, because a deadline could not distinguish a
// reset that never landed from a BMC that is slow to return, and the only remedy it would enable —
// resubmitting — would wipe and reboot a device that has already reset.
func (r *DPUDeviceReconciler) awaitBMCFactoryReset(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) (bool, error) {
	log := log.FromContext(ctx)

	elapsed := time.Since(dpuDevice.Status.BMCFactoryResetRequestTime.Time)
	if elapsed < bmcFactoryResetSettleDelay {
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetInProgress,
			"ResetToDefaults accepted by the BMC; waiting for it to come back")
		setInitializedPending(dpuDevice, "BMC factory reset in progress")
		return true, nil
	}

	// The submission left the BMC on the Secret password, so the factory default answering now can
	// only mean the reset took effect. After ResetToDefaults a BlueField BMC typically accepts
	// 0penBmc but returns PasswordChangeRequired on Managers until hardening changes it; that
	// still counts as the factory default being in effect (see VerifyBMCCredential).
	if _, _, err := rfclient.VerifyBMCCredential(dpuDevice.BMCAddress(), rfclient.BMCDefaultPassword); err != nil {
		message := awaitBMCFactoryResetMessage(elapsed, err)
		setBMCFactoryResetCondition(dpuDevice, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetInProgress, message)
		setInitializedPending(dpuDevice, message)
		return true, nil
	}

	// The reset dropped the BMC's pending CSR key pair, so an existing server CertificateRequest is
	// unusable. setUpMTLS recovers from that on its own, but deleting it here avoids a
	// guaranteed-failing install round trip.
	if err := r.deleteServerCertRequest(ctx, dpuDevice); err != nil {
		return true, err
	}

	log.Info("BMC came back from the factory reset", "elapsed", elapsed.Round(time.Second))
	setBMCFactoryResetCondition(dpuDevice, metav1.ConditionTrue, provisioningv1.ReasonFactoryResetCompleted,
		"BMC was reset to factory defaults and is reachable again")
	return false, nil
}

// awaitBMCFactoryResetMessage reports why await is still waiting. ErrBMCPasswordRejected means the
// BMC answered and rejected the factory default — it is up, but the reset has not taken effect —
// so the message must not claim the BMC is still coming back. Every other error is treated as the
// BMC not giving a usable answer yet. The reason stays FactoryResetInProgress either way: the
// request time is already set, so DPF will not resubmit, and there is no safe automatic recovery.
func awaitBMCFactoryResetMessage(elapsed time.Duration, err error) string {
	rounded := elapsed.Round(time.Second)
	if errors.Is(err, rfclient.ErrBMCPasswordRejected) {
		return fmt.Sprintf("BMC is reachable but still rejects the factory default password after %s; "+
			"the reset may not have taken effect (DPF will not resubmit): %v", rounded, err)
	}
	return fmt.Sprintf("waiting for the BMC to come back from the factory reset, %s elapsed since the request: %v",
		rounded, err)
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
