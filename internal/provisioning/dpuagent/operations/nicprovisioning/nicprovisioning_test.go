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
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	nicconfigurationv1alpha1 "github.com/Mellanox/nic-configuration-operator/api/v1alpha1"
	nicdms "github.com/Mellanox/nic-configuration-operator/pkg/dms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNICProvisioning_ShouldSkip(t *testing.T) {
	op := &NICProvisioning{}

	baseCtx := func() *operations.Context {
		return &operations.Context{
			Options: opts.Options{
				SkipAstra:      false,
				AstraEnabled:   true,
				BFBRegistryURL: "https://registry.example.com",
				NICDeviceCount: opts.DefaultNICDeviceCount,
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

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(provisioningv1.AddToScheme(s))
	return s
}

type fakeDMSServer struct {
	running    bool
	stopCalled bool
	stopErr    error
}

func (f *fakeDMSServer) StartDMSServer(_ []nicconfigurationv1alpha1.NicDevice) error {
	f.running = true
	return nil
}

func (f *fakeDMSServer) StopDMSServer() error {
	f.stopCalled = true
	if f.stopErr != nil {
		return f.stopErr
	}
	f.running = false
	return nil
}

func (f *fakeDMSServer) IsRunning() bool {
	return f.running
}

func (f *fakeDMSServer) GetDMSClientByPCIAddress(_ string) (nicdms.DMSClient, error) {
	return nil, nil
}

func TestNICProvisioning_Execute(t *testing.T) {
	op := &NICProvisioning{
		prepareLocalDMSServerFn:   func(_ *operations.Context) error { return nil },
		installNICFirmwareFn:      func(_ context.Context, _ *operations.Context, _ string) error { return nil },
		applyNVConfigFn:           func(_ context.Context, _ *operations.Context) error { return nil },
		configureRestrictedModeFn: func(_ context.Context, _ *operations.Context) error { return nil },
	}
	originalDir := nicFirmwareDir
	tempDir := t.TempDir()
	nicFirmwareDir = tempDir
	t.Cleanup(func() { nicFirmwareDir = originalDir })

	newBFS := func(nicFWLocation string) *provisioningv1.BlueFieldSoftware {
		return &provisioningv1.BlueFieldSoftware{
			ObjectMeta: metav1.ObjectMeta{Name: "bfs-1", Namespace: "default"},
			Spec: provisioningv1.BlueFieldSpec{
				PlatformPldmFwBundle: ptr.To("https://example.com/pldm.fwpkg"),
			},
			Status: provisioningv1.BlueFieldSoftwareStatus{
				DownloadedComponents: provisioningv1.DownloadedComponents{
					NicFw: nicFWLocation,
				},
			},
		}
	}

	newOptCtx := func(client crclient.Client, bfbRegistryURL string) *operations.Context {
		return &operations.Context{
			Options: opts.Options{
				DPUName:        "dpu-1",
				DPUNamespace:   "default",
				BFBRegistryURL: bfbRegistryURL,
				NICDeviceCount: opts.DefaultNICDeviceCount,
			},
			Client: client,
			LatestDPU: &provisioningv1.DPU{
				Spec: provisioningv1.DPUSpec{
					BlueFieldSoftware: ptr.To("bfs-1"),
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

		bfs := newBFS("downloads/astra-nic-fw.fwpkg")
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(bfs).Build()
		ctx := newOptCtx(fakeClient, "https://registry.example.com")
		runner := &fakeBashRunner{}
		op.runBash = runner.run

		require.NoError(t, op.Execute(context.Background(), ctx))
		content, err := os.ReadFile(existingFile)
		require.NoError(t, err)
		assert.Equal(t, "already here", string(content))
		assert.Equal(t, []string{"flint -i '" + existingFile + "' q"}, runner.commands)
	})

	t.Run("download firmware to local nic-firmware directory", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("firmware-bytes"))
		}))
		defer server.Close()

		localFile := filepath.Join(tempDir, "astra-nic-fw-new.fwpkg")
		require.NoError(t, os.RemoveAll(localFile))

		bfs := newBFS("download/astra-nic-fw-new.fwpkg")
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(bfs).Build()
		ctx := newOptCtx(fakeClient, server.URL)
		runner := &fakeBashRunner{}
		op.runBash = runner.run

		require.NoError(t, op.Execute(context.Background(), ctx))
		content, err := os.ReadFile(localFile)
		require.NoError(t, err)
		assert.Equal(t, "firmware-bytes", string(content))
		assert.Equal(t, []string{"flint -i '" + localFile + "' q"}, runner.commands)
	})

	t.Run("keeps local dms server running when execute returns", func(t *testing.T) {
		existingFile := filepath.Join(tempDir, "astra-nic-fw-stop.fwpkg")
		require.NoError(t, os.WriteFile(existingFile, []byte("already here"), 0600))
		dmsServer := &fakeDMSServer{running: true}
		opWithRunningDMS := &NICProvisioning{
			runBash:                   (&fakeBashRunner{}).run,
			dmsServer:                 dmsServer,
			prepareLocalDMSServerFn:   func(_ *operations.Context) error { return nil },
			installNICFirmwareFn:      func(_ context.Context, _ *operations.Context, _ string) error { return nil },
			applyNVConfigFn:           func(_ context.Context, _ *operations.Context) error { return nil },
			configureRestrictedModeFn: func(_ context.Context, _ *operations.Context) error { return nil },
		}

		bfs := newBFS("downloads/astra-nic-fw-stop.fwpkg")
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(bfs).Build()
		ctx := newOptCtx(fakeClient, "https://registry.example.com")

		require.NoError(t, opWithRunningDMS.Execute(context.Background(), ctx))
		assert.False(t, dmsServer.stopCalled)
		assert.True(t, dmsServer.running)

		require.NoError(t, opWithRunningDMS.Shutdown())
		assert.True(t, dmsServer.stopCalled)
		assert.False(t, dmsServer.running)
	})

	t.Run("skip firmware download and install when PlatformPldmFwBundle is empty", func(t *testing.T) {
		installCalled := false
		opSkipFirmware := &NICProvisioning{
			prepareLocalDMSServerFn: func(_ *operations.Context) error { return nil },
			installNICFirmwareFn: func(_ context.Context, _ *operations.Context, _ string) error {
				installCalled = true
				return nil
			},
			applyNVConfigFn:           func(_ context.Context, _ *operations.Context) error { return nil },
			configureRestrictedModeFn: func(_ context.Context, _ *operations.Context) error { return nil },
		}

		// No PlatformPldmFwBundle and no AstraNicFw: download would fail if not skipped.
		bfs := &provisioningv1.BlueFieldSoftware{
			ObjectMeta: metav1.ObjectMeta{Name: "bfs-1", Namespace: "default"},
			Spec:       provisioningv1.BlueFieldSpec{},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(bfs).Build()
		ctx := newOptCtx(fakeClient, "https://registry.example.com")

		require.NoError(t, opSkipFirmware.Execute(context.Background(), ctx))
		assert.False(t, installCalled)
	})

	t.Run("return error when shutdown cannot stop local dms server", func(t *testing.T) {
		existingFile := filepath.Join(tempDir, "astra-nic-fw-stop-error.fwpkg")
		require.NoError(t, os.WriteFile(existingFile, []byte("already here"), 0600))
		dmsServer := &fakeDMSServer{
			running: true,
			stopErr: errors.New("stop failed"),
		}
		opWithRunningDMS := &NICProvisioning{
			runBash:                   (&fakeBashRunner{}).run,
			dmsServer:                 dmsServer,
			prepareLocalDMSServerFn:   func(_ *operations.Context) error { return nil },
			installNICFirmwareFn:      func(_ context.Context, _ *operations.Context, _ string) error { return nil },
			applyNVConfigFn:           func(_ context.Context, _ *operations.Context) error { return nil },
			configureRestrictedModeFn: func(_ context.Context, _ *operations.Context) error { return nil },
		}

		bfs := newBFS("downloads/astra-nic-fw-stop-error.fwpkg")
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(bfs).Build()
		ctx := newOptCtx(fakeClient, "https://registry.example.com")

		require.NoError(t, opWithRunningDMS.Execute(context.Background(), ctx))

		err := opWithRunningDMS.Shutdown()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to stop local DMS server")
		assert.True(t, dmsServer.stopCalled)
	})
}

