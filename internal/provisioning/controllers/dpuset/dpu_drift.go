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
	"fmt"
	"reflect"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

// dpuDriftReason enumerates the spec fields that can diverge between a DPU and
// its owning DPUSet's DPUTemplate. The constants are listed in fixed precedence
// order; computeDPUDrift always returns reasons in this order so callers get
// stable, deterministic output.
type dpuDriftReason string

const (
	driftReasonBFB               dpuDriftReason = "BFB"
	driftReasonDPUFlavor         dpuDriftReason = "DPUFlavor"
	driftReasonSecureBoot        dpuDriftReason = "SecureBoot"
	driftReasonBlueFieldSoftware dpuDriftReason = "BlueFieldSoftware"
	driftReasonClusterSelector   dpuDriftReason = "ClusterSelector"
)

// dpuDrift records every field that has diverged between a DPU and its owning
// DPUSet's DPUTemplate. Reasons and Diffs are index-aligned: Reasons[i] corresponds
// to Diffs[i].
type dpuDrift struct {
	Reasons []dpuDriftReason
	Diffs   []string // human-readable, e.g. "BFB: bfb-v1 -> bfb-v2"
}

// empty reports whether no drift was detected.
func (d dpuDrift) empty() bool { return len(d.Reasons) == 0 }

// without returns a copy with the given reasons removed. Used to skip
// driftReasonClusterSelector when computing user-actionable outdated state,
// since the DPUSet controller's onDelete() path handles that case automatically.
func (d dpuDrift) without(exclude ...dpuDriftReason) dpuDrift {
	if len(exclude) == 0 {
		return d
	}
	skip := make(map[dpuDriftReason]struct{}, len(exclude))
	for _, r := range exclude {
		skip[r] = struct{}{}
	}
	var out dpuDrift
	for i, r := range d.Reasons {
		if _, drop := skip[r]; drop {
			continue
		}
		out.Reasons = append(out.Reasons, r)
		out.Diffs = append(out.Diffs, d.Diffs[i])
	}
	return out
}

// outdatedReasonForDrift maps each (non-cluster-selector) drift reason to the
// reason string used in dpu.status.outdated.reason.
var outdatedReasonForDrift = map[dpuDriftReason]string{
	driftReasonBFB:               provisioningv1.DPUOutdatedReasonBFB,
	driftReasonDPUFlavor:         provisioningv1.DPUOutdatedReasonDPUFlavor,
	driftReasonSecureBoot:        provisioningv1.DPUOutdatedReasonSecureBoot,
	driftReasonBlueFieldSoftware: provisioningv1.DPUOutdatedReasonBlueFieldSoftware,
}

// computeDPUDrift returns every drift reason between the DPU and its owning
// DPUSet's DPUTemplate. dpuClusters is used only to evaluate the ClusterSelector
// reason; passing nil/empty effectively skips that check.
func (r *DPUSetReconciler) computeDPUDrift(dpuSet provisioningv1.DPUSet, dpu provisioningv1.DPU, dpuClusters []provisioningv1.DPUCluster) dpuDrift {
	var d dpuDrift
	t := dpuSet.Spec.DPUTemplate.Spec
	s := dpu.Spec

	if t.BFB != nil && t.BFB.Name != s.BFB {
		d.Reasons = append(d.Reasons, driftReasonBFB)
		d.Diffs = append(d.Diffs, fmt.Sprintf("BFB: %s -> %s", s.BFB, t.BFB.Name))
	}
	if t.DPUFlavor != s.DPUFlavor {
		d.Reasons = append(d.Reasons, driftReasonDPUFlavor)
		d.Diffs = append(d.Diffs, fmt.Sprintf("DPUFlavor: %s -> %s", s.DPUFlavor, t.DPUFlavor))
	}
	if !reflect.DeepEqual(t.SecureBoot, s.SecureBoot) {
		d.Reasons = append(d.Reasons, driftReasonSecureBoot)
		d.Diffs = append(d.Diffs, fmt.Sprintf("SecureBoot: %s -> %s", formatBoolPtr(s.SecureBoot), formatBoolPtr(t.SecureBoot)))
	}
	if t.BlueFieldSoftware != nil && t.BlueFieldSoftware.Name != s.BlueFieldSoftware {
		d.Reasons = append(d.Reasons, driftReasonBlueFieldSoftware)
		d.Diffs = append(d.Diffs, fmt.Sprintf("BlueFieldSoftware: %s -> %s", s.BlueFieldSoftware, t.BlueFieldSoftware.Name))
	}

	if t.Cluster != nil && !matchDPUClusterSelector(t.Cluster.Selector, s.Cluster, dpuClusters) {
		d.Reasons = append(d.Reasons, driftReasonClusterSelector)
		d.Diffs = append(d.Diffs, "ClusterSelector: matched -> not matched")
	}
	return d
}

// formatBoolPtr renders a *bool as "<nil>" or strconv.FormatBool(*b) for human-readable diffs.
func formatBoolPtr(b *bool) string {
	if b == nil {
		return "<nil>"
	}
	return strconv.FormatBool(*b)
}

// detectOutdated reports whether the DPU has drifted from its owning DPUSet's
// template in a way that requires the DPU to be reprovisioned.
//
// Returns true when at least one of BFB / DPUFlavor / SecureBoot /
// BlueFieldSoftware has diverged. The result is strategy-agnostic — the same
// signal is produced under OnDelete and RollingUpdate. Cluster-selector
// mismatches are intentionally excluded because the DPUSet controller's
// reconcile path handles them automatically under either strategy. When
// multiple fields have diverged, reason reports the first in fixed precedence
// order (BFB -> DPUFlavor -> SecureBoot -> BlueFieldSoftware) and message lists
// every detected mismatch. The returned values populate dpu.status.outdated.
func (r *DPUSetReconciler) detectOutdated(dpuSet provisioningv1.DPUSet, dpu provisioningv1.DPU, dpuClusters []provisioningv1.DPUCluster) (outdated bool, reason string, message string) {
	drift := r.computeDPUDrift(dpuSet, dpu, dpuClusters).without(driftReasonClusterSelector)
	if drift.empty() {
		return false, "", ""
	}
	primary, ok := outdatedReasonForDrift[drift.Reasons[0]]
	if !ok {
		// Defensive guard — ClusterSelector already filtered, map covers all remaining reasons.
		return false, "", ""
	}
	msg := fmt.Sprintf("DPU template has changed (%s).", strings.Join(drift.Diffs, ", "))
	return true, primary, msg
}
