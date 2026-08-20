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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Expect(apierrors.IsInvalid(err)).To(BeTrue())
	Expect(err.Error()).To(ContainSubstring("exactly one of ipv4Network, ipv4Subnet, network, or subnet must be specified"))
}

func ValidateDPUServiceIPAMCreationSubnetSplit(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithIPPoolName := "switched-application"
	By("Creating the DPUServiceIPAM CR")
	dpuServiceIPAM := testutils.GenerateDPUObj(dpuServiceIPAMWithIPPoolName, dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Checking that NVIPAM IPPool CR is created in the DPU clusters")
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
	By("Creating the DPUServiceIPAM CR")
	dpuServiceIPAM := testutils.GenerateDPUObj("switched-application-metrics", dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
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
	dpuServiceIPAM := testutils.GenerateDPUObj(dpuServiceIPAMWithIPPoolName, dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
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

func ValidateDPUServiceIPAMCreationCidrSplit(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithCIDRPoolName := "routed-application"
	By("Creating the DPUServiceIPAM CR")
	dpuServiceIPAM := testutils.GenerateDPUObj(dpuServiceIPAMWithCIDRPoolName, dpuServiceIPAMNamespace, input.cidrDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Checking that NVIPAM CIDRPool CR is created in the DPU clusters")
	Eventually(func(g Gomega) {
		cidrPools := &nvipamv1.CIDRPoolList{}
		g.Expect(dpuClusterClient[0].List(ctx, cidrPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(cidrPools.Items).To(HaveLen(1))

		// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
	}).WithTimeout(180 * time.Second).Should(Succeed())

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
	dpuServiceIPAM := testutils.GenerateDPUObj(dpuServiceIPAMWithCIDRPoolName, dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
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
