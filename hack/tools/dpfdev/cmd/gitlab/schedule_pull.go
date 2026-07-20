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
	"fmt"
	"os"
	"sort"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

var (
	schedulePullFile    string
	schedulePullApply   bool
	schedulePullNewFile string
)

func init() {
	scheduleCmd.AddCommand(schedulePullCmd)

	schedulePullCmd.Flags().StringVar(&schedulePullFile, "file", defaultScheduleFile, "Root scheduled pipelines file to update (may pull in others via include)")
	schedulePullCmd.Flags().BoolVar(&schedulePullApply, "apply", false, "Write the changes to the YAML files; without this flag pull only prints the plan")
	schedulePullCmd.Flags().StringVar(&schedulePullNewFile, "new-schedules-file", "",
		"Group file to place schedules that exist in GitLab but in no file yet; without it, such schedules are only reported")
}

var schedulePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Update the scheduled pipelines files from GitLab",
	Long: `Update the scheduled pipelines files to match GitLab, the reverse of "sync".

Use it after someone edits schedules in the GitLab UI: pull rewrites each group
file so it reflects the live schedules, updating changed fields and variables,
dropping schedules that were deleted in GitLab, and (with --new-schedules-file)
capturing schedules that were added there.

Schedules are matched to their file by description. A schedule that exists in
GitLab but in no file has no home; pull reports it, and only writes it when
--new-schedules-file names the group file to add it to.

Without --apply only the plan is printed and no file is changed.

Values are read back verbatim, so a schedule that stores a plaintext secret in
GitLab fails the same secret check as sync: fix it in the UI to reference a
CI/CD variable ($NAME) first.`,
	Example: `  # Show what pulling from GitLab would change (nothing is written)
  dpfdev gitlab schedule pull

  # Write the changes back to the files
  dpfdev gitlab schedule pull --apply

  # Also capture UI-added schedules, placing them in other.yaml
  dpfdev gitlab schedule pull --apply --new-schedules-file .gitlab/scheduled-pipelines/other.yaml`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		groups, err := loadScheduleGroups(schedulePullFile)
		if err != nil {
			return err
		}

		client, err := newGitLabClient(cmd)
		if err != nil {
			return err
		}

		puller := &schedulePuller{client: client, dryRun: !schedulePullApply, newFile: schedulePullNewFile}
		return puller.pull(groups)
	},
}

// schedulePuller rewrites the on-disk group files from the live GitLab state.
type schedulePuller struct {
	client  *gitlab.Client
	dryRun  bool
	newFile string

	updated, added, removed, files int
}

