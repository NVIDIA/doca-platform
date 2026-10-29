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
	By("creating the invalid DPUServiceIPAM CR")
	dpuServiceIPAMNamespace = dpfOperatorSystemNamespace
	dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-name",
			Namespace: dpuServiceIPAMNamespace,
		},
	}
	dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
	dpuServiceIPAM.SetLabels(afterEachCleanupLabels)
	err := input.client.Create(ctx, dpuServiceIPAM)
	Expect(err).To(HaveOccurred())
	fmt.Printf("Error creating the DPUServiceIPAM CR: %v\n", err)
	Expect(apierrors.IsBadRequest(err)).To(BeTrue())
	Expect(err.Error()).To(ContainSubstring("either ipv4Subnet or ipv4Network must be specified"))
}

func ValidateDPUServiceIPAMCreationSubnetSplit(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithIPPoolName := "switched-application"
	By("creating the DPUServiceIPAM CR")
	dpuServiceIPAM := generateDPUObj(dpuServiceIPAMWithIPPoolName, dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("checking that NVIPAM IPPool CR is created in the DPU clusters")
	Eventually(func(g Gomega) {
		ipPools := &nvipamv1.IPPoolList{}
		g.Expect(dpuClusterClient.List(ctx, ipPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(ipPools.Items).To(HaveLen(1))

		// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMMetrics(ctx context.Context, input *systemTestInput) {
	By("creating the DPUServiceIPAM CR")
	dpuServiceIPAM := generateDPUObj("switched-application-metrics", dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("verify DPUServiceIPAM metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuserviceipam": {"created", "info", "status_conditions", "status_condition_last_transition_time"}, //  "network_info", "subnet_info" missed
	}
	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMMetricsDeletion(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithIPPoolName := "switched-application-delete"
	if input.skipCleanup {
		Skip("Skip cleanup resources")
	}

	By("creating the DPUServiceIPAM CR")
	dpuServiceIPAM := generateDPUObj(dpuServiceIPAMWithIPPoolName, dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("deleting the DPUServiceIPAM")
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceIPAMNamespace, Name: dpuServiceIPAMWithIPPoolName}, dpuServiceIPAM)).To(Succeed())
	Expect(input.client.Delete(ctx, dpuServiceIPAM)).To(Succeed())

	By("checking that NVIPAM IPPool CR is deleted in each DPU cluster")
	Eventually(func(g Gomega) {
		ipPools := &nvipamv1.IPPoolList{}
		g.Expect(dpuClusterClient.List(ctx, ipPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(ipPools.Items).To(BeEmpty())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMCreationCidrSplit(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAMWithCIDRPoolName := "routed-application"
	By("creating the DPUServiceIPAM CR")
	dpuServiceIPAM := generateDPUObj(dpuServiceIPAMWithCIDRPoolName, dpuServiceIPAMNamespace, input.cidrDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("checking that NVIPAM CIDRPool CR is created in the DPU clusters")
	Eventually(func(g Gomega) {
		cidrPools := &nvipamv1.CIDRPoolList{}
		g.Expect(dpuClusterClient.List(ctx, cidrPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(cidrPools.Items).To(HaveLen(1))

		// TODO: Check that NVIPAM has reconciled the resources and status reflects that.
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func ValidateDPUServiceIPAMDeletionCidrSplit(ctx context.Context, input *systemTestInput) {
	if input.skipCleanup {
		Skip("Skip cleanup resources")
	}
	dpuServiceIPAMWithCIDRPoolName := "routed-application-delete"

	By("creating the DPUServiceIPAM CR")
	dpuServiceIPAM := generateDPUObj(dpuServiceIPAMWithCIDRPoolName, dpuServiceIPAMNamespace, input.ipPoolDPUServiceIPAM.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("deleting the DPUServiceIPAM")
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceIPAM.Namespace, Name: dpuServiceIPAM.Name}, dpuServiceIPAM)).To(Succeed())
	Expect(input.client.Delete(ctx, dpuServiceIPAM)).To(Succeed())

	By("checking that NVIPAM CIDRPool CR is deleted in each DPU cluster")
	Eventually(func(g Gomega) {
		cidrPools := &nvipamv1.CIDRPoolList{}
		g.Expect(dpuClusterClient.List(ctx, cidrPools, client.MatchingLabels{
			"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
			"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
		})).To(Succeed())
		g.Expect(cidrPools.Items).To(BeEmpty())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}
