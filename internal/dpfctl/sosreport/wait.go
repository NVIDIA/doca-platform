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

package sosreport

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// WaitForAll waits for all SOS report Jobs with the given case ID to complete.
func WaitForAll(ctx context.Context, targets ClusterTargets, namespace, caseID string, timeout time.Duration) error {
	deadline := timeout + 2*time.Minute
	lastCount := -1
	var stopSpinner func()

	err := wait.PollUntilContextTimeout(ctx, 10*time.Second, deadline, true, func(ctx context.Context) (bool, error) {
		waitingCount := 0
		for i := range targets {
			if err := targets[i].EnsureTunnel(ctx); err != nil {
				return false, nil
			}

			jobs, err := ListJobs(ctx, targets[i].Client, namespace, caseID)
			if err != nil {
				return false, nil
			}

			for _, job := range jobs {
				if IsJobDone(&job) {
					continue
				}
				if IsSosreportDone(ctx, targets[i], &job) {
					continue
				}
				waitingCount++
			}
		}

		if waitingCount == 0 {
			return true, nil
		}

		if waitingCount != lastCount {
			if stopSpinner != nil {
				stopSpinner()
			}
			stopSpinner = StartSpinner("Waiting for %d job(s) to complete...", waitingCount)
			lastCount = waitingCount
		}

		return false, nil
	})

	if stopSpinner != nil {
		stopSpinner()
	}

	if err == nil {
		if lastCount >= 0 {
			Success("All jobs completed")
		} else {
			Info("No jobs found")
		}
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("wait for jobs: %w", err)
}
