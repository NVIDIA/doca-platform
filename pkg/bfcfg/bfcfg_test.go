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
	"context"
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
      _ovs-vsctl set Open_vSwitch . other_config:doca-congestion-threshold=60
      _ovs-vsctl set Open_vSwitch . other_config:flow-limit=500000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload-ct-unidir-udp-enabled=true
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical

      # Activate DOCA for OVNK
      _ovs-vsctl set Open_vSwitch . external-ids:ovn-bridge-datapath-type=netdev
      # setup ovnkube managed bridge, br-dpu (this corresponds to br-ex on ovnk docs)
      _ovs-vsctl --may-exist add-br br-dpu
      _ovs-vsctl br-set-external-id br-dpu bridge-id br-dpu
      _ovs-vsctl br-set-external-id br-dpu bridge-uplink pbrdputobrovn
      _ovs-vsctl set bridge br-dpu datapath_type=netdev
      _ovs-vsctl --may-exist add-port br-dpu pf0hpf
      _ovs-vsctl set Interface pf0hpf mtu_request=9216
      _ovs-vsctl set Interface pf0hpf type=dpdk

      # Create OVS bridge (br-ovn) in between the SC managed bridge and OVNK
      _ovs-vsctl --may-exist add-br br-ovn
      _ovs-vsctl set bridge br-ovn datapath_type=netdev
      _ovs-vsctl --may-exist add-port br-ovn pbrovntobrdpu
      _ovs-vsctl --may-exist add-port br-dpu pbrdputobrovn

      # Patch br-ovn and br-dpu together
      _ovs-vsctl set Interface pbrovntobrdpu type=patch options:peer=pbrdputobrovn
      _ovs-vsctl set Interface pbrdputobrovn type=patch options:peer=pbrovntobrdpu
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
					_, err := Generate(flavor, "name", "kubeadm join", false, DefaultBFCFGTemplateData, instIface, 1500, 2)
					Expect(err).NotTo(HaveOccurred())
				}
			})
			It("error if custom bf.cfg template is invalid", func() {
				for _, instIface := range installInterfaces {
					flavor := &provisioningv1.DPUFlavor{}
					_, err := Generate(flavor, "name", "kubeadm join", false, []byte("{{.Invalid"), instIface, 1500, 2)
					Expect(err).To(HaveOccurred())
				}
			})
			It("generate with correctly formatted template", func() {
				for _, instIface := range installInterfaces {
					flavor := &provisioningv1.DPUFlavor{}
					templateData, err := os.ReadFile(filepath.Join(dir, fileName))
					Expect(err).NotTo(HaveOccurred())
					got, err := Generate(flavor, "name", "kubeadm join", false, templateData, instIface, 1500, 2)
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
				got, err := Generate(flavor, "name", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/opt/dpf/configure-sfs.sh", "PF_TRUSTED_SF=10")).To(BeTrue())
			})

			It("install via RedFish", func() {
				got, err := Generate(flavor, "name", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/etc/netplan/98-oob-tmfifo.yaml", "dhcp4: true")).To(BeTrue())
				Expect(searchFileContent(parsed, "/opt/dpf/join_k8s_cluster.sh", "COMM_CH_BR_NAME=oob_net0")).To(BeTrue())
				Expect(searchFileContent(parsed, "/etc/netplan/99-dpf-comm-ch.yaml", "")).NotTo(BeTrue())
			})
			It("install via gNOI", func() {
				got, err := Generate(flavor, "name", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(extractYAML(got), parsed)).To(Succeed())
				Expect(searchFileContent(parsed, "/etc/netplan/98-oob-tmfifo.yaml", "dhcp4: true")).NotTo(BeTrue())
				Expect(searchFileContent(parsed, "/opt/dpf/join_k8s_cluster.sh", "COMM_CH_BR_NAME=br-comm-ch")).To(BeTrue())
				Expect(searchFileContent(parsed, "/etc/netplan/99-dpf-comm-ch.yaml", "")).To(BeTrue())
			})
			It("install via redfish", func() {
				got, err := Generate(flavor, "name", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaRedFish), 1500, 2)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(got)).Should(ContainSubstring("pre_bmc_components_update"))
			})

			It("should generate bf.cfg with kubelet security parameters in systemd service", func() {
				got, err := Generate(flavor, "test-dpu", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
				Expect(err).NotTo(HaveOccurred())

				yamlData := extractYAML(got)
				parsed := &CloudConfig{}
				Expect(yaml.Unmarshal(yamlData, parsed)).To(Succeed())

				By("Verifying kubelet service file contains KUBELET_EXTRA_ARGS")
				found := searchFileContent(parsed, "/etc/systemd/system/kubelet.service.d/10-bf.conf", "KUBELET_EXTRA_ARGS")
				Expect(found).To(BeTrue(), "Expected KUBELET_EXTRA_ARGS in kubelet service file")

				By("Verifying all four kubelet security parameters are present")
				found = searchFileContent(parsed, "/etc/systemd/system/kubelet.service.d/10-bf.conf", "--protect-kernel-defaults=true")
				Expect(found).To(BeTrue(), "Expected --protect-kernel-defaults=true parameter")

				found = searchFileContent(parsed, "/etc/systemd/system/kubelet.service.d/10-bf.conf", "--seccomp-default=true")
				Expect(found).To(BeTrue(), "Expected --seccomp-default=true parameter")

				found = searchFileContent(parsed, "/etc/systemd/system/kubelet.service.d/10-bf.conf", "--streaming-connection-idle-timeout=5m0s")
				Expect(found).To(BeTrue(), "Expected --streaming-connection-idle-timeout=5m0s parameter")

				found = searchFileContent(parsed, "/etc/systemd/system/kubelet.service.d/10-bf.conf", "--event-qps=50")
				Expect(found).To(BeTrue(), "Expected --event-qps=50 parameter")
			})
		})

		Describe("multi-nvconfig support", func() {
			It("should generate bf.cfg with multiple device-specific nvconfig entries", func() {
				flavor := &provisioningv1.DPUFlavor{}
				Expect(yaml.Unmarshal([]byte(DPUFlavorHBNOVN), flavor)).To(Succeed())

				dev0 := "p0"
				dev1 := "p1"
				flavor.Spec.NVConfig = []provisioningv1.NVConfig{
					{
						Device:     &dev0,
						Parameters: []string{"LINK_TYPE_P1=ETH", "NUM_OF_VFS=8"},
					},
					{
						Device:     &dev1,
						Parameters: []string{"LINK_TYPE_P1=IB", "NUM_OF_VFS=16"},
					},
				}

				got, err := Generate(flavor, "test-dpu", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
				Expect(err).NotTo(HaveOccurred())

				output := string(got)
				Expect(output).To(ContainSubstring("reset NVConfig on dev ${dev} to defaults"))
				// Device identifiers (p0, p1) will be translated by bf.cfg script logic
				Expect(output).To(ContainSubstring("mlxconfig -d p0 -y set LINK_TYPE_P1=ETH NUM_OF_VFS=8"))
				Expect(output).To(ContainSubstring("mlxconfig -d p1 -y set LINK_TYPE_P1=IB NUM_OF_VFS=16"))
			})

			DescribeTable("should generate bf.cfg applying to all devices (wildcard behavior)",
				func(devicePtr *string) {
					flavor := &provisioningv1.DPUFlavor{}
					Expect(yaml.Unmarshal([]byte(DPUFlavorHBNOVN), flavor)).To(Succeed())

					flavor.Spec.NVConfig = []provisioningv1.NVConfig{
						{
							Device:     devicePtr,
							Parameters: []string{"SRIOV_EN=1", "NUM_OF_VFS=32"},
						},
					}

					got, err := Generate(flavor, "test-dpu", "kubeadm join", false, DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
					Expect(err).NotTo(HaveOccurred())

					output := string(got)
					// Both explicit "*" and nil should produce loop over all devices
					Expect(output).To(ContainSubstring("for dev in /dev/mst/*; do"))
					Expect(output).To(ContainSubstring("mlxconfig -d ${dev} -y set SRIOV_EN=1 NUM_OF_VFS=32"))
				},
				Entry("explicit wildcard '*'", func() *string { s := "*"; return &s }()),
				Entry("unspecified device (nil, normalized to '*')", (*string)(nil)),
			)
		})
	})
)

