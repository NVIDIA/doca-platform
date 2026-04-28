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

package nicprovisioning

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	clientpkg "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/client"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNICProvisioning_ShouldSkip(t *testing.T) {
	op := &NICProvisioning{}

	baseCtx := func() *operations.Context {
		return &operations.Context{
			Options: opts.Options{
				SkipAstra:      false,
				AstraEnabled:   true,
				BFBRegistryURL: "https://registry.example.com",
			},
			LatestDPU: &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUType: provisioningv1.DPUTypeBlueField4,
				},
			},
		}
	}
	t.Run("skip when astra is disabled", func(t *testing.T) {
		ctx := baseCtx()
		ctx.Options.AstraEnabled = false
		assert.True(t, op.ShouldSkip(ctx))
	})

	t.Run("skip when SkipAstra is true", func(t *testing.T) {
		ctx := baseCtx()
		ctx.Options.SkipAstra = true
		assert.True(t, op.ShouldSkip(ctx))
	})

	t.Run("do not skip when latest dpu is unavailable", func(t *testing.T) {
		ctx := baseCtx()
		ctx.LatestDPU = nil
		assert.False(t, op.ShouldSkip(ctx))
	})

	t.Run("run when astra enabled and bluefield4 with bfb registry url", func(t *testing.T) {
		assert.False(t, op.ShouldSkip(baseCtx()))
	})
}

type fakeClient struct {
	getObjectFunc func(ctx context.Context, namespace string, name string, obj client.Object) error
}

func (f *fakeClient) HealthCheck() error { return nil }

func (f *fakeClient) UpdateStatus(context.Context, provisioningv1.AgentStatus) error { return nil }

func (f *fakeClient) GetObject(ctx context.Context, namespace string, name string, obj client.Object) error {
	if f.getObjectFunc == nil {
		return fmt.Errorf("unexpected GetObject call")
	}
	return f.getObjectFunc(ctx, namespace, name, obj)
}

var _ clientpkg.Client = &fakeClient{}

func TestNICProvisioning_Execute(t *testing.T) {
	op := &NICProvisioning{}
	originalDir := nicFirmwareDir
	tempDir := t.TempDir()
	nicFirmwareDir = tempDir
	t.Cleanup(func() { nicFirmwareDir = originalDir })

	newOptCtx := func(client clientpkg.Client, bfbRegistryURL string) *operations.Context {
		return &operations.Context{
			Options: opts.Options{
				DPUName:        "dpu-1",
				DPUNamespace:   "default",
				BFBRegistryURL: bfbRegistryURL,
			},
			Client: client,
			LatestDPU: &provisioningv1.DPU{
				Spec: provisioningv1.DPUSpec{
					BlueFieldSoftware: "bfs-1",
				},
				Status: provisioningv1.DPUStatus{
					DPUType: provisioningv1.DPUTypeBlueField4,
				},
			},
		}
	}

	t.Run("skip download when local firmware already exists", func(t *testing.T) {
		existingFile := filepath.Join(tempDir, "astra-nic-fw.fwpkg")
		require.NoError(t, os.WriteFile(existingFile, []byte("already here"), 0600))

		ctx := newOptCtx(&fakeClient{
			getObjectFunc: func(_ context.Context, _ string, _ string, obj client.Object) error {
				bfs := obj.(*provisioningv1.BlueFieldSoftware)
				bfs.Status.DownloadedComponents.AstraNicFw = "downloads/astra-nic-fw.fwpkg"
				return nil
			},
		}, "https://registry.example.com")

		require.NoError(t, op.Execute(context.Background(), ctx))
		content, err := os.ReadFile(existingFile)
		require.NoError(t, err)
		assert.Equal(t, "already here", string(content))
	})

	t.Run("download firmware to local nic-firmware directory", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("firmware-bytes"))
		}))
		defer server.Close()

		localFile := filepath.Join(tempDir, "astra-nic-fw-new.fwpkg")
		require.NoError(t, os.RemoveAll(localFile))

		ctx := newOptCtx(&fakeClient{
			getObjectFunc: func(_ context.Context, _ string, _ string, obj client.Object) error {
				bfs := obj.(*provisioningv1.BlueFieldSoftware)
				bfs.Status.DownloadedComponents.AstraNicFw = "download/astra-nic-fw-new.fwpkg"
				return nil
			},
		}, server.URL)

		require.NoError(t, op.Execute(context.Background(), ctx))
		content, err := os.ReadFile(localFile)
		require.NoError(t, err)
		assert.Equal(t, "firmware-bytes", string(content))
	})
}

func TestResolveNICFirmwareDownloadURL(t *testing.T) {
	t.Run("defaults registry URL scheme to HTTP", func(t *testing.T) {
		got, err := resolveNICFirmwareDownloadURL("10.233.14.188:8082", "/bfb/components/fw.bin")
		require.NoError(t, err)
		assert.Equal(t, "http://10.233.14.188:8082/bfb/components/fw.bin", got)
	})

	t.Run("keeps absolute firmware URL", func(t *testing.T) {
		got, err := resolveNICFirmwareDownloadURL("10.233.14.188:8082", "https://registry.example.com/fw.bin")
		require.NoError(t, err)
		assert.Equal(t, "https://registry.example.com/fw.bin", got)
	})
}
