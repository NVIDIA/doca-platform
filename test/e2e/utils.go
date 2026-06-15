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
	"fmt"
	"maps"
	"net"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Test priorities are defined below. these are used for specific test specs to ensure they are run in the correct order.
// for tests which do not specify a priority, ginkgo will will assign a default priority of 0.
// test specs with a higher priority will run first.
// for more information, see: https://onsi.github.io/ginkgo/#prioritizing-specs
// in most cases, assigning a specific test priority for a test should not be needed.
const (
	// CoreTestPriority is the test priority for the "DPF System tests - Core" test suite.
	CoreTestPriority = 101
	// SDNTestPriority is the test priority for the "DPF System tests - SDN" test suite.
	SDNTestPriority = 100

	// kubeStateMetricsPort is the port used by kube-state-metrics across host and DPU clusters.
	kubeStateMetricsPort = 8080
	// testMTUValue is the MTU value used across e2e tests to trigger configuration changes.
	testMTUValue = 1300
	// defaultAPIServerPort is the default Kubernetes API server port used in performance tests.
	defaultAPIServerPort = 6443
	// performanceMTU is the MTU configured for both the control plane and high-speed networks in performance tests.
	performanceMTU = 9000

	// provisioningTimeout is the Eventually budget for provisioning-side waits in
	// the e2e suite (DPUs being installed and joining the DPU cluster as K8s
	// Nodes). Sized to absorb a first-install BFB run that includes a full BMC +
	// CEC + NIC firmware update cycle plus host power-cycle, which can take
	// ~45-55 minutes per DPU.
	provisioningTimeout = 60 * time.Minute

	// dpuDeploymentReadyTimeout is the Eventually budget for waits that gate on
	// DPUDeployment.Status.Ready=True when DPU provisioning has not been awaited
	// separately upstream. Such waits must absorb the full provisioning chain
	// plus the dpuservice / ArgoCD / ServiceChain layer settling on top, so this
	// is intentionally larger than provisioningTimeout.
	dpuDeploymentReadyTimeout = 75 * time.Minute
)

// CleanupScope is an alias for cleanup.CleanupLabels for ease of use
var CleanupScope = cleanup.CleanupLabels

// TestDomain defines test label domains for categorizing e2e tests
type TestDomain struct {
	DPFSystem            string // DPFSystem test suite (e2e, provisioning-e2e)
	Scale                string // Scale test suite
	SDN                  string // SDN test suite
	SNAP                 string // SNAP test suite
	Provisioning         string // Provisioning test suite
	RequiresNodes        string // Tests that require at least 1 DPU to be provisioned
	L2Connectivity       string // Tests that require L2 connectivity between nodes
	DPFUpgrade           string // Upgrade test suite
	DPFUpgradeValidation string // Upgrade validation test suite
	ExternalTest         string // External test scripts (DPF precondition setup)
	TCP                  string // TCP external performance tests
	UDP                  string // UDP external performance tests
	OVNKPrimary          string // Tests that need OVNK as primary CNI
	OVNKHBN              string // Tests that need OVNK as primary CNI with HBN deployed alongside
	DPFVPCOVN            string // VPC OVN test suite
	Weave                string // Weave test suite
	MultiDPUCluster      string // Multi DPUCluster setup tests
	ZeroTrust            string // Zero Trust mode in DPFOperatorConfig on the BeforeSuite stage
	Observability        string // Observability test suite
	ImagePullSecretsSync string // ImagePullSecrets sync/cleanup validation (opt out in CI via !ImagePullSecretsSync)
	Performance          string // Performance test suite - applies MTU 9000 and extended DMS timeout
}