func TestNICProvisioning_applyRuntimeConfigAndUpdateStatus(t *testing.T) {
	t.Run("sets EWNICConfigured true on success", func(t *testing.T) {
		op := &NICProvisioning{
			applyRuntimeConfigFn: func(_ context.Context, _ *operations.Context) error { return nil },
		}
		ctx := &operations.Context{
			Status: provisioningv1.AgentStatus{Conditions: []metav1.Condition{}},
		}

		require.NoError(t, op.applyRuntimeConfigAndUpdateStatus(context.Background(), ctx))
		require.Len(t, ctx.Status.Conditions, 1)
		assert.Equal(t, cutil.AgentCondEWNICConfigured, ctx.Status.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionTrue, ctx.Status.Conditions[0].Status)
		assert.Equal(t, "RuntimeConfigApplied", ctx.Status.Conditions[0].Reason)
	})

	t.Run("sets EWNICConfigured false on failure", func(t *testing.T) {
		op := &NICProvisioning{
			applyRuntimeConfigFn: func(_ context.Context, _ *operations.Context) error { return errors.New("runtime apply failed") },
		}
		ctx := &operations.Context{
			Status: provisioningv1.AgentStatus{Conditions: []metav1.Condition{}},
		}

		err := op.applyRuntimeConfigAndUpdateStatus(context.Background(), ctx)
		require.Error(t, err)
		require.Len(t, ctx.Status.Conditions, 1)
		assert.Equal(t, cutil.AgentCondEWNICConfigured, ctx.Status.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionFalse, ctx.Status.Conditions[0].Status)
		assert.Equal(t, "RuntimeConfigApplyFailed", ctx.Status.Conditions[0].Reason)
	})
}

