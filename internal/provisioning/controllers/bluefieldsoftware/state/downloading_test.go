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
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
)

func TestHandleDownloadError_ContextCanceled(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{},
	}

	scheme := runtime.NewScheme()
	_ = provisioningv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	recorder := record.NewFakeRecorder(10)

	st := &blueFieldSoftwareDownloadingState{
		bfs:      bfs,
		recorder: recorder,
	}

	// Set a retry counter to verify it gets cleared
	retryKey := st.getRetryKey(butil.ComponentTypeFwBundle)
	downloadRetryCounter.Store(retryKey, 2)

	err := st.handleDownloadError(context.Canceled, butil.ComponentTypeFwBundle)

	// Should return nil for canceled errors
	assert.NoError(t, err)

	// Phase should be set to Deleting
	assert.Equal(t, provisioningv1.BlueFieldSoftwareDeleting, bfs.Status.Phase)

	// Condition should be added with AwaitingDeletion reason
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, string(conditions.ReasonAwaitingDeletion), cond.Reason)

	// Retry counter should be cleared
	assert.Equal(t, 0, st.getRetryCount(retryKey))
}

func TestHandleDownloadError(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{},
	}

	recorder := record.NewFakeRecorder(10)

	st := &blueFieldSoftwareDownloadingState{
		bfs:      bfs,
		recorder: recorder,
	}

	// Clear and set retry counter to simulate two failures already occurred
	retryKey := st.getRetryKey(butil.ComponentTypeOSISO)
	st.clearRetryCounter(butil.ComponentTypeOSISO)
	downloadRetryCounter.Store(retryKey, 2)

	testErr := errors.New("disk full")
	err := st.handleDownloadError(testErr, butil.ComponentTypeOSISO)

	// Should return nil to allow retry (still under max)
	assert.NoError(t, err)

	// Condition should show retry attempt 3
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, cond)
	assert.Equal(t, string(conditions.ReasonRetrying), cond.Reason)
	assert.Contains(t, cond.Message, "Retry attempt 3/3")

	// Retry counter should be incremented to 3
	assert.Equal(t, 3, st.getRetryCount(retryKey))

	// Cleanup
	st.clearRetryCounter(butil.ComponentTypeOSISO)
}

func TestHandleDownloadError_MaxRetriesReached(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{},
	}

	recorder := record.NewFakeRecorder(10)

	st := &blueFieldSoftwareDownloadingState{
		bfs:      bfs,
		recorder: recorder,
	}

	// Clear and set retry counter to simulate max retries already occurred
	retryKey := st.getRetryKey(butil.ComponentTypeFwBundle)
	st.clearRetryCounter(butil.ComponentTypeFwBundle)
	downloadRetryCounter.Store(retryKey, maxDownloadRetries)

	testErr := errors.New("permanent failure")
	err := st.handleDownloadError(testErr, butil.ComponentTypeFwBundle)

	// Should return the error after max retries
	assert.Error(t, err)
	assert.Equal(t, testErr, err)

	// Phase should be set to Error
	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)

	// Condition should be added with Failure reason
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, string(conditions.ReasonFailure), cond.Reason)
	assert.Contains(t, cond.Message, fmt.Sprintf("failed after %d attempts", maxDownloadRetries))
	assert.Contains(t, cond.Message, "permanent failure")

	// Retry counter should be cleared
	assert.Equal(t, 0, st.getRetryCount(retryKey))

	// Check event was recorded
	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "Warning")
		assert.Contains(t, event, fmt.Sprintf("failed after %d attempts", maxDownloadRetries))
	default:
		t.Fatal("Expected event to be recorded")
	}
}

