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
	"strings"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
)

// completeDPUUnpackStdout is a PLDM unpack response that yields all four device
// firmware versions checkFirmwareVersions / applyDeviceVersions require.
const completeDPUUnpackStdout = `{
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
				},
				{
					"ComponentVersionString": "02.00.0016.0000_n05",
					"FWImage": "/tmp/ERoT_02.00.0016.0000_n05_image.bin"
				},
				{
					"ComponentVersionString": "1.2.3",
					"FWImage": "/tmp/SBIOS_1.2.3_image.bin"
				}
			]
		}
	]
}`

func completeDeviceVersions() provisioningv1.BluefieldDeviceVersions {
	return provisioningv1.BluefieldDeviceVersions{
		BMCVersion:     "BF4-26.01-4",
		BMCErotVersion: "02.00.0016.0000_n05",
		SBIOSVersion:   "1.2.3",
		BFNicFwVersion: "82.48.0906",
	}
}

func TestResolvePackagePath(t *testing.T) {
	localPath := "/tmp/custom.fwpkg"
	const psid = "MT_0000001665"
	const bundleURL = "https://example.com/fw.fwpkg"
	tests := []struct {
		name     string
		unit     componentInfo
		bfs      *provisioningv1.BlueFieldSoftware
		expected func(bfs *provisioningv1.BlueFieldSoftware) string
	}{
		{
			name: "dpu bundle status url resolves to per-PSID cached path",
			unit: componentInfo{ComponentType: butil.ComponentTypeFwBundle, Key: psid, URL: bundleURL},
			bfs: &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "bfs1",
				},
				Status: provisioningv1.BlueFieldSoftwareStatus{
					DownloadedComponents: provisioningv1.DownloadedComponents{
						PldmFwBundle: map[string]string{psid: bundleURL},
					},
				},
			},
			expected: func(bfs *provisioningv1.BlueFieldSoftware) string {
				fileName := butil.PldmComponentFilename(bfs, psid, bundleURL)
				return componentDestinationPath(butil.ComponentTypeFwBundle, fileName)
			},
		},
		{
			name: "dpu bundle local spec path is used as-is",
			unit: componentInfo{ComponentType: butil.ComponentTypeFwBundle, Key: psid, URL: localPath},
			bfs: &provisioningv1.BlueFieldSoftware{
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: map[string]string{psid: localPath},
				},
			},
			expected: func(_ *provisioningv1.BlueFieldSoftware) string {
				return localPath
			},
		},
		{
			name: "platform bundle status url resolves to cached path",
			unit: componentInfo{ComponentType: butil.ComponentTypePlatformFwBundle, URL: bundleURL},
			bfs: &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "bfs1",
				},
				Status: provisioningv1.BlueFieldSoftwareStatus{
					DownloadedComponents: provisioningv1.DownloadedComponents{
						PlatformPldmFwBundle: bundleURL,
					},
				},
			},
			expected: func(bfs *provisioningv1.BlueFieldSoftware) string {
				fileName := butil.ComponentDownloadFilename(bfs, butil.ComponentTypePlatformFwBundle, bundleURL)
				return componentDestinationPath(butil.ComponentTypePlatformFwBundle, fileName)
			},
		},
		{
			name: "empty path returns empty",
			unit: componentInfo{ComponentType: butil.ComponentTypeFwBundle},
			bfs:  &provisioningv1.BlueFieldSoftware{},
			expected: func(_ *provisioningv1.BlueFieldSoftware) string {
				return ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &blueFieldSoftwareExtractingState{bfs: tc.bfs}
			assert.Equal(t, tc.expected(tc.bfs), st.resolvePackagePath(tc.unit))
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
			Stdout:  completeDPUUnpackStdout,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	const psid = "MT_0000001775"
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1-" + suffix,
			Name:      "bfs1-" + suffix,
		},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{
				psid: "https://example.com/fw.fwpkg",
			},
			PlatformPldmFwBundle: ptr.To("https://example.com/platform.fwpkg"),
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
		},
	}
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle, psid)))
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypePlatformFwBundle, "")))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	err := st.Handle(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "the DPU bundle and the platform bundle are unpacked separately")
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	// The platform bundle supplies the E/W NIC firmware image the DPU agent flashes.
	assert.Equal(t, "/tmp/CX9_MT_0000001775_82.48.0906_image.bin", bfs.Status.DownloadedComponents.NicFw)
	assert.Equal(t, "82.48.0906", bfs.Status.Versions.EWNicFwVersion)
	// The DPU bundle supplies the per-PSID versions checked before a firmware update.
	assert.Equal(t, completeDeviceVersions(), bfs.Status.Versions.BluefieldSoftwareVersions[psid])
	cond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
}

