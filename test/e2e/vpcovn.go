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
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	vpc "github.com/nvidia/doca-platform/test/utils/vpc"
	ovnutils "github.com/nvidia/doca-platform/test/utils/vpc/ovn"

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
	physicalInterfaceLabels = map[string]string{ovnutils.InterfaceLabelKey: ovnutils.PhysicalInterface0}
	brOVNExtLabels          = map[string]string{ovnutils.InterfaceLabelKey: ovnutils.OvnExtPatchName}
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

type VPCOVNTestInput struct {
	DPUServiceOVNCentral        *dpuservicev1.DPUService
	DPUServiceOVNController     *dpuservicev1.DPUService
	DPUServiceVPCOVNController  *dpuservicev1.DPUService
	DPUServiceVPCOVNNode        *dpuservicev1.DPUService
	DPUServiceIPAMTemplate      *dpuservicev1.DPUServiceIPAM
	DPUServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface
	DPUServiceChainTemplate     *dpuservicev1.DPUServiceChain
	DHCPDaemonSet               *appsv1.DaemonSet
}

func (t *VPCOVNTestInput) ApplyVPCOVNConfig(conf Config) {
	dpuServiceIPAMTemplate := &dpuservicev1.DPUServiceIPAM{}
	ipam := unstructuredFromFile(conf.DPUServiceIPAMTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(ipam.Object, dpuServiceIPAMTemplate)).To(Succeed())
	t.DPUServiceIPAMTemplate = dpuServiceIPAMTemplate

	dpuServiceInterfaceTemplate := &dpuservicev1.DPUServiceInterface{}
	dsiTemplate := unstructuredFromFile(conf.DPUServiceInterfaceTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dsiTemplate.Object, dpuServiceInterfaceTemplate)).To(Succeed())
	t.DPUServiceInterfaceTemplate = dpuServiceInterfaceTemplate

	dpuServiceChainTemplate := &dpuservicev1.DPUServiceChain{}
	chainTemplate := unstructuredFromFile(conf.DPUServiceChainTemplatePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(chainTemplate.Object, dpuServiceChainTemplate)).To(Succeed())
	t.DPUServiceChainTemplate = dpuServiceChainTemplate

	dpuServiceOVNCentral := &dpuservicev1.DPUService{}
	svcOVNCentral := unstructuredFromFile(conf.DPUServiceOVNCentralPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcOVNCentral.Object, dpuServiceOVNCentral)).To(Succeed())
	t.DPUServiceOVNCentral = dpuServiceOVNCentral

	dpuServiceOVNController := &dpuservicev1.DPUService{}
	svcOVNController := unstructuredFromFile(conf.DPUServiceOVNControllerPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcOVNController.Object, dpuServiceOVNController)).To(Succeed())
	t.DPUServiceOVNController = dpuServiceOVNController

	dpuServiceVPCOVNController := &dpuservicev1.DPUService{}
	svcVPCOVNController := unstructuredFromFile(conf.DPUServiceVPCOVNControllerPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcVPCOVNController.Object, dpuServiceVPCOVNController)).To(Succeed())
	t.DPUServiceVPCOVNController = dpuServiceVPCOVNController

	dpuServiceVPCOVNNode := &dpuservicev1.DPUService{}
	svcVPCOVNNode := unstructuredFromFile(conf.DPUServiceVPCOVNNodePath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(svcVPCOVNNode.Object, dpuServiceVPCOVNNode)).To(Succeed())
	t.DPUServiceVPCOVNNode = dpuServiceVPCOVNNode

	dhcpDaemonSet := &appsv1.DaemonSet{}
	dhcpObj := unstructuredFromFile(conf.DHCPDaemonSetPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dhcpObj.Object, dhcpDaemonSet)).To(Succeed())
	t.DHCPDaemonSet = dhcpDaemonSet
}

