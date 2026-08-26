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
	"net/netip"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var dpuServiceIPAMNamespace = dpfOperatorSystemNamespace

func ValidateDPUServiceIPAMCreationInvalid(ctx context.Context, input *systemTestInput) {
	By("Creating the invalid DPUServiceIPAM CR")
	dpuServiceIPAMNamespace = dpfOperatorSystemNamespace
	dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-name",
			Namespace: dpuServiceIPAMNamespace,
		},
	}
	dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
	dpuServiceIPAM.SetLabels(CleanupScope.It)
	err := input.client.Create(ctx, dpuServiceIPAM)
	Expect(err).To(HaveOccurred())
	fmt.Printf("Error creating the DPUServiceIPAM CR: %v\n", err)
	// Two admission paths reject this object, and the OCP reuse suite runs against a
	// pinned 26.4 release while this suite comes from main. The CRD's CEL rule rejects
	// with Invalid, the 26.4 webhook with BadRequest.
	Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue(),
		"expected an admission rejection, got: %v", err)
	Expect(err.Error()).To(Or(
		ContainSubstring("exactly one of ipv4Network, ipv4Subnet, network, or subnet must be specified"),
		ContainSubstring("either ipv4Subnet or ipv4Network must be specified"),
	))
}

func ValidateDPUServiceIPAMCreationSubnetForIPv4AndIPv6(ctx context.Context, input *systemTestInput) {
	testCases := []struct {
		name    string
		subnet  string
		gateway string
	}{
		{name: "ipv4-subnet", subnet: "192.0.2.0/24", gateway: "192.0.2.1"},
		{name: "ipv6-subnet", subnet: "2001:db8:3::/120", gateway: "2001:db8:3::1"},
	}
	poolLabels := map[string]string{"svc.dpu.nvidia.com/pool": "ipv4-and-ipv6-subnet"}
	clusterSelector := firstDPUClusterSelector(input)

	By("Creating IPv4 and IPv6 subnet DPUServiceIPAMs")
	for _, testCase := range testCases {
		dpuServiceIPAM := newSubnetDPUServiceIPAM(
			testCase.name,
			dpuServiceIPAMNamespace,
			poolLabels,
			testCase.subnet,
			testCase.gateway,
			clusterSelector,
		)
		Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())
	}

	By("Checking the rendered IPv4 and IPv6 IPPool specs")
	Eventually(func(g Gomega) {
		for _, testCase := range testCases {
			pool := &nvipamv1.IPPool{}
			g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
				Namespace: dpuServiceIPAMNamespace,
				Name:      testCase.name,
			}, pool)).To(Succeed())
			g.Expect(pool.Spec.Subnet).To(Equal(testCase.subnet))
			g.Expect(pool.Spec.Gateway).To(Equal(testCase.gateway))
			g.Expect(pool.Spec.PerNodeBlockSize).To(Equal(8))
			g.Expect(pool.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/pool", "ipv4-and-ipv6-subnet"))
			g.Expect(pool.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-name", testCase.name))
			g.Expect(pool.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-namespace", dpuServiceIPAMNamespace))
		}
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMCreationNetworkForIPv4AndIPv6(ctx context.Context, input *systemTestInput) {
	testCases := []struct {
		name       string
		network    string
		prefixSize int32
	}{
		{name: "ipv4-network", network: "198.51.100.0/24", prefixSize: 28},
		{name: "ipv6-network", network: "2001:db8:2::/120", prefixSize: 124},
	}
	poolLabels := map[string]string{"svc.dpu.nvidia.com/pool": "ipv4-and-ipv6-network"}
	clusterSelector := firstDPUClusterSelector(input)

	By("Creating IPv4 and IPv6 network DPUServiceIPAMs")
	for _, testCase := range testCases {
		dpuServiceIPAM := newNetworkDPUServiceIPAM(
			testCase.name,
			dpuServiceIPAMNamespace,
			poolLabels,
			testCase.network,
			testCase.prefixSize,
			clusterSelector,
		)
		Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())
	}

	By("Checking the rendered IPv4 and IPv6 CIDRPool specs")
	Eventually(func(g Gomega) {
		for _, testCase := range testCases {
			pool := &nvipamv1.CIDRPool{}
			g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
				Namespace: dpuServiceIPAMNamespace,
				Name:      testCase.name,
			}, pool)).To(Succeed())
			g.Expect(pool.Spec.CIDR).To(Equal(testCase.network))
			g.Expect(pool.Spec.GatewayIndex).To(Equal(ptr.To[int32](1)))
			g.Expect(pool.Spec.PerNodeNetworkPrefix).To(Equal(testCase.prefixSize))
			g.Expect(pool.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/pool", "ipv4-and-ipv6-network"))
			g.Expect(pool.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-name", testCase.name))
			g.Expect(pool.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-namespace", dpuServiceIPAMNamespace))
		}
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateLegacyDPUServiceIPAMCreationSubnetSplit(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithIPPoolName := "legacy-ipv4-subnet"
	By("Creating a DPUServiceIPAM using the legacy ipv4Subnet field")
	dpuServiceIPAM := buildTestDPUServiceIPAM(dpuServiceIPAMWithIPPoolName, input.ipPoolDPUServiceIPAM)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Checking that the legacy field is rendered as an NVIPAM IPPool")
	Eventually(func(g Gomega) {
		ipPools := &nvipamv1.IPPoolList{}
		g.Expect(dpuClusterClient[0].List(ctx, ipPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(ipPools.Items).To(HaveLen(1))

		// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMMetrics(ctx context.Context, input *systemTestInput) {
	skipMetricNamesInOCPReuse()

	By("Creating the DPUServiceIPAM CR")
	dpuServiceIPAM := buildTestDPUServiceIPAM("switched-application-metrics", input.ipPoolDPUServiceIPAM)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Verify DPUServiceIPAM metrics in host cluster KSM")
	expectedHostMetricsNames := map[string][]string{
		"dpf_dpuserviceipam": {"created", "info", "status_conditions", "status_condition_last_transition_time"}, //  "network_info", "subnet_info" missed
	}
	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedHostMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())

	By("Waiting for DPU cluster kube-state-metrics to be ready")
	VerifyClusterPods(ctx, input.client, []string{"in-cluster-kube-state-metrics"})

	By("Wait for IPPool to be created in DPU clusters")
	Eventually(func(g Gomega) {
		ipPools := &nvipamv1.IPPoolList{}
		g.Expect(dpuClusterClient[0].List(ctx, ipPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(ipPools.Items).ToNot(BeEmpty())
	}).WithTimeout(180 * time.Second).Should(Succeed())

	By("Verify IPPool metrics in DPU cluster KSM")
	expectedDPUMetricsNames := map[string][]string{
		"dpf_ippool": {"created", "info", "allocation_info"},
	}
	Eventually(func(g Gomega) {
		g.Expect(input.dpuClusters).ToNot(BeEmpty(), "No DPUClusters found in test input")
		dpuKSMMetricsURI, err := metrics.GetKSMMetricsURIForDPUCluster(ctx, input.client, input.dpuClusters[0], dpfOperatorSystemNamespace, kubeStateMetricsPort, "/metrics")
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get KSM metrics URI for DPUCluster")
		g.Expect(dpuKSMMetricsURI).NotTo(BeEmpty())

		// Use hostClusterRESTClient because in-cluster KSM runs on the management cluster
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, dpuKSMMetricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedDPUMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(30 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMMetricsDeletion(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithIPPoolName := "switched-application-delete"
	if input.cleanupFlags.SkipCleanup {
		Skip("Skip cleanup resources")
	}

	By("Creating the DPUServiceIPAM CR")
	dpuServiceIPAM := buildTestDPUServiceIPAM(dpuServiceIPAMWithIPPoolName, input.ipPoolDPUServiceIPAM)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	// Wait for the controller to set its finalizer before deleting, otherwise a
	// Delete racing with the finalizer patch can remove the object before reconcileDelete
	// runs and leaves the dpu-cluster object orphaned. See https://github.com/kubernetes/kubernetes/issues/77988
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceIPAM), dpuServiceIPAM)).To(Succeed())
		g.Expect(dpuServiceIPAM.Finalizers).To(ContainElement(dpuservicev1.DPUServiceIPAMFinalizer))
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Deleting the DPUServiceIPAM")
	Expect(input.client.Delete(ctx, dpuServiceIPAM)).To(Succeed())

	By("Checking that NVIPAM IPPool CR is deleted in each DPU cluster")
	Eventually(func(g Gomega) {
		ipPools := &nvipamv1.IPPoolList{}
		g.Expect(dpuClusterClient[0].List(ctx, ipPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(ipPools.Items).To(BeEmpty())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateLegacyDPUServiceIPAMCreationCidrSplit(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithCIDRPoolName := "legacy-ipv4-network"
	By("Creating a DPUServiceIPAM using the legacy ipv4Network field")
	dpuServiceIPAM := buildTestDPUServiceIPAM(dpuServiceIPAMWithCIDRPoolName, input.cidrDPUServiceIPAM)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Checking that the legacy field is rendered as an NVIPAM CIDRPool")
	Eventually(func(g Gomega) {
		cidrPools := &nvipamv1.CIDRPoolList{}
		g.Expect(dpuClusterClient[0].List(ctx, cidrPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(cidrPools.Items).To(HaveLen(1))

		// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
	}).WithTimeout(180 * time.Second).Should(Succeed())

	// The pinned release operator used in OCP reuse mode emits the metric names
	// without the dpf_ prefix, so only the CIDRPool checks above apply there.
	if isGinkgoLabelApplied(Domain.OCP) {
		return
	}

	By("Verify CIDRPool metrics in DPU cluster KSM")
	expectedCIDRPoolMetricsNames := map[string][]string{
		"dpf_cidrpool": {"created", "info", "allocation_info"},
	}
	Eventually(func(g Gomega) {
		g.Expect(input.dpuClusters).ToNot(BeEmpty(), "No DPUClusters found in test input")
		dpuKSMMetricsURI, err := metrics.GetKSMMetricsURIForDPUCluster(ctx, input.client, input.dpuClusters[0], dpfOperatorSystemNamespace, kubeStateMetricsPort, "/metrics")
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get KSM metrics URI for DPUCluster")
		g.Expect(dpuKSMMetricsURI).NotTo(BeEmpty())

		// Use hostClusterRESTClient because in-cluster KSM runs on the management cluster
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, dpuKSMMetricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedCIDRPoolMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMDeletionCidrSplit(ctx context.Context, input *systemTestInput) {
	if input.cleanupFlags.SkipCleanup {
		Skip("Skip cleanup resources")
	}
	dpuServiceIPAMWithCIDRPoolName := "routed-application-delete"

	By("Creating the DPUServiceIPAM CR")
	dpuServiceIPAM := buildTestDPUServiceIPAM(dpuServiceIPAMWithCIDRPoolName, input.cidrDPUServiceIPAM)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	// Wait for the controller to set its finalizer before deleting, otherwise a
	// Delete racing with the finalizer patch can remove the object before reconcileDelete
	// runs and leaves the dpu-cluster object orphaned. See https://github.com/kubernetes/kubernetes/issues/77988
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceIPAM), dpuServiceIPAM)).To(Succeed())
		g.Expect(dpuServiceIPAM.Finalizers).To(ContainElement(dpuservicev1.DPUServiceIPAMFinalizer))
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Deleting the DPUServiceIPAM")
	Expect(input.client.Delete(ctx, dpuServiceIPAM)).To(Succeed())

	By("Checking that NVIPAM CIDRPool CR is deleted in each DPU cluster")
	Eventually(func(g Gomega) {
		cidrPools := &nvipamv1.CIDRPoolList{}
		g.Expect(dpuClusterClient[0].List(ctx, cidrPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(cidrPools.Items).To(BeEmpty())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

// buildTestDPUServiceIPAM returns a DPUServiceIPAM for the test. On OCP it
// drops physical-suite-only node selectors and static allocations.
func buildTestDPUServiceIPAM(name string, template *dpuservicev1.DPUServiceIPAM) *dpuservicev1.DPUServiceIPAM {
	dpuServiceIPAM := testutils.GenerateDPUObj(name, dpuServiceIPAMNamespace, template.DeepCopy())
	if !isGinkgoLabelApplied(Domain.OCP) {
		return dpuServiceIPAM
	}
	dpuServiceIPAM.Spec.NodeSelector = nil
	if dpuServiceIPAM.Spec.IPV4Network != nil {
		dpuServiceIPAM.Spec.IPV4Network.Allocations = nil
	}
	return dpuServiceIPAM
}

type dpuServiceIPAMWorkloadPool struct {
	family  corev1.IPFamily
	subnet  string
	gateway string
}

func ValidateDPUServiceIPAMWorkload(ctx context.Context, input *systemTestInput, name, interfaceName string, pools []dpuServiceIPAMWorkloadPool) {
	if !input.hasDpuNodes() {
		Skip("Skip DPUServiceIPAM workload test as there are no DPU nodes")
	}
	Expect(input.dpuClusters).To(HaveLen(1), "DPUServiceIPAM workload test requires exactly one DPU cluster")
	Expect(pools).ToNot(BeEmpty())

	const sfcNetworkName = "mybrsfc"
	serviceID := "ipam-" + name
	poolLabels := map[string]string{"svc.dpu.nvidia.com/pool": serviceID}
	clusterSelector := firstDPUClusterSelector(input)
	requiredIPFamilies := make([]corev1.IPFamily, 0, len(pools))
	expectedPrefixes := make([]netip.Prefix, 0, len(pools))
	dpuServiceIPAMs := make([]*dpuservicev1.DPUServiceIPAM, 0, len(pools))

	By("Creating DPUServiceIPAM pools for the requested IP families")
	for _, poolConfig := range pools {
		poolName := fmt.Sprintf("%s-pool-%s", serviceID, strings.ToLower(string(poolConfig.family)))
		requiredIPFamilies = append(requiredIPFamilies, poolConfig.family)
		prefix, err := netip.ParsePrefix(poolConfig.subnet)
		Expect(err).NotTo(HaveOccurred())
		expectedPrefixes = append(expectedPrefixes, prefix)

		dpuServiceIPAM := newSubnetDPUServiceIPAM(
			poolName,
			input.namespace,
			poolLabels,
			poolConfig.subnet,
			poolConfig.gateway,
			clusterSelector,
		)
		Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())
		dpuServiceIPAMs = append(dpuServiceIPAMs, dpuServiceIPAM)
	}

	By("Waiting for the DPUServiceIPAM pools to allocate node ranges")
	for _, dpuServiceIPAM := range dpuServiceIPAMs {
		EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceIPAM, 5*time.Minute)
	}

	interfaceResourceName := "p0-sf-" + serviceID
	interfaceLabels := map[string]string{
		"svc.dpu.nvidia.com/interface": interfaceResourceName,
		"svc.dpu.nvidia.com/service":   serviceID,
	}
	By("Creating the DPUServiceInterface")
	dpuServiceInterface := constructDPUServiceInterface(interfaceResourceName, input.namespace, serviceID, input.namespace, sfcNetworkName, interfaceLabels)
	dpuServiceInterface.Spec.Template.Spec.Template.Spec.Service.InterfaceName = interfaceName
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceInterface, 10*time.Minute)

	chainName := serviceID + "-chain"
	By("Creating the DPUServiceChain with explicit required IP families")
	dpuServiceChain := constructDPUServiceChain(chainName, input.namespace, 1500, interfaceLabels)
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches[0].Ports[0].ServiceInterface.IPAM = &dpuservicev1.IPAM{
		MatchLabels:        poolLabels,
		RequiredIPFamilies: requiredIPFamilies,
	}
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("Creating the DPUService workload")
	dpuService := constructDummyDPUServiceObject(serviceID, input.namespace, interfaceResourceName)
	configureNetshootDPUService(dpuService, serviceID)
	Expect(input.client.Create(ctx, dpuService)).To(Succeed())

	By("Waiting for the DPUService workload to be scheduled")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceChain, 3*time.Minute)
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{serviceID})

	By("Verifying the allocated interface addresses")
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.InNamespace(input.namespace),
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceID},
		)).To(Succeed())

		activePods := make([]corev1.Pod, 0, len(podList.Items))
		for _, pod := range podList.Items {
			if pod.DeletionTimestamp.IsZero() {
				activePods = append(activePods, pod)
			}
		}
		g.Expect(activePods).To(HaveLen(input.totalDPUs()))

		allocatedAddresses := map[netip.Addr]struct{}{}
		for _, pod := range activePods {
			g.Expect(netshoot.IsPodRunningAndReady(&pod)).To(BeTrue(), "Pod %s/%s is not ready", pod.Namespace, pod.Name)

			ips := getPodIPsForInterface(g, pod, interfaceName)
			g.Expect(ips).To(HaveLen(len(expectedPrefixes)))
			matchedPrefixes := make(map[netip.Prefix]struct{}, len(expectedPrefixes))
			for _, ip := range ips {
				address, err := netip.ParseAddr(ip)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(allocatedAddresses).NotTo(HaveKey(address), "address %s was assigned to more than one Pod", address)
				allocatedAddresses[address] = struct{}{}

				matched := false
				for _, prefix := range expectedPrefixes {
					if prefix.Contains(address) {
						g.Expect(matched).To(BeFalse(), "address %s matched more than one test prefix", address)
						matched = true
						matchedPrefixes[prefix] = struct{}{}
					}
				}
				g.Expect(matched).To(BeTrue(), "address %s is outside the configured pools", address)
			}
			g.Expect(matchedPrefixes).To(HaveLen(len(expectedPrefixes)))
		}
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

func newSubnetDPUServiceIPAM(
	name, namespace string,
	poolLabels map[string]string,
	subnet, gateway string,
	clusterSelector *metav1.LabelSelector,
) *dpuservicev1.DPUServiceIPAM {
	dpuServiceIPAM := testutils.GenerateDPUObj(name, namespace, &dpuservicev1.DPUServiceIPAM{})
	dpuServiceIPAM.Spec = dpuservicev1.DPUServiceIPAMSpec{
		ObjectMeta:         dpuservicev1.ObjectMeta{Labels: poolLabels},
		DPUClusterSelector: clusterSelector.DeepCopy(),
		Subnet: &dpuservicev1.Subnet{
			Subnet:         subnet,
			Gateway:        gateway,
			PerNodeIPCount: 8,
		},
	}
	return dpuServiceIPAM
}

func newNetworkDPUServiceIPAM(
	name, namespace string,
	poolLabels map[string]string,
	network string,
	prefixSize int32,
	clusterSelector *metav1.LabelSelector,
) *dpuservicev1.DPUServiceIPAM {
	dpuServiceIPAM := testutils.GenerateDPUObj(name, namespace, &dpuservicev1.DPUServiceIPAM{})
	dpuServiceIPAM.Spec = dpuservicev1.DPUServiceIPAMSpec{
		ObjectMeta:         dpuservicev1.ObjectMeta{Labels: poolLabels},
		DPUClusterSelector: clusterSelector.DeepCopy(),
		Network: &dpuservicev1.Network{
			Network:      network,
			GatewayIndex: ptr.To[int32](1),
			PrefixSize:   prefixSize,
		},
	}
	return dpuServiceIPAM
}

func firstDPUClusterSelector(input *systemTestInput) *metav1.LabelSelector {
	Expect(input.dpuClusters).ToNot(BeEmpty())
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"svc.dpu.nvidia.com/cluster": input.dpuClusters[0].Name,
		},
	}
}
