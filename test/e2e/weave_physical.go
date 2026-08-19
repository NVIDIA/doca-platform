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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var weavePhysicalInput = &weavePhysicalTestInput{}

var (
	// weavePhysicalPrerequisiteScope manages cleanup for shared WeavePhysical prerequisites.
	weavePhysicalPrerequisiteScope *cleanup.Scope
	// weavePhysicalContextScope manages cleanup for test-specific WeavePhysical resources within each test Context.
	weavePhysicalContextScope *cleanup.Scope
)

const (
	// Patch DPUServiceInterface names for underlay and xplane peers into br-sfc.
	weaveIfaceBrcxRail0Sw0   = "p-brcx-r0swp0-to-br-sfc"
	weaveIfaceBrcxRail0Sw1   = "p-brcx-r0swp1-to-br-sfc"
	weaveIfaceXplaneRail0Sw0 = "p-br-xplane-r0swp0-to-br-sfc"
	weaveIfaceXplaneRail0Sw1 = "p-br-xplane-r0swp1-to-br-sfc"

	// OVS peer bridges and xplane group IDs (1 rail, 2 SW planes).
	weavePeerBridgeBrcxRail0Sw0 = "brcx-r0swp0"
	weavePeerBridgeBrcxRail0Sw1 = "brcx-r0swp1"
	weavePeerBridgeXplane       = "br-xplane"
	weaveGroupIDRail0Sw0        = "rail0swp0"
	weaveGroupIDRail0Sw1        = "rail0swp1"

	// Weave DPUService names and chart.
	weaveServiceFlowController = "doca-weave-flow-controller"
	weaveServiceDHCPAgent      = "doca-weave-dhcp-agent"
	weaveHelmChartName         = "dpf-weave"

	// Xplane DPUService name and chart.
	xplaneServiceName   = "doca-xplane"
	xplaneHelmChartName = "xplane"

	weavePhysicalDPUDeploymentName = "weave-physical"

	// BF4 DPU-side ECPF PCI addresses for rail0 sw0 / sw1.
	weaveRail0Sw0PCIAddress = "0000:01:00.0"
	weaveRail0Sw1PCIAddress = "0001:01:00.0"

	weaveCreateTimeout = 60 * time.Second
)

// weavePhysicalTestInput holds config-loaded objects for the BF4 WeavePhysical suite.
type weavePhysicalTestInput struct {
	// Specialized ZT BF4 DPUDeployment referencing the flavor template and weave/xplane services.
	dpuDeployment *dpuservicev1.DPUDeployment
	// Per-DPU flavor template with Spectrum-X underlay bridges and rail netplan.
	dpuFlavorTemplate *provisioningv1.DPUFlavorTemplate
	// Config-file skeleton copied into per-service DPUServiceConfigurations.
	dpuServiceConfigurationSkeleton *dpuservicev1.DPUServiceConfiguration
	// Built xplane and weave DPUServiceConfigurations applied at provision time.
	dpuServiceConfigurations []*dpuservicev1.DPUServiceConfiguration
	// Patch DPUServiceInterface skeleton for br-sfc peer bridges.
	dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface
	// Config-file skeleton copied into per-service DPUServiceTemplates.
	dpuServiceTemplateSkeleton *dpuservicev1.DPUServiceTemplate
	// Built xplane and weave DPUServiceTemplates applied at provision time.
	dpuServiceTemplates []*dpuservicev1.DPUServiceTemplate
	// Helm chart repo URL for weave (from VPC_REGISTRY).
	weaveChartRepoURL string
	// Helm chart version for weave (from VPC_TAG).
	weaveChartVersion string
	// Helm chart repo URL for xplane (from XPLANE_CHART_REPO).
	xplaneChartRepoURL string
	// Helm chart version for xplane (from XPLANE_CHART_VERSION).
	xplaneChartVersion string
}

