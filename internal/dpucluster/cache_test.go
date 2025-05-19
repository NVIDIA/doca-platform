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

package dpucluster

import (
	"fmt"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestRemoteCache_Reconcile(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// Create a secret which marks envtest as a DPUCluster.
	dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())

	// Create a DPUCluster.
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Eventually(func(g Gomega) {
		gotdpuCluster := &provisioningv1.DPUCluster{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(&dpuCluster), gotdpuCluster)).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	dpuClusterKey := client.ObjectKeyFromObject(&dpuCluster)
	opts := makeRemoteCacheOptions(OptionScheme{Scheme: testEnv.GetScheme()},
		OptionHostClient{Client: testEnv.Manager.GetClient()},
		OptionUserAgent(fmt.Sprintf("test-controller-%s", t.Name())),
		OptionTimeout(10*time.Second))
	rc := &RemoteCache{
		// Use APIReader to avoid cache issues when reading the Cluster object.
		client:    testEnv.Manager.GetAPIReader(),
		options:   opts,
		accessors: make(map[client.ObjectKey]*accessor),
	}

	res, err := rc.Reconcile(ctx, reconcile.Request{NamespacedName: dpuClusterKey})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.IsZero()).To(BeFalse())

	// mark the cluster as ready
	dpuCluster.Status.Phase = provisioningv1.PhaseReady
	g.Expect(testClient.Status().Update(ctx, &dpuCluster)).To(Succeed())

	// Reconcile again, we expect no requeue.
	res, err = rc.Reconcile(ctx, reconcile.Request{NamespacedName: dpuClusterKey})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.IsZero()).To(BeTrue())

	// check that the accessor is created
	g.Expect(rc.accessors).To(HaveKey(dpuClusterKey))

	// Get client and test Get & List
	c, err := rc.GetClient(dpuClusterKey)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(c.Get(ctx, client.ObjectKey{Name: testNS.Name}, &corev1.Namespace{})).To(Succeed())
	nodeList := &provisioningv1.DPUNodeList{}
	g.Expect(c.List(ctx, nodeList)).To(Succeed())
	g.Expect(nodeList.Items).To(BeEmpty())

	// delete the DPUCluster
	g.Expect(testClient.Delete(ctx, &dpuCluster)).To(Succeed())
	res, err = rc.Reconcile(ctx, reconcile.Request{NamespacedName: dpuClusterKey})
	g.Expect(err).ToNot(HaveOccurred())

	// check that the accessor is removed
	g.Expect(rc.accessors).ToNot(HaveKey(dpuClusterKey))
}
