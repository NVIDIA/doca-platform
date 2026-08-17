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
	"net/netip"
	"slices"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	sfccontroller "github.com/nvidia/doca-platform/internal/sfccontroller/controllers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A tenant owns a namespace, selects the DPUs its service runs on by labeling their DPUDevices, and
// either needs no bridge connectivity, attaches to the tenant bridge through a DPUServiceNAD, or
// attaches through a DPUServiceChain it owns. HBN, the tenant bridge patch and the chain to HBN stay in the DPF
// operator namespace.
//
// The DPUFlavor creates the bridges on every DPU at provisioning time:
//
//	br-sfc   The chaining bridge. sfc-controller programs the flows a DPUServiceChain asks for.
//	br-tenant1  The tenant segment. A plain learning bridge in the default fail mode, no flows.
const (
	tenantBridge = "br-tenant1"

	// The DPUDeployment owning the DPUs, created by the Core suite.
	dpuDeploymentName = "dpf-dpudeployment"
	hbnServiceName    = "hbn-tenant"
	// HBN's interface on the tenant bridge, declared in the HBN DPUServiceConfiguration. It takes its
	// address from the gateway pool, which is what makes HBN the tenant's gateway.
	tenantHBNInterface = "tenant1_if"

	tenantServiceID    = "dummydpuservice-tenant"
	tenantChainName    = "app-to-bridge"
	tenantPodInterface = "tenant_if"
	tenantNADName      = "tenant-nad"

	// The pool HBN's tenant1_if allocates from too, which is what makes HBN the tenant's gateway.
	// Its own pool rather than the one the HBN chain specs use, so the two cannot collide.
	//
	// The pool cuts a prefix per DPU and reserves index tenantGatewayIndex of it for HBN, so a Pod is
	// on-link with its own DPU's HBN and with nothing else. Reaching the other DPU is routing:
	//
	//	DPU 1  Pods 10.0.121.0/29, HBN .2          DPU 2  Pods 10.0.121.8/29, HBN .10
	//	           └─────────── BGP over p0_if/p1_if, redistribute connected ──────────┘
	//
	// The geometry is declared once here and the per-DPU subnets and the gateway a Pod is expected to
	// reach both derive from it. Spelling it out in more than one place lets the copies drift, and a
	// wrong gateway surfaces only as a ping that never answers.
	tenantGatewayPool  = "tenantpool"
	tenantSubnet       = "10.0.121.0/24"
	tenantPrefixSize   = 29
	tenantGatewayIndex = 2

	// The label key placed on the DPUDevices of the DPUs a tenant selected, one value per service.
	tenantSelectorKey = "tenant.dpu.nvidia.com/service"

	// The cleanup scope of the platform objects the tenant specs share. Registered by the spec file,
	// which is where the tracker lives.
	TenantPlatformScopeName = "tenant-platform"
)

var (
	// Distinct labels: both select a patch interface for the same bridge, one per namespace.
	platformPatchLabels = map[string]string{"patch": tenantBridge}
	tenantPatchLabels   = map[string]string{"patch": tenantBridge + "-app"}

	tenantGatewayPoolLabels = map[string]string{"svc.dpu.nvidia.com/pool": tenantGatewayPool}

	// The platform side is shared by the tenant specs and outlives them, so it carries its own cleanup
	// scope instead of the It one. SetupTenantPlatform fills both.
	tenantPlatformScope      *cleanup.Scope
	tenantPlatformInterfaces []dpuservice.TestDPUServiceInterfaceConfig
	// Whether the DPUDeployment carries HBN, so a setup that failed before that is not undone.
	tenantPlatformHBNAdded bool
)

// SetupTenantPlatform brings up everything the tenant specs share. Meant for a BeforeAll: building it per
// spec would add HBN to the DPUDeployment and remove it again for each of them, which is two rollouts of
// HBN more than the specs need. The scope is the one registered for TenantPlatformScopeName.
func SetupTenantPlatform(ctx context.Context, input *systemTestInput, scope *cleanup.Scope) {
	tenantPlatformScope = scope
	// Drop anything a previous run left behind, e.g. one that ran with cleanup skipped.
	tenantPlatformScope.CleanupBefore()

	By("Creating the HBN side of the chain in " + input.namespace)
	tenantPlatformInterfaces = setupHBNPlatform(ctx, input)
}

