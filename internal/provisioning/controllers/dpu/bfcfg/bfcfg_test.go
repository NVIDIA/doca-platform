/*
Copyright 2025 NVIDIA

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
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

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
	// DPUFlavorHBNOVN is copied from internal/operator/inventory/manifests/provisioning-controller.yaml
	DPUFlavorHBNOVN = `
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  labels:
    app.kubernetes.io/part-of: dpf-provisioning-controller-manager
    dpu.nvidia.com/component: dpf-provisioning-controller-manager
  name: dpf-provisioning-hbn-ovn
  namespace: dpf-provisioning
spec:
  bfcfgParameters:
  - UPDATE_ATF_UEFI=yes
  - UPDATE_DPU_OS=yes
  - WITH_NIC_FW_UPDATE=yes
  configFiles:
  - operation: override
    path: /etc/mellanox/mlnx-bf.conf
    permissions: "0644"
    raw: |
      ALLOW_SHARED_RQ="no"
      IPSEC_FULL_OFFLOAD="no"
      ENABLE_ESWITCH_MULTIPORT="yes"
  - operation: override
    path: /etc/mellanox/mlnx-ovs.conf
    permissions: "0644"
    raw: |
      CREATE_OVS_BRIDGES="no"
      OVS_DOCA="yes"
  - operation: override
    path: /etc/mellanox/mlnx-sf.conf
    permissions: "0644"
    raw: ""
  grub:
    kernelParameters:
    - console=hvc0
    - console=ttyAMA0
    - earlycon=pl011,0x13010000
    - fixrttc
    - net.ifnames=0
    - biosdevname=0
    - iommu.passthrough=1
    - cgroup_no_v1=net_prio,net_cls
    - hugepagesz=2048kB
    - hugepages=3072
  nvconfig:
  - device: '*'
    parameters:
    - PF_BAR2_ENABLE=0
    - PER_PF_NUM_SF=1
    - PF_TOTAL_SF=20
    - PF_SF_BAR_SIZE=10
    - NUM_PF_MSIX_VALID=0
    - PF_NUM_PF_MSIX_VALID=1
    - PF_NUM_PF_MSIX=228
    - INTERNAL_CPU_MODEL=1
    - INTERNAL_CPU_OFFLOAD_ENGINE=0
    - SRIOV_EN=1
    - NUM_OF_VFS=46
    - LAG_RESOURCE_ALLOCATION=1
    - LINK_TYPE_P1=ETH
    - LINK_TYPE_P2=ETH
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl set Open_vSwitch . other_config:ctl-pipe-size=1024
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical

      _ovs-vsctl set Open_vSwitch . external-ids:ovn-bridge-datapath-type=netdev
      _ovs-vsctl --may-exist add-br br-ovn
      _ovs-vsctl set bridge br-ovn datapath_type=netdev
      _ovs-vsctl br-set-external-id br-ovn bridge-id br-ovn
      _ovs-vsctl br-set-external-id br-ovn bridge-uplink puplinkbrovntobrsfc
      _ovs-vsctl --may-exist add-port br-ovn pf0hpf
      _ovs-vsctl set Interface pf0hpf type=dpdk
`
)

var (
	_ = Describe("Generate", func() {
		Describe("custome bf.cfg template", func() {
			var dir string
			var fileName = "custom-bfb.cfg.template"
			var installInterfaces = []string{string(provisioningv1.InstallViaGNOI), string(provisioningv1.InstallViaRedFish)}

			BeforeEach(func() {
				var err error
				validTemplate := []byte("{{.KubeadmJoinCMD}}")
				dir, err = os.MkdirTemp("", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(dir, fileName), validTemplate, 0644)).To(Succeed())
			})

			AfterEach(func() {
				Expect(os.RemoveAll(dir)).To(Succeed())
			})

			It("test with default bf.cfg", func() {
				for _, instIface := range installInterfaces {
					flavor := &provisioningv1.DPUFlavor{}
					_, err := Generate(flavor, "name", "kubeadm join", false, "", instIface, 1500, 2)
					Expect(err).NotTo(HaveOccurred())
				}
			})
			It("error if custom bf.cfg does not exist", func() {
				for _, instIface := range installInterfaces {
					flavor := &provisioningv1.DPUFlavor{}
					_, err := Generate(flavor, "name", "kubeadm join", false, "/files/does-not-exist", instIface, 1500, 2)
					Expect(err).To(HaveOccurred())
				}
			})
			It("generate with correctly formatted template", func() {
				for _, instIface := range installInterfaces {
					flavor := &provisioningv1.DPUFlavor{}
					got, err := Generate(flavor, "name", "kubeadm join", false, filepath.Join(dir, fileName), instIface, 1500, 2)
					Expect(err).NotTo(HaveOccurred())
					Expect(got).To(Equal([]byte("kubeadm join")))
				}
			})
		})

		Describe("cloud-init YAML", func() {
			var flavor *provisioningv1.DPUFlavor

			extractYAML := func(data []byte) []byte {
				start := strings.Index(string(data), "#cloud-config")
				Expect(start).NotTo(Equal(-1))
				end := strings.LastIndex(string(data), "EOF")
				Expect(end).To(BeNumerically(">", 1))
				b := string(data)[start:end]
				return []byte(b)
			}

			searchFileContent := func(parsed *CloudConfig, filePath, content string) bool {
				for _, file := range parsed.WriteFiles {
					if file.Path != filePath {
						continue
					}
					if i := strings.Index(file.Content, content); i != -1 {
						return true
					}
				}
				return false
			}

			BeforeEach(func() {
				flavor = &provisioningv1.DPUFlavor{}
				Expect(yaml.Unmarshal([]byte(DPUFlavorHBNOVN), flavor)).To(Succeed())
			})

			It("create trusted_sfs in bf.cfg", func() {
				flavor.Annotations = make(map[string]string)
				flavor.Annotations[cutil.TrustedSFCount] = "10"
				got, err := Generate(flavor, "name", "kubeadm join", false, "", string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/opt/dpf/configure-sfs.sh", "PF_TRUSTED_SF=10")).To(BeTrue())
			})

			It("set DPU mode to zero-trust", func() {
				flavor.Spec.DpuMode = provisioningv1.DpuModeType("zero-trust")
				got, err := Generate(flavor, "name", "kubeadm join", false, "", string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/opt/dpf/set-dpu-mode.sh", "zero-trust")).To(BeTrue())
			})

			It("install via RedFish", func() {
				got, err := Generate(flavor, "name", "kubeadm join", false, "", string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/etc/netplan/98-oob-tmfifo.yaml", "dhcp4: true")).To(BeTrue())
				Expect(searchFileContent(parsed, "/opt/dpf/join_k8s_cluster.sh", "COMM_CH_BR_NAME=oob_net0")).To(BeTrue())
				Expect(searchFileContent(parsed, "/etc/netplan/99-dpf-comm-ch.yaml", "")).NotTo(BeTrue())
			})
			It("install via gNOI", func() {
				got, err := Generate(flavor, "name", "kubeadm join", false, "", string(provisioningv1.InstallViaGNOI), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/etc/netplan/98-oob-tmfifo.yaml", "dhcp4: true")).NotTo(BeTrue())
				Expect(searchFileContent(parsed, "/opt/dpf/join_k8s_cluster.sh", "COMM_CH_BR_NAME=br-comm-ch")).To(BeTrue())
				Expect(searchFileContent(parsed, "/etc/netplan/99-dpf-comm-ch.yaml", "")).To(BeTrue())
			})
			It("install via redfish", func() {
				got, err := Generate(flavor, "name", "kubeadm join", false, "", string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(got)).Should(ContainSubstring("pre_bmc_components_update"))
			})
		})
	})
)
