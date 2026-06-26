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
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestResolvePackagePath(t *testing.T) {
	localPath := "/tmp/custom.fwpkg"
	tests := []struct {
		name     string
		bfs      *provisioningv1.BlueFieldSoftware
		expected func(bfs *provisioningv1.BlueFieldSoftware) string
	}{
		{
			name: "status url resolves to cached path",
			bfs: &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "bfs1",
				},
				Status: provisioningv1.BlueFieldSoftwareStatus{
					DownloadedComponents: provisioningv1.DownloadedComponents{
						PldmFwBundle: "https://example.com/fw.fwpkg",
					},
				},
			},
			expected: func(bfs *provisioningv1.BlueFieldSoftware) string {
				return generateComponentFilePath(butil.ComponentDownloadFilename(bfs, butil.ComponentTypeFwBundle, bfs.Status.DownloadedComponents.PldmFwBundle))
			},
		},
		{
			name: "local spec path is used as-is",
			bfs: &provisioningv1.BlueFieldSoftware{
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: &localPath,
				},
			},
			expected: func(_ *provisioningv1.BlueFieldSoftware) string {
				return localPath
			},
		},
		{
			name: "empty path returns empty",
			bfs:  &provisioningv1.BlueFieldSoftware{},
			expected: func(_ *provisioningv1.BlueFieldSoftware) string {
				return ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &blueFieldSoftwareExtractingState{bfs: tc.bfs}
			assert.Equal(t, tc.expected(tc.bfs), st.resolvePackagePath())
		})
	}
}

func TestCallPldmUnpackService_Success(t *testing.T) {
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/unpack", r.URL.Path)

		var req unpackRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "/tmp/in.fwpkg", req.PackagePath)
		assert.Equal(t, "/tmp/out", req.OutDir)

		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success: true,
			Stdout: `{
				"FirmwareDeviceRecords": [
					{
						"Components": [
							{
								"ComponentVersionString": "BF4-26.01-4",
								"FWImage": "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin"
							}
						]
					}
				]
			}`,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)
	components, err := callPldmUnpackService(context.Background(), "/tmp/in.fwpkg", "/tmp/out")
	require.NoError(t, err)
	require.Len(t, components, 1)
	assert.Equal(t, unpackedComponent{
		ComponentVersionString: "BF4-26.01-4",
		FWImage:                "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin",
	}, components[0])
}

func TestExtractUnpackedComponents(t *testing.T) {
	t.Run("extract component version and fw image", func(t *testing.T) {
		stdout := `{
			"FirmwareDeviceRecords": [
				{
					"Components": [
						{
							"ComponentVersionString": "02.00.0016.0000_n05",
							"FWImage": "/tmp/ERoT_02.00.0016.0000_n05_image.bin"
						},
						{
							"ComponentVersionString": "BF4-26.01-4",
							"FWImage": "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin"
						}
					]
				},
				{
					"Components": [
						{
							"ComponentVersionString": "82.48.0906",
							"FWImage": "/tmp/CX9_MT_0000001775_82.48.0906_image.bin"
						}
					]
				}
			]
		}`

		components, err := extractUnpackedComponents(stdout)
		require.NoError(t, err)
		require.Len(t, components, 3)
		assert.Equal(t, unpackedComponent{
			ComponentVersionString: "02.00.0016.0000_n05",
			FWImage:                "/tmp/ERoT_02.00.0016.0000_n05_image.bin",
		}, components[0])
		assert.Equal(t, unpackedComponent{
			ComponentVersionString: "BF4-26.01-4",
			FWImage:                "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin",
		}, components[1])
		assert.Equal(t, unpackedComponent{
			ComponentVersionString: "82.48.0906",
			FWImage:                "/tmp/CX9_MT_0000001775_82.48.0906_image.bin",
		}, components[2])
	})

	t.Run("deduplicate duplicated components", func(t *testing.T) {
		stdout := `{
			"FirmwareDeviceRecords": [
				{
					"Components": [
						{
							"ComponentVersionString": "02.00.0016.0000_n05",
							"FWImage": "/tmp/ERoT_02.00.0016.0000_n05_image.bin"
						},
						{
							"ComponentVersionString": "BF4-26.01-4",
							"FWImage": "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin"
						}
					]
				},
				{
					"Components": [
						{
							"ComponentVersionString": "02.00.0016.0000_n05",
							"FWImage": "/tmp/ERoT_02.00.0016.0000_n05_image.bin"
						},
						{
							"ComponentVersionString": "82.48.0906",
							"FWImage": "/tmp/CX9_MT_0000001775_82.48.0906_image.bin"
						}
					]
				}
			]
		}`

		components, err := extractUnpackedComponents(stdout)
		require.NoError(t, err)
		require.Len(t, components, 3)
		assert.Equal(t, unpackedComponent{
			ComponentVersionString: "02.00.0016.0000_n05",
			FWImage:                "/tmp/ERoT_02.00.0016.0000_n05_image.bin",
		}, components[0])
		assert.Equal(t, unpackedComponent{
			ComponentVersionString: "BF4-26.01-4",
			FWImage:                "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin",
		}, components[1])
		assert.Equal(t, unpackedComponent{
			ComponentVersionString: "82.48.0906",
			FWImage:                "/tmp/CX9_MT_0000001775_82.48.0906_image.bin",
		}, components[2])
	})

	t.Run("empty stdout", func(t *testing.T) {
		components, err := extractUnpackedComponents("")
		require.NoError(t, err)
		assert.Empty(t, components)
	})

	t.Run("invalid stdout json", func(t *testing.T) {
		_, err := extractUnpackedComponents("not-json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse stdout json")
	})
}