// CleanupTenantPlatform removes HBN from the DPUDeployment and then the shared platform objects.
//
// The order matters. The DPUDeployment holds the dependent finalizer on the HBN template and
// configuration for as long as it references them, so deleting them first leaves both with a
// deletionTimestamp and nothing to remove it, and the cleanup times out waiting.
func CleanupTenantPlatform(ctx context.Context, input *systemTestInput) {
	if tenantPlatformScope == nil {
		return
	}
	if tenantPlatformHBNAdded {
		removeHBNFromDPUDeployment(ctx, input)
		tenantPlatformHBNAdded = false
	}
	tenantPlatformScope.CleanupAfter()
}

// VerifyTenantNamespaceViaNAD attaches a DPUService to the tenant bridge through a DPUServiceNAD, with
// no tenant DPUServiceInterface or DPUServiceChain. The NAD names br-tenant1, so the Pod's SF lands on
// the tenant bridge itself and the bridge learns between it and the patch the platform put there:
//
//	Pod ──SF──► br-tenant1 ──► tenant1-patch ──► br-sfc ══flows══► HBN tenant1_if
//	└── tenant namespace ──┘   └────────────── dpf-operator-system ───────────────┘
//	    DPUServiceNAD              DPUServiceInterface, DPUServiceChain, DPUServiceIPAM,
//	    DPUService                 HBN through the DPUDeployment
//
// The tenant owns two objects, and neither of them names HBN, the patch or the chain.
func VerifyTenantNamespaceViaNAD(ctx context.Context, input *systemTestInput) {
	requireTwoDPUs(input)

	const serviceNamespace = "tenant-via-nad"
	const selectorValue = "nad"
	prepareTenantNamespace(ctx, input, serviceNamespace)

	// Both DPUs, so the two Pods can reach each other through HBN.
	selectedNodes := selectDPUsForService(ctx, input, selectorValue, 0, 1)

	By("Create DPUServiceNAD on the tenant bridge in " + serviceNamespace)
	createTenantDPUServiceNAD(ctx, input, serviceNamespace, tenantBridge)

	By("Create the DPUService in " + serviceNamespace)
	tenantService := newTenantDPUServiceOnNAD(input, serviceNamespace, tenantServiceID)
	tenantService.SetServiceDaemonSetNodeSelector(nodeSelectorForService(selectorValue))
	Expect(input.client.Create(ctx, tenantService)).To(Succeed())

	dpuservice.WaitForDPUServices(ctx, input.client, serviceNamespace, []string{tenantServiceID})
	verifyServicePodsOnNodes(ctx, serviceNamespace, tenantServiceID, selectedNodes)

	verifyInterfacesShareSingleNodeServiceInterfaces(ctx, tenantPlatformInterfaces)
	verifyTenantPodTraffic(ctx, serviceNamespace, tenantServiceID)
}

