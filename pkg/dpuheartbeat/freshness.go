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

// Package dpuheartbeat provides the canonical freshness check for the DPU Agent's SPIFFE
// heartbeat. Freshness reads the agent-written DPU.Status.AgentStatus.Spiffe.LastProbeTime
// and compares it against the consumer's clock, with a small future-timestamp guard that
// bounds -- but does not eliminate -- how a clock-ahead DPU can mask a stale heartbeat: a
// timestamp more than skewGuard in the consumer's future is reported Stale. The
// management/consumer clock is authoritative (NTP), and freshness is observability only (the
// kube-apiserver remains the real policy enforcement point), so a bounded wrong answer grants
// no access.
//
// The consumer-facing contract is the Freshness function alone. The underlying data source
// (currently LastProbeTime) can be hardened later -- e.g. swapped for a managedFields-derived
// timestamp or a monotonic counter delta -- without changing the signature or breaking consumers.
package dpuheartbeat

import (
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

// HeartbeatFieldManager is the Server-Side-Apply field-manager name the DPU Agent uses when
// patching its heartbeat status fields. It is deliberately distinct from the agent's other
// status writers so heartbeat ownership stays isolated under SSA. Note: Freshness no longer
// keys off this manager's managedFields timestamp -- it reads LastProbeTime directly -- but the
// constant is retained as the agreed writer field-manager name for the heartbeat Apply.
const HeartbeatFieldManager = "dpuagent-heartbeat"

// skewGuard is the consumer-side tolerance for a LastProbeTime that sits in the consumer's
// future. Healthy NTP keeps DPU/management clock offset sub-millisecond; a few seconds absorbs
// normal jitter. A LastProbeTime more than skewGuard ahead of now is treated as provable DPU
// clock skew and reported Stale so it can never mask a missing heartbeat.
const skewGuard = 5 * time.Second

// FreshnessState reports the DPU Agent's heartbeat liveness as a tri-state string enum.
// String backing matches Kubernetes API enum conventions and prevents silent aliasing of
// invalid numeric values. Consumers MUST distinguish NeverAttested (a legitimate
// Initializing-phase state -- do NOT alarm) from Stale (a failure -- alarm).
type FreshnessState string

const (
	// NeverAttested means the agent has not yet recorded a heartbeat (nil DPU, missing
	// AgentStatus/Spiffe, or an unset/zero LastProbeTime).
	NeverAttested FreshnessState = "NeverAttested"
	// Fresh means the last heartbeat timestamp is within ttl (inclusive boundary).
	Fresh FreshnessState = "Fresh"
	// Stale means the last heartbeat timestamp is older than ttl, dated implausibly far in the
	// future (DPU clock skew), or ttl is non-positive.
	Stale FreshnessState = "Stale"
)

// Freshness returns the DPU Agent's heartbeat liveness state from
// dpu.Status.AgentStatus.Spiffe.LastProbeTime, relative to now and the freshness ttl.
//
// Evaluation order (guards first, fail safe, never panics):
//   - nil dpu / nil AgentStatus / nil Spiffe / nil or zero LastProbeTime -> NeverAttested
//   - ttl <= 0                                                            -> Stale (a non-positive window can never be Fresh; checked before the elapsed comparison)
//   - LastProbeTime later than now+skewGuard                              -> Stale (DPU clock ahead; never mask a stale heartbeat)
//   - now - LastProbeTime <= ttl                                         -> Fresh (inclusive boundary; a small future skew within [now, now+skewGuard] is intentionally accepted as Fresh)
//   - otherwise                                                          -> Stale
//
// Masking is bounded, not eliminated: a frozen future timestamp from a clock-ahead DPU that has
// stopped beating reads Fresh only while now is within ttl of that timestamp (a bounded window of
// at most ttl+skewGuard), after which it returns to Stale. This is acceptable for an
// observability-only signal; tightening it would require an observer-side timestamp.
//
// Consumers MUST call Freshness and never read LastProbeTime directly, so the data source can be
// hardened later without breaking them.
func Freshness(dpu *provisioningv1.DPU, now time.Time, ttl time.Duration) FreshnessState {
	if dpu == nil ||
		dpu.Status.AgentStatus == nil ||
		dpu.Status.AgentStatus.Spiffe == nil ||
		dpu.Status.AgentStatus.Spiffe.LastProbeTime == nil ||
		dpu.Status.AgentStatus.Spiffe.LastProbeTime.IsZero() {
		return NeverAttested
	}

	last := dpu.Status.AgentStatus.Spiffe.LastProbeTime.Time

	if ttl <= 0 {
		return Stale
	}
	if last.After(now.Add(skewGuard)) {
		return Stale
	}
	if now.Sub(last) <= ttl {
		return Fresh
	}
	return Stale
}
