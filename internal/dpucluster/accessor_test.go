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
	"context"
	"fmt"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func TestConnect(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// Create a secret which marks envtest as a DPUCluster.
	dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())

	clusterKey := client.ObjectKeyFromObject(&dpuCluster)

	// Create a DPUCluster.
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Eventually(func(g Gomega) {
		gotdpuCluster := &provisioningv1.DPUCluster{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(&dpuCluster), gotdpuCluster)).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())
	objs := []client.Object{kamajiSecret, &dpuCluster}
	defer func() {
		g.Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
		g.Expect(testClient.Delete(ctx, testNS)).To(Succeed())
	}()

	accessor := newAccessor(clusterKey)
	opts := makeRemoteCacheOptions(OptionScheme{Scheme: testEnv.GetScheme()},
		OptionHostClient{Client: testEnv.Manager.GetClient()},
		OptionUserAgent(fmt.Sprintf("test-controller-%s", t.Name())),
		OptionTimeout(10*time.Second))

	g.Expect(accessor.connect(ctx, opts)).To(Succeed())
	g.Expect(accessor.isConnected()).To(BeTrue())
	g.Expect(accessor.state.connection.cachedClient).ToNot(BeNil())
	g.Expect(accessor.state.connection.cache).ToNot(BeNil())
	g.Expect(accessor.state.connection.watches).ToNot(BeNil())

	// Check if cache was started / synced
	cacheSyncCtx, cacheSyncCtxCancel := context.WithTimeout(ctx, 5*time.Second)
	defer cacheSyncCtxCancel()
	g.Expect(accessor.state.connection.cache.WaitForCacheSync(cacheSyncCtx)).To(BeTrue())

	// Get client and test Get & List
	c, err := accessor.getClient()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(c.Get(ctx, client.ObjectKey{Name: testNS.Name}, &corev1.Namespace{})).To(Succeed())
	nodeList := &provisioningv1.DPUNodeList{}
	g.Expect(c.List(ctx, nodeList)).To(Succeed())
	g.Expect(nodeList.Items).To(BeEmpty())

	// Connect again (no-op)
	g.Expect(accessor.connect(ctx, opts)).To(Succeed())

	// Disconnect
	accessor.disconnect()
	g.Expect(accessor.isConnected()).To(BeFalse())
}

func TestDisconnect(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// Create a secret which marks envtest as a DPUCluster.
	dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())

	clusterKey := client.ObjectKeyFromObject(&dpuCluster)

	// Create a DPUCluster.
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Eventually(func(g Gomega) {
		gotdpuCluster := &provisioningv1.DPUCluster{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(&dpuCluster), gotdpuCluster)).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())
	objs := []client.Object{kamajiSecret, &dpuCluster}
	defer func() {
		g.Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
		g.Expect(testClient.Delete(ctx, testNS)).To(Succeed())
	}()

	accessor := newAccessor(clusterKey)
	opts := makeRemoteCacheOptions(OptionScheme{Scheme: testEnv.GetScheme()},
		OptionHostClient{Client: testEnv.Manager.GetClient()},
		OptionUserAgent(fmt.Sprintf("test-controller-%s", t.Name())),
		OptionTimeout(10*time.Second))

	g.Expect(accessor.connect(ctx, opts)).To(Succeed())
	g.Expect(accessor.isConnected()).To(BeTrue())
	g.Expect(accessor.state.connection.cachedClient).ToNot(BeNil())
	g.Expect(accessor.state.connection.cache).ToNot(BeNil())
	g.Expect(accessor.state.connection.watches).ToNot(BeNil())

	// Disconnect
	accessor.disconnect()
	g.Expect(accessor.isConnected()).To(BeFalse())

	// Disconnect again (no-op)
	accessor.disconnect()
	g.Expect(accessor.isConnected()).To(BeFalse())
}

func TestWatch(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// Create a secret which marks envtest as a DPUCluster.
	dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())

	clusterKey := client.ObjectKeyFromObject(&dpuCluster)

	// Create a DPUCluster.
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Eventually(func(g Gomega) {
		gotdpuCluster := &provisioningv1.DPUCluster{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(&dpuCluster), gotdpuCluster)).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())
	objs := []client.Object{kamajiSecret, &dpuCluster}
	defer func() {
		g.Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
		g.Expect(testClient.Delete(ctx, testNS)).To(Succeed())
	}()

	accessor := newAccessor(clusterKey)
	opts := makeRemoteCacheOptions(OptionScheme{Scheme: testEnv.GetScheme()},
		OptionHostClient{Client: testEnv.Manager.GetClient()},
		OptionUserAgent(fmt.Sprintf("test-controller-%s", t.Name())),
		OptionTimeout(10*time.Second))

	tw := &testWatcher{}
	wi := WatcherOptions{
		Name:         "test-watch",
		Watcher:      tw,
		Kind:         &provisioningv1.DPUNode{},
		EventHandler: &handler.EnqueueRequestForObject{},
	}

	// Add watch when not connected (fails)
	g.Expect(accessor.watch(ctx, NewWatcher(wi))).To(MatchError(ErrDPUClusterNotConnected))

	g.Expect(accessor.connect(ctx, opts)).To(Succeed())
	g.Expect(accessor.isConnected()).To(BeTrue())
	g.Expect(accessor.state.connection.watches).To(BeEmpty())

	g.Expect(accessor.watch(ctx, NewWatcher(wi))).To(Succeed())
	g.Expect(accessor.state.connection.watches.Has("test-watch")).To(BeTrue())
	g.Expect(accessor.state.connection.watches.Len()).To(Equal(1))

	// Add watch again (no-op as watch already exists)
	g.Expect(accessor.watch(ctx, NewWatcher(wi))).To(Succeed())
	g.Expect(accessor.state.connection.watches.Has("test-watch")).To(BeTrue())
	g.Expect(accessor.state.connection.watches.Len()).To(Equal(1))

	// Disconnect
	accessor.disconnect()
	g.Expect(accessor.isConnected()).To(BeFalse())
}

type testWatcher struct {
	watchedSources []source.Source
}

func (w *testWatcher) Watch(src source.Source) error {
	w.watchedSources = append(w.watchedSources, src)
	return nil
}
