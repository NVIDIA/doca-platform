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

package opts

import (
	"fmt"

	provisioningconstants "github.com/nvidia/doca-platform/internal/provisioning/constants"
)

const (
	DPUAgentDir           = "/var/lib/dpf/dpuagent"
	DefaultCertDir        = DPUAgentDir + "/pki"
	DefaultKubeconfig     = DPUAgentDir + "/kubeconfig"
	DefaultNICDeviceCount = provisioningconstants.DefaultNICDeviceCount
)

type Options struct {
	ZeroTrustMode              bool
	ControlPlaneMTU            int32
	Kubeconfig                 string
	BootstrapKubeconfig        string
	CertDir                    string
	DPUName                    string
	DPUNamespace               string
	DPUUID                     string
	DPUFlavor                  string
	KubeadmSecretName          string
	KubeadmSecretNamespace     string
	BFBRegistryURL             string
	AstraEnabled               bool
	NICDeviceCount             int
	SkipSysctl                 bool
	SkipNetworkConfig          bool
	SkipDNSConfig              bool
	SkipContainerdConfigration bool
	SkipSFConfig               bool
	SkipVFMac                  bool
	SkipOVSRawScript           bool
	SkipKernelCmdLine          bool
	SkipRemoveBuiltinKubelet   bool
	SkipConfigureKubelet       bool
	SkipStartKubelet           bool
	SkipRebootMethodDiscovery  bool
	SkipAstra                  bool
}

func (o Options) Validate() error {
	if o.ZeroTrustMode {
		if o.Kubeconfig == "" && o.BootstrapKubeconfig == "" {
			return fmt.Errorf("kubeconfig or bootstrap-kubeconfig is required for zero trust mode")
		}
		if o.BootstrapKubeconfig != "" && o.CertDir == "" {
			return fmt.Errorf("cert-dir is required when bootstrap-kubeconfig is set")
		}
	}
	if o.ControlPlaneMTU < 0 {
		return fmt.Errorf("control plane MTU must be greater than 0: %d", o.ControlPlaneMTU)
	}
	if o.DPUName == "" {
		return fmt.Errorf("dpu name is required")
	}
	if o.DPUUID == "" {
		return fmt.Errorf("dpu uid is required")
	}
	if o.DPUFlavor == "" {
		return fmt.Errorf("dpu flavor is required")
	}
	if !o.SkipConfigureKubelet && o.KubeadmSecretName == "" {
		return fmt.Errorf("kubeadm secret name is required")
	}
	if o.NICDeviceCount < provisioningconstants.MinNICDeviceCount || o.NICDeviceCount > provisioningconstants.MaxNICDeviceCount {
		return fmt.Errorf("nic device count must be in range [%d, %d]: %d",
			provisioningconstants.MinNICDeviceCount, provisioningconstants.MaxNICDeviceCount, o.NICDeviceCount)
	}
	return nil
}
