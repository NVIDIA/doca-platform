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
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/metrics"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUService(ctx context.Context, input systemTestInput) {
	dpuServiceName := "dpu-01"
	hostDPUServiceName := "host-dpu-service"
	dpuServiceNamespace := "dpu-test-ns"
	testNSImagePullSecret := &corev1.Secret{}
	testClient := input.client
	It("create a DPUService and check Objects and ImagePullSecrets are mirrored correctly", func() {
		By("create namespace for DPUService")
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceNamespace}}
		testNS.SetLabels(cleanupLabels)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())

		By("create ImagePullSecret for DPUService in user namespace")
		testNSImagePullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      input.pullSecretNames[0],
				Namespace: dpuServiceNamespace,
				Labels: map[string]string{
					dpuservicev1.DPFImagePullSecretLabelKey: "",
				},
			},
		}
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNSImagePullSecret))).ToNot(HaveOccurred())

		By("create a DPUServiceInterface")
		dsi := input.dpuServiceInterface.DeepCopy()
		dsi.SetName("net1-service")
		dsi.SetNamespace(dpuServiceNamespace)
		dsi.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dsi)).To(Succeed())

		By("create a DPUService to be deployed on the DPUCluster")
		// Create DPUCluster DPUService and check it's correctly reconciled.
		dpuService := input.dpuService.DeepCopy()
		dpuService.SetName(dpuServiceName)
		dpuService.SetNamespace(dpuServiceNamespace)
		dpuService.Spec.Interfaces = []string{"net1-service"}
		dpuService.Spec.ServiceID = ptr.To("my-service")
		dpuService.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuService)).To(Succeed())

		By("create a DPUService to be deployed on the host cluster")
		// Create a host DPUService and check it's correctly reconciled
		// Read the DPUService from file and create it.
		hostDPUService := input.dpuService.DeepCopy()
		hostDPUService.SetName(hostDPUServiceName)
		hostDPUService.SetNamespace(dpuServiceNamespace)
		hostDPUService.Spec.DeployInCluster = ptr.To(true)
		hostDPUService.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, hostDPUService)).To(Succeed())

		By("verify DPUServices and deployments are created")
		Eventually(func(g Gomega) {
			// Check the deployment from the DPUService can be found on the destination cluster.
			deploymentList := appsv1.DeploymentList{}
			g.Expect(dpuClusterClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
			g.Expect(deploymentList.Items).To(HaveLen(1))
			g.Expect(deploymentList.Items[0].Name).To(ContainSubstring("helm-guestbook"))

			// Check an imagePullSecret was created in the same namespace in the destination cluster.
			g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{
				Namespace: dpuService.GetNamespace(),
				Name:      input.pullSecretNames[0]}, &corev1.Secret{})).To(Succeed())
		}).WithTimeout(600 * time.Second).Should(Succeed())

		By("verify DPUService is created in the host cluster")
		Eventually(func(g Gomega) {
			// Check the deployment from the DPUService can be found on the host cluster.
			deploymentList := appsv1.DeploymentList{}
			g.Expect(testClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
			g.Expect(deploymentList.Items).To(HaveLen(1))
			g.Expect(deploymentList.Items[0].Name).To(ContainSubstring("helm-guestbook"))
		}).WithTimeout(600 * time.Second).Should(Succeed())
	})

	It("verify DPUService and DPUServiceInterface metrics", func() {
		if !deployKSM {
			Skip("Skip KSM metrics test due to KSM is not deployed")
		}

		By("verify DPUService metrics in KSM")
		expectedMetricsNames := map[string][]string{
			"dpuservice": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
		}
		Eventually(func(g Gomega) {
			actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("delete the DPUServices and check that the applications are cleaned up", func() {
		if input.skipCleanup {
			Skip("Skip cleanup resources")
		}

		By("pause dpuservice reconciliation")
		svc := &dpuservicev1.DPUService{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svc)).To(Succeed())
		origSvc := svc.DeepCopy()
		svc.Spec.Paused = ptr.To(true)
		Eventually(testClient.Patch).WithArguments(ctx, svc, client.MergeFrom(origSvc)).Should(Succeed())

		By("delete the DPUServices")
		svc = &dpuservicev1.DPUService{}
		// Delete the DPUCluster DPUService.
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: dpuServiceName}, svc)).To(Succeed())
		Expect(testClient.Delete(ctx, svc)).To(Succeed())

		// Delete the host cluster DPUService.
		svc = &dpuservicev1.DPUService{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svc)).To(Succeed())
		Expect(testClient.Delete(ctx, svc)).To(Succeed())

		// Verify that the DPUServices are deleted
		By("verify DPUServices is deleted in the DPU cluster")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: dpuServiceName}, svc)).ToNot(Succeed())
		}).WithTimeout(600 * time.Second).Should(Succeed())

		By("verify DPUService is not deleted in the host cluster")
		Eventually(func(g Gomega) {
			svc = &dpuservicev1.DPUService{}
			g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svc)).To(Succeed())
		}).WithTimeout(600 * time.Second).Should(Succeed())

		By("resume dpuservice reconciliation")
		origSvc = svc.DeepCopy()
		svc.Spec.Paused = ptr.To(false)
		Eventually(testClient.Patch).WithArguments(ctx, svc, client.MergeFrom(origSvc)).Should(Succeed())

		// Verify that the DPUServices are deleted
		By("verify DPUServices is deleted in the host cluster")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svc)).ToNot(Succeed())
		}).WithTimeout(600 * time.Second).Should(Succeed())

		dsi := &dpuservicev1.DPUServiceInterface{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: "net1-service"}, dsi)).To(Succeed())
		Expect(utils.CleanupAndWait(ctx, testClient, dsi)).To(Succeed())

		// Check the DPUCluster DPUService is correctly deleted.
		Eventually(func(g Gomega) {
			deploymentList := appsv1.DeploymentList{}
			g.Expect(dpuClusterClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
			g.Expect(deploymentList.Items).To(BeEmpty())
		}).WithTimeout(300 * time.Second).Should(Succeed())

		// Ensure the hostDPUService deployment is deleted from the host cluster.
		Eventually(func(g Gomega) {
			deploymentList := appsv1.DeploymentList{}
			g.Expect(testClient.List(ctx, &deploymentList, client.HasLabels{"app", "release"})).To(Succeed())
			g.Expect(deploymentList.Items).To(BeEmpty())
		}).WithTimeout(300 * time.Second).Should(Succeed())
	})

	It("verify that the ImagePullSecrets have been synced correctly and cleaned up", func() {
		// Verify that we have 2 secrets in the DPU Cluster.
		verifyImagePullSecretsCount(ctx, testClient, dpfOperatorSystemNamespace, 2)

		desiredConf := &operatorv1.DPFOperatorConfig{}
		Eventually(testClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, desiredConf).Should(Succeed())
		currentConf := desiredConf.DeepCopy()

		// Patch the operatorConfig to remove the second secret. This causes the label to be removed.
		desiredConf.Spec.ImagePullSecrets = append(desiredConf.Spec.ImagePullSecrets[:1], desiredConf.Spec.ImagePullSecrets[2:]...)

		Eventually(testClient.Patch).WithArguments(ctx, desiredConf, client.MergeFrom(currentConf)).Should(Succeed())

		// Patch a DPUService to trigger a reconciliation. The DPUService should clean  this secret up from
		// clusters to which it was previously mirrored.
		Eventually(utils.ForceObjectReconcileWithAnnotation).WithArguments(ctx, testClient,
			&dpuservicev1.DPUService{ObjectMeta: metav1.ObjectMeta{Name: operatorv1.MultusName, Namespace: dpfOperatorSystemNamespace}}).Should(Succeed())
		// Verify we only have one image pull secret.
		verifyImagePullSecretsCount(ctx, testClient, dpfOperatorSystemNamespace, 1)
	})
}

