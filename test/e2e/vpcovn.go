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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	vpcutils "github.com/nvidia/doca-platform/test/utils/vpc/ovn"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// vpcPrerequisiteScope manages cleanup for shared VPC OVN prerequisites (DPU services, interfaces)
	vpcPrerequisiteScope *cleanup.Scope
	// vpcOvnContextScope manages cleanup for test-specific VPC OVN resources within each test Context
	vpcOvnContextScope *cleanup.Scope

	// Common interface label maps used across VPC OVN functions
	physicalInterfaceLabels = map[string]string{vpcutils.InterfaceLabelKey: vpcutils.PhysicalInterface0}
	brOVNExtLabels          = map[string]string{vpcutils.InterfaceLabelKey: vpcutils.OvnExtPatchName}
)

const (
	ovnChartName    = "ovn-chart"
	vpcOvnChartName = "dpf-vpc-ovn"
)

// TestVPCConfig holds VPC test configuration
type TestVPCConfig struct {
	Name               string
	Namespace          string
	Tenant             string
	VirtualNetworkName string
	Subnet             string
	Labels             map[string]string
}

type vpcOvnTestInput struct {
	dpuServiceOVNCentral        *dpuservicev1.DPUService
	dpuServiceOVNController     *dpuservicev1.DPUService
	dpuServiceVPCOVNController  *dpuservicev1.DPUService
	dpuServiceVPCOVNNode        *dpuservicev1.DPUService
	dpuServiceIPAMTemplate      *dpuservicev1.DPUServiceIPAM
	dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface
	dpuServiceChainTemplate     *dpuservicev1.DPUServiceChain
	dhcpDaemonSet               *appsv1.DaemonSet
}

func (t *vpcOvnTestInput) applyVPCOVNConfig(conf config) {
	dpuServiceIPAMTemplate := &dpuservicev1.DPUServiceIPAM{}
	ipam := unstructuredFromFile(conf.DPUServiceIPAMTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ipam.Object, dpuServiceIPAMTemplate)).To(Succeed())
	t.dpuServiceIPAMTemplate = dpuServiceIPAMTemplate

	dpuServiceInterfaceTemplate := &dpuservicev1.DPUServiceInterface{}
	dsiTemplate := unstructuredFromFile(conf.DPUServiceInterfaceTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dsiTemplate.Object, dpuServiceInterfaceTemplate)).To(Succeed())
	t.dpuServiceInterfaceTemplate = dpuServiceInterfaceTemplate

	dpuServiceChainTemplate := &dpuservicev1.DPUServiceChain{}
	chainTemplate := unstructuredFromFile(conf.DPUServiceChainTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(chainTemplate.Object, dpuServiceChainTemplate)).To(Succeed())
	t.dpuServiceChainTemplate = dpuServiceChainTemplate

	dpuServiceOVNCentral := &dpuservicev1.DPUService{}
	svcOVNCentral := unstructuredFromFile(conf.DPUServiceOVNCentralPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcOVNCentral.Object, dpuServiceOVNCentral)).To(Succeed())
	t.dpuServiceOVNCentral = dpuServiceOVNCentral

	dpuServiceOVNController := &dpuservicev1.DPUService{}
	svcOVNController := unstructuredFromFile(conf.DPUServiceOVNControllerPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcOVNController.Object, dpuServiceOVNController)).To(Succeed())
	t.dpuServiceOVNController = dpuServiceOVNController

	dpuServiceVPCOVNController := &dpuservicev1.DPUService{}
	svcVPCOVNController := unstructuredFromFile(conf.DPUServiceVPCOVNControllerPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcVPCOVNController.Object, dpuServiceVPCOVNController)).To(Succeed())
	t.dpuServiceVPCOVNController = dpuServiceVPCOVNController

	dpuServiceVPCOVNNode := &dpuservicev1.DPUService{}
	svcVPCOVNNode := unstructuredFromFile(conf.DPUServiceVPCOVNNodePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcVPCOVNNode.Object, dpuServiceVPCOVNNode)).To(Succeed())
	t.dpuServiceVPCOVNNode = dpuServiceVPCOVNNode

	dhcpDaemonSet := &appsv1.DaemonSet{}
	dhcpDaemonSetObj := unstructuredFromFile(conf.DHCPDaemonSetPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dhcpDaemonSetObj.Object, dhcpDaemonSet)).To(Succeed())
	t.dhcpDaemonSet = dhcpDaemonSet
}