// Domain is the global instance of test label domains
var Domain = TestDomain{
	DPFSystem:            "DPFSystem",
	Scale:                "SCALE",
	SDN:                  "SDN",
	SNAP:                 "SNAP",
	Provisioning:         "Provisioning",
	RequiresNodes:        "RequiresNodes",
	L2Connectivity:       "L2Connectivity",
	DPFUpgrade:           "DPFUpgrade",
	DPFUpgradeValidation: "DPFUpgradeValidation",
	ExternalTest:         "ExternalTest",
	TCP:                  "TCP",
	UDP:                  "UDP",
	OVNKPrimary:          "OVNKPrimary",
	OVNKHBN:              "OVNKHBN",
	DPFVPCOVN:            "DPFVPCOVN",
	Weave:                "Weave",
	MultiDPUCluster:      "MultiDPUCluster",
	ZeroTrust:            "ZeroTrust",
	Observability:        "Observability",
	ImagePullSecretsSync: "ImagePullSecretsSync",
	Performance:          "Performance",
}

var (
	dpuClusterClient             []client.Client
	dpuClusterRestConfig         []*rest.Config
	dpuClusterRestClient         []*rest.RESTClient
	dpuClusterClientsInitialized bool // tracks if getDPUClusterClients was called (must only be called once)
	hostClusterRESTClient        *rest.RESTClient
	metricsURI                   string
	// helmRegistry holds the Helm registry in which the artifacts used in e2e are pushed
	helmRegistry = ""
	// dockerIORegistry is a DockerHub mirror registry used to pull mirrored images to avoid rate-limiting.
	dockerIORegistry = ""
	// tag holds the tag which the artifacts used in e2e are using
	tag = ""
	// bfbImageURL can be used to override the default BFB image URL used in the tests.
	// Required for BF3.
	bfbImageURL = ""
	// bfsOsIsoURL can be used to override the default BlueFieldSoftware OS ISO URL used in the tests.
	// Required for BF4.
	bfsOsIsoURL = ""
	// bfsPldmFwBundleURL can be used to override the default BlueFieldSoftware PLDM FW bundle URL used in the tests.
	// Required for BF4.
	bfsPldmFwBundleURL = ""
	// hbnImageURL can be used to override the default HBN image URL used in the tests.
	hbnImageURL = ""
	// netutilsImage is the image name of the netutils image produced by the release associated with the e2e tests. This
	// image is used for testing traffic. The value does not contain the tag.
	netutilsImage = ""
	// bmcPassword is the BMC root password used by the in-cluster reboot script Job (ZeroTrust only).
	bmcPassword = ""
	// bmcInventoryPath is the filesystem path to the lab DPU-serial -> BMC IP inventory YAML (ZeroTrust only).
	bmcInventoryPath = ""
	// ngcAPIKey can be used to create a secret to be able to pull images from NGC, this secret can be used by DPUservices and should not be used for core components.
	ngcAPIKey = ""
	// dpuClusterName optionally overrides the DPUCluster name (e.g. when created externally with a non-default name).
	dpuClusterName = ""
	// dpuClusterNamespace optionally overrides the DPUCluster namespace.
	dpuClusterNamespace = ""
	// dpuClusterInterface can be used to override the interface specified in DPUCluster YAML files.
	// This is useful when running e2e tests on different hardware setups where the interface name differs.
	dpuClusterInterface = ""
	// targetClusterAPIServerHost is the host cluster API server address (hostname or IP) used on physical
	// performance setups where the VIP differs from the control plane node's InternalIP.
	targetClusterAPIServerHost = ""
	// prereqsNamespace can be used to override the namespace where the prerequisites are deployed.
	// This is useful to test scenarios where the prerequisites are deployed in a different namespace than the known default.
	prereqsNamespace = ""
	// Labels and resources targeted for cleanup before running our e2e tests.
	// This cleanup is typically handled by cleanupObjs, but if an e2e test fails, the standard cleanup may not be executed.
	// Note: order matters as some object deletion depends on controllers that may be deployed via dpuservices/dpudeployments
	resourcesToDelete = []client.ObjectList{
		&dpuservicev1.DPUDeploymentList{},
		&dpuservicev1.DPUServiceCredentialRequestList{},
		&dpuservicev1.DPUServiceList{},
		&dpuservicev1.DPUServiceConfigurationList{},
		&dpuservicev1.DPUServiceTemplateList{},
		&dpuservicev1.DPUServiceNADList{},
		&provisioningv1.DPUSetList{},
		&provisioningv1.DPUList{},
		&provisioningv1.BFBList{},
		&provisioningv1.DPUClusterList{},
		&dpuservicev1.DPUServiceIPAMList{},
		&dpuservicev1.DPUServiceChainList{},
		&dpuservicev1.DPUServiceInterfaceList{},
		&kamajiv1.TenantControlPlaneList{},
		&operatorv1.DPFOperatorConfigList{},
		&appsv1.DeploymentList{},
		&appsv1.DaemonSetList{},
		&corev1.PersistentVolumeClaimList{},
		&corev1.NamespaceList{},
		&corev1.NodeList{},
		&corev1.ServiceList{},
		&corev1.PodList{},
		&corev1.SecretList{},
		&corev1.ConfigMapList{},
		&vpcv1.DPUVirtualNetworkList{},
		&vpcv1.DPUVPCList{},
		&vpcv1.IsolationClassList{},
		&netattdefv1.NetworkAttachmentDefinitionList{},
		&noderesourcesv1.NodeSRIOVDevicePluginConfigList{},
	}
	// systemPodsToVerify is a list of pod name patterns that should be verified in the DPU cluster
	systemPodsToVerify = []string{
		"kube-proxy",
		"kube-flannel",
		"cni-installer",
		"nvidia-k8s-ipam",
		"ovs-cni",
		"sfc-controller",
		"sriov-device-plugin",
		"kube-multus",
	}
)

