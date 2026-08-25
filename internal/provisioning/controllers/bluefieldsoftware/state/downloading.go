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
	st.ensureOSISODOCAVersion()

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
	if !isURL(component.URL) {
		// Non-URL values are opaque refs: no download, but status records the synthetic
		// destination path so componentDownloadSatisfied can match on later reconciles.
		st.updateComponentStatus(component, componentDestinationPath(component.ComponentType, st.fileName(component)))
		return true, nil
	}

	taskName := st.taskName(component)
	if taskFuture, ok := butil.DownloadingTaskMap.Load(taskName); ok {
		return st.handleExistingTask(taskFuture, taskName, component)
	}

	return st.handleNewDownload(ctx, component, taskName)
}

func (st *blueFieldSoftwareDownloadingState) handleExistingTask(taskFuture interface{}, taskName string, component componentInfo) (bool, error) {
	result := taskFuture.(*future.Future)
	if result.GetState() != future.Ready {
		return false, nil
	}

	butil.DownloadingTaskMap.Delete(taskName + "cancel")
	butil.DownloadingTaskMap.Delete(taskName)

	if _, err := result.GetResult(); err != nil {
		return false, st.handleDownloadError(err, component)
	}

	st.updateComponentStatus(component, componentDestinationPath(component.ComponentType, st.fileName(component)))
	return true, nil
}

func (st *blueFieldSoftwareDownloadingState) handleDownloadError(err error, component componentInfo) error {
	componentType := component.ComponentType
	if errors.Is(err, context.Canceled) {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonAwaitingDeletion, conditions.ConditionMessage(fmt.Sprintf("BlueFieldSoftware %s download canceled, deletion in progress", componentType)))
		st.clearRetryCounter(component)
		return nil
	}

	retryKey := st.getRetryKey(component)
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
		taskName := st.taskName(component)
		butil.DownloadingTaskMap.Delete(taskName)
		butil.DownloadingTaskMap.Delete(taskName + "cancel")
		return nil
	}

	// Max immediate retries reached. Classify the failure so the Error state knows whether
	// to keep retrying: recoverable (ReasonError) for transient storage/network conditions
	// and terminal (ReasonFailure) for issues that need user intervention.
	st.clearRetryCounter(component)
	reason := conditions.ReasonFailure
	if cutil.IsRecoverableDownloadError(err) {
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

func (st *blueFieldSoftwareDownloadingState) handleNewDownload(ctx context.Context, component componentInfo, taskName string) (bool, error) {
	fileName := st.fileName(component)
	filePath := componentDestinationPath(component.ComponentType, fileName)

	exist, err := isFileExist(filePath)
	if err != nil {
		return false, st.handleFileCheckError(err, component.ComponentType)
	}

	if exist {
		st.updateComponentStatus(component, filePath)
		return true, nil
	}

	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, fmt.Sprintf("Starting download component %s: (%s/%s)", component.ComponentType, st.bfs.Namespace, st.bfs.Name))

	st.startDownload(ctx, component.ComponentType, component.URL, fileName, taskName)
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
	// Key is the PSID for per-PSID components (platform PLDM bundle) and "" for single-valued components.
	Key string
}

func (st *blueFieldSoftwareDownloadingState) taskName(c componentInfo) string {
	return componentTaskName(st.bfs, c)
}

func (st *blueFieldSoftwareDownloadingState) fileName(c componentInfo) string {
	return componentFileName(st.bfs, c)
}

// componentTaskName returns the download task name for a component, keeping per-PSID
// DPU bundle downloads distinct.
func componentTaskName(bfs *provisioningv1.BlueFieldSoftware, c componentInfo) string {
	if c.ComponentType == butil.ComponentTypeFwBundle {
		return butil.PldmTaskName(bfs, c.Key)
	}
	return butil.GenerateComponentTaskName(*bfs, c.ComponentType)
}

// componentFileName returns the on-disk filename for a component's download.
func componentFileName(bfs *provisioningv1.BlueFieldSoftware, c componentInfo) string {
	if c.ComponentType == butil.ComponentTypeFwBundle {
		return butil.PldmComponentFilename(bfs, c.Key, c.URL)
	}
	return butil.ComponentDownloadFilename(bfs, c.ComponentType, c.URL)
}