func createVtepDPUServiceIPAM(ctx context.Context, input *systemTestInput) {
	vpcVtepIPAMLabels := map[string]string{
		vpcutils.PoolLabelKey: vpcutils.VtepIPPoolName,
	}
	vtepDpuServiceIPAM := generateVPCDPUObj(vpcutils.VtepIPPoolName, dpfOperatorSystemNamespace, input.dpuServiceIPAMTemplate.DeepCopy(), cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, vpcVtepIPAMLabels))
	vpcutils.SetVPCDPUServiceIPAM(vtepDpuServiceIPAM, vpcutils.VtepIPPoolSubnet, vpcutils.VtepIPPoolGateway, vpcutils.IPPoolPerNodeCount)
	By("creating vtep dpu service ipam")
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, vtepDpuServiceIPAM))).ToNot(HaveOccurred())
}

func createGatewayDPUServiceIPAM(ctx context.Context, input *systemTestInput) {
	vpcGatewayIPAMLabels := map[string]string{
		vpcutils.PoolLabelKey: vpcutils.GatewayIPPoolName,
	}
	gatewayDpuServiceIPAM := generateVPCDPUObj(vpcutils.GatewayIPPoolName, dpfOperatorSystemNamespace, input.dpuServiceIPAMTemplate.DeepCopy(), cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, vpcGatewayIPAMLabels))
	vpcutils.SetVPCDPUServiceIPAM(gatewayDpuServiceIPAM, vpcutils.GatewayIPPoolSubnet, vpcutils.GatewayIPPoolGateway, vpcutils.IPPoolPerNodeCount)
	By("creating gateway dpu service ipam")
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, gatewayDpuServiceIPAM))).ToNot(HaveOccurred())
}

// createDPUService creates a generic DPU service with the given name
func createDPUService(ctx context.Context, testClient client.Client, serviceName, namespace string, dpuService *dpuservicev1.DPUService, cleanupLabels map[string]string) {
	dpuService = generateVPCDPUObj(serviceName, namespace, dpuService, cleanupLabels)
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuService))).To(Succeed())
}

// createOVNCentralDPUService creates an OVN central DPU service
func createOVNCentralDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   ovnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	By("creating ovn central service")
	createDPUService(ctx, testClient, vpcutils.OvnCentralService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)
}

// createOVNControllerDPUService creates an OVN controller DPU service
func createOVNControllerDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   ovnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	By("creating ovn controller service")
	createDPUService(ctx, testClient, vpcutils.OvnControllerService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)
}

// createVPCOVNControllerDPUService creates a VPC controller DPU service
func createVPCOVNControllerDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   vpcOvnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	By("creating vpc ovn controller service")
	createDPUService(ctx, testClient, vpcutils.VpcOVNControllerService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)
}

// createVPCOVNNodeDPUService creates a VPC OVN Node DPU service
func createVPCOVNNodeDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   vpcOvnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	dpuService = generateVPCDPUObj(vpcutils.VpcOVNNodeService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)

	// configure OVN SB endpoint
	existingData := make(map[string]any)
	Expect(json.Unmarshal(dpuService.Spec.HelmChart.Values.Raw, &existingData)).To(Succeed())

	dpuSection, ok := existingData["dpu"].(map[string]any)
	Expect(ok).To(BeTrue(), "missing `dpu` section in Helm values")

	vpcNodeSection, ok := dpuSection["vpcOVNNode"].(map[string]any)
	Expect(ok).To(BeTrue(), "missing `vpcOVNNode` section in Helm values")

	initContainers, ok := vpcNodeSection["initContainers"].(map[string]any)
	Expect(ok).To(BeTrue(), "missing `initContainers` in Helm values")

	prov, ok := initContainers["vpcOVNDpuProvisioner"].(map[string]any)
	Expect(ok).To(BeTrue(), "missing `vpcOVNDpuProvisioner` in Helm values")

	dpuProvisionerEnv, ok := prov["env"].(map[string]any)
	Expect(ok).To(BeTrue(), "missing `env` map under vpcOVNDpuProvisioner")

	controlPlaneIP := getClusterControlPlaneIP(ctx, testClient)
	dpuProvisionerEnv["ovnSbEndpoint"] = fmt.Sprintf("tcp:%s:%d", controlPlaneIP, vpcutils.OvnSbPort)

	mergedRaw, err := json.Marshal(existingData)
	Expect(err).NotTo(HaveOccurred())
	dpuService.Spec.HelmChart.Values.Raw = mergedRaw

	By("creating vpc ovn node service")
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuService))).To(Succeed())
}

