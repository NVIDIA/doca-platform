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
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
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

	butil.DownloadingTaskMap.Delete(taskName)
	butil.DownloadingTaskMap.Delete(taskName + "cancel")

	if _, err := result.GetResult(); err != nil {
		return false, st.handleDownloadError(err, componentType)
	}

	st.updateComponentStatus(componentType, componentURL)
	return true, nil
}

func (st *blueFieldSoftwareDownloadingState) handleDownloadError(err error, componentType butil.ComponentType) error {
	if errors.Is(err, context.Canceled) {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonAwaitingDeletion, conditions.ConditionMessage(fmt.Sprintf("BlueFieldSoftware %s download canceled, deletion in progress", componentType)))
		return nil
	}

	msg := fmt.Sprintf("Download component %s: (%s/%s) failed with error: %s",
		componentType, st.bfs.Namespace, st.bfs.Name, err.Error())
	st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedDownloadBFBReason, msg)
	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareError
	conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
		conditions.ReasonError, conditions.ConditionMessage(msg))
	return err
}

func (st *blueFieldSoftwareDownloadingState) handleNewDownload(ctx context.Context, componentType butil.ComponentType, componentURL, taskName string) (bool, error) {
	fileName := butil.DefaultComponentFilename(st.bfs, componentType)
	filePath := generateComponentFilePath(fileName)

	exist, err := isFileExist(filePath)
	if err != nil {
		return false, st.handleFileCheckError(err, componentType)
	}

	if exist {
		st.updateComponentStatus(componentType, componentURL)
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

func (st *blueFieldSoftwareDownloadingState) getComponentsToDownload() []componentInfo {
	var components []componentInfo

	// Check FwBundleURL
	if st.bfs.Spec.PldmFwBundle != "" && st.bfs.Status.DownloadedComponents.PldmFWBundle != st.bfs.Spec.PldmFwBundle {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.PldmFwBundle,
			ComponentType: butil.ComponentTypeFwBundle,
		})
	}

	// Check OSISO
	if st.bfs.Spec.OsIso != "" && st.bfs.Status.DownloadedComponents.OsIso != st.bfs.Spec.OsIso {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.OsIso,
			ComponentType: butil.ComponentTypeOSISO,
		})
	}

	// Check TmpFwComponents.BMCEROT
	if st.bfs.Spec.TmpFwComponents.BmcErot != "" && st.bfs.Status.DownloadedComponents.BmcErot != st.bfs.Spec.TmpFwComponents.BmcErot {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.TmpFwComponents.BmcErot,
			ComponentType: butil.ComponentTypeBMCEROT,
		})
	}

	// Check TmpFwComponents.BMC
	if st.bfs.Spec.TmpFwComponents.BmcFw != "" && st.bfs.Status.DownloadedComponents.BmcFw != st.bfs.Spec.TmpFwComponents.BmcFw {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.TmpFwComponents.BmcFw,
			ComponentType: butil.ComponentTypeBMC,
		})
	}

	// Check TmpFwComponents.NIC
	if st.bfs.Spec.TmpFwComponents.AstraNicFw != "" && st.bfs.Status.DownloadedComponents.AstraNicFw != st.bfs.Spec.TmpFwComponents.AstraNicFw {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.TmpFwComponents.AstraNicFw,
			ComponentType: butil.ComponentTypeNIC,
		})
	}

	// Check TmpFwComponents.GRACEEROT
	if st.bfs.Spec.TmpFwComponents.GraceErot != "" && st.bfs.Status.DownloadedComponents.GraceErot != st.bfs.Spec.TmpFwComponents.GraceErot {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.TmpFwComponents.GraceErot,
			ComponentType: butil.ComponentTypeGRACEEROT,
		})
	}

	// Check TmpFwComponents.GRACEFW
	if st.bfs.Spec.TmpFwComponents.GraceFw != "" && st.bfs.Status.DownloadedComponents.GraceFw != st.bfs.Spec.TmpFwComponents.GraceFw {
		components = append(components, componentInfo{
			URL:           st.bfs.Spec.TmpFwComponents.GraceFw,
			ComponentType: butil.ComponentTypeGRACEFW,
		})
	}

	return components
}

