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

package dpuagent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	spiffeheartbeat "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/spiffe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var _ = Describe("DPUAgent status patches (envtest)", Ordered, func() {
	var (
		testEnv   *envtest.Environment
		k8sClient client.Client
		envCtx    context.Context
	)

	BeforeAll(func() {
		if os.Getenv("KUBEBUILDER_ASSETS") == "" {
			Skip("KUBEBUILDER_ASSETS not set; skipping envtest status patch verification")
		}
		envCtx = context.Background()
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "..", "config", "provisioning", "crd", "bases"),
			},
			ErrorIfCRDPathMissing: true,
			BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
				"1.32.0-"+runtime.GOOS+"-"+runtime.GOARCH),
		}
		restCfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())

		s := k8sruntime.NewScheme()
		Expect(provisioningv1.AddToScheme(s)).To(Succeed())
		k8sClient, err = client.New(restCfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if testEnv != nil {
			Expect(testEnv.Stop()).To(Succeed())
		}
	})

	It("preserves heartbeat fields during a normal agent status update", func() {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "heartbeat-status-" + utilrand.String(6), Namespace: "default"},
			Spec: provisioningv1.DPUSpec{
				BFB:           ptr.To("test-bfb"),
				SerialNumber:  "MT" + utilrand.String(10),
				DPUDeviceName: "dev-" + utilrand.String(4),
				DPUFlavor:     "dpu-flavor",
				NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
			},
		}
		Expect(k8sClient.Create(envCtx, dpu)).To(Succeed())
		dpu.Status.Phase = provisioningv1.DPUReady
		dpu.Status.Conditions = []metav1.Condition{}
		Expect(k8sClient.Status().Update(envCtx, dpu)).To(Succeed())

		heartbeatCtx, cancelHeartbeat := context.WithCancel(envCtx)
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			spiffeheartbeat.Run(heartbeatCtx, spiffeheartbeat.Config{
				Client:       k8sClient,
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				DPUUID:       string(dpu.UID),
			})
		}()

		key := client.ObjectKeyFromObject(dpu)
		got := &provisioningv1.DPU{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
			g.Expect(got.Status.AgentStatus).NotTo(BeNil())
			g.Expect(got.Status.AgentStatus.Spiffe).NotTo(BeNil())
			g.Expect(got.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())
			g.Expect(got.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
		}).Should(Succeed())
		cancelHeartbeat()
		Eventually(heartbeatDone).Should(BeClosed())
		Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeTime).NotTo(BeNil())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeMessage).NotTo(BeNil())
		probeTime := got.Status.AgentStatus.Spiffe.LastProbeTime.DeepCopy()
		probeMessage := *got.Status.AgentStatus.Spiffe.LastProbeMessage

		agent := &DPUAgent{optCtx: newTestOptCtx(k8sClient)}
		agent.optCtx.Options.DPUName = dpu.Name
		agent.optCtx.Options.DPUNamespace = dpu.Namespace
		agent.optCtx.Options.DPUUID = string(dpu.UID)
		agent.optCtx.Status.Conditions = []metav1.Condition{{
			Type:               "HeartbeatCoexistence",
			Status:             metav1.ConditionTrue,
			Reason:             "StatusPatched",
			LastTransitionTime: metav1.Now(),
		}}
		Expect(agent.updateStatus(envCtx)).To(Succeed())

		Expect(k8sClient.Get(envCtx, key, got)).To(Succeed())
		Expect(got.Status.AgentStatus.Spiffe.LastProbeTime).To(Equal(probeTime))
		Expect(*got.Status.AgentStatus.Spiffe.LastProbeMessage).To(Equal(probeMessage))
		Expect(meta.FindStatusCondition(got.Status.AgentStatus.Conditions, "HeartbeatCoexistence")).NotTo(BeNil())
	})
})
