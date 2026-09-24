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

// InstallElapsed returns how long the current OS installation has been running, and whether that
// could be determined at all.
//
// Elapsed time is measured from the BFBPrepared condition, which every install interface sets
// before entering DPUOSInstalling and whose LastTransitionTime is preserved while the condition
// keeps the same status. Install-progress logging shares this anchor with CheckInstallationTimeout
// so a progress line and a timeout error report the same interval.
func InstallElapsed(status *provisioningv1.DPUStatus) (time.Duration, bool) {
	_, bfbPreparedCond := cutil.GetDPUCondition(status, string(provisioningv1.DPUCondBFBPrepared))
	if bfbPreparedCond == nil {
		return 0, false
	}
	return time.Since(bfbPreparedCond.LastTransitionTime.Time), true
}

// CheckInstallationTimeout reports an error when OS installation has run for longer than timeout.
// Returns nil otherwise, including when timeout is non-positive (the check is disabled).
func CheckInstallationTimeout(status *provisioningv1.DPUStatus, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}

	elapsed, ok := InstallElapsed(status)
	if !ok || elapsed <= timeout {
		return nil
	}

	return fmt.Errorf("OS installation timeout exceeded: %v > %v", elapsed, timeout)
}