func TestNICProvisioning_StartRuntimeConfigLoop(t *testing.T) {
	t.Run("no-op when dms session was never started", func(t *testing.T) {
		op := &NICProvisioning{}
		op.StartRuntimeConfigLoop(context.Background(), &operations.Context{})
		// Shutdown must not block when the loop was never started.
		require.NoError(t, op.Shutdown())
	})

	t.Run("applies runtime config then stops on context cancel", func(t *testing.T) {
		originalInterval := RuntimeConfigInterval
		originalRetry := RuntimeConfigRetryInterval
		RuntimeConfigInterval = time.Hour
		RuntimeConfigRetryInterval = 10 * time.Millisecond
		t.Cleanup(func() {
			RuntimeConfigInterval = originalInterval
			RuntimeConfigRetryInterval = originalRetry
		})

		var calls atomic.Int32
		op := &NICProvisioning{
			dmsServer: &fakeDMSServer{running: true},
			applyRuntimeConfigFn: func(_ context.Context, _ *operations.Context) error {
				calls.Add(1)
				return nil
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		optCtx := &operations.Context{
			Status: provisioningv1.AgentStatus{Conditions: []metav1.Condition{}},
		}

		op.StartRuntimeConfigLoop(ctx, optCtx)
		require.Eventually(t, func() bool { return calls.Load() >= 1 }, time.Second, 10*time.Millisecond)
		cancel()
		require.NoError(t, op.Shutdown())
	})

	t.Run("coalesces bursty CC terminations into one reapply", func(t *testing.T) {
		originalInterval := RuntimeConfigInterval
		originalRetry := RuntimeConfigRetryInterval
		originalCoalesce := CCTerminationCoalesceWindow
		RuntimeConfigInterval = time.Hour
		RuntimeConfigRetryInterval = 10 * time.Millisecond
		CCTerminationCoalesceWindow = 50 * time.Millisecond
		t.Cleanup(func() {
			RuntimeConfigInterval = originalInterval
			RuntimeConfigRetryInterval = originalRetry
			CCTerminationCoalesceWindow = originalCoalesce
		})

		ccCh := make(chan string, 8)
		var calls atomic.Int32
		op := &NICProvisioning{
			dmsServer:       &fakeDMSServer{running: true},
			ccTerminationCh: ccCh,
			applyRuntimeConfigFn: func(_ context.Context, _ *operations.Context) error {
				calls.Add(1)
				return nil
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		optCtx := &operations.Context{
			Status: provisioningv1.AgentStatus{Conditions: []metav1.Condition{}},
		}

		op.StartRuntimeConfigLoop(ctx, optCtx)
		require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, 10*time.Millisecond)

		ccCh <- "mlx5_0"
		ccCh <- "mlx5_1"
		ccCh <- "mlx5_2"
		require.Eventually(t, func() bool { return calls.Load() == 2 }, time.Second, 10*time.Millisecond)
		// Give the coalesce window another beat to ensure we did not get 1:1 applies.
		time.Sleep(2 * CCTerminationCoalesceWindow)
		assert.Equal(t, int32(2), calls.Load())

		cancel()
		require.NoError(t, op.Shutdown())
	})
}

func TestNICProvisioning_applyNVConfigAndUpdateStatus(t *testing.T) {
	t.Run("sets EWNICNVConfigApplied true on success", func(t *testing.T) {
		op := &NICProvisioning{
			applyNVConfigFn: func(_ context.Context, _ *operations.Context) error { return nil },
		}
		ctx := &operations.Context{
			Status: provisioningv1.AgentStatus{Conditions: []metav1.Condition{}},
		}

		require.NoError(t, op.applyNVConfigAndUpdateStatus(context.Background(), ctx))
		require.Len(t, ctx.Status.Conditions, 1)
		assert.Equal(t, cutil.AgentCondEWNICNVConfigApplied, ctx.Status.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionTrue, ctx.Status.Conditions[0].Status)
		assert.Equal(t, "NICNVConfigApplied", ctx.Status.Conditions[0].Reason)
	})

	t.Run("sets EWNICNVConfigApplied false on failure", func(t *testing.T) {
		op := &NICProvisioning{
			applyNVConfigFn: func(_ context.Context, _ *operations.Context) error { return errors.New("nvconfig apply failed") },
		}
		ctx := &operations.Context{
			Status: provisioningv1.AgentStatus{Conditions: []metav1.Condition{}},
		}

		err := op.applyNVConfigAndUpdateStatus(context.Background(), ctx)
		require.Error(t, err)
		require.Len(t, ctx.Status.Conditions, 1)
		assert.Equal(t, cutil.AgentCondEWNICNVConfigApplied, ctx.Status.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionFalse, ctx.Status.Conditions[0].Status)
		assert.Equal(t, "NICNVConfigApplyFailed", ctx.Status.Conditions[0].Reason)
	})
}

func TestIsNICFirmwareSourceConfigured(t *testing.T) {
	assert.False(t, isNICFirmwareSourceConfigured(nil))
	assert.False(t, isNICFirmwareSourceConfigured(&provisioningv1.BlueFieldSoftware{}))
	assert.False(t, isNICFirmwareSourceConfigured(&provisioningv1.BlueFieldSoftware{
		Spec: provisioningv1.BlueFieldSpec{PlatformPldmFwBundle: ptr.To("   ")},
	}))
	assert.True(t, isNICFirmwareSourceConfigured(&provisioningv1.BlueFieldSoftware{
		Spec: provisioningv1.BlueFieldSpec{PlatformPldmFwBundle: ptr.To("https://example.com/fw.fwpkg")},
	}))
	assert.True(t, isNICFirmwareSourceConfigured(&provisioningv1.BlueFieldSoftware{
		Spec: provisioningv1.BlueFieldSpec{NicFw: ptr.To("https://example.com/nic-fw.fwpkg")},
	}))
}

func TestResolveNICFirmwareDownloadURL(t *testing.T) {
	t.Run("defaults registry URL scheme to HTTPS", func(t *testing.T) {
		got, err := resolveNICFirmwareDownloadURL("10.233.14.188:8082", "/bfb/components/fw.bin")
		require.NoError(t, err)
		assert.Equal(t, "https://10.233.14.188:8082/bfb/components/fw.bin", got)
	})

	t.Run("keeps absolute firmware URL", func(t *testing.T) {
		got, err := resolveNICFirmwareDownloadURL("10.233.14.188:8082", "https://registry.example.com/fw.bin")
		require.NoError(t, err)
		assert.Equal(t, "https://registry.example.com/fw.bin", got)
	})
}

func TestNICProvisioning_ensureNICFirmwareImageSignature(t *testing.T) {
	t.Run("does nothing when flint query succeeds", func(t *testing.T) {
		runner := &fakeBashRunner{}
		op := &NICProvisioning{runBash: runner.run}

		require.NoError(t, op.ensureNICFirmwareImageSignature("/tmp/fw.bin"))
		assert.Equal(t, []string{"flint -i '/tmp/fw.bin' q"}, runner.commands)
	})

	t.Run("writes signature words when flint reports invalid image signature", func(t *testing.T) {
		runner := &fakeBashSequenceRunner{
			results: []fakeBashResult{
				{stderr: invalidImageSignature, err: errors.New("exit status 1")},
				{},
				{},
				{},
				{},
			},
		}
		op := &NICProvisioning{runBash: runner.run}

		require.NoError(t, op.ensureNICFirmwareImageSignature("/tmp/fw.bin"))
		assert.Equal(t, []string{
			"flint -i '/tmp/fw.bin' q",
			"flint -i '/tmp/fw.bin' ww 0x0 0x4d544657",
			"flint -i '/tmp/fw.bin' ww 0x4 0xabcdef00",
			"flint -i '/tmp/fw.bin' ww 0x8 0xfade1234",
			"flint -i '/tmp/fw.bin' ww 0xc 0x5678dead",
		}, runner.commands)
	})

	t.Run("returns non-signature flint query errors", func(t *testing.T) {
		runner := &fakeBashRunner{
			stderr: "some other flint error",
			err:    errors.New("exit status 1"),
		}
		op := &NICProvisioning{runBash: runner.run}

		err := op.ensureNICFirmwareImageSignature("/tmp/fw.bin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "some other flint error")
		assert.Equal(t, []string{"flint -i '/tmp/fw.bin' q"}, runner.commands)
	})

	t.Run("returns error when writing signature word fails", func(t *testing.T) {
		runner := &fakeBashSequenceRunner{
			results: []fakeBashResult{
				{stderr: invalidImageSignature, err: errors.New("exit status 1")},
				{},
				{stderr: "write failed", err: errors.New("exit status 1")},
			},
		}
		op := &NICProvisioning{runBash: runner.run}

		err := op.ensureNICFirmwareImageSignature("/tmp/fw.bin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "0x4")
		assert.Contains(t, err.Error(), "write failed")
		assert.Equal(t, []string{
			"flint -i '/tmp/fw.bin' q",
			"flint -i '/tmp/fw.bin' ww 0x0 0x4d544657",
			"flint -i '/tmp/fw.bin' ww 0x4 0xabcdef00",
		}, runner.commands)
	})
}

func TestFilterCX9Devices(t *testing.T) {
	devices := map[string]nicconfigurationv1alpha1.NicDevice{
		"0000:01:00.0": {
			Status: nicconfigurationv1alpha1.NicDeviceStatus{
				SerialNumber: "cx9-1",
				Type:         "1025",
			},
		},
		"0000:02:00.0": {
			Status: nicconfigurationv1alpha1.NicDeviceStatus{
				SerialNumber: "cx8-1",
				Type:         "1024",
			},
		},
		"0000:03:00.0": {
			Status: nicconfigurationv1alpha1.NicDeviceStatus{
				SerialNumber: "cx9-2",
				Type:         " 1025 ",
			},
		},
	}

	filtered := filterCX9Devices(devices)
	require.Len(t, filtered, 2)
	filteredSerialNumbers := []string{
		filtered[0].Status.SerialNumber,
		filtered[1].Status.SerialNumber,
	}
	assert.ElementsMatch(t, []string{"cx9-1", "cx9-2"}, filteredSerialNumbers)
}

func TestBuildEWNicConfigurationTemplate(t *testing.T) {
	t.Run("returns nil when flavor nic config is nil", func(t *testing.T) {
		assert.Nil(t, buildEWNicConfigurationTemplate(nil))
	})

	t.Run("maps linkType-based flavor nic config fields to NCO config template", func(t *testing.T) {
		cfg := &provisioningv1.NicConfiguration{
			NumVfs:   1,
			LinkType: nicconfigurationv1alpha1.LinkTypeEnum("Ethernet"),
			Force:    true,
			RawNvConfig: []nicconfigurationv1alpha1.NvConfigParam{
				{Name: "A", Value: "1"},
			},
		}

		template := buildEWNicConfigurationTemplate(cfg)
		require.NotNil(t, template)
		assert.Equal(t, cfg.NumVfs, template.NumVfs)
		assert.Equal(t, cfg.LinkType, template.LinkType)
		assert.Equal(t, cfg.RawNvConfig, template.RawNvConfig)
		assert.Nil(t, template.NetworkBay)
	})

	t.Run("maps networkBay-based flavor nic config fields to NCO config template", func(t *testing.T) {
		cfg := &provisioningv1.NicConfiguration{
			NumVfs: 1,
			NetworkBay: &nicconfigurationv1alpha1.NetworkBaySpec{
				Conf: "nb-template",
			},
			Force: true,
			RawNvConfig: []nicconfigurationv1alpha1.NvConfigParam{
				{Name: "A", Value: "1"},
			},
		}

		template := buildEWNicConfigurationTemplate(cfg)
		require.NotNil(t, template)
		assert.Equal(t, cfg.NumVfs, template.NumVfs)
		assert.Equal(t, cfg.NetworkBay, template.NetworkBay)
		assert.Equal(t, cfg.RawNvConfig, template.RawNvConfig)
		assert.Empty(t, template.LinkType)
	})
}

func TestWarnUnrecognizedNetworkBayTargets(t *testing.T) {
	t.Run("does nothing when networkBay is not requested", func(t *testing.T) {
		require.NotPanics(t, func() {
			warnUnrecognizedNetworkBayTargets(nil, []nicconfigurationv1alpha1.NicDevice{
				newNicDevice("cx9-1", "0000:3b:00.0"),
			})
		})
	})

	t.Run("does nothing when all devices are recognized as network bay", func(t *testing.T) {
		cfg := &provisioningv1.NicConfiguration{
			NetworkBay: &nicconfigurationv1alpha1.NetworkBaySpec{Conf: "nb-template"},
		}
		devices := []nicconfigurationv1alpha1.NicDevice{
			{
				Status: nicconfigurationv1alpha1.NicDeviceStatus{
					SerialNumber: "cx9-1",
					Type:         cx9NICDeviceType,
					Ports:        []nicconfigurationv1alpha1.NicDevicePortSpec{{PCI: "0000:3b:00.0"}},
					NetworkBay:   &nicconfigurationv1alpha1.NicDeviceNetworkBayStatus{Asic: 0},
				},
			},
		}
		require.NotPanics(t, func() {
			warnUnrecognizedNetworkBayTargets(cfg, devices)
		})
	})

	t.Run("logs warning and does not error when a target device is not recognized as network bay", func(t *testing.T) {
		cfg := &provisioningv1.NicConfiguration{
			NetworkBay: &nicconfigurationv1alpha1.NetworkBaySpec{Conf: "nb-template"},
		}
		devices := []nicconfigurationv1alpha1.NicDevice{
			{
				Status: nicconfigurationv1alpha1.NicDeviceStatus{
					SerialNumber: "cx9-1",
					Type:         cx9NICDeviceType,
					Ports:        []nicconfigurationv1alpha1.NicDevicePortSpec{{PCI: "0000:3b:00.0"}},
					NetworkBay:   &nicconfigurationv1alpha1.NicDeviceNetworkBayStatus{Asic: 0},
				},
			},
			newNicDevice("cx9-2", "0000:af:00.0"),
		}
		require.NotPanics(t, func() {
			warnUnrecognizedNetworkBayTargets(cfg, devices)
		})
	})
}

type fakeBashRunner struct {
	commands []string
	stdout   string
	stderr   string
	err      error
}

func (f *fakeBashRunner) run(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	f.commands = append(f.commands, cmd)
	var stdout, stderr bytes.Buffer
	stdout.WriteString(f.stdout)
	stderr.WriteString(f.stderr)
	return stdout, stderr, f.err
}

type fakeBashResult struct {
	stdout string
	stderr string
	err    error
}

type fakeBashSequenceRunner struct {
	commands []string
	results  []fakeBashResult
}

func (f *fakeBashSequenceRunner) run(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	f.commands = append(f.commands, cmd)
	var result fakeBashResult
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	var stdout, stderr bytes.Buffer
	stdout.WriteString(result.stdout)
	stderr.WriteString(result.stderr)
	return stdout, stderr, result.err
}

func newNicDevice(serialNumber string, pciAddresses ...string) nicconfigurationv1alpha1.NicDevice {
	ports := make([]nicconfigurationv1alpha1.NicDevicePortSpec, 0, len(pciAddresses))
	for _, pci := range pciAddresses {
		ports = append(ports, nicconfigurationv1alpha1.NicDevicePortSpec{PCI: pci})
	}
	return nicconfigurationv1alpha1.NicDevice{
		Status: nicconfigurationv1alpha1.NicDeviceStatus{
			SerialNumber: serialNumber,
			Type:         cx9NICDeviceType,
			Ports:        ports,
		},
	}
}

func TestNICProvisioning_configureRestrictedMode(t *testing.T) {
	const restrictArgs = "r --disable_tracer --disable_counter_rd --disable_port_owner"

	t.Run("runs mlxprivhost once per device using first port PCI", func(t *testing.T) {
		runner := &fakeBashRunner{}
		op := &NICProvisioning{
			runBash: runner.run,
			discoveredNICDevices: []nicconfigurationv1alpha1.NicDevice{
				newNicDevice("cx9-1", "0000:3b:00.0", "0000:3b:00.1"),
				newNicDevice("cx9-2", "0000:af:00.0", "0000:af:00.1"),
			},
		}

		require.NoError(t, op.configureRestrictedMode(context.Background(), &operations.Context{}))
		assert.Equal(t, []string{
			"mlxprivhost -d 0000:3b:00.0 " + restrictArgs,
			"mlxprivhost -d 0000:af:00.0 " + restrictArgs,
		}, runner.commands)
	})

	t.Run("trims whitespace around PCI address", func(t *testing.T) {
		runner := &fakeBashRunner{}
		op := &NICProvisioning{
			runBash: runner.run,
			discoveredNICDevices: []nicconfigurationv1alpha1.NicDevice{
				newNicDevice("cx9-1", "  0000:3b:00.0  "),
			},
		}

		require.NoError(t, op.configureRestrictedMode(context.Background(), &operations.Context{}))
		assert.Equal(t, []string{"mlxprivhost -d 0000:3b:00.0 " + restrictArgs}, runner.commands)
	})

	t.Run("returns error when no devices discovered", func(t *testing.T) {
		op := &NICProvisioning{runBash: (&fakeBashRunner{}).run}

		err := op.configureRestrictedMode(context.Background(), &operations.Context{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no discovered NIC devices")
	})

	t.Run("returns error when device has no ports", func(t *testing.T) {
		runner := &fakeBashRunner{}
		op := &NICProvisioning{
			runBash: runner.run,
			discoveredNICDevices: []nicconfigurationv1alpha1.NicDevice{
				newNicDevice("cx9-noport"),
			},
		}

		err := op.configureRestrictedMode(context.Background(), &operations.Context{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cx9-noport")
		assert.Empty(t, runner.commands)
	})

	t.Run("returns error when first port PCI is empty", func(t *testing.T) {
		runner := &fakeBashRunner{}
		op := &NICProvisioning{
			runBash: runner.run,
			discoveredNICDevices: []nicconfigurationv1alpha1.NicDevice{
				newNicDevice("cx9-emptypci", "   "),
			},
		}

		err := op.configureRestrictedMode(context.Background(), &operations.Context{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cx9-emptypci")
		assert.Empty(t, runner.commands)
	})

	t.Run("returns command failure with device context", func(t *testing.T) {
		runner := &fakeBashRunner{
			stdout: "some stdout",
			stderr: "boom stderr",
			err:    errors.New("exit status 1"),
		}
		op := &NICProvisioning{
			runBash: runner.run,
			discoveredNICDevices: []nicconfigurationv1alpha1.NicDevice{
				newNicDevice("cx9-fail", "0000:3b:00.0"),
			},
		}

		err := op.configureRestrictedMode(context.Background(), &operations.Context{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cx9-fail")
		assert.Contains(t, err.Error(), "0000:3b:00.0")
		assert.Contains(t, err.Error(), "boom stderr")
	})
}
