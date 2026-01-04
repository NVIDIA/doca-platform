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

package vpcutils

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LSPMACAddressAnnotationKey is the annotation where the MAC address for a logical switch port is stored
	LSPMACAddressAnnotationKey = "ovn.vpc.dpu.nvidia.com/lsp-mac-address"

	// Network configuration
	VtepIPPoolSubnet     = "20.0.0.0/16"
	VtepIPPoolGateway    = "20.0.0.1"
	GatewayMask          = 16
	GatewayIPPoolSubnet  = "40.0.0.0/16"
	GatewayIPPoolGateway = "40.0.0.1"
	IPPoolPerNodeCount   = 4

	// Network settings
	VfsMTU = 9000

	// Service names
	OvnCentralService       = "ovn-central"
	OvnControllerService    = "ovn-controller"
	VpcOVNControllerService = "vpc-ovn-controller"
	VpcOVNNodeService       = "vpc-ovn-node"

	// Test timeouts
	DefaultTimeout = 1 * time.Minute
	LongTimeout    = 6 * time.Minute

	// Label keys - frequently used in the code
	TenantNodeLabelKey       = "ovn.vpc.dpu.nvidia.com/tenant-node"
	TenantLabelKey           = "ovn.vpc.dpu.nvidia.com/tenant"
	InterfaceLabelKey        = "ovn.vpc.dpu.nvidia.com/interface"
	ServiceInterfaceLabelKey = "svc.dpu.nvidia.com/interface"
	PoolLabelKey             = "ovn.vpc.dpu.nvidia.com/pool"

	// Kubernetes annotation keys
	NetworkStatusAnnotationKey = "k8s.v1.cni.cncf.io/network-status"

	// OVN specific
	OvnNbPort = 30641
	OvnSbPort = 30642

	// Bridge names
	BrOVNExt = "br-ovn-ext"

	// Interface naming patterns
	PhysicalInterface0 = "p0"
	OvnExtPatchName    = "ovn-ext-patch"

	// Service chain name
	VpcOVNServiceChain = "vpc-ovn-vtep-ext-to-p0"

	// IP pool names
	VtepIPPoolName    = "vpc-ippool-vtep"
	GatewayIPPoolName = "vpc-ippool-gateway"
)

// CreatePodNetworkAttachmentDefinition creates a network attachment definition for a pod consuming a VF with the given index and mac address
func CreatePodNetworkAttachmentDefinition(ctx context.Context, testClient client.Client, namespace, podName string, vfIndex int, labels map[string]string) string {
	nadName := fmt.Sprintf("nad-%s", podName)
	name := fmt.Sprintf("hostpf0vf%d", vfIndex)
	hostDevice := fmt.Sprintf("enp8s0f0v%d", vfIndex)

	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      nadName,
				"namespace": namespace,
				"labels":    labels,
			},
			"spec": map[string]interface{}{
				"config": fmt.Sprintf(`{
					"cniVersion": "0.4.0",
					"name": "%s",
					"plugins": [
						{
							"type": "host-device",
							"device": "%s",
							"ipam": {
								"type": "dhcp"
							}
						},
						{
							"type": "tuning",
							"mtu": %d
						}
					]
				}`, name, hostDevice, VfsMTU),
			},
		},
	}

	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, nad))).To(Succeed())
	return nadName
}

// CreateExternalEndpointPodNetworkAttachmentDefinition creates a network attachment definition for a pod consuming a VF with the given index and static ip address
func CreateExternalEndpointPodNetworkAttachmentDefinition(ctx context.Context, testClient client.Client, namespace, podName string, vfIndex int, externalNetIPAddress string, labels map[string]string) string {
	nadName := fmt.Sprintf("nad-%s", podName)
	name := fmt.Sprintf("hostpf0vf%d", vfIndex)
	hostDevice := fmt.Sprintf("enp8s0f0v%d", vfIndex)

	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      nadName,
				"namespace": namespace,
				"labels":    labels,
			},
			"spec": map[string]interface{}{
				"config": fmt.Sprintf(`{
					"cniVersion": "0.4.0",
					"name": "%s",
					"type": "host-device",
					"device": "%s",
					"ipam": {
						"type": "static",
						"addresses": [
							{
								"address": "%s"
							}
						]
					}
				}`, name, hostDevice, externalNetIPAddress),
			},
		},
	}

	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, nad))).To(Succeed())
	return nadName
}