// loadChartEnv reads XPLANE_CHART_* and VPC_* env vars.
func (t *weavePhysicalTestInput) loadChartEnv() {
	xplaneChartRepo := os.Getenv("XPLANE_CHART_REPO")
	Expect(xplaneChartRepo).ToNot(BeEmpty(), "WeavePhysical requires XPLANE_CHART_REPO env var")
	xplaneChartVersion := os.Getenv("XPLANE_CHART_VERSION")
	Expect(xplaneChartVersion).ToNot(BeEmpty(), "WeavePhysical requires XPLANE_CHART_VERSION env var")
	vpcRegistry := os.Getenv("VPC_REGISTRY")
	Expect(vpcRegistry).ToNot(BeEmpty(), "WeavePhysical requires VPC_REGISTRY env var")
	vpcTag := os.Getenv("VPC_TAG")
	Expect(vpcTag).ToNot(BeEmpty(), "WeavePhysical requires VPC_TAG env var")

	t.xplaneChartRepoURL = normalizeChartRepoURL(xplaneChartRepo)
	t.xplaneChartVersion = xplaneChartVersion
	t.weaveChartRepoURL = normalizeChartRepoURL(vpcRegistry)
	t.weaveChartVersion = vpcTag
}

// normalizeChartRepoURL adds an oci:// prefix unless the registry already has a scheme.
func normalizeChartRepoURL(registry string) string {
	if strings.HasPrefix(registry, "oci://") || strings.HasPrefix(registry, "https://") {
		return registry
	}
	return "oci://" + registry
}

// applyWeavePhysicalConfig loads and specializes suite objects from conf.
func (t *weavePhysicalTestInput) applyWeavePhysicalConfig(conf config) {
	t.loadChartEnv()

	t.dpuDeployment = requiredObjectFromFilePath[dpuservicev1.DPUDeployment](
		Domain.WeavePhysical, "dpuDeployment", conf.DPUDeploymentPath)
	t.dpuFlavorTemplate = requiredObjectFromFile[provisioningv1.DPUFlavorTemplate](
		Domain.WeavePhysical, "dpuFlavorTemplate", conf.DPUFlavorTemplatePath)
	t.dpuServiceInterfaceTemplate = requiredObjectFromFile[dpuservicev1.DPUServiceInterface](
		Domain.WeavePhysical, "dpuServiceInterfaceTemplate", conf.DPUServiceInterfaceTemplatePath)
	t.dpuServiceTemplateSkeleton = requiredObjectFromFilePath[dpuservicev1.DPUServiceTemplate](
		Domain.WeavePhysical, "dpuServiceTemplate", conf.DPUServiceTemplatePath)
	t.dpuServiceConfigurationSkeleton = requiredObjectFromFilePath[dpuservicev1.DPUServiceConfiguration](
		Domain.WeavePhysical, "dpuServiceConfiguration", conf.DPUServiceConfiguration)

	t.configureDPUDeployment()
	t.configureDPUServiceTemplates()
	t.configureDPUServiceConfigurations()
}