// VerifyTenantNamespaceViaChain attaches a DPUService to the tenant bridge through a service
// DPUServiceInterface, a patch DPUServiceInterface and a DPUServiceChain, all owned by the tenant and
// all carrying the service node selector. The NAD names br-sfc here, so the Pod's SF lands on the
// chaining bridge and the tenant's own chain and patch carry it to the tenant bridge:
//
//	Pod ──SF──► br-sfc ══flows══► tenant-app-patch ──► br-tenant1 ──► tenant1-patch ──► br-sfc ══flows══► HBN tenant1_if
//	└────────────── tenant namespace ──────────────┘                 └──────────── dpf-operator-system ────────────────┘
//	    DPUServiceNAD, two DPUServiceInterfaces,                         DPUServiceInterface, DPUServiceChain,
//	    DPUServiceChain, DPUService                                      DPUServiceIPAM, HBN through the DPUDeployment
//
// The handover is br-tenant1: the tenant's patch ends there, the platform's starts there, and the bridge
// learns between them. The two patches carry distinct labels, so they are separate ports on one bridge.
func VerifyTenantNamespaceViaChain(ctx context.Context, input *systemTestInput) {
	requireTwoDPUs(input)

	const serviceNamespace = "tenant-via-chain"
	const selectorValue = "chain"
	const tenantServiceInterface = "tenant-app-sf"
	const tenantPatchInterface = "tenant-app-patch"

	prepareTenantNamespace(ctx, input, serviceNamespace)
	selectedNodes := selectDPUsForService(ctx, input, selectorValue, 0, 1)

	// The tenant Pods land on br-sfc, the tenant chain carries them from there to the tenant bridge.
	By("Create DPUServiceNAD on " + sfccontroller.BridgeSFC + " in " + serviceNamespace)
	createTenantDPUServiceNAD(ctx, input, serviceNamespace, sfccontroller.BridgeSFC)

	By("Create the tenant DPUServiceInterfaces in " + serviceNamespace)
	tenantServiceInterfaceLabels := map[string]string{"svc.dpu.nvidia.com/interface": tenantServiceInterface}
	tenantInterfaceConfigs := []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:          tenantServiceInterface,
			Namespace:     serviceNamespace,
			Type:          "sf",
			InterfaceName: tenantPodInterface,
			ServiceID:     tenantServiceID,
			Network:       tenantNADName,
			Labels:        tenantServiceInterfaceLabels,
			NodeSelector:  labelSelectorForService(selectorValue),
		},
		{
			Name:       tenantPatchInterface,
			Namespace:  serviceNamespace,
			Type:       dpuservicev1.InterfaceTypePatch,
			PeerBridge: tenantBridge,
			Labels:     tenantPatchLabels,
			// SetDPUServiceInterfacePatch replaces the object labels with the config ones, so the
			// cleanup scope has to be stated here.
			CleanupLabels: CleanupScope.It,
			NodeSelector:  labelSelectorForService(selectorValue),
		},
	}
	// Not waiting for readiness here: the tenant namespace only exists in the DPU cluster once the
	// DPUService below reconciles. Readiness is asserted together further down.
	for _, interfaceConfig := range tenantInterfaceConfigs {
		createDPUServiceInterface(ctx, interfaceConfig, input.dpuServiceInterfaceTemplate, input.client)
	}

	By("Create the tenant DPUServiceChain in " + serviceNamespace)
	tenantChain := utils.GenerateDPUObj(tenantChainName, serviceNamespace, input.dpuServiceChainTemplate.DeepCopy())
	tenantChain.Spec.Template.Spec.NodeSelector = labelSelectorForService(selectorValue)
	tenantChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: tenantServiceInterfaceLabels,
						// No DefaultGateway: it would replace the cluster default route. The pool
						// carries a route for the tenant subnet instead.
						IPAM: &dpuservicev1.IPAM{MatchLabels: tenantGatewayPoolLabels},
					},
				},
				{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: tenantPatchLabels}},
			},
		},
	}
	Expect(input.client.Create(ctx, tenantChain)).To(Succeed())

	By("Create the DPUService in " + serviceNamespace)
	tenantService := newTenantDPUService(input, serviceNamespace, tenantServiceID)
	tenantService.Spec.Interfaces = []string{tenantServiceInterface}
	tenantService.SetServiceDaemonSetNodeSelector(nodeSelectorForService(selectorValue))
	Expect(input.client.Create(ctx, tenantService)).To(Succeed())

	dpuservice.WaitForDPUServices(ctx, input.client, serviceNamespace, []string{tenantServiceID})
	verifyServicePodsOnNodes(ctx, serviceNamespace, tenantServiceID, selectedNodes)

	By("Verify the tenant ServiceChain and ServiceInterface objects are ready")
	dpuservice.WaitForDPUServiceChainsReady(ctx, input.client, dpuClusterClient[0],
		[]string{tenantChainName}, serviceNamespace, 20*time.Minute)
	// Covers the underlying ServiceInterfaceSets in the DPU cluster too.
	dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.client, dpuClusterClient[0],
		[]string{tenantServiceInterface, tenantPatchInterface}, serviceNamespace)

	verifyInterfacesShareSingleNodeServiceInterfaces(ctx,
		append(slices.Clone(tenantPlatformInterfaces), tenantInterfaceConfigs...))
	verifyTenantPodTraffic(ctx, serviceNamespace, tenantServiceID)
}

