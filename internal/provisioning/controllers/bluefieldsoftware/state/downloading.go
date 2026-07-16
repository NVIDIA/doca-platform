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
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	utils "github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// maxDownloadRetries is the maximum number of retry attempts for a download
	maxDownloadRetries = 3
)

var (
	// downloadRetryCounter tracks the number of retry attempts per component
	// Key format: "namespace/name/componentType"
	downloadRetryCounter     = sync.Map{}
	downloadRetryCounterLock sync.Mutex
)

type blueFieldSoftwareDownloadingState struct {
	bfs      *provisioningv1.BlueFieldSoftware
	recorder record.EventRecorder
}

func (st *blueFieldSoftwareDownloadingState) Handle(ctx context.Context, _ client.Client) error {
	if isDeleting(st.bfs) {
		st.cancelAllDownloads()
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}

	componentsToDownload := st.getComponentsToDownload()
	if len(componentsToDownload) == 0 {
		return st.markAllComponentsReady()
	}

	allCompleted, lastError := st.processComponents(ctx, componentsToDownload)
	if !allCompleted {
		return lastError
	}

	return st.markAllComponentsReady()
}

func (st *blueFieldSoftwareDownloadingState) markAllComponentsReady() error {
	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareExtracting
	msg := fmt.Sprintf("Download BlueFieldSoftware: (%s/%s) successful", st.bfs.Namespace, st.bfs.Name)
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, msg)
	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	return nil
}

func (st *blueFieldSoftwareDownloadingState) processComponents(ctx context.Context, components []componentInfo) (bool, error) {
	allCompleted := true
	var lastError error

	for _, component := range components {
		completed, err := st.processComponent(ctx, component)
		if err != nil {
			return false, err
		}
		if !completed {
			allCompleted = false
		}
	}

	return allCompleted, lastError
}

func (st *blueFieldSoftwareDownloadingState) processComponent(ctx context.Context, component componentInfo) (bool, error) {
	componentURL := component.URL
	componentType := component.ComponentType

	if !isURL(componentURL) {
		// Non-URL values are opaque identifiers stored as-is in status (no download).
		st.updateComponentStatus(componentType, componentURL)
		return true, nil
	}

	taskName := butil.GenerateComponentTaskName(*st.bfs, componentType)
	if taskFuture, ok := butil.DownloadingTaskMap.Load(taskName); ok {
		return st.handleExistingTask(taskFuture, taskName, componentType, componentURL)
	}

	return st.handleNewDownload(ctx, componentType, componentURL, taskName)
}

func (st *blueFieldSoftwareDownloadingState) handleExistingTask(taskFuture interface{}, taskName string, componentType butil.ComponentType, componentURL string) (bool, error) {
	result := taskFuture.(*future.Future)
	if result.GetState() != future.Ready {
		return false, nil
	}

	butil.DownloadingTaskMap.Delete(taskName + "cancel")
	butil.DownloadingTaskMap.Delete(taskName)

	if _, err := result.GetResult(); err != nil {
		return false, st.handleDownloadError(err, componentType)
	}

	fileName := butil.ComponentDownloadFilename(st.bfs, componentType, componentURL)
	st.updateComponentStatus(componentType, componentDestinationPath(componentType, fileName))
	return true, nil
}

func (st *blueFieldSoftwareDownloadingState) handleDownloadError(err error, componentType butil.ComponentType) error {
	if errors.Is(err, context.Canceled) {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonAwaitingDeletion, conditions.ConditionMessage(fmt.Sprintf("BlueFieldSoftware %s download canceled, deletion in progress", componentType)))
		st.clearRetryCounter(componentType)
		return nil
	}

	retryKey := st.getRetryKey(componentType)
	currentRetries := st.getRetryCount(retryKey)

	if currentRetries < maxDownloadRetries {
		st.incrementRetryCounter(retryKey)
		msg := fmt.Sprintf("Download component %s: (%s/%s) failed with error: %s. Retry attempt %d/%d",
			componentType, st.bfs.Namespace, st.bfs.Name, err.Error(), currentRetries+1, maxDownloadRetries)
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDownloading
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonRetrying, conditions.ConditionMessage(msg))
		// Clear the failed task so it can be retried
		taskName := butil.GenerateComponentTaskName(*st.bfs, componentType)
		butil.DownloadingTaskMap.Delete(taskName)
		butil.DownloadingTaskMap.Delete(taskName + "cancel")
		return nil
	}

	// Max immediate retries reached. Classify the failure so the Error state knows whether
	// to keep retrying: recoverable (ReasonError) for transient storage/network conditions
	// such as the /bfb volume being temporarily unavailable after a control-plane node OS
	// revert, and terminal (ReasonFailure) for issues that need user intervention such as
	// a bad URL (HTTP 4xx/5xx).
	st.clearRetryCounter(componentType)
	reason := conditions.ReasonFailure
	if isRecoverableDownloadError(err) {
		reason = conditions.ReasonError
	}
	msg := fmt.Sprintf("Download component %s: (%s/%s) failed after %d attempts with error: %s",
		componentType, st.bfs.Namespace, st.bfs.Name, maxDownloadRetries, err.Error())
	st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareError
	conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
		reason, conditions.ConditionMessage(msg))
	return err
}