// configureDPUDeployment specializes the ZT BF4 DPUDeployment for WeavePhysical.
func (t *weavePhysicalTestInput) configureDPUDeployment() {
	Expect(t.dpuDeployment).ToNot(BeNil())
	Expect(t.dpuFlavorTemplate).ToNot(BeNil())

	t.dpuDeployment.Spec.DPUs.Flavor = nil
	t.dpuDeployment.Spec.DPUs.FlavorTemplate = ptr.To(t.dpuFlavorTemplate.Name)

	// Replace skeleton nodeKey with DPUNodeSelector (CRD forbids both).
	Expect(t.dpuDeployment.Spec.DPUs.DPUSets).NotTo(BeEmpty())
	skeletonDPUSet := t.dpuDeployment.Spec.DPUs.DPUSets[0]
	t.dpuDeployment.Spec.DPUs.DPUSets[0] = dpuservicev1.DPUSet{
		NameSuffix:         skeletonDPUSet.NameSuffix,
		DPUDeviceSelector:  skeletonDPUSet.DPUDeviceSelector,
		DPUClusterSelector: skeletonDPUSet.DPUClusterSelector,
		DPUAnnotations:     skeletonDPUSet.DPUAnnotations,
		DPUNodeSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"feature.node.kubernetes.io/dpu-enabled": "true"},
		},
	}

	t.dpuDeployment.Spec.Services = map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
		xplaneServiceName: {
			ServiceTemplate:      xplaneServiceName,
			ServiceConfiguration: xplaneServiceName,
		},
		weaveServiceFlowController: {
			ServiceTemplate:      weaveServiceFlowController,
			ServiceConfiguration: weaveServiceFlowController,
		},
		weaveServiceDHCPAgent: {
			ServiceTemplate:      weaveServiceDHCPAgent,
			ServiceConfiguration: weaveServiceDHCPAgent,
		},
	}

	t.dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
		UpgradePolicy: dpuservicev1.UpgradePolicy{
			ApplyNodeEffect: ptr.To(false),
		},
		Switches: []dpuservicev1.DPUDeploymentSwitch{
			{
				Ports: []dpuservicev1.DPUDeploymentPort{
					{ServiceInterface: &dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"interface": weaveIfaceBrcxRail0Sw0},
					}},
					{ServiceInterface: &dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"interface": weaveIfaceXplaneRail0Sw0},
					}},
				},
			},
			{
				Ports: []dpuservicev1.DPUDeploymentPort{
					{ServiceInterface: &dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"interface": weaveIfaceBrcxRail0Sw1},
					}},
					{ServiceInterface: &dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"interface": weaveIfaceXplaneRail0Sw1},
					}},
				},
			},
		},
	}
}

// configureDPUServiceTemplates builds xplane and weave DPUServiceTemplates.
func (t *weavePhysicalTestInput) configureDPUServiceTemplates() {
	Expect(t.dpuServiceTemplateSkeleton).ToNot(BeNil())
	xplaneTemplate := t.newDPUServiceTemplate(xplaneServiceName, t.xplaneChartRepoURL, t.xplaneChartVersion, xplaneHelmChartName)
	// Required so ZT privileged-pod denial allows the xplane chart.
	xplaneTemplate.Spec.Security = &dpuservicev1.DPUServiceSecurity{
		Privileged: ptr.To(true),
	}
	t.dpuServiceTemplates = []*dpuservicev1.DPUServiceTemplate{
		xplaneTemplate,
		t.newDPUServiceTemplate(weaveServiceFlowController, t.weaveChartRepoURL, t.weaveChartVersion, weaveHelmChartName),
		t.newDPUServiceTemplate(weaveServiceDHCPAgent, t.weaveChartRepoURL, t.weaveChartVersion, weaveHelmChartName),
	}
}

// newDPUServiceTemplate returns a skeleton DeepCopy pointed at the given chart.
func (t *weavePhysicalTestInput) newDPUServiceTemplate(serviceName, repoURL, version, chart string) *dpuservicev1.DPUServiceTemplate {
	template := t.dpuServiceTemplateSkeleton.DeepCopy()
	template.SetName(serviceName)
	template.SetNamespace(dpfOperatorSystemNamespace)
	template.Spec.DeploymentServiceName = serviceName
	template.Spec.HelmChart = dpuservicev1.HelmChart{
		Source: dpuservicev1.ApplicationSource{
			RepoURL: repoURL,
			Version: version,
			Chart:   chart,
		},
	}
	return template
}

// configureDPUServiceConfigurations builds xplane and weave DPUServiceConfigurations.
func (t *weavePhysicalTestInput) configureDPUServiceConfigurations() {
	Expect(t.dpuServiceConfigurationSkeleton).ToNot(BeNil())
	t.dpuServiceConfigurations = []*dpuservicev1.DPUServiceConfiguration{
		t.newXplaneServiceConfiguration(),
		t.newWeaveFlowControllerConfiguration(),
		t.newWeaveDHCPAgentConfiguration(),
	}
}

