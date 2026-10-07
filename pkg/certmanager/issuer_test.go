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

package certmanager

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
)

// TestIssuerReferenceWithIssuerDefaults verifies that a resolved anchor is completed the way
// cert-manager completes an issuerRef, so it can be stamped onto a Certificate as is.
func TestIssuerReferenceWithIssuerDefaults(t *testing.T) {
	tests := []struct {
		name string
		ref  IssuerReference
		want IssuerReference
	}{
		{
			name: "a kind and group are left alone when set",
			ref: IssuerReference{
				Name:  "openbao-issuer",
				Kind:  operatorv1.CertManagerClusterIssuerKind,
				Group: operatorv1.CertManagerGroup,
			},
			want: IssuerReference{
				Name:  "openbao-issuer",
				Kind:  operatorv1.CertManagerClusterIssuerKind,
				Group: operatorv1.CertManagerGroup,
			},
		},
		{
			name: "a missing kind and group are defaulted like cert-manager does",
			ref:  IssuerReference{Name: "namespaced-issuer"},
			want: IssuerReference{
				Name:  "namespaced-issuer",
				Kind:  operatorv1.CertManagerIssuerKind,
				Group: operatorv1.CertManagerGroup,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.WithIssuerDefaults(); got != tt.want {
				t.Errorf("WithIssuerDefaults() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIssuerReferenceIsExternalIssuer verifies the derived mode, which every other helper keys off.
// The chart creates the self-signed root, and the issuer over it, only when it was given no
// authority of its own, so any other anchor is one the operator provided.
func TestIssuerReferenceIsExternalIssuer(t *testing.T) {
	tests := []struct {
		name string
		ref  IssuerReference
		want bool
	}{
		{name: "an empty reference is self-signed", ref: IssuerReference{}, want: false},
		{
			name: "the global root issuer is self-signed",
			ref:  IssuerReference{Name: operatorv1.GlobalRootIssuerName},
			want: false,
		},
		{
			name: "any other issuer is an external issuer",
			ref:  IssuerReference{Name: "openbao-issuer"},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.IsExternalIssuer(); got != tt.want {
				t.Errorf("IsExternalIssuer() = %v, want %v", got, tt.want)
			}
		})
	}
}
