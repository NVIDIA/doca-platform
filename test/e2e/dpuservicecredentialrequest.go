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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUServiceCredentialRequestCreation(ctx context.Context, input *systemTestInput) {
	hostDPUServiceCredentialRequestName := "host-dpu-credential-request"
	dpuServiceCredentialRequestName := "dpu-01-credential-request"
	dpuServiceCredentialRequestNamespace := "dpucr-test-ns"

	By("create namespace for DPUServiceCredentialRequest")
	createTestNamespace(ctx, input.client, dpuServiceCredentialRequestNamespace)

	By("create a DPUServiceCredentialRequest targeting the DPUCluster")
	dcr := utils.GenerateDPUObj(dpuServiceCredentialRequestName, dpuServiceCredentialRequestNamespace, input.dpuServiceCredentialRequest.DeepCopy())
	dcr.Spec.TargetCluster = &dpuservicev1.NamespacedName{Name: input.dpuClusters[0].Name, Namespace: ptr.To(dpfOperatorSystemNamespace)}
	Expect(input.client.Create(ctx, dcr)).To(Succeed())

	By("create a DPUServiceCredentialRequest targeting the host cluster")
	hostDcr := utils.GenerateDPUObj(hostDPUServiceCredentialRequestName, dpuServiceCredentialRequestNamespace, input.dpuServiceCredentialRequest.DeepCopy())
	Expect(input.client.Create(ctx, hostDcr)).To(Succeed())

	By("verify reconciled DPUServiceCredentialRequest for DPUCluster")
	Eventually(func(g Gomega) {
		assertDPUServiceCredentialRequest(ctx, g, input.client, dcr, false)
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("verify reconciled DPUServiceCredentialRequest for host cluster")
	Eventually(func(g Gomega) {
		assertDPUServiceCredentialRequest(ctx, g, input.client, hostDcr, true)
	}).WithTimeout(600 * time.Second).Should(Succeed())

}

func ValidateDPUServiceCredentialRequestMetrics(ctx context.Context, input *systemTestInput) {
	dpuServiceCredentialRequestName := "dpu-01-credential-request-metrics"
	dpuServiceCredentialRequestNamespace := "dpucr-test-ns-metrics"
	By("create namespace for DPUServiceCredentialRequest")
	createTestNamespace(ctx, input.client, dpuServiceCredentialRequestNamespace)

	By("create a DPUServiceCredentialRequest targeting the DPUCluster")
	dcr := utils.GenerateDPUObj(dpuServiceCredentialRequestName, dpuServiceCredentialRequestNamespace, input.dpuServiceCredentialRequest.DeepCopy())
	dcr.Spec.TargetCluster = &dpuservicev1.NamespacedName{Name: input.dpuClusters[0].Name, Namespace: ptr.To(dpfOperatorSystemNamespace)}
	Expect(input.client.Create(ctx, dcr)).To(Succeed())

	By("verify DPUServiceCredentialRequest metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuservicecredentialrequest": {"created", "info", "expiration", "issued_at", "status_conditions", "status_condition_last_transition_time"},
	}
	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func ValidateDPUServiceCredentialRequestDeletion(ctx context.Context, input *systemTestInput) {
	if input.cleanupFlags.SkipCleanup {
		Skip("Skip cleanup resources")
	}

	hostDPUServiceCredentialRequestName := "host-dpu-credential-request-delete"
	dpuServiceCredentialRequestName := "dpu-01-credential-request-delete"
	dpuServiceCredentialRequestNamespace := "dpucr-test-ns-delete"

	By("create namespace for DPUServiceCredentialRequest")
	createTestNamespace(ctx, input.client, dpuServiceCredentialRequestNamespace)

	By("create a DPUServiceCredentialRequest targeting the DPUCluster")
	dcr := utils.GenerateDPUObj(dpuServiceCredentialRequestName, dpuServiceCredentialRequestNamespace, input.dpuServiceCredentialRequest.DeepCopy())
	dcr.Spec.TargetCluster = &dpuservicev1.NamespacedName{Name: input.dpuClusters[0].Name, Namespace: ptr.To(dpfOperatorSystemNamespace)}
	Expect(input.client.Create(ctx, dcr)).To(Succeed())

	By("create a DPUServiceCredentialRequest targeting the host cluster")
	hostDcr := utils.GenerateDPUObj(hostDPUServiceCredentialRequestName, dpuServiceCredentialRequestNamespace, input.dpuServiceCredentialRequest.DeepCopy())
	Expect(input.client.Create(ctx, hostDcr)).To(Succeed())

	By("verify reconciled DPUServiceCredentialRequest for DPUCluster")
	Eventually(func(g Gomega) {
		assertDPUServiceCredentialRequest(ctx, g, input.client, dcr, false)
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("verify reconciled DPUServiceCredentialRequest for host cluster")
	Eventually(func(g Gomega) {
		assertDPUServiceCredentialRequest(ctx, g, input.client, hostDcr, true)
	}).WithTimeout(600 * time.Second).Should(Succeed())

	By("delete the DPUServiceCredentialRequest")
	key := client.ObjectKey{Namespace: dpuServiceCredentialRequestNamespace, Name: dpuServiceCredentialRequestName}
	Expect(input.client.Get(ctx, key, dcr)).To(Succeed())
	Expect(input.client.Delete(ctx, dcr)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, key, dcr)).NotTo(Succeed())
	}).WithTimeout(300 * time.Second).Should(Succeed())

	key = client.ObjectKey{Namespace: dpuServiceCredentialRequestNamespace, Name: hostDPUServiceCredentialRequestName}
	Expect(input.client.Get(ctx, key, dcr)).To(Succeed())
	Expect(input.client.Delete(ctx, dcr)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, key, dcr)).NotTo(Succeed())
	}).WithTimeout(300 * time.Second).Should(Succeed())
}

func assertDPUServiceCredentialRequest(ctx context.Context, g Gomega, testClient client.Client, dcr *dpuservicev1.DPUServiceCredentialRequest, host bool) {
	gotDsr := &dpuservicev1.DPUServiceCredentialRequest{}
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dcr), gotDsr)).To(Succeed())
	g.Expect(gotDsr.Finalizers).To(ConsistOf([]string{dpuservicev1.DPUServiceCredentialRequestFinalizer}))
	g.Expect(gotDsr.Status.ServiceAccount).NotTo(BeNil())
	g.Expect(*gotDsr.Status.ServiceAccount).To(Equal(dcr.Spec.ServiceAccount.String()))
	g.Expect(gotDsr.Status.ExpirationTimestamp.Time).To(BeTemporally("~", time.Now().Add(time.Hour), time.Minute))
	g.Expect(gotDsr.Status.IssuedAt).NotTo(BeNil())
	if gotDsr.Spec.Duration != nil {
		iat := gotDsr.Status.ExpirationTimestamp.Time.Add(-1 * gotDsr.Spec.Duration.Duration)
		g.Expect(gotDsr.Status.IssuedAt.Time).To(BeTemporally("~", iat, time.Minute))
	}

	if host {
		g.Expect(gotDsr.Status.TargetCluster).To(BeNil())
	} else {
		g.Expect(gotDsr.Status.TargetCluster).To(Equal(ptr.To(dcr.Spec.TargetCluster.String())))
	}
}
