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

package e2e

import (
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPreserveDPUSetRuntimeSelectors(t *testing.T) {
	const mutatedValue = "mutated"

	g := NewWithT(t)
	nodeSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"kubernetes.io/hostname": "worker1"},
	}
	dpuSelector := map[string]string{
		cutil.DPUDevicePCIAddressLabel: "0000-12-00",
	}
	dpuDeviceSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"device": "mlx5_0"},
	}
	installed := []dpuservicev1.DPUSet{{
		NodeSelector:      nodeSelector,
		DPUSelector:       dpuSelector,
		DPUDeviceSelector: dpuDeviceSelector,
	}}
	desired := []dpuservicev1.DPUSet{{}}

	preserveDPUSetRuntimeSelectors(desired, installed)

	//nolint:staticcheck // Upgrade tests preserve previous-release fields during rollout.
	preservedNodeSelector := desired[0].NodeSelector
	//nolint:staticcheck // Upgrade tests preserve previous-release fields during rollout.
	preservedDPUSelector := desired[0].DPUSelector
	preservedDPUDeviceSelector := desired[0].DPUDeviceSelector

	g.Expect(preservedNodeSelector).To(Equal(nodeSelector))
	g.Expect(preservedDPUSelector).To(Equal(dpuSelector))
	g.Expect(preservedDPUDeviceSelector).To(Equal(dpuDeviceSelector))

	nodeSelector.MatchLabels["kubernetes.io/hostname"] = mutatedValue
	dpuSelector[cutil.DPUDevicePCIAddressLabel] = mutatedValue
	dpuDeviceSelector.MatchLabels["device"] = mutatedValue
	g.Expect(preservedNodeSelector.MatchLabels).To(HaveKeyWithValue("kubernetes.io/hostname", "worker1"))
	g.Expect(preservedDPUSelector).To(HaveKeyWithValue(cutil.DPUDevicePCIAddressLabel, "0000-12-00"))
	g.Expect(preservedDPUDeviceSelector.MatchLabels).To(HaveKeyWithValue("device", "mlx5_0"))
}