const (
	configName                 = "dpfoperatorconfig"
	dpfOperatorSystemNamespace = "dpf-operator-system"
	argoCDTrackingIDAnnotation = "argocd.argoproj.io/tracking-id"
	// ngcPullSecretName is the name of the secret used to pull images from NGC
	ngcPullSecretName = "ngc-pull-secret"
	// dpfPullSecretName is the name of the secret that is set in hack/scripts/create-artefact-secrets.sh
	dpfPullSecretName = "dpf-pull-secret"
)

// EventuallyCheckReadyStatusCondition waits until obj has a Ready condition with Status True and
// ObservedGeneration equal to the object's current Generation.
func EventuallyCheckReadyStatusCondition(ctx context.Context, c client.Client, obj client.Object, timeout time.Duration) {
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)).To(Succeed())
		g.Expect(obj).To(
			HaveField("Status.Conditions", ContainElement(And(
				HaveField("Type", Equal("Ready")),
				HaveField("Status", Equal(metav1.ConditionTrue)),
				HaveField("ObservedGeneration", Equal(obj.GetGeneration())),
			))),
			fmt.Sprintf("Object %T %s/%s did not reach Ready condition with matching generation", obj, obj.GetNamespace(), obj.GetName()),
		)
	}).WithTimeout(timeout).Should(Succeed())
}

// ByTracker tracks Ginkgo By() statements to ensure they are only printed once
type ByTracker struct {
	loggedObjects map[string]bool
}

// NewByTracker creates a new ByTracker instance
func NewByTracker() *ByTracker {
	return &ByTracker{
		loggedObjects: make(map[string]bool),
	}
}

// By ensures a By() statement is only printed once for a given key
func (b *ByTracker) By(key string, format string, args ...interface{}) {
	if !b.loggedObjects[key] {
		By(fmt.Sprintf(format, args...))
		b.loggedObjects[key] = true
	}
}

func createTestNamespace(ctx context.Context, testClient client.Client, namespace string) {
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	cleanupLabels := make(map[string]string)
	maps.Copy(cleanupLabels, CleanupScope.It)
	maps.Copy(cleanupLabels, CleanupScope.Suite)
	testNS.SetLabels(cleanupLabels)
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())
}

