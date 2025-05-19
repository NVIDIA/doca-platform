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

func ValidateDPUServiceIPAM(ctx context.Context, input systemTestInput) {
	testClient := input.client
	dpuServiceIPAMWithIPPoolName := "switched-application"
	dpuServiceIPAMWithCIDRPoolName := "routed-application"
	dpuServiceIPAMNamespace := dpfOperatorSystemNamespace

	It("create an invalid DPUServiceIPAM and ensure that the webhook rejects the request", func() {
		By("creating the invalid DPUServiceIPAM CR")
		dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-name",
				Namespace: dpuServiceIPAMNamespace,
			},
		}
		dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
		dpuServiceIPAM.SetLabels(cleanupLabels)
		err := testClient.Create(ctx, dpuServiceIPAM)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsBadRequest(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("either ipv4Subnet or ipv4Network must be specified"))
	})

	It("create a DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is created to each cluster", func() {
		By("creating the DPUServiceIPAM CR")
		dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
		dpuServiceIPAM.SetName(dpuServiceIPAMWithIPPoolName)
		dpuServiceIPAM.SetNamespace(dpuServiceIPAMNamespace)
		dpuServiceIPAM.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())

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
	})

	It("verify DPUServiceIPAM metrics", func() {
		if !deployKSM {
			Skip("Skip KSM metrics test due to KSM is not deployed")
		}

		By("verify DPUServiceIPAM metrics in KSM")
		expectedMetricsNames := map[string][]string{
			"dpuserviceipam": {"created", "info", "status_conditions", "status_condition_last_transition_time"}, //  "network_info", "subnet_info" missed
		}
		Eventually(func(g Gomega) {
			actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("delete the DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is deleted in each cluster", func() {
		if input.skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("deleting the DPUServiceIPAM")
		dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceIPAMNamespace, Name: dpuServiceIPAMWithIPPoolName}, dpuServiceIPAM)).To(Succeed())
		Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

		By("checking that NVIPAM IPPool CR is deleted in each DPU cluster")
		Eventually(func(g Gomega) {
			ipPools := &nvipamv1.IPPoolList{}
			g.Expect(dpuClusterClient.List(ctx, ipPools, client.MatchingLabels{
				"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
				"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
			})).To(Succeed())
			g.Expect(ipPools.Items).To(BeEmpty())
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("create a DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is created to each cluster", func() {
		By("creating the DPUServiceIPAM CR")
		dpuServiceIPAM := input.cidrDPUServiceIPAM.DeepCopy()
		dpuServiceIPAM.SetName(dpuServiceIPAMWithCIDRPoolName)
		dpuServiceIPAM.SetNamespace(dpuServiceIPAMNamespace)
		dpuServiceIPAM.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())

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
	})

	It("delete the DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is deleted in each cluster", func() {
		if input.skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("deleting the DPUServiceIPAM")
		dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceIPAMNamespace, Name: dpuServiceIPAMWithCIDRPoolName}, dpuServiceIPAM)).To(Succeed())
		Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

		By("checking that NVIPAM CIDRPool CR is deleted in each DPU cluster")
		Eventually(func(g Gomega) {
			cidrPools := &nvipamv1.CIDRPoolList{}
			g.Expect(dpuClusterClient.List(ctx, cidrPools, client.MatchingLabels{
				"dpu.nvidia.com/dpuserviceipam-name":      dpuServiceIPAM.GetName(),
				"dpu.nvidia.com/dpuserviceipam-namespace": dpuServiceIPAM.GetNamespace(),
			})).To(Succeed())
			g.Expect(cidrPools.Items).To(BeEmpty())
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

}