// isRecoverableDownloadError reports whether a failed download is caused by a transient
// condition that is worth retrying (filesystem/storage or network errors), as opposed to
// a terminal condition that requires user intervention (e.g. an HTTP status error from a
// bad or missing URL). Unknown errors are treated as terminal to avoid retrying forever
// on genuine misconfiguration.
func isRecoverableDownloadError(err error) bool {
	if err == nil {
		return false
	}

	// Filesystem/storage errors, e.g. "mkdir /bfb/components: no such file or directory"
	// or "open /bfb/...tmp: permission denied" - exactly the symptoms of a /bfb volume
	// wiped or made unavailable by a control-plane node OS revert.
	var pathErr *os.PathError
	var linkErr *os.LinkError
	if errors.As(err, &pathErr) || errors.As(err, &linkErr) {
		return true
	}

	// Network/transport errors while contacting the download server.
	var netErr net.Error
	var urlErr *url.Error
	if errors.As(err, &netErr) || errors.As(err, &urlErr) {
		return true
	}

	return false
}

func (st *blueFieldSoftwareDownloadingState) handleNewDownload(ctx context.Context, componentType butil.ComponentType, componentURL, taskName string) (bool, error) {
	fileName := butil.ComponentDownloadFilename(st.bfs, componentType, componentURL)
	filePath := componentDestinationPath(componentType, fileName)

	exist, err := isFileExist(filePath)
	if err != nil {
		return false, st.handleFileCheckError(err, componentType)
	}

	if exist {
		st.updateComponentStatus(componentType, filePath)
		return true, nil
	}

	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, fmt.Sprintf("Starting download component %s: (%s/%s)", componentType, st.bfs.Namespace, st.bfs.Name))

	st.startDownload(ctx, componentType, componentURL, fileName, taskName)
	return false, nil
}

func (st *blueFieldSoftwareDownloadingState) handleFileCheckError(err error, componentType butil.ComponentType) error {
	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareError
	msg := fmt.Sprintf("Check component file %s: (%s/%s) failed with error: %s",
		componentType, st.bfs.Namespace, st.bfs.Name, err.Error())
	st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
	conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
		conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	return err
}

func (st *blueFieldSoftwareDownloadingState) startDownload(ctx context.Context, componentType butil.ComponentType, componentURL, fileName, taskName string) {
	task := butil.ComponentDownloadTask{
		TaskName:      taskName,
		URL:           componentURL,
		FileName:      fileName,
		ComponentName: string(componentType),
		UID:           st.bfs.UID,
	}

	taskCtx, cancel := context.WithCancel(ctx)
	butil.DownloadingTaskMap.Store(taskName+"cancel", cancel)
	downloadComponent(taskCtx, task)
}

type componentInfo struct {
	URL           string
	ComponentType butil.ComponentType
}

// componentDownloadSatisfied returns true when status reflects a completed download.
// For downloadable (URL) components the recorded file must also still exist on disk;
// this guards against stale status after the backing storage changed underneath the
// controller, so a retry re-downloads the missing file instead of advancing to
// Extracting with a non-existent artifact.
func (st *blueFieldSoftwareDownloadingState) componentDownloadSatisfied(componentType butil.ComponentType, specValue, downloaded string) bool {
	expected := componentDestinationPath(componentType, butil.ComponentDownloadFilename(st.bfs, componentType, specValue))
	if downloaded != expected {
		return false
	}
	if isURL(specValue) {
		if exist, err := isFileExist(expected); err != nil || !exist {
			return false
		}
	}
	return true
}

