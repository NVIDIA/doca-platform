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
	"log/slog"
	"os"
	"sync"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

// defaultSyncConcurrency bounds the concurrent schedule-detail fetches. Sync's
// runtime is dominated by one GET per schedule, so these run in parallel.
const defaultSyncConcurrency = 8

// progress prints a human status line to stderr (keeping stdout for the plan).
// It is suppressed under --debug, where the structured logs cover the same
// ground in more detail.
func progress(format string, args ...any) {
	if debugEnabled {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// Plan exit codes reported with --exit-code.
const (
	planExitInSync = 0 // no changes
	planExitDrift  = 2 // any changes (creates, updates, deletes)
)

var (
	scheduleSyncFile        string
	scheduleSyncApply       bool
	scheduleSyncPrune       bool
	scheduleSyncExitCode    bool
	scheduleSyncConcurrency int
)

func init() {
	scheduleCmd.AddCommand(scheduleSyncCmd)

	scheduleSyncCmd.Flags().StringVar(&scheduleSyncFile, "file", defaultScheduleFile, "Root scheduled pipelines file to apply (may pull in others via include)")
	scheduleSyncCmd.Flags().BoolVar(&scheduleSyncApply, "apply", false, "Apply the changes to GitLab; without this flag sync only prints the plan")
	scheduleSyncCmd.Flags().BoolVar(&scheduleSyncPrune, "prune", false, "Delete GitLab schedules that are not in the file (default: warn only)")
	scheduleSyncCmd.Flags().BoolVar(&scheduleSyncExitCode, "exit-code", false,
		"In plan mode, exit 2 when any schedules would change (0 when in sync)")
	scheduleSyncCmd.Flags().IntVar(&scheduleSyncConcurrency, "concurrency", defaultSyncConcurrency, "Number of schedule detail fetches to run in parallel")
}

// planExitCode maps a completed plan to its --exit-code value.
func planExitCode(s *scheduleSyncer) int {
	if s.created > 0 || s.updated > 0 || s.deleted > 0 {
		return planExitDrift
	}
	return planExitInSync
}

var scheduleSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the scheduled pipelines file to GitLab",
	Long: `Sync the scheduled pipelines file to GitLab: create schedules that are
missing, update drifted fields and variables, and delete variables that were
removed from the file. Schedules are matched by description.

The file may split its schedules across several files with an "include" list
(paths relative to the including file), so the schedules can be organised e.g.
per branch. --file points at the single root; everything it includes is
merged before syncing, and descriptions must be unique across all of them.

Without --apply only the plan is printed and nothing is changed in GitLab.

Schedules that exist in GitLab but not in the file are reported as unmanaged;
--prune deletes them instead.`,
	Example: `  # Show what would change (the default; nothing is modified)
  dpfdev gitlab schedule sync

  # Apply the file
  dpfdev gitlab schedule sync --apply

  # Apply and delete schedules that are not in the file
  dpfdev gitlab schedule sync --apply --prune`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		file, err := loadScheduleFile(scheduleSyncFile)
		if err != nil {
			return err
		}

		client, err := newGitLabClient(cmd)
		if err != nil {
			return err
		}

		syncer := &scheduleSyncer{client: client, dryRun: !scheduleSyncApply, prune: scheduleSyncPrune, concurrency: scheduleSyncConcurrency}
		if err := syncer.sync(file); err != nil {
			return err
		}

		// In plan mode, translate the plan into an exit code so CI can flag
		// any pending changes as a warning.
		if scheduleSyncExitCode && !scheduleSyncApply {
			if planExitCode(syncer) == planExitDrift {
				fmt.Println("\nThis will change schedules in GitLab when merged.")
				os.Exit(planExitDrift)
			}
		}
		return nil
	},
}

// scheduleSyncer applies a scheduleFile to GitLab.
type scheduleSyncer struct {
	client      *gitlab.Client
	dryRun      bool
	prune       bool
	concurrency int

	created, updated, deleted, unmanaged int
}

// plannedAction is the resolved outcome of matching one file spec to GitLab:
// either create the schedule, or update the GitLab schedule with scheduleID.
type plannedAction struct {
	spec       *scheduleSpec
	scheduleID int
	create     bool
}

