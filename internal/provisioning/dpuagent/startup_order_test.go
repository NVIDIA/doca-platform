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

package dpuagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nicprovisioning"

	"github.com/onsi/gomega/gbytes"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestRunStartsRuntimeOperationBeforeHostRelease verifies startup before release, completion after readback, and cancellation.
func TestRunStartsRuntimeOperationBeforeHostRelease(t *testing.T) {
	for _, name := range []string{"certificate", "spiffe", "cancel"} {
		t.Run(name, func(t *testing.T) {
			spiffeMode := name == "spiffe"
			logs := gbytes.NewBuffer()
			logState := klog.CaptureState()
			t.Cleanup(logState.Restore)
			klog.LogToStderr(false)
			klog.SetOutput(logs)
			dir := t.TempDir()
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			// Prevent the attestor from enabling a real SPIRE configuration during the test.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "openssl"), []byte("#!/bin/sh\nexit 1\n"), 0700))
			t.Setenv("RELEASE_STATE", filepath.Join(dir, "released"))
			t.Setenv("RELEASE_READ", filepath.Join(dir, "read"))
			readbackBlock := filepath.Join(dir, "block-readback")
			t.Setenv("RELEASE_BLOCK_READBACK", readbackBlock)
			if name == "certificate" {
				require.NoError(t, os.WriteFile(readbackBlock, nil, 0600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mlxreg"), []byte(`#!/bin/sh
case "$*" in
 *--set*) touch "$RELEASE_STATE" ;;
 *) touch "$RELEASE_READ"
    echo 'delay_host_os_init_clr=0'
    echo 'host_os_init_mode=3'
    if [ -f "$RELEASE_STATE" ] && [ ! -f "$RELEASE_BLOCK_READBACK" ]; then echo 'delay_host_os_init=0'; else echo 'delay_host_os_init=1'; fi ;;
esac
`), 0700))
			dpu := newTestDPU()
			c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			optCtx := newTestOptCtx(c)
			optCtx.LatestDPU = dpu
			optCtx.Options.SpiffeMode = spiffeMode
			optCtx.Options.SkipAstra = true
			optCtx.Options.SkipRebootMethodDiscovery = true
			optCtx.DPUFlavor.Generation = 9
			agent := NewDPUAgent(optCtx)
			// Keep the constructor's startup/release operations; replace only hardware setup.
			runtimeOperations := agent.operations[len(agent.operations)-2:]
			require.Equal(t, "Start runtime workers", runtimeOperations[0].Name())
			require.Equal(t, "Release Host OS Init", runtimeOperations[1].Name())
			agent.operations = append([]operations.Operation{
				&mockOperation{
					name:          "bootstrap",
					conditionType: "Bootstrap",
					executeFunc: func(_ context.Context, oc *operations.Context) error {
						oc.SetResolvedNVConfig(&operations.ResolvedNVConfig{
							HostOSInitRequired: true,
							HostOSInitPCIs:     []string{"0000:03:00.0"},
						})
						return nil
					},
				},
				&nicprovisioning.NICProvisioning{},
			}, runtimeOperations...)
			agent.retryInterval = 10 * time.Millisecond
			agent.runDir = t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("Run did not stop after cancellation")
				}
				require.NoError(t, agent.Shutdown())
			})
			go func() {
				defer close(done)
				done <- agent.Run(ctx)
			}()
			require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "read")); return err == nil }, time.Second, time.Millisecond)
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(dpu), dpu))
			require.True(t, meta.IsStatusConditionTrue(dpu.Status.AgentStatus.Conditions, "RuntimeWorkersStarted"),
				"worker startup must be reported while host release is still waiting")
			// With no DMS session, this diagnostic proves the real NIC startup method was reached.
			require.Contains(t, string(logs.Contents()), "NIC provisioning: no local DMS session; skip runtime configuration loop")
			if spiffeMode {
				require.Eventually(t, func() bool {
					return meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled) != nil
				}, time.Second, time.Millisecond, "SPIRE attestor must reconcile while host release is blocked")
			} else {
				require.Nil(t, meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled))
			}
			require.NoFileExists(t, filepath.Join(dir, "released"))
			require.NoFileExists(t, filepath.Join(agent.runDir, doneMarkerFileName))
			require.Empty(t, optCtx.Status.GetLocal().AppliedFlavorGeneration)

			if name == "cancel" {
				cancel()
				select {
				case err := <-done:
					require.ErrorIs(t, err, context.Canceled)
				case <-time.After(2 * time.Second):
					t.Fatal("release wait did not abort")
				}
				require.NoFileExists(t, filepath.Join(dir, "released"))
				require.NoFileExists(t, filepath.Join(agent.runDir, doneMarkerFileName))
				require.Zero(t, optCtx.Status.GetLocal().AppliedFlavorGeneration)
				return
			}

			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(dpu), dpu))
			base := dpu.DeepCopy()
			dpu.Status.OperationalConditions = []metav1.Condition{{
				Type:   string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
				Status: metav1.ConditionTrue,
			}}
			require.NoError(t, c.Status().Patch(ctx, dpu, client.MergeFrom(base)))
			if name == "certificate" {
				require.Eventually(t, func() bool {
					cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, "ReleaseHostOSInit")
					return cond != nil && strings.Contains(cond.Message, "remains held")
				}, time.Second, time.Millisecond)
				require.NoFileExists(t, filepath.Join(agent.runDir, doneMarkerFileName))
				require.Zero(t, optCtx.Status.GetLocal().AppliedFlavorGeneration)
				require.NoError(t, os.Remove(readbackBlock))
			}
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("ready gate did not release host")
			}
			require.FileExists(t, filepath.Join(dir, "released"))
			require.FileExists(t, filepath.Join(agent.runDir, doneMarkerFileName))
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(dpu), dpu))
			require.NotNil(t, dpu.Status.AgentStatus.HostOSInit.Succeeded)
			require.EqualValues(t, 9, dpu.Status.AgentStatus.AppliedFlavorGeneration)
		})
	}
}
