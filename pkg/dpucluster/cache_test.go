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
	"sync"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachineryversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
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

	// Create a mock watcher callback to test callback functionality
	callbackInvoked := false
	mockWatcherCallback := func(ctx context.Context, c client.Client, cluster client.ObjectKey) (Watcher, error) {
		callbackInvoked = true
		g.Expect(cluster).To(Equal(dpuClusterKey))
		g.Expect(c).ToNot(BeNil())

		// Return a mock watcher
		return &mockWatcher{name: "test-watcher"}, nil
	}

	opts := makeRemoteCacheOptions(OptionScheme{Scheme: testEnv.GetScheme()},
		OptionHostClient{Client: testEnv.Manager.GetClient()},
		OptionUserAgent{UserAgent: fmt.Sprintf("test-controller-%s", t.Name())},
		OptionTimeout{Timeout: 10 * time.Second},
		OptionRequeueAfter{RequeueAfter: 10 * time.Second},
		OptionGetWatcherCallbacks{GetWatcherCallbacks: []GetWatcherCallback{mockWatcherCallback}})
	rc := &RemoteCache{
		// Use APIReader to avoid cache issues when reading the Cluster object.
		client:    testEnv.Manager.GetAPIReader(),
		options:   opts,
		accessors: make(map[client.ObjectKey]*accessor),
	}

	res, err := rc.Reconcile(ctx, reconcile.Request{NamespacedName: dpuClusterKey})
	g.Expect(err).To(HaveOccurred())
	g.Expect(res.IsZero()).To(BeTrue())

	// mark the cluster as ready
	dpuCluster.Status.Phase = provisioningv1.PhaseReady
	g.Expect(testClient.Status().Update(ctx, &dpuCluster)).To(Succeed())

	// Reconcile again, we expect a requeue after 10 seconds
	res, err = rc.Reconcile(ctx, reconcile.Request{NamespacedName: dpuClusterKey})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.RequeueAfter).To(BeNumerically("~", 10*time.Second, 1*time.Second))

	// check that the accessor is created
	g.Expect(rc.accessors).To(HaveKey(dpuClusterKey))

	// verify the watch callback was invoked
	g.Expect(callbackInvoked).To(BeTrue())

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
	g.Expect(res.IsZero()).To(BeTrue())

	// check that the accessor is removed
	g.Expect(rc.accessors).ToNot(HaveKey(dpuClusterKey))
}

// mockWatcher is a simple mock implementation of the Watcher interface for testing
type mockWatcher struct {
	name string
}

func (m *mockWatcher) Name() string {
	return m.name
}

func (m *mockWatcher) Object() client.Object {
	return &corev1.Pod{}
}

func (m *mockWatcher) Watch(cache.Cache) error {
	return nil
}

// testConnection returns a connected accessor and the cancel used when it disconnects.
func testConnection(health *Health, watches map[string]Watcher) (*accessor, context.CancelFunc) {
	_, cancel := context.WithCancel(context.Background())
	if watches == nil {
		watches = map[string]Watcher{}
	}
	return &accessor{
		health: health,
		state: accessorState{
			connection: &accessorConnectionState{
				cachedClient: noopClient{},
				cache:        &cacheWithCancel{cancelFunc: cancel},
				watches:      watches,
			},
		},
	}, cancel
}

// exhaustedHealth returns a health checker whose next Check reports err.
func exhaustedHealth(err error) *Health {
	health := NewHealthServer(nil, DefaultMaxBackoff, 0, time.Second)
	health.attempt = 1
	health.lastCheckErrror = err
	health.status = HealthStatusUnhealthy
	return health
}

// failServerVersion is a discovery client whose version probe fails.
type failServerVersion struct{}

// ServerVersion returns an error so the health check records a failed probe.
func (failServerVersion) ServerVersion() (*apimachineryversion.Info, error) {
	return nil, fmt.Errorf("unreachable")
}

