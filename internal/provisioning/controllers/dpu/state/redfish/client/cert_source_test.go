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

package client

import (
	"context"
	"strings"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// setBundleName configures the trust bundle name for one test. The name is process wide, so it is
// restored afterwards and the rest of the suite keeps running against the default.
func setBundleName(t *testing.T, name string) {
	t.Helper()
	previous := CATrustBundleConfigMapName()
	SetCATrustBundleConfigMapName(name)
	t.Cleanup(func() {
		caTrustBundleConfigMapNameMu.Lock()
		defer caTrustBundleConfigMapNameMu.Unlock()
		caTrustBundleConfigMapName = previous
	})
}

// With no name configured the bundle is read from the default ConfigMap.
func TestReadCACredsUsesDefaultBundleName(t *testing.T) {
	ca := newCA(t, "test-ca")
	if got := CATrustBundleConfigMapName(); got != operatorv1.DefaultCATrustBundleConfigMapName {
		t.Fatalf("default bundle name = %q, want %q", got, operatorv1.DefaultCATrustBundleConfigMapName)
	}
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))

	if _, err := p.readCACreds(context.Background(), providerNamespace); err != nil {
		t.Fatalf("readCACreds: %v", err)
	}
}

// An operator-named bundle is read from the configured ConfigMap.
func TestReadCACredsUsesConfiguredBundleName(t *testing.T) {
	setBundleName(t, "enterprise-trust")
	ca := newCA(t, "test-ca")
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))

	if _, err := p.readCACreds(context.Background(), providerNamespace); err != nil {
		t.Fatalf("readCACreds: %v", err)
	}
}

// A configured name must not silently fall back to the default: reading the wrong bundle would
// present as an empty truststore rather than as a misconfiguration.
func TestReadCACredsDoesNotFallBackToDefaultBundleName(t *testing.T) {
	setBundleName(t, "enterprise-trust")
	ca := newCA(t, "test-ca")
	defaultBundle := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorv1.DefaultCATrustBundleConfigMapName,
			Namespace: providerNamespace,
		},
		Data: map[string]string{CATrustBundleKey: string(ca.pem)},
	}
	p, _ := newTestProvider(t, defaultBundle)

	_, err := p.readCACreds(context.Background(), providerNamespace)
	if err == nil {
		t.Fatal("readCACreds succeeded, want failure against the configured name")
	}
	if !strings.Contains(err.Error(), "enterprise-trust") {
		t.Fatalf("error %q does not name the configured bundle", err)
	}
}

// An empty name keeps the current one rather than leaving nothing to read.
func TestSetCATrustBundleConfigMapNameIgnoresEmpty(t *testing.T) {
	setBundleName(t, "enterprise-trust")

	SetCATrustBundleConfigMapName("")

	if got := CATrustBundleConfigMapName(); got != "enterprise-trust" {
		t.Fatalf("bundle name = %q, want %q", got, "enterprise-trust")
	}
}
