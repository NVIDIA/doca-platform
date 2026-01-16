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

package dpuservice

import (
	"context"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestIPAMConfig represents the configuration for IPAM
type TestIPAMConfig struct {
	Name         string
	Network      string
	GatewayIndex int32
	PrefixSize   int32
	DPU1Subnet   string
	DPU2Subnet   string
}

// TestDPUServiceInterfaceConfig holds service interface configuration
type TestDPUServiceInterfaceConfig struct {
	Name           string
	Namespace      string
	Labels         map[string]string
	Annotations    map[string]string
	NodeName       *string
	Type           string
	InterfaceName  string
	PFIndex        int
	VFIndex        int
	ServiceID      string
	Network        string
	VirtualNetwork *string
	ExternalBridge *string
	PeerBridge     string
}

func WaitForDPUServices(ctx context.Context, client client.Client, namespace string, serviceNames []string) {
	Eventually(func(g Gomega) {
		for _, serviceName := range serviceNames {
			g.Expect(IsDPUServiceReady(ctx, g, client, serviceName, namespace)).To(BeTrue())
		}
	}, 20*time.Minute).Should(Succeed())
}

func WaitForDPUDeploymentReady(ctx context.Context, client client.Client, namespace string, deploymentNames []string, timeout time.Duration) {
	Eventually(func(g Gomega) {
		for _, deploymentName := range deploymentNames {
			g.Expect(IsDPUDeploymentReady(ctx, g, client, deploymentName, namespace)).To(BeTrue())
		}
	}, timeout).Should(Succeed())
}

func IsDPUServiceInterfaceReady(ctx context.Context, g Gomega, testClient client.Client, interfaceName string, namespace string) bool {
	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: interfaceName}, dpuServiceInterface)).To(Succeed())
	return conditions.IsTrue(dpuServiceInterface, conditions.TypeReady)
}

func IsDPUServiceReady(ctx context.Context, g Gomega, testClient client.Client, serviceName string, namespace string) bool {
	svc := &dpuservicev1.DPUService{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceName}, svc)).To(Succeed())
	return conditions.IsTrue(svc, conditions.TypeReady)
}

func IsDPUDeploymentReady(ctx context.Context, g Gomega, testClient client.Client, deploymentName string, namespace string) bool {
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: deploymentName}, dpuDeployment)).To(Succeed())
	return conditions.IsTrue(dpuDeployment, conditions.TypeReady)
}

func VerifyUnderlyingDPUObjectsReady(ctx context.Context, dpuClusterClient client.Client, chainNamespace string, interfaceConfigs []TestDPUServiceInterfaceConfig, chainNames []string) {
	Eventually(func(g Gomega) {
		for _, interfaceConfig := range interfaceConfigs {
			g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, dpuClusterClient, interfaceConfig.Name, interfaceConfig.Namespace)).To(BeTrue())
		}
		for _, chainName := range chainNames {
			g.Expect(verifyUnderlyingDPUChainSetsReady(ctx, g, dpuClusterClient, chainName, chainNamespace)).To(BeTrue())
		}
	}, 20*time.Minute).Should(Succeed())
}

func verifyUnderlyingDPUInterfaceSetsReady(ctx context.Context, g Gomega, dpuClusterClient client.Client, name string, namespace string) bool {
	serviceInterfaceSet := &dpuservicev1.ServiceInterfaceSet{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, serviceInterfaceSet)).To(Succeed())
	return conditions.IsTrue(serviceInterfaceSet, conditions.TypeReady)
}

func verifyUnderlyingDPUChainSetsReady(ctx context.Context, g Gomega, dpuClusterClient client.Client, name string, namespace string) bool {
	serviceChainSet := &dpuservicev1.ServiceChainSet{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, serviceChainSet)).To(Succeed())
	return conditions.IsTrue(serviceChainSet, conditions.TypeReady)
}

func isDPUServiceChainReady(ctx context.Context, g Gomega, testClient client.Client, chainName string, namespace string) bool {
	dpuServiceChain := &dpuservicev1.DPUServiceChain{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: chainName}, dpuServiceChain)).To(Succeed())
	return conditions.IsTrue(dpuServiceChain, conditions.TypeReady)
}

func WaitForDPUServiceChainsReady(ctx context.Context, testClient client.Client, dpuClusterClient client.Client, chainNames []string, namespace string, timeout time.Duration) {
	Eventually(func(g Gomega) {
		for _, name := range chainNames {
			g.Expect(isDPUServiceChainReady(ctx, g, testClient, name, namespace)).To(BeTrue())
			g.Expect(verifyUnderlyingDPUChainSetsReady(ctx, g, dpuClusterClient, name, namespace)).To(BeTrue())
		}
	}, timeout).Should(Succeed())
}

