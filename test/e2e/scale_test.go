/*
Copyright 2025 NVIDIA

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
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("DPF scale tests", Labels{scaleLabel}, func() {

	Context("Validate DPU Operator Cleanup", Labels{requiresNodesLabel}, Serial, Ordered, func() {
		It("should validate DPU Operator Cleanup", func() {
			ValidateOperatorFullCreation(ctx, input)
		})
	})
})

func CreateDPUWorkerNodes(ctx context.Context, n int) {
	By("creates nodes in the target cluster")
	// Get the name of the mock-dms pod

	mockDMSPod := &corev1.PodList{}
	Expect(testClient.List(ctx, mockDMSPod, client.InNamespace(dpfOperatorSystemNamespace), client.MatchingLabels{"app.kubernetes.io/instance": "mock-dms"})).To(Succeed())
	Expect(mockDMSPod.Items).To(HaveLen(1))
	mockDMSPodName := mockDMSPod.Items[0].Name

	labels := map[string]string{
		"dpf-operator-e2e-test-cleanup":                        "true",
		"feature.node.kubernetes.io/dpu-deviceID":              "0xa2d6",
		"feature.node.kubernetes.io/dpu-enabled":               "true",
		"feature.node.kubernetes.io/dpu-oob-bridge-configured": "true",
		"e2e.test.io/fake-node":                                "true",
	}
	annotations := map[string]string{
		"provisioning.dpu.nvidia.com/reboot-command":        "skip",
		"provisioning.dpu.nvidia.com/override-dms-pod-name": mockDMSPodName,
		"kwok.x-k8s.io/node":                                "fake",
	}

	// Get the IP address of the kind control plane node

	mockDMSIPAddress := ""
	nodes := &corev1.NodeList{}
	Expect(testClient.List(ctx, nodes)).To(Succeed())
	Expect(nodes.Items).To(HaveLen(1))
	for addr := range nodes.Items[0].Status.Addresses {
		if nodes.Items[0].Status.Addresses[addr].Type == corev1.NodeInternalIP {
			mockDMSIPAddress = nodes.Items[0].Status.Addresses[addr].Address
			break
		}
	}
	Expect(mockDMSIPAddress).ToNot(BeEmpty())

	for i := 0; i < n; i++ {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				// The Node should have the same name as the DPU.
				Name:        fmt.Sprintf("dpu-worker-%d", i),
				Labels:      labels,
				Annotations: annotations,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Node",
				APIVersion: "v1",
			},
		}
		Expect(testClient.Create(ctx, node)).To(Succeed())
		original := node.DeepCopy()
		node.Status.Addresses = []corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: mockDMSIPAddress,
			},
		}
		Expect(testClient.Status().Patch(ctx, node, client.MergeFrom(original))).To(Succeed())
	}
}