// newLeftoverExtractBFS builds a BlueFieldSoftware whose extract output directory
// already exists on disk, holding a read-only file like a real unpacked image.
func newLeftoverExtractBFS(t *testing.T, nameSuffix string) (*provisioningv1.BlueFieldSoftware, string, string) {
	t.Helper()
	const psid = "MT_0000001775"
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns-" + nameSuffix,
			Name:      "bfs-" + nameSuffix,
		},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{psid: "https://example.com/fw.fwpkg"},
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
		},
	}
	outDir := extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle, psid)
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	leftover := filepath.Join(outDir, "BMC_BF4-BMC_BF4-26.01-1_image.bin")
	require.NoError(t, os.WriteFile(leftover, []byte("leftover"), 0o444))
	return bfs, leftover, psid
}

func TestHandle_ReExtractsWhenVersionsMissing(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success: true,
			Stdout:  completeDPUUnpackStdout,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	bfs, leftover, psid := newLeftoverExtractBFS(t, fmt.Sprintf("reextract-%d", time.Now().UnixNano()))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 1, callCount, "unpack must run again when status carries no versions")
	require.NotNil(t, bfs.Status.Versions, "Ready must not be reached without versions")
	assert.Equal(t, completeDeviceVersions(), bfs.Status.Versions.BluefieldSoftwareVersions[psid])
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

	bfs, leftover, psid := newLeftoverExtractBFS(t, fmt.Sprintf("skip-%d", time.Now().UnixNano()))
	recorded := completeDeviceVersions()
	recorded.BMCVersion = "BF4-26.01-1"
	bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{
		BluefieldSoftwareVersions: map[string]provisioningv1.BluefieldDeviceVersions{
			psid: recorded,
		},
	}

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 0, callCount, "unpack must be skipped when output and versions are both present")
	assert.Equal(t, "BF4-26.01-1", bfs.Status.Versions.BluefieldSoftwareVersions[psid].BMCVersion)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)

	_, err := os.Stat(leftover)
	assert.NoError(t, err, "existing output must be left alone on the skip path")
}

// TestHandle_IncompleteBundleErrors pins that a DPU bundle missing a required
// component type fails extract instead of reaching Ready with a partial record
// that would permanently skip re-unpack while checkFirmwareVersions keeps failing.
func TestHandle_IncompleteBundleErrors(t *testing.T) {
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

	bfs, _, psid := newLeftoverExtractBFS(t, fmt.Sprintf("unmatched-%d", time.Now().UnixNano()))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	err := st.Handle(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required component versions")
	assert.Equal(t, 1, callCount)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	assert.Empty(t, bfs.Status.Versions.BluefieldSoftwareVersions[psid])
}

func TestHandle_PartialVersionsReExtract(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success: true,
			Stdout:  completeDPUUnpackStdout,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	bfs, leftover, psid := newLeftoverExtractBFS(t, fmt.Sprintf("partial-%d", time.Now().UnixNano()))
	// A prior extract that only recorded BMC must not count as done.
	bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{
		BluefieldSoftwareVersions: map[string]provisioningv1.BluefieldDeviceVersions{
			psid: {BMCVersion: "BF4-26.01-1"},
		},
	}

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 1, callCount, "partial versions must force a re-unpack")
	assert.Equal(t, completeDeviceVersions(), bfs.Status.Versions.BluefieldSoftwareVersions[psid])
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	_, err := os.Stat(leftover)
	assert.True(t, os.IsNotExist(err), "stale output must be cleared before re-unpacking")
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

	const psid = "MT_0000001775"
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1-" + suffix,
			Name:      "bfs1-" + suffix,
		},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{
				psid: "/tmp/fw.fwpkg",
			},
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
		},
	}
	require.NoError(t, os.RemoveAll(extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle, psid)))

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
	const psid = "MT_0000001665"
	st := &blueFieldSoftwareExtractingState{bfs: bfs}
	dpuOut := st.extractOutputDir(butil.ComponentTypeFwBundle, psid)
	assert.Contains(t, dpuOut, fmt.Sprintf("%s-%s-%s-%s-extracted", bfs.Namespace, bfs.Name, butil.ComponentTypeFwBundle, psid))
	assert.Equal(t, extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle, psid), dpuOut)
	assert.Equal(t, "", extractOutputDirForBFS(nil, butil.ComponentTypeFwBundle, psid))

	// The platform bundle is single-valued, so its directory carries no key suffix.
	platformOut := st.extractOutputDir(butil.ComponentTypePlatformFwBundle, "")
	assert.Contains(t, platformOut, fmt.Sprintf("%s-%s-%s-extracted", bfs.Namespace, bfs.Name, butil.ComponentTypePlatformFwBundle))
	assert.NotEqual(t, dpuOut, platformOut)
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

	const psid = "MT_0000001775"
	// A PSID's DPU bundle carries that device's firmware versions and nothing the DPU
	// agent flashes, so the NIC firmware image path stays untouched here.
	require.NoError(t, applyUnpackedComponentsToDownloaded(bfs, butil.ComponentTypeFwBundle, psid, components))
	assert.Equal(t, "82.48.0906", bfs.Status.Versions.BluefieldSoftwareVersions[psid].BFNicFwVersion)
	assert.Equal(t, "BF4-26.01-4", bfs.Status.Versions.BluefieldSoftwareVersions[psid].BMCVersion)
	assert.Equal(t, "02.00.0016.0000_n05", bfs.Status.Versions.BluefieldSoftwareVersions[psid].BMCErotVersion)
	assert.Equal(t, "1.2.3", bfs.Status.Versions.BluefieldSoftwareVersions[psid].SBIOSVersion)
	assert.Empty(t, bfs.Status.DownloadedComponents.NicFw)
	assert.Empty(t, bfs.Status.Versions.EWNicFwVersion)

	err := applyUnpackedComponentsToDownloaded(bfs, butil.ComponentTypeFwBundle, "MT_0000001774", components)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `CX9 image PSID "MT_0000001775" does not match expected PSID "MT_0000001774"`)

	// The platform bundle yields the E/W NIC firmware image and its version, and does not
	// touch the per-PSID device versions: it is not tied to any single DPU model.
	platformBFS := &provisioningv1.BlueFieldSoftware{}
	require.NoError(t, applyUnpackedComponentsToDownloaded(platformBFS, butil.ComponentTypePlatformFwBundle, "", components))
	assert.Equal(t, "/tmp/CX9_MT_0000001775_82.48.0906_image.bin", platformBFS.Status.DownloadedComponents.NicFw)
	assert.Equal(t, "82.48.0906", platformBFS.Status.Versions.EWNicFwVersion)
	assert.Empty(t, platformBFS.Status.Versions.BluefieldSoftwareVersions)
}

