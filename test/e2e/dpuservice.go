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
	"maps"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
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

const (
	dpuServiceName          = "dpu-01"
	hostDPUServiceName      = "host-dpu-service"
	dpuServiceNamespace     = "dpu-test-ns"
	dpuServiceInterfaceName = "net1-service"
)

var testNSImagePullSecret = &corev1.Secret{}

// ValidateDPUServiceCreationAndMirroring creates the DPUService in DPU cluster and host cluster.
// It verifies all triggered objects are created and ready.
// Can be used as a test precondition (ex: DPUServiceDeletion test) and as a separate test.
func ValidateDPUServiceCreationAndMirroring(ctx context.Context, input *systemTestInput) {
	By("Create namespace")
	createTestNamespace(ctx, input.client, dpuServiceNamespace)

	By("Create ImagePullSecret for DPUService in user namespace")
	testNSImagePullSecret = generateImagePullSecret(input, dpuServiceNamespace)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, testNSImagePullSecret))).ToNot(HaveOccurred())

	By("Create a DPUServiceInterface")
	dpuServiceInterface := utils.GenerateDPUObj(dpuServiceInterfaceName, dpuServiceNamespace, input.dpuServiceInterface.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	By("Create a DPUService to be deployed on the DPUCluster")
	dpuService := utils.GenerateDPUObj(dpuServiceName, dpuServiceNamespace, input.dpuService.DeepCopy())
	dpuService.Spec.Interfaces = []string{dpuServiceInterfaceName}
	dpuService.Spec.ServiceID = ptr.To("my-service")
	Expect(input.client.Create(ctx, dpuService)).To(Succeed())

	By("Create a DPUService to be deployed on the host cluster")
	hostDPUService := utils.GenerateDPUObj(hostDPUServiceName, dpuServiceNamespace, input.dpuService.DeepCopy())
	hostDPUService.Spec.DeployInCluster = ptr.To(true)
	Expect(input.client.Create(ctx, hostDPUService)).To(Succeed())

	By("Verify DPUServices and deployments are created in DPUCluster")
	verifyKubernetesDeploymentCreated(ctx, dpuClusterClient[0], dpuServiceNamespace)
	verifyImagePullSecretsInCluster(ctx, dpuService.Namespace, testNSImagePullSecret.Name)

	By("Verify DPUService is created in the host cluster")
	verifyKubernetesDeploymentCreated(ctx, input.client, dpuServiceNamespace)
}

