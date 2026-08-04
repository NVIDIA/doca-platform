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

package state

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/constants"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/bfcfg"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type DefaultDPUArtifactGenerator struct {
	ServiceAccountCAPath string
}

func (g *DefaultDPUArtifactGenerator) GenerateBF3(ctx context.Context, req dutil.DPUArtifactRequest) ([]byte, error) {
	params, dpfOperatorConfig, err := g.resolveParamsWithBootstrapKubeconfig(ctx, req)
	if err != nil {
		return nil, err
	}
	return bfcfg.GenerateBFConfigWithParams(ctx, req.ControllerContext, req.DPU, req.Flavor, params, dpfOperatorConfig)
}

func (g *DefaultDPUArtifactGenerator) GenerateBF4(ctx context.Context, req dutil.DPUArtifactRequest) (dutil.BF4Artifact, error) {
	params, _, err := g.resolveParamsWithBootstrapKubeconfig(ctx, req)
	if err != nil {
		return dutil.BF4Artifact{}, err
	}
	userData, err := cloudinit.GenerateUserData(params)
	if err != nil {
		return dutil.BF4Artifact{}, fmt.Errorf("generating cloud-init user-data: %w", err)
	}
	networkCfg := cloudinit.GenerateNetworkCfg()
	return dutil.BF4Artifact{
		UserData:      []byte(userData.Content),
		NetworkConfig: []byte(networkCfg.Content),
	}, nil
}

const (
	trustBundlePEMConfigMapKey    = "bundle.pem"
	trustBundleSPIFFEConfigMapKey = "bundle.spiffe"
)

func (g *DefaultDPUArtifactGenerator) resolveParamsWithBootstrapKubeconfig(ctx context.Context, req dutil.DPUArtifactRequest) (cloudinit.Params, operatorv1.DPFOperatorConfig, error) {
	params, dpfOperatorConfig, err := cloudinit.ResolveParams(ctx, req.ControllerContext, req.DPU, req.Flavor)
	if err != nil {
		return cloudinit.Params{}, operatorv1.DPFOperatorConfig{}, err
	}

	// SPIFFE-mode DPUs authenticate with a SPIRE-issued JWT-SVID, not a bootstrap token,
	// so they get the trust bundle instead of a bootstrap kubeconfig.
	if cutil.IsSpiffeDPU(req.DPU) {
		if err := g.applySpiffeParams(ctx, req, &dpfOperatorConfig, &params); err != nil {
			return cloudinit.Params{}, operatorv1.DPFOperatorConfig{}, err
		}
		return params, dpfOperatorConfig, nil
	}

	apiServerAddress, proxyURL, err := cutil.ResolveAPIServerAddress(dpfOperatorConfig.Spec.Overrides, params.RedfishInterface)
	if err != nil {
		return cloudinit.Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("resolving API server address: %w", err)
	}
	caData, err := g.readCAData()
	if err != nil {
		return cloudinit.Params{}, operatorv1.DPFOperatorConfig{}, err
	}
	kubeconfigData, err := cutil.GenerateBootstrapKubeconfig(apiServerAddress, req.BootstrapToken, caData, proxyURL)
	if err != nil {
		return cloudinit.Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("generating DPU agent bootstrap kubeconfig: %w", err)
	}
	params.BootstrapKubeconfig = string(kubeconfigData)
	return params, dpfOperatorConfig, nil
}

