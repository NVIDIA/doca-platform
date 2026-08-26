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

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func validIdentityTemplateConfig() *operatorv1.SPIFFEConfiguration {
	return &operatorv1.SPIFFEConfiguration{
		SPIRETrustDomain:                  "cs.internal",
		DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/{{ index .DPUMeta.Labels \"tenant\" }}/service/dsx/dpu/{{ .DPUDeviceSpec.SerialNumber }}/process/dpu-agent",
		DPUAgentExchangedSPIFFEIDTemplate: "spiffe://{{ index .DPUDeviceMeta.Labels \"issuer\" }}/dpu/{{ .DPUSpec.SerialNumber }}/process/dpu-agent",
	}
}

func identityTemplateObjects(serial string) (*provisioningv1.DPU, *provisioningv1.DPUDevice) {
	return &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "dpf-operator-system", Labels: map[string]string{"tenant": "dummy-operator"}},
		}, &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{Name: "device-1", Namespace: "dpf-operator-system", Labels: map[string]string{"issuer": "dummy-operator.az51-dev2.dsx.nvid.id"}},
			Spec:       provisioningv1.DPUDeviceSpec{SerialNumber: serial},
		}
}

func TestDPUAgentIdentityRenderer(t *testing.T) {
	renderer, err := NewDPUAgentIdentityRenderer(validIdentityTemplateConfig())
	if err != nil {
		t.Fatalf("NewDPUAgentIdentityRenderer() error = %v", err)
	}
	dpu, dpuDevice := identityTemplateObjects("  MT2440600YYW  ")
	got, err := renderer.Render(dpu, dpuDevice)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if want := "spiffe://cs.internal/tenant/dummy-operator/service/dsx/dpu/mt2440600yyw/process/dpu-agent"; got.SPIFFEID != want {
		t.Fatalf("SPIFFEID = %q, want %q", got.SPIFFEID, want)
	}
	if want := "spiffe://dummy-operator.az51-dev2.dsx.nvid.id/dpu/mt2440600yyw/process/dpu-agent"; got.ExchangedSPIFFEID != want {
		t.Fatalf("ExchangedSPIFFEID = %q, want %q", got.ExchangedSPIFFEID, want)
	}
	if want := "spiffe://cs.internal/spire/agent/dpu_hw/mt2440600yyw"; got.ParentID != want {
		t.Fatalf("ParentID = %q, want %q", got.ParentID, want)
	}
}

func TestDPUAgentIdentityTemplateValidationAllowsArbitraryMetadataKeys(t *testing.T) {
	config := validIdentityTemplateConfig()
	config.DPUAgentSPIFFEIDTemplate = "spiffe://{{ .TrustDomain }}/environment/{{ index .DPUMeta.Labels \"environment\" }}/dpu/{{ .SerialNumber }}"
	config.DPUAgentExchangedSPIFFEIDTemplate = "spiffe://{{ index .DPUDeviceMeta.Annotations \"identity.example.com/issuer\" }}/dpu/{{ .SerialNumber }}"

	renderer, err := NewDPUAgentIdentityRenderer(config)
	if err != nil {
		t.Fatalf("NewDPUAgentIdentityRenderer() rejected operator-defined metadata keys: %v", err)
	}
	dpu, dpuDevice := identityTemplateObjects("MT2440600YYW")
	dpu.Labels["environment"] = "production"
	dpuDevice.Annotations = map[string]string{"identity.example.com/issuer": "operator.example.test"}
	got, err := renderer.Render(dpu, dpuDevice)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if want := "spiffe://cs.internal/environment/production/dpu/mt2440600yyw"; got.SPIFFEID != want {
		t.Fatalf("SPIFFEID = %q, want %q", got.SPIFFEID, want)
	}
	if want := "spiffe://operator.example.test/dpu/mt2440600yyw"; got.ExchangedSPIFFEID != want {
		t.Fatalf("ExchangedSPIFFEID = %q, want %q", got.ExchangedSPIFFEID, want)
	}

	delete(dpu.Labels, "environment")
	if _, err := renderer.Render(dpu, dpuDevice); err == nil {
		t.Fatal("Render() unexpectedly masked a missing runtime metadata key")
	}
}

