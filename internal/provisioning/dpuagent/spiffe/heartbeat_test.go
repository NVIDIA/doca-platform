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

package spiffe

import (
	"context"
	"errors"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpuheartbeat"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("Heartbeat", func() {
	const (
		dpuName      = "test-dpu"
		dpuNamespace = "test-ns"
		dpuUID       = "uid-123"
	)

	var (
		ctx    context.Context
		scheme *runtime.Scheme
		dpu    *provisioningv1.DPU
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(provisioningv1.AddToScheme(scheme))
		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpuName,
				Namespace: dpuNamespace,
				UID:       dpuUID,
			},
			Status: provisioningv1.DPUStatus{
				AgentStatus: &provisioningv1.AgentStatus{
					Conditions: []metav1.Condition{},
				},
			},
		}
	})

	cfgWithClient := func(c client.Client) Config {
		return Config{
			Client:       c,
			DPUName:      dpuName,
			DPUNamespace: dpuNamespace,
			DPUUID:       string(dpuUID),
		}
	}

	It("SSA-patches LastProbeTime with HeartbeatFieldManager on success", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)

		Eventually(func(g Gomega) {
			cfgWithCancel, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			cfg.probeOnce(cfgWithCancel)

			latest := &provisioningv1.DPU{}
			g.Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
			g.Expect(latest.Status.AgentStatus.Spiffe).NotTo(BeNil())
			g.Expect(latest.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())
			g.Expect(latest.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
			g.Expect(*latest.Status.AgentStatus.Spiffe.LastProbeMessage).To(BeEmpty())
		}).Should(Succeed())
	})

	It("does not touch status on UID mismatch so a replacement DPU's heartbeat is not overwritten", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)
		cfg.DPUUID = "other-uid"

		cfg.probeOnce(ctx)

		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.Spiffe).To(BeNil())
	})

	It("preserves unrelated AgentStatus fields written via MergeFrom", func() {
		startup := metav1.Now()
		dpu.Status.AgentStatus.LastStartupTime = &startup
		dpu.Status.AgentStatus.KubeletVersion = ptrString("v1.32.0")
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)

		cfg.probeOnce(ctx)

		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
		Expect(*latest.Status.AgentStatus.KubeletVersion).To(Equal("v1.32.0"))
		Expect(latest.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())
	})

	It("uses the agreed heartbeat field manager name", func() {
		Expect(dpuheartbeat.HeartbeatFieldManager).To(Equal("dpuagent-heartbeat"))
	})

	It("truncates long probe messages to 256 chars", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)
		cfg.DPUUID = "wrong"

		longMsg := strings.Repeat("x", 300)
		cfg.applyFailure(ctx, longMsg)

		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(*latest.Status.AgentStatus.Spiffe.LastProbeMessage).To(HaveLen(256))
	})

	It("records a failure message when the DPU Get fails", func() {
		// Fail the probe Get, then toggle off so the assertion can read back. The failure
		// message is written through Status().Patch (SubResourcePatch), which is not
		// intercepted here, so it lands deterministically.
		failGet := true
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(gctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if failGet {
						return apierrors.NewServiceUnavailable("boom")
					}
					return c.Get(gctx, key, obj, opts...)
				},
			}).Build()
		cfg := cfgWithClient(fakeClient)

		cfg.probeOnce(ctx)

		failGet = false
		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.Spiffe).NotTo(BeNil())
		Expect(latest.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
		Expect(*latest.Status.AgentStatus.Spiffe.LastProbeMessage).To(ContainSubstring("get DPU"))
	})

	It("retries after a transient heartbeat authorization failure", func() {
		denyPatch := true
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(patchCtx context.Context, c client.Client, subResource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if denyPatch {
						return apierrors.NewForbidden(
							schema.GroupResource{Group: provisioningv1.GroupVersion.Group, Resource: "dpus"},
							dpuName,
							errors.New("token authorization has not propagated"),
						)
					}
					return c.SubResource(subResource).Patch(patchCtx, obj, patch, opts...)
				},
			}).Build()
		cfg := cfgWithClient(fakeClient)

		cfg.probeOnce(ctx)
		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.Spiffe).To(BeNil())

		denyPatch = false
		cfg.probeOnce(ctx)
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())
	})

	DescribeTable("swallows expected auth errors when the failure patch is denied",
		func(patchErr error) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).
				WithInterceptorFuncs(interceptor.Funcs{
					SubResourcePatch: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
						return patchErr
					},
				}).Build()
			cfg := cfgWithClient(fakeClient)

			// Must not panic; the rejected patch leaves no Spiffe status persisted.
			Expect(func() { cfg.applyFailure(ctx, "denied") }).NotTo(Panic())

			latest := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
			Expect(latest.Status.AgentStatus.Spiffe).To(BeNil())
		},
		Entry("Forbidden", apierrors.NewForbidden(
			schema.GroupResource{Group: "provisioning.dpu.nvidia.com", Resource: "dpus"}, dpuName, errors.New("denied"))),
		Entry("Unauthorized", apierrors.NewUnauthorized("no token")),
	)

	It("preserves LastProbeTime on failure so a failing agent ages to Stale (value-level)", func() {
		// Value-level guard: after a success then a failure, LastProbeTime must survive (the
		// failure path is a merge patch that only touches lastProbeMessage). The managedFields
		// fidelity that actually reproduces the SSA-pruning bug is covered by the envtest case
		// in heartbeat_envtest_test.go (the fake client does not model field-ownership pruning).
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)

		Expect(cfg.applySuccess(ctx)).To(Succeed())
		cfg.applyFailure(ctx, "probe failed")

		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.Spiffe).NotTo(BeNil())
		Expect(latest.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil(), "LastProbeTime must be preserved across a failure")
		Expect(latest.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
		Expect(*latest.Status.AgentStatus.Spiffe.LastProbeMessage).To(Equal("probe failed"))
	})

	It("writes no failure status when the context is already canceled (shutdown)", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		Expect(func() { cfg.probeOnce(canceledCtx) }).NotTo(Panic())

		latest := &provisioningv1.DPU{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu), latest)).To(Succeed())
		Expect(latest.Status.AgentStatus.Spiffe).To(BeNil(), "a canceled probe must not stamp a failure status")
	})

	It("returns when the context is canceled", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		cfg := cfgWithClient(fakeClient)

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		done := make(chan struct{})
		go func() {
			defer close(done)
			Run(canceledCtx, cfg)
		}()

		Eventually(done, time.Second).Should(BeClosed())
	})

	It("returns short probe messages unchanged and trims surrounding space", func() {
		Expect(truncateProbeMessage("short")).To(Equal("short"))
		// The helper trims before the length check (heartbeat.go), so surrounding space is removed.
		Expect(truncateProbeMessage("  x  ")).To(Equal("x"))
	})
})

func ptrString(s string) *string { return &s }
