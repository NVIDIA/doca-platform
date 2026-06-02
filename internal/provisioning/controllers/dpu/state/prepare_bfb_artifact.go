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
	"os"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/bfcfg"
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

func (g *DefaultDPUArtifactGenerator) resolveParamsWithBootstrapKubeconfig(ctx context.Context, req dutil.DPUArtifactRequest) (cloudinit.Params, operatorv1.DPFOperatorConfig, error) {
	params, dpfOperatorConfig, err := cloudinit.ResolveParams(ctx, req.ControllerContext, req.DPU, req.Flavor)
	if err != nil {
		return cloudinit.Params{}, operatorv1.DPFOperatorConfig{}, err
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