func TestRetryCounter_IndependentPerComponent(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{},
	}

	recorder := record.NewFakeRecorder(10)

	st := &blueFieldSoftwareDownloadingState{
		bfs:      bfs,
		recorder: recorder,
	}

	// Fail different components and verify counters are independent
	components := []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypePlatformFwBundle,
		butil.ComponentTypeOSISO,
		butil.ComponentTypeNicFw,
	}

	// Clear any existing retry counters from previous tests
	for _, comp := range components {
		st.clearRetryCounter(comp)
	}

	for i, comp := range components {
		// Each component should start with 0 retries
		retryKey := st.getRetryKey(comp)
		assert.Equal(t, 0, st.getRetryCount(retryKey))

		// Fail each component a different number of times
		failures := i + 1
		if failures > maxDownloadRetries {
			failures = maxDownloadRetries
		}
		for j := 0; j < failures; j++ {
			testErr := errors.New("test error")
			_ = st.handleDownloadError(testErr, comp)
		}

		// Verify each component has the expected retry count
		assert.Equal(t, failures, st.getRetryCount(retryKey))
	}

	// Verify all counters are still independent
	assert.Equal(t, 1, st.getRetryCount(st.getRetryKey(butil.ComponentTypeFwBundle)))
	assert.Equal(t, 2, st.getRetryCount(st.getRetryKey(butil.ComponentTypePlatformFwBundle)))
	assert.Equal(t, 3, st.getRetryCount(st.getRetryKey(butil.ComponentTypeOSISO)))
	assert.Equal(t, 3, st.getRetryCount(st.getRetryKey(butil.ComponentTypeNicFw)))

	// Cleanup
	for _, comp := range components {
		st.clearRetryCounter(comp)
	}
}

func TestUpdateComponentStatus_ClearsRetryCounter(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{},
	}

	recorder := record.NewFakeRecorder(10)

	st := &blueFieldSoftwareDownloadingState{
		bfs:      bfs,
		recorder: recorder,
	}

	// Set retry counters for different components
	components := []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypePlatformFwBundle,
		butil.ComponentTypeOSISO,
		butil.ComponentTypeNicFw,
	}

	for i, comp := range components {
		retryKey := st.getRetryKey(comp)
		downloadRetryCounter.Store(retryKey, i+1)
	}

	// Verify counters are set
	for i, comp := range components {
		retryKey := st.getRetryKey(comp)
		assert.Equal(t, i+1, st.getRetryCount(retryKey))
	}

	// Update status for FwBundle (should clear its counter)
	expectedFw := componentDestinationPath(butil.ComponentTypeFwBundle, butil.ComponentDownloadFilename(bfs, butil.ComponentTypeFwBundle, ""))
	st.updateComponentStatus(butil.ComponentTypeFwBundle, expectedFw)

	// FwBundle counter should be cleared
	assert.Equal(t, 0, st.getRetryCount(st.getRetryKey(butil.ComponentTypeFwBundle)))

	// Other counters should remain unchanged
	assert.Equal(t, 2, st.getRetryCount(st.getRetryKey(butil.ComponentTypePlatformFwBundle)))
	assert.Equal(t, 3, st.getRetryCount(st.getRetryKey(butil.ComponentTypeOSISO)))
	assert.Equal(t, 4, st.getRetryCount(st.getRetryKey(butil.ComponentTypeNicFw)))

	// Verify status holds the on-disk destination path (not the spec URL)
	assert.Equal(t, expectedFw, bfs.Status.DownloadedComponents.PldmFwBundle)
}

func TestGetRetryKey(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		bfsName       string
		componentType butil.ComponentType
		expected      string
	}{
		{
			name:          "FwBundle component",
			namespace:     "default",
			bfsName:       "my-bfs",
			componentType: butil.ComponentTypeFwBundle,
			expected:      "default/my-bfs/fwbundle",
		},
		{
			name:          "OSISO component",
			namespace:     "test-ns",
			bfsName:       "test-bfs",
			componentType: butil.ComponentTypeOSISO,
			expected:      "test-ns/test-bfs/osiso",
		},
		{
			name:          "PlatformPldmFwBundle component",
			namespace:     "test-ns",
			bfsName:       "test-bfs",
			componentType: butil.ComponentTypePlatformFwBundle,
			expected:      "test-ns/test-bfs/platformpldmfwbundle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.bfsName,
					Namespace: tt.namespace,
				},
			}

			st := &blueFieldSoftwareDownloadingState{bfs: bfs}
			key := st.getRetryKey(tt.componentType)

			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestClearRetryCounter(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
	}

	st := &blueFieldSoftwareDownloadingState{bfs: bfs}

	// Set retry counters
	components := []butil.ComponentType{
		butil.ComponentTypeFwBundle,
		butil.ComponentTypePlatformFwBundle,
		butil.ComponentTypeOSISO,
		butil.ComponentTypeNicFw,
	}

	for _, comp := range components {
		retryKey := st.getRetryKey(comp)
		downloadRetryCounter.Store(retryKey, 5)
	}

	// Clear individual counters
	st.clearRetryCounter(butil.ComponentTypeFwBundle)

	// Verify cleared counters are 0
	assert.Equal(t, 0, st.getRetryCount(st.getRetryKey(butil.ComponentTypeFwBundle)))

	// Verify non-cleared counters remain
	assert.Equal(t, 5, st.getRetryCount(st.getRetryKey(butil.ComponentTypePlatformFwBundle)))
	assert.Equal(t, 5, st.getRetryCount(st.getRetryKey(butil.ComponentTypeOSISO)))
	assert.Equal(t, 5, st.getRetryCount(st.getRetryKey(butil.ComponentTypeNicFw)))
}

