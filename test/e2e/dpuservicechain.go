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
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/metrics"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUServiceInterfaceCreation(ctx context.Context, input *systemTestInput) {
	testDPUServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test-service-interface"

	By("Create test namespace")
	createTestNamespace(ctx, input.client, dpuServiceInterfaceNamespace)

	By("Create DPUServiceInterface")
	dpuServiceInterface := utils.GenerateDPUObj(testDPUServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	By("Verify ServiceInterfaceSet is created in DPF clusters")
	Eventually(func(g Gomega) {
		scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: testDPUServiceInterfaceName, Namespace: dpuServiceInterfaceNamespace}}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
	}, time.Second*300, time.Millisecond*250).Should(Succeed())
}

func ValidateDPUServiceChainCreation(ctx context.Context, input *systemTestInput) {
	dpuServiceChainName := "svc-chain-test"
	dpuServiceChainNamespace := "test-2"
	By("Create test namespace")
	createTestNamespace(ctx, input.client, dpuServiceChainNamespace)

	By("Create DPUServiceChain")
	dpuServiceChain := utils.GenerateDPUObj(dpuServiceChainName, dpuServiceChainNamespace, input.dpuServiceChain.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("Verify ServiceChainSet is created in DPF clusters")
	Eventually(func(g Gomega) {
		scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceChainName, Namespace: dpuServiceChainNamespace}}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
	}, time.Second*300, time.Millisecond*250).Should(Succeed())

}

func ValidateDPUServiceChainMetrics(ctx context.Context, input *systemTestInput) {
	dpuServiceInterfaceName := "pf0-vf2-metrics"
	dpuServiceInterfaceNamespace := "test-metrics"
	dpuServiceChainName := "svc-chain-test-metrics"

	By("Create test namespaces")
	createTestNamespace(ctx, input.client, dpuServiceInterfaceNamespace)

	By("Create DPUServiceInterface and DPUServiceChain")
	dpuServiceInterface := utils.GenerateDPUObj(dpuServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())
	dpuServiceChain := utils.GenerateDPUObj(dpuServiceChainName, dpuServiceInterfaceNamespace, input.dpuServiceChain.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("Verify DPUServiceChain and DPUServiceInterface metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpf_dpuservicechain":     {"created", "info", "status_conditions", "status_condition_last_transition_time"},
		"dpf_dpuserviceinterface": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())

	By("Wait for ServiceChainSet and ServiceInterfaceSet to be created in DPU clusters")
	Eventually(func(g Gomega) {
		scs := &dpuservicev1.ServiceChainSet{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: dpuServiceChainName, Namespace: dpuServiceInterfaceNamespace}, scs)).To(Succeed())
		sis := &dpuservicev1.ServiceInterfaceSet{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: dpuServiceInterfaceName, Namespace: dpuServiceInterfaceNamespace}, sis)).To(Succeed())
	}).WithTimeout(300 * time.Second).Should(Succeed())

	// TODO: add validation for ServiceChain and ServiceInterface metrics when DPU nodes are present
	By("Verify ServiceChainSet, ServiceInterfaceSet metrics in DPU cluster KSM")
	expectedDPUMetricsNames := map[string][]string{
		"dpf_servicechainset":     {"created", "info", "status_conditions", "status_condition_last_transition_time", "status_number_applied", "status_number_ready"},
		"dpf_serviceinterfaceset": {"created", "info", "status_conditions", "status_condition_last_transition_time", "status_number_applied", "status_number_ready"},
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
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

func ValidateDPUServiceChainDeletion(ctx context.Context, input *systemTestInput) {
	if input.cleanupFlags.SkipCleanup {
		Skip("Skip cleanup resources")
	}
	dpuServiceInterfaceName := "pf0-vf2-delete"
	dpuServiceInterfaceNamespace := "test-delete"
	dpuServiceChainName := "svc-chain-test-delete"

	By("Create test namespaces")
	createTestNamespace(ctx, input.client, dpuServiceInterfaceNamespace)

	By("Create DPUServiceInterface and DPUServiceChain")
	dpuServiceInterface := utils.GenerateDPUObj(dpuServiceInterfaceName, dpuServiceInterfaceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())
	dpuServiceChain := utils.GenerateDPUObj(dpuServiceChainName, dpuServiceInterfaceNamespace, input.dpuServiceChain.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	dsi := &dpuservicev1.DPUServiceInterface{}
	dsc := &dpuservicev1.DPUServiceChain{}

	// Wait for the controllers to set their finalizers before deleting, otherwise a
	// Delete racing with the finalizer patch can remove the object before reconcileDelete
	// runs and leaves the dpu-cluster object orphaned. See https://github.com/kubernetes/kubernetes/issues/77988
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceInterface), dsi)).To(Succeed())
		g.Expect(dsi.Finalizers).To(ContainElement(dpuservicev1.DPUServiceInterfaceFinalizer))
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceChain), dsc)).To(Succeed())
		g.Expect(dsc.Finalizers).To(ContainElement(dpuservicev1.DPUServiceChainFinalizer))
	}).WithTimeout(60 * time.Second).Should(Succeed())

	Expect(input.client.Delete(ctx, dsi)).To(Succeed())
	Expect(input.client.Delete(ctx, dsc)).To(Succeed())
	// Get the control plane secrets.
	Eventually(func(g Gomega) {
		serviceChainSetList := dpuservicev1.ServiceChainSetList{}
		g.Expect(dpuClusterClient[0].List(ctx, &serviceChainSetList,
			&client.ListOptions{Namespace: dpuServiceChain.Namespace})).To(Succeed())
		g.Expect(serviceChainSetList.Items).To(BeEmpty())
		serviceInterfaceSetList := dpuservicev1.ServiceInterfaceSetList{}
		g.Expect(dpuClusterClient[0].List(ctx, &serviceInterfaceSetList,
			&client.ListOptions{Namespace: dpuServiceInterfaceNamespace})).To(Succeed())
		g.Expect(serviceInterfaceSetList.Items).To(BeEmpty())
	}).WithTimeout(300 * time.Second).Should(Succeed())
}