// createVPCDPUServiceInterface creates a DPU service interface with the given name, type and namespace
func createVPCDPUServiceInterface(ctx context.Context, input *systemTestInput, config dpuservice.TestDPUServiceInterfaceConfig) {
	dpuServiceInterface := generateVPCDPUObj(config.Name, config.Namespace, input.dpuServiceInterfaceTemplate.DeepCopy(), config.Labels)
	if config.NodeName != nil {
		dpuServiceInterface.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      vpcutils.TenantNodeLabelKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{*config.NodeName},
				},
			},
		}
	}
	switch config.Type {
	case dpuservicev1.InterfaceTypePhysical:
		dpuservice.SetDPUServiceInterfacePhysical(dpuServiceInterface, config)
	case dpuservicev1.InterfaceTypeVF:
		dpuservice.SetDPUServiceInterfaceVF(dpuServiceInterface, config)
	case dpuservicev1.InterfaceTypeService:
		dpuservice.SetDPUServiceInterfaceSF(dpuServiceInterface, config)
	case dpuservicev1.InterfaceTypePatch:
		dpuservice.SetDPUServiceInterfacePatch(dpuServiceInterface, config)
	default:
		Fail(fmt.Sprintf("invalid interface type: %s", config.Type))
	}
	By(fmt.Sprintf("creating %s/%s DPUServiceInterface with interface name %s", config.Name, config.Namespace, config.InterfaceName))
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceInterface))).To(Succeed())
}

func createVPCPrerequisiteDPUServiceInterfaces(ctx context.Context, input *systemTestInput) {
	By("creating physical service interface")
	createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
		Name:          vpcutils.PhysicalInterface0,
		InterfaceName: vpcutils.PhysicalInterface0,
		Type:          dpuservicev1.InterfaceTypePhysical,
		Namespace:     input.namespace,
		Labels:        cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, physicalInterfaceLabels),
		Annotations: map[string]string{
			"svc.dpu.nvidia.com/noop-physical-removal": "",
		},
		NodeName:       nil,
		VirtualNetwork: nil,
	})

	By("creating ovn ext service interface")
	createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
		Name:          vpcutils.OvnExtPatchName,
		InterfaceName: vpcutils.OvnExtPatchName,
		Type:          dpuservicev1.InterfaceTypePatch,
		Namespace:     input.namespace,
		Labels:        cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, brOVNExtLabels),
		PeerBridge:    vpcutils.BrOVNExt,
	})
}

func createOrUpdateVPCDPUServiceChain(ctx context.Context, input *systemTestInput, nodeName *string) {
	// Build desired object from template
	desired := generateVPCDPUObj(vpcutils.VpcOVNServiceChain, input.namespace, input.dpuServiceChainTemplate.DeepCopy(), vpcPrerequisiteScope.CleanupLabels)
	if nodeName != nil {
		desired.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      vpcutils.TenantNodeLabelKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{*nodeName},
				},
			},
		}
	}
	desired.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: physicalInterfaceLabels,
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: brOVNExtLabels,
					},
				},
			},
		},
	}

	By("creating or updating vpc ovn service chain")
	existing := &dpuservicev1.DPUServiceChain{}
	err := input.client.Get(ctx, client.ObjectKey{Namespace: input.namespace, Name: vpcutils.VpcOVNServiceChain}, existing)
	if apierrors.IsNotFound(err) {
		Expect(input.client.Create(ctx, desired)).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
	original := existing.DeepCopy()
	existing.SetLabels(desired.GetLabels())
	existing.Spec = desired.Spec
	Expect(input.client.Patch(ctx, existing, client.MergeFrom(original))).To(Succeed())
}