func ValidateDPUServiceTemplate(ctx context.Context, input systemTestInput) {
	testClient := input.client
	It("create a DPUServiceTemplate with a chart that doesn't include annotations and expect no versions in status", func() {
		By("creating the DPUServiceTemplate")
		dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
		dpuServiceTemplate.SetName("dpuservice-without-annotations")
		dpuServiceTemplate.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())

		By("checking that status is ready and no versions")
		Eventually(func(g Gomega) {
			gotDPUServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
			g.Expect(testClient.Get(ctx,
				types.NamespacedName{Name: dpuServiceTemplate.GetName(), Namespace: dpuServiceTemplate.GetNamespace()},
				gotDPUServiceTemplate,
			)).To(Succeed())
			g.Expect(gotDPUServiceTemplate.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))

			g.Expect(gotDPUServiceTemplate.Status.Versions).To(BeEmpty())
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("create a DPUServiceTemplate with a chart that includes annotations and expect versions in status", func() {
		By("creating the DPUServiceTemplate")
		dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
		dpuServiceTemplate.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
			Chart:   "dummydpuservice-chart",
			Version: tag,
			// The library is able to handle both unauthenticated and authenticated OCI and Helm Registry. As part of this
			// test we don't cover all these permutations, but based on the provided HELM_REGISTRY, it's possible to test
			// all of them.
			RepoURL: helmRegistry,
		}
		dpuServiceTemplate.SetName("dpuservice-with-annotations")
		dpuServiceTemplate.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())

		By("checking that status is ready and versions are set")
		Eventually(func(g Gomega) {
			gotDPUServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
			g.Expect(testClient.Get(ctx,
				types.NamespacedName{Name: dpuServiceTemplate.GetName(), Namespace: dpuServiceTemplate.GetNamespace()},
				gotDPUServiceTemplate,
			)).To(Succeed())

			g.Expect(gotDPUServiceTemplate.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))

			g.Expect(gotDPUServiceTemplate.Status.Versions).To(HaveKeyWithValue("dpu.nvidia.com/doca-version", ">= 2.9"))
		}).WithTimeout(180 * time.Second).Should(Succeed())
	})

	It("verify DPUServiceTemplate metrics", func() {
		if !deployKSM {
			Skip("Skip KSM metrics accessibility test due to KSM is not deployed")
		}

		By("verify DPUServiceTemplate metrics in KSM")
		expectedMetricsNames := map[string][]string{
			"dpuservicetemplate": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
		}
		Eventually(func(g Gomega) {
			actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})
}

func verifyImagePullSecretsCount(ctx context.Context, c client.Client, namespace string, count int) {
	secrets := &corev1.SecretList{}
	Expect(c.List(ctx, secrets,
		client.InNamespace(namespace),
		client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}),
	).ToNot(HaveOccurred())
	Eventually(func(g Gomega) {
		// Check the imagePullSecrets has been deleted.
		secrets := &corev1.SecretList{}
		g.Expect(dpuClusterClient.List(ctx, secrets,
			client.InNamespace(namespace),
			client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}),
		).To(Succeed())
		g.Expect(secrets.Items).To(HaveLen(count))
	}).WithTimeout(60 * time.Second).Should(Succeed())
}