// TestRemoteCache_NotifyDisconnect checks that each cluster notifies every
// running watch and that a later watch with the same name replaces the earlier one.
func TestRemoteCache_NotifyDisconnect(t *testing.T) {
	g := NewWithT(t)
	clusterA := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	clusterB := client.ObjectKey{Namespace: "tenant", Name: "cluster-b"}
	nodeReq := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "dpu-1"}}
	podReq := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "pod-1"}}
	otherReq := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "other"}}

	dpuController := newFakeTypedController[reconcile.Request]()
	serviceController := newFakeTypedController[reconcile.Request]()
	t.Cleanup(dpuController.queue.ShutDown)
	t.Cleanup(serviceController.queue.ShutDown)
	rc := &RemoteCache{}

	accA, cancelA := testConnection(nil, map[string]Watcher{
		"dpu-nodes": NewWatcher(WatcherOptions{
			Name:    "dpu-nodes",
			Watcher: dpuController,
			Kind:    &corev1.Node{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "stale"}}}
			},
		}),
		// A typed object still runs.
		"service-pods": NewWatcher(TypedWatcherOptions[*corev1.Pod, reconcile.Request]{
			Name:    "service-pods",
			Watcher: serviceController,
			Kind:    &corev1.Pod{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{podReq}
			},
		}),
		"ignored": NewWatcher(WatcherOptions{
			Name:    "ignored",
			Watcher: dpuController,
			Kind:    &corev1.Node{},
		}),
	})
	t.Cleanup(cancelA)
	accB, cancelB := testConnection(nil, map[string]Watcher{
		"other": NewWatcher(WatcherOptions{
			Name:    "other",
			Watcher: dpuController,
			Kind:    &corev1.Node{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{otherReq}
			},
		}),
	})
	t.Cleanup(cancelB)
	rc.accessors = map[client.ObjectKey]*accessor{clusterA: accA, clusterB: accB}

	// Replacing the same watch name keeps the latest handler.
	g.Expect(accA.watch(context.Background(), NewWatcher(WatcherOptions{
		Name:    "dpu-nodes",
		Watcher: dpuController,
		Kind:    &corev1.Node{},
		DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
			return []reconcile.Request{nodeReq}
		},
	}))).To(Succeed())

	rc.notifyDisconnect(context.Background(), clusterA, rc.deleteAccessor(clusterA))
	g.Expect(drainQueue(dpuController.queue)).To(ConsistOf(nodeReq))
	g.Expect(drainQueue(serviceController.queue)).To(ConsistOf(podReq))
	g.Expect(dpuController.watches).To(Equal(1))
	g.Expect(serviceController.watches).To(Equal(1))

	rc.notifyDisconnect(context.Background(), clusterB, rc.deleteAccessor(clusterB))
	g.Expect(drainQueue(dpuController.queue)).To(ConsistOf(otherReq))
	g.Expect(dpuController.watches).To(Equal(1))
}

// TestRemoteCache_DisconnectDelete checks that deleting a dpu cluster drops
// its handlers and leaves other clusters registered.
func TestRemoteCache_DisconnectDelete(t *testing.T) {
	g := NewWithT(t)
	clusterA := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	clusterB := client.ObjectKey{Namespace: "tenant", Name: "cluster-b"}
	want := requestWithCluster{
		Request: reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "event-1"}},
		cluster: clusterA,
	}
	kept := requestWithCluster{
		Request: reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "event-2"}},
		cluster: clusterB,
	}

	eventController := newFakeTypedController[requestWithCluster]()
	t.Cleanup(eventController.queue.ShutDown)
	accA, cancelA := testConnection(nil, map[string]Watcher{
		"event-watch": NewWatcher(TypedWatcherOptions[client.Object, requestWithCluster]{
			Name:    "event-watch",
			Watcher: eventController,
			Kind:    &corev1.Event{},
			DisconnectHandler: func(_ context.Context, cluster client.ObjectKey) []requestWithCluster {
				g.Expect(cluster).To(Equal(clusterA))
				return []requestWithCluster{want}
			},
		}),
	})
	t.Cleanup(cancelA)
	accB, cancelB := testConnection(nil, map[string]Watcher{
		"event-watch": NewWatcher(TypedWatcherOptions[client.Object, requestWithCluster]{
			Name:    "event-watch",
			Watcher: eventController,
			Kind:    &corev1.Event{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []requestWithCluster {
				return []requestWithCluster{kept}
			},
		}),
	})
	t.Cleanup(cancelB)
	rc := &RemoteCache{accessors: map[client.ObjectKey]*accessor{clusterA: accA, clusterB: accB}}

	_, err := rc.reconcileDelete(clusterA)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(eventController.queue.Len()).To(Equal(0))
	g.Expect(rc.accessors).ToNot(HaveKey(clusterA))

	rc.notifyDisconnect(context.Background(), clusterB, rc.deleteAccessor(clusterB))
	g.Expect(drainQueue(eventController.queue)).To(ConsistOf(kept))
}

