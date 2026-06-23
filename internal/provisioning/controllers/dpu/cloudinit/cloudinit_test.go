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

package cloudinit

import (
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func skipFirstEmptyLine(s string) string {
	return strings.TrimPrefix(s, "\n")
}

type cloudConfig struct {
	Debug      debugConfig  `json:"debug"`
	Users      []userConfig `json:"users"`
	ChPasswd   chPasswd     `json:"chpasswd"`
	WriteFiles []writeEntry `json:"write_files"`
	RunCmd     [][]string   `json:"runcmd"`
}

type debugConfig struct {
	Verbose bool `json:"verbose"`
}

type userConfig struct {
	Name       string  `json:"name"`
	LockPasswd bool    `json:"lock_passwd"`
	Groups     string  `json:"groups"`
	Sudo       string  `json:"sudo"`
	Shell      string  `json:"shell"`
	Passwd     *string `json:"passwd,omitempty"`
}

type chPasswd struct {
	List   string `json:"list"`
	Expire bool   `json:"expire"`
}

type writeEntry struct {
	Path        string `json:"path"`
	Permissions string `json:"permissions"`
	Content     string `json:"content"`
}

const (
	testDPUFlavorYAML = `
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: test-flavor
  namespace: test-ns
spec:
  bfcfgParameters:
  - UPDATE_DPU_OS=yes
  configFiles:
  - operation: override
    path: /etc/test.conf
    permissions: "0644"
    raw: |
      key=value
  grub:
    kernelParameters:
    - console=ttyAMA0
  nvconfig:
  - device: '*'
    parameters:
    - PF_TOTAL_SF=20
  ovs:
    rawConfigScript: |
      ovs-vsctl add-br br-test
`

	sampleKubeconfig = `apiVersion: v1
clusters:
- cluster:
    server: https://10.0.110.1:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
kind: Config
users:
- name: default
  user:
    token: test-token
`
)

var _ = Describe("Generate", func() {
	var (
		flavor        *provisioningv1.DPUFlavor
		flavorYAMLStr string
	)

	getWriteFile := func(parsed *cloudConfig, filePath string) *writeEntry {
		for i := range parsed.WriteFiles {
			if parsed.WriteFiles[i].Path == filePath {
				return &parsed.WriteFiles[i]
			}
		}
		Fail("write_files entry not found: " + filePath)
		return nil
	}

	generateAndParse := func(params Params) (userData File, parsed *cloudConfig) {
		Expect(params.ApplyFlavor(flavor)).To(Succeed())
		userData, err := GenerateUserData(params)
		Expect(err).NotTo(HaveOccurred())

		parsed = &cloudConfig{}
		Expect(yaml.Unmarshal([]byte(userData.Content), parsed)).To(Succeed())
		return userData, parsed
	}

	BeforeEach(func() {
		flavor = &provisioningv1.DPUFlavor{}
		Expect(yaml.Unmarshal([]byte(testDPUFlavorYAML), flavor)).To(Succeed())
		flavorBytes, err := yaml.Marshal(flavor)
		Expect(err).NotTo(HaveOccurred())
		flavorYAMLStr = string(flavorBytes)
	})

	It("should return dpf.cfg with correct path, permissions, and static content", func() {
		networkCfg := GenerateNetworkCfg()
		Expect(networkCfg.Path).To(Equal(DPFCfgPath))
		Expect(networkCfg.Permissions).To(Equal(DPFCfgPerms))
		Expect(networkCfg.Content).To(Equal(skipFirstEmptyLine(`
network:
  config: disabled
`)))
	})

	It("should return user-data with correct path and permissions", func() {
		userData, _ := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "test-secret",
			KubeadmSecretNamespace: "default",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://example/deb",
		})

		Expect(userData.Path).To(Equal(UserDataPath))
		Expect(userData.Permissions).To(Equal(UserDataPerms))
	})

	It("should produce valid YAML with all branches enabled", func() {
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "test-secret",
			KubeadmSecretNamespace: "default",
			BootstrapKubeconfig:    sampleKubeconfig,
			RedfishInterface:       true,
			OOBNetwork:             true,
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://bfb-registry:8080/deb",
			BFBRegistryURL:         "http://bfb-registry:8080",
			AstraEnabled:           true,
		})

		netplanFile := getWriteFile(parsed, "/etc/netplan/50-dpf-bootstrap.yaml")
		Expect(netplanFile.Permissions).To(Equal("0600"))
		expectedNetplan := skipFirstEmptyLine(`
network:
  renderer: networkd
  version: 2
  ethernets:
    oob_net0:
      dhcp4: true
      mtu: 1500
`)
		Expect(netplanFile.Content).To(Equal(expectedNetplan))

		configFile := getWriteFile(parsed, "/etc/test.conf")
		Expect(configFile.Content).To(Equal("key=value\n"))

		ovsFile := getWriteFile(parsed, "/opt/dpf/ovs.sh")
		Expect(ovsFile.Permissions).To(Equal("0755"))
		expectedOvs := skipFirstEmptyLine(`
#! /bin/bash
set -e
ovs-vsctl add-br br-test
`)
		Expect(ovsFile.Content).To(Equal(expectedOvs))

		flavorFile := getWriteFile(parsed, "/opt/dpf/dpuflavor.yaml")
		Expect(flavorFile.Permissions).To(Equal("0400"))
		Expect(flavorFile.Content).To(Equal(flavorYAMLStr))

		agentConf := getWriteFile(parsed, "/opt/dpf/dpuagent.conf")
		Expect(agentConf.Permissions).To(Equal("0600"))
		expectedAgentConf := skipFirstEmptyLine(`
--dpu-name=dpu-1
--dpu-namespace=ns-1
--dpu-uid=
--dpuflavor=/opt/dpf/dpuflavor.yaml
--control-plane-mtu=1500
--zero-trust-mode=true
--astra-enabled=true
--nic-device-count=8
--kubeadm-secret-name=test-secret
--bfb-registry-url=http://bfb-registry:8080
--kubeadm-secret-namespace=default
--bootstrap-kubeconfig=/var/lib/dpf/dpuagent/bootstrap-kubeconfig
`)
		Expect(agentConf.Content).To(Equal(expectedAgentConf))

		kubeconfigFile := getWriteFile(parsed, "/var/lib/dpf/dpuagent/bootstrap-kubeconfig")
		Expect(kubeconfigFile.Permissions).To(Equal("0600"))
		Expect(kubeconfigFile.Content).To(Equal(sampleKubeconfig))

		aptSource := getWriteFile(parsed, "/etc/apt/sources.list.d/dpf.list")
		Expect(aptSource.Permissions).To(Equal("0644"))
		Expect(aptSource.Content).To(Equal("deb [trusted=yes] http://bfb-registry:8080/deb ./\n"))

		installFile := getWriteFile(parsed, "/opt/dpf/install-dpu-agent.sh")
		Expect(installFile.Permissions).To(Equal("0755"))

		Expect(parsed.RunCmd).To(Equal([][]string{
			{"hostnamectl", "set-hostname", "test-dpu"},
			{"/opt/dpf/install-dpu-agent.sh"},
		}))
	})

	It("redfish mode: OOB network, kubeconfig present", func() {
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "test-secret",
			KubeadmSecretNamespace: "default",
			BootstrapKubeconfig:    sampleKubeconfig,
			RedfishInterface:       true,
			OOBNetwork:             true,
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://bfb-registry:8080/deb",
			BFBRegistryURL:         "http://bfb-registry:8080",
		})

		netplanFile := getWriteFile(parsed, "/etc/netplan/50-dpf-bootstrap.yaml")
		expectedNetplan := skipFirstEmptyLine(`
network:
  renderer: networkd
  version: 2
  ethernets:
    oob_net0:
      dhcp4: true
      mtu: 1500
`)
		Expect(netplanFile.Content).To(Equal(expectedNetplan))

		agentConf := getWriteFile(parsed, "/opt/dpf/dpuagent.conf")
		Expect(agentConf.Content).To(ContainSubstring("--zero-trust-mode=true"))
		Expect(agentConf.Content).To(ContainSubstring("--bootstrap-kubeconfig=/var/lib/dpf/dpuagent/bootstrap-kubeconfig"))
		Expect(agentConf.Content).To(ContainSubstring("--bfb-registry-url=http://bfb-registry:8080"))

		kubeconfigFile := getWriteFile(parsed, "/var/lib/dpf/dpuagent/bootstrap-kubeconfig")
		Expect(kubeconfigFile.Content).To(Equal(sampleKubeconfig))
	})

	It("trusted host mode without bootstrap kubeconfig: tmfifo network, no kubeconfig file", func() {
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "test-secret",
			KubeadmSecretNamespace: "default",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://[fe80::1%25tmfifo_net0]:11029/deb",
		})

		netplanFile := getWriteFile(parsed, "/etc/netplan/50-dpf-bootstrap.yaml")
		expectedNetplan := skipFirstEmptyLine(`
network:
  renderer: networkd
  version: 2
  ethernets:
    oob_net0:
      dhcp4: false
      dhcp6: false
      link-local: []
      optional: true
    tmfifo_net0:
      addresses:
      - fe80::2/64
      dhcp4: false
`)
		Expect(netplanFile.Content).To(Equal(expectedNetplan))

		agentConf := getWriteFile(parsed, "/opt/dpf/dpuagent.conf")
		Expect(agentConf.Content).To(ContainSubstring("--zero-trust-mode=false"))
		Expect(agentConf.Content).NotTo(ContainSubstring("--bootstrap-kubeconfig="))

		for _, f := range parsed.WriteFiles {
			Expect(f.Path).NotTo(Equal("/var/lib/dpf/dpuagent/bootstrap-kubeconfig"))
		}
	})

	It("trusted host mode with bootstrap kubeconfig: includes kubeconfig file and flag", func() {
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "test-secret",
			KubeadmSecretNamespace: "default",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://[fe80::1%25tmfifo_net0]:11029/deb",
			BootstrapKubeconfig:    sampleKubeconfig,
		})

		agentConf := getWriteFile(parsed, "/opt/dpf/dpuagent.conf")
		Expect(agentConf.Content).To(ContainSubstring("--zero-trust-mode=false"))
		Expect(agentConf.Content).To(ContainSubstring("--bootstrap-kubeconfig=/var/lib/dpf/dpuagent/bootstrap-kubeconfig"))

		kubeconfigFile := getWriteFile(parsed, "/var/lib/dpf/dpuagent/bootstrap-kubeconfig")
		Expect(kubeconfigFile.Content).To(Equal(sampleKubeconfig))
	})

	It("no password: should use chpasswd with default credentials", func() {
		params := Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "s",
			KubeadmSecretNamespace: "ns",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://example/deb",
		}
		Expect(params.ApplyFlavor(flavor)).To(Succeed())
		userData, err := GenerateUserData(params)
		Expect(err).NotTo(HaveOccurred())
		content := userData.Content
		Expect(content).To(ContainSubstring(skipFirstEmptyLine(`
users:
  - name: ubuntu
    lock_passwd: False
    groups: adm, audio, cdrom, dialout, dip, floppy, lxd, netdev, plugdev, sudo, video
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
chpasswd:
  list: |
    ubuntu:ubuntu
  expire: True
`)))
		Expect(content).NotTo(MatchRegexp(`(?m)^\s+passwd:`))
	})

	It("with password: should set passwd on user and omit chpasswd", func() {
		flavor.Spec.BFCfgParameters = append(flavor.Spec.BFCfgParameters, "ubuntu_PASSWORD=secret123")
		params := Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "s",
			KubeadmSecretNamespace: "ns",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://example/deb",
		}
		Expect(params.ApplyFlavor(flavor)).To(Succeed())
		userData, err := GenerateUserData(params)
		Expect(err).NotTo(HaveOccurred())
		content := userData.Content
		Expect(content).To(ContainSubstring(skipFirstEmptyLine(`
users:
  - name: ubuntu
    lock_passwd: False
    groups: adm, audio, cdrom, dialout, dip, floppy, lxd, netdev, plugdev, sudo, video
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    passwd: 'secret123'
`)))
		Expect(content).NotTo(ContainSubstring("chpasswd"))
	})

	It("should omit OVS script when not configured", func() {
		flavor.Spec.OVS.RawConfigScript = ""
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "s",
			KubeadmSecretNamespace: "ns",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://example/deb",
		})
		for _, f := range parsed.WriteFiles {
			Expect(f.Path).NotTo(Equal("/opt/dpf/ovs.sh"))
		}
	})
})

