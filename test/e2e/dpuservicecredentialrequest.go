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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUServiceCredentialRequest(ctx context.Context, input systemTestInput) {
	testClient := input.client
	hostDPUServiceCredentialRequestName := "host-dpu-credential-request"
	dpuServiceCredentialRequestName := "dpu-01-credential-request"
	dpuServiceCredentialRequestNamespace := "dpucr-test-ns"

	It("create a DPUServiceCredentialRequest and check that the credentials are created", func() {
		By("create namespace for DPUServiceCredentialRequest")
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceCredentialRequestNamespace}}
		testNS.SetLabels(cleanupLabels)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())

		By("create a DPUServiceCredentialRequest targeting the DPUCluster")
		dcr := input.dpuServiceCredentialRequest.DeepCopy()
		dcr.SetName(dpuServiceCredentialRequestName)
		dcr.SetNamespace(dpuServiceCredentialRequestNamespace)
		dcr.Spec.TargetCluster = &dpuservicev1.NamespacedName{Name: input.dpuCluster.Name, Namespace: ptr.To(dpfOperatorSystemNamespace)}
		dcr.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dcr)).To(Succeed())

		By("create a DPUServiceCredentialRequest targeting the host cluster")
		hostDcr := input.dpuServiceCredentialRequest.DeepCopy()
		hostDcr.SetName(hostDPUServiceCredentialRequestName)
		hostDcr.SetNamespace(dpuServiceCredentialRequestNamespace)
		hostDcr.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, hostDcr)).To(Succeed())

		By("verify reconciled DPUServiceCredentialRequest for DPUCluster")
		Eventually(func(g Gomega) {
			assertDPUServiceCredentialRequest(ctx, g, testClient, dcr, false)
		}).WithTimeout(300 * time.Second).Should(Succeed())

		By("verify reconciled DPUServiceCredentialRequest for host cluster")
		Eventually(func(g Gomega) {
			assertDPUServiceCredentialRequest(ctx, g, testClient, hostDcr, true)
		}).WithTimeout(600 * time.Second).Should(Succeed())

	})
	It("verify DPUServiceCredentialRequest metrics", func() {
		if !deployKSM {
			Skip("Skip KSM metrics accessibility test due to KSM is not deployed")
		}

		By("verify DPUServiceCredentialRequest metrics in KSM")
		expectedMetricsNames := map[string][]string{
			"dpuservicecredentialrequest": {"created", "info", "expiration", "issued_at", "status_conditions", "status_condition_last_transition_time"},
		}
		Eventually(func(g Gomega) {
			actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())

	})

	It("delete the DPUServiceCredentialRequest and check that the credentials are deleted", func() {
		if input.skipCleanup {
			Skip("Skip cleanup resources")
		}
		By("delete the DPUServiceCredentialRequest")
		dcr := &dpuservicev1.DPUServiceCredentialRequest{}
		key := client.ObjectKey{Namespace: dpuServiceCredentialRequestNamespace, Name: dpuServiceCredentialRequestName}
		Expect(testClient.Get(ctx, key, dcr)).To(Succeed())
		Expect(testClient.Delete(ctx, dcr)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, key, dcr)).NotTo(Succeed())
		}).WithTimeout(300 * time.Second).Should(Succeed())

		key = client.ObjectKey{Namespace: dpuServiceCredentialRequestNamespace, Name: hostDPUServiceCredentialRequestName}
		Expect(testClient.Get(ctx, key, dcr)).To(Succeed())
		Expect(testClient.Delete(ctx, dcr)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, key, dcr)).NotTo(Succeed())
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})

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
