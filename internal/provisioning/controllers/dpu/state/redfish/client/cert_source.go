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
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultClientCertDir is the default directory holding the mounted Redfish client key pair.
// The provisioning controller mounts the client-cert secret here (see the --redfish-client-cert-dir
// flag and the controller manifest).
const DefaultClientCertDir = "/etc/dpf/redfish-client-cert"

// clientCertDir is the directory holding the mounted Redfish client key pair (tls.crt / tls.key).
// The BF4 key pair is read from "<clientCertDir>-bf4". It is configured once at startup via
// SetClientCertDir (from the --redfish-client-cert-dir flag) and is guarded by clientCertDirMu so
// setter/reader access is race-free regardless of timing.
var (
	clientCertDirMu sync.RWMutex
	clientCertDir   = DefaultClientCertDir
)

// SetClientCertDir configures the directory holding the mounted Redfish client key pair
// (tls.crt / tls.key). The BF4 key pair is expected in "<dir>-bf4".
func SetClientCertDir(dir string) {
	clientCertDirMu.Lock()
	defer clientCertDirMu.Unlock()
	clientCertDir = dir
}

// getClientCertDir returns the currently configured client cert directory.
func getClientCertDir() string {
	clientCertDirMu.RLock()
	defer clientCertDirMu.RUnlock()
	return clientCertDir
}

// CertSource provides the certificates needed to build the verified Redfish mTLS client.
//
// The DPF CA public certificate is sourced from the existing dpf-provisioning-ca-secret via the
// Kubernetes API. The client key pair is the controller's mTLS identity and is read from a mounted
// directory (the client-cert secret is mounted into the controller pod).
type CertSource interface {
	// CACert returns the DPF CA public certificate (PEM) used to verify the BMC server certificate.
	CACert(ctx context.Context) ([]byte, error)
	// ClientKeyPair returns the Redfish client key pair used as the controller's mTLS identity.
	ClientKeyPair(ctx context.Context, isBF4 bool) (tls.Certificate, error)
}

// newCertSource returns the configured CertSource. The client key pair is read from the mounted
// client-cert directory; the CA certificate is read from the Kubernetes API.
func newCertSource(k8sClient client.Client, namespace string) CertSource {
	return &certSource{k8sClient: k8sClient, namespace: namespace, clientCertDir: getClientCertDir()}
}

type certSource struct {
	k8sClient     client.Client
	namespace     string
	clientCertDir string
}

func (s *certSource) CACert(ctx context.Context) ([]byte, error) {
	caTrustBundle := &corev1.ConfigMap{}
	if err := s.k8sClient.Get(ctx, types.NamespacedName{Name: CATrustBundleConfigMap, Namespace: s.namespace}, caTrustBundle); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q: %w", CATrustBundleConfigMap, err)
	}
	bundle := []byte(caTrustBundle.Data[CATrustBundleKey])
	if len(bundle) == 0 {
		return nil, fmt.Errorf("no %q in ConfigMap %q", CATrustBundleKey, CATrustBundleConfigMap)
	}
	return bundle, nil
}

// ClientKeyPair reads the Redfish client key pair (the controller's mTLS identity) from the mounted
// client-cert directory. The BF4 key pair is read from "<clientCertDir>-bf4".
func (s *certSource) ClientKeyPair(_ context.Context, isBF4 bool) (tls.Certificate, error) {
	if s.clientCertDir == "" {
		return tls.Certificate{}, fmt.Errorf("no Redfish client certificate directory configured")
	}
	dir := s.clientCertDir
	if isBF4 {
		dir += "-bf4"
	}
	certFile := filepath.Join(dir, corev1.TLSCertKey)
	keyFile := filepath.Join(dir, corev1.TLSPrivateKeyKey)
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to read client cert %s: %w", certFile, err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to read client key %s: %w", keyFile, err)
	}
	keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load client key pair from %s: %w", dir, err)
	}
	return keyPair, nil
}