func TestIncrementRetryCounter_ThreadSafety(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-bfs",
			Namespace:  "test-ns",
			Generation: 1,
		},
	}

	st := &blueFieldSoftwareDownloadingState{bfs: bfs}
	retryKey := st.getRetryKey(butil.ComponentTypeFwBundle)

	// Clear any existing counter
	downloadRetryCounter.Delete(retryKey)

	// Increment counter concurrently
	const numGoroutines = 100
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			st.incrementRetryCounter(retryKey)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify counter is exactly numGoroutines (no race conditions)
	assert.Equal(t, numGoroutines, st.getRetryCount(retryKey))
}

// Test downloadComponent function
func TestDownloadComponent(t *testing.T) {
	// Create a test HTTP server
	testContent := "test component data"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, testContent)
	}))
	defer server.Close()

	// Setup test directory
	tempDir := t.TempDir()

	// Override BFB base dir for testing (without leading slash for proper path join)
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	defer func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}()

	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	err := os.MkdirAll(componentsDir, 0755)
	require.NoError(t, err)

	task := butil.ComponentDownloadTask{
		TaskName:      "test-ns-test-bfs-fwbundle",
		URL:           server.URL,
		FileName:      "test-component.tar",
		ComponentName: "fwbundle",
		UID:           types.UID("test-uid-123"),
	}

	ctx := context.Background()

	// Call downloadComponent
	downloadComponent(ctx, task)

	// Wait for download to complete
	var downloadFuture *future.Future
	require.Eventually(t, func() bool {
		if val, ok := butil.DownloadingTaskMap.Load(task.TaskName); ok {
			downloadFuture = val.(*future.Future)
			return downloadFuture.GetState() == future.Ready
		}
		return false
	}, 5*time.Second, 100*time.Millisecond, "Download did not complete in time")

	// Verify the result
	result, err := downloadFuture.GetResult()
	assert.NoError(t, err)
	assert.Equal(t, true, result)

	// Verify file was created at componentDestinationPath (FwBundle uses bfb/components/)
	filePath := componentDestinationPath(butil.ComponentTypeFwBundle, task.FileName)
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content))

	// Cleanup
	butil.DownloadingTaskMap.Delete(task.TaskName)
}

func TestDownloadComponent_Failure(t *testing.T) {
	// Create a test HTTP server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Setup test directory
	tempDir := t.TempDir()

	// Override BFB base dir for testing (without leading slash for proper path join)
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	defer func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}()

	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	err := os.MkdirAll(componentsDir, 0755)
	require.NoError(t, err)

	task := butil.ComponentDownloadTask{
		TaskName:      "test-ns-test-bfs-osiso",
		URL:           server.URL,
		FileName:      "test-component-404.tar",
		ComponentName: "osiso",
		UID:           types.UID("test-uid-456"),
	}

	ctx := context.Background()

	// Call downloadComponent
	downloadComponent(ctx, task)

	// Wait for download to complete (with error)
	var downloadFuture *future.Future
	require.Eventually(t, func() bool {
		if val, ok := butil.DownloadingTaskMap.Load(task.TaskName); ok {
			downloadFuture = val.(*future.Future)
			return downloadFuture.GetState() == future.Ready
		}
		return false
	}, 5*time.Second, 100*time.Millisecond, "Download did not complete in time")

	// Verify the error
	_, err = downloadFuture.GetResult()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")

	// Cleanup
	butil.DownloadingTaskMap.Delete(task.TaskName)
}

