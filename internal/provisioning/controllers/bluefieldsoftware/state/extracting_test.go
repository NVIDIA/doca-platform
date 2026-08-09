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
		name          string
		componentType butil.ComponentType
		bfs           *provisioningv1.BlueFieldSoftware
		expected      func(bfs *provisioningv1.BlueFieldSoftware) string
	}{
		{
			name:          "platform status url resolves to cached path",
			componentType: butil.ComponentTypePlatformFwBundle,
			bfs: &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "bfs1",
				},
				Status: provisioningv1.BlueFieldSoftwareStatus{
					DownloadedComponents: provisioningv1.DownloadedComponents{
						PlatformPldmFwBundle: "https://example.com/fw.fwpkg",
					},
				},
			},
			expected: func(bfs *provisioningv1.BlueFieldSoftware) string {
				fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypePlatformFwBundle, bfs.Status.DownloadedComponents.PlatformPldmFwBundle)
				return componentDestinationPath(butil.ComponentTypePlatformFwBundle, fileName)
			},
		},
		{
			name:          "pldm local spec path is used as-is",
			componentType: butil.ComponentTypeFwBundle,
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
			name:          "empty path returns empty",
			componentType: butil.ComponentTypePlatformFwBundle,
			bfs:           &provisioningv1.BlueFieldSoftware{},
			expected: func(_ *provisioningv1.BlueFieldSoftware) string {
				return ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &blueFieldSoftwareExtractingState{bfs: tc.bfs}
			assert.Equal(t, tc.expected(tc.bfs), st.resolvePackagePath(tc.componentType))
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
	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
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
				PldmFwBundle:         "https://example.com/fw-base.fwpkg",
				PlatformPldmFwBundle: "https://example.com/fw.fwpkg",
			},
		},
	}
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle)))
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypePlatformFwBundle)))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	err := st.Handle(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	assert.Equal(t, "/tmp/CX9_MT_0000001775_82.48.0906_image.bin", bfs.Status.DownloadedComponents.NicFw)
	assert.Equal(t, "BF4-26.01-4", bfs.Status.Versions.BMCVersion)
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
}

// newLeftoverExtractBFS builds a BlueFieldSoftware whose extract output directory
// already exists on disk, holding a read-only file like a real unpacked image.
func newLeftoverExtractBFS(t *testing.T, nameSuffix string) (*provisioningv1.BlueFieldSoftware, string) {
	t.Helper()
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns-" + nameSuffix,
			Name:      "bfs-" + nameSuffix,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
			DownloadedComponents: provisioningv1.DownloadedComponents{
				PldmFwBundle: "https://example.com/fw-base.fwpkg",
			},
		},
	}
	outDir := extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle)
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	leftover := filepath.Join(outDir, "BMC_BF4-BMC_BF4-26.01-1_image.bin")
	require.NoError(t, os.WriteFile(leftover, []byte("leftover"), 0o444))
	return bfs, leftover
}

func TestHandle_ReExtractsWhenVersionsMissing(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
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

	bfs, leftover := newLeftoverExtractBFS(t, fmt.Sprintf("reextract-%d", time.Now().UnixNano()))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 1, callCount, "unpack must run again when status carries no versions")
	require.NotNil(t, bfs.Status.Versions, "Ready must not be reached without versions")
	assert.Equal(t, "BF4-26.01-4", bfs.Status.Versions.BMCVersion)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)

	_, err := os.Stat(leftover)
	assert.True(t, os.IsNotExist(err), "stale output must be cleared before re-unpacking")
}

func TestHandle_SkipsExtractWhenVersionsRecorded(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(unpackResponse{Success: true})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	bfs, leftover := newLeftoverExtractBFS(t, fmt.Sprintf("skip-%d", time.Now().UnixNano()))
	bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{BMCVersion: "BF4-26.01-1"}

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 0, callCount, "unpack must be skipped when output and versions are both present")
	assert.Equal(t, "BF4-26.01-1", bfs.Status.Versions.BMCVersion)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)

	_, err := os.Stat(leftover)
	assert.NoError(t, err, "existing output must be left alone on the skip path")
}

func TestStatusHasVersionsForComponent(t *testing.T) {
	t.Parallel()

	assert.False(t, statusHasVersionsForComponent(nil, butil.ComponentTypeFwBundle))

	bfs := &provisioningv1.BlueFieldSoftware{}
	assert.False(t, statusHasVersionsForComponent(bfs, butil.ComponentTypeFwBundle))

	bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{}
	assert.False(t, statusHasVersionsForComponent(bfs, butil.ComponentTypeFwBundle))

	// The platform bundle only ever populates EWNicFwVersion, so it must not be
	// read as evidence that the base bundle was extracted.
	bfs.Status.Versions.EWNicFwVersion = "82.48.0906"
	assert.True(t, statusHasVersionsForComponent(bfs, butil.ComponentTypePlatformFwBundle))
	assert.False(t, statusHasVersionsForComponent(bfs, butil.ComponentTypeFwBundle))

	bfs.Status.Versions.SBIOSVersion = "1.2.3"
	assert.True(t, statusHasVersionsForComponent(bfs, butil.ComponentTypeFwBundle))

	assert.False(t, statusHasVersionsForComponent(bfs, butil.ComponentTypeOSISO))
}