// applySpiffeParams populates cloud-init SPIFFE/SPIRE params from DPFOperatorConfig
// and generates the tokenFile kubeconfig. BootstrapKubeconfig is intentionally unset.
func (g *DefaultDPUArtifactGenerator) applySpiffeParams(ctx context.Context, req dutil.DPUArtifactRequest, cfg *operatorv1.DPFOperatorConfig, params *cloudinit.Params) error {
	if !cutil.SpiffeEnabled(cfg) {
		return fmt.Errorf("DPU %s is SPIFFE-mode but cluster spec.security.spiffe is unset", req.DPU.Name)
	}
	spiffeCfg := cfg.Spec.Security.SPIFFE

	format := spiffeCfg.TrustBundle.Format
	if format == "" {
		format = operatorv1.SPIFFETrustBundleFormatPEM
	}
	bundleKey, bundlePath, err := trustBundleSettings(format)
	if err != nil {
		return err
	}
	bundle, err := g.readTrustBundle(ctx, req, spiffeCfg.TrustBundle, bundleKey)
	if err != nil {
		return err
	}
	host, portStr, err := net.SplitHostPort(spiffeCfg.SPIREServerAddress)
	if err != nil {
		return fmt.Errorf("parsing SPIRE server address %q: %w", spiffeCfg.SPIREServerAddress, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("parsing SPIRE server port %q: %w", portStr, err)
	}
	kubeconfigData, err := g.generateSpiffeKubeconfig(cfg, params.RedfishInterface)
	if err != nil {
		return err
	}

	params.SpiffeMode = true
	params.SPIFFEKubeconfig = string(kubeconfigData)
	params.SPIRETrustBundle = bundle
	params.SPIRETrustBundlePath = bundlePath
	params.SPIRETrustBundleFormat = string(format)
	params.SPIREServerHost = host
	params.SPIREServerPort = port
	params.SPIRETrustDomain = spiffeCfg.SPIRETrustDomain
	params.KubeAPIAudience = spiffeCfg.KubeAPIAudience
	params.SpiffeTokenPath = constants.SpiffeTokenPath
	params.SpiffeCertDir = path.Dir(constants.SpiffeTokenPath)
	params.SpiffeTokenFileName = path.Base(constants.SpiffeTokenPath)
	params.SpiffeAgentSocketPath = constants.SPIREAgentSocketPath
	params.SpiffeAgentSocketDir = path.Dir(constants.SPIREAgentSocketPath)
	params.SpiffePluginPath = constants.SPIREPluginPath
	return nil
}

func trustBundleSettings(format operatorv1.SPIFFETrustBundleFormat) (key, path string, err error) {
	switch format {
	case operatorv1.SPIFFETrustBundleFormatPEM:
		return trustBundlePEMConfigMapKey, constants.SPIRETrustBundlePEMPath, nil
	case operatorv1.SPIFFETrustBundleFormatSPIFFE:
		return trustBundleSPIFFEConfigMapKey, constants.SPIRETrustBundleSPIFFEPath, nil
	default:
		return "", "", fmt.Errorf("unsupported SPIRE trust bundle format %q", format)
	}
}

func (g *DefaultDPUArtifactGenerator) readTrustBundle(ctx context.Context, req dutil.DPUArtifactRequest, ref operatorv1.SPIFFETrustBundleConfigMapReference, key string) (string, error) {
	cm := &corev1.ConfigMap{}
	if err := req.ControllerContext.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, cm); err != nil {
		return "", fmt.Errorf("getting SPIRE trust bundle ConfigMap %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	bundle, ok := cm.Data[key]
	if !ok || bundle == "" {
		return "", fmt.Errorf("SPIRE trust bundle ConfigMap %s/%s missing non-empty %q key", ref.Namespace, ref.Name, key)
	}
	return bundle, nil
}

func (g *DefaultDPUArtifactGenerator) generateSpiffeKubeconfig(cfg *operatorv1.DPFOperatorConfig, redfishInterface bool) ([]byte, error) {
	apiServerAddress, proxyURL, err := cutil.ResolveAPIServerAddress(cfg.Spec.Overrides, redfishInterface)
	if err != nil {
		return nil, fmt.Errorf("resolving API server address for SPIFFE kubeconfig: %w", err)
	}
	caData, err := g.readCAData()
	if err != nil {
		return nil, err
	}
	kubeconfigData, err := cutil.GenerateSpiffeKubeconfig(apiServerAddress, constants.SpiffeTokenPath, caData, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("generating SPIFFE kubeconfig: %w", err)
	}
	return kubeconfigData, nil
}

func (g *DefaultDPUArtifactGenerator) serviceAccountCAPath() string {
	if g.ServiceAccountCAPath != "" {
		return g.ServiceAccountCAPath
	}
	return cutil.ServiceAccountCAPath
}

func (g *DefaultDPUArtifactGenerator) readCAData() ([]byte, error) {
	caPath := g.serviceAccountCAPath()
	caData, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate from %s: %w", caPath, err)
	}
	return caData, nil
}
