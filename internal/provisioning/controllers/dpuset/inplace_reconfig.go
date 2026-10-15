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

package dpuset

import (
	"context"
	"fmt"
	"sort"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// inPlaceReconfigTrue is the only annotation value that starts or continues a round.
const inPlaceReconfigTrue = "true"

// reconcileInPlaceReconfig runs one annotation round of in-place reconfig.
//
// The annotation must be exactly "true". A new round starts only while the
// previous one is inactive. Ready DPUs that were already Ready before startedAt
// take a slot from the same MaxUnavailableDPUNodes budget as disruptive deletes.
// Host Network, DPU Cluster Config, Service Readiness, Node Effect Removal, and
// Node Effect are already out of service, so they are marked without a new slot.
// A DPU that first becomes Ready during the round is stamped complete and is not
// jumped. The annotation is removed only after every member of this round has
// finished. Removing it early stops new marks and lets in-flight DPUs finish.
func (r *DPUSetReconciler) reconcileInPlaceReconfig(ctx context.Context, dpuSet *provisioningv1.DPUSet, totalDPUDevices int) error {
	logger := log.FromContext(ctx)
	requested := inPlaceReconfigRequested(dpuSet)
	if !requested && !inPlaceRoundActive(dpuSet) {
		return nil
	}

	wasActive := inPlaceRoundActive(dpuSet)
	ensureInPlaceRound(dpuSet, requested)
	if !wasActive && inPlaceRoundActive(dpuSet) {
		logger.Info("Started in-place reconfig round", "round", dpuSet.Status.DPUReconfig.Round)
	}
	round := dpuSet.Status.DPUReconfig
	// Fresh list: strategy may have deleted DPUs, and outdated was just written.
	dpuMap, err := r.GetDPUsMap(ctx, dpuSet)
	if err != nil {
		return fmt.Errorf("list DPUs for in-place reconfig: %w", err)
	}

	dpus := sortedDPUs(dpuMap)
	slots := inPlaceReadySlots(r.Options.MaxUnavailableDPUNodes, totalDPUDevices, dpus)
	// Complete and stamp before starting anyone, so a DPU marked in this pass is
	// not also completed in this pass, and a DPU that just returned to Ready frees a slot.
	freed := 0
	planned := make([]plannedReconfig, len(dpus))
	for i := range dpus {
		planned[i] = planInPlaceCompletion(dpus[i], round.Round, *round.StartedAt, requested)
		if completionFreedSlot(dpus[i], planned[i].next) {
			freed++
		}
	}
	slots += freed

	allowStart := requested
	for i := range dpus {
		if planned[i].next != nil {
			continue
		}
		var used int
		planned[i], used = planInPlaceStart(dpus[i], round.Round, *round.StartedAt, allowStart, slots)
		slots -= used
	}

	// Persist completions before any new inProgress mark. A conflict while clearing
	// the DPU that freed a slot must not leave the next DPU already marked.
	if err := r.patchInPlacePlans(ctx, planned, false); err != nil {
		return err
	}
	if err := r.patchInPlacePlans(ctx, planned, true); err != nil {
		return err
	}

	wasActiveAfter := inPlaceRoundActive(dpuSet)
	finishInPlaceRound(dpuSet, requested, planned)
	if wasActiveAfter && !inPlaceRoundActive(dpuSet) {
		logger.Info("Finished in-place reconfig round", "round", dpuSet.Status.DPUReconfig.Round)
	}
	return nil
}

// ensureInPlaceRound opens a round when the annotation is "true" and no round is
// active. An already active round is left at its current number.
func ensureInPlaceRound(dpuSet *provisioningv1.DPUSet, requested bool) {
	if inPlaceRoundActive(dpuSet) {
		if dpuSet.Status.DPUReconfig.StartedAt == nil {
			now := metav1.Now()
			dpuSet.Status.DPUReconfig.StartedAt = &now
		}
		return
	}
	if !requested {
		return
	}
	now := metav1.Now()
	if dpuSet.Status.DPUReconfig == nil {
		dpuSet.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{}
	}
	dpuSet.Status.DPUReconfig.Round++
	dpuSet.Status.DPUReconfig.Active = ptr.To(true)
	dpuSet.Status.DPUReconfig.StartedAt = &now
}

// finishInPlaceRound closes the round on the in-memory DPUSet. While the annotation
// is still "true", it is removed only after every member has finished. If the user
// removed it, or left another value, in-flight DPUs must finish first and the round
// number stays unchanged.
func finishInPlaceRound(dpuSet *provisioningv1.DPUSet, requested bool, planned []plannedReconfig) {
	if dpuSet.Status.DPUReconfig == nil {
		return
	}
	if requested {
		if !roundHasOutstandingMember(planned, dpuSet.Status.DPUReconfig.Round, *dpuSet.Status.DPUReconfig.StartedAt) {
			clearInPlaceAnnotation(dpuSet)
			dpuSet.Status.DPUReconfig.Active = ptr.To(false)
		}
		return
	}
	// The user removed the annotation, or left a value other than "true".
	// In-flight DPUs may finish; everyone else keeps the previous completedRound.
	if !roundHasInProgress(planned) {
		dpuSet.Status.DPUReconfig.Active = ptr.To(false)
	}
}

// inPlaceReconfigRequested reports whether the annotation value is exactly "true".
func inPlaceReconfigRequested(dpuSet *provisioningv1.DPUSet) bool {
	return dpuSet.Annotations[cutil.DPUSetInPlaceReconfigAnnotation] == inPlaceReconfigTrue
}

// inPlaceRoundActive reports whether a round is open. A nil status or a nil Active
// pointer means no round is in progress.
func inPlaceRoundActive(dpuSet *provisioningv1.DPUSet) bool {
	status := dpuSet.Status.DPUReconfig
	return status != nil && status.Active != nil && *status.Active
}

// clearInPlaceAnnotation removes the in-place annotation. An empty annotation map
// is dropped so the deferred DPUSet patch does not keep an empty object.
func clearInPlaceAnnotation(dpuSet *provisioningv1.DPUSet) {
	if len(dpuSet.Annotations) == 0 {
		return
	}
	delete(dpuSet.Annotations, cutil.DPUSetInPlaceReconfigAnnotation)
	if len(dpuSet.Annotations) == 0 {
		dpuSet.Annotations = nil
	}
}

// inPlaceReadySlots is the number of Ready DPUs that may be marked this pass.
// totalDPUDevices is the provisionable count shared with rolloutRolling, so a
// device this DPUSet will never own does not consume a slot. A missing CR among
// that count does. A DPU already marked inProgress still counts while its Ready
// condition is True, because the DPU controller has not taken it out of service yet.
func inPlaceReadySlots(maxUnavailable int32, totalDPUDevices int, dpus []provisioningv1.DPU) int {
	maxSlots := int(maxUnavailable)
	if maxSlots < 1 {
		maxSlots = 1
	} else if maxSlots > totalDPUDevices {
		maxSlots = totalDPUDevices
	}
	unavailable := totalDPUDevices - len(dpus)
	if unavailable < 0 {
		unavailable = 0
	}
	for i := range dpus {
		dpu := &dpus[i]
		if isUnavailable(dpu) || inProgressStillReady(dpu) {
			unavailable++
		}
	}
	if unavailable >= maxSlots {
		return 0
	}
	return maxSlots - unavailable
}

// inProgressStillReady reports a Ready DPU already marked for this round whose
// Ready condition is still True. It still occupies a MaxUnavailableDPUNodes slot.
func inProgressStillReady(dpu *provisioningv1.DPU) bool {
	return dpu.Status.Phase == provisioningv1.DPUReady &&
		reconfigInProgress(dpu.Status.DPUReconfig) &&
		!isUnavailable(dpu)
}

// plannedReconfig is one DPU and the reconfig status to write. A nil next means
// this pass does not change that DPU.
type plannedReconfig struct {
	dpu  provisioningv1.DPU
	next *provisioningv1.DPUReconfigStatus
}

// effective returns the reconfig status after this pass, falling back to the
// status already on the DPU when nothing was planned.
func (p plannedReconfig) effective() *provisioningv1.DPUReconfigStatus {
	if p.next != nil {
		return p.next
	}
	return p.dpu.Status.DPUReconfig
}

// planInPlaceCompletion records transitions that do not start a new jump:
// disruptive DPUs drop inProgress, a DPU that has returned to Ready finishes,
// and a DPU that first becomes Ready during the round is stamped complete.
// A nil next means this pass does not change status.
func planInPlaceCompletion(dpu provisioningv1.DPU, round int32, startedAt metav1.Time, stampFirstReady bool) plannedReconfig {
	plan := plannedReconfig{dpu: dpu}
	if !dpu.DeletionTimestamp.IsZero() || !agentSupportsInPlace(&dpu) {
		return plan
	}
	current := dpu.Status.DPUReconfig
	if dpu.Status.Outdated != nil {
		if reconfigInProgress(current) {
			next := cloneReconfig(current)
			next.InProgress = ptr.To(false)
			plan.next = next
		}
		return plan
	}
	if reconfigInProgress(current) && readyAtOrAfter(dpu, startedAt) {
		next := cloneReconfig(current)
		next.InProgress = ptr.To(false)
		next.CompletedRound = round
		plan.next = next
		return plan
	}
	// First Ready during an open round is recorded and not jumped. After the user
	// removes the annotation, leave completedRound unchanged for DPUs that never started.
	if stampFirstReady && !reconfigInProgress(current) && completedRound(current) < round && readyAtOrAfter(dpu, startedAt) {
		next := cloneReconfig(current)
		next.CompletedRound = round
		plan.next = next
	}
	return plan
}

// planInPlaceStart marks a DPU that still belongs to this round. The returned
// slot count is 1 only for a Ready DPU that was already Ready before startedAt.
func planInPlaceStart(dpu provisioningv1.DPU, round int32, startedAt metav1.Time, allowStart bool, slots int) (plannedReconfig, int) {
	plan := plannedReconfig{dpu: dpu}
	if !allowStart || !dpu.DeletionTimestamp.IsZero() || dpu.Status.Outdated != nil || !agentSupportsInPlace(&dpu) {
		return plan, 0
	}
	current := dpu.Status.DPUReconfig
	if reconfigInProgress(current) || completedRound(current) >= round || createdAfterRound(dpu, startedAt) {
		return plan, 0
	}
	if readyBeforeRound(dpu, startedAt) {
		if slots <= 0 {
			return plan, 0
		}
		next := cloneReconfig(current)
		next.InProgress = ptr.To(true)
		next.Round = round
		plan.next = next
		return plan, 1
	}
	if phaseJoinsWithoutNewSlot(dpu.Status.Phase) {
		next := cloneReconfig(current)
		next.InProgress = ptr.To(true)
		next.Round = round
		plan.next = next
	}
	return plan, 0
}

// completionFreedSlot reports whether finishing this DPU returns a Ready slot
// that another DPU in the same pass may use.
func completionFreedSlot(dpu provisioningv1.DPU, next *provisioningv1.DPUReconfigStatus) bool {
	if next == nil || reconfigInProgress(next) {
		return false
	}
	return inProgressStillReady(&dpu)
}

// patchInPlacePlans writes reconfig status. starts selects new inProgress marks.
// Completions, including a cleared inProgress and a completedRound stamp, are written
// when starts is false. A nil plan is skipped.
func (r *DPUSetReconciler) patchInPlacePlans(ctx context.Context, planned []plannedReconfig, starts bool) error {
	for i := range planned {
		if planned[i].next == nil || reconfigInProgress(planned[i].next) != starts {
			continue
		}
		if err := r.patchDPUReconfig(ctx, &planned[i]); err != nil {
			return err
		}
	}
	return nil
}

// patchDPUReconfig writes status.dpuReconfig with a merge patch. A missing DPU
// is ignored because strategy may have deleted it earlier in this reconcile.
// An unchanged plan has next cleared so later membership checks use the live status.
func (r *DPUSetReconciler) patchDPUReconfig(ctx context.Context, plan *plannedReconfig) error {
	if plan.next == nil || reconfigEqual(plan.dpu.Status.DPUReconfig, plan.next) {
		plan.next = nil
		return nil
	}
	logger := log.FromContext(ctx)
	base := plan.dpu.DeepCopy()
	patched := base.DeepCopy()
	patched.Status.DPUReconfig = plan.next
	err := r.Status().Patch(ctx, patched, client.MergeFrom(base), client.FieldOwner(DPUSetControllerName))
	switch {
	case err == nil:
		return nil
	case apierrors.IsNotFound(err):
		logger.V(2).Info("Skipping in-place reconfig status patch; DPU no longer exists",
			"DPU", fmt.Sprintf("%s/%s", plan.dpu.Namespace, plan.dpu.Name))
		plan.next = nil
		return nil
	default:
		return fmt.Errorf("patch DPU (%s/%s) in-place reconfig: %w", plan.dpu.Namespace, plan.dpu.Name, err)
	}
}

// roundHasOutstandingMember reports whether any owned DPU still has to finish
// this round before the annotation can be removed.
func roundHasOutstandingMember(planned []plannedReconfig, round int32, startedAt metav1.Time) bool {
	for i := range planned {
		if blocksRound(&planned[i].dpu, planned[i].effective(), round, startedAt) {
			return true
		}
	}
	return false
}

// roundHasInProgress reports whether a DPU that still exists is marked inProgress.
// Used when the annotation is no longer "true" and only in-flight DPUs keep the round open.
func roundHasInProgress(planned []plannedReconfig) bool {
	for i := range planned {
		if planned[i].dpu.DeletionTimestamp.IsZero() && reconfigInProgress(planned[i].effective()) {
			return true
		}
	}
	return false
}

// blocksRound reports whether this DPU must finish the round before the annotation
// is removed. Old agents, deleting DPUs, disruptive DPUs, and first-time installs
// are not members. A DPU created after the round started joins only by being
// stamped on its first Ready, which already sets completedRound.
func blocksRound(dpu *provisioningv1.DPU, reconfig *provisioningv1.DPUReconfigStatus, round int32, startedAt metav1.Time) bool {
	if !dpu.DeletionTimestamp.IsZero() || dpu.Status.Outdated != nil || !agentSupportsInPlace(dpu) {
		return false
	}
	if completedRound(reconfig) == round || reconfigInProgress(reconfig) {
		return reconfigInProgress(reconfig)
	}
	if createdAfterRound(*dpu, startedAt) {
		return false
	}
	if completedRound(reconfig) == 0 && !phaseJoinsRound(dpu.Status.Phase) {
		return false
	}
	return true
}

// agentSupportsInPlace reports whether the DPU agent set a non-empty version.
// An empty version is an old agent and is not a member of the round.
func agentSupportsInPlace(dpu *provisioningv1.DPU) bool {
	return dpu.Status.AgentStatus != nil && dpu.Status.AgentStatus.Version != ""
}

// phaseJoinsWithoutNewSlot reports phases that are already out of service, so
// marking them inProgress does not take another MaxUnavailableDPUNodes slot.
func phaseJoinsWithoutNewSlot(phase provisioningv1.DPUPhase) bool {
	switch phase {
	case provisioningv1.DPUHostNetworkConfiguration,
		provisioningv1.DPUClusterConfig,
		provisioningv1.DPUServiceReadiness,
		provisioningv1.DPUNodeEffectRemoval,
		provisioningv1.DPUNodeEffect:
		return true
	default:
		return false
	}
}

// phaseJoinsRound reports phases whose DPUs are members of an open round.
// DPU Config, Rebooting, and first-install phases are not.
func phaseJoinsRound(phase provisioningv1.DPUPhase) bool {
	return phase == provisioningv1.DPUReady || phaseJoinsWithoutNewSlot(phase)
}

// reconfigInProgress reports whether inProgress is explicitly true. Nil is not in progress.
func reconfigInProgress(status *provisioningv1.DPUReconfigStatus) bool {
	return status != nil && status.InProgress != nil && *status.InProgress
}

// completedRound returns the last finished round, or 0 when this DPU has never finished one.
func completedRound(status *provisioningv1.DPUReconfigStatus) int32 {
	if status == nil {
		return 0
	}
	return status.CompletedRound
}

// cloneReconfig copies status so a planned change does not mutate the object listed from the API.
func cloneReconfig(status *provisioningv1.DPUReconfigStatus) *provisioningv1.DPUReconfigStatus {
	if status == nil {
		return &provisioningv1.DPUReconfigStatus{}
	}
	cloned := *status
	if status.InProgress != nil {
		cloned.InProgress = ptr.To(*status.InProgress)
	}
	return &cloned
}

// reconfigEqual reports whether two reconfig statuses would write the same round fields.
func reconfigEqual(a, b *provisioningv1.DPUReconfigStatus) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Round == b.Round && a.CompletedRound == b.CompletedRound && ptr.Equal(a.InProgress, b.InProgress)
}

