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

package statusmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testDPUName      = "test-dpu"
	testDPUNamespace = "test-ns"
	testDPUUID       = "test-uid"
)

func newTestDPU(uid types.UID) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: testDPUName, Namespace: testDPUNamespace, UID: uid},
	}
}

// newTestClient returns a fake client holding objs. funcs may be empty.
func newTestClient(funcs interceptor.Funcs, objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&provisioningv1.DPU{}).
		WithInterceptorFuncs(funcs).
		Build()
}

// startManager starts m with a short retry interval and stops it when the spec ends.
func startManager(m *Manager) {
	m.retryInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	m.Start(ctx)
}

func getAgentStatus(c client.Client) *provisioningv1.AgentStatus {
	dpu := &provisioningv1.DPU{}
	Expect(c.Get(context.Background(), client.ObjectKey{Namespace: testDPUNamespace, Name: testDPUName}, dpu)).To(Succeed())
	return dpu.Status.AgentStatus
}

func setCondition(s *provisioningv1.AgentStatus, condType string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{Type: condType, Status: metav1.ConditionTrue, Reason: condType})
}

// patchGate makes the first status patch wait until release is closed, so specs can queue
// requests while the worker is busy.
type patchGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
	active  atomic.Int32
	// maxActive is the largest number of patches seen running at the same time.
	maxActive atomic.Int32
	// failCalls lists 1-based patch calls that return an error.
	failCalls map[int32]bool
}

func newPatchGate(failCalls ...int32) *patchGate {
	g := &patchGate{entered: make(chan struct{}), release: make(chan struct{}), failCalls: map[int32]bool{}}
	for _, call := range failCalls {
		g.failCalls[call] = true
	}
	return g
}

func (g *patchGate) funcs() interceptor.Funcs {
	return interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, subResource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			call := g.calls.Add(1)
			active := g.active.Add(1)
			defer g.active.Add(-1)
			for {
				maxActive := g.maxActive.Load()
				if active <= maxActive || g.maxActive.CompareAndSwap(maxActive, active) {
					break
				}
			}
			if call == 1 {
				g.once.Do(func() { close(g.entered) })
				select {
				case <-g.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if g.failCalls[call] {
				return fmt.Errorf("injected patch failure %d", call)
			}
			return c.SubResource(subResource).Patch(ctx, obj, patch, opts...)
		},
	}
}

