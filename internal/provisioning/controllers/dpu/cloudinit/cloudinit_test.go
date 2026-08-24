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
	"context"
	"path"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/constants"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func skipFirstEmptyLine(s string) string {
	return strings.TrimPrefix(s, "\n")
}

type cloudConfig struct {
	PreserveHostname *bool        `json:"preserve_hostname"`
	Hostname         string       `json:"hostname"`
	ManageEtcHosts   string       `json:"manage_etc_hosts"`
	Debug            debugConfig  `json:"debug"`
	Users            []userConfig `json:"users"`
	ChPasswd         chPasswd     `json:"chpasswd"`
	WriteFiles       []writeEntry `json:"write_files"`
	RunCmd           [][]string   `json:"runcmd"`
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
  - operation: override
    path: /etc/from-configmap.conf
    permissions: "0644"
    type: agent-applied
    contentFrom:
      configMapKeyRef:
        name: test-cm
        key: profile.conf
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
		userData, parsed := generateAndParse(Params{
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
		agentConf := getWriteFile(parsed, "/opt/dpf/dpuagent.conf")
		Expect(agentConf.Content).NotTo(ContainSubstring("--dpu-type="))
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
			DPUType:                provisioningv1.DPUTypeBlueField4,
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
      dhcp6: false
      accept-ra: false
      mtu: 1500
`)
		Expect(netplanFile.Content).To(Equal(expectedNetplan))

		configFile := getWriteFile(parsed, "/etc/test.conf")
		Expect(configFile.Content).To(Equal("key=value\n"))
		for _, f := range parsed.WriteFiles {
			Expect(f.Path).NotTo(Equal("/etc/from-configmap.conf"))
		}

		ovsFile := getWriteFile(parsed, "/opt/dpf/ovs.sh")
		Expect(ovsFile.Permissions).To(Equal("0700"))
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
--dpu-type=BlueField4
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
		Expect(installFile.Permissions).To(Equal("0700"))

		Expect(parsed.PreserveHostname).NotTo(BeNil())
		Expect(*parsed.PreserveHostname).To(BeFalse())
		Expect(parsed.Hostname).To(Equal("test-dpu"))
		Expect(parsed.ManageEtcHosts).To(Equal("localhost"))
		Expect(parsed.RunCmd).To(Equal([][]string{
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
      dhcp6: false
      accept-ra: false
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
      accept-ra: false
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

	It("SPIFFE mode: renders SPIRE/spiffe-helper configs, kubeconfig, package install, and service ordering", func() {
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "test-secret",
			KubeadmSecretNamespace: "default",
			SpiffeMode:             true,
			SPIFFEKubeconfig:       sampleKubeconfig,
			SPIRETrustBundle:       "bundle-pem",
			SPIRETrustBundlePath:   constants.SPIRETrustBundlePEMPath,
			SPIRETrustBundleFormat: "pem",
			SPIREServerHost:        "spire-server.spire.svc",
			SPIREServerPort:        8081,
			SPIRETrustDomain:       "cs.internal",
			KubeAPIAudience:        "dpf",
			SpiffeTokenPath:        constants.SpiffeTokenPath,
			SpiffeCertDir:          path.Dir(constants.SpiffeTokenPath),
			SpiffeTokenFileName:    path.Base(constants.SpiffeTokenPath),
			SpiffeAgentSocketPath:  constants.SPIREAgentSocketPath,
			SpiffeAgentSocketDir:   path.Dir(constants.SPIREAgentSocketPath),
			SpiffePluginPath:       constants.SPIREPluginPath,
			RedfishInterface:       true,
			OOBNetwork:             true,
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://bfb-registry:8080/deb",
		})

		agentConf := getWriteFile(parsed, "/opt/dpf/dpuagent.conf")
		Expect(agentConf.Content).To(ContainSubstring("--spiffe-mode=true"))
		Expect(agentConf.Content).To(ContainSubstring("--kubeconfig=/var/lib/dpf/dpuagent/kubeconfig"))
		Expect(agentConf.Content).To(ContainSubstring("--token-file-path=" + constants.SpiffeTokenPath))
		Expect(agentConf.Content).NotTo(ContainSubstring("--bootstrap-kubeconfig="))

		kubeconfigFile := getWriteFile(parsed, constants.SpiffeKubeconfigPath)
		Expect(kubeconfigFile.Permissions).To(Equal("0600"))

		agentCfg := getWriteFile(parsed, constants.SPIREAgentConfigPath)
		// Every reader of the SPIRE agent configuration runs as root, so it stays
		// unreadable to anything else on the DPU.
		Expect(agentCfg.Permissions).To(Equal("0600"))
		Expect(agentCfg.Content).To(ContainSubstring(constants.SPIREPluginPath))
		Expect(agentCfg.Content).To(ContainSubstring(`trust_bundle_path = "` + constants.SPIRETrustBundlePEMPath + `"`))
		Expect(agentCfg.Content).To(ContainSubstring(`trust_bundle_format = "pem"`))
		Expect(agentCfg.Content).To(ContainSubstring(`trust_bundle_path = "` + constants.SPIRETrustBundlePEMPath + `"`))
		Expect(agentCfg.Content).To(ContainSubstring(`trust_bundle_format = "pem"`))
		Expect(agentCfg.Content).To(ContainSubstring("# spire-k8s-workload-attestor"))
		Expect(agentCfg.Content).NotTo(ContainSubstring(`WorkloadAttestor "k8s"`))

		attestorCfg := getWriteFile(parsed, "/etc/spire/agent/k8s-workload-attestor.conf")
		Expect(attestorCfg.Permissions).To(Equal("0600"))
		Expect(attestorCfg.Content).To(ContainSubstring(`WorkloadAttestor "k8s"`))
		Expect(attestorCfg.Content).To(ContainSubstring(`certificate_path = "/var/lib/kubelet/pki/kubelet-client-current.pem"`))
		Expect(attestorCfg.Content).To(ContainSubstring(`node_name_env = "MY_NODE_NAME"`))

		agentDropIn := getWriteFile(parsed, "/etc/systemd/system/spire-agent.service.d/k8s-workload-attestor.conf")
		Expect(agentDropIn.Content).To(ContainSubstring("Environment=MY_NODE_NAME=%H"))

		// The DPU agent splices the attestor in from a loop once kubelet has produced
		// usable certificates, so cloud-init writes the configuration it merges but no
		// units and no bootstrap script.
		for _, f := range parsed.WriteFiles {
			Expect(f.Path).NotTo(Equal("/etc/systemd/system/spire-k8s-workload-attestor.service"))
			Expect(f.Path).NotTo(Equal("/etc/systemd/system/spire-k8s-workload-attestor.path"))
			Expect(f.Path).NotTo(Equal("/opt/dpf/enable-spire-k8s-workload-attestor.sh"))
		}

		helperCfg := getWriteFile(parsed, constants.SpiffeHelperConfigPath)
		Expect(helperCfg.Content).To(ContainSubstring(constants.SPIREAgentSocketPath))
		Expect(helperCfg.Content).To(ContainSubstring(`cert_dir = "` + path.Dir(constants.SpiffeTokenPath) + `"`))
		Expect(helperCfg.Content).To(ContainSubstring(`jwt_audience="dpf"`))
		Expect(helperCfg.Content).To(ContainSubstring(`jwt_svid_file_name="` + path.Base(constants.SpiffeTokenPath) + `"`))
		Expect(helperCfg.Content).To(ContainSubstring("jwt_svid_file_mode = 0600"))
		Expect(helperCfg.Content).NotTo(ContainSubstring("token_exchange_endpoint"))
		Expect(helperCfg.Content).NotTo(ContainSubstring("spiffe_id ="))

		dropIn := getWriteFile(parsed, "/etc/systemd/system/dpu-agent.service.d/spiffe.conf")
		Expect(dropIn.Content).To(ContainSubstring("After=spire-agent.service spiffe-helper.service"))
		Expect(dropIn.Content).To(ContainSubstring("RestartSec=10"))
		// StartLimit* are [Unit] directives in systemd; they are ignored under [Service].
		// Assert presence first so the ordering check below cannot pass on an absent directive (Index == -1).
		Expect(dropIn.Content).To(ContainSubstring("StartLimitIntervalSec=300"))
		Expect(dropIn.Content).To(ContainSubstring("StartLimitBurst=30"))
		Expect(strings.Index(dropIn.Content, "StartLimitIntervalSec=300")).To(BeNumerically("<", strings.Index(dropIn.Content, "[Service]")))

		installScript := getWriteFile(parsed, "/opt/dpf/install-dpu-agent.sh")
		Expect(installScript.Content).To(ContainSubstring("mkdir -p /var/lib/spire/agent"))
		Expect(installScript.Content).To(ContainSubstring(path.Dir(constants.SpiffeTokenPath)))
		Expect(installScript.Content).To(ContainSubstring("exit 1"))
		Expect(installScript.Content).To(ContainSubstring("SPIRE_PACKAGES=(spire-agent spiffe-helper dpu-hw-agent)"))
		Expect(installScript.Content).To(ContainSubstring("failed to install SPIRE packages after 60 attempts"))
		Expect(installScript.Content).To(ContainSubstring("systemctl enable --now spire-agent.service"))
		Expect(installScript.Content).To(ContainSubstring("systemctl enable --now spiffe-helper.service"))
		Expect(installScript.Content).To(ContainSubstring("apt-get install -y --no-install-recommends dpu-agent"))
		Expect(installScript.Content).To(ContainSubstring("systemctl enable --now dpu-agent.service"))
		spireIdx := strings.Index(installScript.Content, "systemctl enable --now spiffe-helper.service")
		agentIdx := strings.Index(installScript.Content, "systemctl enable --now dpu-agent.service")
		Expect(spireIdx).To(BeNumerically("<", agentIdx), "SPIRE services should start before dpu-agent")
	})

	It("writes the CA trust bundle and runs update-ca-certificates when set", func() {
		caBundle := skipFirstEmptyLine(`
-----BEGIN CERTIFICATE-----
MIIBdummycertbase64contentline1
MIIBdummycertbase64contentline2
-----END CERTIFICATE-----`)
		_, parsed := generateAndParse(Params{
			DPUHostName:            "test-dpu",
			KubeadmSecretName:      "s",
			KubeadmSecretNamespace: "ns",
			ControlPlaneMTU:        1500,
			DPUName:                "dpu-1",
			DPUNamespace:           "ns-1",
			DPUAgentRepoURL:        "http://example/deb",
			CATrustBundle:          caBundle,
		})

		caFile := getWriteFile(parsed, "/usr/local/share/ca-certificates/dpf-ca.crt")
		Expect(caFile.Permissions).To(Equal("0644"))
		Expect(caFile.Content).To(Equal(caBundle + "\n"))

		// update-ca-certificates must run before installing the agent
		// (which fetches packages over HTTPS).
		Expect(parsed.RunCmd).To(Equal([][]string{
			{"update-ca-certificates"},
			{"/opt/dpf/install-dpu-agent.sh"},
		}))
	})

	It("omits the CA trust bundle file and update-ca-certificates when empty", func() {
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
			Expect(f.Path).NotTo(Equal("/usr/local/share/ca-certificates/dpf-ca.crt"))
		}
		Expect(parsed.RunCmd).To(Equal([][]string{
			{"/opt/dpf/install-dpu-agent.sh"},
		}))
	})
})

var _ = Describe("resolveCATrustBundle", func() {
	const ns = "dpf-operator-system"

	newControllerCtx := func(objs ...client.Object) *util.ControllerContext {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(operatorv1.AddToScheme(scheme)).To(Succeed())
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		return &util.ControllerContext{Client: c}
	}

	config := &operatorv1.DPFOperatorConfig{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: ns}}

	It("returns the trimmed PEM bundle from the ConfigMap", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: operatorv1.DefaultCATrustBundleConfigMapName, Namespace: ns},
			Data:       map[string]string{operatorv1.CATrustBundleKey: "\n-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----\n\n"},
		}
		ctrlCtx := newControllerCtx(cm)
		bundle, err := resolveCATrustBundle(context.Background(), ctrlCtx, config)
		Expect(err).NotTo(HaveOccurred())
		Expect(bundle).To(Equal("-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----"))
	})

	It("returns an empty bundle (no error) when the ConfigMap does not exist", func() {
		ctrlCtx := newControllerCtx()
		bundle, err := resolveCATrustBundle(context.Background(), ctrlCtx, config)
		Expect(err).NotTo(HaveOccurred())
		Expect(bundle).To(BeEmpty())
	})
})

var _ = Describe("ResolveParams registry scheme", func() {
	It("resolves the dpu-agent repo and registry URLs to https in zero-trusted mode", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(operatorv1.AddToScheme(scheme)).To(Succeed())
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

		operatorConfig := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "dpf-operator-system"},
			Spec: operatorv1.DPFOperatorConfigSpec{
				Networking: &operatorv1.Networking{ControlPlaneMTU: ptr.To(1500)},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorConfig).Build()
		ctrlCtx := &util.ControllerContext{
			Client: c,
			Options: util.DPUOptions{
				DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				// A scheme-less load balancer address must be resolved to https.
				BFBRegistryLoadBalancer: "bfb-registry.example.com",
			},
		}
		dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "default"}}
		flavor := &provisioningv1.DPUFlavor{}

		params, _, err := ResolveParams(context.Background(), ctrlCtx, dpu, flavor)
		Expect(err).NotTo(HaveOccurred())
		Expect(params.BFBRegistryURL).To(Equal("https://bfb-registry.example.com"))
		Expect(params.DPUAgentRepoURL).To(Equal("https://bfb-registry.example.com/deb"))
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