// TestRemoteCache_ExhaustedHealthNotifiesRunningWatches checks that a failed
// probe does not disconnect, and that a later exhausted check notifies the
// watches stored by the earlier reconcile.
func TestRemoteCache_ExhaustedHealthNotifiesRunningWatches(t *testing.T) {
	g := NewWithT(t)
	clusterKey := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	want := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "dpu-1"}}

	controller := newFakeTypedController[reconcile.Request]()
	t.Cleanup(controller.queue.ShutDown)

	acc, cancel := testConnection(NewHealthServer(failServerVersion{}, DefaultMaxBackoff, 0, time.Second), nil)
	t.Cleanup(cancel)
	rc := &RemoteCache{
		options: &Options{
			getWatcherCallbacks: []GetWatcherCallback{
				func(context.Context, client.Client, client.ObjectKey) (Watcher, error) {
					return NewWatcher(WatcherOptions{
						Name:    "dpu-nodes",
						Watcher: controller,
						Kind:    &corev1.Node{},
						DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
							return []reconcile.Request{want}
						},
					}), nil
				},
			},
		},
		accessors: map[client.ObjectKey]*accessor{clusterKey: acc},
	}

	cluster := &provisioningv1.DPUCluster{}
	cluster.Name = clusterKey.Name
	cluster.Namespace = clusterKey.Namespace
	_, err := rc.reconcile(context.Background(), cluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rc.accessors).To(HaveKey(clusterKey))
	g.Expect(acc.state.connection.watches).To(HaveKey("dpu-nodes"))
	g.Expect(controller.queue.Len()).To(Equal(0))

	_, err = rc.reconcile(context.Background(), cluster)
	g.Expect(err).To(MatchError("unreachable"))
	g.Expect(rc.accessors).ToNot(HaveKey(clusterKey))
	g.Expect(drainQueue(controller.queue)).To(ConsistOf(want))
}

// TestRemoteCache_NilHandlerClearsPrevious checks that registering the same
// watch again without a DisconnectHandler removes the previous handler.
func TestRemoteCache_NilHandlerClearsPrevious(t *testing.T) {
	g := NewWithT(t)
	cluster := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	kept := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "pod-1"}}

	controller := newFakeTypedController[reconcile.Request]()
	t.Cleanup(controller.queue.ShutDown)
	acc, cancel := testConnection(nil, map[string]Watcher{
		"dpu-nodes": NewWatcher(WatcherOptions{
			Name:    "dpu-nodes",
			Watcher: controller,
			Kind:    &corev1.Node{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "stale"}}}
			},
		}),
		"service-pods": NewWatcher(WatcherOptions{
			Name:    "service-pods",
			Watcher: controller,
			Kind:    &corev1.Pod{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{kept}
			},
		}),
	})
	t.Cleanup(cancel)
	g.Expect(acc.watch(context.Background(), NewWatcher(WatcherOptions{
		Name:    "dpu-nodes",
		Watcher: controller,
		Kind:    &corev1.Node{},
	}))).To(Succeed())

	rc := &RemoteCache{accessors: map[client.ObjectKey]*accessor{cluster: acc}}
	rc.notifyDisconnect(context.Background(), cluster, rc.deleteAccessor(cluster))
	g.Expect(drainQueue(controller.queue)).To(ConsistOf(kept))
}

// TestRemoteCache_DisconnectBeforeStart checks that requests returned before
// the controller starts the source are enqueued when Start runs.
func TestRemoteCache_DisconnectBeforeStart(t *testing.T) {
	g := NewWithT(t)
	cluster := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	want := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "dpu-1"}}
	controller := &deferredController{}
	rc := &RemoteCache{}
	rc.notifyDisconnect(context.Background(), cluster, []Watcher{
		NewWatcher(WatcherOptions{
			Name:    "dpu-nodes",
			Watcher: controller,
			Kind:    &corev1.Node{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{want}
			},
		}),
	})

	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[reconcile.Request](),
		workqueue.TypedRateLimitingQueueConfig[reconcile.Request]{Name: "test"},
	)
	t.Cleanup(queue.ShutDown)
	g.Expect(controller.sources[0].Start(context.Background(), queue)).To(Succeed())
	g.Expect(drainQueue(queue)).To(ConsistOf(want))
}