func ValidateDPUServiceMetrics(ctx context.Context, input *systemTestInput) {
	By("Create namespace and DPUService")
	createTestNamespace(ctx, input.client, dpuServiceNamespace)
	dpuService := utils.GenerateDPUObj("dpu-01-metrics", dpuServiceNamespace, input.dpuService.DeepCopy())
	Expect(input.client.Create(ctx, dpuService)).To(Succeed())

	By("Verify DPUService metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuservice": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
	}
	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func ValidateDPUServiceDeletion(ctx context.Context, input *systemTestInput) {
	if input.cleanupFlags.SkipCleanup {
		Skip("Skip cleanup resources")
	}
	By("Precondition")
	ValidateDPUServiceCreationAndMirroring(ctx, input)

	By("Pause dpuservice reconciliation")
	svc := &dpuservicev1.DPUService{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svc)).To(Succeed())
	origSvc := svc.DeepCopy()
	svc.Spec.Paused = ptr.To(true)
	Eventually(input.client.Patch).WithArguments(ctx, svc, client.MergeFrom(origSvc)).Should(Succeed())

	svcHost := &dpuservicev1.DPUService{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svcHost)).To(Succeed())
	Expect(svcHost.Spec.Paused).NotTo(BeNil())

	By("Delete the DPUServices")
	svc = &dpuservicev1.DPUService{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: dpuServiceName}, svc)).To(Succeed())
	// Delete the DPUCluster DPUService.
	Expect(input.client.Delete(ctx, svc)).To(Succeed())

	// Delete the host cluster DPUService.
	svcHost = &dpuservicev1.DPUService{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: hostDPUServiceName}, svcHost)).To(Succeed())
	Expect(input.client.Delete(ctx, svcHost)).To(Succeed())

	// Verify that the DPUServices are deleted
	By("Verify DPUServices is deleted in the DPU cluster")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: svc.Name}, svc)).ToNot(Succeed())
	}).WithTimeout(600 * time.Second).Should(Succeed())

	By("Verify DPUService is not deleted in the host cluster")
	Eventually(func(g Gomega) {
		svc = &dpuservicev1.DPUService{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: svcHost.Namespace, Name: svcHost.Name}, svc)).To(Succeed())
	}).WithTimeout(600 * time.Second).Should(Succeed())

	By("Resume dpuservice reconciliation")
	origSvc = svc.DeepCopy()
	svc.Spec.Paused = ptr.To(false)
	Eventually(input.client.Patch).WithArguments(ctx, svc, client.MergeFrom(origSvc)).Should(Succeed())

	// Verify that the DPUServices are deleted
	By("Verify DPUServices is deleted in the host cluster")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: svcHost.Namespace, Name: svcHost.Name}, svc)).ToNot(Succeed())
	}).WithTimeout(600 * time.Second).Should(Succeed())

	dsi := &dpuservicev1.DPUServiceInterface{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuServiceNamespace, Name: dpuServiceInterfaceName}, dsi)).To(Succeed())
	Expect(utils.CleanupAndWait(ctx, input.client, dsi)).To(Succeed())

	// Check the DPUCluster DPUService is correctly deleted.
	Eventually(func(g Gomega) {
		deploymentList := appsv1.DeploymentList{}
		g.Expect(dpuClusterClient[0].List(ctx, &deploymentList, client.HasLabels{"app", "release"}, client.InNamespace(dpuServiceNamespace))).To(Succeed())
		g.Expect(deploymentList.Items).To(BeEmpty())
	}).WithTimeout(300 * time.Second).Should(Succeed())

	// Ensure the hostDPUService deployment is deleted from the host cluster.
	Eventually(func(g Gomega) {
		deploymentList := appsv1.DeploymentList{}
		g.Expect(input.client.List(ctx, &deploymentList, client.HasLabels{"app", "release"}, client.InNamespace(dpuServiceNamespace))).To(Succeed())
		g.Expect(deploymentList.Items).To(BeEmpty())
	}).WithTimeout(300 * time.Second).Should(Succeed())
}

func ValidateImagePullSecretsSync(ctx context.Context, input *systemTestInput) {
	imagePullSecretsSyncTestNamespace := "dpu-test-ns-image-pull-secrets"
	By("Create namespace, DPUServiceInterface and DPUService")
	createTestNamespace(ctx, input.client, imagePullSecretsSyncTestNamespace)

	By("Create ImagePullSecret for DPUService in user namespace")
	testNSImagePullSecret = generateImagePullSecret(input, imagePullSecretsSyncTestNamespace)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, testNSImagePullSecret))).ToNot(HaveOccurred())

	// Verify that we have the precreated secrets + the new secret in the DPU Cluster.
	secretCount := 2
	if ngcAPIKey != "" {
		secretCount += 1
	}
	verifyImagePullSecretsCount(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, secretCount)

	desiredConf := &operatorv1.DPFOperatorConfig{}
	Eventually(input.client.Get).WithArguments(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, desiredConf).Should(Succeed())
	currentConf := desiredConf.DeepCopy()

	// Patch the operatorConfig to remove the second secret. This causes the label to be removed.
	desiredConf.Spec.ImagePullSecrets = append(desiredConf.Spec.ImagePullSecrets[:1], desiredConf.Spec.ImagePullSecrets[2:]...)

	Eventually(input.client.Patch).WithArguments(ctx, desiredConf, client.MergeFrom(currentConf)).Should(Succeed())

	// Patch a DPUService to trigger a reconciliation. The DPUService should clean  this secret up from
	// clusters to which it was previously mirrored.
	Eventually(utils.ForceObjectReconcileWithAnnotation).WithArguments(ctx, input.client,
		&dpuservicev1.DPUService{ObjectMeta: metav1.ObjectMeta{Name: operatorv1.MultusName.String(), Namespace: dpfOperatorSystemNamespace}}).Should(Succeed())
	// Verify that we have only the precreated secrets in the DPU Cluster.
	secretCount = 1
	if ngcAPIKey != "" {
		secretCount += 1
	}
	verifyImagePullSecretsCount(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, secretCount)
}