func (s *scheduleSyncer) sync(file *scheduleFile) error {
	progress("Fetching schedules from GitLab...")
	slog.Debug("listing pipeline schedules")
	existing, err := s.client.ListPipelineSchedules()
	if err != nil {
		return fmt.Errorf("failed to fetch pipeline schedules: %v", err)
	}
	slog.Debug("listed pipeline schedules", "count", len(existing), "file", len(file.Schedules))

	byDescription := map[string][]gitlab.PipelineSchedule{}
	for _, schedule := range existing {
		byDescription[schedule.Description] = append(byDescription[schedule.Description], schedule)
	}
	// consumed marks GitLab schedules claimed by a spec so the leftovers can
	// be pruned and a description never matches two specs.
	consumed := map[int]bool{}

	// Pass 1: resolve every spec to a create or an update, matching by description.
	var actions []plannedAction
	for i := range file.Schedules {
		spec := &file.Schedules[i]
		var matches []gitlab.PipelineSchedule
		for _, schedule := range byDescription[spec.Description] {
			if !consumed[schedule.ID] {
				matches = append(matches, schedule)
			}
		}
		switch len(matches) {
		case 0:
			slog.Debug("no gitlab match, will create", "description", spec.Description)
			actions = append(actions, plannedAction{spec: spec, create: true})
		case 1:
			slog.Debug("matched schedule by description", "description", spec.Description, "id", matches[0].ID)
			consumed[matches[0].ID] = true
			actions = append(actions, plannedAction{spec: spec, scheduleID: matches[0].ID})
		default:
			return fmt.Errorf("schedule description %q exists %d times in GitLab; delete the duplicates", spec.Description, len(matches))
		}
	}

	// Fetch full details (including variables) for all matched schedules
	// concurrently — this is the slow part, one GET per schedule.
	var matchedIDs []int
	for _, a := range actions {
		if !a.create {
			matchedIDs = append(matchedIDs, a.scheduleID)
		}
	}
	details, err := s.fetchDetails(matchedIDs)
	if err != nil {
		return err
	}

	// Pass 2: apply each action in file order, so output stays deterministic.
	for _, a := range actions {
		if a.create {
			if err := s.create(a.spec); err != nil {
				return err
			}
			continue
		}
		schedule := details[a.scheduleID]
		if schedule == nil {
			return fmt.Errorf("schedule %q disappeared from GitLab during sync; re-run", a.spec.Description)
		}
		if err := s.update(schedule, a.spec); err != nil {
			return err
		}
	}

	// Anything not claimed by a spec exists in GitLab but not in the file.
	for _, schedule := range existing {
		if !consumed[schedule.ID] {
			if err := s.pruneOrWarn(schedule); err != nil {
				return err
			}
		}
	}

	action := "applied"
	if s.dryRun {
		action = "planned (not applied; use --apply)"
	}
	fmt.Printf("Sync %s: %d created, %d updated, %d deleted, %d unmanaged\n",
		action, s.created, s.updated, s.deleted, s.unmanaged)
	return nil
}

func (s *scheduleSyncer) create(spec *scheduleSpec) error {
	s.created++
	fmt.Printf("+ create schedule %q (ref %s, cron %q, active %t)\n", spec.Description, spec.Ref, spec.Cron, spec.active())
	for _, variable := range spec.Variables {
		fmt.Printf("  + variable %s\n", variable.Key)
	}
	if s.dryRun {
		return nil
	}

	schedule, err := s.client.CreatePipelineSchedule(spec.Description, spec.Ref, spec.Cron, spec.CronTimezone, spec.active())
	if err != nil {
		return fmt.Errorf("failed to create schedule %q: %v", spec.Description, err)
	}
	slog.Debug("created schedule", "id", schedule.ID, "description", spec.Description)
	for _, variable := range spec.Variables {
		slog.Debug("creating variable", "id", schedule.ID, "key", variable.Key)
		if err := s.client.CreatePipelineScheduleVariable(schedule.ID, variable.Key, variable.Value, variable.variableType()); err != nil {
			return fmt.Errorf("failed to create variable %s on schedule %q: %v", variable.Key, spec.Description, err)
		}
	}
	return nil
}

// fetchDetails fetches the full schedule (including variables) for each id,
// bounded by the syncer's concurrency. This is the slow part of a sync, one
// request per schedule, so the requests run in parallel.
func (s *scheduleSyncer) fetchDetails(ids []int) (map[int]*gitlab.PipelineSchedule, error) {
	workers := s.concurrency
	if workers < 1 {
		workers = 1
	}
	slog.Debug("fetching schedule details", "count", len(ids), "concurrency", workers)

	details := make(map[int]*gitlab.PipelineSchedule, len(ids))
	var mu sync.Mutex
	var firstErr error
	var once sync.Once

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			schedule, err := s.client.GetPipelineSchedule(id)
			if err != nil {
				// An unknown id (prefetched from the file) 404s here; leave it
				// out so the caller's byID check reports it more clearly.
				if gitlab.IsNotFound(err) {
					return
				}
				once.Do(func() { firstErr = fmt.Errorf("failed to fetch pipeline schedule %d: %v", id, err) })
				return
			}
			// Logged per completion so progress streams during the fetch.
			slog.Debug("fetched schedule detail", "id", id, "description", schedule.Description, "variables", len(schedule.Variables))
			mu.Lock()
			details[id] = schedule
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return details, firstErr
}

