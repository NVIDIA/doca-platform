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

package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	httputil "github.com/nvidia/doca-platform/internal/provisioning/utils/http"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const maxDownloadRetries = 3

var (
	downloadRetryCounter     sync.Map
	downloadRetryCounterLock sync.Mutex
)

type bfbDownloadingState struct {
	bfb        *provisioningv1.BFB
	recorder   record.EventRecorder
	checkBFB   func(string) (bool, error)
	versionBFB func(string) (*provisioningv1.BFBVersions, error)
}

func (st *bfbDownloadingState) Handle(ctx context.Context, _ client.Client) error {
	bfbTaskName := cutil.GenerateBFBTaskName(*st.bfb)

	if isDeleting(st.bfb) {
		// Retrieve and call the cancel function to stop the download
		if cancelFunc, ok := butil.DownloadingTaskMap.Load(bfbTaskName + "cancel"); ok {
			cancelFunc.(context.CancelFunc)()
			butil.DownloadingTaskMap.Delete(bfbTaskName)
			butil.DownloadingTaskMap.Delete(bfbTaskName + "cancel")
		}
		st.clearRetryCounter(bfbTaskName)
		st.bfb.Status.Phase = provisioningv1.BFBDeleting
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonAwaitingDeletion, "BFB is being deleted")
		return nil
	}

	exist, err := st.checkBFB(st.bfb.Status.FileName)
	if err != nil {
		st.bfb.Status.Phase = provisioningv1.BFBError
		msg := fmt.Sprintf("Download BFB: (%s/%s) failed with error :%s", st.bfb.Namespace, st.bfb.Name, err.Error())
		st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
		// Use ReasonFailure for filesystem errors - user intervention required (e.g., fix permissions, disk space)
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonFailure, conditions.ConditionMessage(err.Error()))
		return err
	}

	if bfbDownloader, ok := butil.DownloadingTaskMap.Load(bfbTaskName); ok {
		// Wait for downloading task completion
		result := bfbDownloader.(*future.Future)
		if result.GetState() != future.Ready {
			return nil
		}
		// Remove downloading task context
		butil.DownloadingTaskMap.Delete(bfbTaskName)
		butil.DownloadingTaskMap.Delete(bfbTaskName + "cancel")
		// Check task result
		if _, err := result.GetResult(); err != nil {
			return st.handleDownloadError(err, bfbTaskName)
		}
		st.clearRetryCounter(bfbTaskName)
	} else if !exist {
		// Start BFB downloading task
		bfbTask := butil.BFBTask{
			TaskName: bfbTaskName,
			URL:      st.bfb.Spec.URL,
			FileName: st.bfb.Status.FileName,
			UID:      st.bfb.UID,
		}

		// Create a new context for this download task
		taskCtx, cancel := context.WithCancel(ctx)
		// Store the cancel function in the map
		butil.DownloadingTaskMap.Store(bfbTaskName+"cancel", cancel)
		// Start the download with the new context
		downloadBFB(taskCtx, bfbTask)
		return nil
	}
	// There is no related downloading task and BFB file exists in cache.
	// Use spec versions if provided, otherwise extract from the BFB file.
	if st.bfb.Spec.Versions != nil {
		st.bfb.Status.Versions = *st.bfb.Spec.Versions
	} else {
		versions, err := st.versionBFB(cutil.GenerateBFBFilePath(st.bfb.Status.FileName))
		if err != nil {
			st.bfb.Status.Phase = provisioningv1.BFBError
			msg := fmt.Sprintf("Retrieving BFB version: (%s/%s) failed with error :%s", st.bfb.Namespace, st.bfb.Name, err.Error())
			st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
			// Use ReasonFailure for version parsing errors - user intervention required (invalid BFB file at URL)
			conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
				conditions.ReasonFailure, conditions.ConditionMessage(err.Error()))
			return err
		}
		st.bfb.Status.Versions = *versions
	}

	st.clearRetryCounter(bfbTaskName)
	st.bfb.Status.Phase = provisioningv1.BFBReady
	msg := fmt.Sprintf("Download BFB: (%s/%s) successful", st.bfb.Namespace, st.bfb.Name)
	st.recorder.Eventf(st.bfb, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, msg)
	conditions.AddTrue(st.bfb, provisioningv1.BFBCondDownloaded)

	return nil
}

