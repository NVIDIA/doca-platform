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
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// warningCollector collects warnings emitted as HTTP warning headers by the API server.
type warningCollector struct {
	mu       sync.Mutex
	warnings []string
}

func (w *warningCollector) HandleWarningHeader(_ int, _ string, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warnings = append(w.warnings, text)
}

func (w *warningCollector) get() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.warnings...)
}

// ValidateVAPDeprecationWarnings verifies that the generated VAP deprecation
// warnings fire when a deprecated field is set on a DPF resource. It uses
// spec.bmcIP on a DPU as one arbitrary example of a deprecated field to
// trigger and assert on the warning.
func ValidateVAPDeprecationWarnings(ctx context.Context, input *SystemTestInput) {
	collector := &warningCollector{}
	cfg := rest.CopyConfig(input.RestConfig)
	cfg.WarningHandler = collector

	warningClient, err := client.New(cfg, client.Options{Scheme: input.Client.Scheme()})
	Expect(err).NotTo(HaveOccurred())

	By("Creating a DPU with deprecated spec.bmcIP set")
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-vap-warning-",
			Namespace:    DPFOperatorSystemNamespace,
			Labels:       CleanupScope.It,
		},
		Spec: provisioningv1.DPUSpec{
			// Deprecated field we want to test.
			BMCIP: "192.168.1.100",
			// Required fields.
			NodeEffect: provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
			},
			DPUNodeName:   "e2e-vap-test-node",
			DPUDeviceName: "e2e-vap-test-device",
			BFB:           ptr.To("e2e-vap-test-bfb"),
			SerialNumber:  "e2e-vap-test-serial",
			DPUFlavor:     "e2e-vap-test-flavor",
		},
	}
	Expect(warningClient.Create(ctx, dpu)).To(Succeed())

	By("Verifying the VAP deprecation warning was emitted")
	Expect(collector.get()).To(ContainElement(ContainSubstring("spec.bmcIP is deprecated")))

	By("Creating a DPU without any deprecated fields set")
	negativeCollector := &warningCollector{}
	negativeCfg := rest.CopyConfig(input.RestConfig)
	negativeCfg.WarningHandler = negativeCollector

	negativeClient, err := client.New(negativeCfg, client.Options{Scheme: input.Client.Scheme()})
	Expect(err).NotTo(HaveOccurred())

	dpuNoDeprecated := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-vap-no-warning-",
			Namespace:    DPFOperatorSystemNamespace,
			Labels:       CleanupScope.It,
		},
		Spec: provisioningv1.DPUSpec{
			NodeEffect: provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
			},
			DPUNodeName:   "e2e-vap-test-node-no-dep",
			DPUDeviceName: "e2e-vap-test-device-no-dep",
			BFB:           ptr.To("e2e-vap-test-bfb"),
			SerialNumber:  "e2e-vap-test-serial-no-dep",
			DPUFlavor:     "e2e-vap-test-flavor",
		},
	}
	Expect(negativeClient.Create(ctx, dpuNoDeprecated)).To(Succeed())

	By("Verifying no VAP deprecation warning was emitted")
	Expect(negativeCollector.get()).NotTo(ContainElement(ContainSubstring("is deprecated")))
}
