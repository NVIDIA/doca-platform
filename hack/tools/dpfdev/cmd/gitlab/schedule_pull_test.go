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

package gitlab

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"
)

// realGroupFile is a representative group file in the exact format the repo
// uses (4-space indent, single-quoted descriptions, explicit active, variables
// sorted by key), used to prove pull re-renders it byte-for-byte.
const realGroupFile = `# Scheduled pipelines of this project.
#
# Schedules are matched to GitLab by description.
schedules:
    - description: 'main: e2e tests'
      ref: refs/heads/main
      cron: 0 */4 * * *
      cron_timezone: Africa/Casablanca
      active: true
      variables:
        - key: CI_PIPELINE_NAME
          value: 'main: e2e tests'
        - key: E2E_TEST
          value: "true"
    - description: 'main: unit tests'
      ref: refs/heads/main
      cron: 05 * * * *
      cron_timezone: Africa/Casablanca
      active: false
      variables:
        - key: UNIT_TEST
          value: "true"
`

// TestPullRendersExistingFormat is the fidelity guarantee: parsing a group file
// and rendering it back through the pull path must reproduce it exactly, so a
// pull that finds no drift does not reformat the file.
func TestPullRendersExistingFormat(t *testing.T) {
	dir := t.TempDir()
	root := writeFile(t, dir, "root.yaml", "include:\n  - main.yaml\n")
	writeFile(t, dir, "main.yaml", realGroupFile)

	groups, err := loadScheduleGroups(root)
	if err != nil {
		t.Fatalf("loadScheduleGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	rendered, err := marshalGroupFile(groups[0].header, scheduleFile{Schedules: groups[0].schedules})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(rendered) != realGroupFile {
		t.Fatalf("re-render changed the file:\n--- got ---\n%s\n--- want ---\n%s", rendered, realGroupFile)
	}
}

// TestSpecFromLiveNew checks the conversion conventions for a schedule new to
// the files: active is explicit, env_var is omitted, non-default type kept, and
// variables keep GitLab's order (no re-sorting).
func TestSpecFromLiveNew(t *testing.T) {
	spec := specFromLive(nil, gitlab.PipelineSchedule{
		Description:  "s",
		Ref:          "refs/heads/main",
		Cron:         "0 1 * * *",
		CronTimezone: "Etc/UTC",
		Active:       false,
		Variables: []gitlab.PipelineScheduleVariable{
			{Key: "ZED", VariableType: "env_var", Value: "z"},
			{Key: "ABLE", VariableType: "file", Value: "a"},
		},
	})
	if spec.Active == nil || *spec.Active != false {
		t.Fatalf("active should be explicit false, got %v", spec.Active)
	}
	if spec.Variables[0].Key != "ZED" || spec.Variables[1].Key != "ABLE" {
		t.Fatalf("variables should keep GitLab order, got %v", spec.Variables)
	}
	if spec.Variables[0].VariableType != "" {
		t.Fatalf("env_var type should be omitted, got %q", spec.Variables[0].VariableType)
	}
	if spec.Variables[1].VariableType != "file" {
		t.Fatalf("non-default type should be kept, got %q", spec.Variables[1].VariableType)
	}
}

// TestSpecFromLivePreservesOrder is the anti-churn guarantee: when the file
// already has the variables, their on-disk order is preserved (values refreshed
// from GitLab), and only variables new to the file are appended.
func TestSpecFromLivePreservesOrder(t *testing.T) {
	old := &scheduleSpec{Variables: []variableSpec{
		{Key: "CI_PIPELINE_NAME", Value: "x"},
		{Key: "NIGHTLY_RELEASE", Value: "true"},
		{Key: "ENABLE_SOS_REPORTS", Value: "true"},
	}}
	// GitLab returns them alphabetically and adds one; order must still follow
	// the file, with the new key appended last.
	spec := specFromLive(old, gitlab.PipelineSchedule{
		Description: "s", Ref: "refs/heads/main", Cron: "0 1 * * *", Active: true,
		Variables: []gitlab.PipelineScheduleVariable{
			{Key: "CI_PIPELINE_NAME", Value: "x"},
			{Key: "ENABLE_SOS_REPORTS", Value: "true"},
			{Key: "NEW_KEY", Value: "n"},
			{Key: "NIGHTLY_RELEASE", Value: "false"},
		},
	})
	var keys []string
	for _, v := range spec.Variables {
		keys = append(keys, v.Key)
	}
	want := "CI_PIPELINE_NAME,NIGHTLY_RELEASE,ENABLE_SOS_REPORTS,NEW_KEY"
	if strings.Join(keys, ",") != want {
		t.Fatalf("variable order = %v, want %s", keys, want)
	}
	// The refreshed value came through.
	if spec.Variables[1].Value != "false" {
		t.Fatalf("NIGHTLY_RELEASE value not refreshed, got %q", spec.Variables[1].Value)
	}
}

// pullTestServer serves a fixed set of schedules (list + per-id detail) so pull
// can be driven end to end.
func pullTestServer(t *testing.T, list string, details map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/1/pipeline_schedules" {
			_, _ = w.Write([]byte(list))
			return
		}
		if body, ok := details[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// TestPullUpdatesInPlace drives a full pull: a group file whose schedule drifted
// in GitLab (cron changed, a variable added) is rewritten in place, and the
// dry-run before it leaves the file untouched.
func TestPullUpdatesInPlace(t *testing.T) {
	dir := t.TempDir()
	spDir := filepath.Join(dir, "scheduled-pipelines")
	if err := os.MkdirAll(spDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := writeFile(t, dir, "scheduled-pipelines.yaml", "include:\n  - scheduled-pipelines/main.yaml\n")
	groupPath := writeFile(t, spDir, "main.yaml", realGroupFile)

	server := pullTestServer(t,
		`[{"id":10,"description":"main: e2e tests","ref":"refs/heads/main","cron":"0 1 * * *","active":true,"owner":{"username":"a"}},
		  {"id":20,"description":"main: unit tests","ref":"refs/heads/main","cron":"05 * * * *","active":false,"owner":{"username":"a"}}]`,
		map[string]string{
			// e2e tests: cron changed 0 */4 -> 0 1, and a new variable added.
			"/projects/1/pipeline_schedules/10": `{"id":10,"description":"main: e2e tests","ref":"refs/heads/main","cron":"0 1 * * *","cron_timezone":"Africa/Casablanca","active":true,"variables":[
				{"key":"CI_PIPELINE_NAME","variable_type":"env_var","value":"main: e2e tests"},
				{"key":"E2E_TEST","variable_type":"env_var","value":"true"},
				{"key":"NEW_VAR","variable_type":"env_var","value":"x"}]}`,
			"/projects/1/pipeline_schedules/20": `{"id":20,"description":"main: unit tests","ref":"refs/heads/main","cron":"05 * * * *","cron_timezone":"Africa/Casablanca","active":false,"variables":[
				{"key":"UNIT_TEST","variable_type":"env_var","value":"true"}]}`,
		})
	defer server.Close()
	client := gitlab.NewClient(server.URL, "token", "1")

	groups, err := loadScheduleGroups(root)
	if err != nil {
		t.Fatalf("loadScheduleGroups: %v", err)
	}

	// Dry-run must not touch the file.
	dry := &schedulePuller{client: client, dryRun: true}
	out := captureStdout(t, func() {
		if err := dry.pull(groups); err != nil {
			t.Fatalf("dry pull: %v", err)
		}
	})
	if got, _ := os.ReadFile(groupPath); string(got) != realGroupFile {
		t.Fatalf("dry-run modified the file")
	}
	if !strings.Contains(out, `~ cron: "0 */4 * * *" -> "0 1 * * *"`) || !strings.Contains(out, "+ variable NEW_VAR") {
		t.Fatalf("dry-run plan missing expected changes:\n%s", out)
	}
	if dry.updated != 1 || dry.files != 1 {
		t.Fatalf("dry-run counts: updated=%d files=%d", dry.updated, dry.files)
	}

	// Apply writes the file; reloading and pulling again is a no-op.
	apply := &schedulePuller{client: client, dryRun: false}
	if err := apply.pull(groups); err != nil {
		t.Fatalf("apply pull: %v", err)
	}
	updated, _ := os.ReadFile(groupPath)
	if !strings.Contains(string(updated), "NEW_VAR") || !strings.Contains(string(updated), "cron: 0 1 * * *") {
		t.Fatalf("file not updated:\n%s", updated)
	}

	groups2, _ := loadScheduleGroups(root)
	noop := &schedulePuller{client: client, dryRun: true}
	if err := noop.pull(groups2); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if noop.files != 0 {
		t.Fatalf("second pull should be a no-op, got files=%d", noop.files)
	}
}

// TestPullRemovedAndOrphan checks the two edge directions: a schedule deleted in
// GitLab is dropped from its file, and one that exists only in GitLab is
// reported (and captured only when --new-schedules-file names a target).
func TestPullRemovedAndOrphan(t *testing.T) {
	dir := t.TempDir()
	spDir := filepath.Join(dir, "scheduled-pipelines")
	if err := os.MkdirAll(spDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := writeFile(t, dir, "scheduled-pipelines.yaml", "include:\n  - scheduled-pipelines/main.yaml\n")
	groupPath := writeFile(t, spDir, "main.yaml", realGroupFile)

	// GitLab: "e2e tests" gone, "unit tests" unchanged, a brand-new "extra".
	server := pullTestServer(t,
		`[{"id":20,"description":"main: unit tests","ref":"refs/heads/main","cron":"05 * * * *","active":false,"owner":{"username":"a"}},
		  {"id":30,"description":"main: extra","ref":"refs/heads/main","cron":"0 2 * * *","active":true,"owner":{"username":"a"}}]`,
		map[string]string{
			"/projects/1/pipeline_schedules/20": `{"id":20,"description":"main: unit tests","ref":"refs/heads/main","cron":"05 * * * *","cron_timezone":"Africa/Casablanca","active":false,"variables":[{"key":"UNIT_TEST","variable_type":"env_var","value":"true"}]}`,
			"/projects/1/pipeline_schedules/30": `{"id":30,"description":"main: extra","ref":"refs/heads/main","cron":"0 2 * * *","cron_timezone":"Etc/UTC","active":true,"variables":[{"key":"EXTRA","variable_type":"env_var","value":"y"}]}`,
		})
	defer server.Close()
	client := gitlab.NewClient(server.URL, "token", "1")

	// Without a target, the orphan is reported and not written; the deletion
	// still applies.
	groups, _ := loadScheduleGroups(root)
	p := &schedulePuller{client: client, dryRun: false}
	out := captureStdout(t, func() {
		if err := p.pull(groups); err != nil {
			t.Fatalf("pull: %v", err)
		}
	})
	if !strings.Contains(out, `exists in GitLab but in no file`) {
		t.Fatalf("orphan not reported:\n%s", out)
	}
	if p.removed != 1 {
		t.Fatalf("expected 1 removal, got %d", p.removed)
	}
	written, _ := os.ReadFile(groupPath)
	if strings.Contains(string(written), "e2e tests") {
		t.Fatalf("deleted schedule still present:\n%s", written)
	}
	if strings.Contains(string(written), "extra") {
		t.Fatalf("orphan written without a target file:\n%s", written)
	}

	// With the target, the orphan lands in the named file.
	writeFile(t, spDir, "main.yaml", realGroupFile) // reset
	groups2, _ := loadScheduleGroups(root)
	p2 := &schedulePuller{client: client, dryRun: false, newFile: groupPath}
	if err := p2.pull(groups2); err != nil {
		t.Fatalf("pull with target: %v", err)
	}
	if p2.added != 1 {
		t.Fatalf("expected 1 add, got %d", p2.added)
	}
	final, _ := os.ReadFile(groupPath)
	if !strings.Contains(string(final), "main: extra") {
		t.Fatalf("orphan not captured into target file:\n%s", final)
	}
}
