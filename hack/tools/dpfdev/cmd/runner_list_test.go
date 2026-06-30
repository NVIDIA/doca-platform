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

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"
)

func TestRunnerStatusBadge(t *testing.T) {
	tests := []struct {
		name   string
		runner gitlab.RunnerDetails
		want   string
	}{
		{name: "online", runner: gitlab.RunnerDetails{Active: true, Online: true}, want: "● ONLINE"},
		{name: "offline", runner: gitlab.RunnerDetails{Active: true}, want: "● OFFLINE"},
		{name: "paused", runner: gitlab.RunnerDetails{Active: true, Online: true, Paused: true}, want: "Ⅱ PAUSED"},
		{name: "inactive", runner: gitlab.RunnerDetails{Online: true}, want: "○ INACTIVE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runnerStatusBadge(&tt.runner, false); got != tt.want {
				t.Fatalf("runnerStatusBadge() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintRunnersTo(t *testing.T) {
	var output bytes.Buffer
	printRunnersTo(&output, []runnerWithJob{{
		Details: &gitlab.RunnerDetails{ID: 42, Name: "runner-42", Description: "BlueField test runner", Active: true, Online: true, RunnerType: "project_type", TagList: []string{"bf3", "e2e"}},
		Job:     "running: e2e-health",
	}}, true)

	got := output.String()
	for _, want := range []string{"STATUS", "JOB", ansiGreen + "● ONLINE" + ansiReset, ansiYellow + "▶ e2e-health" + ansiReset, "Total runners: 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered list does not contain %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"TYPE", "project_type", "NAME", "runner-42"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("rendered list unexpectedly contains %q:\n%s", unwanted, got)
		}
	}
}

func TestPrintRunnersToWithoutColor(t *testing.T) {
	var output bytes.Buffer
	printRunnersTo(&output, []runnerWithJob{{
		Details: &gitlab.RunnerDetails{ID: 42, Active: true, Online: true},
		Job:     "idle",
	}}, false)

	if strings.Contains(output.String(), "\033[") {
		t.Fatalf("expected non-colored output, got %q", output.String())
	}
}

func TestColorEnabledWhenNoColorIsPresent(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	if colorEnabled() {
		t.Fatal("colorEnabled() = true when NO_COLOR is present")
	}
}

func TestColorEnabledForNonTerminal(t *testing.T) {
	if colorEnabledForFD(-1, false) {
		t.Fatal("colorEnabledForFD() = true for a non-terminal file descriptor")
	}
}