// setupHBNPlatform brings up the platform side: the uplink and tenant bridge patch interfaces, the
// gateway pools, and HBN itself carrying an interface on the tenant bridge.
//
// HBN is added to the DPUDeployment that owns the DPUs, which the Core suite creates and which runs first
// through its higher SpecPriority. Running the SDN specs with Core filtered out therefore leaves the
// DPUDeployment absent and these specs fail.
func setupHBNPlatform(ctx context.Context, input *systemTestInput) []dpuservice.TestDPUServiceInterfaceConfig {
	// Only the interfaces the DPUDeployment does not own. The HBN service interfaces come from the
	// interfaces declared in its DPUServiceConfiguration.
	interfaceConfigs := append(uplinkInterfaceConfigs(input.namespace, "p0", "p1"), []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:       "tenant1-patch",
			Namespace:  input.namespace,
			Type:       dpuservicev1.InterfaceTypePatch,
			PeerBridge: tenantBridge,
			Labels:     platformPatchLabels,
		},
	}...)
	// These outlive the spec that triggered the setup, so they are cleaned up with the container.
	for i := range interfaceConfigs {
		interfaceConfigs[i].CleanupLabels = tenantPlatformScope.CleanupLabels
	}

	ipamConfigs := []dpuservice.TestIPAMConfig{
		{
			Name:         tenantGatewayPool,
			Network:      tenantSubnet,
			GatewayIndex: tenantGatewayIndex,
			PrefixSize:   tenantPrefixSize,
			DPU1Subnet:   nthTenantSubnet(0),
			DPU2Subnet:   nthTenantSubnet(1),
			// Each node gets its own prefix, so a Pod is on-link only with the HBN gateway of its
			// own DPU. Without this route the reply to a Pod on another DPU leaves through the
			// cluster default route instead of the tenant bridge, and the traffic test sees no response.
			Routes: []string{tenantSubnet},
			// Labeled so the tenant chain port can select this pool.
			Labels:        tenantGatewayPoolLabels,
			CleanupLabels: tenantPlatformScope.CleanupLabels,
		},
		{
			Name:          "loopback",
			Network:       "11.0.0.0/24",
			PrefixSize:    32,
			CleanupLabels: tenantPlatformScope.CleanupLabels,
		},
	}

	By("Wait for prerequisite services")
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"sfc-controller"})

	By("Create and wait for DPU service interfaces")
	createAndWaitForInterfaces(ctx, input.client, input.dpuServiceInterfaceTemplate, interfaceConfigs)

	By("Create HBN IPAMs")
	createHBNIPAMs(ctx, input.client, input.namespace, input.dpuServiceIPAMTemplate, ipamConfigs)

	By("Add HBN with a tenant bridge interface to the DPUDeployment")
	addHBNToDPUDeployment(ctx, input)

	return interfaceConfigs
}