// TestRemoteCache_NonComparableWatcher checks that a watcher which cannot be a
// map key returns an error instead of panicking.
func TestRemoteCache_NonComparableWatcher(t *testing.T) {
	g := NewWithT(t)
	cluster := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	rc := &RemoteCache{}
	nodes := NewWatcher(WatcherOptions{
		Name:    "dpu-nodes",
		Watcher: nonComparableWatcher{},
		Kind:    &corev1.Node{},
		DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
			return nil
		},
	}).(*watcher[client.Object, reconcile.Request])
	g.Expect(nodes.invokeDisconnect(context.Background(), cluster, rc)).To(MatchError(ContainSubstring("not comparable")))

	withoutHandler := NewWatcher(WatcherOptions{
		Name:    "dpu-nodes",
		Watcher: nonComparableWatcher{},
		Kind:    &corev1.Node{},
	}).(*watcher[client.Object, reconcile.Request])
	g.Expect(withoutHandler.invokeDisconnect(context.Background(), cluster, rc)).To(Succeed())
}

// TestRemoteCache_FailedWatchIsNotReused checks that a second registration does
// not keep a source when the first Watch call fails.
func TestRemoteCache_FailedWatchIsNotReused(t *testing.T) {
	g := NewWithT(t)
	controller := &failFirstWatchController{}
	rc := &RemoteCache{}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ensureDisconnectSource(rc, controller)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		g.Expect(err).To(MatchError(ContainSubstring("watch failed")))
	}
	g.Expect(rc.disconnectSources).To(BeEmpty())

	src, err := ensureDisconnectSource(rc, controller)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(src).NotTo(BeNil())
}

// TestRemoteCache_ClearedHandlerSkipsStoredWatcher checks that a health-check
// failure does not run a watcher stored on the connection after its handler
// was cleared.
func TestRemoteCache_ClearedHandlerSkipsStoredWatcher(t *testing.T) {
	g := NewWithT(t)
	clusterKey := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	controller := newFakeTypedController[reconcile.Request]()
	t.Cleanup(controller.queue.ShutDown)

	acc, cancel := testConnection(exhaustedHealth(fmt.Errorf("unreachable")), map[string]Watcher{
		"dpu-nodes": NewWatcher(WatcherOptions{
			Name:    "dpu-nodes",
			Watcher: controller,
			Kind:    &corev1.Node{},
			DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "stale"}}}
			},
		}),
	})
	t.Cleanup(cancel)
	g.Expect(acc.watch(context.Background(), NewWatcher(WatcherOptions{
		Name:    "dpu-nodes",
		Watcher: controller,
		Kind:    &corev1.Node{},
	}))).To(Succeed())
	rc := &RemoteCache{accessors: map[client.ObjectKey]*accessor{clusterKey: acc}}

	cluster := &provisioningv1.DPUCluster{}
	cluster.Name = clusterKey.Name
	cluster.Namespace = clusterKey.Namespace
	_, err := rc.reconcile(context.Background(), cluster)
	g.Expect(err).To(MatchError("unreachable"))
	g.Expect(controller.queue.Len()).To(Equal(0))
}

// TestRemoteCache_WatchRegistersDisconnect checks that Watch registers a
// DisconnectHandler, and that another watch with no handler leaves it in place.
func TestRemoteCache_WatchRegistersDisconnect(t *testing.T) {
	g := NewWithT(t)
	cluster := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	want := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "dpf", Name: "dpu-1"}}
	controller := &deferredController{}
	acc, cancel := testConnection(nil, nil)
	t.Cleanup(cancel)
	rc := &RemoteCache{accessors: map[client.ObjectKey]*accessor{cluster: acc}}

	g.Expect(rc.Watch(context.Background(), cluster, NewWatcher(WatcherOptions{
		Name:    "dpu-nodes",
		Watcher: controller,
		Kind:    &corev1.Node{},
		DisconnectHandler: func(context.Context, client.ObjectKey) []reconcile.Request {
			return []reconcile.Request{want}
		},
	}))).To(Succeed())
	g.Expect(rc.Watch(context.Background(), cluster, NewWatcher(WatcherOptions{
		Name:    "pods",
		Watcher: controller,
		Kind:    &corev1.Pod{},
	}))).To(Succeed())

	running := make([]Watcher, 0, len(acc.state.connection.watches))
	for _, watcher := range acc.state.connection.watches {
		running = append(running, watcher)
	}
	rc.notifyDisconnect(context.Background(), cluster, running)
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[reconcile.Request](),
		workqueue.TypedRateLimitingQueueConfig[reconcile.Request]{Name: "test"},
	)
	t.Cleanup(queue.ShutDown)
	var src *requestSource[reconcile.Request]
	for _, candidate := range controller.sources {
		if got, ok := candidate.(*requestSource[reconcile.Request]); ok {
			src = got
		}
	}
	g.Expect(src).NotTo(BeNil())
	g.Expect(src.Start(context.Background(), queue)).To(Succeed())
	g.Expect(drainQueue(queue)).To(ConsistOf(want))
}