func createDPUServiceChainP0ToInterfaceMatchingLabels(ctx context.Context, input *systemTestInput, name string, matchingInterfaceLabels map[string]string, nodeName *string, labels map[string]string) {
	dpuServiceChain := generateVPCDPUObj(name, input.namespace, input.dpuServiceChainTemplate.DeepCopy(), labels)
	if nodeName != nil {
		dpuServiceChain.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      vpcutils.TenantNodeLabelKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{*nodeName},
				},
			},
		}
	}
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: physicalInterfaceLabels,
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: matchingInterfaceLabels,
					},
				},
			},
		},
	}
	By("creating vpc ovn service chain")
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceChain))).To(Succeed())
}

func getDPUNodesInOrder(ctx context.Context, input *systemTestInput) (corev1.Node, corev1.Node) {
	By("getting DPU cluster nodes in order")
	worker1, _ := getTwoWorkerNodeNames(ctx, input.client)
	dpuNodes := getDPUClusterNodes(ctx, dpuClusterClient[0])
	Expect(dpuNodes).To(HaveLen(2))
	if dpuNodes[0].ObjectMeta.Labels[provisioningv1.DPUNodeNameLabel] == worker1 {
		return dpuNodes[0], dpuNodes[1]
	} else {
		return dpuNodes[1], dpuNodes[0]
	}
}

// generateVPCDPUObj generates a DPU object with the given name, namespace and labels
func generateVPCDPUObj[T client.Object](name, ns string, obj T, labels map[string]string) T {
	obj.SetName(name)
	obj.SetNamespace(ns)
	obj.SetLabels(labels)
	return obj
}

func waitForDHCPDaemonPodsReady(ctx context.Context, testClient client.Client, vpcOvnInput *vpcOvnTestInput) {
	Eventually(func(g Gomega) {
		ds := &appsv1.DaemonSet{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: vpcOvnInput.dhcpDaemonSet.GetNamespace(),
			Name:      vpcOvnInput.dhcpDaemonSet.GetName(),
		}, ds)).To(Succeed())
		g.Expect(ds.Status.ObservedGeneration).To(Equal(ds.GetGeneration()))
		g.Expect(ds.Status.NumberReady).To(BeNumerically(">", 0))
		g.Expect(ds.Status.NumberReady).To(Equal(ds.Status.DesiredNumberScheduled))
	}).WithTimeout(5 * time.Minute).Should(Succeed())
}

// cleanupDPUClusterNodeLabels cleans up the DPU cluster node labels
func cleanupDPUClusterNodeLabels(ctx context.Context) {
	dpuNodes := getDPUClusterNodes(ctx, dpuClusterClient[0])
	Expect(dpuNodes).To(HaveLen(2))

	// Delete the specific labels
	for _, dpuNode := range dpuNodes {
		vpcutils.UpdateDPUNodeLabelsMerge(ctx, dpuClusterClient[0], dpuNode.Name, nil, []string{vpcutils.TenantNodeLabelKey, vpcutils.TenantLabelKey})
	}
}

// createOVNIsolationClass creates an OVN isolation class
func createOVNIsolationClass(ctx context.Context, testClient client.Client, name string, labels map[string]string) {
	controlPlaneIP := getClusterControlPlaneIP(ctx, testClient)
	ovnNbEndpoint := fmt.Sprintf("tcp:%s:%d", controlPlaneIP, vpcutils.OvnNbPort)
	ovnSbEndpoint := fmt.Sprintf("tcp:%s:%d", controlPlaneIP, vpcutils.OvnSbPort)
	ovni := &vpcv1.IsolationClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: vpcv1.IsolationClassSpec{
			Provisioner: name,
			Parameters: map[string]string{
				"ovn-nb-endpoint":       ovnNbEndpoint,
				"ovn-nb-reconnect-time": "5",
				"ovn-sb-endpoint":       ovnSbEndpoint,
				"ovn-sb-reconnect-time": "5",
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, ovni))).To(Succeed())
}