func (st *blueFieldSoftwareDownloadingState) getComponentsToDownload() []componentInfo {
	var components []componentInfo

	// Check FwBundleURL
	if st.bfs.Spec.PldmFwBundle != nil && !st.componentDownloadSatisfied(butil.ComponentTypeFwBundle, *st.bfs.Spec.PldmFwBundle, st.bfs.Status.DownloadedComponents.PldmFwBundle) {
		components = append(components, componentInfo{
			URL:           *st.bfs.Spec.PldmFwBundle,
			ComponentType: butil.ComponentTypeFwBundle,
		})
	}

	// Check OSISO
	if st.bfs.Spec.OsIso != "" && !st.componentDownloadSatisfied(butil.ComponentTypeOSISO, st.bfs.Spec.OsIso, st.bfs.Status.DownloadedComponents.OsIso) {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.OsIso,
			ComponentType: butil.ComponentTypeOSISO,
		})
	}

	// Check PlatformPldmFwBundle
	platformPldmFwBundle := ptr.Deref(st.bfs.Spec.PlatformPldmFwBundle, "")
	if platformPldmFwBundle != "" &&
		!st.componentDownloadSatisfied(butil.ComponentTypePlatformFwBundle, platformPldmFwBundle, st.bfs.Status.DownloadedComponents.PlatformPldmFwBundle) {
		components = append(components, componentInfo{
			URL:           platformPldmFwBundle,
			ComponentType: butil.ComponentTypePlatformFwBundle,
		})
	}

	// Check NicFw
	nicFw := ptr.Deref(st.bfs.Spec.NicFw, "")
	if nicFw != "" &&
		!st.componentDownloadSatisfied(butil.ComponentTypeNicFw, nicFw, st.bfs.Status.DownloadedComponents.NicFw) {
		components = append(components, componentInfo{
			URL:           nicFw,
			ComponentType: butil.ComponentTypeNicFw,
		})
	}

	return components
}

func (st *blueFieldSoftwareDownloadingState) updateComponentStatus(componentType butil.ComponentType, destinationPath string) {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		st.bfs.Status.DownloadedComponents.PldmFwBundle = destinationPath
	case butil.ComponentTypePlatformFwBundle:
		st.bfs.Status.DownloadedComponents.PlatformPldmFwBundle = destinationPath
	case butil.ComponentTypeOSISO:
		st.bfs.Status.DownloadedComponents.OsIso = destinationPath
	case butil.ComponentTypeNicFw:
		st.bfs.Status.DownloadedComponents.NicFw = destinationPath
	}
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, fmt.Sprintf("Component %s downloaded successfully", componentType))
	// Clear retry counter on successful download
	st.clearRetryCounter(componentType)
}

func (st *blueFieldSoftwareDownloadingState) cancelAllDownloads() {
	cancelDownloadsForBFS(st.bfs)
}

// componentTypesWithDownloads returns the components fetched during the Downloading phase.
func componentTypesWithDownloads() []butil.ComponentType {
	return []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypePlatformFwBundle,
		butil.ComponentTypeOSISO,
		butil.ComponentTypeNicFw,
	}
}

func cancelDownloadsForBFS(bfs *provisioningv1.BlueFieldSoftware) {
	for _, componentType := range componentTypesWithDownloads() {
		taskName := butil.GenerateComponentTaskName(*bfs, componentType)
		if cancelFunc, ok := butil.DownloadingTaskMap.Load(taskName + "cancel"); ok {
			cancelFunc.(context.CancelFunc)()
			butil.DownloadingTaskMap.Delete(taskName)
			butil.DownloadingTaskMap.Delete(taskName + "cancel")
		}
	}
}

func cleanupPartialComponentFiles(bfs *provisioningv1.BlueFieldSoftware) error {
	var errs []error
	for _, componentType := range componentTypesWithDownloads() {
		specURL := butil.SpecURLForComponent(bfs, componentType)
		if specURL == "" || !isURL(specURL) {
			continue
		}

		fileName := butil.ComponentDownloadFilename(bfs, componentType, specURL)
		destPath := componentDestinationPath(componentType, fileName)
		var downloaded string
		switch componentType {
		case butil.ComponentTypeFwBundle:
			downloaded = bfs.Status.DownloadedComponents.PldmFwBundle
		case butil.ComponentTypePlatformFwBundle:
			downloaded = bfs.Status.DownloadedComponents.PlatformPldmFwBundle
		case butil.ComponentTypeOSISO:
			downloaded = bfs.Status.DownloadedComponents.OsIso
		case butil.ComponentTypeNicFw:
			downloaded = bfs.Status.DownloadedComponents.NicFw
		}
		if downloaded == destPath {
			continue
		}

		// Only remove in-flight .tmp artifacts. Completed downloads are atomically renamed
		// to destPath before status is updated; removing destPath here would race with
		// a sibling failure and delete a valid file.
		pattern := filepath.Join(filepath.Dir(destPath), filepath.Base(destPath)+"-*.tmp")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			errs = append(errs, fmt.Errorf("glob partial component files %q: %w", pattern, err))
			continue
		}
		for _, match := range matches {
			if err := cutil.RemoveFileEx(match); err != nil {
				errs = append(errs, fmt.Errorf("remove partial component file %q: %w", match, err))
			}
		}
	}
	return errors.Join(errs...)
}

