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

package controller

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
)

func TestValidateSPIFFEIdentityTemplates(t *testing.T) {
	config := &operatorv1.DPFOperatorConfig{
		Spec: operatorv1.DPFOperatorConfigSpec{
			Security: &operatorv1.SecurityConfiguration{
				SPIFFE: &operatorv1.SPIFFEConfiguration{
					SPIRETrustDomain:                  "cs.internal",
					DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
					DPUAgentExchangedSPIFFEIDTemplate: "spiffe://operator.example.test/dpu/{{ .SerialNumber }}/process/dpu-agent",
				},
			},
		},
	}
	if err := validateSPIFFEIdentityTemplates(config); err != nil {
		t.Fatalf("valid configuration failed: %v", err)
	}

	config.Spec.Security.SPIFFE.DPUAgentExchangedSPIFFEIDTemplate = "spiffe://operator.example.test/dpu/static/process/dpu-agent"
	if err := validateSPIFFEIdentityTemplates(config); err == nil {
		t.Fatal("configuration missing SerialNumber unexpectedly passed")
	}
}
