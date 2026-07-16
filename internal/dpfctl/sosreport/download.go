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
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/util"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// 255 is the common filesystem NAME_MAX for one path component.
	maxDownloadFilenameLength = 255
	downloadFileSuffix        = ".tar.gz"
)

// DownloadAndArchive downloads completed SOS reports and optionally archives them.
// It is the programmatic equivalent of `dpfctl sosreport download` (without
// the interactive cleanup prompt).
func DownloadAndArchive(ctx context.Context, targets ClusterTargets, opts DownloadOptions) error {
	outputDir := opts.OutputDir
	if outputDir == "" {
		if opts.CaseID != "" {
			outputDir = fmt.Sprintf("sosreport-%s", opts.CaseID)
		} else {
			outputDir = fmt.Sprintf("sosreport-%s", time.Now().Format("20060102-150405"))
		}
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", outputDir, err)
	}

	downloaded := Download(ctx, targets, opts.Namespace, opts.CaseID, outputDir)

	if downloaded == 0 {
		util.ResultFail("No completed SOS reports found to download")
		if opts.ShowStatusHint {
			util.Info("Use 'dpfctl sosreport status' to check Job progress")
		}
		return nil
	}

	util.Result("Downloaded %d report(s) to %s", downloaded, outputDir)

	if opts.ArchiveOnly {
		opts.Archive = true
	}
	if opts.Archive {
		util.Step("Creating archive")
		archivePath, err := CreateArchive(outputDir)
		if err != nil {
			return fmt.Errorf("failed to create archive: %w", err)
		}
		util.Result("Archive created: %s", archivePath)
		if opts.ArchiveOnly {
			if err := os.RemoveAll(outputDir); err != nil {
				return fmt.Errorf("failed to remove report directory: %w", err)
			}
		}
	}

	return nil
}

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
	filename := downloadFilename(clusterName, nodeName)
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

// Download downloads completed SOS reports from the given targets.
func Download(ctx context.Context, targets ClusterTargets, namespace, caseID, outputDir string) int {
	downloaded := 0
	for i := range targets {
		n, err := downloadFromCluster(ctx, &targets[i], namespace, caseID, outputDir)
		if err != nil {
			util.Failure("cluster %s: %v", targets[i].Name, err)
			continue
		}
		downloaded += n
	}
	return downloaded
}

func downloadFromCluster(ctx context.Context, target *ClusterTarget, namespace, caseID, outputDir string) (int, error) {
	jobs, err := ListJobs(ctx, target.Client, namespace, caseID)
	if err != nil {
		return 0, err
	}

	downloaded := 0
	for _, job := range jobs {
		nodeName := job.Annotations[annotationNode]
		clusterName := job.Annotations[annotationCluster]

		if nodeName == "" {
			continue
		}

		if err := target.EnsureTunnel(ctx); err != nil {
			util.Failure("%s/%s: tunnel reconnect failed: %v", clusterName, nodeName, err)
			continue
		}

		// NFS-mode jobs use a "done" container that exits immediately — reports
		// live on the NFS share, not inside the pod. Skip download for these.
		if isNFSJob(&job) {
			util.Warn("%s/%s: NFS output mode — reports were written to the NFS share, not downloadable via this command", clusterName, nodeName)
			continue
		}

		runningPod, err := FindReadyDownloadPod(ctx, target.Client, namespace, &job)
		if err != nil {
			util.Failure("%s/%s: %v", clusterName, nodeName, err)
			continue
		}
		if runningPod == nil {
			util.Warn("%s/%s: not ready yet", clusterName, nodeName)
			continue
		}

		stopSpinner := util.StartSpinner("Downloading %s/%s...", clusterName, nodeName)
		localPath, err := DownloadToFile(ctx, target.RestConfig, namespace, runningPod.Name, "sleep", outputDir, clusterName, nodeName)
		stopSpinner()
		if err != nil {
			util.Failure("%s/%s: %v", clusterName, nodeName, err)
			continue
		}
		util.Success("%s/%s → %s", clusterName, nodeName, localPath)
		downloaded++
	}

	return downloaded, nil
}

func downloadFilename(clusterName, nodeName string) string {
	rawName := fmt.Sprintf("sosreport-%s-%s", clusterName, nodeName)
	filename := rawName + downloadFileSuffix
	if len(filename) <= maxDownloadFilenameLength {
		return filename
	}

	hash := shortHash(rawName)
	maxBaseLength := maxDownloadFilenameLength - len(downloadFileSuffix)
	maxPrefixLength := maxBaseLength - len(hash) - 1
	prefix := rawName[:maxPrefixLength]
	prefix = strings.TrimRight(prefix, ".-")
	return fmt.Sprintf("%s-%s%s", prefix, hash, downloadFileSuffix)
}

// CleanupResources deletes sosreport resources (Jobs, Secrets) matching the given labels.
func CleanupResources(ctx context.Context, c client.Client, namespace string, labels map[string]string) error {
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	}
	deleteOpts := []client.DeleteOption{
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	}

	var errs []error

	// Delete Jobs (foreground propagation also deletes their pods).
	jobList := &batchv1.JobList{}
	if err := c.List(ctx, jobList, listOpts...); err != nil {
		errs = append(errs, fmt.Errorf("list Jobs: %w", err))
	} else {
		for i := range jobList.Items {
			if err := c.Delete(ctx, &jobList.Items[i], deleteOpts...); client.IgnoreNotFound(err) != nil {
				errs = append(errs, fmt.Errorf("delete Job %s: %w", jobList.Items[i].Name, err))
			}
		}
	}

	// Delete Secrets.
	secretList := &corev1.SecretList{}
	if err := c.List(ctx, secretList, listOpts...); err != nil {
		errs = append(errs, fmt.Errorf("list Secrets: %w", err))
	} else {
		for i := range secretList.Items {
			if err := c.Delete(ctx, &secretList.Items[i]); client.IgnoreNotFound(err) != nil {
				errs = append(errs, fmt.Errorf("delete Secret %s: %w", secretList.Items[i].Name, err))
			}
		}
	}

	return errors.Join(errs...)
}