// componentTypesWithExtraction returns the bundle components unpacked into *-extracted
// directories during the Extracting phase.
func componentTypesWithExtraction() []butil.ComponentType {
	return []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypePlatformFwBundle,
	}
}

// cleanupExtractedComponentDirs removes the *-extracted output directories produced
// during the Extracting phase. They are regenerable from the downloaded bundles, so
// removing them on Error lets a subsequent retry re-extract cleanly instead of leaving
// partial firmware files on shared bfb storage.
func cleanupExtractedComponentDirs(bfs *provisioningv1.BlueFieldSoftware) error {
	var errs []error
	for _, componentType := range componentTypesWithExtraction() {
		extractDir := extractOutputDirForBFS(bfs, componentType)
		if extractDir == "" {
			continue
		}
		if err := cutil.RemoveAllEx(extractDir); err != nil {
			errs = append(errs, fmt.Errorf("remove extract output directory %q: %w", extractDir, err))
		}
	}
	return errors.Join(errs...)
}

func cleanupInFlightComponentArtifacts(bfs *provisioningv1.BlueFieldSoftware) error {
	cancelDownloadsForBFS(bfs)
	return errors.Join(cleanupPartialComponentFiles(bfs), cleanupExtractedComponentDirs(bfs))
}

func downloadComponent(ctx context.Context, task butil.ComponentDownloadTask) {
	downloader := future.New(func() (any, error) {
		return executeComponentDownload(ctx, task)
	}, nil)
	butil.DownloadingTaskMap.Store(task.TaskName, downloader)
}

func executeComponentDownload(ctx context.Context, task butil.ComponentDownloadTask) (any, error) {
	logger := log.FromContext(ctx)
	logger.V(3).Info("ComponentDownload", "start downloading", task.ComponentName, "url", task.URL)

	componentFile := componentDestinationPath(butil.ComponentType(task.ComponentName), task.FileName)
	if err := utils.DownloadFile(ctx, task.URL, componentFile, 0644); err != nil {
		return nil, err
	}

	logger.V(3).Info("ComponentDownload", "finish", task.ComponentName)
	return true, nil
}

func isFileExist(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func isURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func generateComponentFilePath(fileName string) string {
	return filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components", fileName)
}

// componentDestinationPath returns the on-disk path for a downloaded component.
// OSISO uses the same layout as BFB (GenerateBFBFilePath); other components use bfb/components/.
func componentDestinationPath(componentType butil.ComponentType, fileName string) string {
	if componentType == butil.ComponentTypeOSISO {
		return cutil.GenerateBFBFilePath(fileName)
	}
	return generateComponentFilePath(fileName)
}

// getRetryKey generates a unique key for tracking retry attempts
func (st *blueFieldSoftwareDownloadingState) getRetryKey(componentType butil.ComponentType) string {
	return fmt.Sprintf("%s/%s/%s", st.bfs.Namespace, st.bfs.Name, componentType)
}

// getRetryCount retrieves the current retry count for a component
func (st *blueFieldSoftwareDownloadingState) getRetryCount(retryKey string) int {
	if val, ok := downloadRetryCounter.Load(retryKey); ok {
		return val.(int)
	}
	return 0
}

// incrementRetryCounter increments the retry counter for a component
func (st *blueFieldSoftwareDownloadingState) incrementRetryCounter(retryKey string) {
	downloadRetryCounterLock.Lock()
	defer downloadRetryCounterLock.Unlock()
	currentCount := st.getRetryCount(retryKey)
	downloadRetryCounter.Store(retryKey, currentCount+1)
}

// clearRetryCounter clears the retry counter for a component
func (st *blueFieldSoftwareDownloadingState) clearRetryCounter(componentType butil.ComponentType) {
	retryKey := st.getRetryKey(componentType)
	downloadRetryCounter.Delete(retryKey)
}
