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

package redfish

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/diag"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Installing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check if DPU deletion is requested during OS installation
	if !dpu.DeletionTimestamp.IsZero() {
		logger.Info("DPU deletion requested while in Installing state, cannot delete DPU during OS installation")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "CannotDeleteWhileInstalling", "Cannot delete DPU during OS installation. Wait for completion or timeout."))
	}

	// Check for installation timeout
	if err := checkInstallationTimeout(state, ctrlCtx.Options.OSInstallTimeout, logger); err != nil {
		// Best-effort: append a SEL rail hint so an operator can spot
		// ATX/PCIe power drops. No client is constructed yet here, so pass
		// nil and let the helper build one. Hint is folded into err because
		// cutil.NewCondition uses err.Error() as the condition message.
		if hint := bestEffortRailHint(ctx, dpu, ctrlCtx, logger, nil); hint != "" {
			err = fmt.Errorf("%s. %s", err.Error(), hint)
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "InstallationTimeout", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		return *state, err
	}

	client, err := rc.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		err = fmt.Errorf("failed to create TLS client: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	_, cond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondBFBTransferred))
	if cond == nil || cond.Status != metav1.ConditionTrue {
		return submitAndMonitorBfbInstallTask(ctx, dpu, ctrlCtx, client)
	}

	resp, system, err := client.GetSystem()
	if err != nil || resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("failed to get system: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToGetSystem", err.Error()))
		return *state, err
	}

	if system.BootProgress.OemLastState != "OsIsRunning" {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "OemLastState", system.BootProgress.OemLastState))
		return *state, nil
	}

	_, cond = cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	if cond == nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "BFBInstalled", "BFB installed, waiting for the DPU agent to start"))
		_, cond = cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	}

	// wait until the DPU agent is started
	if dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.LastStartupTime == nil {
		if time.Since(cond.LastTransitionTime.Time) > 20*time.Minute {
			err := fmt.Errorf("DPU agent not started after 20 minutes")
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "DPUAgentNotStarted", err.Error()))
			// NOTE: This differs slightly from the HLD, which specifies retrying Installing after
			// 20 minutes. Since the current Installing phase has no retry mechanism, we fall back
			// to reporting an Error directly.
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		logger.Info("DPU agent not started, waiting for it to start")
		return *state, nil
	}

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
	state.Phase = provisioningv1.DPUConfig
	logger.Info("installation finished")
	return *state, nil
}

func submitAndMonitorBfbInstallTask(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, client *rc.Client) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if dpu.Status.RedfishTaskID == nil {
		bfbRegistryAddr, err := getBFBRegistryAddress(ctx, ctrlCtx)
		if err != nil {
			err = fmt.Errorf("failed to get bfb-registry address: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToGetBFBRegistryAddress", err.Error()))
			return *state, err
		}
		logger.Info("submit BFB install task", "will use bfbRegistry", bfbRegistryAddr, "bfbFile", dpu.Status.BFBFile, "bfcfgFile", dpu.Status.BFCFGFile)
		resp, taskInfo, err := client.InstallBFB(concatBFBAndBFCFGPath(bfbRegistryAddr, dpu.Status.BFBFile, dpu.Status.BFCFGFile))
		if err != nil {
			err = fmt.Errorf("failed to install BFB: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToInstall", err.Error()))
			state.Phase = provisioningv1.DPUError
			// when we transition to ERROR phase, we should return nil to make sure the next Reconcile is trigger by the UPDATE event.
			// If an error is returned, the next Reconcile may be triggered as a retry, leading to installing BFB again.
			// todo: other phases trasitioning to ERROR phase should also follow this pattern
			return *state, nil
		} else if resp.StatusCode() == http.StatusBadRequest && strings.Contains(resp.String(), "Another update is in progress") {
			logger.Info("another update is in progress, waiting for it to finish", "dpuName", dpu.Name)
			return *state, nil
		} else if resp.StatusCode() != http.StatusAccepted {
			err = fmt.Errorf("get status: %s", resp.Status())
			logger.Error(err, "Failed to install BFB", "status", resp.Status(), "body", resp.String())
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToInstall", resp.String()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		// Update the state with the task ID so it's reflected in the returned status
		state.RedfishTaskID = &taskInfo.ID

		logger.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
		return *state, nil
	}

	// check progress
	resp, prog, err := client.CheckTaskProgress(*dpu.Status.RedfishTaskID)
	if err != nil {
		err = fmt.Errorf("failed to check task progress: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToCheckProgress", err.Error()))
		return *state, err
	} else if resp.StatusCode() != http.StatusOK {
		enrichedErr := buildNon200ProgressError(ctx, dpu, ctrlCtx, logger, client, resp)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), enrichedErr, "FailToCheckProgress", enrichedErr.Error()))
		return *state, enrichedErr
	}
	if prog.TaskState == "Exception" {
		taskErr := buildExceptionTaskError(ctx, dpu, ctrlCtx, logger, client, resp, prog)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), taskErr, "FailToInstall", taskErr.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	logger.Info(fmt.Sprintf("taskProgress: %+v", prog))
	if prog.PercentComplete < 100 {
		taskProgress := fmt.Sprintf("install task %d%% complete", prog.PercentComplete)
		cond := cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), nil, "TaskProgress", taskProgress)
		cond.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, cond)
		return *state, nil
	}

	state.RedfishTaskID = nil
	cond := cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), nil, "", "")
	cond.Status = metav1.ConditionTrue
	cutil.SetDPUCondition(state, cond)
	return *state, nil
}

