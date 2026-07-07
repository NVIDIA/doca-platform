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
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"
)

var errRunnerStatus = errors.New("status unavailable")

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

func TestRunnerDetailsFromRunner(t *testing.T) {
	runner := gitlab.Runner{ID: 7, Description: "group runner", Active: true, IsShared: true, RunnerType: "group_type", Name: "gr-7", Online: true, Status: "online", IPAddress: "10.0.0.7"}
	want := &gitlab.RunnerDetails{ID: 7, Description: "group runner", Active: true, IsShared: true, RunnerType: "group_type", Name: "gr-7", Online: true, Status: "online", IPAddress: "10.0.0.7"}

	if got := runnerDetailsFromRunner(runner); !reflect.DeepEqual(got, want) {
		t.Fatalf("runnerDetailsFromRunner() = %+v, want %+v", got, want)
	}
}

func TestIsSharedRunner(t *testing.T) {
	tests := []struct {
		name   string
		runner gitlab.Runner
		want   bool
	}{
		{name: "instance type", runner: gitlab.Runner{RunnerType: "instance_type"}, want: true},
		{name: "is_shared flag", runner: gitlab.Runner{IsShared: true}, want: true},
		{name: "group type", runner: gitlab.Runner{RunnerType: "group_type"}, want: true},
		{name: "project type", runner: gitlab.Runner{RunnerType: "project_type"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSharedRunner(tt.runner); got != tt.want {
				t.Fatalf("isSharedRunner() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobStatusBadge(t *testing.T) {
	tests := []struct {
		name string
		job  string
		want string
	}{
		{name: "idle", job: "idle", want: "• IDLE"},
		{name: "unknown", job: "unknown", want: "• N/A"},
		{name: "busy", job: "busy", want: "▶ RUNNING"},
		{name: "running", job: "running: e2e-health", want: "▶ e2e-health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jobStatusBadge(tt.job, false); got != tt.want {
				t.Fatalf("jobStatusBadge(%q) = %q, want %q", tt.job, got, tt.want)
			}
		})
	}
}

type fakeRunnerJobClient struct {
	status    string
	statusErr error
	jobs      []gitlab.Job
	jobsErr   error
}

func (f fakeRunnerJobClient) GetRunnerJobExecutionStatus(int) (string, error) {
	return f.status, f.statusErr
}

func (f fakeRunnerJobClient) GetRunnerJobs(int, string, int) ([]gitlab.Job, error) {
	return f.jobs, f.jobsErr
}

func TestRunnerJobState(t *testing.T) {
	groupRunner := gitlab.Runner{ID: 1, RunnerType: "group_type"}
	projectRunner := gitlab.Runner{ID: 2, RunnerType: "project_type"}

	tests := []struct {
		name        string
		client      fakeRunnerJobClient
		runner      gitlab.Runner
		projectJobs map[int]gitlab.Job
		want        string
	}{
		{
			name:        "active named from project jobs",
			client:      fakeRunnerJobClient{status: "active"},
			runner:      groupRunner,
			projectJobs: map[int]gitlab.Job{1: {Name: "confluence-docs"}},
			want:        "running: confluence-docs",
		},
		{
			name:   "active with visible runner job name",
			client: fakeRunnerJobClient{status: "active", jobs: []gitlab.Job{{Name: "e2e-quick"}}},
			runner: groupRunner,
			want:   "running: e2e-quick",
		},
		{
			name:   "active with hidden job name",
			client: fakeRunnerJobClient{status: "active"},
			runner: groupRunner,
			want:   "busy",
		},
		{
			name:   "idle status",
			client: fakeRunnerJobClient{status: "idle"},
			runner: groupRunner,
			want:   "idle",
		},
		{
			name:   "status unavailable falls back to visible job",
			client: fakeRunnerJobClient{statusErr: errRunnerStatus, jobs: []gitlab.Job{{Name: "lint"}}},
			runner: groupRunner,
			want:   "running: lint",
		},
		{
			name:   "status unavailable on shared runner is unknown",
			client: fakeRunnerJobClient{statusErr: errRunnerStatus},
			runner: groupRunner,
			want:   "unknown",
		},
		{
			name:   "status unavailable on project runner is idle",
			client: fakeRunnerJobClient{statusErr: errRunnerStatus},
			runner: projectRunner,
			want:   "idle",
		},
		{
			name:   "empty status on shared runner is unknown",
			client: fakeRunnerJobClient{status: ""},
			runner: groupRunner,
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runnerJobState(tt.client, tt.runner, tt.projectJobs); got != tt.want {
				t.Fatalf("runnerJobState() = %q, want %q", got, tt.want)
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
