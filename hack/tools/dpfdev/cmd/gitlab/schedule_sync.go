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

// Plan exit codes reported with --exit-code. They let CI treat drift as a
// warning but a new schedule as a hard failure (see the schedule-plan job).
const (
	planExitInSync = 0 // no changes
	planExitDrift  = 2 // updates or prunes to existing schedules
	planExitCreate = 3 // a schedule in the file does not exist in GitLab yet
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
		"In plan mode, exit 2 when existing schedules would change and 3 when the file adds a new schedule (0 when in sync)")
	scheduleSyncCmd.Flags().IntVar(&scheduleSyncConcurrency, "concurrency", defaultSyncConcurrency, "Number of schedule detail fetches to run in parallel")
}

// planExitCode maps a completed plan to its --exit-code value. A new schedule
// (create) outranks drift, because it needs a manual local create so its id
// can be written back to the file before merging.
func planExitCode(s *scheduleSyncer) int {
	switch {
	case s.created > 0:
		return planExitCreate
	case s.updated > 0 || s.deleted > 0:
		return planExitDrift
	default:
		return planExitInSync
	}
}

var scheduleSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the scheduled pipelines file to GitLab",
	Long: `Sync the scheduled pipelines file to GitLab: create schedules that are
missing, update drifted fields and variables, and delete variables that were
removed from the file. Schedules are matched by their GitLab id when the file
carries one, falling back to the description for schedules that do not yet
have an id.

The file may split its schedules across several files with an "include" list
(paths relative to the including file), so the schedules can be organised e.g.
per branch. --file points at the single root; everything it includes is
merged before syncing, and descriptions must be unique across all of them.

Without --apply only the plan is printed and nothing is changed in GitLab.

Schedules that exist in GitLab but not in the file are reported as unmanaged;
--prune deletes them instead.

Modifying a schedule owned by another user requires taking it over (GitLab
only lets the owner's token update a schedule, and ownership cannot be given
away, only taken). Sync takes ownership whenever an update requires it and
reports every transfer, so run it with the token that should end up owning
the schedules, e.g. the CI bot account.`,
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
		// drift as a warning and a new schedule as a failure.
		if scheduleSyncExitCode && !scheduleSyncApply {
			switch planExitCode(syncer) {
			case planExitCreate:
				fmt.Println("\nThe file adds a schedule that does not exist in GitLab yet.")
				fmt.Println("Create it locally with 'dpfdev gitlab schedule sync --apply', then add its")
				fmt.Println("id (from 'dpfdev gitlab schedule list') to the YAML before merging.")
				os.Exit(planExitCreate)
			case planExitDrift:
				fmt.Println("\nThis will change existing schedules when merged.")
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
	// The file already carries the ids it manages, so their details (the slow,
	// per-schedule part) are fetched concurrently with the list call rather
	// than after it. The list is only needed afterwards, to find schedules
	// that exist in GitLab but not in the file.
	var fileIDs []int
	for i := range file.Schedules {
		if file.Schedules[i].ID != 0 {
			fileIDs = append(fileIDs, file.Schedules[i].ID)
		}
	}

	progress("Fetching %d schedules from GitLab...", len(file.Schedules))

	var (
		existing   []gitlab.PipelineSchedule
		listErr    error
		details    map[int]*gitlab.PipelineSchedule
		detailsErr error
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		slog.Debug("listing pipeline schedules")
		existing, listErr = s.client.ListPipelineSchedules()
		slog.Debug("listed pipeline schedules", "count", len(existing))
	}()
	go func() {
		defer wg.Done()
		details, detailsErr = s.fetchDetails(fileIDs)
	}()
	wg.Wait()
	if listErr != nil {
		return fmt.Errorf("failed to fetch pipeline schedules: %v", listErr)
	}
	if detailsErr != nil {
		return detailsErr
	}
	slog.Debug("fetched schedules from gitlab", "existing", len(existing), "prefetched", len(details), "file", len(file.Schedules))

	byID := map[int]gitlab.PipelineSchedule{}
	byDescription := map[string][]gitlab.PipelineSchedule{}
	for _, schedule := range existing {
		byID[schedule.ID] = schedule
		byDescription[schedule.Description] = append(byDescription[schedule.Description], schedule)
	}
	// consumed marks GitLab schedules claimed by a spec, so the leftovers can
	// be pruned and a description fallback never re-matches an id-matched one.
	consumed := map[int]bool{}

	// Pass 1: resolve every spec to a create or an update, matching by id
	// first and description second.
	var actions []plannedAction
	for i := range file.Schedules {
		spec := &file.Schedules[i]

		// Prefer the stable ID: a schedule matched by ID can be renamed in the
		// file and sync updates it in place instead of deleting and recreating.
		if spec.ID != 0 {
			schedule, ok := byID[spec.ID]
			if !ok {
				return fmt.Errorf("schedule id %d (%q) is in the file but not in GitLab; "+
					"remove the id to create it, or fix the id", spec.ID, spec.Description)
			}
			slog.Debug("matched schedule by id", "id", spec.ID, "description", spec.Description)
			consumed[spec.ID] = true
			actions = append(actions, plannedAction{spec: spec, scheduleID: schedule.ID})
			continue
		}

		// No ID yet (a hand-authored schedule): fall back to description,
		// ignoring GitLab schedules already claimed by an ID.
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
			return fmt.Errorf("schedule description %q exists %d times in GitLab; add an id to the file to disambiguate, or delete the duplicates", spec.Description, len(matches))
		}
	}

	// Details for id-matched schedules were prefetched alongside the list.
	// Description-matched schedules had no id to prefetch, so fetch them now
	// (usually none, once ids are backfilled into the file).
	var missingIDs []int
	for _, a := range actions {
		if !a.create {
			if _, ok := details[a.scheduleID]; !ok {
				missingIDs = append(missingIDs, a.scheduleID)
			}
		}
	}
	if len(missingIDs) > 0 {
		more, err := s.fetchDetails(missingIDs)
		if err != nil {
			return err
		}
		for id, d := range more {
			details[id] = d
		}
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
			// The list call returned this id but its detail fetch 404'd (both
			// on prefetch and re-fetch): the schedule was deleted between the
			// two calls. Report it cleanly instead of dereferencing nil.
			return fmt.Errorf("schedule id %d (%q) disappeared from GitLab during sync; re-run", a.scheduleID, a.spec.Description)
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

	// Changing both the ref and the description of an id-matched schedule is
	// the signature of an accidental copy: a group file was duplicated to seed
	// a new branch without stripping the ids, so this would overwrite the
	// original schedule instead of creating a new one.
	if spec.Description != schedule.Description && shortRef(spec.Ref) != shortRef(schedule.Ref) {
		fmt.Printf("! warning: schedule id %d changes both ref (%s -> %s) and description (%q -> %q).\n",
			scheduleID, schedule.Ref, spec.Ref, schedule.Description, spec.Description)
		fmt.Printf("  If you copied this schedule to seed a new branch, remove its id so a new\n")
		fmt.Printf("  schedule is created instead of overwriting id %d.\n", scheduleID)
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
		if err := s.withOwnership(scheduleID, func() error {
			return s.client.UpdatePipelineSchedule(scheduleID, spec.Description, spec.Ref, spec.Cron, timezone, spec.active())
		}); err != nil {
			return fmt.Errorf("failed to update schedule %q: %v", spec.Description, err)
		}
	}
	for _, variable := range createVars {
		slog.Debug("creating variable", "id", scheduleID, "key", variable.Key)
		if err := s.withOwnership(scheduleID, func() error {
			return s.client.CreatePipelineScheduleVariable(scheduleID, variable.Key, variable.Value, variable.variableType())
		}); err != nil {
			return fmt.Errorf("failed to create variable %s on schedule %q: %v", variable.Key, spec.Description, err)
		}
	}
	for _, variable := range updateVars {
		slog.Debug("updating variable", "id", scheduleID, "key", variable.Key)
		if err := s.withOwnership(scheduleID, func() error {
			return s.client.UpdatePipelineScheduleVariable(scheduleID, variable.Key, variable.Value, variable.variableType())
		}); err != nil {
			return fmt.Errorf("failed to update variable %s on schedule %q: %v", variable.Key, spec.Description, err)
		}
	}
	for _, key := range deleteVars {
		slog.Debug("deleting variable", "id", scheduleID, "key", key)
		if err := s.withOwnership(scheduleID, func() error {
			return s.client.DeletePipelineScheduleVariable(scheduleID, key)
		}); err != nil {
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

// withOwnership runs a schedule modification, taking ownership of the
// schedule and retrying once if GitLab rejects it for a schedule the token
// does not own.
func (s *scheduleSyncer) withOwnership(scheduleID int, modify func() error) error {
	err := modify()
	if !gitlab.IsForbidden(err) {
		return err
	}
	slog.Debug("modification forbidden, taking ownership", "id", scheduleID)
	if err := takeScheduleOwnership(s.client, scheduleID); err != nil {
		return err
	}
	return modify()
}

// takeScheduleOwnership takes over the schedule and reports the transfer,
// including the previous owner. Taking ownership is one-way: GitLab has no
// API to assign ownership to another user, so the previous owner has to take
// the schedule back themselves, and until then it runs with the new owner's
// permissions. The schedule is fetched first because the take-ownership
// response only carries the new owner.
func takeScheduleOwnership(client *gitlab.Client, scheduleID int) error {
	previous, err := client.GetPipelineSchedule(scheduleID)
	if err != nil {
		return fmt.Errorf("failed to fetch pipeline schedule %d before taking ownership: %v", scheduleID, err)
	}

	schedule, err := client.TakeOwnershipPipelineSchedule(scheduleID)
	if err != nil {
		return fmt.Errorf("failed to take ownership of schedule %d: %v", scheduleID, err)
	}

	fmt.Printf("⚠ Took ownership of schedule %d (previous owner: %s, new owner: %s).\n",
		scheduleID, previous.Owner.Username, schedule.Owner.Username)
	fmt.Printf("  The schedule now runs with %s's permissions. GitLab cannot transfer\n", schedule.Owner.Username)
	fmt.Printf("  ownership back; ask %s to take ownership again if needed.\n", previous.Owner.Username)
	return nil
}
