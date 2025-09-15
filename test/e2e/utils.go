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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/api/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	dpuClusterClient       client.Client
	testRESTClient         *rest.RESTClient
	metricsURI             string
	afterAllCleanupLabels  = map[string]string{"dpf-operator-e2e-test-cleanup": "true"}
	afterEachCleanupLabels = map[string]string{"between-tests-cleanup": "true"}
	// helmRegistry holds the Helm registry in which the artifacts used in e2e are pushed
	helmRegistry = ""
	// tag holds the tag which the artifacts used in e2e are using
	tag = ""
	// bfbImageURL can be used to override the default BFB image URL used in the tests.
	bfbImageURL = ""
	// hbnImageURL can be used to override the default HBN image URL used in the tests.
	hbnImageURL = ""
	// ngcAPIKey can be used to create a secret to be able to pull images from NGC, this secret can be used by DPUservices and should not be used for core components.
	ngcAPIKey = ""
	// externalTestCommands can be used to run external tests as part of our test suite. Tests will be skipped, if the variable is empty.
	externalTestCommands []string
	// Labels and resources targeted for cleanup before running our e2e tests.
	// This cleanup is typically handled by cleanupObjs, but if an e2e test fails, the standard cleanup may not be executed.
	resourcesToDelete = []client.ObjectList{
		&dpuservicev1.DPUDeploymentList{},
		&dpuservicev1.DPUServiceCredentialRequestList{},
		&dpuservicev1.DPUServiceList{},
		&dpuservicev1.DPUServiceConfigurationList{},
		&dpuservicev1.DPUServiceTemplateList{},
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
		&vpcv1.DPUVirtualNetworkList{},
		&vpcv1.DPUVPCList{},
		&vpcv1.IsolationClassList{},
	}
	// systemPodsToVerify is a list of pod name patterns that should be verified in the DPU cluster
	systemPodsToVerify = []string{
		"kube-proxy",
		"kube-flannel-ds",
		"coredns",
	}
)

const (
	configName                 = "dpfoperatorconfig"
	dpfOperatorSystemNamespace = "dpf-operator-system"
	argoCDInstanceLabel        = "argocd.argoproj.io/instance"
	// ngcPullSecretName is the name of the secret used to pull images from NGC
	ngcPullSecretName = "ngc-pull-secret"
	// scaleLabel is used to mark the tests related to the Scale test suite
	scaleLabel = "SCALE"
	// dpfSystemLabel is used to mark the tests related to the DPFSystem test suite, ex: e2e, provisioning-e2e
	dpfSystemLabel = "DPFSystem"
	// sdnLabel is used to mark the tests related to the SDN test suite, ex: sdn
	sdnLabel = "SDN"
	// snapLabel is used to mark the tests related to the SNAP test suite, ex: snap
	snapLabel = "SNAP"
	// provisioningLabel is used to mark the tests related to the Provisioning test suite, ex: provisioning
	provisioningLabel = "Provisioning"
	// requiresNodesLabel is used to mark the tests that has steps require at least 1 DPU to be provisioned to get the correct signal. Such tests should still pass without any DPU.
	requiresNodesLabel = "RequiresNodes"
	// l2ConnectivityLabel is used to mark the tests that require L2 connectivity between nodes
	l2ConnectivityLabel = "L2Connectivity"
	// dpfUpgradeTestLabel is used to mark the tests related to the upgrade test suite.
	dpfUpgradeTestLabel = "DPFUpgrade"
	// dpfUpgradeTestLabel is used to mark the tests related to the upgrade test suite.
	dpfUpgradeValidationTestLabel = "DPFUpgradeValidation"
	// externalTestLabel is used run DPF precondition (DPFOperator, DPFComponents and DPUProvisioning). Run external test as a part of the suite.
	externalTestLabel = "ExternalTest"
	// ovnkPrimaryLabel is used to mark the tests that need to run with OVNK as primary CNI.
	ovnkPrimaryLabel = "OVNKPrimary"
	// dpfVPCTestLabel is used to mark the tests related to the VPC OVN test suite.
	dpfVPCTestLabel = "DPFVPCOVN"
)

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

func generateDPUObj[T client.Object](name, ns string, obj T, customLabels ...map[string]string) T {
	obj.SetName(name)
	obj.SetNamespace(ns)
	cleanupLabels := afterEachCleanupLabels
	if len(customLabels) > 0 {
		cleanupLabels = customLabels[0]
	}
	for k, v := range afterAllCleanupLabels {
		cleanupLabels[k] = v
	}
	obj.SetLabels(cleanupLabels)
	return obj
}

func createTestNamespace(ctx context.Context, testClient client.Client, namespace string) {
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	cleanupLabels := afterEachCleanupLabels
	for k, v := range afterAllCleanupLabels {
		cleanupLabels[k] = v
	}
	testNS.SetLabels(cleanupLabels)
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())
}

// getTwoNodes returns two worker nodes using the client provided as input
func getTwoWorkerNodeNames(ctx context.Context, c client.Client) (string, string) {
	nodes := &corev1.NodeList{}
	Expect(c.List(ctx, nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))).To(Succeed())
	Expect(len(nodes.Items)).To(BeNumerically(">=", 2), "Not enough worker nodes in the cluster")
	return nodes.Items[0].Name, nodes.Items[1].Name
}

// getTwoNodes returns two random nodes using the client provided as input
func getTwoNodes(ctx context.Context, c client.Client) (corev1.Node, corev1.Node) {
	nodes := &corev1.NodeList{}
	Expect(c.List(ctx, nodes)).To(Succeed())
	Expect(len(nodes.Items)).To(BeNumerically(">=", 2), "Not enough nodes in the cluster")
	return nodes.Items[0], nodes.Items[1]
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

// getDPUClusterNodes returns all the nodes in the DPU cluster
func getDPUClusterNodes(ctx context.Context, dpuClusterClient client.Client) []corev1.Node {
	nodes := &corev1.NodeList{}
	Expect(dpuClusterClient.List(ctx, nodes)).To(Succeed())
	return nodes.Items
}