func TestDPUAgentIdentityRendererRejectsInvalidSerial(t *testing.T) {
	renderer, err := NewDPUAgentIdentityRenderer(validIdentityTemplateConfig())
	if err != nil {
		t.Fatalf("NewDPUAgentIdentityRenderer() error = %v", err)
	}
	dpu, dpuDevice := identityTemplateObjects("bad serial!")
	if _, err := renderer.Render(dpu, dpuDevice); err == nil {
		t.Fatal("Render() expected invalid serial error")
	}
}

func TestDPUAgentIdentityRendererDefaultsToDefaultTemplate(t *testing.T) {
	config := &operatorv1.SPIFFEConfiguration{SPIRETrustDomain: "cs.internal"}
	renderer, err := NewDPUAgentIdentityRenderer(config)
	if err != nil {
		t.Fatalf("NewDPUAgentIdentityRenderer() error = %v", err)
	}
	dpu, dpuDevice := identityTemplateObjects("MT2440600YYW")
	got, err := renderer.Render(dpu, dpuDevice)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	want := "spiffe://cs.internal/dpu/mt2440600yyw/process/dpu-agent"
	if got.SPIFFEID != want || got.ExchangedSPIFFEID != want {
		t.Fatalf("identities = %#v, want both %q", got, want)
	}
}

func TestDPUAgentIdentityTemplateValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*operatorv1.SPIFFEConfiguration)
	}{
		{
			name: "unknown variable",
			mutate: func(c *operatorv1.SPIFFEConfiguration) {
				c.DPUAgentExchangedSPIFFEIDTemplate = "spiffe://example.test/dpu/{{ .Unknown }}/{{ .SerialNumber }}"
			},
		},
		{
			name: "missing SerialNumber",
			mutate: func(c *operatorv1.SPIFFEConfiguration) {
				c.DPUAgentSPIFFEIDTemplate = "spiffe://{{ .TrustDomain }}/tenant/operator/service/dsx/dpu/static"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validIdentityTemplateConfig()
			tt.mutate(config)
			if _, err := NewDPUAgentIdentityRenderer(config); err == nil {
				t.Fatal("NewDPUAgentIdentityRenderer() expected error")
			}
		})
	}
}

func TestDPUAgentIdentityRendererValidatesRenderedIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*operatorv1.SPIFFEConfiguration)
	}{
		{
			name: "relative path",
			mutate: func(c *operatorv1.SPIFFEConfiguration) {
				c.DPUAgentSPIFFEIDTemplate = "tenant/dpu/{{ .SerialNumber }}"
			},
		},
		{
			name: "malformed ID",
			mutate: func(c *operatorv1.SPIFFEConfiguration) {
				c.DPUAgentExchangedSPIFFEIDTemplate = "spiffe://bad host/dpu/{{ .SerialNumber }}"
			},
		},
		{
			name: "wrong local trust domain",
			mutate: func(c *operatorv1.SPIFFEConfiguration) {
				c.DPUAgentSPIFFEIDTemplate = "spiffe://other.example.test/dpu/{{ .SerialNumber }}"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validIdentityTemplateConfig()
			tt.mutate(config)
			renderer, err := NewDPUAgentIdentityRenderer(config)
			if err != nil {
				t.Fatalf("NewDPUAgentIdentityRenderer() error = %v", err)
			}
			dpu, dpuDevice := identityTemplateObjects("MT2440600YYW")
			if _, err := renderer.Render(dpu, dpuDevice); err == nil {
				t.Fatal("Render() expected invalid identity error")
			}
		})
	}
}

func TestDPUAgentIdentityTemplateValidationAggregatesErrors(t *testing.T) {
	config := validIdentityTemplateConfig()
	config.DPUAgentSPIFFEIDTemplate = "spiffe://{{ .TrustDomain }}/dpu/static"
	config.DPUAgentExchangedSPIFFEIDTemplate = "spiffe://example.test/dpu/static"

	_, err := NewDPUAgentIdentityRenderer(config)
	if err == nil {
		t.Fatal("NewDPUAgentIdentityRenderer() expected error")
	}
	for _, templateName := range []string{"dpuAgentSPIFFEIDTemplate", "dpuAgentExchangedSPIFFEIDTemplate"} {
		if !strings.Contains(err.Error(), templateName) {
			t.Errorf("error %q does not contain %q", err, templateName)
		}
	}
}