func ValidateDPUServiceTemplateCreationNoAnnotations(ctx context.Context, input *systemTestInput) {
	By("Creating the DPUServiceTemplate")
	dpuServiceTemplate := utils.GenerateDPUObj(
		"dpuservice-without-annotations-metrics",
		input.dpuServiceTemplate.DeepCopy().Namespace,
		input.dpuServiceTemplate.DeepCopy())
	Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())

	By("Checking that status is ready and no versions")
	Eventually(func(g Gomega) {
		gotDPUServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
		g.Expect(input.client.Get(ctx,
			types.NamespacedName{Name: dpuServiceTemplate.GetName(), Namespace: dpuServiceTemplate.GetNamespace()},
			gotDPUServiceTemplate,
		)).To(Succeed())
		g.Expect(conditions.IsTrue(gotDPUServiceTemplate, conditions.TypeReady)).To(BeTrue())
		g.Expect(gotDPUServiceTemplate.Status.Versions).To(BeEmpty())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func VerifyDPUServiceTemplateCreationWithAnnotations(ctx context.Context, input *systemTestInput) {

	By("Creating the DPUServiceTemplate")
	dpuServiceTemplate := generateDPUServiceTemplate(input, "with-annotations")
	useDummyDPUServiceChart(dpuServiceTemplate)
	Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())

	By("Checking that status is ready and versions are set")
	Eventually(func(g Gomega) {
		gotDPUServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
		g.Expect(input.client.Get(ctx,
			types.NamespacedName{Name: dpuServiceTemplate.GetName(), Namespace: dpuServiceTemplate.GetNamespace()},
			gotDPUServiceTemplate,
		)).To(Succeed())
		g.Expect(conditions.IsTrue(gotDPUServiceTemplate, conditions.TypeReady)).To(BeTrue())
		g.Expect(gotDPUServiceTemplate.Status.Versions).To(HaveKeyWithValue("dpu.nvidia.com/doca-version", ">= 2.9"))
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func VerifyDPUServiceTemplateMetrics(ctx context.Context, input *systemTestInput) {
	By("Create namespace and DPUServiceTemplate")
	dpuServiceTemplate := generateDPUServiceTemplate(input, "-metrics")
	Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())

	By("Verify DPUServiceTemplate metrics in KSM")
	expectedMetricsNames := map[string][]string{
		"dpuservicetemplate": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
	}
	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
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
		g.Expect(dpuClusterClient[0].List(ctx, secrets,
			client.InNamespace(namespace),
			client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}),
		).To(Succeed())
		g.Expect(secrets.Items).To(HaveLen(count))
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

func generateImagePullSecret(input *systemTestInput, dpuServiceNamespace string) *corev1.Secret {
	labels := maps.Clone(CleanupScope.Suite)
	labels[dpuservicev1.DPFImagePullSecretLabelKey] = ""
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.pullSecretNames[0],
			Namespace: dpuServiceNamespace,
			Labels:    labels,
		},
	}
}

func verifyKubernetesDeploymentCreated(ctx context.Context, testClient client.Client, namespace string) {
	Eventually(func(g Gomega) {
		// Check the deployment from the DPUService can be found on the destination cluster.
		deploymentList := appsv1.DeploymentList{}
		g.Expect(testClient.List(ctx, &deploymentList, client.MatchingLabels{"app.kubernetes.io/name": "hello-world"}, client.InNamespace(namespace))).To(Succeed())
		g.Expect(deploymentList.Items).To(HaveLen(1))
	}).WithTimeout(300 * time.Second).Should(Succeed())
}

func verifyImagePullSecretsInCluster(ctx context.Context, namespace string, secretName string) {
	// Check an imagePullSecret was created in the same namespace in the destination cluster.
	Eventually(func(g Gomega) {
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      secretName}, &corev1.Secret{})).To(Succeed())
	}).WithTimeout(300 * time.Second).Should(Succeed())
}
