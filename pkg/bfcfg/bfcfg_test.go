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

package bfcfg

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"

	"github.com/Masterminds/sprig/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func skipFirstEmptyLine(s string) string {
	return strings.TrimPrefix(s, "\n")
}

// CloudConfig represents the structure of the cloud-init configuration YAML.
type CloudConfig struct {
	Debug      DebugConfig  `json:"debug"`       // Debug settings
	Users      []UserConfig `json:"users"`       // List of Linux users
	ChPasswd   ChPasswd     `json:"chpasswd"`    // Password configuration
	WriteFiles []WriteFile  `json:"write_files"` // Files to write during cloud-init
	RunCmd     [][]string   `json:"runcmd"`      // List of command sequences
}

// DebugConfig represents the debug section of the YAML file.
type DebugConfig struct {
	Verbose bool `json:"verbose"` // Verbose output enabled
}

// UserConfig represents a user configuration in the YAML file.
type UserConfig struct {
	Name       string  `json:"name"`             // Username
	LockPasswd bool    `json:"lock_passwd"`      // Whether the password is locked
	Groups     string  `json:"groups"`           // Groups the user belongs to
	Sudo       string  `json:"sudo"`             // Sudo permissions
	Shell      string  `json:"shell"`            // Default shell for the user
	Passwd     *string `json:"passwd,omitempty"` // Optional password hash
}

// ChPasswd represents the chpasswd section for setting passwords.
type ChPasswd struct {
	List   string `json:"list"`   // Username:password pairs
	Expire bool   `json:"expire"` // Whether passwords should expire
}

