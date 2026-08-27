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
	"context"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

func spiffeSystemTestConfig(spiffe *operatorv1.SPIFFEConfiguration) *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dpfoperatorconfig", Namespace: "dpf-operator-system"},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode:         operatorv1.DeploymentModeHostTrusted,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
			Security:               &operatorv1.SecurityConfiguration{SPIFFE: spiffe},
		},
	}
}

func spiffeSystemTestVariables(g Gomega, spiffe *operatorv1.SPIFFEConfiguration) Variables {
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())
	vars := VariablesFromDPFOperatorConfig(defaults, spiffeSystemTestConfig(spiffe), nil)
	vars.Namespace = "dpf-operator-system"
	return vars
}

func enabledSPIFFEConfiguration() *operatorv1.SPIFFEConfiguration {
	return &operatorv1.SPIFFEConfiguration{
		SPIREServerAddress: "spire-server.spire-system.svc:8081",
		SPIRETrustDomain:   "cs.internal",
		KubeAPIAudience:    "dpf",
	}
}

// generatedDPUService returns the DPUService a component generates under its own name.
// Components that also emit credential requests or per-cluster services generate more
// than one object, so the DPUService cannot be assumed to be the first.
func generatedDPUService(g Gomega, component Component, vars Variables) *unstructured.Unstructured {
	g.Expect(component.Parse()).To(Succeed())

	objs, err := component.GenerateManifests(context.Background(), vars)
	g.Expect(err).ToNot(HaveOccurred())

	for _, obj := range objs {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		if u.GetKind() == dpuservicev1.DPUServiceKind && u.GetName() == component.Name().String() {
			return u
		}
	}
	g.Expect(objs).To(BeEmpty(), "no DPUService named %s was generated", component.Name())
	return nil
}

func requestsSPIFFE(g Gomega, component Component, vars Variables) bool {
	dpuService := generatedDPUService(g, component, vars)
	g.Expect(dpuService).ToNot(BeNil())

	_, found, err := unstructured.NestedFieldNoCopy(dpuService.Object, "spec", "security", "spiffe")
	g.Expect(err).ToNot(HaveOccurred())
	return found
}

func TestSystemDPUServiceSPIFFEOptIn(t *testing.T) {
	t.Run("not requested when SPIFFE is off", func(t *testing.T) {
		g := NewWithT(t)

		vars := spiffeSystemTestVariables(g, nil)
		g.Expect(vars.SpiffeEnabled).To(BeFalse())
		g.Expect(requestsSPIFFE(g, New().Multus, vars)).To(BeFalse())
	})

	t.Run("requested when SPIFFE is on", func(t *testing.T) {
		g := NewWithT(t)

		vars := spiffeSystemTestVariables(g, enabledSPIFFEConfiguration())
		g.Expect(vars.SpiffeEnabled).To(BeTrue())
		g.Expect(requestsSPIFFE(g, New().Multus, vars)).To(BeTrue())
	})

	// Guards the allowlist itself: a name that is not a known component, or one whose
	// component does not render a DPUService under that name, must not go unnoticed.
	t.Run("every eligible component opts in", func(t *testing.T) {
		g := NewWithT(t)

		vars := spiffeSystemTestVariables(g, enabledSPIFFEConfiguration())
		byName := map[operatorv1.ComponentName]Component{}
		for _, component := range New().AllComponents() {
			byName[component.Name()] = component
		}

		for name := range spiffeEligibleComponents {
			component, known := byName[name]
			g.Expect(known).To(BeTrue(), "%s is not a known component", name)
			vars.DisableSystemComponents[name] = false
			g.Expect(requestsSPIFFE(g, component, vars)).To(BeTrue(), "%s did not opt in", name)
		}
	})

	// RBAC-only DPUServices have no pods to attest, so they must stay out.
	t.Run("not requested for an ineligible component", func(t *testing.T) {
		g := NewWithT(t)

		vars := spiffeSystemTestVariables(g, enabledSPIFFEConfiguration())
		g.Expect(spiffeEligibleComponents[operatorv1.SpireAgentRBACName]).To(BeFalse())
		g.Expect(requestsSPIFFE(g, New().SpireAgentRBAC, vars)).To(BeFalse())
	})
}

// An in-cluster DPUService runs on the host with no per-DPU SPIRE agent to parent to, and
// the API rejects the combination, so the edit must leave it alone in either order.
func TestDPUServiceSetSpiffeEditSkipsInCluster(t *testing.T) {
	g := NewWithT(t)

	inCluster := &dpuservicev1.DPUService{
		Spec: dpuservicev1.DPUServiceSpec{DeployInCluster: ptr.To(true)},
	}
	g.Expect(dpuServiceSetSpiffeEdit()(inCluster)).To(Succeed())
	g.Expect(inCluster.Spec.Security).To(BeNil())

	onDPU := &dpuservicev1.DPUService{}
	g.Expect(dpuServiceSetSpiffeEdit()(onDPU)).To(Succeed())
	g.Expect(onDPU.Spec.Security.SPIFFE).ToNot(BeNil())
	g.Expect(dpuServiceInClusterEdit(true)(onDPU)).To(Succeed())
	g.Expect(onDPU.Spec.Security).To(BeNil())
}

func TestDPUServiceSetSpiffeEditPreservesPrivileged(t *testing.T) {
	g := NewWithT(t)

	dpuService := &dpuservicev1.DPUService{
		Spec: dpuservicev1.DPUServiceSpec{
			Security: &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(true)},
		},
	}
	g.Expect(dpuServiceSetSpiffeEdit()(dpuService)).To(Succeed())
	g.Expect(dpuService.Spec.Security.SPIFFE).ToNot(BeNil())
	g.Expect(ptr.Deref(dpuService.Spec.Security.Privileged, false)).To(BeTrue())
}
