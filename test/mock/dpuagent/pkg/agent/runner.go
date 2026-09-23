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

package agent

import (
	"context"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	dpuutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultRetryInterval = 30 * time.Second
	statusRetryInterval  = 2 * time.Second
)

// Runner is the operation pipeline of internal/provisioning/dpuagent DPUAgent.Run without the
// bootstrap-abort checks and the done marker: it walks the operations in order, retries a failing
// one every 30 seconds, records the per-operation condition and patches status.agentStatus of
// the DPU when an operation asks for it or fails.
type Runner struct {
	optCtx        *operations.Context
	operations    []operations.Operation
	retryInterval time.Duration
}

// NewRunner builds a runner for one agent run.
func NewRunner(optCtx *operations.Context, ops []operations.Operation) *Runner {
	return &Runner{optCtx: optCtx, operations: ops, retryInterval: defaultRetryInterval}
}

// Run executes the pipeline. It returns nil after the final status patch, or the context error when
// the run was interrupted (reboot, reprovision, shutdown).
func (r *Runner) Run(ctx context.Context) error {
	r.optCtx.UpdateStatusUntilSuccess = r.updateStatusUntilSuccess
	// The mock always behaves like an agent on the device-query reboot path.
	r.optCtx.RebootMethodDiscovery = true
	r.optCtx.Status = provisioningv1.AgentStatus{
		Conditions:   []metav1.Condition{},
		RebootMethod: ptr.To(provisioningv1.RebootMethodUnknown),
	}
	// There is no /proc/sys/kernel/random/boot_id for a DPU that does not exist; a fresh UUID per
	// run is what a rebooted kernel would produce.
	r.optCtx.CurrentBootID = uuid.NewString()

	for _, op := range r.operations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.ShouldSkip(r.optCtx) {
			klog.InfoS("skipping operation", "operation", op.Name())
			continue
		}
		err := wait.PollUntilContextCancel(ctx, r.retryInterval, true, func(execCtx context.Context) (bool, error) {
			r.optCtx.CondMessage = ""
			err := op.Execute(execCtx, r.optCtx)
			if err != nil {
				if execCtx.Err() != nil {
					return false, execCtx.Err()
				}
				klog.ErrorS(err, "operation failed, retrying", "operation", op.Name())
				hostutil.NewCondition(op.ConditionType()).Failure(err, "FailedToExecute").Set(&r.optCtx.Status.Conditions)
			} else {
				klog.InfoS("operation succeeded", "operation", op.Name())
				hostutil.NewCondition(op.ConditionType()).Success(dpuutil.TruncateConditionMessage(r.optCtx.CondMessage)).Set(&r.optCtx.Status.Conditions)
			}
			if err != nil || op.ShouldUpdateStatusBeforeContinue(r.optCtx) {
				if updateErr := r.updateStatusUntilSuccess(execCtx); updateErr != nil {
					return false, updateErr
				}
			}
			return err == nil, nil
		})
		if err != nil {
			return fmt.Errorf("execution of operation %s aborted: %w", op.Name(), err)
		}
	}
	return r.updateStatusUntilSuccess(ctx)
}

// updateStatusUntilSuccess patches status.agentStatus until it succeeds or ctx ends.
func (r *Runner) updateStatusUntilSuccess(ctx context.Context) error {
	return wait.PollUntilContextCancel(ctx, statusRetryInterval, true, func(updateCtx context.Context) (bool, error) {
		if err := r.updateStatus(updateCtx); err != nil {
			if updateCtx.Err() != nil {
				return false, updateCtx.Err()
			}
			klog.ErrorS(err, "failed to update DPU status, retrying")
			return false, nil
		}
		return true, nil
	})
}

// updateStatus is DPUAgent.updateStatus: fetch the latest DPU, reject a stale UID and merge the
// in-memory agent status into status.agentStatus with a merge patch.
func (r *Runner) updateStatus(ctx context.Context) error {
	latestDPU := &provisioningv1.DPU{}
	key := client.ObjectKey{Namespace: r.optCtx.Options.DPUNamespace, Name: r.optCtx.Options.DPUName}
	if err := r.optCtx.Client.Get(ctx, key, latestDPU); err != nil {
		return err
	}
	if string(latestDPU.UID) != r.optCtx.Options.DPUUID {
		return fmt.Errorf("stale DPU object: expected UID %s but got %s", r.optCtx.Options.DPUUID, latestDPU.UID)
	}
	patch := client.MergeFrom(latestDPU.DeepCopy())
	if latestDPU.Status.AgentStatus == nil {
		latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}
	}
	agentStatus := r.optCtx.Status
	if agentStatus.LastStartupTime != nil {
		latestDPU.Status.AgentStatus.LastStartupTime = agentStatus.LastStartupTime
	}
	if agentStatus.InitialBootID != nil {
		latestDPU.Status.AgentStatus.InitialBootID = agentStatus.InitialBootID
	}
	if agentStatus.RebootMethod != nil {
		latestDPU.Status.AgentStatus.RebootMethod = agentStatus.RebootMethod
	}
	if agentStatus.RebootSequenceCount != nil {
		latestDPU.Status.AgentStatus.RebootSequenceCount = agentStatus.RebootSequenceCount
	}
	if agentStatus.KubeletVersion != nil {
		latestDPU.Status.AgentStatus.KubeletVersion = agentStatus.KubeletVersion
	}
	if agentStatus.TrustBundleHash != nil {
		latestDPU.Status.AgentStatus.TrustBundleHash = agentStatus.TrustBundleHash
	}
	if agentStatus.TrustBundleLastUpdateTime != nil {
		latestDPU.Status.AgentStatus.TrustBundleLastUpdateTime = agentStatus.TrustBundleLastUpdateTime
	}
	if agentStatus.LastObservedPendingNVConfig != nil {
		latestDPU.Status.AgentStatus.LastObservedPendingNVConfig = agentStatus.LastObservedPendingNVConfig.DeepCopy()
	}
	if r.optCtx.ClearHostOSInit {
		latestDPU.Status.AgentStatus.HostOSInit = nil
	} else if agentStatus.HostOSInit != nil {
		latestDPU.Status.AgentStatus.HostOSInit = agentStatus.HostOSInit.DeepCopy()
	}
	for _, condition := range agentStatus.Conditions {
		meta.SetStatusCondition(&latestDPU.Status.AgentStatus.Conditions, condition)
	}
	return r.optCtx.Client.Status().Patch(ctx, latestDPU, patch)
}