func (s *scheduleSyncer) update(schedule *gitlab.PipelineSchedule, spec *scheduleSpec) error {
	scheduleID := schedule.ID
	if schedule.Variables == nil {
		return fmt.Errorf("GitLab did not return the variables of schedule %d (%s): the token needs the Maintainer role", scheduleID, spec.Description)
	}

	var changes []string
	if spec.Description != schedule.Description {
		changes = append(changes, fmt.Sprintf("  ~ description: %q -> %q", schedule.Description, spec.Description))
	}
	if shortRef(spec.Ref) != shortRef(schedule.Ref) {
		changes = append(changes, fmt.Sprintf("  ~ ref: %q -> %q", schedule.Ref, spec.Ref))
	}
	if spec.Cron != schedule.Cron {
		changes = append(changes, fmt.Sprintf("  ~ cron: %q -> %q", schedule.Cron, spec.Cron))
	}
	if spec.CronTimezone != "" && spec.CronTimezone != schedule.CronTimezone {
		changes = append(changes, fmt.Sprintf("  ~ cron_timezone: %q -> %q", schedule.CronTimezone, spec.CronTimezone))
	}
	if spec.active() != schedule.Active {
		changes = append(changes, fmt.Sprintf("  ~ active: %t -> %t", schedule.Active, spec.active()))
	}

	existingVars := map[string]gitlab.PipelineScheduleVariable{}
	for _, variable := range schedule.Variables {
		existingVars[variable.Key] = variable
	}

	var createVars, updateVars []variableSpec
	for _, variable := range spec.Variables {
		current, ok := existingVars[variable.Key]
		delete(existingVars, variable.Key)
		switch {
		case !ok:
			createVars = append(createVars, variable)
			changes = append(changes, fmt.Sprintf("  + variable %s", variable.Key))
		case current.Value != variable.Value || current.VariableType != variable.variableType():
			updateVars = append(updateVars, variable)
			changes = append(changes, fmt.Sprintf("  ~ variable %s: %q -> %q", variable.Key,
				redactValue(variable.Key, current.Value), redactValue(variable.Key, variable.Value)))
		}
	}
	var deleteVars []string
	for key := range existingVars {
		deleteVars = append(deleteVars, key)
		changes = append(changes, fmt.Sprintf("  - variable %s", key))
	}

	if len(changes) == 0 {
		return nil
	}

	s.updated++
	fmt.Printf("~ update schedule %q (%d)\n", spec.Description, scheduleID)
	for _, change := range changes {
		fmt.Println(change)
	}
	if s.dryRun {
		return nil
	}

	if spec.Description != schedule.Description || shortRef(spec.Ref) != shortRef(schedule.Ref) || spec.Cron != schedule.Cron ||
		(spec.CronTimezone != "" && spec.CronTimezone != schedule.CronTimezone) || spec.active() != schedule.Active {
		timezone := spec.CronTimezone
		if timezone == "" {
			timezone = schedule.CronTimezone
		}
		slog.Debug("updating schedule fields", "id", scheduleID)
		if err := s.client.UpdatePipelineSchedule(scheduleID, spec.Description, spec.Ref, spec.Cron, timezone, spec.active()); err != nil {
			return fmt.Errorf("failed to update schedule %q: %v", spec.Description, err)
		}
	}
	for _, variable := range createVars {
		slog.Debug("creating variable", "id", scheduleID, "key", variable.Key)
		if err := s.client.CreatePipelineScheduleVariable(scheduleID, variable.Key, variable.Value, variable.variableType()); err != nil {
			return fmt.Errorf("failed to create variable %s on schedule %q: %v", variable.Key, spec.Description, err)
		}
	}
	for _, variable := range updateVars {
		slog.Debug("updating variable", "id", scheduleID, "key", variable.Key)
		if err := s.client.UpdatePipelineScheduleVariable(scheduleID, variable.Key, variable.Value, variable.variableType()); err != nil {
			return fmt.Errorf("failed to update variable %s on schedule %q: %v", variable.Key, spec.Description, err)
		}
	}
	for _, key := range deleteVars {
		slog.Debug("deleting variable", "id", scheduleID, "key", key)
		if err := s.client.DeletePipelineScheduleVariable(scheduleID, key); err != nil {
			return fmt.Errorf("failed to delete variable %s on schedule %q: %v", key, spec.Description, err)
		}
	}
	return nil
}

func (s *scheduleSyncer) pruneOrWarn(schedule gitlab.PipelineSchedule) error {
	if !s.prune {
		s.unmanaged++
		fmt.Printf("! schedule %q (%d, owner %s) is not in the file; use --prune to delete it\n",
			schedule.Description, schedule.ID, schedule.Owner.Username)
		return nil
	}

	s.deleted++
	fmt.Printf("- delete schedule %q (%d, owner %s)\n", schedule.Description, schedule.ID, schedule.Owner.Username)
	if s.dryRun {
		return nil
	}
	if err := s.client.DeletePipelineSchedule(schedule.ID); err != nil {
		return fmt.Errorf("failed to delete schedule %q: %v", schedule.Description, err)
	}
	return nil
}

