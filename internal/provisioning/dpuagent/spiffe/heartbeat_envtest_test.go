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
	"os"
	"path/filepath"
	"runtime"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpuheartbeat"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Verifies against a real apiserver that the failure path preserves LastProbeTime. The fake
// client does not model Server-Side Apply field ownership, so only envtest can catch a failure
// write that prunes LastProbeTime from the heartbeat manager's fieldset and clears it (which
// would flip Freshness from Stale to NeverAttested). Skips when envtest assets are unavailable
// (KUBEBUILDER_ASSETS unset), so fast local runs and `make test` both behave correctly.
var _ = Describe("Heartbeat managedFields (envtest)", Ordered, func() {
	var (
		testEnv   *envtest.Environment
		k8sClient client.Client
		envCtx    context.Context
	)

	BeforeAll(func() {
		if os.Getenv("KUBEBUILDER_ASSETS") == "" {
			Skip("KUBEBUILDER_ASSETS not set; skipping envtest managedFields verification")
		}
		envCtx = context.Background()
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
			},
			ErrorIfCRDPathMissing: true,
			BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "hack", "tools", "bin", "k8s",
				"1.32.0-"+runtime.GOOS+"-"+runtime.GOARCH),
		}
		restCfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(restCfg).NotTo(BeNil())

		s := scheme.Scheme
		Expect(provisioningv1.AddToScheme(s)).To(Succeed())
		k8sClient, err = client.New(restCfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if testEnv != nil {
			Expect(testEnv.Stop()).To(Succeed())
		}
	})

	It("preserves LastProbeTime across failures via real Server-Side-Apply", func() {
		name := "hb-" + utilrand.String(6)
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: provisioningv1.DPUSpec{
				BFB:           ptr.To("test-bfb"),
				SerialNumber:  "MT" + utilrand.String(10),
				DPUDeviceName: "hb-dev-" + utilrand.String(4),
				DPUFlavor:     "dpu-flavor",
				NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
			},
		}
		Expect(k8sClient.Create(envCtx, dpu)).To(Succeed())
		// Seed a valid status: in production the provisioning controller sets phase/conditions
		// before the agent's heartbeat runs, and the CRD rejects a status patch that would leave
		// phase empty or conditions null.
		dpu.Status.Phase = provisioningv1.DPUReady
		dpu.Status.Conditions = []metav1.Condition{}
		Expect(k8sClient.Status().Update(envCtx, dpu)).To(Succeed())
		key := client.ObjectKeyFromObject(dpu)
		cfg := Config{Client: k8sClient, DPUName: name, DPUNamespace: "default", DPUUID: string(dpu.UID)}

		By("first success stamps LastProbeTime")
		Expect(cfg.applySuccess(envCtx)).To(Succeed())
		got := &provisioningv1.DPU{}
		Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
		Expect(got.Status.AgentStatus).NotTo(BeNil())
		Expect(got.Status.AgentStatus.Spiffe).NotTo(BeNil())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())

		By("a failure updates the message WITHOUT pruning LastProbeTime")
		cfg.applyFailure(envCtx, "probe failed")
		Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil(),
			"merge-patch failure path must not clear LastProbeTime (regression: SSA field pruning)")
		Expect(got.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
		Expect(*got.Status.AgentStatus.Spiffe.LastProbeMessage).To(Equal("probe failed"))

		By("Freshness reflects Stale/Fresh by time, never NeverAttested after a failure")
		Expect(dpuheartbeat.Freshness(got, time.Now(), time.Minute)).NotTo(Equal(dpuheartbeat.NeverAttested))

		By("the next success clears the message and refreshes the timestamp")
		Expect(cfg.applySuccess(envCtx)).To(Succeed())
		Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
		Expect(*got.Status.AgentStatus.Spiffe.LastProbeMessage).To(BeEmpty())

		By("the heartbeat field manager owns the spiffe status entry")
		Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
		var sawManager bool
		for _, mf := range got.GetManagedFields() {
			if mf.Manager == dpuheartbeat.HeartbeatFieldManager {
				sawManager = true
				break
			}
		}
		Expect(sawManager).To(BeTrue())
	})
})
