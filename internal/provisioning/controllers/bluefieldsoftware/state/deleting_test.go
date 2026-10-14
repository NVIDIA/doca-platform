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

package state

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestDeletingBlockedByDPUReference checks that only a same-namespace DPU naming this
// BlueFieldSoftware, including one being deleted, blocks its deletion.
func TestDeletingBlockedByDPUReference(t *testing.T) {
	const (
		namespace = "test-ns"
		bfsName   = "old-bfs"
	)
	now := metav1.Now()

	newDPU := func(name, ns string, spec provisioningv1.DPUSpec) *provisioningv1.DPU {
		spec.DPUNodeName = "worker1"
		spec.DPUDeviceName = "device-" + name
		spec.SerialNumber = "SN-" + name
		return &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: spec,
		}
	}
	deletingDPU := newDPU("dpu-deleting", namespace, provisioningv1.DPUSpec{BlueFieldSoftware: ptr.To(bfsName)})
	deletingDPU.DeletionTimestamp = &now
	deletingDPU.Finalizers = []string{provisioningv1.DPUFinalizer}

	tests := []struct {
		name        string
		dpu         *provisioningv1.DPU
		wantBlocked bool
	}{
		{
			name:        "same-namespace DPU still names the software",
			dpu:         newDPU("dpu-live", namespace, provisioningv1.DPUSpec{BlueFieldSoftware: ptr.To(bfsName)}),
			wantBlocked: true,
		},
		{
			name:        "DPU being deleted still names the software",
			dpu:         deletingDPU,
			wantBlocked: true,
		},
		{
			name: "DPU in another namespace names the same software",
			dpu:  newDPU("dpu-other-ns", "other-ns", provisioningv1.DPUSpec{BlueFieldSoftware: ptr.To(bfsName)}),
		},
		{
			name: "DPU names different software",
			dpu:  newDPU("dpu-new", namespace, provisioningv1.DPUSpec{BlueFieldSoftware: ptr.To("new-bfs")}),
		},
		{
			name: "DPU uses a BFB",
			dpu:  newDPU("dpu-bfb", namespace, provisioningv1.DPUSpec{BFB: ptr.To(bfsName)}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, provisioningv1.AddToScheme(scheme))

			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bfsName,
					Namespace:  namespace,
					Finalizers: []string{provisioningv1.BlueFieldSoftwareFinalizer},
				},
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.dpu.DeepCopy()).Build()

			st := &blueFieldSoftwareDeletingState{
				bfs:      bfs,
				recorder: record.NewFakeRecorder(10),
			}
			err := st.Handle(context.Background(), cl)

			deleted := meta.FindStatusCondition(bfs.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDeleted))
			require.NotNil(t, deleted)
			if tt.wantBlocked {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "still referenced by DPU "+tt.dpu.Name)
				assert.Equal(t, metav1.ConditionFalse, deleted.Status)
				assert.Equal(t, string(conditions.ReasonPending), deleted.Reason)
				assert.Contains(t, bfs.Finalizers, provisioningv1.BlueFieldSoftwareFinalizer)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, metav1.ConditionTrue, deleted.Status)
			assert.NotContains(t, bfs.Finalizers, provisioningv1.BlueFieldSoftwareFinalizer)
		})
	}
}