var _ = Describe("getTemplateDataFromConfigMap", func() {
	var (
		ctx              context.Context
		scheme           *runtime.Scheme
		namespace        string
		bfbName          string
		bfbNamespace     string
		clusterName      string
		clusterNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		namespace = "dpf-system"
		bfbName = "my-bfb"
		bfbNamespace = "dpf-provisioning"
		clusterName = "my-cluster"
		clusterNamespace = "dpf-provisioning"
	})

	makeConfigMap := func(name string, templateData string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					cutil.BFCFGTemplateLabel: "true",
				},
				Annotations: map[string]string{
					cutil.BFCFGTemplateBFBNameAnnotation:          bfbName,
					cutil.BFCFGTemplateBFBNamespaceAnnotation:     bfbNamespace,
					cutil.BFCFGTemplateClusterNameAnnotation:      clusterName,
					cutil.BFCFGTemplateClusterNamespaceAnnotation: clusterNamespace,
				},
			},
			Data: map[string]string{
				ConfigMapDataKey: templateData,
			},
		}
	}

	It("should return nil when no matching ConfigMaps exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace)
		Expect(err).To(HaveOccurred())
	})

	It("should return template data when exactly one matching ConfigMap exists", func() {
		cm := makeConfigMap("bfcfg-template-1", "hostname={{.DPUHostName}}")
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		data, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).NotTo(BeNil())
		Expect(string(data)).To(Equal("hostname={{.DPUHostName}}"))
	})

	It("should return an error when multiple matching ConfigMaps exist", func() {
		cm1 := makeConfigMap("bfcfg-template-1", "template1")
		cm2 := makeConfigMap("bfcfg-template-2", "template2")
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm1, cm2).Build()

		data, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace)
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
					cutil.BFCFGTemplateLabel: "true",
				},
				Annotations: map[string]string{
					cutil.BFCFGTemplateBFBNameAnnotation:          bfbName,
					cutil.BFCFGTemplateBFBNamespaceAnnotation:     bfbNamespace,
					cutil.BFCFGTemplateClusterNameAnnotation:      clusterName,
					cutil.BFCFGTemplateClusterNamespaceAnnotation: clusterNamespace,
				},
			},
			Data: map[string]string{
				"wrong-key": "some data",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		data, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing required key"))
		Expect(err.Error()).To(ContainSubstring(ConfigMapDataKey))
		Expect(data).To(BeNil())
	})

	It("should not match ConfigMaps with different annotations", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bfcfg-template-different",
				Namespace: namespace,
				Labels: map[string]string{
					cutil.BFCFGTemplateLabel: "true",
				},
				Annotations: map[string]string{
					cutil.BFCFGTemplateBFBNameAnnotation:          "different-bfb",
					cutil.BFCFGTemplateBFBNamespaceAnnotation:     bfbNamespace,
					cutil.BFCFGTemplateClusterNameAnnotation:      clusterName,
					cutil.BFCFGTemplateClusterNamespaceAnnotation: clusterNamespace,
				},
			},
			Data: map[string]string{
				ConfigMapDataKey: "template",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace)
		Expect(err).To(HaveOccurred())
	})

	It("should not match ConfigMaps in a different namespace", func() {
		cm := makeConfigMap("bfcfg-template-wrong-ns", "template")
		cm.Namespace = "other-namespace"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

		_, err := getTemplateDataFromConfigMap(ctx, c, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace)
		Expect(err).To(HaveOccurred())
	})
})