// CreateDPUIntergrationBridgeNetworkAttachmentDefinition creates a network attachment definition for a pod
func CreateDPUIntergrationBridgeNetworkAttachmentDefinition(ctx context.Context, dpuClusterClient client.Client, namespace string, labels map[string]string) string {
	nadName := "mybrint-vpc"

	annotations := map[string]string{
		"k8s.v1.cni.cncf.io/resourceName": "nvidia.com/bf_sf",
	}

	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":        nadName,
				"namespace":   namespace,
				"annotations": annotations,
				"labels":      labels,
			},
			"spec": map[string]interface{}{
				"config": `{
					"cniVersion": "0.4.0",
					"bridge": "br-int",
					"interface_type": "dpdk",
					"mtu": 9000,
					"type": "ovs",
					"ipam": {
						"type": "dhcp",
						"daemonSocketPath": "/run/vpc/cni/dhcp.sock"
					}
				}`,
			},
		},
	}

	Expect(client.IgnoreAlreadyExists(dpuClusterClient.Create(ctx, nad))).To(Succeed())
	return nadName
}

// GetPodIPAddressFromNetworkStatus returns the IP address of a pod from the network status with the given interface name
func GetPodIPAddressFromNetworkStatus(ctx context.Context, testClient client.Client, namespace, podName string, interfaceName string) string {
	pod := &corev1.Pod{}
	Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)).To(Succeed())
	networkStatusList, err := nadutils.GetNetworkStatus(pod)
	Expect(err).NotTo(HaveOccurred())
	for _, network := range networkStatusList {
		if network.Interface == interfaceName {
			return network.IPs[0]
		}
	}
	return ""
}

// GetPodsMatchingLabels gets the pods matching the given labels
func GetPodsMatchingLabels(ctx context.Context, testclient client.Client, namespace string, matchingLabels map[string]string) []corev1.Pod {
	pods := &corev1.PodList{}
	err := testclient.List(
		ctx,
		pods,
		client.InNamespace(namespace),
		client.MatchingLabels(matchingLabels),
	)
	Expect(err).NotTo(HaveOccurred())
	return pods.Items
}

// GetDPUClusterServiceInterfaces gets the DPU cluster service interfaces
func GetDPUClusterServiceInterfaces(ctx context.Context, dpuClusterClient client.Client, namespace string, matchingLabels map[string]string) []dpuservicev1.ServiceInterface {
	serviceInterfaces := &dpuservicev1.ServiceInterfaceList{}
	Expect(dpuClusterClient.List(ctx, serviceInterfaces, client.InNamespace(namespace), client.MatchingLabels(matchingLabels))).To(Succeed())
	return serviceInterfaces.Items
}

// SetVPCDPUServiceIPAM sets the IPAM for the DPU service
func SetVPCDPUServiceIPAM(dpuServiceIPAM *dpuservicev1.DPUServiceIPAM, subnet string, gateway string, perNodeIPCount int) {
	dpuServiceIPAM.Spec = dpuservicev1.DPUServiceIPAMSpec{
		IPV4Subnet: &dpuservicev1.IPV4Subnet{
			Subnet:         subnet,
			Gateway:        gateway,
			PerNodeIPCount: perNodeIPCount,
		},
	}
}

func IsDPUVPCReady(ctx context.Context, g Gomega, testClient client.Client, vpcName, namespace string) bool {
	dpuVPC := &vpcv1.DPUVPC{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: vpcName}, dpuVPC)).To(Succeed())
	return conditions.IsTrue(dpuVPC, conditions.TypeReady)
}

func WaitForDPUVPCReady(ctx context.Context, testClient client.Client, vpcName, namespace string) {
	Eventually(func(g Gomega) {
		g.Expect(IsDPUVPCReady(ctx, g, testClient, vpcName, namespace)).To(BeTrue())
	}, DefaultTimeout).Should(Succeed())
}

func IsDPUServiceVirtualNetworkReady(ctx context.Context, g Gomega, testClient client.Client, networkName, namespace string) bool {
	dpuServiceVirtualNetwork := &vpcv1.DPUVirtualNetwork{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: networkName}, dpuServiceVirtualNetwork)).To(Succeed())
	return conditions.IsTrue(dpuServiceVirtualNetwork, conditions.TypeReady)
}

