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

package ovnutils

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	vpcutils "github.com/nvidia/doca-platform/test/utils/vpc"

	. "github.com/onsi/gomega"
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

	// Label keys
	TenantNodeLabelKey       = "ovn.vpc.dpu.nvidia.com/tenant-node"
	TenantLabelKey           = "ovn.vpc.dpu.nvidia.com/tenant"
	InterfaceLabelKey        = "ovn.vpc.dpu.nvidia.com/interface"
	ServiceInterfaceLabelKey = "svc.dpu.nvidia.com/interface"
	PoolLabelKey             = "ovn.vpc.dpu.nvidia.com/pool"

	// OVN specific
	OvnNbPort = 30041
	OvnSbPort = 30042

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

// CreateExternalEndpointPodNetworkAttachmentDefinition creates a network attachment definition for a pod consuming a VF with the given index and static ip address.
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

// CreateDPUIntergrationBridgeNetworkAttachmentDefinition creates a network attachment definition for a pod.
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

// GetDPUClusterServiceInterfaces gets the DPU cluster service interfaces.
func GetDPUClusterServiceInterfaces(ctx context.Context, dpuClusterClient client.Client, namespace string, matchingLabels map[string]string) []dpuservicev1.ServiceInterface {
	serviceInterfaces := &dpuservicev1.ServiceInterfaceList{}
	Expect(dpuClusterClient.List(ctx, serviceInterfaces, client.InNamespace(namespace), client.MatchingLabels(matchingLabels))).To(Succeed())
	return serviceInterfaces.Items
}

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
	}, vpcutils.DefaultTimeout).Should(Succeed())
}

func IsDPUServiceVirtualNetworkReady(ctx context.Context, g Gomega, testClient client.Client, networkName, namespace string) bool {
	dpuServiceVirtualNetwork := &vpcv1.DPUVirtualNetwork{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: networkName}, dpuServiceVirtualNetwork)).To(Succeed())
	return conditions.IsTrue(dpuServiceVirtualNetwork, conditions.TypeReady)
}

func WaitForDPUServiceVirtualNetworkReady(ctx context.Context, testClient client.Client, networkName, namespace string) {
	Eventually(func(g Gomega) {
		g.Expect(IsDPUServiceVirtualNetworkReady(ctx, g, testClient, networkName, namespace)).To(BeTrue())
	}, vpcutils.DefaultTimeout).Should(Succeed())
}

// GetServiceInterfaceMacAddressesMatchingLabels gets the MAC address of the VPC service interface with the given labels.
func GetServiceInterfaceMacAddressesMatchingLabels(ctx context.Context, dpuClusterClient client.Client, dpfOperatorSystemNamespace string, labels map[string]string) map[string]string {
	nodeToMACAddresseMap := make(map[string]string)
	Eventually(func(g Gomega) {
		serviceInterfaces := GetDPUClusterServiceInterfaces(ctx, dpuClusterClient, dpfOperatorSystemNamespace, labels)
		g.Expect(serviceInterfaces).ToNot(BeEmpty())
		for _, serviceInterface := range serviceInterfaces {
			g.Expect(serviceInterface.ObjectMeta.Annotations).ToNot(BeNil())
			g.Expect(serviceInterface.ObjectMeta.Annotations[LSPMACAddressAnnotationKey]).ToNot(BeEmpty())
			g.Expect(serviceInterface.Spec.Node).ToNot(BeNil())
			g.Expect(*serviceInterface.Spec.Node).ToNot(BeEmpty())
			nodeToMACAddresseMap[*serviceInterface.Spec.Node] = serviceInterface.ObjectMeta.Annotations[LSPMACAddressAnnotationKey]
		}
	}, vpcutils.DefaultTimeout).Should(Succeed())
	return nodeToMACAddresseMap
}
