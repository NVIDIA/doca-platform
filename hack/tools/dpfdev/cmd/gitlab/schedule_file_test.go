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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeFile writes content to dir/name and returns the full path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// descriptions returns the sorted schedule descriptions of a file.
func descriptions(file *scheduleFile) []string {
	var out []string
	for _, s := range file.Schedules {
		out = append(out, s.Description)
	}
	sort.Strings(out)
	return out
}

func TestLoadScheduleFileIncludesAndMerges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.yaml", "schedules:\n  - {description: main-a, ref: main, cron: '0 1 * * *'}\n")
	writeFile(t, dir, "release.yaml", "schedules:\n  - {description: rel-a, ref: release-v1, cron: '0 2 * * *'}\n")
	root := writeFile(t, dir, "root.yaml", "include:\n  - main.yaml\n  - release.yaml\nschedules:\n  - {description: root-a, ref: main, cron: '0 3 * * *'}\n")

	file, err := loadScheduleFile(root)
	if err != nil {
		t.Fatalf("loadScheduleFile: %v", err)
	}
	got := descriptions(file)
	want := []string{"main-a", "rel-a", "root-a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("descriptions = %v, want %v", got, want)
	}
}

func TestLoadScheduleFileCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", "include:\n  - b.yaml\nschedules:\n  - {description: a, ref: main, cron: '0 1 * * *'}\n")
	writeFile(t, dir, "b.yaml", "include:\n  - a.yaml\nschedules:\n  - {description: b, ref: main, cron: '0 2 * * *'}\n")

	file, err := loadScheduleFile(filepath.Join(dir, "a.yaml"))
	if err != nil {
		t.Fatalf("loadScheduleFile with cycle should not error: %v", err)
	}
	if got := descriptions(file); strings.Join(got, ",") != "a,b" {
		t.Fatalf("descriptions = %v, want [a b]", got)
	}
}

func TestLoadScheduleFileDiamondLoadsOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "common.yaml", "schedules:\n  - {description: common, ref: main, cron: '0 1 * * *'}\n")
	writeFile(t, dir, "left.yaml", "include: [common.yaml]\n")
	writeFile(t, dir, "right.yaml", "include: [common.yaml]\n")
	root := writeFile(t, dir, "root.yaml", "include:\n  - left.yaml\n  - right.yaml\n")

	file, err := loadScheduleFile(root)
	if err != nil {
		t.Fatalf("loadScheduleFile: %v", err)
	}
	if len(file.Schedules) != 1 {
		t.Fatalf("common schedule loaded %d times via diamond, want 1", len(file.Schedules))
	}
}

func TestLoadScheduleFileDuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", "schedules:\n  - {description: dup, ref: main, cron: '0 1 * * *'}\n")
	root := writeFile(t, dir, "root.yaml", "include: [a.yaml]\nschedules:\n  - {description: dup, ref: main, cron: '0 2 * * *'}\n")

	_, err := loadScheduleFile(root)
	if err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("expected uniqueness error, got %v", err)
	}
}

func TestLoadScheduleFileRejectsPlaintextSecret(t *testing.T) {
	dir := t.TempDir()
	root := writeFile(t, dir, "root.yaml",
		"schedules:\n  - description: s\n    ref: main\n    cron: '0 1 * * *'\n    variables:\n      - {key: MY_TOKEN, value: supersecret}\n")

	_, err := loadScheduleFile(root)
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected plaintext-secret error, got %v", err)
	}
}

func TestLoadScheduleFileAllowsSecretReference(t *testing.T) {
	dir := t.TempDir()
	root := writeFile(t, dir, "root.yaml",
		"schedules:\n  - description: s\n    ref: main\n    cron: '0 1 * * *'\n    variables:\n      - {key: MY_TOKEN, value: $MY_TOKEN}\n")

	if _, err := loadScheduleFile(root); err != nil {
		t.Fatalf("secret reference should be allowed: %v", err)
	}
}

func TestLoadScheduleFileRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	// "cron_timezon" is a typo of "cron_timezone"; a declarative source of truth
	// must reject it rather than silently drop it and sync partial state.
	root := writeFile(t, dir, "root.yaml",
		"schedules:\n  - description: s\n    ref: main\n    cron: '0 1 * * *'\n    cron_timezon: Etc/UTC\n")

	_, err := loadScheduleFile(root)
	if err == nil || !strings.Contains(err.Error(), "cron_timezon") {
		t.Fatalf("expected unknown-field error mentioning cron_timezon, got %v", err)
	}
}

// TestLoadScheduleFileTimezone covers the single-timezone rule: an omitted
// cron_timezone means scheduleTimezone, spelling it out is allowed, and any
// other timezone is refused so no schedule can run on one of its own.
func TestLoadScheduleFileTimezone(t *testing.T) {
	dir := t.TempDir()
	base := "schedules:\n  - description: s\n    ref: main\n    cron: '0 1 * * *'\n"

	omitted := writeFile(t, dir, "omitted.yaml", base)
	file, err := loadScheduleFile(omitted)
	if err != nil {
		t.Fatalf("omitted cron_timezone should load: %v", err)
	}
	if got := file.Schedules[0].cronTimezone(); got != scheduleTimezone {
		t.Fatalf("cronTimezone() = %q, want %q", got, scheduleTimezone)
	}

	explicit := writeFile(t, dir, "explicit.yaml", base+"    cron_timezone: "+scheduleTimezone+"\n")
	if _, err := loadScheduleFile(explicit); err != nil {
		t.Fatalf("explicit %s should load: %v", scheduleTimezone, err)
	}

	other := writeFile(t, dir, "other.yaml", base+"    cron_timezone: Etc/UTC\n")
	_, err = loadScheduleFile(other)
	if err == nil || !strings.Contains(err.Error(), "cron_timezone") {
		t.Fatalf("expected a cron_timezone error, got %v", err)
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"secret key with plaintext value is redacted", "MY_TOKEN", "supersecret", "<redacted>"},
		{"secret key with variable reference is kept", "MY_TOKEN", "$MY_TOKEN", "$MY_TOKEN"},
		{"secret key with empty value is kept empty", "MY_TOKEN", "", ""},
		{"non-secret key with plaintext value is kept", "CACHE_KEY", "e2e", "e2e"},
		{"non-secret key with plaintext value is kept 2", "REGISTRY", "nvcr.io/nvstaging/doca", "nvcr.io/nvstaging/doca"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactValue(tt.key, tt.value); got != tt.want {
				t.Fatalf("redactValue(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
			}
		})
	}
}