// CopySecretToNamespace copies a secret from one namespace to another
// If the source secret doesn't exist, does nothing (nothing to copy)
// Always set's the label "dpu.nvidia.com/image-pull-secret" to "" in the target namespace
// to ensure reconciliation in the DPU cluster
func CopySecretToNamespace(ctx context.Context, c client.Client, secretName string, sourceNamespace, targetNamespace string, targetNamespaceLabels map[string]string) {
	// Get source secret
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Name: secretName, Namespace: sourceNamespace}, secret)
	if err != nil {
		// Secret doesn't exist, nothing to copy
		Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
		return
	}

	mergedLabels := make(map[string]string)
	maps.Copy(mergedLabels, secret.Labels)
	maps.Copy(mergedLabels, targetNamespaceLabels)
	// Make sure the secret is reconciled in the DPU cluster
	mergedLabels[dpuservicev1.DPFImagePullSecretLabelKey] = ""

	secret.ObjectMeta = metav1.ObjectMeta{
		Name:      secretName,
		Namespace: targetNamespace,
		Labels:    mergedLabels,
	}

	Expect(client.IgnoreAlreadyExists(c.Create(ctx, secret))).To(Succeed())

	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKey{Name: secretName, Namespace: targetNamespace}, secret)).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())
}

// getTwoWorkerNodeNames returns the names of two worker nodes using the client provided as input.
func getTwoWorkerNodeNames(ctx context.Context, c client.Client) (string, string) {
	nodes := &corev1.NodeList{}
	Expect(c.List(ctx, nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))).To(Succeed())
	Expect(len(nodes.Items)).To(BeNumerically(">=", 2), "Not enough worker nodes in the cluster")
	return nodes.Items[0].Name, nodes.Items[1].Name // FIXME: Refactor to return two nodes in order instead of names
}

// getClusterControlPlaneIP returns the internal IP of the control plane node in the cluster
func getClusterControlPlaneIP(ctx context.Context, testClient client.Client) string {
	nodes := &corev1.NodeList{}
	Expect(testClient.List(ctx, nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/control-plane": ""}))).To(Succeed())
	Expect(nodes.Items).ToNot(BeEmpty(), "Not enough control plane nodes in the cluster")

	for _, addr := range nodes.Items[0].Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	Fail("No internal IP found for control plane node")
	return ""
}

// GetNodeInternalIP returns the internal IP of the node with the given name.
func GetNodeInternalIP(ctx context.Context, c client.Client, nodeName string) net.IP {
	node := &corev1.Node{}
	Expect(c.Get(ctx, client.ObjectKey{Name: nodeName}, node)).To(Succeed())
	for _, addr := range node.Status.Addresses {
		if addr.Type != corev1.NodeInternalIP {
			continue
		}
		ip := net.ParseIP(addr.Address)
		if ip != nil {
			return ip
		}
	}
	Fail(fmt.Sprintf("No internal IP found for node %s", nodeName))
	return nil
}

// getDPUClusterNodes returns all the nodes in the DPU cluster
func getDPUClusterNodes(ctx context.Context, dpuClusterClient client.Client) []corev1.Node {
	nodes := &corev1.NodeList{}
	Expect(dpuClusterClient.List(ctx, nodes)).To(Succeed())
	return nodes.Items
}

// getDPUNodesInOrder returns the two DPU cluster nodes ordered so that the first
// matches the first host worker (provisioningv1.DPUNodeNameLabel). Requires exactly two DPU nodes.
func getDPUNodesInOrder(ctx context.Context, hostClient, dpuClusterClient client.Client) (corev1.Node, corev1.Node) {
	worker1, worker2 := getTwoWorkerNodeNames(ctx, hostClient)
	dpuNodes := getDPUClusterNodes(ctx, dpuClusterClient)
	Expect(dpuNodes).To(HaveLen(2))
	dpuNode0Worker := dpuNodes[0].Labels[provisioningv1.DPUNodeNameLabel]
	dpuNode1Worker := dpuNodes[1].Labels[provisioningv1.DPUNodeNameLabel]
	if dpuNode0Worker == worker1 && dpuNode1Worker == worker2 {
		return dpuNodes[0], dpuNodes[1]
	}
	return dpuNodes[1], dpuNodes[0]
}

// isGinkgoLabel returns if a label is passed while running ginkgo and is not excluded
func isGinkgoLabelApplied(ginkgoLabel string) bool {
	return strings.Contains(GinkgoLabelFilter(), ginkgoLabel) && !strings.Contains(GinkgoLabelFilter(), "!"+ginkgoLabel)
}

// VerifyPerformancePodToPodSameNode verifies performance between pods on the same node
func VerifyPerformancePodToPodSameNode(ctx context.Context, input *systemTestInput, namespacePrefix string) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	hostNamespace := namespacePrefix + "-same-node"
	createTestNamespace(ctx, input.client, hostNamespace)

	By("Creating test pods")
	pod1Config, pod2Config := getPodSameNodeConfigs(ctx, input, hostNamespace)
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("Get pod2 IP")
	pod2IP := netshoot.GetPodIP(ctx, input.client, hostNamespace, pod2Config.Name)

	By("Running traffic test between pods")
	netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2IP)
}

