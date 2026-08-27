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
	"fmt"
	"strings"
	"text/template"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/spire/identity"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MaxIDSegmentLen is the maximum length of a namespace or service ID path segment. It matches the
// Kubernetes label value limit, since the service ID is also carried as the
// svc.dpu.nvidia.com/service pod label the entry selects on.
const MaxIDSegmentLen = 63

// normalizeIDSegment validates a value for use as a SPIFFE path segment. Like the serial builders
// it rejects rather than strips, which is many-to-one and could collapse two DPUServices onto one
// SPIFFE ID. The charset is RFC 3986 unreserved, the same policy as serials, and deliberately not
// DNS-1123: DPUDeployment generates service IDs of the form <deployment>_<service>_<digest>.
func normalizeIDSegment(kind, value string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(value))
	if s == "" {
		return "", fmt.Errorf("%s is empty after trimming", kind)
	}
	if len(s) > MaxIDSegmentLen {
		return "", fmt.Errorf("%s length %d exceeds maximum %d", kind, len(s), MaxIDSegmentLen)
	}
	for _, r := range s {
		if !identity.IsUnreservedRune(r) {
			return "", fmt.Errorf("%s %q contains invalid character %q (allowed: RFC 3986 unreserved)", kind, value, r)
		}
	}
	return s, nil
}

// dpuServiceIdentityTemplateData is the data a DPUService identity template is rendered against.
// It carries the DPU the entry is parented to rather than the DPUDevice, because that is what the
// DPUService controller resolves. Metadata and specs are snapshots; rendering never mutates
// informer-backed objects.
type dpuServiceIdentityTemplateData struct {
	// TrustDomain is the configured local SPIRE trust domain, for example cs.internal.
	TrustDomain string
	// SerialNumber is the normalized serial of the DPU the entry is parented to.
	SerialNumber string
	// Namespace is the normalized DPUService namespace.
	Namespace string
	// ServiceID is the normalized DPUService service ID, for example my-service.
	ServiceID string
	// DPUMeta exposes DPU metadata. For example, a label is accessed with
	// {{ index .DPUMeta.Labels "tenant" }}.
	DPUMeta *metav1.ObjectMeta
	// DPUSpec exposes the DPU spec with SerialNumber normalized to match .SerialNumber.
	DPUSpec *provisioningv1.DPUSpec
	// DPUServiceMeta exposes DPUService metadata, so an identity can carry a tenant taken from
	// the object rather than hardcoded in the template.
	DPUServiceMeta *metav1.ObjectMeta
	// DPUServiceSpec exposes the DPUService spec.
	DPUServiceSpec *dpuservicev1.DPUServiceSpec
}

// DPUServiceIdentity contains the pre- and post-exchange subjects for one DPUService on one DPU.
type DPUServiceIdentity struct {
	// SPIFFEID is the pre-exchange subject registered in the ClusterStaticEntry.
	SPIFFEID string
	// ExchangedSPIFFEID is the post-exchange subject. DPF renders and validates it so the whole
	// identity layout is declared in one place, but does not consume it: unlike the DPU Agent,
	// a DPUService identity is never presented back to DPF.
	ExchangedSPIFFEID string
	// ParentID is the SPIRE agent of the DPU the entry is parented to.
	ParentID string
}

// DPUServiceIdentityRenderer renders DPUService identities from validated operator configuration.
type DPUServiceIdentityRenderer struct {
	spiffeIDTemplate          *template.Template
	exchangedSPIFFEIDTemplate *template.Template
	trustDomain               spiffeid.TrustDomain
}

// NewDPUServiceIdentityRenderer parses and validates both configured identity templates.
func NewDPUServiceIdentityRenderer(config *operatorv1.SPIFFEConfiguration) (*DPUServiceIdentityRenderer, error) {
	if config == nil {
		return nil, fmt.Errorf("SPIFFE configuration is nil")
	}
	trustDomain, err := spiffeid.TrustDomainFromString(config.SPIRETrustDomain)
	if err != nil {
		return nil, fmt.Errorf("validating SPIRE trust domain: %w", err)
	}

	r := &DPUServiceIdentityRenderer{trustDomain: trustDomain}
	// The pre-exchange subject is what SPIRE issues, so it must stay in the local trust domain.
	// The post-exchange one is by definition reissued elsewhere, so its trust domain is free.
	r.spiffeIDTemplate, err = parseDPUServiceTemplate("dpuServiceSPIFFEIDTemplate",
		dpuServiceTemplateOrDefault(config.DPUServiceSPIFFEIDTemplate), trustDomain.Name())
	if err != nil {
		return nil, err
	}
	r.exchangedSPIFFEIDTemplate, err = parseDPUServiceTemplate("dpuServiceExchangedSPIFFEIDTemplate",
		dpuServiceTemplateOrDefault(config.DPUServiceExchangedSPIFFEIDTemplate), "")
	if err != nil {
		return nil, err
	}
	return r, nil
}

