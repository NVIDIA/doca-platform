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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSelectDPUDeviceWithPCIAddress(t *testing.T) {
	tests := []struct {
		name       string
		dpuDevices []provisioningv1.DPUDevice
		wantName   string
		wantPCI    string
		wantErr    string
	}{
		{
			name: "selects the lowest PCI address",
			dpuDevices: []provisioningv1.DPUDevice{
				dpuDeviceWithPCIAddress("device-a2", "0000-a2-00"),
				dpuDeviceWithPCIAddress("device-12", "0000-12-00"),
			},
			wantName: "device-12",
			wantPCI:  "0000-12-00",
		},
		{
			name: "compares hexadecimal addresses case-insensitively",
			dpuDevices: []provisioningv1.DPUDevice{
				dpuDeviceWithPCIAddress("device-a2", "0000-A2-00"),
				dpuDeviceWithPCIAddress("device-a1", "0000-a1-00"),
			},
			wantName: "device-a1",
			wantPCI:  "0000-a1-00",
		},
		{
			name: "ignores devices without a PCI address label",
			dpuDevices: []provisioningv1.DPUDevice{
				{ObjectMeta: metav1.ObjectMeta{Name: "device-without-label"}},
				dpuDeviceWithPCIAddress("device-with-label", "0000-a2-00"),
			},
			wantName: "device-with-label",
			wantPCI:  "0000-a2-00",
		},
		{
			name: "returns an error when no PCI address is available",
			dpuDevices: []provisioningv1.DPUDevice{
				{ObjectMeta: metav1.ObjectMeta{Name: "device-without-label"}},
			},
			wantErr: "no DPUDevice has a PCI address label",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			selected, err := selectDPUDeviceWithPCIAddress(test.dpuDevices)
			if test.wantErr != "" {
				g.Expect(err).To(MatchError(test.wantErr))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(selected.Name).To(Equal(test.wantName))
			g.Expect(selected.Labels).To(HaveKeyWithValue(cutil.DPUDevicePCIAddressLabel, test.wantPCI))
		})
	}
}

func TestSharedDPUDevicePCIAddress(t *testing.T) {
	tests := []struct {
		name             string
		dpuDevicesByNode map[string][]provisioningv1.DPUDevice
		wantPCI          string
		wantErr          string
	}{
		{
			name: "selects the lowest address shared by every node",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker2": {
					dpuDeviceWithPCIAddress("worker2-a2", "0000-a2-00"),
					dpuDeviceWithPCIAddress("worker2-12", "0000-12-00"),
				},
				"worker1": {
					dpuDeviceWithPCIAddress("worker1-12", "0000-12-00"),
					dpuDeviceWithPCIAddress("worker1-a2", "0000-a2-00"),
				},
			},
			wantPCI: "0000-12-00",
		},
		{
			name: "ignores devices without PCI address labels",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker1": {
					{ObjectMeta: metav1.ObjectMeta{Name: "worker1-unlabeled"}},
					dpuDeviceWithPCIAddress("worker1-a2", "0000-a2-00"),
				},
				"worker2": {
					dpuDeviceWithPCIAddress("worker2-a2", "0000-a2-00"),
				},
			},
			wantPCI: "0000-a2-00",
		},
		{
			name: "ignores addresses that only some nodes expose",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker2": {
					dpuDeviceWithPCIAddress("worker2-2b", "0000-2b-00"),
					dpuDeviceWithPCIAddress("worker2-a2", "0000-a2-00"),
				},
				"worker1": {
					dpuDeviceWithPCIAddress("worker1-12", "0000-12-00"),
					dpuDeviceWithPCIAddress("worker1-a2", "0000-a2-00"),
				},
			},
			wantPCI: "0000-a2-00",
		},
		{
			name: "orders addresses case-insensitively",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker1": {
					dpuDeviceWithPCIAddress("worker1-b1", "0000-B1-00"),
					dpuDeviceWithPCIAddress("worker1-a2", "0000-a2-00"),
				},
				"worker2": {
					dpuDeviceWithPCIAddress("worker2-b1", "0000-B1-00"),
					dpuDeviceWithPCIAddress("worker2-a2", "0000-a2-00"),
				},
			},
			wantPCI: "0000-a2-00",
		},
		{
			// The address becomes a label selector, so it must stay byte identical to
			// the label instead of being normalized.
			name: "returns the address as labeled",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker1": {dpuDeviceWithPCIAddress("worker1-a2", "0000-A2-00")},
				"worker2": {dpuDeviceWithPCIAddress("worker2-a2", "0000-A2-00")},
			},
			wantPCI: "0000-A2-00",
		},
		{
			// No single selector can match both spellings, so this has to fail loudly.
			name: "rejects the same address labeled with different casing",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker1": {dpuDeviceWithPCIAddress("worker1-a2", "0000-A2-00")},
				"worker2": {dpuDeviceWithPCIAddress("worker2-a2", "0000-a2-00")},
			},
			wantErr: "no DPUDevice PCI address shared by DPU nodes: " +
				"worker1=[0000-A2-00], worker2=[0000-a2-00]",
		},
		{
			name: "reports each node inventory when no address is shared",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker2": {
					dpuDeviceWithPCIAddress("worker2-a2", "0000-a2-00"),
				},
				"worker1": {
					dpuDeviceWithPCIAddress("worker1-12", "0000-12-00"),
				},
			},
			wantErr: "no DPUDevice PCI address shared by DPU nodes: " +
				"worker1=[0000-12-00], worker2=[0000-a2-00]",
		},
		{
			name: "reports the inventory when a node has no labeled device",
			dpuDevicesByNode: map[string][]provisioningv1.DPUDevice{
				"worker1": {{ObjectMeta: metav1.ObjectMeta{Name: "worker1-unlabeled"}}},
			},
			wantErr: "no DPUDevice PCI address shared by DPU nodes: worker1=[]",
		},
		{
			name:    "rejects an empty node set",
			wantErr: "no DPU nodes provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			selected, err := sharedDPUDevicePCIAddress(test.dpuDevicesByNode)
			if test.wantErr != "" {
				g.Expect(err).To(MatchError(test.wantErr))
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(selected).To(Equal(test.wantPCI))
		})
	}
}

func dpuDeviceWithPCIAddress(name, pciAddress string) provisioningv1.DPUDevice {
	return provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				cutil.DPUDevicePCIAddressLabel: pciAddress,
			},
		},
	}
}