func CreateVtepDPUServiceIPAM(ctx context.Context, input *SystemTestInput) {
	vpcVtepIPAMLabels := map[string]string{
		ovnutils.PoolLabelKey: ovnutils.VtepIPPoolName,
	}
	vtepDpuServiceIPAM := GenerateVPCDPUObj(ovnutils.VtepIPPoolName, DPFOperatorSystemNamespace, input.DPUServiceIPAMTemplate.DeepCopy(), cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, vpcVtepIPAMLabels))
	ovnutils.SetVPCDPUServiceIPAM(vtepDpuServiceIPAM, ovnutils.VtepIPPoolSubnet, ovnutils.VtepIPPoolGateway, ovnutils.IPPoolPerNodeCount)
	By("Creating VTEP DPU service IPAM")
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, vtepDpuServiceIPAM))).ToNot(HaveOccurred())
}

func CreateGatewayDPUServiceIPAM(ctx context.Context, input *SystemTestInput) {
	vpcGatewayIPAMLabels := map[string]string{
		ovnutils.PoolLabelKey: ovnutils.GatewayIPPoolName,
	}
	gatewayDpuServiceIPAM := GenerateVPCDPUObj(ovnutils.GatewayIPPoolName, DPFOperatorSystemNamespace, input.DPUServiceIPAMTemplate.DeepCopy(), cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, vpcGatewayIPAMLabels))
	ovnutils.SetVPCDPUServiceIPAM(gatewayDpuServiceIPAM, ovnutils.GatewayIPPoolSubnet, ovnutils.GatewayIPPoolGateway, ovnutils.IPPoolPerNodeCount)
	By("Creating Gateway DPU service IPAM")
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, gatewayDpuServiceIPAM))).ToNot(HaveOccurred())
}

// CreateDPUService creates a generic DPU service with the given name
func CreateDPUService(ctx context.Context, testClient client.Client, serviceName, namespace string, dpuService *dpuservicev1.DPUService, cleanupLabels map[string]string) {
	dpuService = GenerateVPCDPUObj(serviceName, namespace, dpuService, cleanupLabels)
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuService))).To(Succeed())
}

// CreateOVNCentralDPUService creates an OVN central DPU service
func CreateOVNCentralDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   ovnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	By("Creating OVN central service")
	CreateDPUService(ctx, testClient, ovnutils.OvnCentralService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)
}

// CreateOVNControllerDPUService creates an OVN controller DPU service
func CreateOVNControllerDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   ovnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	By("Creating OVN controller service")
	CreateDPUService(ctx, testClient, ovnutils.OvnControllerService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)
}

// CreateVPCOVNControllerDPUService creates a VPC controller DPU service
func CreateVPCOVNControllerDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   vpcOvnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	By("Creating VPC OVN controller service")
	CreateDPUService(ctx, testClient, ovnutils.VpcOVNControllerService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)
}

// CreateVPCOVNNodeDPUService creates a VPC OVN Node DPU service
func CreateVPCOVNNodeDPUService(ctx context.Context, testClient client.Client, namespace string, dpuServiceTemplate *dpuservicev1.DPUService) {
	dpuService := dpuServiceTemplate.DeepCopy()
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   vpcOvnChartName,
		Version: tag,
		RepoURL: helmRegistry,
	}
	dpuService = GenerateVPCDPUObj(ovnutils.VpcOVNNodeService, namespace, dpuService, vpcPrerequisiteScope.CleanupLabels)

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
	dpuProvisionerEnv["ovnSbEndpoint"] = fmt.Sprintf("tcp:%s:%d", controlPlaneIP, ovnutils.OvnSbPort)

	mergedRaw, err := json.Marshal(existingData)
	Expect(err).NotTo(HaveOccurred())
	dpuService.Spec.HelmChart.Values.Raw = mergedRaw

	By("Creating VPC OVN node service")
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuService))).To(Succeed())
}

// CreateVPCDPUServiceInterface creates a DPU service interface with the given name, type and namespace
func CreateVPCDPUServiceInterface(ctx context.Context, input *SystemTestInput, config dpuservice.TestDPUServiceInterfaceConfig) {
	dpuServiceInterface := GenerateVPCDPUObj(config.Name, config.Namespace, input.DPUServiceInterfaceTemplate.DeepCopy(), config.Labels)
	if config.NodeName != nil {
		dpuServiceInterface.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      ovnutils.TenantNodeLabelKey,
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
	By(fmt.Sprintf("Creating %s/%s DPUServiceInterface with interface name %s", config.Name, config.Namespace, config.InterfaceName))
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, dpuServiceInterface))).To(Succeed())
}

