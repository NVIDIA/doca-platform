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
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// statusColumns tracks column widths so appended lines align with the initial table.
type statusColumns struct {
	cluster, node, job, status int
}

const colPad = 2 // spaces between columns, matching tabwriter default

// statusFmt returns a format string with fixed-width columns.
func (c *statusColumns) fmt() string {
	return fmt.Sprintf("%%-%ds%%-%ds%%-%ds%%-%ds%%s\n",
		c.cluster+colPad, c.node+colPad, c.job+colPad, c.status+colPad)
}

// update widens columns if the given values are longer.
func (c *statusColumns) update(cluster, node, job, status string) {
	if len(cluster) > c.cluster {
		c.cluster = len(cluster)
	}
	if len(node) > c.node {
		c.node = len(node)
	}
	if len(job) > c.job {
		c.job = len(job)
	}
	if len(status) > c.status {
		c.status = len(status)
	}
}

// statusResult holds the data needed to print a status line.
type statusResult struct {
	cluster, node, job, status string
	age                        time.Duration
}

// collectStatus gathers all current job statuses from the given targets.
func collectStatus(ctx context.Context, targets ClusterTargets, namespace, caseID string) []statusResult {
	var results []statusResult
	for _, target := range targets {
		jobs, err := ListJobs(ctx, target.Client, namespace, caseID)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to list Jobs on cluster %s: %v\n", target.Name, err)
			continue
		}
		for _, job := range jobs {
			results = append(results, statusResult{
				cluster: job.Labels[labelCluster],
				node:    job.Labels[labelNode],
				job:     job.Name,
				status:  resolveStatus(ctx, target, namespace, &job),
				age:     time.Since(job.CreationTimestamp.Time).Truncate(time.Second),
			})
		}
	}
	return results
}

// Status prints the status of all SOS report Jobs across the given targets.
func Status(ctx context.Context, targets ClusterTargets, namespace, caseID string) error {
	_, _ = statusWithColumns(ctx, targets, namespace, caseID)
	return nil
}

// statusWithColumns prints the initial table and returns the column widths and
// the initial status map, for use by callers that need to append lines later.
func statusWithColumns(ctx context.Context, targets ClusterTargets, namespace, caseID string) (statusColumns, map[string]string) {
	results := collectStatus(ctx, targets, namespace, caseID)

	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "No resources found in %s namespace.\n", namespace)
		return statusColumns{}, nil
	}

	cols := statusColumns{}
	cols.update("CLUSTER", "NODE", "JOB", "STATUS")
	for _, r := range results {
		cols.update(r.cluster, r.node, r.job, r.status)
	}
	// If any job is still in progress, pre-widen the status column to fit
	// the longest terminal status ("Completed") so watch-mode appended lines
	// stay aligned.
	for _, r := range results {
		if r.status != "Completed" && r.status != "Ready" {
			cols.update("", "", "", "Completed")
			break
		}
	}

	f := cols.fmt()
	fmt.Printf(f, "CLUSTER", "NODE", "JOB", "STATUS", "AGE")
	for _, r := range results {
		fmt.Printf(f, r.cluster, r.node, r.job, r.status, r.age)
	}

	prev := make(map[string]string, len(results))
	for _, r := range results {
		prev[r.job] = r.status
	}
	return cols, prev
}

// WatchStatus prints the initial status table then polls and appends a line whenever
// a Job's status changes, similar to kubectl get -w.
func WatchStatus(ctx context.Context, targets ClusterTargets, namespace, caseID string, interval time.Duration) error {
	cols, prev := statusWithColumns(ctx, targets, namespace, caseID)
	f := cols.fmt()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}

		for _, target := range targets {
			jobs, err := ListJobs(ctx, target.Client, namespace, caseID)
			if err != nil {
				continue
			}
			for _, job := range jobs {
				status := resolveStatus(ctx, target, namespace, &job)
				if old, seen := prev[job.Name]; seen && old == status {
					continue
				}
				prev[job.Name] = status
				fmt.Printf(f,
					job.Labels[labelCluster], job.Labels[labelNode], job.Name, status,
					time.Since(job.CreationTimestamp.Time).Truncate(time.Second))
			}
		}
	}
}

// resolveStatus returns the effective status string for a job, including the Ready check.
func resolveStatus(ctx context.Context, target ClusterTarget, namespace string, job *batchv1.Job) string {
	status := JobStatus(job)
	if status == "Running" {
		pod, _ := FindReadyDownloadPod(ctx, target.Client, namespace, job.Spec.Template.Labels)
		if pod != nil {
			status = "Ready"
		}
	}
	return status
}

// JobStatus returns a human-readable status string for a Job.
func JobStatus(job *batchv1.Job) string {
	if job.Status.Succeeded > 0 {
		return "Completed"
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return fmt.Sprintf("Failed: %s", cond.Reason)
		}
	}

	if job.Status.Active > 0 {
		return "Running"
	}

	return "Pending"
}

// IsJobDone returns true if the Job has succeeded or failed.
func IsJobDone(job *batchv1.Job) bool {
	if job.Status.Succeeded > 0 {
		return true
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsSosreportDone returns true if the sosreport init container has completed
// and the pod is running (ready for download).
func IsSosreportDone(ctx context.Context, target ClusterTarget, job *batchv1.Job) bool {
	pod, err := FindReadyDownloadPod(ctx, target.Client, job.Namespace, job.Spec.Template.Labels)
	return err == nil && pod != nil
}