// addHBNToDPUDeployment adds the HBN service and the chain joining the tenant bridge patch to HBN's
// interface to the DPUDeployment owning the DPUs. CleanupTenantPlatform takes them back out.
func addHBNToDPUDeployment(ctx context.Context, input *systemTestInput) {
	// The stock HBN template, renamed onto the service name this spec uses. Its three SFs match the
	// three interfaces the tenant HBN DPUServiceConfiguration declares.
	serviceTemplate := objectFromFile[dpuservicev1.DPUServiceTemplate]("../objects/performance/dpuservicetemplate-hbn.yaml")
	serviceTemplate.SetName(hbnServiceName)
	serviceTemplate.Spec.DeploymentServiceName = hbnServiceName
	serviceTemplate.SetNamespace(input.namespace)
	serviceTemplate.SetLabels(tenantPlatformScope.CleanupLabels)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, serviceTemplate))).To(Succeed())

	serviceConfiguration := objectFromFile[dpuservicev1.DPUServiceConfiguration]("../objects/application/dpuserviceconfig-hbn-tenant.yaml")
	serviceConfiguration.SetNamespace(input.namespace)
	serviceConfiguration.SetLabels(tenantPlatformScope.CleanupLabels)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, serviceConfiguration))).To(Succeed())

	switches := []dpuservicev1.DPUDeploymentSwitch{
		{Ports: []dpuservicev1.DPUDeploymentPort{
			{ServiceInterface: &dpuservicev1.ServiceIfc{MatchLabels: map[string]string{"uplink": "p0"}}},
			{Service: &dpuservicev1.DPUDeploymentService{Name: hbnServiceName, InterfaceName: "p0_if"}},
		}},
		{Ports: []dpuservicev1.DPUDeploymentPort{
			{ServiceInterface: &dpuservicev1.ServiceIfc{MatchLabels: map[string]string{"uplink": "p1"}}},
			{Service: &dpuservicev1.DPUDeploymentService{Name: hbnServiceName, InterfaceName: "p1_if"}},
		}},
		{Ports: []dpuservicev1.DPUDeploymentPort{
			{ServiceInterface: &dpuservicev1.ServiceIfc{MatchLabels: platformPatchLabels}},
			{Service: &dpuservicev1.DPUDeploymentService{Name: hbnServiceName, InterfaceName: tenantHBNInterface}},
		}},
	}

	patchDPUDeployment(ctx, input, func(dpuDeployment *dpuservicev1.DPUDeployment) {
		dpuDeployment.Spec.Services[hbnServiceName] = dpuservicev1.DPUDeploymentServiceConfiguration{
			ServiceTemplate:      serviceTemplate.GetName(),
			ServiceConfiguration: serviceConfiguration.GetName(),
		}
		if dpuDeployment.Spec.ServiceChains == nil {
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{}
		}
		dpuDeployment.Spec.ServiceChains.Switches = append(dpuDeployment.Spec.ServiceChains.Switches, switches...)
	})
	tenantPlatformHBNAdded = true

	By("Wait for the DPUDeployment to become ready with HBN")
	Eventually(func(g Gomega) {
		dpuDeployment := &dpuservicev1.DPUDeployment{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: input.namespace, Name: dpuDeploymentName}, dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}, 20*time.Minute).Should(Succeed())
}

// removeHBNFromDPUDeployment takes the HBN service and its switches back out of the DPUDeployment, which
// is what makes it drop the dependent finalizer it holds on the HBN template and configuration.
func removeHBNFromDPUDeployment(ctx context.Context, input *systemTestInput) {
	By("Remove HBN from the DPUDeployment")
	patchDPUDeployment(ctx, input, func(dpuDeployment *dpuservicev1.DPUDeployment) {
		delete(dpuDeployment.Spec.Services, hbnServiceName)
		if dpuDeployment.Spec.ServiceChains == nil {
			return
		}
		dpuDeployment.Spec.ServiceChains.Switches = slices.DeleteFunc(dpuDeployment.Spec.ServiceChains.Switches,
			func(sw dpuservicev1.DPUDeploymentSwitch) bool {
				return slices.ContainsFunc(sw.Ports, func(port dpuservicev1.DPUDeploymentPort) bool {
					return port.Service != nil && port.Service.Name == hbnServiceName
				})
			})
	})
}

func patchDPUDeployment(ctx context.Context, input *systemTestInput, mutate func(*dpuservicev1.DPUDeployment)) {
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: input.namespace, Name: dpuDeploymentName}, dpuDeployment)).To(Succeed())
	original := dpuDeployment.DeepCopy()

	mutate(dpuDeployment)
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(original))).To(Succeed())
}

func prepareTenantNamespace(ctx context.Context, input *systemTestInput, serviceNamespace string) {
	By("Create test namespace: " + serviceNamespace)
	createTestNamespace(ctx, input.client, serviceNamespace)

	By("Copy image pull secret to namespace " + serviceNamespace)
	CopySecretToNamespace(ctx, input.client, dpfPullSecretName, dpfOperatorSystemNamespace, serviceNamespace, CleanupScope.It)
}

