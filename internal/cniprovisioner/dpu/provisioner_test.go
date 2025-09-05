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

package dpucniprovisioner_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"time"

	dpucniprovisioner "github.com/nvidia/doca-platform/internal/cniprovisioner/dpu"
	ovsclientMock "github.com/nvidia/doca-platform/internal/utils/ovsclient/mock"
	networkhelperMock "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	testclient "k8s.io/client-go/kubernetes/fake"
	clock "k8s.io/utils/clock/testing"
	kexec "k8s.io/utils/exec"
	kexecTesting "k8s.io/utils/exec/testing"
	"k8s.io/utils/ptr"
)

var _ = Describe("DPU CNI Provisioner in Internal mode", func() {
	Context("When it runs once for the first time", func() {
		It("should configure the system fully when different subnets per DPU", func() {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			vtepIPNet, err := netlink.ParseIPNet("192.168.1.1/24")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.10")
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			hostCIDR, err := netlink.ParseIPNet("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			pfIPNet, err := netlink.ParseIPNet("192.168.1.2/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNet, err := netlink.ParseIPNet("10.0.100.100/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNetWith32Mask, err := netlink.ParseIPNet("10.0.100.100/32")
			Expect(err).ToNot(HaveOccurred())
			flannelIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			_, defaultRouteNetwork, err := net.ParseCIDR("0.0.0.0/0")
			Expect(err).ToNot(HaveOccurred())
			defaultGateway := net.ParseIP("10.0.100.254")
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset()
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.InternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, vtepIPNet, gateway, vtepCIDR, hostCIDR, pfIPNet, fakeNode.Name, nil, 8940)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())
			ovnInputPath := filepath.Join(ovnInputDirPath, "ovn_k8s.conf")

			mac, _ := net.ParseMAC("00:00:00:00:00:01")
			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				Expect(cmd).To(Equal("dnsmasq"))
				Expect(args).To(Equal([]string{
					"--keep-in-foreground",
					"--port=0",
					"--log-facility=-",
					"--interface=br-ovn",
					"--dhcp-option=option:router",
					"--dhcp-option=option:mtu,9000",
					"--dhcp-range=192.168.1.0,static",
					"--dhcp-host=00:00:00:00:00:01,192.168.1.2",
					"--dhcp-option=option:classless-static-route,192.168.1.0/23,192.168.1.10",
				}))

				return kexec.New().Command("echo")
			}))

			networkhelper.EXPECT().LinkIPAddressExists("br-ovn", vtepIPNet)
			networkhelper.EXPECT().SetLinkIPAddress("br-ovn", vtepIPNet)
			networkhelper.EXPECT().SetLinkUp("br-ovn")
			networkhelper.EXPECT().RouteExists(vtepCIDR, gateway, "br-ovn", nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, gateway, "br-ovn", nil, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil)
			networkhelper.EXPECT().AddRoute(hostCIDR, gateway, "br-ovn", ptr.To[int](10000), nil)
			networkhelper.EXPECT().GetPFRepMACAddress("pf0hpf").Return(mac, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			_, flannelIPNet, err := net.ParseCIDR(flannelIP.String())
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(false, nil)
			networkhelper.EXPECT().AddRule(flannelIPNet, 60, 31000).Return(nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(false, nil)
			networkhelper.EXPECT().AddRule(oobIPNetWith32Mask, 60, 32000).Return(nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(false, nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, defaultGateway, "br-comm-ch", nil, ptr.To(60)).Return(nil)

			ovsClient.EXPECT().SetOVNEncapIP(net.ParseIP("192.168.1.1"))
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")

			fakeNode.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Node"))
			fakeNode.SetManagedFields(nil)
			data, err := json.Marshal(fakeNode)
			Expect(err).ToNot(HaveOccurred())
			_, err = kubernetesClient.CoreV1().Nodes().Patch(context.Background(), fakeNode.Name, types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: "somemanager",
				Force:        ptr.To[bool](true),
			})
			Expect(err).NotTo(HaveOccurred())

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			ovnInputGatewayOpts, err := os.ReadFile(ovnInputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ovnInputGatewayOpts)).To(Equal("[Gateway]\nnext-hop=192.168.1.10\nrouter-subnet=192.168.1.0/24\n"))

			Expect(fakeExec.CommandCalls).To(Equal(1))
		})
		It("should configure the system fully when same subnet across DPUs", func() {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			vtepIPNet, err := netlink.ParseIPNet("192.168.1.1/24")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.10")
			_, vtepCIDR, err := net.ParseCIDR("192.168.1.0/24")
			Expect(err).ToNot(HaveOccurred())
			_, hostCIDR, err := net.ParseCIDR("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			pfIPNet, err := netlink.ParseIPNet("192.168.1.2/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNet, err := netlink.ParseIPNet("10.0.100.100/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNetWith32Mask, err := netlink.ParseIPNet("10.0.100.100/32")
			Expect(err).ToNot(HaveOccurred())
			flannelIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			_, defaultRouteNetwork, err := net.ParseCIDR("0.0.0.0/0")
			Expect(err).ToNot(HaveOccurred())
			defaultGateway := net.ParseIP("10.0.100.254")
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset()
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.InternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, vtepIPNet, gateway, vtepCIDR, hostCIDR, pfIPNet, fakeNode.Name, nil, 1440)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())
			ovnInputPath := filepath.Join(ovnInputDirPath, "ovn_k8s.conf")

			mac, _ := net.ParseMAC("00:00:00:00:00:01")
			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				Expect(cmd).To(Equal("dnsmasq"))
				Expect(args).To(Equal([]string{
					"--keep-in-foreground",
					"--port=0",
					"--log-facility=-",
					"--interface=br-ovn",
					"--dhcp-option=option:router",
					"--dhcp-option=option:mtu,1500",
					"--dhcp-range=192.168.1.0,static",
					"--dhcp-host=00:00:00:00:00:01,192.168.1.2",
				}))

				return kexec.New().Command("echo")
			}))

			Expect(vtepIPNet.String()).To(Equal("192.168.1.1/24"))
			_, vtepNetwork, _ := net.ParseCIDR(vtepIPNet.String())
			Expect(vtepNetwork.String()).To(Equal("192.168.1.0/24"))
			Expect(vtepCIDR).To(Equal(vtepNetwork))
			networkhelper.EXPECT().LinkIPAddressExists("br-ovn", vtepIPNet)
			networkhelper.EXPECT().SetLinkIPAddress("br-ovn", vtepIPNet)
			networkhelper.EXPECT().SetLinkUp("br-ovn")
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil)
			networkhelper.EXPECT().AddRoute(hostCIDR, gateway, "br-ovn", ptr.To[int](10000), nil)
			networkhelper.EXPECT().GetPFRepMACAddress("pf0hpf").Return(mac, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			_, flannelIPNet, err := net.ParseCIDR(flannelIP.String())
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(false, nil)
			networkhelper.EXPECT().AddRule(flannelIPNet, 60, 31000).Return(nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(false, nil)
			networkhelper.EXPECT().AddRule(oobIPNetWith32Mask, 60, 32000).Return(nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(false, nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, defaultGateway, "br-comm-ch", nil, ptr.To(60)).Return(nil)

			ovsClient.EXPECT().SetOVNEncapIP(net.ParseIP("192.168.1.1"))
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")

			fakeNode.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Node"))
			fakeNode.SetManagedFields(nil)
			data, err := json.Marshal(fakeNode)
			Expect(err).ToNot(HaveOccurred())
			_, err = kubernetesClient.CoreV1().Nodes().Patch(context.Background(), fakeNode.Name, types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: "somemanager",
				Force:        ptr.To[bool](true),
			})
			Expect(err).NotTo(HaveOccurred())

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			ovnInputGatewayOpts, err := os.ReadFile(ovnInputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ovnInputGatewayOpts)).To(Equal("[Gateway]\nnext-hop=192.168.1.10\nrouter-subnet=192.168.1.0/24\n"))
		})
	})
	Context("When checking for idempotency", func() {
		It("should not error out on subsequent runs when network calls and OVS calls are fully mocked", func(ctx context.Context) {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			vtepIPNet, err := netlink.ParseIPNet("192.168.1.1/24")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.10")
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			hostCIDR, err := netlink.ParseIPNet("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			pfIPNet, err := netlink.ParseIPNet("192.168.1.2/24")
			Expect(err).ToNot(HaveOccurred())
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset(fakeNode)
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.InternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, vtepIPNet, gateway, vtepCIDR, hostCIDR, pfIPNet, fakeNode.Name, nil, 1500)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				return kexec.New().Command("echo")
			}))

			// These are needed because of checks we have specific to num of IPs belonging to each interface, we can't
			// mock them with gomock.Any()
			dummyIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{dummyIP}, nil)
			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{dummyIP}, nil)
			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{dummyIP}, nil)
			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{dummyIP}, nil)

			networkHelperMockAll(networkhelper)
			ovsClientMockAll(ovsClient)

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())
		})
		It("should not error out when network and ovs clients are mocked like in the real world", func(ctx context.Context) {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			vtepIPNet, err := netlink.ParseIPNet("192.168.1.1/24")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.10")
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			hostCIDR, err := netlink.ParseIPNet("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			pfIPNet, err := netlink.ParseIPNet("192.168.1.2/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNet, err := netlink.ParseIPNet("10.0.100.100/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNetWith32Mask, err := netlink.ParseIPNet("10.0.100.100/32")
			Expect(err).ToNot(HaveOccurred())
			flannelIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			_, defaultRouteNetwork, err := net.ParseCIDR("0.0.0.0/0")
			Expect(err).ToNot(HaveOccurred())
			defaultGateway := net.ParseIP("10.0.100.254")
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset(fakeNode)
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.InternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, vtepIPNet, gateway, vtepCIDR, hostCIDR, pfIPNet, fakeNode.Name, nil, 1500)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				return kexec.New().Command("echo")
			}))

			By("Checking the first run")
			networkhelper.EXPECT().LinkIPAddressExists("br-ovn", vtepIPNet)
			networkhelper.EXPECT().SetLinkIPAddress("br-ovn", vtepIPNet)
			networkhelper.EXPECT().SetLinkUp("br-ovn")
			networkhelper.EXPECT().RouteExists(vtepCIDR, gateway, "br-ovn", nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, gateway, "br-ovn", nil, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil)
			networkhelper.EXPECT().AddRoute(hostCIDR, gateway, "br-ovn", ptr.To[int](10000), nil)
			mac, _ := net.ParseMAC("00:00:00:00:00:01")
			networkhelper.EXPECT().GetPFRepMACAddress("pf0hpf").Return(mac, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			_, flannelIPNet, err := net.ParseCIDR(flannelIP.String())
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(false, nil)
			networkhelper.EXPECT().AddRule(flannelIPNet, 60, 31000).Return(nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(false, nil)
			networkhelper.EXPECT().AddRule(oobIPNetWith32Mask, 60, 32000).Return(nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(false, nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, defaultGateway, "br-comm-ch", nil, ptr.To(60)).Return(nil)

			ovsClient.EXPECT().SetOVNEncapIP(net.ParseIP("192.168.1.1"))
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			By("Checking the second run")
			networkhelper.EXPECT().LinkIPAddressExists("br-ovn", vtepIPNet).Return(true, nil)
			networkhelper.EXPECT().SetLinkUp("br-ovn")
			networkhelper.EXPECT().RouteExists(vtepCIDR, gateway, "br-ovn", nil).Return(true, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil).Return(true, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(true, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(true, nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(true, nil)

			ovsClient.EXPECT().SetOVNEncapIP(net.ParseIP("192.168.1.1"))
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())
		})
		It("should not start another dnsmasq if dnsmasq already running", func(ctx context.Context) {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			vtepIPNet, err := netlink.ParseIPNet("192.168.1.1/24")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.10")
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			hostCIDR, err := netlink.ParseIPNet("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			pfIPNet, err := netlink.ParseIPNet("192.168.1.2/24")
			Expect(err).ToNot(HaveOccurred())
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset(fakeNode)
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.InternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, vtepIPNet, gateway, vtepCIDR, hostCIDR, pfIPNet, fakeNode.Name, nil, 1500)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				return kexec.New().Command("echo")
			}))

			// These are needed because of checks we have specific to num of IPs belonging to each interface, we can't
			// mock them with gomock.Any()
			dummyIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{dummyIP}, nil)
			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{dummyIP}, nil)
			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{dummyIP}, nil)
			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{dummyIP}, nil)

			networkHelperMockAll(networkhelper)
			ovsClientMockAll(ovsClient)

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())
			Expect(fakeExec.CommandCalls).To(Equal(1))
		})

	})
})