// createDPUVPC creates a DPU VPC
func createDPUVPC(ctx context.Context, testClient client.Client, name, tenant, isolationClassName string, labels map[string]string) {
	dpuVPC := &vpcv1.DPUVPC{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    labels,
		},
		Spec: vpcv1.DPUVPCSpec{
			Tenant:             tenant,
			IsolationClassName: isolationClassName,
			InterNetworkAccess: true,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					vpcutils.TenantLabelKey: tenant,
				},
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuVPC))).To(Succeed())
}

// createDPUVirtualNetwork creates a DPU virtual network
func createDPUVirtualNetwork(ctx context.Context, testClient client.Client, name, vpcName, tenant, subnet string, labels map[string]string) {
	dpuVirtualNetwork := &vpcv1.DPUVirtualNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    labels,
		},
		Spec: vpcv1.DPUVirtualNetworkSpec{
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					vpcutils.TenantLabelKey: tenant,
				},
			},
			VPCName:          vpcName,
			Type:             vpcv1.BridgedVirtualNetworkType,
			ExternallyRouted: true,
			Masquerade:       ptr.To(true),
			BridgedNetwork: &vpcv1.BridgedNetworkSpec{
				IPAM: &vpcv1.BridgedNetworkIPAMSpec{
					IPv4: &vpcv1.BridgedNetworkIPAMIPv4Spec{
						DHCP:   true,
						Subnet: subnet,
					},
				},
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuVirtualNetwork))).To(Succeed())
}

// labelDPUNodesWithTenantAndTenantNode labels DPU nodes with tenant and tenant-node labels
func labelDPUNodesWithTenantAndTenantNode(ctx context.Context, dpuClusterClient client.Client, dpuNode1, dpuNode2 corev1.Node, tenant1Label, tenant2Label string) {
	labelsDPUNode1 := map[string]string{
		vpcutils.TenantNodeLabelKey: dpuNode1.Name,
		vpcutils.TenantLabelKey:     tenant1Label,
	}
	vpcutils.UpdateDPUNodeLabelsMerge(ctx, dpuClusterClient, dpuNode1.Name, labelsDPUNode1, nil)

	labelsDPUNode2 := map[string]string{
		vpcutils.TenantNodeLabelKey: dpuNode2.Name,
		vpcutils.TenantLabelKey:     tenant2Label,
	}
	vpcutils.UpdateDPUNodeLabelsMerge(ctx, dpuClusterClient, dpuNode2.Name, labelsDPUNode2, nil)
}

// createDummyDPUService creates a dummy DPU service
func createDummyDPUService(ctx context.Context, testClient client.Client, namespace, name string, labels map[string]string, tenantNode *string, serviceID, network, interfaceName string) {
	dpuService := &dpuservicev1.DPUService{}
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}
	if ngcAPIKey != "" {
		dpuService.Spec.HelmChart.Values = &machineryruntime.RawExtension{
			Raw: []byte(fmt.Sprintf(`{"imagePullSecrets": [{"name": "%s"}]}`, ngcPullSecretName)),
		}
	}
	dpuService.Spec.ServiceID = ptr.To(serviceID)
	dpuService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
		Resources: corev1.ResourceList{
			"nvidia.com/bf_sf": resource.MustParse("1"),
		},
	}
	if tenantNode != nil {
		dpuService.Spec.ServiceDaemonSet.NodeSelector = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      vpcutils.TenantNodeLabelKey,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{*tenantNode},
						},
					},
				},
			},
		}
	}
	dpuService.Spec.ServiceDaemonSet.Annotations = map[string]string{
		"k8s.v1.cni.cncf.io/networks": fmt.Sprintf(`[{"name": "%s", "interface": "%s"}]`, network, interfaceName),
	}
	dpuService = generateVPCDPUObj(name, namespace, dpuService, labels)

	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuService))).To(Succeed())
}

func validateVPCMetrics(ctx context.Context) {
	By("verify DPUVPC and DPUVirtualNetwork metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuvpc":            {"created", "info", "inter_network_access", "status_conditions", "status_condition_last_transition_time"},
		"dpuvirtualnetwork": {"created", "info", "externally_routed", "masquerade", "status_conditions", "status_condition_last_transition_time"},
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}