func TestExecuteComponentDownload_Success(t *testing.T) {
	// Create a test HTTP server
	testContent := "component download test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, testContent)
	}))
	defer server.Close()

	// Setup test directory
	tempDir := t.TempDir()

	// Override BFB base dir for testing (without leading slash for proper path join)
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	defer func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}()

	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	err := os.MkdirAll(componentsDir, 0755)
	require.NoError(t, err)

	task := butil.ComponentDownloadTask{
		TaskName:      "test-task",
		URL:           server.URL,
		FileName:      "test-download.tar",
		ComponentName: "bmc",
		UID:           types.UID("test-uid-789"),
	}

	ctx := context.Background()

	// Execute download
	result, err := executeComponentDownload(ctx, task)

	// Verify success
	assert.NoError(t, err)
	assert.Equal(t, true, result)

	// Verify file was created with correct content
	filePath := generateComponentFilePath(task.FileName)
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content))
}

func TestExecuteComponentDownload_HTTPError(t *testing.T) {
	// Create a test HTTP server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Setup test directory
	tempDir := t.TempDir()

	// Override BFB base dir for testing (without leading slash for proper path join)
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	defer func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}()

	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	err := os.MkdirAll(componentsDir, 0755)
	require.NoError(t, err)

	task := butil.ComponentDownloadTask{
		TaskName:      "test-task-error",
		URL:           server.URL,
		FileName:      "test-error.tar",
		ComponentName: "nic",
		UID:           types.UID("test-uid-error"),
	}

	ctx := context.Background()

	// Execute download
	result, err := executeComponentDownload(ctx, task)

	// Verify error
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "500")
}

func TestExecuteComponentDownload_ContextCanceled(t *testing.T) {
	// Create a slow server to test cancellation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Setup test directory
	tempDir := t.TempDir()

	// Override BFB base dir for testing (without leading slash for proper path join)
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	defer func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}()

	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	err := os.MkdirAll(componentsDir, 0755)
	require.NoError(t, err)

	task := butil.ComponentDownloadTask{
		TaskName:      "test-task-cancel",
		URL:           server.URL,
		FileName:      "test-cancel.tar",
		ComponentName: "graceerot",
		UID:           types.UID("test-uid-cancel"),
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	// Execute download
	result, err := executeComponentDownload(ctx, task)

	// Verify error
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestExecuteComponentDownload_SkipsExistingFile(t *testing.T) {
	// Setup test directory
	tempDir := t.TempDir()

	// Override BFB base dir for testing (without leading slash for proper path join)
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	defer func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}()

	componentsDir := filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components")
	err := os.MkdirAll(componentsDir, 0755)
	require.NoError(t, err)

	// Create existing file
	existingContent := "existing file content"
	fileName := "existing-component.tar"
	filePath := generateComponentFilePath(fileName)
	err = os.WriteFile(filePath, []byte(existingContent), 0644)
	require.NoError(t, err)

	// Create a test HTTP server (should not be called)
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "new content")
	}))
	defer server.Close()

	task := butil.ComponentDownloadTask{
		TaskName:      "test-task-existing",
		URL:           server.URL,
		FileName:      fileName,
		ComponentName: "gracefw",
		UID:           types.UID("test-uid-existing"),
	}

	ctx := context.Background()

	// Execute download
	result, err := executeComponentDownload(ctx, task)

	// Verify success without calling server
	assert.NoError(t, err)
	assert.Equal(t, true, result)
	assert.False(t, serverCalled, "Server should not be called for existing file")

	// Verify file content was not changed
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(content))
}

