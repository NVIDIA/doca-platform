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

package apivalidation_test

import (
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestInterfaceEntrySpecHash_StableAndSensitive(t *testing.T) {
	base := dpuservicev1.InterfaceEntry{
		Name:          "ns_sis-a",
		InterfaceType: "vf",
		VF:            &dpuservicev1.VF{PFID: 0, VFID: 1, ParentInterfaceRef: ptr.To("p0")},
		Labels:        map[string]string{"k": "v"},
	}
	same := base.DeepCopy()
	if base.SpecHash() == "" {
		t.Fatal("SpecHash returned empty")
	}
	if base.SpecHash() != same.SpecHash() {
		t.Fatalf("identical entries must hash equal: %q vs %q", base.SpecHash(), same.SpecHash())
	}

	changed := base.DeepCopy()
	changed.Labels["k"] = "other"
	if base.SpecHash() == changed.SpecHash() {
		t.Fatal("label change must change SpecHash")
	}

	terminating := base.DeepCopy()
	terminating.Terminating = true
	if base.SpecHash() == terminating.SpecHash() {
		t.Fatal("Terminating flip must change SpecHash")
	}
}

func TestIsEntryReady_IgnoresSiblingGenerationBump(t *testing.T) {
	entry := dpuservicev1.InterfaceEntry{
		Name:          "ns_live",
		InterfaceType: "physical",
		Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
	}
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{Name: "node-sfc", Generation: 100},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node:       "node",
			Type:       dpuservicev1.NSITypeSFC,
			Interfaces: []dpuservicev1.InterfaceEntry{entry},
		},
		Status: dpuservicev1.NodeServiceInterfacesStatus{
			InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{{
				Name:             entry.Name,
				ObservedSpecHash: entry.SpecHash(),
				Conditions: []metav1.Condition{{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					ObservedGeneration: 99, // ignored: entry freshness is ObservedSpecHash, not this field
				}},
			}},
		},
	}

	if !nsi.IsEntryReady(&entry) {
		t.Fatal("live entry with matching ObservedSpecHash must be ready regardless of condition ObservedGeneration")
	}

	nsi.Generation = 101 // sibling delete bumps object generation again
	if !nsi.IsEntryReady(&entry) {
		t.Fatal("sibling generation bump must not demote an entry whose ObservedSpecHash still matches")
	}
}

func TestIsEntryReady_RequiresHashMatch(t *testing.T) {
	entry := dpuservicev1.InterfaceEntry{
		Name:          "ns_live",
		InterfaceType: "physical",
		Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
	}
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
		Status: dpuservicev1.NodeServiceInterfacesStatus{
			InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{{
				Name:             entry.Name,
				ObservedSpecHash: "stale-hash",
				Conditions: []metav1.Condition{{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					ObservedGeneration: 1,
				}},
			}},
		},
	}
	if nsi.IsEntryReady(&entry) {
		t.Fatal("Ready=True with mismatched ObservedSpecHash must not count as ready")
	}
}

func TestIsEntryResourceReleased_RequiresHashMatch(t *testing.T) {
	entry := dpuservicev1.InterfaceEntry{
		Name:          "ns_dying",
		Terminating:   true,
		InterfaceType: "physical",
		Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
	}
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{Generation: 5},
		Status: dpuservicev1.NodeServiceInterfacesStatus{
			InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{{
				Name:             entry.Name,
				ObservedSpecHash: entry.SpecHash(),
				Conditions: []metav1.Condition{{
					Type:               string(dpuservicev1.ResourceReleased),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					ObservedGeneration: 4,
				}},
			}},
		},
	}
	if !nsi.IsEntryResourceReleased(&entry) {
		t.Fatal("ResourceReleased with matching ObservedSpecHash must be accepted regardless of condition ObservedGeneration")
	}

	nsi.Status.InterfaceStatuses[0].ObservedSpecHash = "wrong"
	if nsi.IsEntryResourceReleased(&entry) {
		t.Fatal("ResourceReleased with mismatched hash must be rejected")
	}
}
