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
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// secretKeyPattern matches variable keys whose values are sensitive and must
// never be stored in the file as plaintext. Such variables are kept as
// references to a GitLab CI/CD variable instead (e.g. value: $BLACK_DUCK_API_TOKEN).
var secretKeyPattern = regexp.MustCompile(`(?i)(TOKEN|PASSWORD|PASSWD|SECRET|CREDENTIAL|PRIVATE|APIKEY|SIGNING_KEY|ENCRYPTION_KEY)`)

// isVariableReference reports whether a value references a CI/CD variable
// (e.g. "$DPF_RELEASE_TOKEN") rather than being a literal value. GitLab
// expands such references at pipeline run time.
func isVariableReference(value string) bool {
	return strings.HasPrefix(value, "$")
}

// looksSecret reports whether a variable key names a sensitive value.
func looksSecret(key string) bool {
	return secretKeyPattern.MatchString(key)
}

// redactValue hides a variable's value when its key names a secret and the
// value is a plaintext literal (not a $NAME reference). Plan and inspection
// output can reach CI logs, so a schedule that still stores a secret directly
// must not have that value printed.
func redactValue(key, value string) string {
	if value != "" && looksSecret(key) && !isVariableReference(value) {
		return "<redacted>"
	}
	return value
}

// defaultScheduleFile is the root scheduled pipelines file. It may hold
// schedules directly and/or pull in other files via "include", so it is the
// single entry point for "dpfdev gitlab schedule sync".
const defaultScheduleFile = ".gitlab/scheduled-pipelines.yaml"

// scheduleFile is the on-disk format of a scheduled pipelines file. A file
// may declare schedules, include other files, or both. Include paths are
// resolved relative to the file that declares them.
type scheduleFile struct {
	Include   []string       `yaml:"include,omitempty"`
	Schedules []scheduleSpec `yaml:"schedules"`
}

// scheduleSpec describes one scheduled pipeline. Schedules are matched to
// GitLab by description, so descriptions must be unique across all files.
type scheduleSpec struct {
	Description  string `yaml:"description"`
	Ref          string `yaml:"ref"`
	Cron         string `yaml:"cron"`
	CronTimezone string `yaml:"cron_timezone,omitempty"`
	// Active defaults to true when omitted.
	Active    *bool          `yaml:"active,omitempty"`
	Variables []variableSpec `yaml:"variables,omitempty"`
}

// variableSpec describes one variable of a scheduled pipeline.
type variableSpec struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
	// VariableType defaults to env_var when omitted.
	VariableType string `yaml:"variable_type,omitempty"`
}

func (s *scheduleSpec) active() bool {
	return s.Active == nil || *s.Active
}

func (v *variableSpec) variableType() string {
	if v.VariableType == "" {
		return "env_var"
	}
	return v.VariableType
}

// mergeState tracks cross-file uniqueness and load bookkeeping while includes
// are resolved. descByName maps a schedule's description to the file that
// first defined it (for uniqueness errors); loaded tracks already-loaded
// absolute paths, which both dedups diamond includes and breaks include cycles.
type mergeState struct {
	descByName map[string]string
	loaded     map[string]bool
}

// loadScheduleFile reads the file at path, recursively resolves its includes,
// merges every schedule into one set and validates them. Include paths are
// relative to the file that declares them. Cycles and files pulled in more
// than once (e.g. a shared fragment) are loaded only once. Both schedule IDs
// and descriptions must be unique across all included files.
func loadScheduleFile(path string) (*scheduleFile, error) {
	merged := &scheduleFile{}
	state := &mergeState{
		descByName: map[string]string{},
		loaded:     map[string]bool{},
	}
	if err := loadScheduleFileInto(path, merged, state); err != nil {
		return nil, err
	}
	slog.Debug("merged schedule files", "root", path, "total", len(merged.Schedules))
	return merged, nil
}

// loadScheduleFileInto loads one file into merged, then recurses into its
// includes.
func loadScheduleFileInto(path string, merged *scheduleFile, state *mergeState) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %v", path, err)
	}
	if state.loaded[abs] {
		return nil
	}
	state.loaded[abs] = true

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", path, err)
	}

	// Decode with KnownFields so a mistyped key (e.g. "cron_timezon:") is a hard
	// error instead of being silently dropped. This file is a source of truth,
	// so partial state must never sync to GitLab unnoticed.
	var file scheduleFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return fmt.Errorf("failed to parse %s: %v", path, err)
	}
	slog.Debug("loaded schedule file", "path", path, "schedules", len(file.Schedules), "includes", len(file.Include))

	if err := validateSchedules(path, file.Schedules); err != nil {
		return err
	}
	for _, spec := range file.Schedules {
		if prev, ok := state.descByName[spec.Description]; ok {
			if prev == path {
				return fmt.Errorf("%s: duplicate schedule description %q", path, spec.Description)
			}
			return fmt.Errorf("schedule %q is defined in both %s and %s; descriptions must be unique", spec.Description, prev, path)
		}
		state.descByName[spec.Description] = path
		merged.Schedules = append(merged.Schedules, spec)
	}

	dir := filepath.Dir(path)
	for _, include := range file.Include {
		includePath := include
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(dir, include)
		}
		slog.Debug("resolving include", "from", path, "include", includePath)
		if err := loadScheduleFileInto(includePath, merged, state); err != nil {
			return err
		}
	}

	return nil
}

// validateSchedules checks a single file's schedules for the required fields,
// well-formed variables and absence of plaintext secrets. Cross-file
// description uniqueness is enforced by the caller.
func validateSchedules(path string, schedules []scheduleSpec) error {
	for i := range schedules {
		schedule := &schedules[i]
		if schedule.Description == "" || schedule.Ref == "" || schedule.Cron == "" {
			return fmt.Errorf("%s: schedule %d: description, ref and cron are required", path, i)
		}

		keys := map[string]bool{}
		for _, variable := range schedule.Variables {
			if variable.Key == "" {
				return fmt.Errorf("%s: schedule %q: variable key is required", path, schedule.Description)
			}
			if keys[variable.Key] {
				return fmt.Errorf("%s: schedule %q: duplicate variable %q", path, schedule.Description, variable.Key)
			}
			keys[variable.Key] = true
			if t := variable.variableType(); t != "env_var" && t != "file" {
				return fmt.Errorf("%s: schedule %q: variable %q: variable_type must be env_var or file", path, schedule.Description, variable.Key)
			}
			if looksSecret(variable.Key) && variable.Value != "" && !isVariableReference(variable.Value) {
				return fmt.Errorf("%s: schedule %q: variable %q looks like a secret but has a plaintext value; "+
					"store the secret as a GitLab CI/CD variable and reference it here as $NAME", path, schedule.Description, variable.Key)
			}
		}
	}
	return nil
}

// shortRef normalizes a branch ref for comparison, so "main" in the file
// matches the "refs/heads/main" GitLab returns.
func shortRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}
