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
	"bytes"
	"errors"
	"fmt"
	"text/template"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/spire/identity"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// dpuAgentIdentityTemplateData follows SPIRE controller-manager's workload template model,
// adapted from Pod/Node data to DPU/DPUDevice data. Its exported fields form the template
// contract even though the Go type itself is private. Metadata and specs are snapshots; rendering
// never mutates informer-backed objects.
type dpuAgentIdentityTemplateData struct {
	// TrustDomain is the configured local SPIRE trust domain, for example cs.internal.
	TrustDomain string
	// SerialNumber is the normalized DPUDevice serial, for example mt2440600yyw.
	SerialNumber string
	// DPUMeta exposes DPU metadata. For example, a label is accessed with
	// {{ index .DPUMeta.Labels "tenant" }}.
	DPUMeta *metav1.ObjectMeta
	// DPUSpec exposes the DPU spec with SerialNumber normalized to match .SerialNumber.
	// For example, the associated node name is {{ .DPUSpec.DPUNodeName }}.
	DPUSpec *provisioningv1.DPUSpec
	// DPUDeviceMeta exposes DPUDevice metadata. For example, an annotation is accessed with
	// {{ index .DPUDeviceMeta.Annotations "identity.example.com/issuer" }}.
	DPUDeviceMeta *metav1.ObjectMeta
	// DPUDeviceSpec exposes the DPUDevice spec with SerialNumber normalized to match .SerialNumber.
	// For example, the serial is {{ .DPUDeviceSpec.SerialNumber }}.
	DPUDeviceSpec *provisioningv1.DPUDeviceSpec
}

// DPUAgentIdentity contains the pre- and post-exchange SVID subjects used when
// SVID exchange is enabled.
type DPUAgentIdentity struct {
	// SPIFFEID is the pre-exchange SVID subject registered in ClusterStaticEntry, for example
	// spiffe://cs.internal/tenant/operator/service/dsx/dpu/mt2440600yyw/process/dpu-agent.
	SPIFFEID string
	// ExchangedSPIFFEID is the post-exchange SVID subject used by the DPU Agent RoleBinding, for example
	// spiffe://operator.example.dsx.nvid.id/dpu/mt2440600yyw/process/dpu-agent.
	ExchangedSPIFFEID string
	// ParentID is the SPIRE agent identity that owns the ClusterStaticEntry, for example
	// spiffe://cs.internal/spire/agent/dpu_hw/mt2440600yyw.
	ParentID string
}

// DPUAgentIdentityRenderer renders both identities from validated operator configuration.
type DPUAgentIdentityRenderer struct {
	spiffeIDTemplate          *template.Template
	exchangedSPIFFEIDTemplate *template.Template
	trustDomain               spiffeid.TrustDomain
}

// NewDPUAgentIdentityRenderer parses and validates both configured identity templates.
func NewDPUAgentIdentityRenderer(config *operatorv1.SPIFFEConfiguration) (*DPUAgentIdentityRenderer, error) {
	if config == nil {
		return nil, fmt.Errorf("SPIFFE configuration is nil")
	}
	trustDomain, err := spiffeid.TrustDomainFromString(config.SPIRETrustDomain)
	if err != nil {
		return nil, fmt.Errorf("validating SPIRE trust domain: %w", err)
	}
	probes := [2]*dpuAgentIdentityTemplateData{
		probeTemplateData(trustDomain.Name(), "dpf-probe-serial-a"),
		probeTemplateData(trustDomain.Name(), "dpf-probe-serial-b"),
	}
	r := &DPUAgentIdentityRenderer{trustDomain: trustDomain}
	var spiffeIDErr, exchangedSPIFFEIDErr error
	r.spiffeIDTemplate, spiffeIDErr = parseIdentityTemplate("dpuAgentSPIFFEIDTemplate", identityTemplateOrDefault(config.DPUAgentSPIFFEIDTemplate), probes)
	r.exchangedSPIFFEIDTemplate, exchangedSPIFFEIDErr = parseIdentityTemplate("dpuAgentExchangedSPIFFEIDTemplate", identityTemplateOrDefault(config.DPUAgentExchangedSPIFFEIDTemplate), probes)
	if err := errors.Join(spiffeIDErr, exchangedSPIFFEIDErr); err != nil {
		return nil, err
	}
	return r, nil
}

// identityTemplateOrDefault applies the same default to either identity template.
func identityTemplateOrDefault(source string) string {
	if source == "" {
		return operatorv1.DefaultDPUAgentSPIFFEIDTemplate
	}
	return source
}

