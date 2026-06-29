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
		for j := 0; j <= i; j++ {
			testErr := errors.New("test error")
			_ = st.handleDownloadError(testErr, comp)
		}

		// Verify each component has the expected retry count
		assert.Equal(t, i+1, st.getRetryCount(retryKey))
	}

	// Verify all counters are still independent
	assert.Equal(t, 1, st.getRetryCount(st.getRetryKey(butil.ComponentTypeFwBundle)))
	assert.Equal(t, 2, st.getRetryCount(st.getRetryKey(butil.ComponentTypePlatformFwBundle)))
	assert.Equal(t, 3, st.getRetryCount(st.getRetryKey(butil.ComponentTypeOSISO)))

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