func CreateVPCPrerequisiteDPUServiceInterfaces(ctx context.Context, input *SystemTestInput) {
	By("Creating physical service interface")
	CreateVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
		Name:          ovnutils.PhysicalInterface0,
		InterfaceName: ovnutils.PhysicalInterface0,
		Type:          dpuservicev1.InterfaceTypePhysical,
		Namespace:     input.Namespace,
		Labels:        cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, physicalInterfaceLabels),
		Annotations: map[string]string{
			"svc.dpu.nvidia.com/noop-physical-removal": "",
		},
		NodeName:       nil,
		VirtualNetwork: nil,
	})

	By("Creating OVN ext service interface")
	CreateVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
		Name:          ovnutils.OvnExtPatchName,
		InterfaceName: ovnutils.OvnExtPatchName,
		Type:          dpuservicev1.InterfaceTypePatch,
		Namespace:     input.Namespace,
		Labels:        cleanup.MergeMaps(vpcPrerequisiteScope.CleanupLabels, brOVNExtLabels),
		PeerBridge:    ovnutils.BrOVNExt,
	})
}

func CreateOrUpdateVPCDPUServiceChain(ctx context.Context, input *SystemTestInput, nodeName *string) {
	// Build desired object from template
	desired := GenerateVPCDPUObj(ovnutils.VpcOVNServiceChain, input.Namespace, input.DPUServiceChainTemplate.DeepCopy(), vpcPrerequisiteScope.CleanupLabels)
	if nodeName != nil {
		desired.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      ovnutils.TenantNodeLabelKey,
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

	By("Creating or updating VPC OVN service chain")
	existing := &dpuservicev1.DPUServiceChain{}
	err := input.Client.Get(ctx, client.ObjectKey{Namespace: input.Namespace, Name: ovnutils.VpcOVNServiceChain}, existing)
	if apierrors.IsNotFound(err) {
		Expect(input.Client.Create(ctx, desired)).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
	original := existing.DeepCopy()
	existing.SetLabels(desired.GetLabels())
	existing.Spec = desired.Spec
	Expect(input.Client.Patch(ctx, existing, client.MergeFrom(original))).To(Succeed())
}

func CreateDPUServiceChainP0ToInterfaceMatchingLabels(ctx context.Context, input *SystemTestInput, name string, matchingInterfaceLabels map[string]string, nodeName *string, labels map[string]string) {
	dpuServiceChain := GenerateVPCDPUObj(name, input.Namespace, input.DPUServiceChainTemplate.DeepCopy(), labels)
	if nodeName != nil {
		dpuServiceChain.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      ovnutils.TenantNodeLabelKey,
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
	By("Creating VPC OVN service chain")
	Expect(client.IgnoreAlreadyExists(input.Client.Create(ctx, dpuServiceChain))).To(Succeed())
}

// GenerateVPCDPUObj generates a DPU object with the given name, namespace and labels
func GenerateVPCDPUObj[T client.Object](name, ns string, obj T, labels map[string]string) T {
	obj.SetName(name)
	obj.SetNamespace(ns)
	obj.SetLabels(labels)
	return obj
}

// CleanupDPUClusterNodeLabels cleans up the DPU cluster node labels
func CleanupDPUClusterNodeLabels(ctx context.Context) {
	dpuNodes := getDPUClusterNodes(ctx, DPUClusterClient[0])
	Expect(dpuNodes).To(HaveLen(2))

	// Delete the specific labels
	for _, dpuNode := range dpuNodes {
		vpc.UpdateDPUNodeLabelsMerge(ctx, DPUClusterClient[0], dpuNode.Name, nil, []string{ovnutils.TenantNodeLabelKey, ovnutils.TenantLabelKey})
	}
}

// CreateOVNIsolationClass creates an OVN isolation class
func CreateOVNIsolationClass(ctx context.Context, testClient client.Client, name string, labels map[string]string) {
	controlPlaneIP := getClusterControlPlaneIP(ctx, testClient)
	ovnNbEndpoint := fmt.Sprintf("tcp:%s:%d", controlPlaneIP, ovnutils.OvnNbPort)
	ovnSbEndpoint := fmt.Sprintf("tcp:%s:%d", controlPlaneIP, ovnutils.OvnSbPort)
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

// CreateDPUVPC creates a DPU VPC
func CreateDPUVPC(ctx context.Context, testClient client.Client, name, tenant, isolationClassName string, labels map[string]string) {
	dpuVPC := &vpcv1.DPUVPC{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DPFOperatorSystemNamespace,
			Labels:    labels,
		},
		Spec: vpcv1.DPUVPCSpec{
			Tenant:             tenant,
			IsolationClassName: isolationClassName,
			InterNetworkAccess: true,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					ovnutils.TenantLabelKey: tenant,
				},
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuVPC))).To(Succeed())
}

// CreateDPUVirtualNetwork creates a DPU virtual network
func CreateDPUVirtualNetwork(ctx context.Context, testClient client.Client, name, vpcName, tenant, subnet string, labels map[string]string) {
	dpuVirtualNetwork := &vpcv1.DPUVirtualNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DPFOperatorSystemNamespace,
			Labels:    labels,
		},
		Spec: vpcv1.DPUVirtualNetworkSpec{
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					ovnutils.TenantLabelKey: tenant,
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

// LabelDPUNodesWithTenantAndTenantNode labels DPU nodes with tenant and tenant-node labels
func LabelDPUNodesWithTenantAndTenantNode(ctx context.Context, dpuClusterClient client.Client, dpuNode1, dpuNode2 corev1.Node, tenant1Label, tenant2Label string) {
	labelsDPUNode1 := map[string]string{
		ovnutils.TenantNodeLabelKey: dpuNode1.Name,
		ovnutils.TenantLabelKey:     tenant1Label,
	}
	vpc.UpdateDPUNodeLabelsMerge(ctx, dpuClusterClient, dpuNode1.Name, labelsDPUNode1, nil)

	labelsDPUNode2 := map[string]string{
		ovnutils.TenantNodeLabelKey: dpuNode2.Name,
		ovnutils.TenantLabelKey:     tenant2Label,
	}
	vpc.UpdateDPUNodeLabelsMerge(ctx, dpuClusterClient, dpuNode2.Name, labelsDPUNode2, nil)
}

// CreateDummyDPUService creates a dummy DPU service
func CreateDummyDPUService(ctx context.Context, testClient client.Client, namespace, name string, labels map[string]string, tenantNode *string, serviceID, network, interfaceName string) {
	dpuService := &dpuservicev1.DPUService{}
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}
	if ngcAPIKey != "" {
		dpuService.Spec.HelmChart.Values = &machineryruntime.RawExtension{
			Raw: []byte(fmt.Sprintf(`{"imagePullSecrets": [{"name": "%s"}]}`, NGCPullSecretName)),
		}
	}
	dpuService.Spec.ServiceID = ptr.To(serviceID)
	dpuService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
		Resources: corev1.ResourceList{
			"nvidia.com/bf_sf": resource.MustParse("1"),
		},
	}
	dpuService.Spec.Security = &dpuservicev1.DPUServiceSecurity{
		Privileged: ptr.To(false),
	}
	if tenantNode != nil {
		dpuService.Spec.ServiceDaemonSet.NodeSelector = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      ovnutils.TenantNodeLabelKey,
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
	dpuService = GenerateVPCDPUObj(name, namespace, dpuService, labels)

	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuService))).To(Succeed())
}

func ValidateVPCMetrics(ctx context.Context) {
	By("Verify DPUVPC and DPUVirtualNetwork metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuvpc":            {"created", "info", "inter_network_access", "status_conditions", "status_condition_last_transition_time"},
		"dpuvirtualnetwork": {"created", "info", "externally_routed", "masquerade", "status_conditions", "status_condition_last_transition_time"},
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, HostClusterRESTClient, MetricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func VPCOVNBeforeSuite() {
	By("Setting VPC OVN configs for the test")
	VPCOVNInput.ApplyVPCOVNConfig(*Conf)
}