// getBFBRegistryAddress returns the full bfb-registry address
func getBFBRegistryAddress(ctx context.Context, ctrlCtx *dutil.ControllerContext) (string, error) {
	if ctrlCtx.Options.BFBRegistryLoadBalancer != "" {
		return ctrlCtx.Options.BFBRegistryLoadBalancer, nil
	}
	return cutil.GetBFBRegistryAddressWithPort(ctx, ctrlCtx.Client, os.Getenv("POD_NAMESPACE"), ctrlCtx.Options.BFBRegistry)
}

// concatBFBAndBFCFGPath returns the bfb-registry path of concatenated bfbFile and bfcfgFile
// Given bfbFile is /bfb/file.bfb and bfcfgFile is /bfb/bfcfg/file.cfg,
// it returns /bfb/??file.bfb,bfcfg/file.cfg?/bfb-to-install
func concatBFBAndBFCFGPath(bfbRegistry string, bfbFile string, bfcfgFile string) string {
	schemes := []string{"http://", "https://"}
	for _, prefix := range schemes {
		bfbRegistry = strings.TrimPrefix(bfbRegistry, prefix)
	}
	bfCfg := strings.TrimPrefix(bfcfgFile, "/"+cutil.BFBBaseDir+"/")
	return filepath.Join(bfbRegistry, cutil.BFBBaseDir, fmt.Sprintf("??%s,%s?", filepath.Base(bfbFile), bfCfg), "bfb-to-install")
}

// responseBodyMaxChars trims a Redfish response body so we don't blow up the
// condition message field. ~1 KiB is enough to capture MessageId + a small
// JSON snippet; the full body is always logged at Error level for forensics.
const responseBodyMaxChars = 1024

// truncateForCondition trims a Redfish response body to responseBodyMaxChars.
func truncateForCondition(body string) string {
	if len(body) <= responseBodyMaxChars {
		return body
	}
	return body[:responseBodyMaxChars] + "...(truncated)"
}

// buildNon200ProgressError formats the enriched error for a non-200 response
// from CheckTaskProgress. Preserves the legacy "get status: ... is not OK"
// prefix for tools grepping on it; appends taskID, classified hint, optional
// rail hint (only on power-related statuses), and a truncated body. Also
// emits the full forensic detail to the logger.
func buildNon200ProgressError(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, logger logr.Logger, client *rc.Client, resp *resty.Response) error {
	status := resp.Status()
	body := resp.String()
	parts := []string{
		fmt.Sprintf("get status: %s is not OK", status),
		fmt.Sprintf("taskID=%s", *dpu.Status.RedfishTaskID),
	}
	if hint := diag.ClassifyHTTPStatus(resp.StatusCode(), body); hint != "" {
		parts = append(parts, "Hint: "+hint)
	}
	if diag.ShouldProbeRails(resp.StatusCode()) {
		if rh := bestEffortRailHint(ctx, dpu, ctrlCtx, logger, client); rh != "" {
			parts = append(parts, rh)
		}
	}
	if body != "" {
		parts = append(parts, "body: "+truncateForCondition(body))
	}
	enrichedErr := fmt.Errorf("%s", strings.Join(parts, ". "))
	logger.Error(enrichedErr, "CheckTaskProgress returned non-OK status",
		"taskID", *dpu.Status.RedfishTaskID,
		"status", status,
		"body", body)
	return enrichedErr
}

// buildExceptionTaskError formats the enriched error for a Redfish task that
// ended in TaskState=Exception. Preserves the legacy "Task X is in Exception
// state" prefix; appends a translated MessageId hint (with BMC Resolution
// when present) or the raw messages dump for unknown MessageIds, and a
// best-effort SEL rail hint (ATX-off can land in any MessageId). Also emits
// the full forensic detail to the logger.
func buildExceptionTaskError(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, logger logr.Logger, client *rc.Client, resp *resty.Response, prog *rc.TaskProgress) error {
	baseMsg := fmt.Sprintf("Task %s is in Exception state", *dpu.Status.RedfishTaskID)
	var trailers []string
	if translated, found := diag.TranslateTaskMessages(prog.Messages); found {
		trailers = append(trailers, fmt.Sprintf("Hint: %s (MessageId=%s)", translated.Hint, translated.MessageID))
		if translated.Resolution != "" {
			trailers = append(trailers, "BMC Resolution: "+translated.Resolution)
		}
	} else {
		// Unknown MessageId - keep raw-messages shape so we never regress
		// for messages we have not cataloged yet.
		trailers = append(trailers, fmt.Sprintf("messages=%v", prog.Messages))
	}
	if rh := bestEffortRailHint(ctx, dpu, ctrlCtx, logger, client); rh != "" {
		trailers = append(trailers, rh)
	}
	taskErr := fmt.Errorf("%s: %s", baseMsg, strings.Join(trailers, ". "))
	logger.Error(taskErr, "Failed to install BFB",
		"taskID", *dpu.Status.RedfishTaskID,
		"messages", prog.Messages,
		"body", resp.String())
	return taskErr
}

