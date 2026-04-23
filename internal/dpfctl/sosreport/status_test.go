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

package sosreport

import (
	"testing"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestJobStatus(t *testing.T) {
	tests := []struct {
		name string
		job  batchv1.Job
		want string
	}{
		{
			name: "succeeded",
			job:  batchv1.Job{Status: batchv1.JobStatus{Succeeded: 1}},
			want: "Completed",
		},
		{
			name: "active",
			job:  batchv1.Job{Status: batchv1.JobStatus{Active: 1}},
			want: "Running",
		},
		{
			name: "failed",
			job: batchv1.Job{Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"},
				},
			}},
			want: "Failed: BackoffLimitExceeded",
		},
		{
			name: "pending",
			job:  batchv1.Job{},
			want: "Pending",
		},
		{
			name: "succeeded takes priority over failed condition",
			job: batchv1.Job{Status: batchv1.JobStatus{
				Succeeded: 1,
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "SomeReason"},
				},
			}},
			want: "Completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(JobStatus(&tt.job)).To(Equal(tt.want))
		})
	}
}

func TestIsJobDone(t *testing.T) {
	tests := []struct {
		name string
		job  batchv1.Job
		want bool
	}{
		{
			name: "succeeded",
			job:  batchv1.Job{Status: batchv1.JobStatus{Succeeded: 1}},
			want: true,
		},
		{
			name: "failed",
			job: batchv1.Job{Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
				},
			}},
			want: true,
		},
		{
			name: "active",
			job:  batchv1.Job{Status: batchv1.JobStatus{Active: 1}},
			want: false,
		},
		{
			name: "pending",
			job:  batchv1.Job{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsJobDone(&tt.job)).To(Equal(tt.want))
		})
	}
}