var _ = Describe("DPU CNI Provisioner in External mode", func() {
	Context("When it runs once for the first time", func() {
		It("should configure the system fully when same subnet across DPUs", func() {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			_, hostCIDR, err := net.ParseCIDR("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			_, gatewayDiscoveryNetwork, err := net.ParseCIDR("169.254.99.100/32")
			Expect(err).ToNot(HaveOccurred())
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			oobIPNet, err := netlink.ParseIPNet("10.0.100.100/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNetWith32Mask, err := netlink.ParseIPNet("10.0.100.100/32")
			Expect(err).ToNot(HaveOccurred())
			flannelIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			_, defaultRouteNetwork, err := net.ParseCIDR("0.0.0.0/0")
			Expect(err).ToNot(HaveOccurred())
			defaultGateway := net.ParseIP("10.0.100.254")
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset(fakeNode)
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.ExternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, nil, nil, vtepCIDR, hostCIDR, nil, fakeNode.Name, gatewayDiscoveryNetwork, 0)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			netplanDirPath := filepath.Join(tmpDir, "/etc/netplan")
			Expect(os.MkdirAll(netplanDirPath, 0755)).To(Succeed())
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())
			ovnInputPath := filepath.Join(ovnInputDirPath, "ovn_k8s.conf")

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				Expect(cmd).To(Equal("netplan"))
				Expect(args).To(Equal([]string{"apply"}))
				return kexec.New().Command("echo")
			}))

			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			brOVNAddress, err := netlink.ParseIPNet("192.168.0.3/23")
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{brOVNAddress}, nil)
			_, fakeNetwork, err := net.ParseCIDR("169.254.99.100/32")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.254")
			networkhelper.EXPECT().GetGateway(fakeNetwork).Return(gateway, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil)
			networkhelper.EXPECT().AddRoute(hostCIDR, gateway, "br-ovn", ptr.To[int](10000), nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			_, flannelIPNet, err := net.ParseCIDR(flannelIP.String())
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(false, nil)
			networkhelper.EXPECT().AddRule(flannelIPNet, 60, 31000).Return(nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(false, nil)
			networkhelper.EXPECT().AddRule(oobIPNetWith32Mask, 60, 32000).Return(nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(false, nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, defaultGateway, "br-comm-ch", nil, ptr.To(60)).Return(nil)

			ovsClient.EXPECT().SetOVNEncapIP(brOVNAddress.IP)

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			ovnInputGatewayOpts, err := os.ReadFile(ovnInputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ovnInputGatewayOpts)).To(Equal("[Gateway]\nnext-hop=192.168.1.254\nrouter-subnet=192.168.0.0/23\n"))

			netplanFileContent, err := os.ReadFile(filepath.Join(netplanDirPath, "80-br-ovn.yaml"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(netplanFileContent)).To(Equal(`
network:
  renderer: networkd
  version: 2
  bridges:
    br-ovn:
      dhcp4: yes
      dhcp4-overrides:
        use-dns: no
      openvswitch: {}
`))

		})
	})
	Context("When checking for idempotency", func() {
		It("should not error out when network and ovs clients are mocked like in the real world", func(ctx context.Context) {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			_, hostCIDR, err := net.ParseCIDR("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			_, gatewayDiscoveryNetwork, err := net.ParseCIDR("169.254.99.100/32")
			Expect(err).ToNot(HaveOccurred())
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			oobIPNet, err := netlink.ParseIPNet("10.0.100.100/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNetWith32Mask, err := netlink.ParseIPNet("10.0.100.100/32")
			Expect(err).ToNot(HaveOccurred())
			flannelIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			_, defaultRouteNetwork, err := net.ParseCIDR("0.0.0.0/0")
			Expect(err).ToNot(HaveOccurred())
			defaultGateway := net.ParseIP("10.0.100.254")
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset(fakeNode)
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.ExternalIPAM, clock.NewFakeClock(time.Now()), ovsClient, networkhelper, fakeExec, kubernetesClient, nil, nil, vtepCIDR, hostCIDR, nil, fakeNode.Name, gatewayDiscoveryNetwork, 0)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			netplanDirPath := filepath.Join(tmpDir, "/etc/netplan")
			Expect(os.MkdirAll(netplanDirPath, 0755)).To(Succeed())
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())

			fakeExec.CommandScript = append(fakeExec.CommandScript, kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				Expect(cmd).To(Equal("netplan"))
				Expect(args).To(Equal([]string{"apply"}))
				return kexec.New().Command("echo")
			}))

			brOVNAddress, err := netlink.ParseIPNet("192.168.0.3/23")
			Expect(err).ToNot(HaveOccurred())
			_, fakeNetwork, err := net.ParseCIDR("169.254.99.100/32")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.254")
			By("Checking the first run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{}, nil)

			err = provisioner.RunOnce()
			Expect(err).To(HaveOccurred())

			By("Checking the second run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{brOVNAddress}, nil)
			networkhelper.EXPECT().GetGateway(fakeNetwork).Return(gateway, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil).Return(true, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			_, flannelIPNet, err := net.ParseCIDR(flannelIP.String())
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(false, nil)
			networkhelper.EXPECT().AddRule(flannelIPNet, 60, 31000).Return(nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(false, nil)
			networkhelper.EXPECT().AddRule(oobIPNetWith32Mask, 60, 32000).Return(nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(false, nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, defaultGateway, "br-comm-ch", nil, ptr.To(60)).Return(nil)

			ovsClient.EXPECT().SetOVNEncapIP(brOVNAddress.IP)

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			By("Checking the third run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{brOVNAddress}, nil)
			networkhelper.EXPECT().GetGateway(fakeNetwork).Return(gateway, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil).Return(true, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(true, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(true, nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(true, nil)

			ovsClient.EXPECT().SetOVNEncapIP(brOVNAddress.IP)

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			By("Checking that netplan was restarted only once")
			Expect(fakeExec.CommandCalls).To(Equal(1))
		})
		It("should not run netplan apply when in cooldown period and when network and ovs clients are mocked like in the real world", func(ctx context.Context) {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient := ovsclientMock.NewMockOVSClient(testCtrl)
			networkhelper := networkhelperMock.NewMockNetworkHelper(testCtrl)
			fakeExec := &kexecTesting.FakeExec{}
			_, hostCIDR, err := net.ParseCIDR("10.0.100.1/24")
			Expect(err).ToNot(HaveOccurred())
			_, gatewayDiscoveryNetwork, err := net.ParseCIDR("169.254.99.100/32")
			Expect(err).ToNot(HaveOccurred())
			vtepCIDR, err := netlink.ParseIPNet("192.168.1.0/23")
			Expect(err).ToNot(HaveOccurred())
			oobIPNet, err := netlink.ParseIPNet("10.0.100.100/24")
			Expect(err).ToNot(HaveOccurred())
			oobIPNetWith32Mask, err := netlink.ParseIPNet("10.0.100.100/32")
			Expect(err).ToNot(HaveOccurred())
			flannelIP, err := netlink.ParseIPNet("10.244.6.30/24")
			Expect(err).ToNot(HaveOccurred())
			_, defaultRouteNetwork, err := net.ParseCIDR("0.0.0.0/0")
			Expect(err).ToNot(HaveOccurred())
			defaultGateway := net.ParseIP("10.0.100.254")
			fakeNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu1",
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/host": "host1",
					},
				},
			}
			kubernetesClient := testclient.NewClientset(fakeNode)
			fakeClock := clock.NewFakeClock(time.Now())
			provisioner := dpucniprovisioner.New(context.Background(), dpucniprovisioner.ExternalIPAM, fakeClock, ovsClient, networkhelper, fakeExec, kubernetesClient, nil, nil, vtepCIDR, hostCIDR, nil, fakeNode.Name, gatewayDiscoveryNetwork, 0)

			// Prepare Filesystem
			tmpDir, err := os.MkdirTemp("", "dpucniprovisioner")
			defer func() {
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			}()
			Expect(err).NotTo(HaveOccurred())
			provisioner.FileSystemRoot = tmpDir
			netplanDirPath := filepath.Join(tmpDir, "/etc/netplan")
			Expect(os.MkdirAll(netplanDirPath, 0755)).To(Succeed())
			ovnInputDirPath := filepath.Join(tmpDir, "/etc/openvswitch")
			Expect(os.MkdirAll(ovnInputDirPath, 0755)).To(Succeed())

			fakeCommand := kexecTesting.FakeCommandAction(func(cmd string, args ...string) kexec.Cmd {
				Expect(cmd).To(Equal("netplan"))
				Expect(args).To(Equal([]string{"apply"}))
				return kexec.New().Command("echo")
			})
			fakeExec.CommandScript = append(fakeExec.CommandScript, fakeCommand, fakeCommand)

			brOVNAddress, err := netlink.ParseIPNet("192.168.0.3/23")
			Expect(err).ToNot(HaveOccurred())
			_, fakeNetwork, err := net.ParseCIDR("169.254.99.100/32")
			Expect(err).ToNot(HaveOccurred())
			gateway := net.ParseIP("192.168.1.254")

			By("Checking the first run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{}, nil)

			err = provisioner.RunOnce()
			Expect(err).To(HaveOccurred())

			fakeClock.Step(60 * time.Second)

			By("Checking the second run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{}, nil)

			err = provisioner.RunOnce()
			Expect(err).To(HaveOccurred())

			fakeClock.Step(60 * time.Second)

			By("Checking the third run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{}, nil)

			err = provisioner.RunOnce()
			Expect(err).To(HaveOccurred())

			fakeClock.Step(60 * time.Second)

			By("Checking the fourth run")
			ovsClient.EXPECT().SetKubernetesHostNodeName("host1")
			networkhelper.EXPECT().GetLinkIPAddresses("br-ovn").Return([]*net.IPNet{brOVNAddress}, nil)
			networkhelper.EXPECT().GetGateway(fakeNetwork).Return(gateway, nil)
			networkhelper.EXPECT().RouteExists(hostCIDR, gateway, "br-ovn", nil).Return(true, nil)

			networkhelper.EXPECT().GetLinkIPAddresses("cni0").Return([]*net.IPNet{flannelIP}, nil)
			_, flannelIPNet, err := net.ParseCIDR(flannelIP.String())
			Expect(err).ToNot(HaveOccurred())
			networkhelper.EXPECT().RuleExists(flannelIPNet, 60, 31000).Return(false, nil)
			networkhelper.EXPECT().AddRule(flannelIPNet, 60, 31000).Return(nil)

			networkhelper.EXPECT().GetLinkIPAddresses("br-comm-ch").Return([]*net.IPNet{oobIPNet}, nil)
			networkhelper.EXPECT().RuleExists(oobIPNetWith32Mask, 60, 32000).Return(false, nil)
			networkhelper.EXPECT().AddRule(oobIPNetWith32Mask, 60, 32000).Return(nil)

			networkhelper.EXPECT().GetGateway(defaultRouteNetwork).Return(defaultGateway, nil)
			networkhelper.EXPECT().RouteExists(vtepCIDR, defaultGateway, "br-comm-ch", ptr.To(60)).Return(false, nil)
			networkhelper.EXPECT().AddRoute(vtepCIDR, defaultGateway, "br-comm-ch", nil, ptr.To(60)).Return(nil)

			ovsClient.EXPECT().SetOVNEncapIP(brOVNAddress.IP)

			err = provisioner.RunOnce()
			Expect(err).ToNot(HaveOccurred())

			By("Checking that netplan was restarted only once")
			Expect(fakeExec.CommandCalls).To(Equal(2))
		})
	})
})

// networkHelperMockAll mocks all networkhelper functions. Useful for tests where we don't test the network calls
func networkHelperMockAll(networkHelper *networkhelperMock.MockNetworkHelper) {
	networkHelper.EXPECT().AddRoute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	networkHelper.EXPECT().AddRule(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	networkHelper.EXPECT().GetGateway(gomock.Any()).AnyTimes()
	networkHelper.EXPECT().GetLinkIPAddresses(gomock.Any()).AnyTimes()
	networkHelper.EXPECT().GetPFRepMACAddress(gomock.Any()).AnyTimes()
	networkHelper.EXPECT().LinkIPAddressExists(gomock.Any(), gomock.Any()).AnyTimes()
	networkHelper.EXPECT().RouteExists(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	networkHelper.EXPECT().RuleExists(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	networkHelper.EXPECT().SetLinkIPAddress(gomock.Any(), gomock.Any()).AnyTimes()
	networkHelper.EXPECT().SetLinkUp(gomock.Any()).AnyTimes()
}

// ovsClientMockAll mocks all ovsclient functions. Useful for tests where we don't test the ovsclient calls
func ovsClientMockAll(ovsClient *ovsclientMock.MockOVSClient) {
	ovsClient.EXPECT().SetKubernetesHostNodeName(gomock.Any()).AnyTimes()
	ovsClient.EXPECT().SetOVNEncapIP(gomock.Any()).AnyTimes()
}