var _ = Describe("ExtractUbuntuPassword", func() {
	It("should return empty string when no parameters exist", func() {
		flavor := &provisioningv1.DPUFlavor{}
		Expect(ExtractUbuntuPassword(flavor)).To(BeEmpty())
	})

	It("should return empty string when ubuntu_PASSWORD is not present", func() {
		flavor := &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				BFCfgParameters: []string{"UPDATE_DPU_OS=yes", "WITH_NIC_FW_UPDATE=no"},
			},
		}
		Expect(ExtractUbuntuPassword(flavor)).To(BeEmpty())
	})

	It("should extract and single-quote a plain password", func() {
		flavor := &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				BFCfgParameters: []string{"ubuntu_PASSWORD=mypassword"},
			},
		}
		Expect(ExtractUbuntuPassword(flavor)).To(Equal("'mypassword'"))
	})

	It("should not double-quote an already single-quoted password", func() {
		flavor := &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				BFCfgParameters: []string{"ubuntu_PASSWORD='already-quoted'"},
			},
		}
		Expect(ExtractUbuntuPassword(flavor)).To(Equal("'already-quoted'"))
	})

	It("should extract a hashed password", func() {
		flavor := &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				BFCfgParameters: []string{"ubuntu_PASSWORD=$1$rvRv4qpw$mS6kYODr8oMxORt.TkiTB0"},
			},
		}
		Expect(ExtractUbuntuPassword(flavor)).To(Equal("'$1$rvRv4qpw$mS6kYODr8oMxORt.TkiTB0'"))
	})

	It("should return empty string when password value is empty", func() {
		flavor := &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				BFCfgParameters: []string{"ubuntu_PASSWORD="},
			},
		}
		Expect(ExtractUbuntuPassword(flavor)).To(BeEmpty())
	})

	It("should trim whitespace around key and value", func() {
		flavor := &provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				BFCfgParameters: []string{"  ubuntu_PASSWORD = secret  "},
			},
		}
		Expect(ExtractUbuntuPassword(flavor)).To(Equal("'secret'"))
	})
})