func dpuServiceTemplateOrDefault(source string) string {
	if source == "" {
		return operatorv1.DefaultDPUServiceSPIFFEIDTemplate
	}
	return source
}

// parseDPUServiceTemplate rejects a template at configuration time rather than per DPUService.
// A DPUService workload is identified by the namespace, the service ID and the DPU it runs on, so
// the template has to vary with all three. Each probe changes exactly one of them and the
// rendered identity has to change with it.
//
// Nothing detects a collapse later: spire-controller-manager keys entries on the SPIFFE ID, the
// parent ID and the selectors, and two DPUServices in different namespaces differ in their
// k8s:ns selector. Both entries are therefore created and both issue the same SVID, so a template
// that drops one of the three has to be rejected here or not at all.
func parseDPUServiceTemplate(name, source, expectedTrustDomain string) (*template.Template, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}
	render := func(probe dpuServiceProbe) (string, error) {
		out, err := executeDPUServiceTemplate(tmpl,
			dpuServiceProbeTemplateData(expectedTrustDomain, probe.serial, probe.serviceID, probe.namespace), expectedTrustDomain)
		if err != nil {
			return "", fmt.Errorf("validating %s: %w", name, err)
		}
		return out, nil
	}

	const base, other = "dpf-probe-a", "dpf-probe-b"
	const baseNamespace, otherNamespace = "dpf-probe-ns-a", "dpf-probe-ns-b"
	first, err := render(dpuServiceProbe{base, base, baseNamespace})
	if err != nil {
		return nil, err
	}
	for _, probe := range []struct {
		dpuServiceProbe
		missing string
	}{
		{dpuServiceProbe{other, base, baseNamespace}, "DPU serial number"},
		{dpuServiceProbe{base, other, baseNamespace}, "service ID"},
		{dpuServiceProbe{base, base, otherNamespace}, "namespace"},
	} {
		out, err := render(probe.dpuServiceProbe)
		if err != nil {
			return nil, err
		}
		if out == first {
			return nil, fmt.Errorf("%s must depend on the %s", name, probe.missing)
		}
	}

	// A template joining two fields without a delimiter aliases across their boundary: namespace
	// "a-b" with service "c" renders the same identity as namespace "a" with service "b-c". The
	// probes above cannot see it, they vary one field at a time using values that share no prefix.
	// Each boundary needs two pairs, one aliasing under a bare join and one under a "-" join.
	for _, pair := range [][2]dpuServiceProbe{
		{{base, "c", "ab"}, {base, "bc", "a"}},
		{{base, "c", "a-b"}, {base, "b-c", "a"}},
		{{"ab", "c", baseNamespace}, {"a", "bc", baseNamespace}},
		{{"a-b", "c", baseNamespace}, {"a", "b-c", baseNamespace}},
		{{"ab", base, "c"}, {"a", base, "bc"}},
		{{"a-b", base, "c"}, {"a", base, "b-c"}},
	} {
		left, err := render(pair[0])
		if err != nil {
			return nil, err
		}
		right, err := render(pair[1])
		if err != nil {
			return nil, err
		}
		if left == right {
			return nil, fmt.Errorf("%s must separate the fields it renders, joining two of them without a delimiter makes distinct workloads share an identity", name)
		}
	}
	return tmpl, nil
}

// dpuServiceProbe is one set of configuration-time input values.
type dpuServiceProbe struct {
	serial, serviceID, namespace string
}

// Render returns both subjects and the parent agent ID for one DPUService on one DPU. The parent
// is the DPU's own SPIRE agent, the same one parenting the DPU Agent entry, so a DPUService only
// gets an identity on DPUs that completed SPIFFE node attestation.
func (r *DPUServiceIdentityRenderer) Render(dpuService *dpuservicev1.DPUService, dpu *provisioningv1.DPU, serviceID string) (DPUServiceIdentity, error) {
	data, err := newDPUServiceIdentityTemplateData(r.trustDomain.Name(), dpuService, dpu, serviceID)
	if err != nil {
		return DPUServiceIdentity{}, err
	}
	spiffeID, err := executeDPUServiceTemplate(r.spiffeIDTemplate, data, r.trustDomain.Name())
	if err != nil {
		return DPUServiceIdentity{}, fmt.Errorf("rendering dpuServiceSPIFFEIDTemplate: %w", err)
	}
	exchanged, err := executeDPUServiceTemplate(r.exchangedSPIFFEIDTemplate, data, "")
	if err != nil {
		return DPUServiceIdentity{}, fmt.Errorf("rendering dpuServiceExchangedSPIFFEIDTemplate: %w", err)
	}
	return DPUServiceIdentity{
		SPIFFEID:          spiffeID,
		ExchangedSPIFFEID: exchanged,
		ParentID:          identity.MakeAgentID(r.trustDomain.Name(), data.SerialNumber),
	}, nil
}

