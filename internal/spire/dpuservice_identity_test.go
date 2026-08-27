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

package spire

import (
	"strings"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func testDPUService(namespace, serviceID string) *dpuservicev1.DPUService {
	return &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: namespace, Labels: map[string]string{"tenant": "blue"}},
		Spec:       dpuservicev1.DPUServiceSpec{ServiceID: ptr.To(serviceID)},
	}
}

func testDPU(serial string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-a", Namespace: "dpf-operator-system", Labels: map[string]string{"tenant": "blue"}},
		Spec:       provisioningv1.DPUSpec{SerialNumber: serial, DPUNodeName: "node-a"},
	}
}

func renderDPUServiceIdentity(t *testing.T, cfg *operatorv1.SPIFFEConfiguration, namespace, serial, serviceID string) (DPUServiceIdentity, error) {
	t.Helper()
	renderer, err := NewDPUServiceIdentityRenderer(cfg)
	if err != nil {
		return DPUServiceIdentity{}, err
	}
	return renderer.Render(testDPUService(namespace, serviceID), testDPU(serial), serviceID)
}

func defaultCfg() *operatorv1.SPIFFEConfiguration {
	return &operatorv1.SPIFFEConfiguration{SPIRETrustDomain: "cs.internal"}
}

// TestDPUServiceIdentityDefault pins the shape that shipped before the templates existed, so an
// unset field is not a behavior change.
func TestDPUServiceIdentityDefault(t *testing.T) {
	got, err := renderDPUServiceIdentity(t, defaultCfg(), "svc-ns", "MT2440600YYW", "my-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "spiffe://cs.internal/dpu/mt2440600yyw/service/svc-ns/my-service"; got.SPIFFEID != want {
		t.Fatalf("SPIFFEID: got %q, want %q", got.SPIFFEID, want)
	}
	// Both default to the same template, so with no exchange configured they agree.
	if got.ExchangedSPIFFEID != got.SPIFFEID {
		t.Fatalf("ExchangedSPIFFEID: got %q, want it to equal SPIFFEID %q", got.ExchangedSPIFFEID, got.SPIFFEID)
	}
	if want := "spiffe://cs.internal/spire/agent/dpu_hw/mt2440600yyw"; got.ParentID != want {
		t.Fatalf("ParentID: got %q, want %q", got.ParentID, want)
	}
}

// TestDPUServiceIdentityTenantServiceLayout pins the layout an external identity service asks
// for: /tenant/<tenant>/service/<service>/dpu/<id>, optionally with a trailing process segment.
// The tenant comes from the namespace rather than a label, which is what keeps the identity
// unique: a label is not tied to the namespace, so two namespaces could carry the same one.
func TestDPUServiceIdentityTenantServiceLayout(t *testing.T) {
	cfg := defaultCfg()
	cfg.DPUServiceSPIFFEIDTemplate = `spiffe://{{ .TrustDomain }}/tenant/{{ .Namespace }}/service/{{ .ServiceID }}/dpu/{{ .SerialNumber }}`
	cfg.DPUServiceExchangedSPIFFEIDTemplate = `spiffe://operator.example.test/tenant/{{ .Namespace }}/service/{{ .ServiceID }}/dpu/{{ .SerialNumber }}/process/agent`

	got, err := renderDPUServiceIdentity(t, cfg, "tenant-blue", "mt2440", "dsx-imds")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "spiffe://cs.internal/tenant/tenant-blue/service/dsx-imds/dpu/mt2440"; got.SPIFFEID != want {
		t.Fatalf("SPIFFEID: got %q, want %q", got.SPIFFEID, want)
	}
	// The exchanged subject may leave the local trust domain and carry extra segments.
	if want := "spiffe://operator.example.test/tenant/tenant-blue/service/dsx-imds/dpu/mt2440/process/agent"; got.ExchangedSPIFFEID != want {
		t.Fatalf("ExchangedSPIFFEID: got %q, want %q", got.ExchangedSPIFFEID, want)
	}
}