func TestComponentDestinationPath(t *testing.T) {
	fileName := "ns-bfs-fwbundle"
	t.Run("OSISO uses BFB root layout", func(t *testing.T) {
		assert.Equal(t,
			cutil.GenerateBFBFilePath(fileName),
			componentDestinationPath(butil.ComponentTypeOSISO, fileName))
	})

	t.Run("non-OSISO uses components subdir", func(t *testing.T) {
		for _, ct := range []butil.ComponentType{
			butil.ComponentTypeFwBundle,
			butil.ComponentTypePlatformFwBundle,
			butil.ComponentTypeNicFw,
		} {
			assert.Equal(t,
				generateComponentFilePath(fileName),
				componentDestinationPath(ct, fileName),
				"type %s", ct)
		}
	})
}

func TestComponentDownloadSatisfied(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{Name: "bfs", Namespace: "ns"},
	}
	st := &blueFieldSoftwareDownloadingState{bfs: bfs}
	ct := butil.ComponentTypeFwBundle
	specURL := "https://example.com/path/fw-bundle.tar"
	expectedPath := componentDestinationPath(ct, butil.ComponentDownloadFilename(bfs, ct, specURL))

	t.Run("empty or wrong downloaded value is not satisfied", func(t *testing.T) {
		assert.False(t, st.componentDownloadSatisfied(ct, specURL, ""))
		assert.False(t, st.componentDownloadSatisfied(ct, specURL, "anything"))
	})

	t.Run("status holds destination path", func(t *testing.T) {
		assert.True(t, st.componentDownloadSatisfied(ct, specURL, expectedPath))
	})
	t.Run("mismatch", func(t *testing.T) {
		assert.False(t, st.componentDownloadSatisfied(ct, specURL, "/wrong/path"))
	})
}

func TestGetComponentsToDownload_IncludesNicFw(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{Name: "bfs", Namespace: "ns"},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: "https://example.com/os.iso",
			NicFw: ptr.To("https://example.com/nic.bin"),
		},
	}
	st := &blueFieldSoftwareDownloadingState{bfs: bfs}

	components := st.getComponentsToDownload()

	require.Len(t, components, 2)
	assert.Equal(t, butil.ComponentTypeOSISO, components[0].ComponentType)
	assert.Equal(t, "https://example.com/os.iso", components[0].URL)
	assert.Equal(t, butil.ComponentTypeNicFw, components[1].ComponentType)
	assert.Equal(t, "https://example.com/nic.bin", components[1].URL)
}

const testBF4OsIsoURL = "https://example.com/bf4-os.iso"

func withTestBFBBaseDir(t *testing.T) func() {
	t.Helper()
	tempDir := t.TempDir()
	originalBFBBaseDir := cutil.BFBBaseDir
	cutil.BFBBaseDir = filepath.Join(tempDir, "bfb")
	return func() {
		cutil.BFBBaseDir = originalBFBBaseDir
	}
}

func loadDownloadFuture(t *testing.T, taskName string) *future.Future {
	t.Helper()
	var downloadFuture *future.Future
	require.Eventually(t, func() bool {
		val, ok := butil.DownloadingTaskMap.Load(taskName)
		if !ok {
			return false
		}
		downloadFuture = val.(*future.Future)
		return true
	}, time.Second, 10*time.Millisecond, "download task should be registered")
	return downloadFuture
}

func waitForDownloadFutureDone(t *testing.T, downloadFuture *future.Future) {
	t.Helper()
	require.NotNil(t, downloadFuture)
	require.Eventually(t, func() bool {
		return downloadFuture.GetState() == future.Ready
	}, 5*time.Second, 10*time.Millisecond, "download task should complete")
	_, _ = downloadFuture.GetResult()
}

func TestCleanupPartialComponentFiles(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	osIsoURL := testBF4OsIsoURL
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso:        osIsoURL,
			PldmFwBundle: ptr.To("https://example.com/fw.fwpkg"),
		},
	}

	fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, osIsoURL)
	destPath := componentDestinationPath(butil.ComponentTypeOSISO, fileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destPath), 0755))

	tmpPath := filepath.Join(filepath.Dir(destPath), filepath.Base(destPath)+"-1234567890.tmp")
	require.NoError(t, os.WriteFile(tmpPath, []byte("partial download"), 0644))

	require.NoError(t, cleanupPartialComponentFiles(bfs))

	_, err := os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestCleanupPartialComponentFiles_PreservesCompletedComponent(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	osIsoURL := testBF4OsIsoURL
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: osIsoURL,
		},
	}

	fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, osIsoURL)
	destPath := componentDestinationPath(butil.ComponentTypeOSISO, fileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destPath), 0755))
	require.NoError(t, os.WriteFile(destPath, []byte("complete download"), 0644))
	bfs.Status.DownloadedComponents.OsIso = destPath

	require.NoError(t, cleanupPartialComponentFiles(bfs))

	content, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, "complete download", string(content))
}