// UpdateDPUNodeLabelsMerge adds/updates labels from toSet and deletes labels in toDelete.
func UpdateDPUNodeLabelsMerge(ctx context.Context, c client.Client, nodeName string, toSet map[string]string, toDelete []string) {
	node := &corev1.Node{}
	Expect(c.Get(ctx, client.ObjectKey{Name: nodeName}, node)).To(Succeed())
	original := node.DeepCopy()

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	// Add/update
	maps.Copy(node.Labels, toSet)
	// Delete
	for _, k := range toDelete {
		delete(node.Labels, k)
	}

	Expect(c.Patch(ctx, node, client.MergeFrom(original))).To(Succeed())
}

// WaitForDPUServiceVirtualNetworkReady waits for the DPU service virtual network to be ready
func WaitForDPUServiceVirtualNetworkReady(ctx context.Context, testClient client.Client, networkName, namespace string) {
	Eventually(func(g Gomega) {
		g.Expect(IsDPUServiceVirtualNetworkReady(ctx, g, testClient, networkName, namespace)).To(BeTrue())
	}, DefaultTimeout).Should(Succeed())
}

// GetServiceInterfaceMacAddressesMatchingLabels gets the MAC address of the VPC service interface with the given labels
func GetServiceInterfaceMacAddressesMatchingLabels(ctx context.Context, dpuClusterClient client.Client, dpfOperatorSystemNamespace string, labels map[string]string) map[string]string {
	nodeToMACAddresseMap := make(map[string]string)
	Eventually(func(g Gomega) {
		serviceInterface := GetDPUClusterServiceInterfaces(ctx, dpuClusterClient, dpfOperatorSystemNamespace, labels)
		g.Expect(serviceInterface).ToNot(BeEmpty())
		for _, serviceInterface := range serviceInterface {
			g.Expect(serviceInterface.ObjectMeta.Annotations).ToNot(BeNil())
			g.Expect(serviceInterface.ObjectMeta.Annotations[LSPMACAddressAnnotationKey]).ToNot(BeEmpty())
			g.Expect(serviceInterface.Spec.Node).ToNot(BeNil())
			g.Expect(*serviceInterface.Spec.Node).ToNot(BeEmpty())
			nodeToMACAddresseMap[*serviceInterface.Spec.Node] = serviceInterface.ObjectMeta.Annotations[LSPMACAddressAnnotationKey]
		}
	}, DefaultTimeout).Should(Succeed())
	return nodeToMACAddresseMap
}

// SetLinkMacAddress sets the MAC address on a link
func SetLinkMacAddress(host, interfaceName, macAddress string) error {
	// Get password from environment variable
	password := os.Getenv("VM_PASSWORD")
	if password == "" {
		return fmt.Errorf("VM_PASSWORD environment variable is not set")
	}

	// Validate inputs to prevent command injection
	if strings.ContainsAny(interfaceName, ";|&$`") || strings.ContainsAny(macAddress, ";|&$`") {
		return fmt.Errorf("invalid characters in interface name or MAC address")
	}

	// Remove host from known_hosts to avoid conflicts
	sshKeygenCmd := fmt.Sprintf("ssh-keygen -R %s 2>/dev/null || true", host)
	err := exec.Command("bash", "-c", sshKeygenCmd).Run()
	if err != nil {
		return fmt.Errorf("failed to remove host from known_hosts: %v", err)
	}

	// Use password authentication for internal testing
	sshCommand := fmt.Sprintf("sshpass -p '%s' ssh -o StrictHostKeyChecking=no -o PubkeyAuthentication=no root@%s 'ip link set dev %s down && ip link set dev %s address %s && ip link set dev %s up'",
		password, host, interfaceName, interfaceName, macAddress, interfaceName)

	cmd := exec.Command("bash", "-c", sshCommand)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set MAC address on %s: %w, output: %s", host, err, string(output))
	}

	return nil
}

func CreateTestNamespace(ctx context.Context, testClient client.Client, namespace string, labels map[string]string) {
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	testNS.SetLabels(labels)
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())
}