// pendingCount returns the number of requests waiting for a push.
func pendingCount(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// updateRemoteAsync calls UpdateRemote in a goroutine and returns its result channel.
func updateRemoteAsync(m *Manager, untilSuccess bool) <-chan error {
	result := make(chan error, 1)
	go func() { result <- m.UpdateRemote(untilSuccess) }()
	return result
}

var _ = Describe("Manager", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("GetLocal and UpdateLocal", func() {
		It("returns a copy that callers cannot use to change the status", func() {
			m := New(newTestClient(interceptor.Funcs{}), testDPUNamespace, testDPUName, "")
			m.UpdateLocal(func(s *provisioningv1.AgentStatus) { setCondition(s, "A") })

			got := m.GetLocal()
			got.Conditions[0].Type = "changed"
			got.KubeletVersion = ptr.To("changed")

			Expect(m.GetLocal().Conditions[0].Type).To(Equal("A"))
			Expect(m.GetLocal().KubeletVersion).To(BeNil())
		})

		It("is safe to use from many goroutines", func() {
			c := newTestClient(interceptor.Funcs{}, newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)

			var wg sync.WaitGroup
			for i := range 8 {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					for j := range 20 {
						condType := fmt.Sprintf("Cond%d", i)
						m.UpdateLocal(func(s *provisioningv1.AgentStatus) {
							setCondition(s, condType)
							s.RebootSequenceCount = ptr.To(int32(j))
						})
						_ = m.GetLocal()
						Expect(m.UpdateRemote(j%2 == 0)).To(Succeed())
					}
				}()
			}
			wg.Wait()
			Expect(getAgentStatus(c).Conditions).To(HaveLen(8))
		})
	})

	Describe("UpdateRemote", func() {
		It("pushes every change made before the call", func() {
			c := newTestClient(interceptor.Funcs{}, newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)

			m.UpdateLocal(func(s *provisioningv1.AgentStatus) {
				s.KubeletVersion = ptr.To("v1.30")
				setCondition(s, "A")
			})
			Expect(m.UpdateRemote(true)).To(Succeed())

			status := getAgentStatus(c)
			Expect(status.KubeletVersion).To(Equal(ptr.To("v1.30")))
			Expect(meta.IsStatusConditionTrue(status.Conditions, "A")).To(BeTrue())
		})

		It("runs one push at a time and batches requests queued while the worker is busy", func() {
			gate := newPatchGate()
			c := newTestClient(gate.funcs(), newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)

			m.UpdateLocal(func(s *provisioningv1.AgentStatus) { s.KubeletVersion = ptr.To("v1") })
			first := updateRemoteAsync(m, true)
			Eventually(gate.entered).Should(BeClosed())

			// The first push already took its snapshot, so these requests need a push of their own.
			m.UpdateLocal(func(s *provisioningv1.AgentStatus) { s.KubeletVersion = ptr.To("v2") })
			queued := []<-chan error{
				updateRemoteAsync(m, true),
				updateRemoteAsync(m, true),
				updateRemoteAsync(m, false),
			}
			Eventually(func() int { return pendingCount(m) }).Should(Equal(3))
			close(gate.release)

			Eventually(first).Should(Receive(BeNil()))
			for _, result := range queued {
				Eventually(result).Should(Receive(BeNil()))
			}
			Expect(gate.calls.Load()).To(Equal(int32(2)))
			Expect(gate.maxActive.Load()).To(Equal(int32(1)))
			Expect(getAgentStatus(c).KubeletVersion).To(Equal(ptr.To("v2")))
		})

		It("retries until the push succeeds", func() {
			gate := newPatchGate(2, 3)
			close(gate.release)
			c := newTestClient(gate.funcs(), newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)

			m.UpdateLocal(func(s *provisioningv1.AgentStatus) { s.KubeletVersion = ptr.To("v1") })
			Expect(m.UpdateRemote(true)).To(Succeed())
			Expect(m.UpdateRemote(true)).To(Succeed())
			Expect(gate.calls.Load()).To(Equal(int32(4)))
		})

		It("tries once and returns the patch error when untilSuccess is false", func() {
			gate := newPatchGate(1)
			close(gate.release)
			c := newTestClient(gate.funcs(), newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)

			Expect(m.UpdateRemote(false)).To(MatchError(ContainSubstring("injected patch failure 1")))
			Expect(gate.calls.Load()).To(Equal(int32(1)))
		})

		It("returns a single-attempt request early while a batched retrying request keeps retrying", func() {
			gate := newPatchGate(2)
			c := newTestClient(gate.funcs(), newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)

			first := updateRemoteAsync(m, true)
			Eventually(gate.entered).Should(BeClosed())
			once := updateRemoteAsync(m, false)
			retrying := updateRemoteAsync(m, true)
			Eventually(func() int { return pendingCount(m) }).Should(Equal(2))
			close(gate.release)

			Eventually(first).Should(Receive(BeNil()))
			Eventually(once).Should(Receive(MatchError(ContainSubstring("injected patch failure 2"))))
			Eventually(retrying).Should(Receive(BeNil()))
			Expect(gate.calls.Load()).To(Equal(int32(3)))
		})

		It("returns the context error to callers when the worker context is canceled", func() {
			gate := newPatchGate()
			c := newTestClient(gate.funcs(), newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			m.retryInterval = time.Millisecond
			workerCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.Start(workerCtx)

			first := updateRemoteAsync(m, true)
			Eventually(gate.entered).Should(BeClosed())
			queued := updateRemoteAsync(m, true)
			Eventually(func() int { return pendingCount(m) }).Should(Equal(1))
			cancel()

			Eventually(first).Should(Receive(MatchError(context.Canceled)))
			Eventually(queued).Should(Receive(MatchError(context.Canceled)))
		})

		It("starts only one worker when Start is called again", func() {
			gate := newPatchGate()
			c := newTestClient(gate.funcs(), newTestDPU(testDPUUID))
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)
			startManager(m)

			first := updateRemoteAsync(m, true)
			Eventually(gate.entered).Should(BeClosed())
			second := updateRemoteAsync(m, true)
			// A second worker would take this request while the first push is blocked.
			Eventually(func() int { return pendingCount(m) }).Should(Equal(1))
			Consistently(func() int { return pendingCount(m) }, 100*time.Millisecond).Should(Equal(1))
			close(gate.release)

			Eventually(first).Should(Receive(BeNil()))
			Eventually(second).Should(Receive(BeNil()))
			Expect(gate.maxActive.Load()).To(Equal(int32(1)))
		})

		Context("with the UID check", func() {
			It("stops retrying and returns NotFound when the DPU is missing", func() {
				var gets atomic.Int32
				c := newTestClient(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						gets.Add(1)
						return c.Get(ctx, key, obj, opts...)
					},
				})
				m := New(c, testDPUNamespace, testDPUName, testDPUUID)
				startManager(m)
				err := m.UpdateRemote(true)
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "got %v", err)
				Expect(gets.Load()).To(Equal(int32(1)))
			})

			It("stops retrying and returns ErrStaleDPU when the DPU UID changed", func() {
				m := New(newTestClient(interceptor.Funcs{}, newTestDPU("new-uid")), testDPUNamespace, testDPUName, testDPUUID)
				startManager(m)
				Expect(m.UpdateRemote(true)).To(MatchError(ErrStaleDPU))
			})

			It("returns the original errors to a single-attempt request", func() {
				m := New(newTestClient(interceptor.Funcs{}), testDPUNamespace, testDPUName, testDPUUID)
				startManager(m)
				err := m.UpdateRemote(false)
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "got %v", err)

				m = New(newTestClient(interceptor.Funcs{}, newTestDPU("new-uid")), testDPUNamespace, testDPUName, testDPUUID)
				startManager(m)
				Expect(m.UpdateRemote(false)).To(MatchError("stale DPU object: expected UID test-uid but got new-uid"))
			})
		})

		Context("without the UID check", func() {
			It("pushes to a DPU with any UID", func() {
				c := newTestClient(interceptor.Funcs{}, newTestDPU("new-uid"))
				m := New(c, testDPUNamespace, testDPUName, "")
				startManager(m)
				m.UpdateLocal(func(s *provisioningv1.AgentStatus) { s.KubeletVersion = ptr.To("v1") })
				Expect(m.UpdateRemote(true)).To(Succeed())
				Expect(getAgentStatus(c).KubeletVersion).To(Equal(ptr.To("v1")))
			})

			It("keeps retrying while the DPU is missing", func() {
				var gets atomic.Int32
				c := newTestClient(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						gets.Add(1)
						return c.Get(ctx, key, obj, opts...)
					},
				})
				m := New(c, testDPUNamespace, testDPUName, "")
				m.retryInterval = time.Millisecond
				workerCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
				defer cancel()
				m.Start(workerCtx)

				err := m.UpdateRemote(true)
				Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue(), "got %v", err)
				Expect(gets.Load()).To(BeNumerically(">", 1))
			})
		})
	})

	Describe("merge rules", func() {
		var c client.Client

		BeforeEach(func() {
			dpu := newTestDPU(testDPUUID)
			c = newTestClient(interceptor.Funcs{}, dpu)
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				KubeletVersion: ptr.To("old"),
				RebootMethod:   ptr.To(provisioningv1.RebootMethodNoAction),
				HostOSInit: &provisioningv1.HostOSInitStatus{
					Succeeded: &provisioningv1.HostOSInitSucceeded{},
				},
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					Conditions: []metav1.Condition{{Type: "Old", Status: metav1.ConditionTrue, Reason: "Old", LastTransitionTime: metav1.Now()}},
				},
				Conditions: []metav1.Condition{{Type: "A", Status: metav1.ConditionTrue, Reason: "A", LastTransitionTime: metav1.Now()}},
			}
			Expect(c.Status().Update(ctx, dpu)).To(Succeed())
		})

		It("leaves fields that are nil in memory unchanged and merges conditions by type", func() {
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)
			m.UpdateLocal(func(s *provisioningv1.AgentStatus) {
				s.RebootSequenceCount = ptr.To(int32(1))
				setCondition(s, "B")
			})
			Expect(m.UpdateRemote(true)).To(Succeed())

			status := getAgentStatus(c)
			Expect(status.KubeletVersion).To(Equal(ptr.To("old")))
			Expect(status.RebootMethod).To(Equal(ptr.To(provisioningv1.RebootMethodNoAction)))
			Expect(status.RebootSequenceCount).To(Equal(ptr.To(int32(1))))
			Expect(meta.IsStatusConditionTrue(status.Conditions, "A")).To(BeTrue())
			Expect(meta.IsStatusConditionTrue(status.Conditions, "B")).To(BeTrue())
			Expect(status.PreInstall.Conditions).To(HaveLen(1))
		})

		It("clears hostOSInit when it is nil in memory", func() {
			m := New(c, testDPUNamespace, testDPUName, testDPUUID)
			startManager(m)
			Expect(m.UpdateRemote(true)).To(Succeed())
			Expect(getAgentStatus(c).HostOSInit).To(BeNil())
		})

		It("pushes preInstall and leaves the regular fields unchanged when only preInstall is set", func() {
			m := New(c, testDPUNamespace, testDPUName, "")
			startManager(m)
			reported := metav1.Now()
			m.UpdateLocal(func(s *provisioningv1.AgentStatus) {
				s.PreInstall = &provisioningv1.AgentPreInstallStatus{AgentReported: &reported}
				meta.SetStatusCondition(&s.PreInstall.Conditions, metav1.Condition{Type: "New", Status: metav1.ConditionTrue, Reason: "New"})
			})
			Expect(m.UpdateRemote(true)).To(Succeed())

			status := getAgentStatus(c)
			Expect(status.PreInstall.AgentReported).NotTo(BeNil())
			Expect(meta.IsStatusConditionTrue(status.PreInstall.Conditions, "Old")).To(BeTrue())
			Expect(meta.IsStatusConditionTrue(status.PreInstall.Conditions, "New")).To(BeTrue())
			Expect(status.KubeletVersion).To(Equal(ptr.To("old")))
			Expect(status.Conditions).To(HaveLen(1))
		})
	})
})