// TestDPUServiceIDPolicy pins the service ID charset. Rejecting rather than stripping is what
// stops two DPUServices collapsing onto one SPIFFE ID.
func TestDPUServiceIDPolicy(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		want      string
		wantErr   bool
	}{
		{name: "lowercases the segment", serviceID: "My-Service", want: "spiffe://cs.internal/dpu/mt2440/service/svc-ns/my-service"},
		{name: "reject empty service ID", serviceID: "", wantErr: true},
		{name: "accept dots", serviceID: "my.service", want: "spiffe://cs.internal/dpu/mt2440/service/svc-ns/my.service"},
		// DPUDeployment generates <deployment>_<service>_<digest>, so underscores must pass.
		{name: "accept the DPUDeployment generated shape", serviceID: "mydeploy_hbn_abc123", want: "spiffe://cs.internal/dpu/mt2440/service/svc-ns/mydeploy_hbn_abc123"},
		{name: "accept digits only", serviceID: "12345", want: "spiffe://cs.internal/dpu/mt2440/service/svc-ns/12345"},
		{name: "reject a slash, which would forge a path segment", serviceID: "a/b", wantErr: true},
		{name: "accept exactly 63 chars", serviceID: strings.Repeat("a", 63), want: "spiffe://cs.internal/dpu/mt2440/service/svc-ns/" + strings.Repeat("a", 63)},
		{name: "reject over 63 chars", serviceID: strings.Repeat("a", 64), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderDPUServiceIdentity(t, defaultCfg(), "svc-ns", "mt2440", tt.serviceID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for service %q, got %q", tt.serviceID, got.SPIFFEID)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.SPIFFEID != tt.want {
				t.Fatalf("got %q, want %q", got.SPIFFEID, tt.want)
			}
		})
	}
}

// TestDPUServiceIdentityTemplateRejected covers templates that must not survive configuration:
// an identity ignoring the serial or the service ID hands one SVID to distinct workloads.
func TestDPUServiceIdentityTemplateRejected(t *testing.T) {
	tests := []struct {
		name     string
		template string
	}{
		{name: "constant identity", template: "spiffe://cs.internal/dpu/fixed/service/fixed"},
		{name: "ignores the serial", template: "spiffe://{{ .TrustDomain }}/service/{{ .ServiceID }}"},
		{name: "ignores the service ID", template: "spiffe://{{ .TrustDomain }}/dpu/{{ .SerialNumber }}/process/dpu-agent"},
		{name: "ignores the namespace", template: "spiffe://{{ .TrustDomain }}/dpu/{{ .SerialNumber }}/service/{{ .ServiceID }}"},
		// A label is not tied to the namespace, so it cannot stand in for one.
		{name: "qualifies by a label instead of the namespace", template: `spiffe://{{ .TrustDomain }}/tenant/{{ index .DPUServiceMeta.Labels "tenant" }}/service/{{ .ServiceID }}/dpu/{{ .SerialNumber }}`},
		// namespace "tenant" with service "a-svc" would render the same as "tenant-a" with "svc".
		{name: "joins namespace and service ID without a delimiter", template: "spiffe://{{ .TrustDomain }}/dpu/{{ .SerialNumber }}/service/{{ .Namespace }}-{{ .ServiceID }}"},
		{name: "joins serial and service ID without a delimiter", template: "spiffe://{{ .TrustDomain }}/ns/{{ .Namespace }}/w/{{ .SerialNumber }}{{ .ServiceID }}"},
		{name: "another trust domain", template: "spiffe://other.test/dpu/{{ .SerialNumber }}/service/{{ .ServiceID }}"},
		{name: "not a SPIFFE ID", template: "https://{{ .TrustDomain }}/dpu/{{ .SerialNumber }}/service/{{ .ServiceID }}"},
		{name: "unparsable template", template: "spiffe://{{ .TrustDomain }/dpu"},
		{name: "unknown field", template: "spiffe://{{ .TrustDomain }}/{{ .Nope }}/{{ .SerialNumber }}/{{ .ServiceID }}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultCfg()
			cfg.DPUServiceSPIFFEIDTemplate = tt.template
			if _, err := NewDPUServiceIdentityRenderer(cfg); err == nil {
				t.Fatal("expected the template to be rejected")
			}
		})
	}
}

// TestDPUServiceExchangedTemplateTrustDomainFree asserts the exchanged template is allowed to
// leave the local trust domain, which is the whole point of an exchange, while the registered one
// is not: SPIRE only issues within its own trust domain.
func TestDPUServiceExchangedTemplateTrustDomainFree(t *testing.T) {
	cfg := defaultCfg()
	cfg.DPUServiceExchangedSPIFFEIDTemplate = "spiffe://elsewhere.test/dpu/{{ .SerialNumber }}/service/{{ .Namespace }}/{{ .ServiceID }}"
	if _, err := NewDPUServiceIdentityRenderer(cfg); err != nil {
		t.Fatalf("exchanged template with another trust domain must be accepted: %v", err)
	}

	cfg = defaultCfg()
	cfg.DPUServiceSPIFFEIDTemplate = "spiffe://elsewhere.test/dpu/{{ .SerialNumber }}/service/{{ .Namespace }}/{{ .ServiceID }}"
	if _, err := NewDPUServiceIdentityRenderer(cfg); err == nil {
		t.Fatal("registered template with another trust domain must be rejected")
	}
}

