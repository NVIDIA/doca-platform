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

func ValidateDPUServiceInterfaceCreation(ctx context.Context, input *systemTestInput) {
	dpuServiceInterfaceNamespace := "test-service-interface"
	dpuServiceInterfaceName := "pf0-vf2"

	By("create test namespace")
	createTestNamespace(ctx, input.client, dpuServiceInterfaceNamespace)

	By("create DPUServiceInterface")
	dpuServiceInterface := generateDPUObj(dpuServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	By("verify ServiceInterfaceSet is created in DPF clusters")
	Eventually(func(g Gomega) {
		scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceName, Namespace: dpuServiceInterfaceNamespace}}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
	}, time.Second*300, time.Millisecond*250).Should(Succeed())
}

func ValidateDPUServiceChainCreation(ctx context.Context, input *systemTestInput) {
	dpuServiceChainName := "svc-chain-test"
	dpuServiceChainNamespace := "test-2"
	By("create test namespace")
	createTestNamespace(ctx, input.client, dpuServiceChainNamespace)

	By("create DPUServiceChain")
	dpuServiceChain := generateDPUObj(dpuServiceChainName, dpuServiceChainNamespace, input.dpuServiceChain.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("verify ServiceChainSet is created in DPF clusters")
	Eventually(func(g Gomega) {
		scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceChainName, Namespace: dpuServiceChainNamespace}}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
	}, time.Second*300, time.Millisecond*250).Should(Succeed())

}

func ValidateDPUServiceChainMetrics(ctx context.Context, input *systemTestInput) {
	dpuServiceInterfaceName := "pf0-vf2-metrics"
	dpuServiceInterfaceNamespace := "test-metrics"
	dpuServiceChainName := "svc-chain-test-metrics"

	By("create test namespaces")
	createTestNamespace(ctx, input.client, dpuServiceInterfaceNamespace)

	By("create DPUServiceInterface and DPUServiceChain")
	dpuServiceInterface := generateDPUObj(dpuServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())
	dpuServiceChain := generateDPUObj(dpuServiceChainName, dpuServiceInterfaceNamespace, input.dpuServiceChain.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("verify DPUServiceChain and DPUServiceInterface metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuservicechain":     {"created", "info", "status_conditions", "status_condition_last_transition_time"},
		"dpuserviceinterface": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func ValidateDPUServiceChainDeletion(ctx context.Context, input *systemTestInput) {
	if input.skipCleanup {
		Skip("Skip cleanup resources")
	}
	dpuServiceInterfaceName := "pf0-vf2-delete"
	dpuServiceInterfaceNamespace := "test-delete"
	dpuServiceChainName := "svc-chain-test-delete"

	By("create test namespaces")
	createTestNamespace(ctx, input.client, dpuServiceInterfaceNamespace)

	By("create DPUServiceInterface and DPUServiceChain")
	dpuServiceInterface := generateDPUObj(dpuServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())
	dpuServiceChain := generateDPUObj(dpuServiceChainName, dpuServiceInterfaceNamespace, input.dpuServiceChain.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	dsi := &dpuservicev1.DPUServiceInterface{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceInterface.Namespace, Name: dpuServiceInterface.Name}, dsi)).To(Succeed())
	Expect(input.client.Delete(ctx, dsi)).To(Succeed())
	dsc := &dpuservicev1.DPUServiceChain{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceChain.Namespace, Name: dpuServiceChain.Name}, dsc)).To(Succeed())
	Expect(input.client.Delete(ctx, dsc)).To(Succeed())
	// Get the control plane secrets.
	Eventually(func(g Gomega) {
		serviceChainSetList := dpuservicev1.ServiceChainSetList{}
		g.Expect(dpuClusterClient.List(ctx, &serviceChainSetList,
			&client.ListOptions{Namespace: dpuServiceChain.Namespace})).To(Succeed())
		g.Expect(serviceChainSetList.Items).To(BeEmpty())
		serviceInterfaceSetList := dpuservicev1.ServiceInterfaceSetList{}
		g.Expect(dpuClusterClient.List(ctx, &serviceInterfaceSetList,
			&client.ListOptions{Namespace: dpuServiceInterfaceNamespace})).To(Succeed())
		g.Expect(serviceInterfaceSetList.Items).To(BeEmpty())
	}).WithTimeout(300 * time.Second).Should(Succeed())
}
