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

// Package certmanager carries the cert-manager issuer reference that the components of DPF resolve,
// apply and report the PKI on.
package certmanager

import (
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
)

// IssuerReference references a cert-manager issuer.
//
// It is deliberately not part of any API of DPF. The authority anchoring the PKI is chosen once in
// the dpf-operator Helm chart and read back from the cluster, so nothing configures a reference
// through a DPFOperatorConfig and there is no schema to validate it against.
type IssuerReference struct {
	// Name of the issuer.
	Name string

	// Kind of the issuer. An Issuer must exist in the namespace of the DPFOperatorConfig, a
	// ClusterIssuer is cluster scoped. cert-manager itself defaults an issuerRef without a kind to
	// Issuer, and DPF follows that default.
	Kind string

	// Group of the issuer. Only the cert-manager API group is honored: the kind above already commits
	// the reference to a cert-manager issuer and DPF resolves it as one, so any other group would be
	// stamped onto the certificates and then not honored.
	Group string
}

// IsExternalIssuer reports whether an issuer provided by the operator anchors the PKI in place of
// the self-signed root of DPF.
//
// It is derived from the anchor rather than configured: the dpf-operator chart creates its
// self-signed root, and the issuer over it, only when it has not been pointed at an authority of
// your own, so an anchor that is not that issuer is necessarily one the operator provided.
func (r IssuerReference) IsExternalIssuer() bool {
	return r.Name != "" && r.Name != operatorv1.GlobalRootIssuerName
}

// WithIssuerDefaults returns the reference with the kind and group defaulted the way cert-manager
// defaults them, so callers can stamp the result onto an issuerRef as is.
func (r IssuerReference) WithIssuerDefaults() IssuerReference {
	if r.Kind == "" {
		r.Kind = operatorv1.CertManagerIssuerKind
	}
	if r.Group == "" {
		r.Group = operatorv1.CertManagerGroup
	}
	return r
}
