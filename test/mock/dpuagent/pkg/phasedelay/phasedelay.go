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

// Package phasedelay spreads the extra time a tester gives a DPU phase over the steps the mock
// performs while the DPU is in that phase. See the "Phase delays" section of the README.
package phasedelay

import (
	"context"
	"math/rand/v2"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/klog/v2"
)

// Step is one thing the mock does that a phase delay can stretch.
type Step string

const (
	// StepTransfer is the BF3 BFB SimpleUpdate task.
	StepTransfer Step = "transfer"
	// StepTransferISO is the BF4 OS ISO SimpleUpdate task.
	StepTransferISO Step = "transferISO"
	// StepTransferSeed is the BF4 seed.iso SimpleUpdate task.
	StepTransferSeed Step = "transferSeed"
	// StepInstall is the Arm writing a newly delivered OS before booting it.
	StepInstall Step = "install"
	// StepBoot is the Arm reporting OsStarting.
	StepBoot Step = "boot"
	// StepAgentStart covers the agent operations up to Report Last Startup Time.
	StepAgentStart Step = "agentStart"
	// StepFirmwareTask is the BF4 PLDM bundle upload task.
	StepFirmwareTask Step = "firmwareTask"
	// StepArmShutdown is the Arm shutting down after the agent reported System Level Reset.
	StepArmShutdown Step = "armShutdown"
	// StepOperation covers the agent operations that hold up a phase's controller gate.
	StepOperation Step = "operation"
)

// supportedPhases lists the phases that can be delayed, in provisioning order.
var supportedPhases = []provisioningv1.DPUPhase{
	provisioningv1.DPUUpdateFirmware,
	provisioningv1.DPUOSInstalling,
	provisioningv1.DPURebooting,
	provisioningv1.DPUConfig,
	provisioningv1.DPUClusterConfig,
	provisioningv1.DPUServiceReadiness,
}

// weights is the share of a phase's delay each of its steps waits. The steps one DPU performs in a
// phase sum to 1: on OS Installing a BF3 transfers once (StepTransfer), a BF4 twice
// (StepTransferISO and StepTransferSeed).
var weights = map[provisioningv1.DPUPhase]map[Step]float64{
	provisioningv1.DPUOSInstalling: {
		StepTransfer:     0.50,
		StepTransferISO:  0.40,
		StepTransferSeed: 0.10,
		StepInstall:      0.25,
		StepBoot:         0.15,
		StepAgentStart:   0.10,
	},
	provisioningv1.DPUUpdateFirmware:   {StepFirmwareTask: 1},
	provisioningv1.DPURebooting:        {StepArmShutdown: 0.20, StepBoot: 0.60, StepAgentStart: 0.20},
	provisioningv1.DPUConfig:           {StepOperation: 1},
	provisioningv1.DPUClusterConfig:    {StepOperation: 1},
	provisioningv1.DPUServiceReadiness: {StepOperation: 1},
}

// Supported reports whether phase can be delayed.
func Supported(phase provisioningv1.DPUPhase) bool {
	_, ok := weights[phase]
	return ok
}

// SupportedPhases lists the phases that can be delayed, in provisioning order.
func SupportedPhases() []provisioningv1.DPUPhase {
	return append([]provisioningv1.DPUPhase(nil), supportedPhases...)
}

// Range is a closed range of extra time for one phase.
type Range struct {
	Min, Max time.Duration
}

// Delays draws the waits of the configured phases. A nil *Delays never waits.
type Delays struct {
	ranges map[provisioningv1.DPUPhase]Range
	randN  func(n int64) int64
}

// New returns the delays of ranges, or nil when no phase is delayed.
func New(ranges map[provisioningv1.DPUPhase]Range) *Delays {
	if len(ranges) == 0 {
		return nil
	}
	return &Delays{ranges: ranges, randN: rand.Int64N}
}

// Draw returns how long one of parts equal pieces of step waits in phase: a uniformly random
// duration in the phase range scaled by the step weight divided by parts. It is zero when the
// phase is not configured or the step has no weight in it. A non-zero draw is logged.
func (d *Delays) Draw(phase provisioningv1.DPUPhase, step Step, parts int) time.Duration {
	if d == nil || parts <= 0 {
		return 0
	}
	r, ok := d.ranges[phase]
	if !ok {
		return 0
	}
	w := weights[phase][step]
	if w == 0 {
		return 0
	}
	scale := w / float64(parts)
	low := time.Duration(float64(r.Min) * scale)
	high := time.Duration(float64(r.Max) * scale)
	dur := low
	if span := high - low; span > 0 {
		dur += time.Duration(d.randN(int64(span) + 1))
	}
	if dur > 0 {
		klog.InfoS("DPU phase delay", "phase", phase, "step", step, "duration", dur)
	}
	return dur
}

// Wait sleeps for Draw(phase, step, parts) and returns ctx.Err() when ctx ends first.
func (d *Delays) Wait(ctx context.Context, phase provisioningv1.DPUPhase, step Step, parts int) error {
	return Sleep(ctx, d.Draw(phase, step, parts))
}

// Sleep waits for dur or until ctx ends, whichever comes first, and returns ctx.Err(), which is
// nil when the full duration elapsed on a live context.
func Sleep(ctx context.Context, dur time.Duration) error {
	if dur <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(dur)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
