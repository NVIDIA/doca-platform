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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("OVS readiness tests", Labels{Domain.OVSVPC}, Ordered, func() {
	var flowControllerPods []*corev1.Pod

	BeforeAll(func() {
		if !input.hasDpuNodes() {
			Skip("Skip test as there are no DPU nodes")
		}
		provInput := getProvisionDPUClustersInputForOVSVPC(ctx, getProvisionDPUClustersInput(), input.client)
		Expect(provInput.dpuClusters).ToNot(BeEmpty(), "no DPU clusters found via config or discovery")

		By("Creating DPU cluster client for verification")
		getDPUClusterClients(ctx, provInput)
		Expect(dpuClusterClient).ToNot(BeEmpty(), "no DPU cluster clients were created")

		By("Verifying DPU cluster has ready nodes")
		VerifyDPUClusterWithNodes(ctx, provInput)

		By("Waiting for DPU cluster pods to be ready")
		VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
		By("Waiting for DPFOperatorConfig to be ready")
		VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)

		By("Waiting for VPC OVS pods on DPU cluster to be ready")
		VerifyClusterPods(ctx, dpuClusterClient[0], ovsVPCPodsToVerify)

		By("Getting ready flow controller pods")
		flowControllerPods = getReadyFlowControllerPods(ctx, dpuClusterClient[0])
		Expect(flowControllerPods).To(HaveLen(2), "expected 2 ready vpc-ovs-flow-controller pods")
	})

	Context("OVS infrastructure", Ordered, func() {
		It("should be able to execute ovs-vsctl commands", func() {
			verifyOVSCommands(flowControllerPods)
		})

		It("should be able to call gRPC ListVirtualNetworks", func() {
			verifyGRPCListVirtualNetworks(flowControllerPods)
		})
	})
})
