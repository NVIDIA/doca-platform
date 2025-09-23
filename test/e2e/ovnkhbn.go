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
	"time"

	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"
)

func VerifyPerformancePodToPodSameNode(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	hostNamespace := "ovnkhbn-same-node"
	createTestNamespace(ctx, input.client, hostNamespace)

	By("creating test pods")
	pod1Config, pod2Config := getPodSameNodeConfigs(ctx, input, hostNamespace)
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("get pod2 IP")
	pod2IP := netshoot.GetPodIP(ctx, input.client, hostNamespace, pod2Config.Name)

	By("running traffic test between pods")
	netshoot.RunTrafficTest(testRESTClient, input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2IP)
}

func VerifyPerformancePodToPodDifferentNode(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	hostNamespace := "ovnkhbn-different-node"
	createTestNamespace(ctx, input.client, hostNamespace)

	By("creating test pods")
	pod1Config, pod2Config := getPodDifferentNodeConfigs(ctx, input, hostNamespace)
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("get pod2 IP")
	pod2IP := netshoot.GetPodIP(ctx, input.client, hostNamespace, pod2Config.Name)

	By("running traffic test between pods")
	netshoot.RunTrafficTest(testRESTClient, input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2IP)
}

func WaitForOVNKHBNDeploymentReady(ctx context.Context, input *systemTestInput) {
	dpuservice.WaitForDPUDeploymentReady(ctx, input.client, dpfOperatorSystemNamespace, []string{"ovn-hbn"}, 50*time.Minute)
}

func CleanupAfterEachOVNKHBN(ctx context.Context, input *systemTestInput) {
	Expect(utils.CleanupWithLabelAndWait(ctx, input.client, labels.SelectorFromSet(afterEachCleanupLabels), resourcesToDelete...)).To(Succeed())
}

func getPodDifferentNodeConfigs(ctx context.Context, input *systemTestInput, namespace string) (netshoot.TestPodConfig, netshoot.TestPodConfig) {
	workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.client)

	pod1Config := netshoot.TestPodConfig{
		Name:      "pod1",
		Namespace: namespace,
		NodeName:  workerNode1,
	}
	pod2Config := netshoot.TestPodConfig{
		Name:      "pod2",
		Namespace: namespace,
		NodeName:  workerNode2,
	}

	return pod1Config, pod2Config
}

func getPodSameNodeConfigs(ctx context.Context, input *systemTestInput, namespace string) (netshoot.TestPodConfig, netshoot.TestPodConfig) {
	workerNode1, _ := getTwoWorkerNodeNames(ctx, input.client)

	pod1Config := netshoot.TestPodConfig{
		Name:      "pod1",
		Namespace: namespace,
		NodeName:  workerNode1,
	}
	pod2Config := netshoot.TestPodConfig{
		Name:      "pod2",
		Namespace: namespace,
		NodeName:  workerNode1,
	}

	return pod1Config, pod2Config
}
