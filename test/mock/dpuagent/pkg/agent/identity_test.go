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

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
)

const sampleConf = `--dpu-name=dpu-node-mt2616abcdef-0000
--dpu-namespace=dpf-operator-system
--dpu-uid=1a2b3c4d-0000-4000-8000-000000000001
--dpu-type=BlueField4
--dpuflavor=/opt/dpf/dpuflavor.yaml
--control-plane-mtu=1500
--zero-trust-mode=true
--astra-enabled=true
--nic-device-count=1
--kubeadm-secret-name=dpu-node-mt2616abcdef-0000-kubeadm-join
--kubeadm-secret-namespace=dpf-operator-system
--bootstrap-kubeconfig=/var/lib/dpf/dpuagent/bootstrap-kubeconfig
`

func TestParseAgentConf(t *testing.T) {
	id, err := ParseAgentConf(sampleConf)
	if err != nil {
		t.Fatal(err)
	}
	if id.DPUName != "dpu-node-mt2616abcdef-0000" || id.DPUNamespace != "dpf-operator-system" ||
		id.DPUUID != "1a2b3c4d-0000-4000-8000-000000000001" || id.DPUType != "BlueField4" || !id.AstraEnabled ||
		id.KubeadmSecretName != "dpu-node-mt2616abcdef-0000-kubeadm-join" || id.KubeadmSecretNamespace != "dpf-operator-system" {
		t.Fatalf("unexpected identity %+v", *id)
	}
	opts := id.Options()
	if !opts.ZeroTrustMode || opts.DPUName != id.DPUName || opts.DPUUID != id.DPUUID || !opts.AstraEnabled {
		t.Fatalf("unexpected options %+v", opts)
	}
	if _, err := ParseAgentConf("--dpu-name=x\n"); err == nil {
		t.Fatal("expected error without uid and namespace")
	}
	defaulted, err := ParseAgentConf("--dpu-name=d\n--dpu-namespace=ns\n--dpu-uid=u\n")
	if err != nil {
		t.Fatal(err)
	}
	if defaulted.KubeadmSecretName != "d-kubeadm-join" || defaulted.KubeadmSecretNamespace != "ns" {
		t.Fatalf("kubeadm secret defaults not applied: %+v", *defaulted)
	}
}

func TestMaterializeAndLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uid")
	files := &artifact.AgentFiles{
		AgentConf:           sampleConf,
		BootstrapKubeconfig: "apiVersion: v1\nkind: Config\n",
		CATrustBundle:       "-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n",
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, kubeconfigFile), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := Materialize(files, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, kubeconfigFile)); !os.IsNotExist(err) {
		t.Fatal("stale kubeconfig must be removed on install")
	}
	conf, err := os.ReadFile(filepath.Join(dir, agentConfFile))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParseAgentConf(string(conf))
	if err != nil {
		t.Fatal(err)
	}
	if *loaded != *id {
		t.Fatalf("loaded identity %+v differs from %+v", *loaded, *id)
	}
}

func TestBuildOperationsMatchesRealAgent(t *testing.T) {
	want := []struct{ name, cond string }{
		{"Load kernel modules", "KernelModuleLoaded"},
		{"Configure Network", "NetworkConfigured"},
		{"Check Network", "NetworkChecked"},
		{"Report Last Startup Time", "LastStartupTimeReported"},
		{"Get Latest DPU", "DPURetrieved"},
		{"Configure DNS", "DNSConfigured"},
		{"Verify Static Files", "StaticFilesVerified"},
		{"Install Packages", "PackagesInstalled"},
		{"Manage Systemd Services", "SystemdServicesManaged"},
		{"Remove Built-in Kubelet", "BuiltinKubeletRemoved"},
		{"Set Sysctl", "SysctlParametersSet"},
		{"Check Sysctl Parameters", "SysctlParametersChecked"},
		{"Configure Kernel Cmd Line", "KernelCmdLineConfigured"},
		{"Configure Containerd", "ContainerdConfigured"},
		{"Ensure DPU Mode", "DpuModeEnsured"},
		{"NIC provisioning", "NICProvisioning"},
		{"NVConfig", "NVConfigApplied"},
		{"Handle Reboot", "RebootHandled"},
		{"Check Kernel Cmd Line", "KernelCmdLineChecked"},
		{"Configure SF", "SFCreated"},
		{"Set VF MAC", "VFMacSet"},
		{"Run OVS Script", "OVSScriptRun"},
		{"Set Netplan Underlay MTU", "UnderlayNetplanMTUConfigured"},
		{"Check Bridge", "BridgeChecked"},
		{"Configure Kubelet", "KubeletConfigured"},
		{"Start Kubelet", "KubeletStarted"},
		{"Report Node Labels", "NodeLabelsReported"},
		{"Release Host OS Init", "ReleaseHostOSInit"},
	}
	for _, mode := range []config.JoinMode{config.JoinSimulated, config.JoinKubelet} {
		ops := buildOperations(&runInputs{joinMode: mode})
		if len(ops) != len(want) {
			t.Fatalf("%s: got %d operations, want %d", mode, len(ops), len(want))
		}
		for i, op := range ops {
			if op.Name() != want[i].name || op.ConditionType() != want[i].cond {
				t.Errorf("%s: operation %d is %q/%q, want %q/%q", mode, i, op.Name(), op.ConditionType(), want[i].name, want[i].cond)
			}
		}
	}
}
