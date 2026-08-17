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
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
	// Routes are static routes added through the gateway of the node subnet, for destinations a Pod
	// is not on-link with.
	Routes []string
	// Labels are set on the pool the DPUServiceIPAM creates, so that a DPUServiceChain port can
	// select it through the matchLabels of its IPAM.
	Labels map[string]string
	// CleanupLabels are the labels the cleanup tracker selects the object by. Empty means the It scope,
	// set it to a named scope for objects that have to outlive the spec creating them.
	CleanupLabels map[string]string
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
	PeerBridge     string
	// NodeSelector restricts the DPUServiceInterface to the DPU cluster nodes it matches.
	NodeSelector *metav1.LabelSelector
	// CleanupLabels are the labels the cleanup tracker selects the object by. Empty means the It scope,
	// set it to a named scope for objects that have to outlive the spec creating them.
	CleanupLabels map[string]string
}

// WaitForDPUServices waits until all expected DPUService objects exist and report Ready.
// On failure, it prints the expected, found, missing, extra, and not-ready services.
func WaitForDPUServices(ctx context.Context, testClient client.Client, namespace string, serviceNames []string) {
	// A timeout of 20 minutes is necessary here because pulling images for all DPUServices
	// on the DPUCluster can be slow. Polling every second avoids busy-listing the namespace.
	Eventually(func(g Gomega) {
		report, err := newDPUServiceReadinessReport(ctx, testClient, namespace, serviceNames)
		if err != nil {
			g.Expect(err).ToNot(HaveOccurred())
			return
		}
		g.Expect(report.missing).To(BeEmpty(), report.String())
		g.Expect(report.notReady).To(BeEmpty(), report.String())
	}).WithTimeout(20 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// dpuServiceReadinessReport captures the current DPUService readiness state for diagnostics.
type dpuServiceReadinessReport struct {
	namespace string
	expected  []string
	found     []string
	missing   []string
	extra     []string
	notReady  []string
}

// newDPUServiceReadinessReport lists DPUService objects and compares them with the expected service names.
func newDPUServiceReadinessReport(ctx context.Context, testClient client.Client, namespace string, expected []string) (dpuServiceReadinessReport, error) {
	dpuServices := &dpuservicev1.DPUServiceList{}
	if err := testClient.List(ctx, dpuServices, client.InNamespace(namespace)); err != nil {
		return dpuServiceReadinessReport{}, err
	}

	expectedSet := sets.New(expected...)
	report := dpuServiceReadinessReport{
		namespace: namespace,
		expected:  sets.List(expectedSet),
	}

	foundSet := sets.New[string]()
	servicesByName := make(map[string]dpuservicev1.DPUService, len(dpuServices.Items))
	for _, service := range dpuServices.Items {
		foundSet.Insert(service.Name)
		servicesByName[service.Name] = service
	}

	report.found = sets.List(foundSet)
	report.missing = sets.List(expectedSet.Difference(foundSet))
	report.extra = sets.List(foundSet.Difference(expectedSet))

	for _, name := range sets.List(expectedSet.Intersection(foundSet)) {
		service := servicesByName[name]
		if !conditions.IsTrue(&service, conditions.TypeReady) {
			report.notReady = append(report.notReady, dpuServiceReadyConditionMessage(&service))
		}
	}

	return report, nil
}

// String formats the readiness report as a stable multi-line failure message.
func (r dpuServiceReadinessReport) String() string {
	return fmt.Sprintf(
		"DPUService readiness mismatch in namespace %q\nexpected: %v\nfound: %v\nmissing: %v\nextra: %v\nnot ready: %v",
		r.namespace, r.expected, r.found, r.missing, r.extra, r.notReady,
	)
}

// dpuServiceReadyConditionMessage formats the Ready condition details for one not-ready service.
func dpuServiceReadyConditionMessage(service *dpuservicev1.DPUService) string {
	readyCondition := conditions.Get(service, conditions.TypeReady)
	if readyCondition == nil {
		return fmt.Sprintf("%s (Ready condition missing, generation=%d)", service.Name, service.Generation)
	}

	message := strings.TrimSpace(readyCondition.Message)
	message = strings.ReplaceAll(message, "\n", "; ")
	if message == "" {
		message = "<empty>"
	}

	return fmt.Sprintf(
		"%s (Ready=%s reason=%s message=%q observedGeneration=%d generation=%d)",
		service.Name,
		readyCondition.Status,
		readyCondition.Reason,
		message,
		readyCondition.ObservedGeneration,
		service.Generation,
	)
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
		ObjectMeta: dpuservicev1.ObjectMeta{
			Labels: cfg.Labels,
		},
		IPV4Network: &dpuservicev1.IPV4Network{
			Network:    cfg.Network,
			PrefixSize: cfg.PrefixSize,
		},
	}
	if cfg.GatewayIndex != 0 {
		DPUServiceIPAM.Spec.IPV4Network.GatewayIndex = &cfg.GatewayIndex
	}
	for _, route := range cfg.Routes {
		DPUServiceIPAM.Spec.IPV4Network.Routes = append(DPUServiceIPAM.Spec.IPV4Network.Routes,
			dpuservicev1.Route{Dst: route})
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
