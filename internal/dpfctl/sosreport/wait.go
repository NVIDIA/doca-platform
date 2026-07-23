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

	"github.com/nvidia/doca-platform/internal/dpfctl/util"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForAll waits for all SOS report Jobs with the given case ID to complete.
func WaitForAll(ctx context.Context, targets ClusterTargets, namespace, caseID string, timeout time.Duration) error {
	deadline := timeout + 2*time.Minute
	lastCount := -1
	var stopSpinner func()

	initContainerFailures := map[string]bool{}

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
				// Report init container failures — skip waiting since the Job
				// will either retry (new pod) or fail permanently (IsJobDone).
				if msg := checkInitContainerFailure(ctx, targets[i].Client, job.Namespace, &job); msg != "" {
					key := job.Name + ":" + msg
					if !initContainerFailures[key] {
						initContainerFailures[key] = true
						if stopSpinner != nil {
							stopSpinner()
							stopSpinner = nil
						}
						nodeName := job.Annotations[annotationNode]
						util.Failure("%s/%s: %s", targets[i].Name, nodeName, msg)
					}
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
			stopSpinner = util.StartSpinner("Waiting for %d job(s) to complete...", waitingCount)
			lastCount = waitingCount
		}

		return false, nil
	})

	if stopSpinner != nil {
		stopSpinner()
	}

	if err == nil {
		if len(initContainerFailures) > 0 {
			util.Warn("Some jobs had init container failures")
		} else if lastCount >= 0 {
			util.Success("All jobs completed")
		} else {
			util.Info("No jobs found")
		}
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("wait for jobs: %w", err)
}

// checkInitContainerFailure returns a description of a failing init container
// on the most recent pod selected by the Job, or "" if none are failing.
func checkInitContainerFailure(ctx context.Context, c client.Client, namespace string, job *batchv1.Job) string {
	podSelector, err := metav1.LabelSelectorAsSelector(job.Spec.Selector)
	if err != nil {
		return ""
	}

	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: podSelector}); err != nil {
		return ""
	}
	for _, pod := range podList.Items {
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				return fmt.Sprintf("init container %q is crash-looping: %s", cs.Name, cs.State.Waiting.Message)
			}
			if cs.State.Terminated == nil || cs.State.Terminated.ExitCode == 0 {
				continue
			}

			msg := cs.State.Terminated.Message
			if msg == "" {
				msg = cs.State.Terminated.Reason
			}
			failure := fmt.Sprintf("init container %q failed (exit %d): %s", cs.Name, cs.State.Terminated.ExitCode, msg)
			if cs.State.Terminated.Reason == "OOMKilled" && cs.Name == sosreportContainerName {
				cur := initContainerMemoryLimit(&pod, cs.Name)
				suggested := cur.DeepCopy()
				suggested.Add(cur)
				failure += fmt.Sprintf(" - the container exceeded its memory limit (%s); retry with a larger --limits.memory value (e.g. --limits.memory %s)", cur.String(), suggested.String())
			}
			return failure
		}
	}
	return ""
}

// initContainerMemoryLimit returns the memory limit of the named init container,
// falling back to DefaultMemoryLimit if the limit is not set in the pod spec.
func initContainerMemoryLimit(pod *corev1.Pod, containerName string) resource.Quantity {
	for _, c := range pod.Spec.InitContainers {
		if c.Name != containerName {
			continue
		}
		if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			return q
		}
	}
	return resource.MustParse(DefaultMemoryLimit)
}