// newTenantDPUService runs the dummy service with the netutils image for its ping and iperf3 binaries.
func newTenantDPUService(input *systemTestInput, serviceNamespace, serviceID string) *dpuservicev1.DPUService {
	tenantService := utils.GenerateDPUObj(serviceID, serviceNamespace, input.dpuService.DeepCopy())
	tenantService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}

	values := map[string]any{
		"imagePullSecrets": []map[string]string{{"name": dpfPullSecretName}},
		"image":            map[string]string{"repository": netutilsImage},
	}
	rawValues, err := json.Marshal(values)
	Expect(err).NotTo(HaveOccurred())
	tenantService.Spec.HelmChart.Values.Raw = rawValues

	tenantService.Spec.ServiceID = ptr.To(serviceID)
	return tenantService
}

// newTenantDPUServiceOnNAD builds the DPUService for the DPUServiceNAD scenario. Without spec.interfaces
// nothing walks the NAD for this DPUService, so it has to carry both the IPAM arguments the
// pod-ipam-injector would inject and the SF the resource injection would add.
func newTenantDPUServiceOnNAD(input *systemTestInput, serviceNamespace, serviceID string) *dpuservicev1.DPUService {
	// No allocateDefaultGateway: that allocates the gateway address, which HBN's interface holds. The
	// Pod reaches the other DPU over the route the pool carries, not over a default route.
	networkAnnotation := fmt.Sprintf(
		`[{"name":%q,"interface":%q,"cni-args":{"poolNames":[%q],"poolType":"cidrpool"}}]`,
		tenantNADName, tenantPodInterface, tenantGatewayPool)

	tenantService := newTenantDPUService(input, serviceNamespace, serviceID)
	tenantService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
		Annotations: map[string]string{"k8s.v1.cni.cncf.io/networks": networkAnnotation},
		Resources:   corev1.ResourceList{"nvidia.com/bf_sf": resource.MustParse("1")},
	}
	return tenantService
}

func createTenantDPUServiceNAD(ctx context.Context, input *systemTestInput, serviceNamespace, bridge string) {
	nadTemplate := dpuservicev1.DPUServiceNAD{
		Spec: dpuservicev1.DPUServiceNADSpec{
			ResourceType: "sf",
			Bridge:       bridge,
			IPAM:         true,
		},
	}
	nad := utils.GenerateDPUObj(tenantNADName, serviceNamespace, &nadTemplate)
	Expect(input.client.Create(ctx, nad)).To(Succeed())
}

// selectDPUsForService labels the DPUDevices of the DPUs at dpuIndexes the way a tenant selecting those
// DPUs would, and returns the DPU cluster nodes that ended up carrying the label. The devices are sorted
// by name so an index means the same DPU across specs. The labels are removed when the spec finishes.
func selectDPUsForService(ctx context.Context, input *systemTestInput, selectorValue string, dpuIndexes ...int) []string {
	By(fmt.Sprintf("Select DPU(s) %v for the service by labeling their DPUDevices", dpuIndexes))
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(input.namespace))).To(Succeed())

	deviceNames := []string{}
	for _, dpu := range dpuList.Items {
		if dpu.Spec.DPUDeviceName == "" || slices.Contains(deviceNames, dpu.Spec.DPUDeviceName) {
			continue
		}
		deviceNames = append(deviceNames, dpu.Spec.DPUDeviceName)
	}
	slices.Sort(deviceNames)

	for _, index := range dpuIndexes {
		Expect(len(deviceNames)).To(BeNumerically(">", index), "not enough DPUs with a DPUDevice to select from")
		deviceName := deviceNames[index]
		patchDPUDeviceNodeLabels(ctx, input, deviceName, func(labels map[string]string) {
			labels[tenantSelectorKey] = selectorValue
		})
		DeferCleanup(patchDPUDeviceNodeLabels, ctx, input, deviceName, func(labels map[string]string) {
			delete(labels, tenantSelectorKey)
		})
	}

	var nodeNames []string
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes,
			client.MatchingLabels{tenantSelectorKey: selectorValue})).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(len(dpuIndexes)))
		nodeNames = nil
		for _, node := range nodes.Items {
			nodeNames = append(nodeNames, node.Name)
		}
	}, 10*time.Minute).Should(Succeed(), "DPUDevice node labels did not reach the DPU cluster nodes")

	return nodeNames
}