// baseConfiguration returns a named skeleton DeepCopy with shared defaults.
func (t *weavePhysicalTestInput) baseConfiguration(serviceName string) *dpuservicev1.DPUServiceConfiguration {
	configuration := t.dpuServiceConfigurationSkeleton.DeepCopy()
	configuration.SetName(serviceName)
	configuration.SetNamespace(dpfOperatorSystemNamespace)
	configuration.Spec.DeploymentServiceName = serviceName
	configuration.Spec.Interfaces = nil
	configuration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{
		ApplyNodeEffect: ptr.To(false),
	}
	return configuration
}

// newXplaneServiceConfiguration creates a DPUServiceConfiguration for the xplane service.
func (t *weavePhysicalTestInput) newXplaneServiceConfiguration() *dpuservicev1.DPUServiceConfiguration {
	configuration := t.baseConfiguration(xplaneServiceName)
	configuration.Spec.ServiceConfiguration = dpuservicev1.ServiceConfiguration{
		HelmChart: dpuservicev1.ServiceConfigurationHelmChart{},
		ServiceDaemonSet: &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
			Labels: map[string]string{weaveDPUServiceLabelKey: xplaneServiceName},
		},
	}
	if ngcAPIKey != "" {
		configuration.Spec.ServiceConfiguration.HelmChart.Values = rawExtension(map[string]any{
			"imagePullSecrets": []map[string]string{{"name": ngcPullSecretName}},
		})
	}
	return configuration
}

// newWeaveFlowControllerConfiguration creates a DPUServiceConfiguration for the weave flow controller.
func (t *weavePhysicalTestInput) newWeaveFlowControllerConfiguration() *dpuservicev1.DPUServiceConfiguration {
	configuration := t.baseConfiguration(weaveServiceFlowController)
	configuration.Spec.ServiceConfiguration = dpuservicev1.ServiceConfiguration{
		HelmChart: dpuservicev1.ServiceConfigurationHelmChart{
			Values: rawExtension(map[string]any{
				"imagePullSecrets": []map[string]string{{"name": dpfPullSecretName}},
				"weaveFlowController": map[string]any{
					"enabled": true,
					"underlayConfigMapData": map[string]any{
						"nicIDType":                  "mac",
						"overlayNetworkPrefixLength": 12,
						"softwarePlaneIDBitLength":   1,
						"railIDBitLength":            3,
						"interfaces": []map[string]string{
							{
								"pciAddress":           weaveRail0Sw0PCIAddress,
								"underlayInterface":    weavePeerBridgeBrcxRail0Sw0,
								"overlayDHCPInterface": "r0swp0",
								"dhcpBridgeName":       "br-dhcp-r0swp0",
								"dropBridgeName":       "br-drop-r0swp0",
							},
							{
								"pciAddress":           weaveRail0Sw1PCIAddress,
								"underlayInterface":    weavePeerBridgeBrcxRail0Sw1,
								"overlayDHCPInterface": "r0swp1",
								"dhcpBridgeName":       "br-dhcp-r0swp1",
								"dropBridgeName":       "br-drop-r0swp1",
							},
						},
					},
				},
			}),
		},
		ServiceDaemonSet: &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
			Labels: map[string]string{weaveDPUServiceLabelKey: weaveServiceFlowController},
		},
	}
	return configuration
}