// railHintProbeTimeout caps the best-effort BMC SEL probe so a slow/unreachable
// BMC cannot delay the failure path. Propagated into the HTTP layer via the
// request context, so a blackholed BMC cannot keep the call alive past this
// budget.
const railHintProbeTimeout = 2 * time.Second

// railHintMaxEntries caps how many of the most recent SEL entries we scan for
// a low-rail event. Sensor threshold events are emitted only on crossings, so
// the relevant 12V_ATX / 12V_PCIe event for the current install attempt is
// almost always within the last few entries; 20 is a conservative window.
const railHintMaxEntries = 20

// bestEffortRailHint queries the BMC SEL for a recent low-rail event on the
// monitored power rails (12V_ATX, 12V_PCIe) and returns a short operator hint
// if one is found. All errors (DPUDevice fetch, client construction, Redfish
// call, missing fields) are silently swallowed - this is supplemental
// diagnostic context, never a hard requirement.
//
// If existingClient is non-nil, it is reused as-is to avoid the extra
// GetProductDescription verification round-trip that NewTLSClient performs.
// Callers in branches that already have a validated client (non-200,
// Exception) should pass it; the timeout branch passes nil.
func bestEffortRailHint(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, logger logr.Logger, existingClient *rc.Client) string {
	cl := existingClient
	if cl == nil {
		// NewTLSClient (incl. its GetProductDescription verification probe) is
		// bounded by the parent reconcile context, NOT by railHintProbeTimeout.
		// We accept this gap because the reconcile context is the natural upper
		// bound and tightening it would mask real TLS setup failures as opaque
		// "context deadline exceeded" entries in the V(1) log.
		device := &provisioningv1.DPUDevice{}
		if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
			logger.V(1).Info("rail hint probe: failed to fetch DPUDevice", "err", err)
			return ""
		}
		var err error
		cl, err = rc.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
		if err != nil {
			logger.V(1).Info("rail hint probe: failed to construct client", "err", err)
			return ""
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, railHintProbeTimeout)
	defer cancel()
	return probeRailHint(probeCtx, cl, logger)
}

// probeRailHint executes the SEL fetch + scan against an already-constructed
// Redfish client. Split out from bestEffortRailHint so tests can inject a
// mock client directly without going through the controller-runtime cache.
// Bounded by ctx (propagated into the HTTP layer via SetContext).
func probeRailHint(ctx context.Context, client *rc.Client, logger logr.Logger) string {
	resp, entries, err := client.GetSELEntries(ctx)
	if err != nil || entries == nil {
		var status string
		if resp != nil {
			status = resp.Status()
		}
		logger.V(1).Info("rail hint probe: GetSELEntries failed", "err", err, "status", status)
		return ""
	}

	// Walk entries newest-last (BMC returns oldest-first); cap scan window so
	// a long-lived BMC with thousands of entries cannot stall us.
	members := entries.Members
	start := 0
	if len(members) > railHintMaxEntries {
		start = len(members) - railHintMaxEntries
	}
	for i := len(members) - 1; i >= start; i-- {
		e := members[i]
		if e.MessageID != diag.SensorThresholdLowMessageID {
			continue
		}
		if len(e.MessageArgs) == 0 {
			continue
		}
		rail := e.MessageArgs[0]
		if _, ok := diag.PowerRailHints[rail]; !ok {
			continue
		}
		var reading, threshold string
		if len(e.MessageArgs) > 1 {
			reading = e.MessageArgs[1]
		}
		if len(e.MessageArgs) > 2 {
			threshold = e.MessageArgs[2]
		}
		return diag.FormatRailHint(rail, reading, threshold)
	}
	return ""
}

// checkInstallationTimeout checks if the OS installation has exceeded the configured timeout.
// Returns an error if timeout is exceeded, nil otherwise.
func checkInstallationTimeout(state *provisioningv1.DPUStatus, timeout time.Duration, logger logr.Logger) error {
	if timeout <= 0 {
		return nil
	}

	_, bfbPreparedCond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondBFBPrepared))
	if bfbPreparedCond == nil {
		return nil
	}

	elapsed := time.Since(bfbPreparedCond.LastTransitionTime.Time)
	if elapsed <= timeout {
		return nil
	}

	logger.Info("OS installation timeout exceeded", "elapsed", elapsed, "timeout", timeout)
	return fmt.Errorf("OS installation timeout exceeded: %v > %v", elapsed, timeout)
}