func (st *bfbDownloadingState) handleDownloadError(err error, taskName string) error {
	if errors.Is(err, context.Canceled) {
		st.clearRetryCounter(taskName)
		st.bfb.Status.Phase = provisioningv1.BFBDeleting
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonAwaitingDeletion, "BFB download canceled, deletion in progress")
		return nil
	}

	currentRetries := st.getRetryCount(taskName)
	if currentRetries < maxDownloadRetries {
		st.incrementRetryCounter(taskName)
		msg := fmt.Sprintf("Download BFB: (%s/%s) failed with error: %s. Retry attempt %d/%d",
			st.bfb.Namespace, st.bfb.Name, err.Error(), currentRetries+1, maxDownloadRetries)
		st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
		st.bfb.Status.Phase = provisioningv1.BFBDownloading
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonRetrying, conditions.ConditionMessage(msg))
		return nil
	}

	st.clearRetryCounter(taskName)
	reason := conditions.ReasonFailure
	if cutil.IsRecoverableDownloadError(err) {
		reason = conditions.ReasonError
	}
	msg := fmt.Sprintf("Download BFB: (%s/%s) failed after %d retries with error: %s",
		st.bfb.Namespace, st.bfb.Name, maxDownloadRetries, err.Error())
	st.recorder.Eventf(st.bfb, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
	st.bfb.Status.Phase = provisioningv1.BFBError
	conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
		reason, conditions.ConditionMessage(msg))
	return err
}

func (st *bfbDownloadingState) getRetryCount(taskName string) int {
	if value, ok := downloadRetryCounter.Load(taskName); ok {
		return value.(int)
	}
	return 0
}

func (st *bfbDownloadingState) incrementRetryCounter(taskName string) {
	downloadRetryCounterLock.Lock()
	defer downloadRetryCounterLock.Unlock()
	downloadRetryCounter.Store(taskName, st.getRetryCount(taskName)+1)
}

func (st *bfbDownloadingState) clearRetryCounter(taskName string) {
	downloadRetryCounterLock.Lock()
	defer downloadRetryCounterLock.Unlock()
	downloadRetryCounter.Delete(taskName)
}

func downloadBFB(ctx context.Context, bfbTask butil.BFBTask) {
	bfbDownloader := future.New(func() (any, error) {
		logger := log.FromContext(ctx)
		logger.V(3).Info("BFBPackage", "start downloading", bfbTask.FileName)

		// Create a temporary file
		tempFileName := cutil.GenerateBFBTMPFilePath(string(bfbTask.UID))
		tempFile, err := os.Create(tempFileName)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tempFile.Name()) //nolint: errcheck

		// Retry protects initial connection from transient network failures.
		// Once connected, body reading has no retry.
		resp, err := httputil.DoRequestWithRetry(ctx, "GET", bfbTask.URL, 3, time.Second)
		if err != nil {
			return nil, err
		}
		defer httputil.CloseBody(resp)

		expectedSize := resp.ContentLength
		var totalWritten int64

		buf := make([]byte, 128*1024*1024)
	copyLoop:
		for {
			select {
			case <-ctx.Done():
				logger.V(3).Info("BFBPackage", "context canceled", bfbTask.FileName)
				return nil, nil
			default:
				n, err := resp.Body.Read(buf)
				if err != nil && err != io.EOF {
					if errors.Is(err, context.Canceled) {
						return nil, ctx.Err()
					}
					return nil, fmt.Errorf("failed to read from source file: %w", err)
				}
				if n == 0 {
					break copyLoop
				}
				if _, writeErr := tempFile.Write(buf[:n]); writeErr != nil {
					return nil, writeErr
				}
				totalWritten += int64(n)
			}
		}

		// Validate downloaded file size against Content-Length header from this GET response.
		// This detects truncated downloads caused by network failures or connection drops.
		// Only validate when Content-Length is positive (known size).
		// Skip validation when Content-Length is -1 (not set) or 0 (empty) since we can't reliably verify.
		// If validation fails, the function returns an error and the deferred os.Remove() cleans up
		// the temporary file, ensuring no partial/corrupted file is left at the destination.
		if expectedSize > 0 && totalWritten != expectedSize {
			return nil, fmt.Errorf("downloaded file size mismatch: expected %d bytes, got %d bytes", expectedSize, totalWritten)
		}

		// Close the temp file before renaming
		if err := tempFile.Close(); err != nil {
			return nil, err
		}

		bfbfile := cutil.GenerateBFBFilePath(bfbTask.FileName)
		if err := os.Chmod(tempFile.Name(), 0644); err != nil {
			return nil, err
		}

		if err := os.Rename(tempFile.Name(), bfbfile); err != nil {
			return nil, err
		}

		logger.V(3).Info("createBFBPackage", "finish", bfbTask.FileName)
		return true, nil
	}, nil)
	butil.DownloadingTaskMap.Store(bfbTask.TaskName, bfbDownloader)
}

// checkBFB checks if the BFB file exists on the filesystem
func checkBFB(fileName string) (bool, error) {
	bfbFilePath := cutil.GenerateBFBFilePath(fileName)
	_, err := os.Stat(bfbFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// versionBFB extracts version information from a BFB file
func versionBFB(filePath string) (*provisioningv1.BFBVersions, error) {
	return butil.VersionFromBFBFile(filePath)
}
