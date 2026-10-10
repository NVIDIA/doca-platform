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
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statusmanager"
	dpuutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const defaultRetryInterval = 30 * time.Second

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
	optCtx.Status = statusmanager.New(optCtx.Client, optCtx.Options.DPUNamespace, optCtx.Options.DPUName, optCtx.Options.DPUUID)
	return &Runner{optCtx: optCtx, operations: ops, retryInterval: defaultRetryInterval}
}

// Run executes the pipeline. It returns nil after the final status patch, or the context error when
// the run was interrupted (reboot, reprovision, shutdown).
func (r *Runner) Run(ctx context.Context) error {
	r.optCtx.Status.Start(ctx)
	// The mock always behaves like an agent on the device-query reboot path.
	r.optCtx.RebootMethodDiscovery = true
	r.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
		*s = provisioningv1.AgentStatus{
			Conditions:   []metav1.Condition{},
			RebootMethod: ptr.To(provisioningv1.RebootMethodUnknown),
		}
	})
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
				r.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
					hostutil.NewCondition(op.ConditionType()).Failure(err, "FailedToExecute").Set(&s.Conditions)
				})
			} else {
				klog.InfoS("operation succeeded", "operation", op.Name())
				r.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
					hostutil.NewCondition(op.ConditionType()).Success(dpuutil.TruncateConditionMessage(r.optCtx.CondMessage)).Set(&s.Conditions)
				})
			}
			if err != nil || op.ShouldUpdateStatusBeforeContinue(r.optCtx) {
				if updateErr := r.optCtx.Status.UpdateRemote(true); updateErr != nil {
					return false, updateErr
				}
			}
			return err == nil, nil
		})
		if err != nil {
			return fmt.Errorf("execution of operation %s aborted: %w", op.Name(), err)
		}
	}
	return r.optCtx.Status.UpdateRemote(true)
}