func (st *blueFieldSoftwareDownloadingState) updateComponentStatus(componentType butil.ComponentType, value string) {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		st.bfs.Status.DownloadedComponents.PldmFWBundle = value
	case butil.ComponentTypeOSISO:
		st.bfs.Status.DownloadedComponents.OsIso = value
	case butil.ComponentTypeBMCEROT:
		st.bfs.Status.DownloadedComponents.BmcErot = value
	case butil.ComponentTypeBMC:
		st.bfs.Status.DownloadedComponents.BmcFw = value
	case butil.ComponentTypeNIC:
		st.bfs.Status.DownloadedComponents.AstraNicFw = value
	case butil.ComponentTypeGRACEEROT:
		st.bfs.Status.DownloadedComponents.GraceErot = value
	case butil.ComponentTypeGRACEFW:
		st.bfs.Status.DownloadedComponents.GraceFw = value
	}
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulDownloadBFBReason, fmt.Sprintf("Component %s downloaded successfully", componentType))
}

func (st *blueFieldSoftwareDownloadingState) cancelAllDownloads() {
	componentsToCancel := []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypeOSISO,
		butil.ComponentTypeBMCEROT,
		butil.ComponentTypeBMC,
		butil.ComponentTypeNIC,
		butil.ComponentTypeGRACEEROT,
		butil.ComponentTypeGRACEFW,
	}

	for _, componentType := range componentsToCancel {
		taskName := butil.GenerateComponentTaskName(*st.bfs, componentType)
		if cancelFunc, ok := butil.DownloadingTaskMap.Load(taskName + "cancel"); ok {
			cancelFunc.(context.CancelFunc)()
			butil.DownloadingTaskMap.Delete(taskName)
			butil.DownloadingTaskMap.Delete(taskName + "cancel")
		}
	}
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

	tempFile, err := createTempFile(task)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempFile.Name()) //nolint: errcheck

	resp, err := fetchComponentFromURL(ctx, task.URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint: errcheck

	if err := copyResponseToFile(ctx, resp.Body, tempFile, task.ComponentName, logger); err != nil {
		return nil, err
	}

	if err := tempFile.Close(); err != nil {
		return nil, err
	}

	if err := finalizeComponentFile(tempFile.Name(), task.FileName); err != nil {
		return nil, err
	}

	logger.V(3).Info("ComponentDownload", "finish", task.ComponentName)
	return true, nil
}

func createTempFile(task butil.ComponentDownloadTask) (*os.File, error) {
	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	if err := os.MkdirAll(componentsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create components directory: %w", err)
	}

	tempFileName := filepath.Join(componentsDir, fmt.Sprintf("tmp-%s-%s", task.ComponentName, string(task.UID)))
	return os.Create(tempFileName)
}

func fetchComponentFromURL(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() //nolint: errcheck
		return nil, fmt.Errorf("failed to get: %s status: %d", url, resp.StatusCode)
	}

	return resp, nil
}

func copyResponseToFile(ctx context.Context, src io.Reader, dest *os.File, componentName string, logger logr.Logger) error {
	buf := make([]byte, 4*1024*1024)
	if _, err := io.CopyBuffer(dest, src, buf); err != nil {
		logger.V(3).Info("ComponentDownload", "failed to copy response to file", "componentName", componentName, "error", err)
		if errors.Is(err, context.Canceled) {
			return ctx.Err()
		}
		return fmt.Errorf("failed to copy response to file: %w", err)
	}
	return nil
}

func finalizeComponentFile(tempFileName, finalFileName string) error {
	componentFile := generateComponentFilePath(finalFileName)
	if err := os.Rename(tempFileName, componentFile); err != nil {
		return err
	}
	return os.Chmod(componentFile, 0644)
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
