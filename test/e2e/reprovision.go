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
	"context"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateHostTrustedDPUReprovision deletes every DPU CR in the operator
// namespace and waits for the existing DPUSet(s) to recreate them Ready.
//
// This is the OnDelete-style reprovision path: the DPUSet is left in place so
// the controller issues a new DPU per device. It is host-trusted only (drain
// node effect). Zero Trust hold/reboot is not driven here.
func ValidateHostTrustedDPUReprovision(ctx context.Context, input *systemTestInput) {
	// Destructive OnDelete reprovision. Domain.OCP is additive, so physical and
	// cloud DPFSystem jobs also select this spec. Skip there so they do not
	// pay a second full provisioning cycle on top of DPUDeployment full creation.
	if !isGinkgoLabelApplied(Domain.OCP) {
		Skip("Skip host-trusted DPU CR reprovision outside the OCP suite")
	}
	// RequiresNodes only makes BeforeEach wait when nodes exist; quick e2e still
	// selects this spec (numberOfDPUNodes: 0) and must Skip rather than Fail.
	if !input.hasDpuNodes() {
		Skip("Test requires provisioned DPU nodes")
	}

	expectedDPUs := input.totalDPUs()

	By("Recording the DPUSet(s) that must survive the reprovision")
	dpuSetList := &provisioningv1.DPUSetList{}
	Expect(input.client.List(ctx, dpuSetList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	Expect(dpuSetList.Items).NotTo(BeEmpty(), "expected at least one DPUSet to recreate the DPUs")
	dpuSetNames := make([]string, 0, len(dpuSetList.Items))
	for i := range dpuSetList.Items {
		dpuSetNames = append(dpuSetNames, dpuSetList.Items[i].Name)
	}

	By("Listing Ready DPUs before delete")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	Expect(dpuList.Items).To(HaveLen(expectedDPUs), "expected %d DPU CRs before reprovision", expectedDPUs)

	type dpuRecord struct {
		oldUID      string
		deviceLabel string
	}
	before := make([]dpuRecord, 0, len(dpuList.Items))
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]
		Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady), "DPU %s must be Ready before reprovision", dpu.Name)
		deviceLabel := dpu.GetLabels()[cutil.DPUDeviceNameLabel]
		Expect(deviceLabel).NotTo(BeEmpty(), "DPU %s must have %s", dpu.Name, cutil.DPUDeviceNameLabel)
		before = append(before, dpuRecord{oldUID: string(dpu.GetUID()), deviceLabel: deviceLabel})
	}

	By(fmt.Sprintf("Deleting %d DPU CRs (keeping DPUSet(s) %v)", len(before), dpuSetNames))
	for i := range dpuList.Items {
		Expect(client.IgnoreNotFound(input.client.Delete(ctx, &dpuList.Items[i]))).To(Succeed())
	}

	By("Waiting for DPUSet(s) to recreate each DPU with a new UID and Ready phase")
	Eventually(func(g Gomega) {
		for _, recorded := range before {
			updated := &provisioningv1.DPUList{}
			g.Expect(input.client.List(ctx, updated,
				client.InNamespace(dpfOperatorSystemNamespace),
				client.MatchingLabels{cutil.DPUDeviceNameLabel: recorded.deviceLabel},
			)).To(Succeed())
			g.Expect(updated.Items).To(HaveLen(1), "DPU for device %s should be recreated", recorded.deviceLabel)
			dpu := &updated.Items[0]
			g.Expect(dpu.GetDeletionTimestamp()).To(BeNil(), "DPU for device %s should not be deleting", recorded.deviceLabel)
			g.Expect(string(dpu.GetUID())).NotTo(Equal(recorded.oldUID), "DPU for device %s should have a new UID", recorded.deviceLabel)
			g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady), "DPU %s should be Ready", dpu.Name)
		}
	}).WithTimeout(provisioningTimeout).WithPolling(1 * time.Second).Should(Succeed())

	By("Confirming the original DPUSet(s) still exist")
	for _, name := range dpuSetNames {
		Expect(input.client.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      name,
		}, &provisioningv1.DPUSet{})).To(Succeed())
	}

	By("Waiting for the recreated DPUs to join the DPU cluster as Nodes")
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(expectedDPUs),
			"DPU cluster should have %d nodes after reprovision, found %d", expectedDPUs, len(nodes.Items))
	}).WithTimeout(provisioningTimeout).WithPolling(1 * time.Second).Should(Succeed())
}