func TestCleanupPartialComponentFiles_PreservesCompletedFileBeforeStatusUpdate(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	osIsoURL := testBF4OsIsoURL
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: osIsoURL,
		},
	}

	fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, osIsoURL)
	destPath := componentDestinationPath(butil.ComponentTypeOSISO, fileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destPath), 0755))
	require.NoError(t, os.WriteFile(destPath, []byte("complete download"), 0644))

	require.NoError(t, cleanupPartialComponentFiles(bfs))

	content, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, "complete download", string(content))
}

func TestCleanupExtractedComponentDirs(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle:         ptr.To("https://example.com/fw.fwpkg"),
			PlatformPldmFwBundle: ptr.To("https://example.com/platform.fwpkg"),
		},
	}

	fwExtractDir := extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle)
	platformExtractDir := extractOutputDirForBFS(bfs, butil.ComponentTypePlatformFwBundle)
	for _, dir := range []string{fwExtractDir, platformExtractDir} {
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "image.bin"), []byte("extracted"), 0644))
	}

	require.NoError(t, cleanupExtractedComponentDirs(bfs))

	for _, dir := range []string{fwExtractDir, platformExtractDir} {
		_, err := os.Stat(dir)
		assert.True(t, os.IsNotExist(err), "extract dir %q should be removed", dir)
	}
}

func TestBlueFieldSoftwareErrorState_RemovesExtractedDirs(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: ptr.To("https://example.com/fw.fwpkg"),
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
		},
	}

	extractDir := extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle)
	require.NoError(t, os.MkdirAll(extractDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(extractDir, "image.bin"), []byte("extracted"), 0644))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	_, err := os.Stat(extractDir)
	assert.True(t, os.IsNotExist(err))
}

func TestBlueFieldSoftwareErrorState_CancelsInFlightDownloads(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	downloadStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(downloadStarted)
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		buf := make([]byte, 1024*1024)
		for {
			select {
			case <-r.Context().Done():
				return
			default:
				if _, err := w.Write(buf); err != nil {
					return
				}
				flusher.Flush()
				time.Sleep(20 * time.Millisecond)
			}
		}
	}))
	defer server.Close()

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
			UID:       types.UID("test-uid"),
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso:        server.URL,
			PldmFwBundle: ptr.To("https://example.com/missing.fwpkg"),
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
		},
	}

	osIsoFileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, server.URL)
	taskName := butil.GenerateComponentTaskName(*bfs, butil.ComponentTypeOSISO)

	taskCtx, cancel := context.WithCancel(context.Background())
	butil.DownloadingTaskMap.Store(taskName+"cancel", cancel)
	downloadComponent(taskCtx, butil.ComponentDownloadTask{
		TaskName:      taskName,
		URL:           server.URL,
		FileName:      osIsoFileName,
		ComponentName: string(butil.ComponentTypeOSISO),
		UID:           bfs.UID,
	})

	select {
	case <-downloadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}

	downloadFuture := loadDownloadFuture(t, taskName)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	tmpPattern := filepath.Join(
		filepath.Dir(componentDestinationPath(butil.ComponentTypeOSISO, osIsoFileName)),
		filepath.Base(componentDestinationPath(butil.ComponentTypeOSISO, osIsoFileName))+"-*.tmp",
	)

	require.Eventually(t, func() bool {
		matches, globErr := filepath.Glob(tmpPattern)
		require.NoError(t, globErr)
		return len(matches) == 0
	}, 5*time.Second, 100*time.Millisecond, "partial .tmp files should be removed in Error state")

	waitForDownloadFutureDone(t, downloadFuture)

	butil.DownloadingTaskMap.Delete(taskName)
	butil.DownloadingTaskMap.Delete(taskName + "cancel")
}