func SetDPUServiceInterfacePhysical(dpuServiceInterface *dpuservicev1.DPUServiceInterface, config TestDPUServiceInterfaceConfig) {
	labels := config.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := config.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels:      labels,
		Annotations: annotations,
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypePhysical,
		Physical: &dpuservicev1.Physical{
			InterfaceName: config.InterfaceName,
		},
	}
}

func SetDPUServiceInterfaceVF(dpuServiceInterface *dpuservicev1.DPUServiceInterface, config TestDPUServiceInterfaceConfig) {
	labels := config.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := config.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels:      labels,
		Annotations: annotations,
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypeVF,
		VF: &dpuservicev1.VF{
			ParentInterfaceRef: ptr.To(""),
			PFID:               config.PFIndex,
			VFID:               config.VFIndex,
		},
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec.VF.VirtualNetwork = config.VirtualNetwork
}

func SetDPUServiceInterfaceSF(dpuServiceInterface *dpuservicev1.DPUServiceInterface, config TestDPUServiceInterfaceConfig) {
	labels := config.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := config.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels:      labels,
		Annotations: annotations,
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: "service",
		Service: &dpuservicev1.ServiceDef{
			ServiceID:     config.ServiceID,
			Network:       config.Network,
			InterfaceName: config.InterfaceName,
		},
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec.Service.VirtualNetwork = config.VirtualNetwork
}

func SetDPUServiceInterfaceOVN(dpuServiceInterface *dpuservicev1.DPUServiceInterface, config TestDPUServiceInterfaceConfig) {
	dpuServiceInterface.SetLabels(config.Labels)
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels:      config.Labels,
		Annotations: config.Annotations,
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypeOVN,
		OVN: &dpuservicev1.OVN{
			ExternalBridge: config.ExternalBridge,
		},
	}
}

func SetDPUServiceInterfacePatch(dpuServiceInterface *dpuservicev1.DPUServiceInterface, config TestDPUServiceInterfaceConfig) {
	dpuServiceInterface.SetLabels(config.Labels)
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels:      config.Labels,
		Annotations: config.Annotations,
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypePatch,
		Patch: &dpuservicev1.PatchDef{
			PeerBridge: config.PeerBridge,
		},
	}
}

// WaitForDPUServiceInterfacesReady waits for multiple dpu service interfaces to be ready
func WaitForDPUServiceInterfacesReady(ctx context.Context, testClient client.Client, dpuClusterClient client.Client, dpuServiceInterfaceNames []string, namespace string) {
	for _, name := range dpuServiceInterfaceNames {
		VerifyUnderlyingDPUObjectsReady(ctx, dpuClusterClient, namespace, []TestDPUServiceInterfaceConfig{{Name: name, Namespace: namespace}}, []string{})
		Eventually(func(g Gomega) {
			g.Expect(IsDPUServiceInterfaceReady(ctx, g, testClient, name, namespace)).To(BeTrue())
		}, 10*time.Minute).Should(Succeed())
	}
}

func SetDPUServiceHBNIPAM(DPUServiceIPAM *dpuservicev1.DPUServiceIPAM, cfg TestIPAMConfig, dpu1Name, dpu2Name string) {
	DPUServiceIPAM.Spec = dpuservicev1.DPUServiceIPAMSpec{
		IPV4Network: &dpuservicev1.IPV4Network{
			Network:    cfg.Network,
			PrefixSize: cfg.PrefixSize,
		},
	}
	if cfg.GatewayIndex != 0 {
		DPUServiceIPAM.Spec.IPV4Network.GatewayIndex = &cfg.GatewayIndex
	}
	allocations := make(map[string]string)
	if cfg.DPU1Subnet != "" {
		allocations[dpu1Name] = cfg.DPU1Subnet
	}
	if cfg.DPU2Subnet != "" {
		allocations[dpu2Name] = cfg.DPU2Subnet
	}
	if len(allocations) > 0 {
		DPUServiceIPAM.Spec.IPV4Network.Allocations = allocations
	}
}

func SetHBNChainSwitches(dpuServiceChain *dpuservicev1.DPUServiceChain, interfaceType string, firstInterfaceName string, secondInterfaceName string) {
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{interfaceType: firstInterfaceName},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{dpuservicev1.DPFServiceIDLabelKey: "doca-hbn", "svc.dpu.nvidia.com/interface": fmt.Sprintf("%s_sf", firstInterfaceName)},
					},
				},
			},
		},
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{interfaceType: secondInterfaceName},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{dpuservicev1.DPFServiceIDLabelKey: "doca-hbn", "svc.dpu.nvidia.com/interface": fmt.Sprintf("%s_sf", secondInterfaceName)},
					},
				},
			},
		},
	}
}