func patchDPUDeviceNodeLabels(ctx context.Context, input *systemTestInput, deviceName string, mutate func(map[string]string)) {
	dpuDevice := &provisioningv1.DPUDevice{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: input.namespace, Name: deviceName}, dpuDevice)).To(Succeed())
	original := dpuDevice.DeepCopy()

	if dpuDevice.Spec.Cluster == nil {
		dpuDevice.Spec.Cluster = &provisioningv1.DPUDeviceClusterSpec{}
	}
	if dpuDevice.Spec.Cluster.NodeLabels == nil {
		dpuDevice.Spec.Cluster.NodeLabels = map[string]string{}
	}
	mutate(dpuDevice.Spec.Cluster.NodeLabels)
	Expect(input.client.Patch(ctx, dpuDevice, client.MergeFrom(original))).To(Succeed())
}

func nodeSelectorForService(selectorValue string) *corev1.NodeSelector {
	return &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      tenantSelectorKey,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{selectorValue},
					},
				},
			},
		},
	}
}

func labelSelectorForService(selectorValue string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{tenantSelectorKey: selectorValue}}
}

// verifyServicePodsOnNodes asserts the service runs on the selected nodes and nowhere else.
func verifyServicePodsOnNodes(ctx context.Context, serviceNamespace, serviceID string, expectedNodes []string) {
	By(fmt.Sprintf("Verify the Pods of %s run only on %v", serviceID, expectedNodes))
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(dpuClusterClient[0].List(ctx, pods,
			client.InNamespace(serviceNamespace),
			client.MatchingLabels{dpuservicev1.DPFServiceIDLabelKey: serviceID})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(len(expectedNodes)),
			"expected Pods labeled %s=%s in %s", dpuservicev1.DPFServiceIDLabelKey, serviceID, serviceNamespace)
		for _, pod := range pods.Items {
			g.Expect(expectedNodes).To(ContainElement(pod.Spec.NodeName))
		}
	}, 10*time.Minute).Should(Succeed())
}

// verifyTenantPodTraffic pings the gateway before running pod to pod, so a failure separates the tenant
// to HBN path this test is about from the HBN routing the plain HBN specs already cover.
func verifyTenantPodTraffic(ctx context.Context, serviceNamespace, serviceID string) {
	pod1, pod2 := get2DPUServicePods(ctx, serviceNamespace, serviceID)
	pod1IP := getPodIPForInterface(Default, pod1, tenantPodInterface)
	pod2IP := getPodIPForInterface(Default, pod2, tenantPodInterface)
	Expect(pod1IP).ToNot(BeEmpty())
	Expect(pod2IP).ToNot(BeEmpty())
	Expect(pod1.Spec.NodeName).ToNot(Equal(pod2.Spec.NodeName), "expected the two Pods to sit on different DPUs")

	By(fmt.Sprintf("Pinging the HBN gateway from %s (%s)", pod1.Name, pod1IP))
	netshoot.AssertPingSuccess(&dpuClusterRestClient[0], &dpuClusterRestConfig[0], serviceNamespace, pod1.Name, gatewayForPodIP(pod1IP))

	By(fmt.Sprintf("Pinging the HBN gateway from %s (%s)", pod2.Name, pod2IP))
	netshoot.AssertPingSuccess(&dpuClusterRestClient[0], &dpuClusterRestConfig[0], serviceNamespace, pod2.Name, gatewayForPodIP(pod2IP))

	By(fmt.Sprintf("Running traffic test from %s (%s) to %s (%s) through HBN", pod1.Name, pod1IP, pod2.Name, pod2IP))
	netshoot.RunTrafficTest(&dpuClusterRestClient[0], &dpuClusterRestConfig[0], serviceNamespace, pod1.Name, pod2.Name, pod2IP)
}