// newWeaveDHCPAgentConfiguration creates a DPUServiceConfiguration for the weave DHCP agent.
func (t *weavePhysicalTestInput) newWeaveDHCPAgentConfiguration() *dpuservicev1.DPUServiceConfiguration {
	configuration := t.baseConfiguration(weaveServiceDHCPAgent)
	configuration.Spec.ServiceConfiguration = dpuservicev1.ServiceConfiguration{
		ServiceDaemonSet: &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
			Labels: map[string]string{weaveDPUServiceLabelKey: weaveServiceDHCPAgent},
			Resources: corev1.ResourceList{
				"nvidia.com/bf_sf": resource.MustParse("2"),
			},
		},
		HelmChart: dpuservicev1.ServiceConfigurationHelmChart{
			Values: rawExtension(map[string]any{
				"imagePullSecrets": []map[string]string{{"name": dpfPullSecretName}},
				"weaveDHCPAgent": map[string]any{
					"enabled": true,
					"dhcpNetworks": map[string]any{
						"createNADs": true,
						"networks": []map[string]string{
							{
								"name":          "dhcp-r0swp0",
								"bridge":        "br-dhcp-r0swp0",
								"resourceName":  "nvidia.com/bf_sf",
								"interfaceName": "r0swp0",
							},
							{
								"name":          "dhcp-r0swp1",
								"bridge":        "br-dhcp-r0swp1",
								"resourceName":  "nvidia.com/bf_sf",
								"interfaceName": "r0swp1",
							},
						},
					},
				},
			}),
		},
	}
	return configuration
}

// rawExtension marshals values into a RawExtension.
func rawExtension(values map[string]any) *machineryruntime.RawExtension {
	raw, err := json.Marshal(values)
	Expect(err).NotTo(HaveOccurred())
	return &machineryruntime.RawExtension{Raw: raw}
}

// WeavePhysicalBeforeSuite loads WeavePhysical objects during e2e BeforeSuite.
func WeavePhysicalBeforeSuite(c config) {
	By("Setting WeavePhysical configs for the test")
	weavePhysicalInput.applyWeavePhysicalConfig(c)
}

// PrepareWeavePhysicalProvisioning creates the flavor template, services, and
// DPUDeployment that own provisioning for this suite.
func PrepareWeavePhysicalProvisioning(ctx context.Context, input *systemTestInput) {
	Expect(weavePhysicalInput.dpuFlavorTemplate).ToNot(BeNil())
	Expect(weavePhysicalInput.dpuDeployment).ToNot(BeNil(), "dpuDeployment must be configured for WeavePhysical provisioning")

	By("Creating WeavePhysical DPUFlavorTemplate")
	ft := weavePhysicalInput.dpuFlavorTemplate.DeepCopy()
	ft.SetLabels(weavePhysicalPrerequisiteScope.CleanupLabels)
	createWeavePhysicalObject(ctx, input.client, ft)

	By("Creating WeavePhysical DPUServiceInterfaces")
	createWeavePhysicalDPUServiceInterfaces(ctx, input)

	By("Creating WeavePhysical DPUServiceTemplates")
	for _, configuredTemplate := range weavePhysicalInput.dpuServiceTemplates {
		template := configuredTemplate.DeepCopy()
		template.SetLabels(weavePhysicalPrerequisiteScope.CleanupLabels)
		createWeavePhysicalObject(ctx, input.client, template)
	}

	By("Creating WeavePhysical DPUServiceConfigurations")
	for _, configuredConfiguration := range weavePhysicalInput.dpuServiceConfigurations {
		configuration := configuredConfiguration.DeepCopy()
		configuration.SetLabels(weavePhysicalPrerequisiteScope.CleanupLabels)
		createWeavePhysicalObject(ctx, input.client, configuration)
	}

	By("Creating the DPUDeployment with the WeavePhysical DPUFlavorTemplate")
	dpuDeployment := weavePhysicalInput.dpuDeployment.DeepCopy()
	dpuDeployment.SetName(weavePhysicalDPUDeploymentName)
	dpuDeployment.SetLabels(weavePhysicalPrerequisiteScope.CleanupLabels)
	createWeavePhysicalObject(ctx, input.client, dpuDeployment)
}

