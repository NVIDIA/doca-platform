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

package inventory

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// spiffeConfig is the minimal Security stanza that turns the SPIFFE gate on.
func spiffeConfig() *operatorv1.SecurityConfiguration {
	return &operatorv1.SecurityConfiguration{
		SPIFFE: &operatorv1.SPIFFEConfiguration{
			SPIREServerAddress:                "spire-server.spire-system.svc:8081",
			SPIRETrustDomain:                  "cs.internal",
			DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
			DPUAgentExchangedSPIFFEIDTemplate: "spiffe://operator.example.test/dpu/{{ .SerialNumber }}/process/dpu-agent",
			KubeAPIAudience:                   "dpf",
			SPIREOIDCURL:                      "https://spire.example.com",
		},
	}
}

// spireTestNamespace is the namespace the singleton DPFOperatorConfig lives in.
const spireTestNamespace = "dpf-operator-system"

func spireTestConfig(security *operatorv1.SecurityConfiguration) *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dpfoperatorconfig", Namespace: spireTestNamespace},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode:         operatorv1.DeploymentModeHostTrusted,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
			Security:               security,
		},
	}
}

// The SPIRE agent runs on the DPU host OS and cannot attest workloads without the
// nodes/pods grant, so enablement must follow the SPIFFE gate exactly rather than an
// independent component toggle.
func TestSpireAgentRBACFollowsSpiffeGate(t *testing.T) {
	g := NewWithT(t)
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	t.Run("disabled when SPIFFE is not configured", func(t *testing.T) {
		g := NewWithT(t)
		vars := VariablesFromDPFOperatorConfig(defaults, spireTestConfig(nil), nil)
		g.Expect(vars.DisableSystemComponents[operatorv1.SpireAgentRBACName]).To(BeTrue())
	})

	t.Run("enabled when SPIFFE is configured", func(t *testing.T) {
		g := NewWithT(t)
		vars := VariablesFromDPFOperatorConfig(defaults, spireTestConfig(spiffeConfig()), nil)
		g.Expect(vars.DisableSystemComponents[operatorv1.SpireAgentRBACName]).To(BeFalse())
	})
}

func TestSpireAgentRBACGenerateManifests(t *testing.T) {
	g := NewWithT(t)
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	component := New().SpireAgentRBAC
	g.Expect(component.Parse()).To(Succeed())
	g.Expect(component.Name()).To(Equal(operatorv1.SpireAgentRBACName))

	t.Run("generates nothing without SPIFFE", func(t *testing.T) {
		g := NewWithT(t)
		vars := VariablesFromDPFOperatorConfig(defaults, spireTestConfig(nil), nil)
		vars.Namespace = spireTestNamespace

		objs, err := component.GenerateManifests(t.Context(), vars)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(objs).To(BeEmpty())
	})

	t.Run("generates a DPUService enabling the subchart with SPIFFE", func(t *testing.T) {
		g := NewWithT(t)
		vars := VariablesFromDPFOperatorConfig(defaults, spireTestConfig(spiffeConfig()), nil)
		vars.Namespace = spireTestNamespace

		objs, err := component.GenerateManifests(t.Context(), vars)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(objs).To(HaveLen(1))

		dpuService, ok := objs[0].(*unstructured.Unstructured)
		g.Expect(ok).To(BeTrue())
		g.Expect(dpuService.GetName()).To(Equal(operatorv1.SpireAgentRBACName.String()))
		g.Expect(dpuService.GetNamespace()).To(Equal(spireTestNamespace))

		// The subchart is one of many in the dpu-networking umbrella chart, which
		// disables every subchart by default, so the DPUService must switch this one on.
		enabled, found, err := unstructured.NestedBool(dpuService.Object,
			"spec", "helmChart", "values", operatorv1.SpireAgentRBACName.String(), "enabled")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(enabled).To(BeTrue())
	})
}