// VerifyPerformancePodToPodDifferentNode verifies performance between pods on different nodes
func VerifyPerformancePodToPodDifferentNode(ctx context.Context, input *systemTestInput, namespacePrefix string) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	hostNamespace := namespacePrefix + "-different-node"
	createTestNamespace(ctx, input.client, hostNamespace)

	By("Creating test pods")
	pod1Config, pod2Config := getPodDifferentNodeConfigs(ctx, input, hostNamespace)
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("Get pod2 IP")
	pod2IP := netshoot.GetPodIP(ctx, input.client, hostNamespace, pod2Config.Name)

	By("Running traffic test between pods")
	netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2IP)
}

// getPodDifferentNodeConfigs returns two pod configs for different nodes
func getPodDifferentNodeConfigs(ctx context.Context, input *systemTestInput, namespace string) (netshoot.TestPodConfig, netshoot.TestPodConfig) {
	workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.client)

	pod1Config := netshoot.TestPodConfig{
		Name:      "pod1",
		Namespace: namespace,
		NodeName:  workerNode1,
	}
	pod2Config := netshoot.TestPodConfig{
		Name:      "pod2",
		Namespace: namespace,
		NodeName:  workerNode2,
	}

	return pod1Config, pod2Config
}

// getPodSameNodeConfigs returns two pod configs for the same node
func getPodSameNodeConfigs(ctx context.Context, input *systemTestInput, namespace string) (netshoot.TestPodConfig, netshoot.TestPodConfig) {
	workerNode1, _ := getTwoWorkerNodeNames(ctx, input.client)

	pod1Config := netshoot.TestPodConfig{
		Name:      "pod1",
		Namespace: namespace,
		NodeName:  workerNode1,
	}
	pod2Config := netshoot.TestPodConfig{
		Name:      "pod2",
		Namespace: namespace,
		NodeName:  workerNode1,
	}

	return pod1Config, pod2Config
}

// GetServiceIDForDPUDeploymentService retrieves the ServiceID for the named service within a DPUDeployment.
func GetServiceIDForDPUDeploymentService(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment, serviceName string) string {
	var serviceID string
	Eventually(func(g Gomega) {
		dpuServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(c.List(ctx, dpuServiceList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				dpuservicev1.ParentDPUDeploymentNameLabel:            fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
				dpuservicev1.ServiceReferenceInDPUDeploymentLabelKey: serviceName,
			})).To(Succeed())
		g.Expect(dpuServiceList.Items).To(HaveLen(1))
		g.Expect(dpuServiceList.Items[0].Status.ServiceID).ToNot(BeEmpty())
		serviceID = dpuServiceList.Items[0].Status.ServiceID
	}).WithTimeout(30 * time.Second).Should(Succeed())
	return serviceID
}
