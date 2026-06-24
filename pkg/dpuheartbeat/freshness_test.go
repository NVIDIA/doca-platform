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

package dpuheartbeat

import (
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// dpuWithProbe builds a DPU whose Spiffe sub-status carries the given LastProbeTime.
func dpuWithProbe(t *metav1.Time) *provisioningv1.DPU {
	dpu := &provisioningv1.DPU{}
	dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
		Spiffe: &provisioningv1.SpiffeStatus{
			LastProbeTime: t,
		},
	}
	return dpu
}

func TestFreshness(t *testing.T) {
	now := time.Now()
	ttl := 90 * time.Second

	withinTTL := &metav1.Time{Time: now.Add(-30 * time.Second)}
	atTTL := &metav1.Time{Time: now.Add(-ttl)}
	overTTL := &metav1.Time{Time: now.Add(-120 * time.Second)}
	futureWithinGuard := &metav1.Time{Time: now.Add(skewGuard - time.Second)}
	futureBeyondGuard := &metav1.Time{Time: now.Add(skewGuard + time.Second)}

	nilAgentStatus := &provisioningv1.DPU{}

	nilSpiffe := &provisioningv1.DPU{}
	nilSpiffe.Status.AgentStatus = &provisioningv1.AgentStatus{}

	tests := []struct {
		name string
		dpu  *provisioningv1.DPU
		ttl  time.Duration
		want FreshnessState
	}{
		{name: "nil dpu", dpu: nil, ttl: ttl, want: NeverAttested},
		{name: "nil AgentStatus", dpu: nilAgentStatus, ttl: ttl, want: NeverAttested},
		{name: "nil Spiffe", dpu: nilSpiffe, ttl: ttl, want: NeverAttested},
		{name: "nil LastProbeTime", dpu: dpuWithProbe(nil), ttl: ttl, want: NeverAttested},
		{name: "zero LastProbeTime", dpu: dpuWithProbe(&metav1.Time{}), ttl: ttl, want: NeverAttested},
		{name: "within ttl is Fresh", dpu: dpuWithProbe(withinTTL), ttl: ttl, want: Fresh},
		{name: "exact ttl boundary is Fresh (inclusive)", dpu: dpuWithProbe(atTTL), ttl: ttl, want: Fresh},
		{name: "older than ttl is Stale", dpu: dpuWithProbe(overTTL), ttl: ttl, want: Stale},
		{name: "future within skewGuard is Fresh", dpu: dpuWithProbe(futureWithinGuard), ttl: ttl, want: Fresh},
		{name: "future beyond skewGuard is Stale", dpu: dpuWithProbe(futureBeyondGuard), ttl: ttl, want: Stale},
		{name: "ttl == 0 with present timestamp is Stale", dpu: dpuWithProbe(withinTTL), ttl: 0, want: Stale},
		{name: "negative ttl with present timestamp is Stale", dpu: dpuWithProbe(withinTTL), ttl: -1 * time.Second, want: Stale},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Freshness(tt.dpu, now, tt.ttl)
			if got != tt.want {
				t.Fatalf("Freshness() = %q, want %q", got, tt.want)
			}
		})
	}
}
