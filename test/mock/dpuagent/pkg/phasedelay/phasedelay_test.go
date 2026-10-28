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

package phasedelay

import (
	"context"
	"errors"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

// lowest and highest replace the random source with its extremes.
func lowest(int64) int64    { return 0 }
func highest(n int64) int64 { return n - 1 }

func newDelays(randN func(int64) int64, ranges map[provisioningv1.DPUPhase]Range) *Delays {
	d := New(ranges)
	d.randN = randN
	return d
}

func TestNilDelaysNeverWait(t *testing.T) {
	var d *Delays
	if got := d.Draw(provisioningv1.DPUOSInstalling, StepBoot, 1); got != 0 {
		t.Fatalf("nil Draw = %v", got)
	}
	if err := d.Wait(context.Background(), provisioningv1.DPUOSInstalling, StepBoot, 1); err != nil {
		t.Fatalf("nil Wait = %v", err)
	}
	if New(nil) != nil {
		t.Fatal("New(nil) must return nil")
	}
}

func TestDrawUnconfiguredOrUnweighted(t *testing.T) {
	d := New(map[provisioningv1.DPUPhase]Range{provisioningv1.DPURebooting: {Min: time.Second, Max: time.Second}})
	if got := d.Draw(provisioningv1.DPUOSInstalling, StepBoot, 1); got != 0 {
		t.Fatalf("unconfigured phase drew %v", got)
	}
	if got := d.Draw(provisioningv1.DPURebooting, StepTransfer, 1); got != 0 {
		t.Fatalf("step without weight drew %v", got)
	}
	if got := d.Draw(provisioningv1.DPURebooting, StepBoot, 0); got != 0 {
		t.Fatalf("zero parts drew %v", got)
	}
}

func TestDrawBounds(t *testing.T) {
	ranges := map[provisioningv1.DPUPhase]Range{provisioningv1.DPUOSInstalling: {Min: 10 * time.Second, Max: 20 * time.Second}}
	for _, tc := range []struct {
		step     Step
		parts    int
		low, top time.Duration
	}{
		{StepBoot, 1, 1500 * time.Millisecond, 3 * time.Second},
		{StepAgentStart, 4, 250 * time.Millisecond, 500 * time.Millisecond},
		{StepTransfer, 1, 5 * time.Second, 10 * time.Second},
	} {
		if got := newDelays(lowest, ranges).Draw(provisioningv1.DPUOSInstalling, tc.step, tc.parts); got != tc.low {
			t.Errorf("%s/%d lowest = %v, want %v", tc.step, tc.parts, got, tc.low)
		}
		if got := newDelays(highest, ranges).Draw(provisioningv1.DPUOSInstalling, tc.step, tc.parts); got != tc.top {
			t.Errorf("%s/%d highest = %v, want %v", tc.step, tc.parts, got, tc.top)
		}
	}
}

// TestPhaseTotalWithinRange sums every step a BF3 and a BF4 OS install performs and checks the
// total stays within the configured range.
func TestPhaseTotalWithinRange(t *testing.T) {
	const minDelay, maxDelay = 600 * time.Second, 900 * time.Second
	ranges := map[provisioningv1.DPUPhase]Range{provisioningv1.DPUOSInstalling: {Min: minDelay, Max: maxDelay}}
	total := func(d *Delays, transfer []Step) time.Duration {
		var sum time.Duration
		for _, s := range append(transfer, StepInstall, StepBoot) {
			sum += d.Draw(provisioningv1.DPUOSInstalling, s, 1)
		}
		for range 4 {
			sum += d.Draw(provisioningv1.DPUOSInstalling, StepAgentStart, 4)
		}
		return sum
	}
	for name, transfer := range map[string][]Step{"bf3": {StepTransfer}, "bf4": {StepTransferISO, StepTransferSeed}} {
		const slack = 10 * time.Nanosecond // float scaling rounds each step down
		if got := total(newDelays(lowest, ranges), transfer); got < minDelay-slack || got > minDelay {
			t.Errorf("%s lowest total %v, want %v", name, got, minDelay)
		}
		if got := total(newDelays(highest, ranges), transfer); got < maxDelay-slack || got > maxDelay {
			t.Errorf("%s highest total %v, want %v", name, got, maxDelay)
		}
	}
}

func TestWeightsSumToOne(t *testing.T) {
	sets := map[provisioningv1.DPUPhase][][]Step{
		provisioningv1.DPUOSInstalling: {
			{StepTransfer, StepInstall, StepBoot, StepAgentStart},
			{StepTransferISO, StepTransferSeed, StepInstall, StepBoot, StepAgentStart},
		},
		provisioningv1.DPUUpdateFirmware:   {{StepFirmwareTask}},
		provisioningv1.DPURebooting:        {{StepArmShutdown, StepBoot, StepAgentStart}},
		provisioningv1.DPUConfig:           {{StepOperation}},
		provisioningv1.DPUClusterConfig:    {{StepOperation}},
		provisioningv1.DPUServiceReadiness: {{StepOperation}},
	}
	if len(sets) != len(SupportedPhases()) {
		t.Fatalf("test covers %d phases, %d supported", len(sets), len(SupportedPhases()))
	}
	for phase, stepSets := range sets {
		if !Supported(phase) {
			t.Errorf("%s not supported", phase)
		}
		for _, steps := range stepSets {
			var sum float64
			for _, s := range steps {
				sum += weights[phase][s]
			}
			if sum < 0.999999 || sum > 1.000001 {
				t.Errorf("%s %v weights sum to %v", phase, steps, sum)
			}
		}
	}
	for _, phase := range []provisioningv1.DPUPhase{provisioningv1.DPUPerformArmForceRestart, provisioningv1.DPUInitializeInterface, provisioningv1.DPUConfigFWParameters, provisioningv1.DPUReady} {
		if Supported(phase) {
			t.Errorf("%s must not be supported", phase)
		}
	}
}

func TestSleep(t *testing.T) {
	start := time.Now()
	if err := Sleep(context.Background(), 50*time.Millisecond); err != nil || time.Since(start) < 50*time.Millisecond {
		t.Fatalf("Sleep returned %v after %v", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start = time.Now()
	if err := Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("canceled Sleep returned %v after %v", err, time.Since(start))
	}
	if err := Sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("zero Sleep on a canceled context returned %v", err)
	}
}

func TestWaitSleepsTheDraw(t *testing.T) {
	d := New(map[provisioningv1.DPUPhase]Range{provisioningv1.DPUUpdateFirmware: {Min: 100 * time.Millisecond, Max: 100 * time.Millisecond}})
	start := time.Now()
	if err := d.Wait(context.Background(), provisioningv1.DPUUpdateFirmware, StepFirmwareTask, 1); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("Wait returned after %v", elapsed)
	}
}
