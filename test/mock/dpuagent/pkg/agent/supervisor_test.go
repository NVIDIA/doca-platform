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

package agent

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/phasedelay"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/redfish"
)

const testSerial = "MT2616606M3H"

func newTestSupervisor(t *testing.T, delays *phasedelay.Delays) (*Supervisor, *redfish.State) {
	t.Helper()
	cfg, err := config.Parse([]byte("dpuType: bf3\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := redfish.NewState(redfish.Options{
		Personality:  redfish.ForType(config.DPUTypeBF3),
		SerialNumber: testSerial,
		PSID:         config.DefaultPSID,
		PF0MAC:       config.PF0MAC(testSerial),
		Firmware:     cfg.BMC.Firmware,
	})
	sup, err := NewSupervisor(cfg, t.TempDir(), state, delays)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sup.Stop)
	return sup, state
}

func setBootDelay(sup *Supervisor, d time.Duration) {
	sup.mu.Lock()
	defer sup.mu.Unlock()
	sup.bootDelay = d
}

func oemLastState(state *redfish.State) string {
	_, _, s := state.Power()
	return s
}

// returnsWithin fails the test when f is still running after a second.
func returnsWithin(t *testing.T, what string, f func()) {
	t.Helper()
	const d = time.Second
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
	}
}

func waitForOSUp(t *testing.T, state *redfish.State) {
	t.Helper()
	const want = redfish.OemLastStateOSUp
	deadline := time.Now().Add(5 * time.Second)
	for oemLastState(state) != want {
		if time.Now().After(deadline) {
			t.Fatalf("OemLastState %q, want %q", oemLastState(state), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRebootDuringBootWaitReturnsPromptly(t *testing.T) {
	sup, state := newTestSupervisor(t, nil)
	setBootDelay(sup, time.Hour)
	returnsWithin(t, "first Reboot", func() {
		sup.Reboot(context.Background(), provisioningv1.DPUPerformArmForceRestart)
	})
	if got := oemLastState(state); got != redfish.OemLastStateOSBoot {
		t.Fatalf("OemLastState during the boot wait %q", got)
	}
	setBootDelay(sup, 10*time.Millisecond)
	returnsWithin(t, "second Reboot", func() {
		sup.Reboot(context.Background(), provisioningv1.DPURebooting)
	})
	waitForOSUp(t, state)
}

func TestStopDuringBootWaitLeavesTheArmBooting(t *testing.T) {
	sup, state := newTestSupervisor(t, nil)
	setBootDelay(sup, time.Hour)
	sup.Reboot(context.Background(), provisioningv1.DPURebooting)
	returnsWithin(t, "Stop", sup.Stop)
	time.Sleep(50 * time.Millisecond)
	if got := oemLastState(state); got != redfish.OemLastStateOSBoot {
		t.Fatalf("a canceled boot must not report the OS up, got %q", got)
	}
}

func TestHostRebootReturnsWithTheArmBooting(t *testing.T) {
	for name, before := range map[string]func(*redfish.State){
		"OS running":             func(*redfish.State) {},
		"Arm shut down (SLR)":    (*redfish.State).SetPowerOff,
		"Arm already OsStarting": (*redfish.State).SetOSStarting,
	} {
		t.Run(name, func(t *testing.T) {
			sup, state := newTestSupervisor(t, nil)
			if !sup.ArmBooted() {
				t.Fatal("a factory DPU has its Arm up")
			}
			before(state)
			setBootDelay(sup, 200*time.Millisecond)
			sup.HostReboot(context.Background(), string(provisioningv1.RebootMethodSystemLevelReset))
			if got := oemLastState(state); got != redfish.OemLastStateOSBoot {
				t.Fatalf("OemLastState right after HostReboot %q, want %q", got, redfish.OemLastStateOSBoot)
			}
			if sup.ArmBooted() {
				t.Fatal("ArmBooted right after HostReboot")
			}
			waitForOSUp(t, state)
			if !sup.ArmBooted() {
				t.Fatal("ArmBooted false once the Arm is up")
			}
			// The agent shutting the Arm down again does not undo the boot.
			state.SetPowerOff()
			if !sup.ArmBooted() {
				t.Fatal("ArmBooted false after a later Arm shutdown")
			}
		})
	}
}

func TestPowerOffCancelsThePendingBoot(t *testing.T) {
	sup, state := newTestSupervisor(t, nil)
	setBootDelay(sup, time.Hour)
	sup.Reboot(context.Background(), provisioningv1.DPUPerformArmForceRestart)
	returnsWithin(t, "PowerOff during the boot wait", sup.PowerOff)
	powerState, _, oem := state.Power()
	if powerState != redfish.ForType(config.DPUTypeBF3).PowerOffState || oem != redfish.OemLastStateOff {
		t.Fatalf("after PowerOff: PowerState %q OemLastState %q", powerState, oem)
	}
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if sup.cancel != nil {
		t.Fatal("a lifecycle is still running after PowerOff")
	}
}

func TestBootWaitIncludesThePhaseDelay(t *testing.T) {
	const phase = 500 * time.Millisecond
	const share = phase * 6 / 10 // the Rebooting boot weight
	delays := phasedelay.New(map[provisioningv1.DPUPhase]phasedelay.Range{
		provisioningv1.DPURebooting: {Min: phase, Max: phase},
	})
	sup, state := newTestSupervisor(t, delays)
	setBootDelay(sup, 0)
	start := time.Now()
	sup.Reboot(context.Background(), provisioningv1.DPURebooting)
	waitForOSUp(t, state)
	if elapsed := time.Since(start); elapsed < share {
		t.Fatalf("Arm up after %v, want at least %v", elapsed, share)
	}
}

func TestForceRestartBootIsNotDelayed(t *testing.T) {
	delays := phasedelay.New(map[provisioningv1.DPUPhase]phasedelay.Range{
		provisioningv1.DPURebooting: {Min: time.Hour, Max: time.Hour},
	})
	sup, state := newTestSupervisor(t, delays)
	setBootDelay(sup, 0)
	sup.Reboot(context.Background(), provisioningv1.DPUPerformArmForceRestart)
	waitForOSUp(t, state)
}

func TestOnlyBootsThatStartTheAgentConsumeARebootMethod(t *testing.T) {
	identityDir := t.TempDir() // removed after the supervisor's Stop cleanup
	sup, state := newTestSupervisor(t, nil)
	sup.mu.Lock()
	sup.identity = &Identity{DPUName: "dpu", DPUNamespace: "ns", DPUUID: "uid"}
	sup.identityDir = identityDir
	sup.mu.Unlock()
	cursor := sup.cursor.Load

	setBootDelay(sup, time.Hour)
	sup.Reboot(context.Background(), provisioningv1.DPURebooting)
	sup.Reboot(context.Background(), provisioningv1.DPURebooting)
	sup.PowerOff()
	if got := cursor(); got != 0 {
		t.Fatalf("canceled boots consumed %d reboot methods", got)
	}

	setBootDelay(sup, 0)
	sup.Reboot(context.Background(), provisioningv1.DPURebooting)
	waitForOSUp(t, state)
	deadline := time.Now().Add(5 * time.Second)
	for cursor() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("a boot that started the agent consumed %d reboot methods, want 1", cursor())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
