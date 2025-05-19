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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUServiceChain(ctx context.Context, input systemTestInput) {
	testClient := input.client
	dpuServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test"
	dpuServiceChainName := "svc-chain-test"
	dpuServiceChainNamespace := "test-2"

	It("create DPUServiceInterface and check that it is mirrored to each cluster", func() {
		By("create test namespace")
		createTestNamespace(ctx, testClient, dpuServiceInterfaceNamespace)

		By("create DPUServiceInterface")
		createDPUObj(ctx, testClient, dpuServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())

		By("verify ServiceInterfaceSet is created in DPF clusters")
		Eventually(func(g Gomega) {
			scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceName, Namespace: dpuServiceInterfaceNamespace}}
			g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
		}, time.Second*300, time.Millisecond*250).Should(Succeed())
	})

	It("create DPUServiceChain and check that it is mirrored to each cluster", func() {
		By("create test namespace")
		createTestNamespace(ctx, testClient, dpuServiceChainNamespace)

		By("create DPUServiceChain")
		createDPUObj(ctx, testClient, dpuServiceChainName, dpuServiceChainNamespace, input.dpuServiceChain.DeepCopy())

		By("verify ServiceChainSet is created in DPF clusters")
		Eventually(func(g Gomega) {
			scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceChainName, Namespace: dpuServiceChainNamespace}}
			g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
		}, time.Second*300, time.Millisecond*250).Should(Succeed())

	})

	It("verify DPUServiceChain metrics", func() {
		if !deployKSM {
			Skip("Skip KSM metrics accessibility test due to KSM is not deployed")
		}

		By("verify DPUServiceChain metrics in KSM")
		expectedMetricsNames := map[string][]string{
			"dpuservicechain": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
		}
		Eventually(func(g Gomega) {
			actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("delete the DPUServiceChain & DPUServiceInterface and check that the Sets are cleaned up", func() {
		if input.skipCleanup {
			Skip("Skip cleanup resources")
		}
		dsi := &dpuservicev1.DPUServiceInterface{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceInterfaceNamespace, Name: dpuServiceInterfaceName}, dsi)).To(Succeed())
		Expect(testClient.Delete(ctx, dsi)).To(Succeed())
		dsc := &dpuservicev1.DPUServiceChain{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceChainNamespace, Name: dpuServiceChainName}, dsc)).To(Succeed())
		Expect(testClient.Delete(ctx, dsc)).To(Succeed())
		// Get the control plane secrets.
		Eventually(func(g Gomega) {
			serviceChainSetList := dpuservicev1.ServiceChainSetList{}
			g.Expect(dpuClusterClient.List(ctx, &serviceChainSetList,
				&client.ListOptions{Namespace: dpuServiceChainNamespace})).To(Succeed())
			g.Expect(serviceChainSetList.Items).To(BeEmpty())
			serviceInterfaceSetList := dpuservicev1.ServiceInterfaceSetList{}
			g.Expect(dpuClusterClient.List(ctx, &serviceInterfaceSetList,
				&client.ListOptions{Namespace: dpuServiceInterfaceNamespace})).To(Succeed())
			g.Expect(serviceInterfaceSetList.Items).To(BeEmpty())
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})
}
