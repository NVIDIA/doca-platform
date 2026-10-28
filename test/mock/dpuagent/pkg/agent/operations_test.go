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
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/phasedelay"
)

// TestBuildOperationsStages checks that every operation counts against the phase whose controller
// gate it holds up, and that the last operation of each group is the gate.
func TestBuildOperationsStages(t *testing.T) {
	ops := buildOperations(&runInputs{startPhase: provisioningv1.DPURebooting, joinMode: config.JoinSimulated})
	if len(ops) != 28 {
		t.Fatalf("got %d operations, want 28", len(ops))
	}
	groups := []struct {
		phase provisioningv1.DPUPhase
		step  phasedelay.Step
		size  int
		gate  string
	}{
		{provisioningv1.DPURebooting, phasedelay.StepAgentStart, 4, "Report Last Startup Time"},
		{provisioningv1.DPUConfig, phasedelay.StepOperation, 14, "Handle Reboot"},
		{provisioningv1.DPUClusterConfig, phasedelay.StepOperation, 8, "Start Kubelet"},
		{provisioningv1.DPUServiceReadiness, phasedelay.StepOperation, 2, "Release Host OS Init"},
	}
	i := 0
	for _, g := range groups {
		for j := 0; j < g.size; j++ {
			o := ops[i+j]
			if o.phase != g.phase || o.step != g.step || o.parts != g.size {
				t.Fatalf("operation %d %q: phase %q step %q parts %d, want %q %q %d", i+j+1, o.Name(), o.phase, o.step, o.parts, g.phase, g.step, g.size)
			}
		}
		if last := ops[i+g.size-1].Name(); last != g.gate {
			t.Fatalf("%s ends with %q, want %q", g.phase, last, g.gate)
		}
		i += g.size
	}
	first := buildOperations(&runInputs{startPhase: provisioningv1.DPUOSInstalling, joinMode: config.JoinSimulated})
	if first[0].phase != provisioningv1.DPUOSInstalling {
		t.Fatalf("agent start after a new OS counts against %q", first[0].phase)
	}
}