func (p *schedulePuller) pull(groups []scheduleGroup) error {
	progress("Fetching schedules from GitLab...")
	existing, err := p.client.ListPipelineSchedules()
	if err != nil {
		return fmt.Errorf("failed to fetch pipeline schedules: %v", err)
	}

	ids := make([]int, 0, len(existing))
	for _, schedule := range existing {
		ids = append(ids, schedule.ID)
	}
	// Reuse the syncer's concurrent detail fetch: the list omits variables, so
	// each schedule needs its own GET.
	details, err := (&scheduleSyncer{client: p.client, concurrency: defaultSyncConcurrency}).fetchDetails(ids)
	if err != nil {
		return err
	}

	liveByDesc := map[string]*gitlab.PipelineSchedule{}
	for _, id := range ids {
		schedule := details[id]
		if schedule == nil {
			continue
		}
		if schedule.Variables == nil {
			return fmt.Errorf("GitLab did not return the variables of schedule %d (%s): the token needs the Maintainer role", schedule.ID, schedule.Description)
		}
		if prev, ok := liveByDesc[schedule.Description]; ok {
			return fmt.Errorf("GitLab has two schedules named %q (ids %d and %d); pull matches by description, so delete or rename one", schedule.Description, prev.ID, schedule.ID)
		}
		liveByDesc[schedule.Description] = schedule
	}

	// consumed marks live schedules already written to some file, so the
	// leftovers are the ones added in the UI with no home yet.
	consumed := map[string]bool{}

	// Resolve every file's new schedule set from the live state, preserving the
	// per-file grouping. Deleted schedules simply fall out.
	newSpecs := make([][]scheduleSpec, len(groups))
	targetIdx := -1
	for i := range groups {
		if p.newFile != "" && groups[i].path == p.newFile {
			targetIdx = i
		}
		for _, spec := range groups[i].schedules {
			live, ok := liveByDesc[spec.Description]
			if !ok {
				continue // deleted in GitLab
			}
			consumed[spec.Description] = true
			old := spec
			newSpecs[i] = append(newSpecs[i], specFromLive(&old, *live))
		}
	}

	// Schedules present in GitLab but in no file. Route them to the target file
	// if one was named, otherwise just report them.
	var orphans []*gitlab.PipelineSchedule
	for _, id := range ids {
		schedule := details[id]
		if schedule == nil || consumed[schedule.Description] {
			continue
		}
		orphans = append(orphans, schedule)
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Description < orphans[j].Description })

	if len(orphans) > 0 {
		if p.newFile == "" {
			for _, schedule := range orphans {
				fmt.Printf("! schedule %q (%d) exists in GitLab but in no file; pass --new-schedules-file to capture it\n",
					schedule.Description, schedule.ID)
			}
		} else if targetIdx == -1 {
			return fmt.Errorf("--new-schedules-file %s is not one of the group files pulled from %s", p.newFile, schedulePullFile)
		} else {
			for _, schedule := range orphans {
				newSpecs[targetIdx] = append(newSpecs[targetIdx], specFromLive(nil, *schedule))
			}
		}
	}

	// Rewrite each file whose content changed, in file order for deterministic
	// output.
	for i := range groups {
		if err := p.applyGroup(&groups[i], newSpecs[i]); err != nil {
			return err
		}
	}

	action := "applied"
	if p.dryRun {
		action = "planned (not applied; use --apply)"
	}
	fmt.Printf("Pull %s: %d files changed, %d schedules updated, %d added, %d removed\n",
		action, p.files, p.updated, p.added, p.removed)
	return nil
}

// applyGroup diffs one file's live-derived schedules against its current
// contents, prints the change, and (unless dry-run) writes it back. specs keep
// the file's existing order (surviving schedules in place, new ones appended),
// so an unchanged pull rewrites nothing.
func (p *schedulePuller) applyGroup(group *scheduleGroup, specs []scheduleSpec) error {
	// Validate the generated file the same way the loader validates a
	// hand-authored one, so pull can never write a plaintext secret or a
	// malformed schedule.
	if err := validateSchedules(group.path, specs); err != nil {
		return err
	}

	rendered, err := marshalGroupFile(group.header, scheduleFile{Include: group.include, Schedules: specs})
	if err != nil {
		return fmt.Errorf("failed to render %s: %v", group.path, err)
	}
	if string(rendered) == string(group.raw) {
		return nil
	}

	p.files++
	oldByDesc := map[string]scheduleSpec{}
	for _, spec := range group.schedules {
		oldByDesc[spec.Description] = spec
	}
	newByDesc := map[string]scheduleSpec{}
	for _, spec := range specs {
		newByDesc[spec.Description] = spec
	}

	fmt.Printf("~ file %s\n", group.path)
	for i := range specs {
		spec := &specs[i]
		old, ok := oldByDesc[spec.Description]
		if !ok {
			p.added++
			fmt.Printf("  + schedule %q\n", spec.Description)
			continue
		}
		changes := diffSpec(&old, spec)
		if len(changes) == 0 {
			continue
		}
		p.updated++
		fmt.Printf("  ~ schedule %q\n", spec.Description)
		for _, change := range changes {
			fmt.Println(change)
		}
	}
	for _, spec := range group.schedules {
		if _, ok := newByDesc[spec.Description]; !ok {
			p.removed++
			fmt.Printf("  - schedule %q (deleted in GitLab)\n", spec.Description)
		}
	}

	if p.dryRun {
		return nil
	}
	if err := os.WriteFile(group.path, rendered, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %v", group.path, err)
	}
	return nil
}

