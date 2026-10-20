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

package nicprovisioning

import (
	"sync"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestRuntimeFlavorDuringReleaseRetries exercises concurrent flavor reloads and NIC snapshots under the race detector.
func TestRuntimeFlavorDuringReleaseRetries(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, provisioningv1.AddToScheme(scheme))
	flavor := &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "flavor",
			Namespace:  "default",
			Generation: 2,
		},
		Spec: provisioningv1.DPUFlavorSpec{
			EWNicConfigurations: []provisioningv1.NicConfiguration{{NumVfs: 8}},
		},
	}
	shared := &operations.Context{
		Options:   opts.Options{DPUNamespace: "default"},
		Client:    fake.NewClientBuilder().WithScheme(scheme).WithObjects(flavor).Build(),
		DPUFlavor: *flavor.DeepCopy(),
	}
	shared.DPUFlavor.Generation = 1
	shared.DPUFlavor.Spec.EWNicConfigurations[0].NumVfs = 1
	nic := &NICProvisioning{}
	oldApply := nic.runtimeFlavorContext(shared)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for range 200 {
			if err := shared.ReloadDPUFlavor(t.Context()); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 200 {
			nic.runtimeFlavorContext(shared)
			nic.runtimeRetryContext(t.Context(), shared)
		}
	}()
	close(start)
	workers.Wait()
	latest := nic.runtimeFlavorContext(shared)
	require.EqualValues(t, 2, latest.DPUFlavor.Generation)
	require.Equal(t, 8, latest.DPUFlavor.Spec.FirstEWNicConfiguration().NumVfs)
	require.EqualValues(t, 1, oldApply.DPUFlavor.Generation)
	require.Equal(t, 1, oldApply.DPUFlavor.Spec.FirstEWNicConfiguration().NumVfs)
}