func TestPsidFromCX9ImageName(t *testing.T) {
	t.Parallel()
	psid, err := psidFromCX9ImageName(strings.ToUpper("CX9_MT_0000001774_82.48.1680_4fdd89de_image.bin"))
	require.NoError(t, err)
	assert.Equal(t, "MT_0000001774", psid)

	_, err = psidFromCX9ImageName("BMC_BF4-BMC_BF4-26.01-4_IMAGE.BIN")
	require.Error(t, err)
}

func TestExtractedVersionsRecorded(t *testing.T) {
	t.Parallel()

	const psid = "MT_0000001775"
	bfs := &provisioningv1.BlueFieldSoftware{}
	assert.False(t, extractedVersionsRecorded(bfs, butil.ComponentTypeFwBundle, psid))

	bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{
		BluefieldSoftwareVersions: map[string]provisioningv1.BluefieldDeviceVersions{},
	}
	assert.False(t, extractedVersionsRecorded(bfs, butil.ComponentTypeFwBundle, psid))

	// A partial record is not done: checkFirmwareVersions needs all four fields.
	bfs.Status.Versions.BluefieldSoftwareVersions[psid] = provisioningv1.BluefieldDeviceVersions{BMCVersion: "BF4-26.01-4"}
	assert.False(t, extractedVersionsRecorded(bfs, butil.ComponentTypeFwBundle, psid))

	bfs.Status.Versions.BluefieldSoftwareVersions[psid] = completeDeviceVersions()
	assert.True(t, extractedVersionsRecorded(bfs, butil.ComponentTypeFwBundle, psid))
	assert.False(t, extractedVersionsRecorded(bfs, butil.ComponentTypeFwBundle, "MT_0000001774"))

	// The platform bundle records no PSID, so its own version field is what marks it done.
	assert.False(t, extractedVersionsRecorded(bfs, butil.ComponentTypePlatformFwBundle, ""))
	bfs.Status.Versions.EWNicFwVersion = "82.48.0906"
	assert.True(t, extractedVersionsRecorded(bfs, butil.ComponentTypePlatformFwBundle, ""))
}

// A BlueFieldSoftware recreated with a previously used name finds the extract output
// directory of the earlier object still on the /bfb hostPath. It must unpack anyway,
// because status.Versions is the only thing the firmware update flow can read.
func TestHandle_ExtractWithStaleOutputDirectory(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	callCount := 0
	socketPath, shutdown := startUnixHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(unpackResponse{
			Success: true,
			Stdout:  completeDPUUnpackStdout,
		})
	})
	defer shutdown()

	setEnvForUnpackServer(t, socketPath)

	const psid = "MT_0000001775"
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "bfs1"},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{psid: "https://example.com/fw.fwpkg"},
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareExtracting,
		},
	}

	outDir := extractOutputDirForBFS(bfs, butil.ComponentTypeFwBundle, psid)
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	staleFile := filepath.Join(outDir, "BMC_BF4-BMC_BF4-25.10-1_image.bin")
	require.NoError(t, os.WriteFile(staleFile, []byte("stale"), 0o644))

	st := &blueFieldSoftwareExtractingState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, 1, callCount)
	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	assert.Equal(t, completeDeviceVersions(), bfs.Status.Versions.BluefieldSoftwareVersions[psid])
	_, err := os.Stat(staleFile)
	assert.True(t, os.IsNotExist(err), "stale extract output should be cleared before unpack")

	// Versions already recorded: a re-reconcile must not unpack again.
	require.NoError(t, st.Handle(context.Background(), nil))
	assert.Equal(t, 1, callCount)
}
