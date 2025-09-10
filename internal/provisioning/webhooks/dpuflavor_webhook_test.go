/*
Copyright 2024 NVIDIA

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

package webhooks

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = Describe("DPUFlavor", func() {

	var (
		DefaultGrub   = []string{`hugepagesz=2048kB`, `cgroup_no_v1=net_prio`}
		DefaultSysctl = []string{`net.mc_forwarding=2048kB`}
	)

	var getObjKey = func(obj *provisioningv1.DPUFlavor) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.DPUFlavor {
		return &provisioningv1.DPUFlavor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: provisioningv1.DPUFlavorSpec{},
		}
	}

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("create and get object", func() {
			obj := createObj("obj-1")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("delete object", func() {
			obj := createObj("obj-2")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("update object", func() {
			obj := createObj("obj-3")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("update object with not default data", func() {
			obj := createObj("obj-4")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("check default settings", func() {
			obj := createObj("obj-5")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
			Expect(objFetched.Spec.Grub.KernelParameters).To(BeEmpty())
			Expect(objFetched.Spec.Sysctl.Parameters).To(BeEmpty())
			Expect(objFetched.Spec.NVConfig).To(BeEmpty())
			Expect(objFetched.Spec.OVS.RawConfigScript).To(BeEmpty())
			Expect(objFetched.Spec.BFCfgParameters).To(BeEmpty())
			Expect(objFetched.Spec.ConfigFiles).To(BeEmpty())
			Expect(objFetched.Spec.ContainerdConfig.RegistryEndpoint).To(BeEmpty())
			Expect(objFetched.Spec.HostNetworkInterfaceConfigs).To(BeEmpty())
		})

		It("spec.grub is immutable", func() {
			refValue := DefaultGrub
			newValue := []string{`spec.grub`}

			obj := createObj("obj-6")
			obj.Spec.Grub.KernelParameters = refValue
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.Grub.KernelParameters = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Grub.KernelParameters[0]).To(Equal(refValue[0]))
		})

		It("spec.sysctl is immutable", func() {
			refValue := DefaultSysctl
			newValue := []string{`spec.sysctl`}

			obj := createObj("obj-7")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.Sysctl.Parameters = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Sysctl.Parameters[0]).To(Equal(refValue[0]))
		})

		It("spec.nvconfig is immutable", func() {
			refValue := []string{`PF_BAR2_ENABLE=0`, `PER_PF_NUM_SF=1`}
			newValue := []string{`spec.nvconfig`}

			obj := createObj("obj-8")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			obj.Spec.NVConfig = []provisioningv1.NVConfig{
				{Parameters: refValue},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.NVConfig[0].Parameters = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.NVConfig[0].Parameters[0]).To(Equal(refValue[0]))
		})

		It("spec.ovs is immutable", func() {
			refValue := `ovs-vsct add-br br-hbn`
			newValue := `spec.ovs`

			obj := createObj("obj-9")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			obj.Spec.OVS.RawConfigScript = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.OVS.RawConfigScript = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.OVS.RawConfigScript).To(Equal(refValue))
		})

		It("spec.bfcfgParameters is immutable", func() {
			refValue := []string{`PF_BAR2_ENABLE=0`, `PER_PF_NUM_SF=1`}
			newValue := []string{`spec.bfcfgParameters`}

			obj := createObj("obj-10")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			obj.Spec.BFCfgParameters = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.BFCfgParameters = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.BFCfgParameters[0]).To(Equal(refValue[0]))
		})

		It("spec.configFiles is immutable", func() {
			refValue := `/etc/dummy.cfg`
			newValue := `spec.configFiles`

			obj := createObj("obj-11")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			obj.Spec.ConfigFiles = []provisioningv1.ConfigFile{
				{Path: refValue},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.ConfigFiles[0].Path = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.ConfigFiles[0].Path).To(Equal(refValue))
		})

		It("spec.ovs is immutable", func() {
			refValue := `127.0.0.1:8001`
			newValue := `spec.ovs`

			obj := createObj("obj-12")
			obj.Spec.Grub.KernelParameters = DefaultGrub
			obj.Spec.Sysctl.Parameters = DefaultSysctl
			obj.Spec.ContainerdConfig.RegistryEndpoint = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.ContainerdConfig.RegistryEndpoint = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.ContainerdConfig.RegistryEndpoint).To(Equal(refValue))
		})

		It("create from yaml", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: obj-13
  namespace: default
spec:
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
  sysctl:
    parameters:
    - net.ipv4.ip_forward=1
    - net.ipv4.ip_forward_update_priority=0
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=40
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - SRIOV_EN=1
        - NUM_OF_VFS=30
        - LAG_RESOURCE_ALLOCATION=1
  ovs:
    rawConfigScript: |
      ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones="50000"
      ovs-vsctl set Open_vSwitch . other_config:hw-offload="true"
      ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
  bfcfgParameters:
    - ubuntu_PASSWORD=$1$rvRv4qpw$mS6kYODr8oMxORt.TkiTB0
    - WITH_NIC_FW_UPDATE=yes
    - ENABLE_SFC_HBN=no
  configFiles:
  - path: /etc/bla/blabla.cfg
    operation: append
    raw: |
        CREATE_OVS_BRIDGES="no"
        CREATE_OVS_BRIDGES="no"
    permissions: "0755"
`)
			obj := &provisioningv1.DPUFlavor{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("create from yaml minimal", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: obj-14
  namespace: default
`)
			obj := &provisioningv1.DPUFlavor{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("validates host network interface config fields", func() {
			obj := createObj("network-config-test")
			mtu1500 := int32(1500)
			dhcpTrue := true
			port0 := int32(0)
			port1 := int32(1)

			obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
				{
					PortNumber: port0,
					MTU:        &mtu1500,
					DHCP:       &dhcpTrue,
				},
				{
					PortNumber: port1,
					MTU:        &mtu1500,
					DHCP:       &dhcpTrue,
				},
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.HostNetworkInterfaceConfigs).To(HaveLen(2))

			// Find P0 config (port 0)
			var p0Config *provisioningv1.NetworkInterfaceConfig
			for i := range objFetched.Spec.HostNetworkInterfaceConfigs {
				if objFetched.Spec.HostNetworkInterfaceConfigs[i].PortNumber == 0 {
					p0Config = &objFetched.Spec.HostNetworkInterfaceConfigs[i]
					break
				}
			}
			Expect(p0Config).ToNot(BeNil())
			Expect(*p0Config.MTU).To(Equal(int32(1500)))
			Expect(*p0Config.DHCP).To(BeTrue())

			// Find P1 config (port 1)
			var p1Config *provisioningv1.NetworkInterfaceConfig
			for i := range objFetched.Spec.HostNetworkInterfaceConfigs {
				if objFetched.Spec.HostNetworkInterfaceConfigs[i].PortNumber == 1 {
					p1Config = &objFetched.Spec.HostNetworkInterfaceConfigs[i]
					break
				}
			}
			Expect(p1Config).ToNot(BeNil())
			Expect(*p1Config.MTU).To(Equal(int32(1500)))
			Expect(*p1Config.DHCP).To(BeTrue())
		})

		It("rejects duplicate port numbers", func() {
			obj := createObj("duplicate-port-test")
			mtu1500 := int32(1500)
			dhcpTrue := true
			port0 := int32(0)

			obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
				{
					PortNumber: port0,
					MTU:        &mtu1500,
					DHCP:       &dhcpTrue,
				},
				{
					PortNumber: port0, // Duplicate port number
					MTU:        &mtu1500,
					DHCP:       &dhcpTrue,
				},
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate port number 0"))
		})

		It("rejects configs with no configuration options", func() {
			obj := createObj("empty-config-test")
			port0 := int32(0)

			obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
				{
					PortNumber: port0,
					// No MTU, DHCP, or NVConfig specified
				},
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has no configuration options specified"))
		})

		It("supports multi-port configurations", func() {
			obj := createObj("multi-port-test")
			mtu1500 := int32(1500)
			port0 := int32(0)
			port1 := int32(1)

			obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
				{
					PortNumber: port0,
					MTU:        &mtu1500,
				},
				{
					PortNumber: port1,
					MTU:        &mtu1500,
				},
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("handles mixed NVConfig scenarios", func() {
			obj := createObj("mixed-nvconfig-test")
			mtu1500 := int32(1500)
			port0 := int32(0)
			port1 := int32(1)

			// Global NVConfig
			obj.Spec.NVConfig = []provisioningv1.NVConfig{
				{
					Parameters: []string{"GLOBAL_PARAM=global_value", "ANOTHER_GLOBAL=value"},
				},
			}

			// Mixed per-interface configs: some with NVConfig, some without
			obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
				{
					PortNumber: port0,
					MTU:        &mtu1500,
					// No NVConfig - should be fine
				},
				{
					PortNumber: port1,
					MTU:        &mtu1500,
					NVConfig: &provisioningv1.NVConfig{
						Parameters: []string{"PORT1_SPECIFIC=value"},
					},
				},
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
		DescribeTable("MTU validation works as expected",
			func(mtu int32, expectError bool) {
				obj := &provisioningv1.DPUFlavor{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "mtu-validation-",
						Namespace:    "default",
					},
					Spec: provisioningv1.DPUFlavorSpec{},
				}
				port0 := int32(0)
				obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
					{
						PortNumber: port0,
						MTU:        &mtu,
					},
				}
				err := k8sClient.Create(ctx, obj)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},
			Entry("valid MTU 1500", int32(1500), false),
			Entry("valid MTU 9000", int32(9000), false),
			Entry("valid MTU 1000 (minimum)", int32(1000), false),
			Entry("valid MTU 9216 (maximum)", int32(9216), false),
			Entry("invalid MTU too low", int32(999), true),
			Entry("invalid MTU too high", int32(9217), true),
		)

		DescribeTable("port number validation works as expected", func(portNumber int32, expectError bool) {
			obj := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "port-number-validation-",
					Namespace:    "default",
				},
				Spec: provisioningv1.DPUFlavorSpec{},
			}
			mtu1500 := int32(1500)
			obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
				{
					PortNumber: portNumber,
					MTU:        &mtu1500,
				},
			}
			err := k8sClient.Create(ctx, obj)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
			Entry("valid port number 0", int32(0), false),
			Entry("valid port number 1", int32(1), false),
			Entry("invalid port number too low", int32(-1), true),
			Entry("invalid port number too high", int32(2), true),
		)

		It("create from yaml with network interface config", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: network-config-yaml
  namespace: default
spec:
  hostNetworkInterfaceConfigs:
  - portNumber: 0
    mtu: 9000
    dhcp: true
  - portNumber: 1
    mtu: 1500
    dhcp: false
`)
			obj := &provisioningv1.DPUFlavor{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "network-config-yaml", Namespace: "default"}, objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.HostNetworkInterfaceConfigs).To(HaveLen(2))

			// Find P0 config (port 0)
			var p0Config *provisioningv1.NetworkInterfaceConfig
			for i := range objFetched.Spec.HostNetworkInterfaceConfigs {
				if objFetched.Spec.HostNetworkInterfaceConfigs[i].PortNumber == 0 {
					p0Config = &objFetched.Spec.HostNetworkInterfaceConfigs[i]
					break
				}
			}
			Expect(p0Config).ToNot(BeNil())
			Expect(*p0Config.MTU).To(Equal(int32(9000)))
			Expect(*p0Config.DHCP).To(BeTrue())

			// Find P1 config (port 1)
			var p1Config *provisioningv1.NetworkInterfaceConfig
			for i := range objFetched.Spec.HostNetworkInterfaceConfigs {
				if objFetched.Spec.HostNetworkInterfaceConfigs[i].PortNumber == 1 {
					p1Config = &objFetched.Spec.HostNetworkInterfaceConfigs[i]
					break
				}
			}
			Expect(p1Config).ToNot(BeNil())
			Expect(*p1Config.MTU).To(Equal(int32(1500)))
			Expect(*p1Config.DHCP).To(BeFalse())
		})

		DescribeTable("resource validation works as expected", func(dpuResources corev1.ResourceList, systemReservedResources corev1.ResourceList, expectError bool) {
			obj := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "resources",
					Namespace:    "default",
				},
				Spec: provisioningv1.DPUFlavorSpec{
					DPUResources:            dpuResources,
					SystemReservedResources: systemReservedResources,
				},
			}
			err := k8sClient.Create(ctx, obj)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("nothing specified",
				nil,
				nil,
				false),
			Entry("dpuResources specified",
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				nil,
				false),
			Entry("dpuResources and systemReservedResources specified",
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				false),
			Entry("systemReservedResources specified",
				nil,
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				true),
			Entry("dpuResources and systemReservedResources specified - missing resource in dpuResource",
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				corev1.ResourceList{"cpu": resource.MustParse("5"), "memory": resource.MustParse("5Gi")},
				true),
			Entry("dpuResources and systemReservedResources specified - missing resource in systemReservedResources",
				corev1.ResourceList{"cpu": resource.MustParse("5"), "memory": resource.MustParse("5Gi")},
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				false),
			Entry("dpuResources and systemReservedResources specified - resource in dpuResource exceeds resource in systemReservedResources",
				corev1.ResourceList{"cpu": resource.MustParse("7")},
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				false),
			Entry("dpuResources and systemReservedResources specified - resource in systemReservedResources exceeds resource in dpuResource",
				corev1.ResourceList{"cpu": resource.MustParse("5")},
				corev1.ResourceList{"cpu": resource.MustParse("7")},
				true),
		)
	})
})