func TestBlueFieldSoftwareErrorState_CleansPartialFiles(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	osIsoURL := testBF4OsIsoURL
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: osIsoURL,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
		},
	}

	fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, osIsoURL)
	destPath := componentDestinationPath(butil.ComponentTypeOSISO, fileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destPath), 0755))

	tmpPath := filepath.Join(filepath.Dir(destPath), filepath.Base(destPath)+"-3393455104.tmp")
	require.NoError(t, os.WriteFile(tmpPath, []byte("partial"), 0644))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	_, err := os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestBlueFieldSoftwareErrorState_SetsErrorConditionWhenCleanupFails(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	osIsoURL := testBF4OsIsoURL
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: osIsoURL,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
		},
	}

	fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, osIsoURL)
	destPath := componentDestinationPath(butil.ComponentTypeOSISO, fileName)
	componentDir := filepath.Dir(destPath)
	require.NoError(t, os.MkdirAll(componentDir, 0755))

	tmpPath := filepath.Join(componentDir, filepath.Base(destPath)+"-3393455104.tmp")
	require.NoError(t, os.WriteFile(tmpPath, []byte("partial"), 0644))
	require.NoError(t, os.Chmod(componentDir, 0555))
	t.Cleanup(func() { _ = os.Chmod(componentDir, 0755) })

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	err := st.Handle(context.Background(), nil)
	require.Error(t, err)

	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondError)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
}

func TestBlueFieldSoftwareErrorState_CancelsDownloadsOnDelete(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, &slowReader{chunk: 1024 * 1024, delay: 20 * time.Millisecond})
	}))
	defer server.Close()

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bf4-software",
			Namespace:         "dpf-operator-system",
			UID:               types.UID("test-uid"),
			DeletionTimestamp: ptr.To(metav1.Now()),
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: server.URL,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
		},
	}

	osIsoFileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, server.URL)
	taskName := butil.GenerateComponentTaskName(*bfs, butil.ComponentTypeOSISO)

	taskCtx, cancel := context.WithCancel(context.Background())
	butil.DownloadingTaskMap.Store(taskName+"cancel", cancel)
	downloadComponent(taskCtx, butil.ComponentDownloadTask{
		TaskName:      taskName,
		URL:           server.URL,
		FileName:      osIsoFileName,
		ComponentName: string(butil.ComponentTypeOSISO),
		UID:           bfs.UID,
	})
	downloadFuture := loadDownloadFuture(t, taskName)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))
	assert.Equal(t, provisioningv1.BlueFieldSoftwareDeleting, bfs.Status.Phase)

	require.Eventually(t, func() bool {
		_, ok := butil.DownloadingTaskMap.Load(taskName + "cancel")
		return !ok
	}, 5*time.Second, 100*time.Millisecond, "download cancel func should be removed after delete transition")

	waitForDownloadFutureDone(t, downloadFuture)

	butil.DownloadingTaskMap.Delete(taskName)
	butil.DownloadingTaskMap.Delete(taskName + "cancel")
}

type slowReader struct {
	chunk int
	delay time.Duration
}

func (r *slowReader) Read(p []byte) (int, error) {
	time.Sleep(r.delay)
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if n == 0 {
		return 0, io.EOF
	}
	copy(p, make([]byte, n))
	return n, nil
}

func TestCleanupPartialComponentFiles_GlobPattern(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	osIsoURL := "https://example.com/bf4-os-doca-bundle.iso"
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: osIsoURL,
		},
	}

	fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypeOSISO, osIsoURL)
	destPath := componentDestinationPath(butil.ComponentTypeOSISO, fileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destPath), 0755))

	tmpPath := filepath.Join(filepath.Dir(destPath), fmt.Sprintf("%s-3393455104.tmp", filepath.Base(destPath)))
	require.NoError(t, os.WriteFile(tmpPath, []byte("partial"), 0644))

	require.NoError(t, cleanupPartialComponentFiles(bfs))

	_, err := os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}
