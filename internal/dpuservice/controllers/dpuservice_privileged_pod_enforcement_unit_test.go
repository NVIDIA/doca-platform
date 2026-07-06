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

package controllers

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// podIsPrivileged reports whether any container in obj (a Pod) requests privileged.
func podIsPrivileged(obj client.Object) bool {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return false
	}
	for _, c := range pod.Spec.Containers {
		if c.SecurityContext != nil && ptr.Deref(c.SecurityContext.Privileged, false) {
			return true
		}
	}
	return false
}

// errInvalidPrivilegedPodDenied mimics the API server rejecting a privileged pod
// via our VAP (Invalid + privilegedPodDeniedMessage).
var errInvalidPrivilegedPodDenied = &apierrors.StatusError{ErrStatus: metav1.Status{
	Reason:  metav1.StatusReasonInvalid,
	Message: `Pod "x" is invalid: ` + privilegedPodDeniedMessage,
}}

// errParamRefNotSynced mimics the transient paramRef-informer-not-synced denial.
var errParamRefNotSynced = errors.New("admission webhook denied the request: " + paramRefNotSyncedMarker)

// errForbiddenOtherStage mimics a denial from a different admission stage.
var errForbiddenOtherStage = &apierrors.StatusError{ErrStatus: metav1.Status{
	Reason:  metav1.StatusReasonForbidden,
	Message: `pods "x" is forbidden: violates PodSecurity "restricted:latest"`,
}}

// TestValidatePrivilegedPodEnforcement drives the post-apply probe through all of
// its branches with a fake client whose Create is programmed to admit or reject
// the non-privileged (probe 1) and privileged (probe 2) probe pods. This replaces
// the former envtest specs, which raced the live reconciler over the shared VAP.
func TestValidatePrivilegedPodEnforcement(t *testing.T) {
	tests := []struct {
		name          string
		nonPrivileged error // Create result for probe 1 (non-privileged pod)
		privileged    error // Create result for probe 2 (privileged pod)
		wantErr       string
	}{
		{name: "enforcing: non-privileged admitted, privileged denied", nonPrivileged: nil, privileged: errInvalidPrivilegedPodDenied, wantErr: ""},
		{name: "not enforcing: privileged unexpectedly admitted", nonPrivileged: nil, privileged: nil, wantErr: "unexpectedly allowed"},
		{name: "probe 1 paramRef not synced", nonPrivileged: errParamRefNotSynced, privileged: errInvalidPrivilegedPodDenied, wantErr: "waiting for the VAP paramRef informer to catch up"},
		{name: "probe 1 unexpected rejection", nonPrivileged: errForbiddenOtherStage, privileged: errInvalidPrivilegedPodDenied, wantErr: "non-privileged pod unexpectedly rejected"},
		{name: "probe 2 paramRef not synced", nonPrivileged: nil, privileged: errParamRefNotSynced, wantErr: "waiting for the VAP paramRef informer to catch up"},
		{name: "probe 2 unexpected error", nonPrivileged: nil, privileged: errForbiddenOtherStage, wantErr: "privileged pod got unexpected error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := applicationPrereqsTestScheme(t)
			c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
					if podIsPrivileged(obj) {
						return tt.privileged
					}
					return tt.nonPrivileged
				},
			}).Build()

			err := validatePrivilegedPodEnforcement(context.Background(), c)
			if tt.wantErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}

// TestValidateAllowlistedPrivilegedPodAdmission drives the allowlisted-admission
// probe through all of its branches with a programmed fake client.
func TestValidateAllowlistedPrivilegedPodAdmission(t *testing.T) {
	tests := []struct {
		name         string
		serviceID    string
		createResult error
		wantErr      string
	}{
		{name: "empty service ID is rejected before any probe", serviceID: "", createResult: nil, wantErr: "requires a DPUService ID"},
		{name: "allowlisted privileged pod admitted", serviceID: "svc-id", createResult: nil, wantErr: ""},
		{name: "denied by our VAP", serviceID: "svc-id", createResult: errInvalidPrivilegedPodDenied, wantErr: "allowlisted privileged pod rejected"},
		{name: "paramRef not synced", serviceID: "svc-id", createResult: errParamRefNotSynced, wantErr: "waiting for the VAP paramRef informer to catch up"},
		{name: "unexpected error from another stage", serviceID: "svc-id", createResult: errForbiddenOtherStage, wantErr: "allowlisted privileged pod got unexpected error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := applicationPrereqsTestScheme(t)
			c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return tt.createResult
				},
			}).Build()

			err := validateAllowlistedPrivilegedPodAdmission(context.Background(), c, tt.serviceID)
			if tt.wantErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}
