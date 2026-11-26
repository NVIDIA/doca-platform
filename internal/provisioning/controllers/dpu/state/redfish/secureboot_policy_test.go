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

package redfish

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	redfishmock "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestSecureBootResetPolicy counts real reset requests for each family and direction.
func TestSecureBootResetPolicy(t *testing.T) {
	for _, family := range []redfishmock.DpuVersion{redfishmock.BF3, redfishmock.BF4} {
		for _, desired := range []bool{false, true} {
			name := "BF3"
			if family == redfishmock.BF4 {
				name = "BF4"
			}
			if desired {
				name += "/enable"
			} else {
				name += "/disable"
			}
			t.Run(name, func(t *testing.T) {
				server := redfishmock.NewRedfishMockServer("BF-26.10", "password")
				server.SetDpuVersion(family)
				server.SetSecureBootEnable(!desired)
				server.SetSecureBootCurrentBoot(!desired)
				server.Start()
				t.Cleanup(server.Stop)
				client, err := server.GetClient()
				if err != nil {
					t.Fatal(err)
				}
				dpu := &provisioningv1.DPU{Spec: provisioningv1.DPUSpec{SecureBoot: ptr.To(desired)}}
				state := &dpu.Status
				done, err := detectAndStageSecureBoot(context.Background(), dpu, state, client)
				if err != nil || !done {
					t.Fatalf("stage: done=%v err=%v", done, err)
				}
				tracker, err := dutil.LoadArmRestartTracker(dpu)
				if err != nil || tracker == nil {
					t.Fatalf("tracker=%+v err=%v", tracker, err)
				}
				want := 2
				if family == redfishmock.BF4 {
					want = 1
				}
				if tracker.MaxAttempts != want {
					t.Fatalf("reset count=%d want=%d", tracker.MaxAttempts, want)
				}
				for attempt := 0; attempt < want; attempt++ {
					if attempt > 0 {
						tracker.LastRestartTime = time.Now().Add(-MinRestartInterval - time.Second)
					}
					_, err = executeRestartStateMachine(context.Background(), dpu, state, tracker, client)
					if err != nil || tracker.Attempt != attempt+1 {
						t.Fatalf("reset: tracker=%+v err=%v", tracker, err)
					}
				}
				if got := server.GetArmResetRequests(); got != want {
					t.Fatalf("actual reset requests=%d want=%d", got, want)
				}
				acceptedAt := tracker.LastRestartTime
				_, err = executeRestartStateMachine(context.Background(), dpu, state, tracker, client)
				if err != nil || state.Phase != provisioningv1.DPUInitializeInterface || tracker.Attempt != want || tracker.LastRestartTime != acceptedAt {
					t.Fatalf("completed resets replayed: tracker=%+v state=%+v err=%v", tracker, state, err)
				}
				if got := server.GetArmResetRequests(); got != want {
					t.Fatalf("completed reconcile sent another reset: requests=%d want=%d", got, want)
				}
			})
		}
	}
}

// TestSecureBootVerificationPolicy preserves the BF3 timeout and extends only BF4.
func TestSecureBootVerificationPolicy(t *testing.T) {
	for _, tt := range []struct {
		name         string
		family       redfishmock.DpuVersion
		age          time.Duration
		wantTerminal bool
	}{
		{name: "BF3 waits before four minutes", family: redfishmock.BF3, age: 3 * time.Minute},
		{name: "BF3 retains four-minute limit", family: redfishmock.BF3, age: 5 * time.Minute, wantTerminal: true},
		{name: "BF4 waits past four minutes", family: redfishmock.BF4, age: 7 * time.Minute},
		{name: "BF4 waits before twelve minutes", family: redfishmock.BF4, age: 12*time.Minute - time.Second},
		{name: "BF4 expires after twelve minutes", family: redfishmock.BF4, age: 12*time.Minute + time.Second, wantTerminal: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := redfishmock.NewRedfishMockServer("BF-26.10", "password")
			server.SetDpuVersion(tt.family)
			server.SetSecureBootCurrentBoot(true)
			server.SetSecureBootEnable(false)
			server.Start()
			t.Cleanup(server.Stop)
			client, err := server.GetClient()
			if err != nil {
				t.Fatal(err)
			}
			dpu := &provisioningv1.DPU{Spec: provisioningv1.DPUSpec{SecureBoot: ptr.To(false)}}
			dpu.Status.Phase = provisioningv1.DPUInitializeInterface
			dpu.Status.Conditions = []metav1.Condition{{
				Type:               provisioningv1.DPUCondArmForceRestarted.String(),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-tt.age)),
			}}
			tracker := &dutil.ArmRestartTracker{Attempt: 1, MaxAttempts: 1, LastRestartTime: time.Now().Add(-tt.age)}
			if err := dutil.SaveArmRestartTracker(dpu, tracker); err != nil {
				t.Fatal(err)
			}
			original := dpu.Annotations[provisioningv1.AnnotationArmRestartTracker]
			done, err := verifySecureBootAfterRestarts(context.Background(), dpu, &dpu.Status, client, tracker)
			if err != nil || !done || (dpu.Status.Phase == provisioningv1.DPUError) != tt.wantTerminal {
				t.Fatalf("done=%v phase=%s err=%v wantTerminal=%v", done, dpu.Status.Phase, err, tt.wantTerminal)
			}
			if dpu.Annotations[provisioningv1.AnnotationArmRestartTracker] != original || server.GetArmResetRequests() != 0 {
				t.Fatal("verification changed the reset sequence")
			}
		})
	}
}