// TestRemoteCache_WatcherCallbackErrorStopsLaterWatches checks that a callback
// error returns before later watches are registered and leaves the connection up.
func TestRemoteCache_WatcherCallbackErrorStopsLaterWatches(t *testing.T) {
	g := NewWithT(t)
	clusterKey := client.ObjectKey{Namespace: "tenant", Name: "cluster-a"}
	acc := &accessor{
		health: NewHealthServer(serverVersion{}, DefaultMaxBackoff, DefaultMaxRetries, time.Second),
		state: accessorState{
			connection: &accessorConnectionState{
				cachedClient: noopClient{},
				watches:      map[string]Watcher{},
			},
		},
	}
	rc := &RemoteCache{
		options: &Options{
			getWatcherCallbacks: []GetWatcherCallback{
				func(context.Context, client.Client, client.ObjectKey) (Watcher, error) {
					return nil, fmt.Errorf("operator config missing")
				},
				func(context.Context, client.Client, client.ObjectKey) (Watcher, error) {
					return &mockWatcher{name: "nodes"}, nil
				},
			},
		},
		accessors: map[client.ObjectKey]*accessor{clusterKey: acc},
	}

	cluster := &provisioningv1.DPUCluster{}
	cluster.Name = clusterKey.Name
	cluster.Namespace = clusterKey.Namespace
	_, err := rc.reconcile(context.Background(), cluster)
	g.Expect(err).To(MatchError(ContainSubstring("operator config missing")))
	g.Expect(rc.accessors).To(HaveKey(clusterKey))
	g.Expect(acc.state.connection.watches).To(BeEmpty())
}

// serverVersion reports a reachable API server to the health check.
type serverVersion struct{}

// ServerVersion returns a version so the health check succeeds.
func (serverVersion) ServerVersion() (*apimachineryversion.Info, error) {
	return &apimachineryversion.Info{}, nil
}

// noopClient is returned by a test accessor and is not used.
type noopClient struct {
	client.Client
}

// requestWithCluster mirrors a controller reconcile request that carries its cluster.
type requestWithCluster struct {
	reconcile.Request
	cluster client.ObjectKey
}

// fakeTypedController starts a disconnect source on the controller queue.
type fakeTypedController[request comparable] struct {
	queue   workqueue.TypedRateLimitingInterface[request]
	watches int
}

// newFakeTypedController returns a controller whose Watch starts sources on its queue.
func newFakeTypedController[request comparable]() *fakeTypedController[request] {
	return &fakeTypedController[request]{
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[request](),
			workqueue.TypedRateLimitingQueueConfig[request]{Name: "test"},
		),
	}
}

// Watch records the source. A disconnect source is started on the controller queue.
func (f *fakeTypedController[request]) Watch(src source.TypedSource[request]) error {
	f.watches++
	disconnect, ok := src.(*requestSource[request])
	if !ok {
		return nil
	}
	return disconnect.Start(context.Background(), f.queue)
}

// failFirstWatchController fails its first Watch so a concurrent registration
// can observe the in-flight source.
type failFirstWatchController struct {
	mu    sync.Mutex
	calls int
}

// Watch fails once, then succeeds.
func (f *failFirstWatchController) Watch(source.TypedSource[reconcile.Request]) error {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	if call == 1 {
		time.Sleep(20 * time.Millisecond)
		return fmt.Errorf("watch failed")
	}
	return nil
}

// deferredController records sources without starting them.
type deferredController struct {
	sources []source.TypedSource[reconcile.Request]
}

// Watch stores src so the test can start it after disconnect.
func (d *deferredController) Watch(src source.TypedSource[reconcile.Request]) error {
	d.sources = append(d.sources, src)
	return nil
}

// nonComparableWatcher is a SourceWatcher whose value cannot be a map key.
type nonComparableWatcher struct {
	hooks []func()
}

// Watch implements SourceWatcher and does not start a source.
func (w nonComparableWatcher) Watch(source.TypedSource[reconcile.Request]) error {
	if len(w.hooks) > 0 {
		return fmt.Errorf("unexpected hooks")
	}
	return nil
}

// drainQueue returns every item currently on q.
func drainQueue[request comparable](q workqueue.TypedRateLimitingInterface[request]) []request {
	var got []request
	for q.Len() > 0 {
		item, shutdown := q.Get()
		if shutdown {
			break
		}
		got = append(got, item)
		q.Done(item)
	}
	return got
}
