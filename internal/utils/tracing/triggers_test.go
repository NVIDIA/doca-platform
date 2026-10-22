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

package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestRecordAndInjectTrigger checks which recorded trigger's trace a target receives: the reconciled
// object wins over any other trigger, otherwise the first recorded one is kept, and injecting
// forgets the trigger.
func TestRecordAndInjectTrigger(t *testing.T) {
	useTracker(t)
	const (
		reconciledTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
		firstTraceparent      = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
		secondTraceparent     = "00-11111111111111111111111111111111-2222222222222222-01"
	)
	reconciled := tracedConfigMap("reconciled", reconciledTraceparent)
	sameKindOtherName := tracedConfigMap("other", secondTraceparent)
	first := tracedSecret("first", firstTraceparent)
	second := tracedSecret("second", secondTraceparent)

	for name, tc := range map[string]struct {
		triggers        []client.Object
		wantTraceparent string
	}{
		"reconciled object after another trigger wins":                   {[]client.Object{first, reconciled}, reconciledTraceparent},
		"another trigger after the reconciled object loses":              {[]client.Object{reconciled, first}, reconciledTraceparent},
		"first of two other triggers is kept":                            {[]client.Object{first, second}, firstTraceparent},
		"an object of the reconciled kind but another name does not win": {[]client.Object{first, sameKindOtherName}, firstTraceparent},
		"nil trigger is ignored":                                         {[]client.Object{nil}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := WithTriggers(context.Background(), reconciled, nil)
			target := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "ns"}}
			for _, trigger := range tc.triggers {
				RecordTrigger(ctx, target, trigger)
			}
			require.Equal(t, tc.wantTraceparent != "", HasTrigger(ctx, target))

			require.Equal(t, tc.wantTraceparent != "", InjectTrigger(ctx, target))

			if tc.wantTraceparent == "" {
				require.NotContains(t, target.Annotations, GeneratedTraceParentAnnotation)
			} else {
				require.Equal(t, tc.wantTraceparent, target.Annotations[GeneratedTraceParentAnnotation])
			}
			require.False(t, HasTrigger(ctx, target), "injecting forgets the trigger")
		})
	}
}

// TestRecordTriggerWithoutRecord checks that recording outside WithTriggers is a no-op.
func TestRecordTriggerWithoutRecord(t *testing.T) {
	target := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "ns"}}
	RecordTrigger(context.Background(), target, tracedSecret("first", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"))
	require.False(t, HasTrigger(context.Background(), target))
	require.False(t, InjectTrigger(context.Background(), target))
}

// TestInjectTriggerUsesOptionsOf checks that a trigger without a trace hands on its derived workflow
// only when optionsOf marks its controller as a WorkflowRoot, and that otherwise the target's
// earlier trace is removed.
func TestInjectTriggerUsesOptionsOf(t *testing.T) {
	useTracker(t)
	untraced := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:       "untraced",
		Namespace:  "ns",
		UID:        "untraced-uid",
		Generation: 1,
	}}
	for name, tc := range map[string]struct {
		optionsOf func(client.Object) []ReconcileOption
		want      bool
	}{
		"no options": {
			optionsOf: nil,
			want:      false,
		},
		"workflow root": {
			optionsOf: func(client.Object) []ReconcileOption { return []ReconcileOption{WorkflowRoot()} },
			want:      true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := WithTriggers(context.Background(), tracedConfigMap("reconciled", ""), tc.optionsOf)
			target := tracedPod(earlierTraceparent)
			RecordTrigger(ctx, target, untraced)

			require.Equal(t, tc.want, InjectTrigger(ctx, target))
			require.NotContains(t, target.Annotations, TraceParentAnnotation)
			if tc.want {
				require.NotEmpty(t, target.Annotations[GeneratedTraceParentAnnotation])
				require.NotEqual(t, earlierTraceparent, target.Annotations[GeneratedTraceParentAnnotation])
			} else {
				require.NotContains(t, target.Annotations, GeneratedTraceParentAnnotation)
			}
		})
	}
}

// TestInjectTriggerWithoutTriggerKeepsTrace checks that a write without a recorded trigger leaves
// the target's trace context untouched.
func TestInjectTriggerWithoutTriggerKeepsTrace(t *testing.T) {
	useTracker(t)
	ctx := WithTriggers(context.Background(), tracedConfigMap("reconciled", ""), nil)
	target := tracedPod(earlierTraceparent)

	require.False(t, InjectTrigger(ctx, target))
	require.Equal(t, earlierTraceparent, target.Annotations[TraceParentAnnotation])
}

// earlierTraceparent is the trace context a target carries from an earlier, unrelated change.
const earlierTraceparent = "00-33333333333333333333333333333333-4444444444444444-01"

// tracedPod returns the Pod "target" whose consumer trace context is traceparent.
func tracedPod(traceparent string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        "target",
		Namespace:   "ns",
		Annotations: map[string]string{TraceParentAnnotation: traceparent},
	}}
}

// tracedConfigMap returns a ConfigMap named name whose consumer trace context is traceparent, or
// none when traceparent is empty.
func tracedConfigMap(name, traceparent string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
	if traceparent != "" {
		cm.Annotations = map[string]string{TraceParentAnnotation: traceparent}
	}
	return cm
}

// tracedSecret returns a Secret named name whose consumer trace context is traceparent.
func tracedSecret(name, traceparent string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:        name,
		Namespace:   "ns",
		Annotations: map[string]string{TraceParentAnnotation: traceparent},
	}}
}