// WriteFile represents a file to be written during cloud-init.
type WriteFile struct {
	Path        string `json:"path"`        // File path
	Permissions string `json:"permissions"` // File permissions
	Content     string `json:"content"`     // File content
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

var (
	_ = Describe("Generate", func() {
		Describe("custome bf.cfg template", func() {
			var dir string
			var fileName = "custom-bfb.cfg.template"
			var redfishModes = []bool{false, true}

			BeforeEach(func() {
				var err error
				validTemplate := []byte("{{.KubeadmSecretName}}")
				dir, err = os.MkdirTemp("", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dir, fileName), validTemplate, 0644)).To(Succeed())
			})

			AfterEach(func() {
				Expect(os.RemoveAll(dir)).To(Succeed())
			})

			It("error if custom bf.cfg template is invalid", func() {
				for _, redfish := range redfishModes {
					flavor := &provisioningv1.DPUFlavor{}
					_, err := Generate(flavor, cloudinit.Params{DPUHostName: "name", KubeadmSecretName: "test-secret", KubeadmSecretNamespace: "default", RedfishInterface: redfish, OOBNetwork: redfish, ControlPlaneMTU: 1500}, []byte("{{.Invalid"))
					Expect(err).To(HaveOccurred())
				}
			})
			It("generate with correctly formatted template", func() {
				for _, redfish := range redfishModes {
					flavor := &provisioningv1.DPUFlavor{}
					templateData, err := os.ReadFile(filepath.Join(dir, fileName))
					Expect(err).NotTo(HaveOccurred())
					got, err := Generate(flavor, cloudinit.Params{DPUHostName: "name", KubeadmSecretName: "test-secret", KubeadmSecretNamespace: "default", RedfishInterface: redfish, OOBNetwork: redfish, ControlPlaneMTU: 1500}, templateData)
					Expect(err).NotTo(HaveOccurred())
					Expect(got).To(Equal([]byte("test-secret")))
				}
			})
		})

		Describe("default vs custom template equivalence", func() {
			// customTemplate is based on the bf.cfg.template after dpu-agent was
			// merged but before the cloud-init refactoring, with the kubeconfig
			// path updated to use bootstrap-kubeconfig. It inlines cloud-init
			// content directly using the same Go template directives.
			const customTemplate = `{{range .BFGCFGParams}}{{.}}
{{end}}

BMC_PASSWORD="$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 4)-$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 4)_$(tr -dc '0-9' </dev/urandom | head -c 2)$(tr -dc 'a-z' </dev/urandom | head -c 1)$(tr -dc 'A-Z' </dev/urandom | head -c 1)"
BMC_USER="fw_updater"
BMC_REBOOT="yes"
CEC_REBOOT="yes"
USER_ID=8

pre_bmc_components_update() {
    ipmitool user set name $USER_ID $BMC_USER
    ipmitool user set password $USER_ID $BMC_PASSWORD
    ipmitool user enable $USER_ID
    ipmitool channel setaccess 1 $USER_ID ipmi=on
    ipmitool user priv $USER_ID 0x4 1
}

post_bmc_components_update() {
    ipmitool user set name $USER_ID ""
}

bfb_modify_os()
{
cat << \EOF > /mnt/etc/cloud/cloud.cfg.d/dpf.cfg
network:
  config: disabled
EOF

cat << \EOF > /mnt/var/lib/cloud/seed/nocloud-net/user-data
#cloud-config
debug:
  verbose: true
users:
  - name: ubuntu
    lock_passwd: False
    groups: adm, audio, cdrom, dialout, dip, floppy, lxd, netdev, plugdev, sudo, video
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
{{- if .UbuntuPassword}}
    passwd: {{.UbuntuPassword}}
{{else}}
chpasswd:
  list: |
    ubuntu:ubuntu
  expire: True
{{end}}

write_files:
  - path: /etc/netplan/50-dpf-bootstrap.yaml
    permissions: '0600'
    content: |
      network:
        renderer: networkd
        version: 2
        ethernets:
{{- if .OOBNetwork}}
          oob_net0:
            dhcp4: true
            mtu: {{.ControlPlaneMTU}}
{{- else}}
          oob_net0:
            dhcp4: false
            dhcp6: false
            link-local: []
            optional: true
          tmfifo_net0:
            addresses:
            - fe80::2/64
            dhcp4: false
{{- end}}

{{- range .ConfigFiles}}
  - path: {{.Path}}
    {{if .IsAppend}}append: true
    {{end -}}
    permissions: {{.Permissions}}
    content: |
{{indent 6 .Content}}
{{end}}

{{- if .OVSRawScript}}
  - path: /opt/dpf/ovs.sh
    permissions: '0755'
    content: |
      #! /bin/bash
      set -e
{{indent 6 .OVSRawScript}}
{{end}}

  - path: /opt/dpf/dpuflavor.yaml
    permissions: '0400'
    content: |
{{indent 6 .DPUFlavorYAML}}

  - path: /opt/dpf/dpuagent.conf
    permissions: '0600'
    content: |
      --dpu-name={{.DPUName}}
      --dpu-namespace={{.DPUNamespace}}
      --dpu-uid={{.DPUUID}}
      --dpuflavor=/opt/dpf/dpuflavor.yaml
      --control-plane-mtu={{.ControlPlaneMTU}}
      --zero-trust-mode={{.RedfishInterface}}
      --astra-enabled={{.AstraEnabled}}
      --nic-device-count={{.NICDeviceCount}}
      --kubeadm-secret-name={{.KubeadmSecretName}}
      --kubeadm-secret-namespace={{.KubeadmSecretNamespace}}
{{- if .RedfishInterface}}
      --bootstrap-kubeconfig=/var/lib/dpf/dpuagent/bootstrap-kubeconfig
{{- end}}

{{- if .BootstrapKubeconfig}}
  - path: /var/lib/dpf/dpuagent/bootstrap-kubeconfig
    permissions: '0600'
    content: |
{{indent 6 .BootstrapKubeconfig}}
{{- end}}

  - path: /etc/apt/sources.list.d/dpf.list
    permissions: '0644'
    content: |
      deb [trusted=yes] {{.DPUAgentRepoURL}} ./

  - path: /opt/dpf/install-dpu-agent.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      set -euo pipefail

      netplan apply

      # Install dpu-agent with retry.
      # Using "if" so that apt-get failures don't trigger "set -e" exit;
      # bash ignores -e for commands evaluated as part of a condition.
      installed=false
      for i in $(seq 1 60); do
        if apt-get update -o Dir::Etc::sourcelist="sources.list.d/dpf.list" -o Dir::Etc::sourceparts="-" && apt-get install -y --no-install-recommends dpu-agent; then
          installed=true
          break
        fi
        echo "apt-get failed, retrying ($i/60)..."
        sleep 10
      done
      if [ "$installed" != "true" ]; then
        echo "failed to install dpu-agent after 60 attempts" >&2
        exit 1
      fi

      # The systemd service file is included in the dpu-agent package.
      # It reads startup parameters from /opt/dpf/dpuagent.conf (written by cloud-init above).
      systemctl daemon-reload
      systemctl enable --now dpu-agent.service

runcmd:
  - [ hostnamectl, set-hostname, {{.DPUHostName}} ]
  - [ /opt/dpf/install-dpu-agent.sh ]
EOF
}
`

			It("should produce identical bf.cfg via default and custom template paths (Zero Trust)", func() {
				flavor := &provisioningv1.DPUFlavor{}
				Expect(yaml.Unmarshal([]byte(testDPUFlavorYAML), flavor)).To(Succeed())

				params := cloudinit.Params{
					DPUHostName:            "test-dpu",
					KubeadmSecretName:      "test-secret",
					KubeadmSecretNamespace: "default",
					BootstrapKubeconfig:    sampleKubeconfig,
					RedfishInterface:       true,
					OOBNetwork:             true,
					ControlPlaneMTU:        1500,
					DPUName:                "dpu-1",
					DPUNamespace:           "ns-1",
					DPUUID:                 "uid-123",
					DPUAgentRepoURL:        "http://bfb-registry:8080/deb",
				}
				Expect(params.ApplyFlavor(flavor)).To(Succeed())

				defaultOut, err := generateDefault(flavor, params)
				Expect(err).NotTo(HaveOccurred())

				customOut, err := Generate(flavor, params, []byte(customTemplate))
				Expect(err).NotTo(HaveOccurred())

				Expect(string(customOut)).To(Equal(string(defaultOut)))
			})

			It("should produce identical bf.cfg via default and custom template paths (Trusted Host)", func() {
				flavor := &provisioningv1.DPUFlavor{}
				Expect(yaml.Unmarshal([]byte(testDPUFlavorYAML), flavor)).To(Succeed())

				params := cloudinit.Params{
					DPUHostName:            "test-dpu",
					KubeadmSecretName:      "test-secret",
					KubeadmSecretNamespace: "default",
					ControlPlaneMTU:        1500,
					DPUName:                "dpu-1",
					DPUNamespace:           "ns-1",
					DPUUID:                 "uid-123",
					DPUAgentRepoURL:        "http://[fe80::1%25tmfifo_net0]:11029/deb",
				}
				Expect(params.ApplyFlavor(flavor)).To(Succeed())

				defaultOut, err := generateDefault(flavor, params)
				Expect(err).NotTo(HaveOccurred())

				customOut, err := Generate(flavor, params, []byte(customTemplate))
				Expect(err).NotTo(HaveOccurred())

				Expect(string(customOut)).To(Equal(string(defaultOut)))
			})
		})

		Describe("cloud-init YAML with default template", func() {
			var (
				flavor        *provisioningv1.DPUFlavor
				flavorYAMLStr string
			)

			extractYAML := func(data []byte) []byte {
				start := strings.Index(string(data), "#cloud-config")
				Expect(start).NotTo(Equal(-1))
				end := strings.LastIndex(string(data), "EOF")
				Expect(end).To(BeNumerically(">", 1))
				return []byte(string(data)[start:end])
			}

			getWriteFile := func(parsed *CloudConfig, filePath string) *WriteFile {
				for i := range parsed.WriteFiles {
					if parsed.WriteFiles[i].Path == filePath {
						return &parsed.WriteFiles[i]
					}
				}
				Fail("write_files entry not found: " + filePath)
				return nil
			}

			generateAndParse := func(opts cloudinit.Params) ([]byte, *CloudConfig) {
				Expect(opts.ApplyFlavor(flavor)).To(Succeed())
				got, err := generateDefault(flavor, opts)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				return got, parsed
			}

			BeforeEach(func() {
				flavor = &provisioningv1.DPUFlavor{}
				Expect(yaml.Unmarshal([]byte(testDPUFlavorYAML), flavor)).To(Succeed())
				flavorBytes, err := yaml.Marshal(flavor)
				Expect(err).NotTo(HaveOccurred())
				flavorYAMLStr = string(flavorBytes)
			})

			It("should produce valid YAML with all branches enabled (indentation validation)", func() {
				_, parsed := generateAndParse(cloudinit.Params{
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

			It("redfish mode: BMC functions, OOB network, kubeconfig", func() {
				raw, parsed := generateAndParse(cloudinit.Params{
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
				})

				Expect(string(raw)).To(ContainSubstring("pre_bmc_components_update"))
				Expect(string(raw)).To(ContainSubstring("post_bmc_components_update"))

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
				expectedAgentConf := skipFirstEmptyLine(`
--dpu-name=dpu-1
--dpu-namespace=ns-1
--dpu-uid=
--dpuflavor=/opt/dpf/dpuflavor.yaml
--control-plane-mtu=1500
--zero-trust-mode=true
--astra-enabled=false
--nic-device-count=8
--kubeadm-secret-name=test-secret
--kubeadm-secret-namespace=default
--bootstrap-kubeconfig=/var/lib/dpf/dpuagent/bootstrap-kubeconfig
`)
				Expect(agentConf.Content).To(Equal(expectedAgentConf))

				kubeconfigFile := getWriteFile(parsed, "/var/lib/dpf/dpuagent/bootstrap-kubeconfig")
				Expect(kubeconfigFile.Content).To(Equal(sampleKubeconfig))
			})

			It("verify bf.cfg template output updates bmc firmware in both zero trust and trusted host modes", func() {
				zeroTrust := cloudinit.Params{
					DPUHostName:            "test-dpu",
					KubeadmSecretName:      "test-secret",
					KubeadmSecretNamespace: "default",
					BootstrapKubeconfig:    sampleKubeconfig,
					RedfishInterface:       true,
				}
				trustedHost := cloudinit.Params{
					DPUHostName:            "test-dpu",
					KubeadmSecretName:      "test-secret",
					KubeadmSecretNamespace: "default",
					RedfishInterface:       false,
				}
				for _, params := range []cloudinit.Params{zeroTrust, trustedHost} {
					Expect(params.ApplyFlavor(flavor)).To(Succeed())
					got, err := generateDefault(flavor, params)
					Expect(err).NotTo(HaveOccurred())
					Expect(string(got)).To(ContainSubstring("pre_bmc_components_update"))
				}
			})

			It("bf.cfg template output format", func() {
				tmpl, err := template.New("").Funcs(sprig.FuncMap()).Parse(string(DefaultBFCFGTemplateData))
				Expect(err).NotTo(HaveOccurred())

				files := []cloudinit.File{
					{Path: "/mnt/etc/file1", Content: "content1\n"},
					{Path: "/mnt/etc/file2", Content: "line1\nline2\n"},
					{Path: "/mnt/etc/file3", Content: "no-trailing-newline"},
				}

				var buf bytes.Buffer
				Expect(tmpl.Execute(&buf, &defaultBFCFGData{
					BFGCFGParams:     []string{"PARAM1=yes", "PARAM2=no"},
					RedfishInterface: true,
					CloudInitFiles:   files,
				})).To(Succeed())
				Expect(buf.String()).To(Equal(skipFirstEmptyLine(`
PARAM1=yes
PARAM2=no


BMC_PASSWORD="$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 4)-$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 4)_$(tr -dc '0-9' </dev/urandom | head -c 2)$(tr -dc 'a-z' </dev/urandom | head -c 1)$(tr -dc 'A-Z' </dev/urandom | head -c 1)"
BMC_USER="fw_updater"
BMC_REBOOT="yes"
CEC_REBOOT="yes"
USER_ID=8

pre_bmc_components_update() {
    ipmitool user set name $USER_ID $BMC_USER
    ipmitool user set password $USER_ID $BMC_PASSWORD
    ipmitool user enable $USER_ID
    ipmitool channel setaccess 1 $USER_ID ipmi=on
    ipmitool user priv $USER_ID 0x4 1
}

post_bmc_components_update() {
    ipmitool user set name $USER_ID ""
}

bfb_modify_os()
{
cat << \EOF > /mnt/etc/file1
content1
EOF

cat << \EOF > /mnt/etc/file2
line1
line2
EOF

cat << \EOF > /mnt/etc/file3
no-trailing-newline
EOF
}
`)))
			})

			It("gNOI mode: tmfifo network, no kubeconfig", func() {
				raw, parsed := generateAndParse(cloudinit.Params{
					DPUHostName:            "test-dpu",
					KubeadmSecretName:      "test-secret",
					KubeadmSecretNamespace: "default",
					RedfishInterface:       false,
					OOBNetwork:             false,
					ControlPlaneMTU:        1500,
					DPUName:                "dpu-1",
					DPUNamespace:           "ns-1",
					DPUAgentRepoURL:        "http://[fe80::1%25tmfifo_net0]:11029/deb",
				})

				Expect(string(raw)).To(ContainSubstring("pre_bmc_components_update"))

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
				expectedAgentConf := skipFirstEmptyLine(`
--dpu-name=dpu-1
--dpu-namespace=ns-1
--dpu-uid=
--dpuflavor=/opt/dpf/dpuflavor.yaml
--control-plane-mtu=1500
--zero-trust-mode=false
--astra-enabled=false
--nic-device-count=8
--kubeadm-secret-name=test-secret
--kubeadm-secret-namespace=default
`)
				Expect(agentConf.Content).To(Equal(expectedAgentConf))

				for _, f := range parsed.WriteFiles {
					Expect(f.Path).NotTo(Equal("/var/lib/dpf/dpuagent/bootstrap-kubeconfig"))
				}
			})

		})
	})
)

var _ = Describe("getTemplateDataFromConfigMap", func() {
	const testProvisioningNamespace = "dpf-provisioning"

	var (
		ctx                context.Context
		scheme             *runtime.Scheme
		namespace          string
		bfbName            string
		bfbNamespace       string
		clusterName        string
		clusterNamespace   string
		dpuFlavorName      string
		dpuFlavorNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		namespace = "dpf-system"
		bfbName = "my-bfb"
		bfbNamespace = testProvisioningNamespace
		clusterName = "my-cluster"
		clusterNamespace = testProvisioningNamespace
		dpuFlavorName = "my-flavor"
		dpuFlavorNamespace = testProvisioningNamespace
	})

	makeConfigMap := func(name string, templateData string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					TemplateLabel: "true",
				},
				Annotations: map[string]string{
					TemplateBFBNameAnnotation:            bfbName,
					TemplateBFBNamespaceAnnotation:       bfbNamespace,
					TemplateClusterNameAnnotation:        clusterName,
					TemplateClusterNamespaceAnnotation:   clusterNamespace,
					TemplateDPUFlavorNameAnnotation:      dpuFlavorName,
					TemplateDPUFlavorNamespaceAnnotation: dpuFlavorNamespace,
				},
			},
			Data: map[string]string{
				ConfigMapDataKey: templateData,
			},
		}
	}

	It("should return nil when no matching ConfigMaps exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).To(HaveOccurred())
	})

	It("should return template data when exactly one matching ConfigMap exists", func() {
		cm := makeConfigMap("bfcfg-template-1", "hostname={{.DPUHostName}}")
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		data, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).NotTo(BeNil())
		Expect(string(data)).To(Equal("hostname={{.DPUHostName}}"))
	})

	It("should return an error when multiple matching ConfigMaps exist", func() {
		cm1 := makeConfigMap("bfcfg-template-1", "template1")
		cm2 := makeConfigMap("bfcfg-template-2", "template2")
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm1, cm2).Build()

		data, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("found 2 bf.cfg template ConfigMaps"))
		Expect(data).To(BeNil())
	})

	It("should return an error when the ConfigMap is missing the template data key", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bfcfg-template-no-key",
				Namespace: namespace,
				Labels: map[string]string{
					TemplateLabel: "true",
				},
				Annotations: map[string]string{
					TemplateBFBNameAnnotation:            bfbName,
					TemplateBFBNamespaceAnnotation:       bfbNamespace,
					TemplateClusterNameAnnotation:        clusterName,
					TemplateClusterNamespaceAnnotation:   clusterNamespace,
					TemplateDPUFlavorNameAnnotation:      dpuFlavorName,
					TemplateDPUFlavorNamespaceAnnotation: dpuFlavorNamespace,
				},
			},
			Data: map[string]string{
				"wrong-key": "some data",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		data, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing required key"))
		Expect(err.Error()).To(ContainSubstring(ConfigMapDataKey))
		Expect(data).To(BeNil())
	})

	It("should not match ConfigMaps with different BFB annotations", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bfcfg-template-different",
				Namespace: namespace,
				Labels: map[string]string{
					TemplateLabel: "true",
				},
				Annotations: map[string]string{
					TemplateBFBNameAnnotation:            "different-bfb",
					TemplateBFBNamespaceAnnotation:       bfbNamespace,
					TemplateClusterNameAnnotation:        clusterName,
					TemplateClusterNamespaceAnnotation:   clusterNamespace,
					TemplateDPUFlavorNameAnnotation:      dpuFlavorName,
					TemplateDPUFlavorNamespaceAnnotation: dpuFlavorNamespace,
				},
			},
			Data: map[string]string{
				ConfigMapDataKey: "template",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).To(HaveOccurred())
	})

	It("should not match ConfigMaps with different DPUFlavor annotations", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bfcfg-template-different-flavor",
				Namespace: namespace,
				Labels: map[string]string{
					TemplateLabel: "true",
				},
				Annotations: map[string]string{
					TemplateBFBNameAnnotation:            bfbName,
					TemplateBFBNamespaceAnnotation:       bfbNamespace,
					TemplateClusterNameAnnotation:        clusterName,
					TemplateClusterNamespaceAnnotation:   clusterNamespace,
					TemplateDPUFlavorNameAnnotation:      "different-flavor",
					TemplateDPUFlavorNamespaceAnnotation: dpuFlavorNamespace,
				},
			},
			Data: map[string]string{
				ConfigMapDataKey: "template",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).To(HaveOccurred())
	})

	It("should not match ConfigMaps in a different namespace", func() {
		cm := makeConfigMap("bfcfg-template-wrong-ns", "template")
		cm.Namespace = "other-namespace"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace)
		Expect(err).To(HaveOccurred())
	})
})
