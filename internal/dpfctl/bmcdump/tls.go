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

package bmcdump

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	redfishclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// resolveCAPool returns the pool that BMC server certificates are verified against.
//
// Every BMC reachable through a DPUDevice has had its server certificate reissued by the DPF
// provisioning CA, which the operator publishes in a ConfigMap alongside the DPUDevices. Reading it
// from the cluster is therefore enough to verify any BMC this command can discover, with no input
// from the caller. CACertFile covers a cluster anchored to an authority of its own, where the
// bundle is the operator's to fill but may be named or distributed differently.
func resolveCAPool(ctx context.Context, c client.Client, opts CollectOptions) (*x509.CertPool, error) {
	if opts.CACertFile != "" {
		caPEM, err := os.ReadFile(opts.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("reading bmc ca certificate file %s: %w", opts.CACertFile, err)
		}
		return caPoolFromPEM(caPEM, opts.CACertFile)
	}

	namespace, bundleName, err := trustBundleLocation(ctx, c, opts.Namespace)
	if err != nil {
		return nil, err
	}

	trustBundle := &corev1.ConfigMap{}
	bundleKey := client.ObjectKey{Namespace: namespace, Name: bundleName}
	if err := c.Get(ctx, bundleKey, trustBundle); err != nil {
		return nil, fmt.Errorf("reading bmc ca trust bundle %s/%s: %w (pass --ca-cert-file to supply the CA directly)",
			namespace, bundleName, err)
	}
	source := fmt.Sprintf("ConfigMap %s/%s key %q", namespace, bundleName, redfishclient.CATrustBundleKey)
	return caPoolFromPEM([]byte(trustBundle.Data[redfishclient.CATrustBundleKey]), source)
}

// trustBundleLocation asks the DPFOperatorConfig where the trust bundle is published, since operators
// can rename it and the operator publishes it in the config's own namespace, which need not be the
// namespace of the DPUDevices.
//
// Callers that cannot list DPFOperatorConfigs, and clusters with none, fall back to the default name in
// fallbackNamespace: the ConfigMap the dump needs is the one worth reporting as missing, not the config
// that would have renamed it.
func trustBundleLocation(ctx context.Context, c client.Client, fallbackNamespace string) (namespace, name string, err error) {
	config, err := utils.GetDPFOperatorConfig(ctx, c)
	if err != nil {
		if errors.Is(err, utils.ErrDPFOperatorConfigNotFound) || apierrors.IsForbidden(err) {
			return fallbackNamespace, operatorv1.DefaultCATrustBundleConfigMapName, nil
		}
		return "", "", fmt.Errorf("resolving the bmc ca trust bundle name: %w", err)
	}
	return config.Namespace, config.GetCATrustBundleConfigMapName(), nil
}

// caPoolFromPEM parses a trust bundle with the provisioning client's own parser, so dpfctl accepts
// exactly the bundles the controller does, and names the source it came from: the error is only
// actionable if it says which file or ConfigMap to go and look at.
func caPoolFromPEM(caPEM []byte, source string) (*x509.CertPool, error) {
	pool, err := redfishclient.CAPoolFromBundle(caPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: bmc ca trust source %s", err, source)
	}
	return pool, nil
}
