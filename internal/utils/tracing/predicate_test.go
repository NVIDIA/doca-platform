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
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// TestIgnoreTraceOnlyUpdates checks that only an update changing nothing but the trace annotations
// is dropped, and that other events pass.
func TestIgnoreTraceOnlyUpdates(t *testing.T) {
	useTracker(t)
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	base := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cm",
			Namespace:       "ns",
			ResourceVersion: "1",
			Annotations:     map[string]string{"other": "value"},
		},
		Data: map[string]string{"key": "value"},
	}
	for name, tc := range map[string]struct {
		update func(cm *corev1.ConfigMap)
		want   bool
	}{
		"resync": {
			update: func(*corev1.ConfigMap) {},
			want:   true,
		},
		"trace added": {
			update: func(cm *corev1.ConfigMap) {
				cm.ResourceVersion = "2"
				cm.Annotations[GeneratedTraceParentAnnotation] = traceparent
				cm.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "dpuset"}}
			},
			want: false,
		},
		"trace added with a data change": {
			update: func(cm *corev1.ConfigMap) {
				cm.ResourceVersion = "2"
				cm.Annotations[GeneratedTraceParentAnnotation] = traceparent
				cm.Data["key"] = "changed"
			},
			want: true,
		},
		"trace added with another annotation change": {
			update: func(cm *corev1.ConfigMap) {
				cm.ResourceVersion = "2"
				cm.Annotations[GeneratedTraceParentAnnotation] = traceparent
				cm.Annotations["other"] = "changed"
			},
			want: true,
		},
		"data change only": {
			update: func(cm *corev1.ConfigMap) {
				cm.ResourceVersion = "2"
				cm.Data["key"] = "changed"
			},
			want: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			updated := base.DeepCopy()
			tc.update(updated)
			require.Equal(t, tc.want, IgnoreTraceOnlyUpdates().Update(event.UpdateEvent{ObjectOld: base, ObjectNew: updated}))
		})
	}

	t.Run("trace removed leaving no annotations", func(t *testing.T) {
		traced := base.DeepCopy()
		traced.Annotations = map[string]string{TraceParentAnnotation: traceparent}
		removed := traced.DeepCopy()
		removed.ResourceVersion = "2"
		removed.Annotations = nil
		require.False(t, IgnoreTraceOnlyUpdates().Update(event.UpdateEvent{ObjectOld: traced, ObjectNew: removed}))
	})

	t.Run("other events pass", func(t *testing.T) {
		p := IgnoreTraceOnlyUpdates()
		require.True(t, p.Create(event.CreateEvent{Object: base}))
		require.True(t, p.Delete(event.DeleteEvent{Object: base}))
		require.True(t, p.Generic(event.GenericEvent{Object: base}))
	})
}