// verifyInterfacesShareSingleNodeServiceInterfaces asserts every interface lands as an entry of the one
// NodeServiceInterfaces object its node owns in the DPF operator namespace, whatever namespace the owning
// DPUServiceInterface lives in.
func verifyInterfacesShareSingleNodeServiceInterfaces(ctx context.Context, interfaceConfigs []dpuservice.TestDPUServiceInterfaceConfig) {
	// Entry names are "<namespace>_<name>" of the owning ServiceInterfaceSet.
	expectedEntries := make([]string, 0, len(interfaceConfigs))
	for _, interfaceConfig := range interfaceConfigs {
		expectedEntries = append(expectedEntries, interfaceConfig.Namespace+"_"+interfaceConfig.Name)
	}

	By(fmt.Sprintf("Verify %v are entries of a single NodeServiceInterfaces object per node", expectedEntries))
	Eventually(func(g Gomega) {
		nsiList := &dpuservicev1.NodeServiceInterfacesList{}
		g.Expect(dpuClusterClient[0].List(ctx, nsiList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		sfcByNode := map[string]*dpuservicev1.NodeServiceInterfaces{}
		for i := range nsiList.Items {
			nsi := &nsiList.Items[i]
			if nsi.Spec.Type != dpuservicev1.NSITypeSFC {
				continue
			}
			_, seen := sfcByNode[nsi.Spec.Node]
			g.Expect(seen).To(BeFalse(), "node %s has more than one sfc NodeServiceInterfaces object, found %s twice",
				nsi.Spec.Node, nsi.Name)
			sfcByNode[nsi.Spec.Node] = nsi
		}
		g.Expect(sfcByNode).ToNot(BeEmpty(), "no sfc NodeServiceInterfaces object found in %s", dpfOperatorSystemNamespace)

		for node, nsi := range sfcByNode {
			entryNames := make([]string, 0, len(nsi.Spec.Interfaces))
			for _, entry := range nsi.Spec.Interfaces {
				entryNames = append(entryNames, entry.Name)
			}
			for _, expected := range expectedEntries {
				g.Expect(entryNames).To(ContainElement(expected),
					"node %s object %s is missing entry %s, has %v", node, nsi.Name, expected, entryNames)
			}
		}
	}, 10*time.Minute).Should(Succeed())
}

func requireTwoDPUs(input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		Skip("Skip test as there are not exactly 2 nodes")
	}
}

// nthTenantSubnet returns the nth tenantPrefixSize-sized subnet of tenantSubnet, which is the block the pool
// assigns to one DPU.
func nthTenantSubnet(index int) string {
	network, err := netip.ParsePrefix(tenantSubnet)
	Expect(err).NotTo(HaveOccurred(), "tenantSubnet %q is not a prefix", tenantSubnet)

	first := addressAt(network.Addr(), uint32(index)<<(32-tenantPrefixSize))
	return netip.PrefixFrom(first, tenantPrefixSize).String()
}

// gatewayForPodIP returns the address the pool places HBN on for the DPU the Pod runs on: index
// tenantGatewayIndex of the tenantPrefixSize-sized subnet the Pod address falls in.
func gatewayForPodIP(podIP string) string {
	address, err := netip.ParseAddr(podIP)
	Expect(err).NotTo(HaveOccurred(), "pod IP %q is not an address", podIP)
	Expect(address.Is4()).To(BeTrue(), "pod IP %q is not IPv4", podIP)

	subnet, err := address.Prefix(tenantPrefixSize)
	Expect(err).NotTo(HaveOccurred(), "pod IP %q has no /%d prefix", podIP, tenantPrefixSize)

	return addressAt(subnet.Addr(), tenantGatewayIndex).String()
}

// addressAt returns the IPv4 address sitting offset addresses above base.
func addressAt(base netip.Addr, offset uint32) netip.Addr {
	octets := base.As4()
	value := uint32(octets[0])<<24 | uint32(octets[1])<<16 | uint32(octets[2])<<8 | uint32(octets[3])
	value += offset
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