// TestStatusHasVersionsForComponent_CoversExtractableTypes fails if a component type
// is added to extractableComponentTypes without a case in
// statusHasVersionsForComponent, which would make its bundle unpack again on every
// entry to the Extracting state.
func TestStatusHasVersionsForComponent_CoversExtractableTypes(t *testing.T) {
	t.Parallel()

	bfs := &provisioningv1.BlueFieldSoftware{
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Versions: &provisioningv1.BluefieldSoftwareVersions{
				FwBundleVersion: "set",
				OSISOVersion:    "set",
				EWNicFwVersion:  "set",
				BMCVersion:      "set",
				BMCErotVersion:  "set",
				SBIOSVersion:    "set",
				BFNicFwVersion:  "set",
			},
		},
	}

	require.NotEmpty(t, extractableComponentTypes)
	for _, componentType := range extractableComponentTypes {
		assert.True(t, statusHasVersionsForComponent(bfs, componentType),
			"component type %q has no case in statusHasVersionsForComponent", componentType)
	}
}

// TestHandle_UnmatchedComponentNamesStillReachReady pins that a bundle whose image
// names match none of the known patterns settles in Ready rather than re-unpacking:
// state is dispatched on Status.Phase, so leaving Extracting is what bounds the work.
func TestHandle_UnmatchedComponentNamesStillReachReady(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success: true,
			Stdout: `{
				"FirmwareDeviceRecords": [
					{
						"Components": [
							{
								"ComponentVersionString": "1.2.3",
								"FWImage": "/tmp/UNKNOWN_COMPONENT_image.bin"
							}
						]
					}
				]
			}`,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	bfs, _ := newLeftoverExtractBFS(t, fmt.Sprintf("unmatched-%d", time.Now().UnixNano()))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 1, callCount)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	assert.Empty(t, bfs.Status.Versions.BMCVersion)
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
				PldmFwBundle:         "/tmp/fw.fwpkg",
				PlatformPldmFwBundle: "/tmp/fw.fwpkg",
			},
		},
	}
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle)))
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypePlatformFwBundle)))

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
	pldmOut := st.extractOutputDir(butil.ComponentTypeFwBundle)
	platformOut := st.extractOutputDir(butil.ComponentTypePlatformFwBundle)
	assert.Contains(t, pldmOut, fmt.Sprintf("%s-%s-%s-extracted", bfs.Namespace, bfs.Name, butil.ComponentTypeFwBundle))
	assert.Contains(t, platformOut, fmt.Sprintf("%s-%s-%s-extracted", bfs.Namespace, bfs.Name, butil.ComponentTypePlatformFwBundle))
	assert.Equal(t, extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle), pldmOut)
	assert.Equal(t, extractOutputDirForBFS(bfs, butil.ComponentTypePlatformFwBundle), platformOut)
	assert.Equal(t, "", extractOutputDirForBFS(nil, butil.ComponentTypeFwBundle))
}

func TestApplyUnpackedComponentsToDownloaded_BySourceBundle(t *testing.T) {
	bfs := &provisioningv1.BlueFieldSoftware{
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Versions: &provisioningv1.BluefieldSoftwareVersions{},
		},
	}
	components := []unpackedComponent{
		{
			ComponentVersionString: "82.48.0906",
			FWImage:                "/tmp/CX9_MT_0000001775_82.48.0906_image.bin",
		},
		{
			ComponentVersionString: "BF4-26.01-4",
			FWImage:                "/tmp/BMC_BF4-BMC_BF4-26.01-4_image.bin",
		},
		{
			ComponentVersionString: "02.00.0016.0000_n05",
			FWImage:                "/tmp/ERoT_02.00.0016.0000_n05_image.bin",
		},
		{
			ComponentVersionString: "1.2.3",
			FWImage:                "/tmp/SBIOS_1.2.3_image.bin",
		},
	}

	applyUnpackedComponentsToDownloaded(bfs, butil.ComponentTypePlatformFwBundle, components)
	assert.Equal(t, "/tmp/CX9_MT_0000001775_82.48.0906_image.bin", bfs.Status.DownloadedComponents.NicFw)
	assert.Equal(t, "82.48.0906", bfs.Status.Versions.EWNicFwVersion)
	assert.Empty(t, bfs.Status.Versions.BMCVersion)
	assert.Empty(t, bfs.Status.Versions.BMCErotVersion)
	assert.Empty(t, bfs.Status.Versions.SBIOSVersion)

	applyUnpackedComponentsToDownloaded(bfs, butil.ComponentTypeFwBundle, components)
	assert.Equal(t, "BF4-26.01-4", bfs.Status.Versions.BMCVersion)
	assert.Equal(t, "02.00.0016.0000_n05", bfs.Status.Versions.BMCErotVersion)
	assert.Equal(t, "1.2.3", bfs.Status.Versions.SBIOSVersion)
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