func executeDPUServiceTemplate(tmpl *template.Template, data *dpuServiceIdentityTemplateData, expectedTrustDomain string) (string, error) {
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		return "", err
	}
	id, err := spiffeid.FromString(output.String())
	if err != nil {
		return "", fmt.Errorf("invalid SPIFFE ID: %w", err)
	}
	if expectedTrustDomain != "" && id.TrustDomain().Name() != expectedTrustDomain {
		return "", fmt.Errorf("invalid SPIFFE ID: expected trust domain %q but got %q", expectedTrustDomain, id.TrustDomain().Name())
	}
	return id.String(), nil
}

func newDPUServiceIdentityTemplateData(trustDomain string, dpuService *dpuservicev1.DPUService, dpu *provisioningv1.DPU, serviceID string) (*dpuServiceIdentityTemplateData, error) {
	if dpuService == nil {
		return nil, fmt.Errorf("DPUService is nil")
	}
	if dpu == nil {
		return nil, fmt.Errorf("DPU is nil")
	}
	serial, err := identity.NormalizeSerial(dpu.Spec.SerialNumber)
	if err != nil {
		return nil, fmt.Errorf("normalizing DPU serial number: %w", err)
	}
	namespace, err := normalizeIDSegment("namespace", dpuService.Namespace)
	if err != nil {
		return nil, err
	}
	service, err := normalizeIDSegment("service ID", serviceID)
	if err != nil {
		return nil, err
	}
	dpuCopy := dpu.DeepCopy()
	dpuCopy.Spec.SerialNumber = serial
	dpuServiceCopy := dpuService.DeepCopy()
	return &dpuServiceIdentityTemplateData{
		TrustDomain:    trustDomain,
		SerialNumber:   serial,
		Namespace:      namespace,
		ServiceID:      service,
		DPUMeta:        &dpuCopy.ObjectMeta,
		DPUSpec:        &dpuCopy.Spec,
		DPUServiceMeta: &dpuServiceCopy.ObjectMeta,
		DPUServiceSpec: &dpuServiceCopy.Spec,
	}, nil
}

// dpuServiceProbeTemplateData supplies representative values for configuration-time rendering.
// Everything naming the DPU varies with serial and everything naming the DPUService varies with
// serviceID, so a template may reach either through the scalar field or through the embedded
// metadata and spec without validation mistaking it for a constant. Both metadata carry the same
// keys under Labels and Annotations because a template may qualify the identity from either.
//
// The trust domain falls back to a placeholder for the exchanged template, whose own trust domain
// is not constrained but still has to parse.
func dpuServiceProbeTemplateData(trustDomain, serial, serviceID, namespace string) *dpuServiceIdentityTemplateData {
	if trustDomain == "" {
		trustDomain = "dpf-probe.example"
	}
	// Constant across probes: a label is not tied to the namespace, so a template qualifying the
	// identity by one alone still lets two namespaces collide.
	tenant := map[string]string{"tenant": "dpf-probe-tenant"}
	return &dpuServiceIdentityTemplateData{
		TrustDomain:  trustDomain,
		SerialNumber: serial,
		Namespace:    namespace,
		ServiceID:    serviceID,
		DPUMeta: &metav1.ObjectMeta{Name: "dpf-probe-dpu-" + serial, Namespace: "dpf-probe",
			Labels: tenant, Annotations: tenant},
		DPUSpec: &provisioningv1.DPUSpec{SerialNumber: serial,
			DPUNodeName: "dpf-probe-node-" + serial, DPUDeviceName: "dpf-probe-device-" + serial},
		DPUServiceMeta: &metav1.ObjectMeta{Name: "dpf-probe-service-" + serviceID, Namespace: namespace,
			Labels: tenant, Annotations: tenant},
		DPUServiceSpec: &dpuservicev1.DPUServiceSpec{ServiceID: &serviceID},
	}
}