// sortedDPUs returns owned DPUs in name order so slot assignment is stable.
func sortedDPUs(dpuMap map[string]provisioningv1.DPU) []provisioningv1.DPU {
	dpus := make([]provisioningv1.DPU, 0, len(dpuMap))
	for _, dpu := range dpuMap {
		dpus = append(dpus, dpu)
	}
	sort.Slice(dpus, func(i, j int) bool {
		return dpus[i].Name < dpus[j].Name
	})
	return dpus
}

// readyConditionTime returns the Ready condition's last transition when phase is Ready
// and the condition is True.
func readyConditionTime(dpu provisioningv1.DPU) (metav1.Time, bool) {
	if dpu.Status.Phase != provisioningv1.DPUReady {
		return metav1.Time{}, false
	}
	_, cond := cutil.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondReady.String())
	if cond == nil || cond.Status != metav1.ConditionTrue {
		return metav1.Time{}, false
	}
	return cond.LastTransitionTime, true
}

// readyBeforeRound is a DPU that was already Ready when this round started.
// A zero transition time has no later observation, so it counts as already Ready.
func readyBeforeRound(dpu provisioningv1.DPU, startedAt metav1.Time) bool {
	transition, ready := readyConditionTime(dpu)
	if !ready {
		return false
	}
	return transition.IsZero() || transition.Time.Before(startedAt.Time)
}

// readyAtOrAfter is the first Ready of this round, or a return to Ready after a jump.
func readyAtOrAfter(dpu provisioningv1.DPU, startedAt metav1.Time) bool {
	transition, ready := readyConditionTime(dpu)
	if !ready || transition.IsZero() {
		return false
	}
	return !transition.Time.Before(startedAt.Time)
}

// createdAfterRound reports a DPU that did not exist when this round started.
// It joins the round only by being stamped on its first Ready.
func createdAfterRound(dpu provisioningv1.DPU, startedAt metav1.Time) bool {
	return !dpu.CreationTimestamp.IsZero() && dpu.CreationTimestamp.After(startedAt.Time)
}
