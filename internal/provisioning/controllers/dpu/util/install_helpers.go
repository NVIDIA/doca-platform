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

package util

import (
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
)

// CheckInstallationTimeout reports an error when OS installation has run for longer than timeout.
// Returns nil otherwise, including when timeout is non-positive (the check is disabled).
//
// Elapsed time is measured from the BFBPrepared condition, which every install interface sets
// before entering DPUOSInstalling and whose LastTransitionTime is preserved while the condition
// keeps the same status.
func CheckInstallationTimeout(status *provisioningv1.DPUStatus, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}

	_, bfbPreparedCond := cutil.GetDPUCondition(status, string(provisioningv1.DPUCondBFBPrepared))
	if bfbPreparedCond == nil {
		return nil
	}

	elapsed := time.Since(bfbPreparedCond.LastTransitionTime.Time)
	if elapsed <= timeout {
		return nil
	}

	return fmt.Errorf("OS installation timeout exceeded: %v > %v", elapsed, timeout)
}
