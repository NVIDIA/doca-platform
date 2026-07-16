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

	"github.com/nvidia/doca-platform/internal/dpfctl/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cleanup removes all SOS report resources across the given targets.
// If caseID is non-empty, only resources with that case ID are removed.
func Cleanup(ctx context.Context, targets ClusterTargets, namespace, caseID string) int {
	totalCleaned := 0
	for i := range targets {
		if err := targets[i].EnsureTunnel(ctx); err != nil {
			util.Failure("cluster %s: %v", targets[i].Name, err)
			continue
		}
		n, err := cleanupOnCluster(ctx, targets[i], namespace, caseID)
		if err != nil {
			util.Failure("cluster %s: %v", targets[i].Name, err)
			continue
		}
		totalCleaned += n
	}
	return totalCleaned
}

func cleanupOnCluster(ctx context.Context, target ClusterTarget, namespace, caseID string) (int, error) {
	jobs, err := ListJobs(ctx, target.Client, namespace, caseID)
	if err != nil {
		return 0, err
	}

	if len(jobs) == 0 {
		return 0, nil
	}

	// Log each Job being deleted for user feedback.
	deleteOpts := []client.DeleteOption{
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	}
	for i := range jobs {
		job := &jobs[i]
		nodeName := job.Annotations[annotationNode]
		util.Success("Deleting %s/%s (job: %s)", target.Name, nodeName, job.Name)
		if err := target.Client.Delete(ctx, job, deleteOpts...); err != nil {
			util.Warn("failed to delete Job %s: %v", job.Name, err)
		}
	}

	// Clean up remaining resources (Secrets, orphaned pods) that share the selector labels.
	cleanupLabels := selectorLabels()
	if caseID != "" {
		cleanupLabels[labelCaseID] = caseID
	}
	if err := CleanupResources(ctx, target.Client, namespace, cleanupLabels); err != nil {
		util.Warn("resource cleanup on %s: %v", target.Name, err)
	}

	return len(jobs), nil
}