func TestNewDPUServiceIdentityRendererInvalidConfig(t *testing.T) {
	if _, err := NewDPUServiceIdentityRenderer(nil); err == nil {
		t.Fatal("expected an error for a nil configuration")
	}
	if _, err := NewDPUServiceIdentityRenderer(&operatorv1.SPIFFEConfiguration{SPIRETrustDomain: "CS_INTERNAL"}); err == nil {
		t.Fatal("expected an error for an invalid trust domain")
	}
}

// TestDPUServiceIdentityTemplateAccepted covers layouts that reach the serial or the service ID
// through the embedded metadata and spec rather than the scalar fields. Validation renders probes
// rather than inspecting the parse tree, so the probe data has to vary every field that names a
// DPU or a DPUService or these would be rejected as constant.
func TestDPUServiceIdentityTemplateAccepted(t *testing.T) {
	tests := []struct {
		name     string
		template string
	}{
		{name: "object names", template: `spiffe://{{ .TrustDomain }}/dpu/{{ .DPUMeta.Name }}/ns/{{ .DPUServiceMeta.Namespace }}/service/{{ .DPUServiceMeta.Name }}`},
		{name: "annotation qualified", template: `spiffe://{{ .TrustDomain }}/tenant/{{ index .DPUServiceMeta.Annotations "tenant" }}/ns/{{ .Namespace }}/service/{{ .ServiceID }}/dpu/{{ .SerialNumber }}`},
		{name: "label qualified", template: `spiffe://{{ .TrustDomain }}/tenant/{{ index .DPUServiceMeta.Labels "tenant" }}/ns/{{ .Namespace }}/service/{{ .ServiceID }}/dpu/{{ .SerialNumber }}`},
		{name: "spec fields", template: `spiffe://{{ .TrustDomain }}/node/{{ .DPUSpec.DPUNodeName }}/ns/{{ .DPUServiceMeta.Namespace }}/service/{{ .DPUServiceSpec.ServiceID }}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultCfg()
			cfg.DPUServiceSPIFFEIDTemplate = tt.template
			if _, err := NewDPUServiceIdentityRenderer(cfg); err != nil {
				t.Fatalf("template must be accepted: %v", err)
			}
		})
	}
}

// TestDPUServiceIdentityNamespaceDistinct is why the default template carries the namespace:
// service IDs are only unique within one, so two tenants running the same deployment on one DPU
// would otherwise share an identity.
func TestDPUServiceIdentityNamespaceDistinct(t *testing.T) {
	first, err := renderDPUServiceIdentity(t, defaultCfg(), "tenant-a", "mt2440", "shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := renderDPUServiceIdentity(t, defaultCfg(), "tenant-b", "mt2440", "shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.SPIFFEID == second.SPIFFEID {
		t.Fatalf("two namespaces share the identity %q", first.SPIFFEID)
	}
}

// TestDPUServiceIdentityNeverEqualsDPUAgent guards the trust boundary between a DPUService and
// the DPU Agent parenting it: they live in one trust domain, so an overlap would let a service
// present itself as the agent.
func TestDPUServiceIdentityNeverEqualsDPUAgent(t *testing.T) {
	const serial = "mt2440"
	service, err := renderDPUServiceIdentity(t, defaultCfg(), "svc-ns", serial, "my-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	agentRenderer, err := NewDPUAgentIdentityRenderer(defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dpu, dpuDevice := identityTemplateObjects(serial)
	agent, err := agentRenderer.Render(dpu, dpuDevice)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.SPIFFEID == agent.SPIFFEID {
		t.Fatalf("DPUService and DPU Agent share the identity %q", service.SPIFFEID)
	}
	if service.SPIFFEID == service.ParentID {
		t.Fatalf("DPUService identity equals its parent agent %q", service.ParentID)
	}
}

// TestDPUServiceIdentityRejectsInvalidInput pins the fail-closed contract on the values feeding
// the default template. Rejecting beats sanitizing: a stripped character silently merges two
// workloads onto one identity.
func TestDPUServiceIdentityRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		serial    string
	}{
		{name: "empty namespace", namespace: "", serial: "mt2440"},
		{name: "namespace with a slash", namespace: "a/b", serial: "mt2440"},
		{name: "empty serial", namespace: "svc-ns", serial: ""},
		{name: "serial with reserved characters", namespace: "svc-ns", serial: "MT:24/40"},
		{name: "serial over the maximum", namespace: "svc-ns", serial: strings.Repeat("a", 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := renderDPUServiceIdentity(t, defaultCfg(), tt.namespace, tt.serial, "my-service"); err == nil {
				t.Fatalf("expected an error, got %q", got.SPIFFEID)
			}
		})
	}
}