// createWeavePhysicalObject creates obj with retries, ignoring AlreadyExists.
func createWeavePhysicalObject(ctx context.Context, cl client.Client, obj client.Object) {
	Eventually(func(g Gomega) {
		g.Expect(client.IgnoreAlreadyExists(cl.Create(ctx, obj.DeepCopyObject().(client.Object)))).To(Succeed())
	}).WithTimeout(weaveCreateTimeout).Should(Succeed())
}

// createWeavePhysicalDPUServiceInterfaces creates br-sfc patch interfaces for rails and xplane.
func createWeavePhysicalDPUServiceInterfaces(ctx context.Context, input *systemTestInput) {
	By("Creating WeavePhysical DPUServiceInterfaces")
	for _, iface := range []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:       weaveIfaceBrcxRail0Sw0,
			Namespace:  dpfOperatorSystemNamespace,
			Type:       dpuservicev1.InterfaceTypePatch,
			Labels:     map[string]string{"interface": weaveIfaceBrcxRail0Sw0},
			PeerBridge: weavePeerBridgeBrcxRail0Sw0,
		},
		{
			Name:       weaveIfaceBrcxRail0Sw1,
			Namespace:  dpfOperatorSystemNamespace,
			Type:       dpuservicev1.InterfaceTypePatch,
			Labels:     map[string]string{"interface": weaveIfaceBrcxRail0Sw1},
			PeerBridge: weavePeerBridgeBrcxRail0Sw1,
		},
		{
			Name:       weaveIfaceXplaneRail0Sw0,
			Namespace:  dpfOperatorSystemNamespace,
			Type:       dpuservicev1.InterfaceTypePatch,
			Labels:     map[string]string{"interface": weaveIfaceXplaneRail0Sw0},
			PeerBridge: weavePeerBridgeXplane,
			PeerExternalIDs: map[string]string{
				"xplane":          "true",
				"xplane-group-id": weaveGroupIDRail0Sw0,
				"xplane-downlink": "patch",
			},
		},
		{
			Name:       weaveIfaceXplaneRail0Sw1,
			Namespace:  dpfOperatorSystemNamespace,
			Type:       dpuservicev1.InterfaceTypePatch,
			Labels:     map[string]string{"interface": weaveIfaceXplaneRail0Sw1},
			PeerBridge: weavePeerBridgeXplane,
			PeerExternalIDs: map[string]string{
				"xplane":          "true",
				"xplane-group-id": weaveGroupIDRail0Sw1,
				"xplane-downlink": "patch",
			},
		},
	} {
		createWeavePhysicalDPUServiceInterface(ctx, input, iface)
	}
}

// createWeavePhysicalDPUServiceInterface creates one patch DPUServiceInterface from the template.
func createWeavePhysicalDPUServiceInterface(ctx context.Context, input *systemTestInput, config dpuservice.TestDPUServiceInterfaceConfig) {
	Expect(weavePhysicalInput.dpuServiceInterfaceTemplate).ToNot(BeNil(),
		"dpuServiceInterfaceTemplate must be loaded for WeavePhysical")
	Expect(config.Type).To(Equal(dpuservicev1.InterfaceTypePatch),
		"WeavePhysical currently only creates patch DPUServiceInterfaces")

	dpuServiceInterface := weavePhysicalInput.dpuServiceInterfaceTemplate.DeepCopy()
	dpuServiceInterface.SetName(config.Name)
	dpuServiceInterface.SetNamespace(config.Namespace)
	dpuservice.SetDPUServiceInterfacePatch(dpuServiceInterface, config)
	// Patch helper overwrites object labels; restore cleanup labels and set matchLabels on the template.
	dpuServiceInterface.SetLabels(weavePhysicalPrerequisiteScope.CleanupLabels)
	dpuServiceInterface.Spec.Template.Spec.Template.Labels = config.Labels

	By(fmt.Sprintf("Creating DPUServiceInterface %s/%s (peerBridge=%s)",
		config.Namespace, config.Name, config.PeerBridge))
	createWeavePhysicalObject(ctx, input.client, dpuServiceInterface)
}