func TestCallPldmUnpackService_ErrorStatus(t *testing.T) {
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)
	_, err := callPldmUnpackService(context.Background(), "/tmp/in.fwpkg", "/tmp/out")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestHandle_ExtractSuccess(t *testing.T) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success: true,
			Stdout: `{
				"FirmwareDeviceRecords": [
					{
						"Components": [
							{
								"ComponentVersionString": "BF4-26.01-4",
								"FWImage": "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin"
							},
							{
								"ComponentVersionString": "82.48.0906",
								"FWImage": "/tmp/CX9_MT_0000001775_82.48.0906_image.bin"
							}
						]
					}
				]
			}`,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1-" + suffix,
			Name:      "bfs1-" + suffix,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
			DownloadedComponents: provisioningv1.DownloadedComponents{
				PldmFwBundle: "https://example.com/fw.fwpkg",
			},
		},
	}
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs)))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	err := st.Handle(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	assert.Equal(t, "/tmp/CX9_MT_0000001775_82.48.0906_image.bin", bfs.Status.DownloadedComponents.AstraNicFw)
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
}

func TestHandle_ExtractFailure(t *testing.T) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success:  false,
			ExitCode: 1,
			Error:    "boom",
			Stderr:   "bad package",
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1-" + suffix,
			Name:      "bfs1-" + suffix,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
			DownloadedComponents: provisioningv1.DownloadedComponents{
				PldmFwBundle: "/tmp/fw.fwpkg",
			},
		},
	}
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs)))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	err := st.Handle(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
}

func startUnixHTTPServer(t *testing.T, handler http.HandlerFunc) (string, func()) {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), "pldm-unpack.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(ln)
	}()

	waitForUnixSocket(t, socketPath)
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return socketPath, shutdown
}

func setEnvForUnpackServer(t *testing.T, socketPath string) {
	t.Helper()
	require.NoError(t, os.Setenv("PLDM_UNPACK_SOCKET_PATH", socketPath))
	require.NoError(t, os.Setenv("PLDM_UNPACK_ENDPOINT", "/v1/unpack"))
	t.Cleanup(func() {
		_ = os.Unsetenv("PLDM_UNPACK_SOCKET_PATH")
		_ = os.Unsetenv("PLDM_UNPACK_ENDPOINT")
	})
}

func waitForUnixSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unix socket %s not ready in time", socketPath)
}

func TestExtractOutputDir(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "bfs",
		},
	}
	st := &blueFieldSoftwareExtractingState{bfs: bfs}
	out := st.extractOutputDir()
	assert.Contains(t, out, fmt.Sprintf("%s-%s-fwbundle-extracted", bfs.Namespace, bfs.Name))
	assert.Equal(t, extractOutputDirForBFS(bfs), out)
	assert.Equal(t, "", extractOutputDirForBFS(nil))
}

func TestIsExtractOutputPresent(t *testing.T) {
	t.Parallel()

	t.Run("missing path", func(t *testing.T) {
		t.Parallel()
		ok, err := isExtractOutputPresent(filepath.Join(t.TempDir(), "nope"))
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("empty directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		ok, err := isExtractOutputPresent(dir)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("directory with file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "x"), []byte("y"), 0o644))
		ok, err := isExtractOutputPresent(dir)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("path is file not directory", func(t *testing.T) {
		t.Parallel()
		f := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
		_, err := isExtractOutputPresent(f)
		require.Error(t, err)
	})
}