// Render normalizes the device serial once and supplies the same normalized value throughout
// both templates, including the copied DPU and DPUDevice specs.
func (r *DPUAgentIdentityRenderer) Render(dpu *provisioningv1.DPU, dpuDevice *provisioningv1.DPUDevice) (DPUAgentIdentity, error) {
	data, err := newDPUAgentIdentityTemplateData(r.trustDomain.Name(), dpu, dpuDevice)
	if err != nil {
		return DPUAgentIdentity{}, err
	}
	spiffeID, err := executeIdentityTemplate(r.spiffeIDTemplate, data, r.trustDomain.Name())
	if err != nil {
		return DPUAgentIdentity{}, fmt.Errorf("rendering dpuAgentSPIFFEIDTemplate: %w", err)
	}
	exchangedSPIFFEID, err := executeIdentityTemplate(r.exchangedSPIFFEIDTemplate, data, "")
	if err != nil {
		return DPUAgentIdentity{}, fmt.Errorf("rendering dpuAgentExchangedSPIFFEIDTemplate: %w", err)
	}
	return DPUAgentIdentity{
		SPIFFEID:          spiffeID,
		ExchangedSPIFFEID: exchangedSPIFFEID,
		ParentID:          identity.MakeAgentID(r.trustDomain.Name(), data.SerialNumber),
	}, nil
}

func parseIdentityTemplate(name, source string, probes [2]*dpuAgentIdentityTemplateData) (*template.Template, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}
	// Probe with two serials to reject templates that would assign every DPU the same identity.
	// The synthetic objects lack user-defined metadata, so SPIFFE ID validation is deferred until
	// the template is evaluated with the actual DPU and DPUDevice.
	var rendered [2]string
	for i, data := range probes {
		rendered[i], err = executeTemplate(tmpl, data)
		if err != nil {
			return nil, fmt.Errorf("validating %s: %w", name, err)
		}
	}
	if rendered[0] == rendered[1] {
		return nil, fmt.Errorf("%s must depend on the DPU serial number", name)
	}
	return tmpl, nil
}

func executeIdentityTemplate(tmpl *template.Template, data *dpuAgentIdentityTemplateData, expectedTrustDomain string) (string, error) {
	output, err := executeTemplate(tmpl, data)
	if err != nil {
		return "", err
	}
	id, err := spiffeid.FromString(output)
	if err != nil {
		return "", fmt.Errorf("invalid SPIFFE ID: %w", err)
	}
	if expectedTrustDomain != "" && id.TrustDomain().Name() != expectedTrustDomain {
		return "", fmt.Errorf("invalid SPIFFE ID: expected trust domain %q but got %q", expectedTrustDomain, id.TrustDomain().Name())
	}
	return id.String(), nil
}

func executeTemplate(tmpl *template.Template, data *dpuAgentIdentityTemplateData) (string, error) {
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}

func newDPUAgentIdentityTemplateData(trustDomain string, dpu *provisioningv1.DPU, dpuDevice *provisioningv1.DPUDevice) (*dpuAgentIdentityTemplateData, error) {
	if dpu == nil {
		return nil, fmt.Errorf("DPU is nil")
	}
	if dpuDevice == nil {
		return nil, fmt.Errorf("DPUDevice is nil")
	}
	normalized, err := identity.NormalizeSerial(dpuDevice.Spec.SerialNumber)
	if err != nil {
		return nil, fmt.Errorf("normalizing DPU serial number: %w", err)
	}
	dpuCopy := dpu.DeepCopy()
	dpuDeviceCopy := dpuDevice.DeepCopy()
	dpuCopy.Spec.SerialNumber = normalized
	dpuDeviceCopy.Spec.SerialNumber = normalized
	return &dpuAgentIdentityTemplateData{
		TrustDomain:   trustDomain,
		SerialNumber:  normalized,
		DPUMeta:       &dpuCopy.ObjectMeta,
		DPUSpec:       &dpuCopy.Spec,
		DPUDeviceMeta: &dpuDeviceCopy.ObjectMeta,
		DPUDeviceSpec: &dpuDeviceCopy.Spec,
	}, nil
}

// probeTemplateData supplies representative metadata and spec values for configuration-time
// rendering. Two probes with different serials let validation reject a constant identity while
// still allowing the serial to be referenced through SerialNumber, DPUSpec, or DPUDeviceSpec.
func probeTemplateData(trustDomain, serial string) *dpuAgentIdentityTemplateData {
	dpuMeta := &metav1.ObjectMeta{Name: "dpf-probe-dpu", Namespace: "dpf-probe", Labels: map[string]string{"tenant": "dpf-probe-tenant"}}
	dpuSpec := &provisioningv1.DPUSpec{SerialNumber: serial, DPUNodeName: "dpf-probe-node", DPUDeviceName: "dpf-probe-device"}
	dpuDeviceMeta := &metav1.ObjectMeta{Name: "dpf-probe-device", Namespace: "dpf-probe", Labels: map[string]string{"tenant": "dpf-probe-tenant", "issuer": "dpf-probe.example.test"}}
	dpuDeviceSpec := &provisioningv1.DPUDeviceSpec{SerialNumber: serial}
	return &dpuAgentIdentityTemplateData{
		TrustDomain:   trustDomain,
		SerialNumber:  serial,
		DPUMeta:       dpuMeta,
		DPUSpec:       dpuSpec,
		DPUDeviceMeta: dpuDeviceMeta,
		DPUDeviceSpec: dpuDeviceSpec,
	}
}
