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
	"sync"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
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

// caTrustBundleConfigMapName is the ConfigMap holding the trust bundle the BMC server certificate
// is validated against. It is configured once at startup via SetCATrustBundleConfigMapName (from
// the --ca-trust-bundle-configmap-name flag) and is guarded by caTrustBundleConfigMapNameMu so
// setter/reader access is race-free regardless of timing.
var (
	caTrustBundleConfigMapNameMu sync.RWMutex
	caTrustBundleConfigMapName   = operatorv1.DefaultCATrustBundleConfigMapName
)

// SetCATrustBundleConfigMapName configures the ConfigMap the trust bundle is read from. With an
// external issuer the operator owns that ConfigMap and names it, so the name cannot be fixed at
// compile time. An empty name keeps the default rather than leaving nothing to read.
func SetCATrustBundleConfigMapName(name string) {
	if name == "" {
		return
	}
	caTrustBundleConfigMapNameMu.Lock()
	defer caTrustBundleConfigMapNameMu.Unlock()
	caTrustBundleConfigMapName = name
}

// CATrustBundleConfigMapName returns the ConfigMap the trust bundle is read from.
func CATrustBundleConfigMapName() string {
	caTrustBundleConfigMapNameMu.RLock()
	defer caTrustBundleConfigMapNameMu.RUnlock()
	return caTrustBundleConfigMapName
}

// getClientCertDir returns the currently configured client cert directory.
func getClientCertDir() string {
	clientCertDirMu.RLock()
	defer clientCertDirMu.RUnlock()
	return clientCertDir
}
