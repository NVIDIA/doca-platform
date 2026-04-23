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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StreamFromPod execs into a pod and streams the output directory as a tar.gz to the provided writer.
func StreamFromPod(ctx context.Context, restConfig *rest.Config, namespace, podName, containerName string, stdout io.Writer) error {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	// Redirect stderr from tar to a separate buffer to avoid corrupting the tar
	// stream on stdout. Any stderr output is logged after the stream completes.
	var stderrBuf bytes.Buffer

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		Param("container", containerName).
		Param("command", "tar").
		Param("command", "czf").
		Param("command", "-").
		Param("command", "-C").
		Param("command", outputDir).
		Param("command", ".").
		Param("stdout", "true").
		Param("stderr", "true")

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create SPDY executor: %w", err)
	}

	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: &stderrBuf,
	}); err != nil {
		if stderrBuf.Len() > 0 {
			return fmt.Errorf("stream SOS report: %w (stderr: %s)", err, stderrBuf.String())
		}
		return fmt.Errorf("stream SOS report: %w", err)
	}

	return nil
}

// DownloadToFile downloads the SOS report from a pod to a local file.
func DownloadToFile(ctx context.Context, restConfig *rest.Config, namespace, podName, containerName, outputDir, clusterName, nodeName string) (string, error) {
	filename := fmt.Sprintf("sosreport-%s-%s.tar.gz", clusterName, nodeName)
	localPath := filepath.Join(outputDir, filename)

	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create local file %s: %w", localPath, err)
	}
	defer func() { _ = f.Close() }()

	if err := StreamFromPod(ctx, restConfig, namespace, podName, containerName, f); err != nil {
		_ = os.Remove(localPath)
		return "", err
	}

	return localPath, nil
}

// Download downloads all completed SOS reports from the given targets.
func Download(ctx context.Context, targets ClusterTargets, namespace, outputDir string) int {
	downloaded := 0
	for i := range targets {
		n, err := downloadFromCluster(ctx, &targets[i], namespace, outputDir)
		if err != nil {
			Failure("cluster %s: %v", targets[i].Name, err)
			continue
		}
		downloaded += n
	}
	return downloaded
}

func downloadFromCluster(ctx context.Context, target *ClusterTarget, namespace, outputDir string) (int, error) {
	jobs, err := ListJobs(ctx, target.Client, namespace, "")
	if err != nil {
		return 0, err
	}

	downloaded := 0
	for _, job := range jobs {
		nodeName := job.Labels[labelNode]
		clusterName := job.Labels[labelCluster]

		if nodeName == "" {
			continue
		}

		if err := target.EnsureTunnel(ctx); err != nil {
			Failure("%s/%s: tunnel reconnect failed: %v", clusterName, nodeName, err)
			continue
		}

		runningPod, err := FindReadyDownloadPod(ctx, target.Client, namespace, job.Spec.Template.Labels)
		if err != nil {
			Failure("%s/%s: %v", clusterName, nodeName, err)
			continue
		}
		if runningPod == nil {
			Warn("%s/%s: not ready yet", clusterName, nodeName)
			continue
		}

		stopSpinner := StartSpinner("Downloading %s/%s...", clusterName, nodeName)
		localPath, err := DownloadToFile(ctx, target.RestConfig, namespace, runningPod.Name, "sleep", outputDir, clusterName, nodeName)
		stopSpinner()
		if err != nil {
			Failure("%s/%s: %v", clusterName, nodeName, err)
			continue
		}
		Success("%s/%s → %s", clusterName, nodeName, localPath)
		downloaded++
	}

	return downloaded, nil
}

// CleanupResources deletes sosreport resources (Jobs, Secrets, pods) matching the given labels.
func CleanupResources(ctx context.Context, c client.Client, namespace string, labels map[string]string) error {
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	}
	deleteOpts := []client.DeleteOption{
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	}

	var errs []error

	// Delete Jobs first (foreground propagation also deletes their pods)
	jobList := &batchv1.JobList{}
	if err := c.List(ctx, jobList, listOpts...); err != nil {
		errs = append(errs, fmt.Errorf("list Jobs: %w", err))
	} else {
		for i := range jobList.Items {
			if err := c.Delete(ctx, &jobList.Items[i], deleteOpts...); err != nil {
				errs = append(errs, fmt.Errorf("delete Job %s: %w", jobList.Items[i].Name, err))
			}
		}
	}

	// Delete any remaining pods (e.g., download pods)
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, listOpts...); err != nil {
		errs = append(errs, fmt.Errorf("list Pods: %w", err))
	} else {
		for i := range podList.Items {
			if err := c.Delete(ctx, &podList.Items[i], deleteOpts...); err != nil {
				errs = append(errs, fmt.Errorf("delete Pod %s: %w", podList.Items[i].Name, err))
			}
		}
	}

	// Delete Secrets
	secretList := &corev1.SecretList{}
	if err := c.List(ctx, secretList, listOpts...); err != nil {
		errs = append(errs, fmt.Errorf("list Secrets: %w", err))
	} else {
		for i := range secretList.Items {
			if err := c.Delete(ctx, &secretList.Items[i], deleteOpts...); err != nil {
				errs = append(errs, fmt.Errorf("delete Secret %s: %w", secretList.Items[i].Name, err))
			}
		}
	}

	return errors.Join(errs...)
}