// specFromLive converts a live GitLab schedule into the file's scheduleSpec
// form, preserving the order of the file's current version (old, nil for a
// schedule new to the file) so an unchanged pull re-renders identically. Only
// values and types are refreshed; existing variables keep their position and
// variables GitLab added are appended in GitLab's order. active is always
// written explicitly and the default env_var variable type is omitted, matching
// the conventions the group files already use.
func specFromLive(old *scheduleSpec, s gitlab.PipelineSchedule) scheduleSpec {
	active := s.Active
	spec := scheduleSpec{
		Description:  s.Description,
		Ref:          s.Ref,
		Cron:         s.Cron,
		CronTimezone: s.CronTimezone,
		Active:       &active,
	}

	liveByKey := make(map[string]gitlab.PipelineScheduleVariable, len(s.Variables))
	for _, variable := range s.Variables {
		liveByKey[variable.Key] = variable
	}
	seen := map[string]bool{}
	if old != nil {
		for _, ov := range old.Variables {
			live, ok := liveByKey[ov.Key]
			if !ok {
				continue // removed in GitLab
			}
			spec.Variables = append(spec.Variables, toVarSpec(live))
			seen[ov.Key] = true
		}
	}
	for _, variable := range s.Variables {
		if seen[variable.Key] {
			continue
		}
		spec.Variables = append(spec.Variables, toVarSpec(variable))
	}
	return spec
}

// toVarSpec converts a live variable to its file form, omitting the default
// env_var type as the files do.
func toVarSpec(v gitlab.PipelineScheduleVariable) variableSpec {
	vs := variableSpec{Key: v.Key, Value: v.Value}
	if v.VariableType != "" && v.VariableType != "env_var" {
		vs.VariableType = v.VariableType
	}
	return vs
}

// diffSpec lists the field and variable differences from old to new, for the
// pull plan. Secret values are redacted like everywhere else.
func diffSpec(old, new *scheduleSpec) []string {
	var changes []string
	if shortRef(old.Ref) != shortRef(new.Ref) {
		changes = append(changes, fmt.Sprintf("    ~ ref: %q -> %q", old.Ref, new.Ref))
	}
	if old.Cron != new.Cron {
		changes = append(changes, fmt.Sprintf("    ~ cron: %q -> %q", old.Cron, new.Cron))
	}
	if old.CronTimezone != new.CronTimezone {
		changes = append(changes, fmt.Sprintf("    ~ cron_timezone: %q -> %q", old.CronTimezone, new.CronTimezone))
	}
	if old.active() != new.active() {
		changes = append(changes, fmt.Sprintf("    ~ active: %t -> %t", old.active(), new.active()))
	}

	oldVars := map[string]variableSpec{}
	for _, variable := range old.Variables {
		oldVars[variable.Key] = variable
	}
	for _, variable := range new.Variables {
		current, ok := oldVars[variable.Key]
		delete(oldVars, variable.Key)
		switch {
		case !ok:
			changes = append(changes, fmt.Sprintf("    + variable %s", variable.Key))
		case current.Value != variable.Value || current.variableType() != variable.variableType():
			changes = append(changes, fmt.Sprintf("    ~ variable %s: %q -> %q", variable.Key,
				redactValue(variable.Key, current.Value), redactValue(variable.Key, variable.Value)))
		}
	}
	var removed []string
	for key := range oldVars {
		removed = append(removed, key)
	}
	sort.Strings(removed)
	for _, key := range removed {
		changes = append(changes, fmt.Sprintf("    - variable %s", key))
	}
	return changes
}