// specComponentUnits expands the spec into individual (componentType, key, url) units,
// with one unit per PSID for the DPU PLDM bundle.
func specComponentUnits(bfs *provisioningv1.BlueFieldSoftware) []componentInfo {
	pldmBundles := butil.PldmFwBundles(bfs)
	platformBundle := ptr.Deref(bfs.Spec.PlatformPldmFwBundle, "")
	nicFw := ptr.Deref(bfs.Spec.NicFw, "")
	capacity := len(pldmBundles)
	for _, url := range []string{bfs.Spec.OsIso, platformBundle, nicFw} {
		if url != "" {
			capacity++
		}
	}
	units := make([]componentInfo, 0, capacity)
	if bfs.Spec.OsIso != "" {
		units = append(units, componentInfo{URL: bfs.Spec.OsIso, ComponentType: butil.ComponentTypeOSISO})
	}
	for psid, url := range pldmBundles {
		units = append(units, componentInfo{URL: url, ComponentType: butil.ComponentTypeFwBundle, Key: psid})
	}
	if platformBundle != "" {
		units = append(units, componentInfo{URL: platformBundle, ComponentType: butil.ComponentTypePlatformFwBundle})
	}
	if nicFw != "" {
		units = append(units, componentInfo{URL: nicFw, ComponentType: butil.ComponentTypeNicFw})
	}
	return units
}

// componentDownloadSatisfied returns true when status reflects a completed download.
// URL-based components must also still exist on disk.
func (st *blueFieldSoftwareDownloadingState) componentDownloadSatisfied(component componentInfo) bool {
	expected := componentDestinationPath(component.ComponentType, st.fileName(component))
	if downloadedComponentPath(st.bfs, component.ComponentType, component.Key) != expected {
		return false
	}
	if isURL(component.URL) {
		exists, err := isFileExist(expected)
		return err == nil && exists
	}
	return true
}

func (st *blueFieldSoftwareDownloadingState) getComponentsToDownload() []componentInfo {
	var components []componentInfo
	for _, c := range specComponentUnits(st.bfs) {
		if !st.componentDownloadSatisfied(c) {
			components = append(components, c)
		}
	}
	return components
}

func (st *blueFieldSoftwareDownloadingState) updateComponentStatus(component componentInfo, destinationPath string) {
	setDownloadedComponentPath(st.bfs, component.ComponentType, component.Key, destinationPath)
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, fmt.Sprintf("Component %s downloaded successfully", component.ComponentType))
	// Clear retry counter on successful download
	st.clearRetryCounter(component)
}

// downloadedComponentPath returns the recorded local path for a component/key from status.
func downloadedComponentPath(bfs *provisioningv1.BlueFieldSoftware, componentType butil.ComponentType, key string) string {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		return bfs.Status.DownloadedComponents.PldmFwBundle[key]
	case butil.ComponentTypePlatformFwBundle:
		return bfs.Status.DownloadedComponents.PlatformPldmFwBundle
	case butil.ComponentTypeOSISO:
		return bfs.Status.DownloadedComponents.OsIso
	case butil.ComponentTypeNicFw:
		return bfs.Status.DownloadedComponents.NicFw
	}
	return ""
}

// setDownloadedComponentPath records the local path for a component/key in status,
// lazily allocating the per-PSID maps.
func setDownloadedComponentPath(bfs *provisioningv1.BlueFieldSoftware, componentType butil.ComponentType, key, path string) {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		if bfs.Status.DownloadedComponents.PldmFwBundle == nil {
			bfs.Status.DownloadedComponents.PldmFwBundle = map[string]string{}
		}
		bfs.Status.DownloadedComponents.PldmFwBundle[key] = path
	case butil.ComponentTypePlatformFwBundle:
		bfs.Status.DownloadedComponents.PlatformPldmFwBundle = path
	case butil.ComponentTypeOSISO:
		bfs.Status.DownloadedComponents.OsIso = path
	case butil.ComponentTypeNicFw:
		bfs.Status.DownloadedComponents.NicFw = path
	}
}

// ensureOSISODOCAVersion derives the DOCA version from the downloaded OS ISO filename and records
// it in status. Published bf4-os-doca-bundle images encode the version in the filename
// (e.g. bf4-os-doca-bundle-3.3.0-341_...), so it is re-derived on every call to stay in sync with
// the current Spec.OsIso even after the URL changes.
//
// It is best-effort and never blocks the BlueFieldSoftware from becoming Ready:
//   - A non-URL OsIso is an opaque identifier that is never downloaded, so there is no ISO file.
//   - A URL whose filename does not follow the doca-bundle-X.Y.Z convention (custom mirror, renamed
//     file, future naming change) simply leaves the version unset.
//
// verifyVersionMatching surfaces a clear error only when a DPUServiceTemplate declares a version
// constraint that cannot be checked against a missing DOCA version.
func (st *blueFieldSoftwareDownloadingState) ensureOSISODOCAVersion() {
	if st.bfs.Spec.OsIso == "" || !isURL(st.bfs.Spec.OsIso) {
		return
	}
	isoPath := st.bfs.Status.DownloadedComponents.OsIso
	if isoPath == "" {
		return
	}
	if err := butil.ApplyDOCAVersionFromISO(st.bfs, isoPath); err != nil {
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason,
			"Could not derive DOCA version from OS ISO (%s/%s): %s", st.bfs.Namespace, st.bfs.Name, err.Error())
	}
}

