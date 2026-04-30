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

package vpc

import (
	"context"
	"time"

	"github.com/nvidia/doca-platform/test/e2e/cleanup"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeployDHCPDaemon creates the host-side CNI DHCP DaemonSet from the given spec (typically from applyVPCOVNConfig / applyVPCOVSConfig).
// The DHCP daemon creates /run/cni/dhcp.sock on each node, which the DHCP CNI IPAM
// plugin uses to obtain overlay IPs from the DPU's DHCP agent.
// cleanupLabels are merged into the DaemonSet and pod template so named-scope cleanup
// can delete the workload (see cleanup.Tracker.executeCleanup label selector).
func DeployDHCPDaemon(ctx context.Context, testClient client.Client, ds *appsv1.DaemonSet, cleanupLabels map[string]string) *appsv1.DaemonSet {
	toCreate := ds.DeepCopy()
	if len(cleanupLabels) > 0 {
		toCreate.Labels = cleanup.MergeMaps(toCreate.Labels, cleanupLabels)
		toCreate.Spec.Template.Labels = cleanup.MergeMaps(toCreate.Spec.Template.Labels, cleanupLabels)
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, toCreate))).To(Succeed())
	return toCreate
}

// WaitForDHCPDaemonReady waits until the DHCP DaemonSet has all pods ready.
func WaitForDHCPDaemonReady(ctx context.Context, testClient client.Client, ds *appsv1.DaemonSet) {
	Eventually(func(g Gomega) {
		current := &appsv1.DaemonSet{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: ds.GetNamespace(),
			Name:      ds.GetName(),
		}, current)).To(Succeed())
		g.Expect(current.Status.ObservedGeneration).To(Equal(current.GetGeneration()))
		g.Expect(current.Status.NumberReady).To(BeNumerically(">", 0))
		g.Expect(current.Status.NumberReady).To(Equal(current.Status.DesiredNumberScheduled))
	}).WithTimeout(5 * time.Minute).Should(Succeed())
}
