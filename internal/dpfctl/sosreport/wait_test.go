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
	"context"
	"testing"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCheckInitContainerFailure(t *testing.T) {
	labels := map[string]string{"app": "test"}

	tests := []struct {
		name    string
		pod     *corev1.Pod
		wantMsg string
	}{
		{
			name: "no failure",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default", Labels: labels},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{Name: "mkdir", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
					},
				},
			},
			wantMsg: "",
		},
		{
			name: "terminated with non-zero exit",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default", Labels: labels},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "mkdir",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
								},
							},
						},
					},
				},
			},
			wantMsg: `init container "mkdir" failed (exit 1): Error`,
		},
		{
			name: "crash loop backoff",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "default", Labels: labels},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "sosreport",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason:  "CrashLoopBackOff",
									Message: "back-off 10s",
								},
							},
						},
					},
				},
			},
			wantMsg: `init container "sosreport" is crash-looping: back-off 10s`,
		},
		{
			name: "terminated with message prefers message over reason",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-4", Namespace: "default", Labels: labels},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "mkdir",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 126,
									Reason:   "Error",
									Message:  "Permission denied",
								},
							},
						},
					},
				},
			},
			wantMsg: `init container "mkdir" failed (exit 126): Permission denied`,
		},
		{
			name:    "no pods",
			pod:     nil,
			wantMsg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.pod != nil {
				builder = builder.WithRuntimeObjects(tt.pod)
			}
			c := builder.Build()
			job := testJobWithSelector(labels)

			msg := checkInitContainerFailure(context.Background(), c, "default", job)
			if tt.wantMsg == "" {
				g.Expect(msg).To(BeEmpty())
			} else {
				g.Expect(msg).To(Equal(tt.wantMsg))
			}
		})
	}
}

func TestFindReadyDownloadPod(t *testing.T) {
	tests := []struct {
		name    string
		job     *batchv1.Job
		pod     *corev1.Pod
		wantPod string
	}{
		{
			name: "uses job selector instead of template labels",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "job-1"},
				Spec: batchv1.JobSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{batchv1.JobNameLabel: "job-1"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"wrong": "label"}},
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					Labels:    map[string]string{batchv1.JobNameLabel: "job-1"},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
			wantPod: "pod-1",
		},
		{
			name: "ignores non-running pod",
			job:  testJobWithSelector(map[string]string{batchv1.JobNameLabel: "job-1"}),
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					Labels:    map[string]string{batchv1.JobNameLabel: "job-1"},
				},
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			},
			wantPod: "",
		},
		{
			name: "supports selector match expressions",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "job-1"},
				Spec: batchv1.JobSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "sosreport"},
						MatchExpressions: []metav1.LabelSelectorRequirement{{
							Key:      batchv1.JobNameLabel,
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"job-1"},
						}},
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					Labels: map[string]string{
						"app":                "sosreport",
						batchv1.JobNameLabel: "job-1",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
			wantPod: "pod-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.pod).Build()

			pod, err := FindReadyDownloadPod(context.Background(), c, "default", tt.job)
			g.Expect(err).NotTo(HaveOccurred())
			if tt.wantPod == "" {
				g.Expect(pod).To(BeNil())
				return
			}
			g.Expect(pod).NotTo(BeNil())
			g.Expect(pod.Name).To(Equal(tt.wantPod))
		})
	}
}

func testJobWithSelector(labels map[string]string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job-1"},
		Spec: batchv1.JobSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
		},
	}
}