func (st *blueFieldSoftwareDownloadingState) cancelAllDownloads() {
	cancelDownloadsForBFS(st.bfs)
}

func cancelDownloadsForBFS(bfs *provisioningv1.BlueFieldSoftware) {
	for _, unit := range specComponentUnits(bfs) {
		taskName := componentTaskName(bfs, unit)
		if cancelFunc, ok := butil.DownloadingTaskMap.Load(taskName + "cancel"); ok {
			cancelFunc.(context.CancelFunc)()
			butil.DownloadingTaskMap.Delete(taskName)
			butil.DownloadingTaskMap.Delete(taskName + "cancel")
		}
	}
}

func cleanupPartialComponentFiles(bfs *provisioningv1.BlueFieldSoftware) error {
	var errs []error
	for _, unit := range specComponentUnits(bfs) {
		if unit.URL == "" || !isURL(unit.URL) {
			continue
		}

		destPath := componentDestinationPath(unit.ComponentType, componentFileName(bfs, unit))
		if downloadedComponentPath(bfs, unit.ComponentType, unit.Key) == destPath {
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

// extractionUnits returns the bundle components (with per-PSID keys) unpacked into
// *-extracted directories during the Extracting phase.
func extractionUnits(bfs *provisioningv1.BlueFieldSoftware) []componentInfo {
	var units []componentInfo
	for _, u := range specComponentUnits(bfs) {
		switch u.ComponentType {
		case butil.ComponentTypeFwBundle, butil.ComponentTypePlatformFwBundle:
			units = append(units, u)
		}
	}
	return units
}

// cleanupExtractedComponentDirs removes the *-extracted output directories produced
// during the Extracting phase. They are regenerable from the downloaded bundles, so
// removing them on Error lets a subsequent retry re-extract cleanly instead of leaving
// partial firmware files on shared bfb storage.
func cleanupExtractedComponentDirs(bfs *provisioningv1.BlueFieldSoftware) error {
	var errs []error
	for _, unit := range extractionUnits(bfs) {
		extractDir := extractOutputDirForBFS(bfs, unit.ComponentType, unit.Key)
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

// completedComponentFilePath returns the absolute path of a fully-downloaded component
// file for URL-based specs, or "" when there is no local file (opaque/non-URL spec). It
// prefers the path recorded in status and falls back to the deterministic destination.
func completedComponentFilePath(bfs *provisioningv1.BlueFieldSoftware, unit componentInfo) string {
	if unit.URL == "" || !isURL(unit.URL) {
		return ""
	}
	if p := downloadedComponentPath(bfs, unit.ComponentType, unit.Key); p != "" {
		return p
	}
	return componentDestinationPath(unit.ComponentType, componentFileName(bfs, unit))
}

// cleanupCompletedComponentFiles removes completed (fully downloaded) component files for
// every download component. Unlike cleanupPartialComponentFiles it also removes the final
// destPath, so it must only run when no retry will reuse the files and no concurrent
// download can race the removal (terminal Error or deletion). See issue 5104307.
func cleanupCompletedComponentFiles(bfs *provisioningv1.BlueFieldSoftware) error {
	var errs []error
	for _, unit := range specComponentUnits(bfs) {
		filePath := completedComponentFilePath(bfs, unit)
		if filePath == "" {
			continue
		}
		if err := cutil.RemoveFileEx(filePath); err != nil {
			errs = append(errs, fmt.Errorf("remove completed component file %q: %w", filePath, err))
		}
	}
	return errors.Join(errs...)
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

// getRetryKey generates a unique key for tracking retry attempts (per PSID for platform bundles)
func (st *blueFieldSoftwareDownloadingState) getRetryKey(component componentInfo) string {
	return fmt.Sprintf("%s/%s/%s", st.bfs.Namespace, st.bfs.Name, componentTaskName(st.bfs, component))
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
func (st *blueFieldSoftwareDownloadingState) clearRetryCounter(component componentInfo) {
	downloadRetryCounter.Delete(st.getRetryKey(component))
}
