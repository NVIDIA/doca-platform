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
	"bytes"
	"context"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUDeploymentCreation(ctx context.Context, input *systemTestInput) {
	By("creating the dependencies")
	createDeploymentDependencies(ctx, input, "")

	By("creating the dpudeployment")
	dpuDeployment := generateDPUDeployment(input, "")
	dpuDeployment.SetLabels(afterEachCleanupLabels)
	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	By("checking that the underlying objects are created")
	Eventually(func(g Gomega) {
		g.Expect(VerifyDeploymentUnderlyingObjectsCreated(ctx, g, input.client, dpuDeployment)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

func ValidateDPUDeploymentMetrics(ctx context.Context, input *systemTestInput) {
	By("create DPUDeployment for metrics")
	createDeploymentDependencies(ctx, input, "metrics")
	dpuDeployment := generateDPUDeployment(input, "metrics")
	dpuDeployment.SetLabels(afterEachCleanupLabels)
	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	By("verify DPUDeployment and DPUServiceInterface metrics are in KSM")
	expectedMetricsNames := map[string][]string{
		"dpudeployment": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
		g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
		g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
	}).WithTimeout(5 * time.Second).Should(Succeed())
}

func ValidateDPUDeploymentDeletionWhileDisruptiveUpgradeInProgress(ctx context.Context, input *systemTestInput) {
	// This part is needed so that we can test that the deletion logic is able to delete all the DPUServices, even
	// stale paused ones.

	By("create DPUDeployment until deletion while disruptive upgrade is in progress")
	dpuServiceTemplate := generateDPUServiceTemplate(input, "disruptive")
	Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())
	dpuServiceConfiguration := generateServiceConfiguration(input, "disruptive")
	Expect(input.client.Create(ctx, dpuServiceConfiguration)).To(Succeed())
	dpuDeployment := generateDPUDeployment(input, "disruptive")
	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	By("checking that the underlying objects are created")
	Eventually(func(g Gomega) {
		g.Expect(VerifyDeploymentUnderlyingObjectsCreated(ctx, g, input.client, dpuDeployment)).To(BeTrue())

		// Checking that application exists for the created DPUService. This is needed so that the HACK step is stable.
		gotDPUServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(input.client.List(ctx,
			gotDPUServiceList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())
		g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

		gotApplicationList := &argov1.ApplicationList{}
		g.Expect(input.client.List(ctx, gotApplicationList, client.InNamespace(dpuDeployment.GetNamespace()))).To(Succeed())
		dpuServiceNameToApplication := getDPUServiceNameToApplication(gotDPUServiceList.Items, gotApplicationList.Items)
		g.Expect(dpuServiceNameToApplication).To(HaveLen(1))
	}).WithTimeout(15 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("triggering the disruptive upgrade with bad parameters")
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
	originalDPUServiceConfiguration := dpuServiceConfiguration.DeepCopy()

	dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &machineryruntime.RawExtension{Raw: []byte(`{"image":{"pullPolicy":"malformedPullPolicy"}}`)}
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(originalDPUServiceConfiguration))).To(Succeed())

	By("checking that 2 DPUServices and Applications exist and one of them is paused")
	Eventually(func(g Gomega) {
		gotDPUServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(input.client.List(ctx,
			gotDPUServiceList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())
		g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

		// Checking that applications exist for the both the DPUServices. This is needed so that the HACK step is stable.
		gotApplicationList := &argov1.ApplicationList{}
		g.Expect(input.client.List(ctx, gotApplicationList, client.InNamespace(dpuDeployment.GetNamespace()))).To(Succeed())
		dpuServiceNameToApplication := getDPUServiceNameToApplication(gotDPUServiceList.Items, gotApplicationList.Items)
		g.Expect(dpuServiceNameToApplication).To(HaveLen(2))

		var isAnyServicePaused bool
		for _, dpuService := range gotDPUServiceList.Items {
			if dpuService.IsPaused() {
				isAnyServicePaused = true
			}
			// We expect that no service is deleted
			g.Expect(dpuService.DeletionTimestamp).To(BeNil())
		}
		g.Expect(isAnyServicePaused).To(BeTrue())
		// We run this test repeatedly to ensure that no service is removed
	}).WithTimeout(30 * time.Second).MustPassRepeatedly(5).Should(Succeed())

	By("deleting the dpudeployment")
	Expect(input.client.Delete(ctx, dpuDeployment)).To(Succeed())

	// Failed to apply ArgoCD Application deletion can take up to 5 mins based on the current configuration,
	// therefore we modify the application to have correct configuration so that the deletion goes faster
	// https://github.com/argoproj/argo-cd/blob/e6e92552167ad10ce7ca45c02f5534af6741e710/pkg/apis/application/v1alpha1/types.go#L1473-L1480
	By("HACK: Modify the bad application to ensure that it can be deleted faster")
	Eventually(func(g Gomega) {
		gotDPUServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(input.client.List(ctx,
			gotDPUServiceList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())

		// Ensure that DPUDeployment has marked all the DPUServices for removal and that the dpuservice controller has
		// already ran reconcileDelete once to ensure that the Application spec is no longer patched
		for _, dpuService := range gotDPUServiceList.Items {
			g.Expect(dpuService.DeletionTimestamp).ToNot(BeNil())
			conditionReady := conditions.Get(&dpuService, conditions.TypeReady)
			g.Expect(conditionReady).ToNot(BeNil())
			g.Expect(conditionReady.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(conditionReady.Reason).To(BeEquivalentTo(conditions.ReasonAwaitingDeletion))
		}

		gotApplicationList := &argov1.ApplicationList{}
		g.Expect(input.client.List(ctx, gotApplicationList, client.InNamespace(dpuDeployment.GetNamespace()))).To(Succeed())
		dpuServiceNameToApplication := getDPUServiceNameToApplication(gotDPUServiceList.Items, gotApplicationList.Items)

		// modify Application to have no malformedPullPolicy in their values
		for _, application := range dpuServiceNameToApplication {
			if bytes.Contains(application.Spec.Source.Helm.ValuesObject.Raw, []byte("malformedPullPolicy")) {
				origApp := application.DeepCopy()
				application.Spec.Source.Helm.ValuesObject = &machineryruntime.RawExtension{Raw: []byte(`{}`)}
				g.Expect(input.client.Patch(ctx, &application, client.MergeFrom(origApp))).To(Succeed())
				// Delete the application to ensure that we haven't recreated the application in the meantime with the
				// patch above
				g.Expect(input.client.Delete(ctx, &application)).To(Succeed())
			}
		}

		// ensure Applications don't have malformedPullPolicy in their values
		gotApplicationList = &argov1.ApplicationList{}
		g.Expect(input.client.List(ctx, gotApplicationList, client.InNamespace(dpuDeployment.GetNamespace()))).To(Succeed())
		dpuServiceNameToApplication = getDPUServiceNameToApplication(gotDPUServiceList.Items, gotApplicationList.Items)

		for _, application := range dpuServiceNameToApplication {
			g.Expect(bytes.Contains(application.Spec.Source.Helm.ValuesObject.Raw, []byte("malformedPullPolicy"))).To(BeFalse())
		}
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("checking that the underlying objects are deleted")
	Eventually(func(g Gomega) {
		gotDPUSetList := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx,
			gotDPUSetList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())
		g.Expect(gotDPUSetList.Items).To(BeEmpty())

		gotDPUServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(input.client.List(ctx,
			gotDPUServiceList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())
		g.Expect(gotDPUServiceList.Items).To(BeEmpty())

		gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
		g.Expect(input.client.List(ctx,
			gotDPUServiceChainList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())
		g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

		gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
		g.Expect(input.client.List(ctx,
			gotDPUServiceInterfaceList,
			client.InNamespace(dpuDeployment.GetNamespace()),
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
			})).To(Succeed())
		g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())

		// Expect the DPUDeployment to be deleted
		err := input.client.Get(ctx, client.ObjectKey{Namespace: dpuDeployment.GetNamespace(), Name: dpuDeployment.GetName()}, &dpuservicev1.DPUDeployment{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}).WithTimeout(180 * time.Second).Should(Succeed())

	By("cleanup DPUServiceConfiguration and DPUServiceTemplate")
	Expect(input.client.Delete(ctx, dpuServiceTemplate)).To(Succeed())
	Expect(input.client.Delete(ctx, dpuServiceConfiguration)).To(Succeed())
}

func VerifyDeploymentUnderlyingObjectsCreated(ctx context.Context, g Gomega, testClient client.Client, dpuDeployment *dpuservicev1.DPUDeployment) bool {
	serviceCount := len(dpuDeployment.Spec.Services)
	gotDPUSetList := &provisioningv1.DPUSetList{}
	g.Expect(testClient.List(ctx,
		gotDPUSetList,
		client.InNamespace(dpuDeployment.GetNamespace()),
		client.MatchingLabels{
			"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
		})).To(Succeed())
	g.Expect(gotDPUSetList.Items).To(HaveLen(1))

	gotDPUServiceList := &dpuservicev1.DPUServiceList{}
	g.Expect(testClient.List(ctx,
		gotDPUServiceList,
		client.InNamespace(dpuDeployment.GetNamespace()),
		client.MatchingLabels{
			"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
		})).To(Succeed())
	g.Expect(gotDPUServiceList.Items).To(HaveLen(serviceCount))

	gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
	g.Expect(testClient.List(ctx,
		gotDPUServiceChainList,
		client.InNamespace(dpuDeployment.GetNamespace()),
		client.MatchingLabels{
			"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
		})).To(Succeed())
	g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

	gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	g.Expect(testClient.List(ctx,
		gotDPUServiceInterfaceList,
		client.InNamespace(dpuDeployment.GetNamespace()),
		client.MatchingLabels{
			"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
		})).To(Succeed())
	return g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(serviceCount))
	// A couple of dependencies must become ready before the DPUDeployment can take action, therefore we have to wait
	// a little longer here.
}

func ValidateDPUDeploymentFullCreation(ctx context.Context, input *systemTestInput) {
	By("delete DPUs and DPUSets and ensure they are deleted for a clean test condition")
	if input.skipCleanup {
		Skip("Skip cleanup resources")
	}
	Eventually(func(g Gomega) {
		dpuSetList := &provisioningv1.DPUSetList{}
		dpuList := &provisioningv1.DPUList{}
		g.Expect(client.IgnoreNotFound(input.client.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())
		g.Expect(input.client.List(ctx, dpuSetList)).To(Succeed())
		g.Expect(dpuSetList.Items).To(BeEmpty())

		// Expect all DPUs to have been deleted.
		g.Expect(input.client.List(ctx, dpuList)).To(Succeed())
		g.Expect(dpuList.Items).To(BeEmpty())

		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient.List(ctx, nodes)).To(Succeed())
		By(fmt.Sprintf("Expected number of nodes %d to equal %d", len(nodes.Items), 0))
		g.Expect(nodes.Items).To(BeEmpty())
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	By("create DPUServiceIPAM to be used by dpuDeployment")
	dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetLabels(afterAllCleanupLabels)
	dpuServiceIPAM.SetName("dpudeployment-ipam-pool1")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	// Remove selectors so it applies to all nodes/clusters
	dpuServiceIPAM.Spec.NodeSelector = nil
	dpuServiceIPAM.Spec.ClusterSelector = nil
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("create a DPUDeployment with its dependencies and ensure that the underlying objects are created")
	dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
	dpuServiceTemplate.SetLabels(afterAllCleanupLabels)
	useDummyDPUServiceChart(dpuServiceTemplate)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceTemplate))).To(Succeed())

	dpuServiceTemplate2 := input.dpuServiceTemplate.DeepCopy()
	dpuServiceTemplate2.SetLabels(afterAllCleanupLabels)
	dpuServiceTemplate2.SetName("dpudeployment-example2-servicetemplate")
	dpuServiceTemplate2.Spec.DeploymentServiceName = "example2"
	useDummyDPUServiceChart(dpuServiceTemplate2)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceTemplate2))).To(Succeed())

	dpuServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.SetLabels(afterAllCleanupLabels)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceConfiguration))).To(Succeed())

	dpuServiceConfiguration2 := input.dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration2.SetLabels(afterAllCleanupLabels)
	dpuServiceConfiguration2.SetName("dpudeployment-example2-serviceconfiguration")
	dpuServiceConfiguration2.Spec.DeploymentServiceName = "example2"
	dpuServiceConfiguration2.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "net2", Network: "mybrsfc"}}
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceConfiguration2))).To(Succeed())

	dpuDeployment := generateDPUObj("dpf-dpudeployment", input.dpuDeployment.DeepCopy().Namespace, input.dpuDeployment.DeepCopy(), afterAllCleanupLabels)
	dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"feature.node.kubernetes.io/dpu-enabled": "true"},
	}
	// Add example2 service to the DPUDeployment
	dpuDeployment.Spec.Services["example2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
		ServiceTemplate:      "dpudeployment-example2-servicetemplate",
		ServiceConfiguration: "dpudeployment-example2-serviceconfiguration",
	}
	// Update the switch to map net1 to net2
	dpuDeployment.Spec.ServiceChains.Switches[0] = dpuservicev1.DPUDeploymentSwitch{
		Ports: []dpuservicev1.DPUDeploymentPort{
			{
				Service: &dpuservicev1.DPUDeploymentService{
					InterfaceName: "net1",
					Name:          "example",
					IPAM: &dpuservicev1.IPAM{
						MatchLabels: map[string]string{
							"svc.dpu.nvidia.com/pool": "pool1",
						},
					},
				},
			},
			{
				Service: &dpuservicev1.DPUDeploymentService{
					InterfaceName: "net2",
					Name:          "example2",
					IPAM: &dpuservicev1.IPAM{
						MatchLabels: map[string]string{
							"svc.dpu.nvidia.com/pool": "pool1",
						},
					},
				},
			},
		},
	}
	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	Eventually(func(g Gomega) {
		g.Expect(VerifyDeploymentUnderlyingObjectsCreated(ctx, g, input.client, dpuDeployment)).To(BeTrue())
	}).WithTimeout(180 * time.Second).Should(Succeed())

	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		if !input.hasDpuNodes() {
			return
		}
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient.List(ctx, nodes)).To(Succeed())
		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), input.numberOfDPUNodes)
		tracker.By(nodeKey, "Checking that the number of nodes %d is equal to %d", len(nodes.Items), input.numberOfDPUNodes)
		g.Expect(nodes.Items).To(HaveLen(input.numberOfDPUNodes))
	}).WithTimeout(45 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("create DPUServiceInterface and check that it is mirrored to each cluster")
	dpuServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test-dpudeployment"
	By("create test namespace")
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceNamespace}}
	testNS.SetLabels(afterAllCleanupLabels)
	Expect(input.client.Create(ctx, testNS)).To(Succeed())
	By("create DPUServiceInterface")
	dpuServiceInterface := input.dpuServiceInterface.DeepCopy()
	dpuServiceInterface.SetName(dpuServiceInterfaceName)
	dpuServiceInterface.SetNamespace(dpuServiceInterfaceNamespace)
	dpuServiceInterface.SetLabels(afterAllCleanupLabels)
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	serviceInterfaceLabels := map[string]string{}
	By("verify ServiceInterfaceSet is created in DPF clusters")
	Eventually(func(g Gomega) {
		//get the DPUServiceInterface owned by DPUDeployment
		dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
		g.Expect(input.client.List(ctx, dpuServiceInterfaceList,
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName())})).
			To(Succeed())
		g.Expect(dpuServiceInterfaceList.Items).To(HaveLen(2))
		// getting labels for ServiceInterface check
		serviceInterfaceLabels = dpuServiceInterfaceList.Items[0].Spec.Template.Spec.Template.Labels

		// verify ServiceInterfaceSet is created in namespace
		serviceInterfaceSetListInNamespace := &dpuservicev1.ServiceInterfaceSetList{}
		g.Expect(dpuClusterClient.List(ctx, serviceInterfaceSetListInNamespace,
			client.InNamespace(dpuServiceInterfaceNamespace),
		)).To(Succeed())
		g.Expect(serviceInterfaceSetListInNamespace.Items).To(HaveLen(1))
	}, time.Second*300, time.Millisecond*250).Should(Succeed())

	if Label(scaleLabel).MatchesLabelFilter(GinkgoLabelFilter()) {
		// mock-DMS / scale scenario: serviceChains and ServiceInterfaces cannot
		// reach ready state in mock-dms nodes
		return
	}

	if !input.hasDpuNodes() {
		return
	}

	By("Verifying DPUs are provisioned")
	VerifyDPUClusterWithNodes(ctx, ProvisionDPUClustersInput{
		numberOfNodesPerCluster: input.numberOfDPUNodes,
		client:                  input.client,
	})

	By(fmt.Sprintf("verify ServiceInterface is created in %d nodes", input.numberOfDPUNodes))
	Eventually(func(g Gomega) {
		serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
		g.Expect(dpuClusterClient.List(ctx, serviceInterfaceList, client.MatchingLabels(serviceInterfaceLabels))).To(Succeed())
		g.Expect(serviceInterfaceList.Items).To(HaveLen(input.numberOfDPUNodes))
	}).WithTimeout(15 * time.Minute).WithPolling(120 * time.Second).Should(Succeed())

	By("verifying that the dpuDeployment is ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

func createDeploymentDependencies(ctx context.Context, input *systemTestInput, nameDiff string) {
	dpuServiceTemplate := generateDPUServiceTemplate(input, nameDiff)
	useDummyDPUServiceChart(dpuServiceTemplate)
	Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())
	dpuServiceConfiguration := generateServiceConfiguration(input, nameDiff)
	Expect(input.client.Create(ctx, dpuServiceConfiguration)).To(Succeed())
}

func generateDPUServiceTemplate(input *systemTestInput, nameDiff string) *dpuservicev1.DPUServiceTemplate {
	if nameDiff != "" {
		nameDiff = "-" + nameDiff
	}
	dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
	dpuServiceTemplate.SetLabels(afterAllCleanupLabels)
	dpuServiceTemplate.SetName(dpuServiceTemplate.GetName() + nameDiff)
	dpuServiceTemplate.Spec.DeploymentServiceName += nameDiff
	return dpuServiceTemplate
}

func useDummyDPUServiceChart(dpuServiceTemplate *dpuservicev1.DPUServiceTemplate) {
	dpuServiceTemplate.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}
	dpuServiceTemplate.Spec.HelmChart.Values = nil
}

func generateServiceConfiguration(input *systemTestInput, nameDiff string) *dpuservicev1.DPUServiceConfiguration {
	if nameDiff != "" {
		nameDiff = "-" + nameDiff
	}
	dpuServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.SetLabels(afterAllCleanupLabels)
	dpuServiceConfiguration.SetName(dpuServiceConfiguration.GetName() + nameDiff)
	dpuServiceConfiguration.Spec.DeploymentServiceName += nameDiff
	return dpuServiceConfiguration
}

func generateDPUDeployment(input *systemTestInput, nameDiff string) *dpuservicev1.DPUDeployment {
	if nameDiff != "" {
		nameDiff = "-" + nameDiff
	}
	dpuDeployment := input.dpuDeployment.DeepCopy()
	dpuDeployment.SetLabels(afterAllCleanupLabels)
	dpuDeployment.SetName(dpuDeployment.GetName() + nameDiff)
	currentSpecService := dpuDeployment.Spec.Services

	newSpecServiceConfig := make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
	for key, specService := range currentSpecService {
		newSpecServiceConfig[key+nameDiff] = dpuservicev1.DPUDeploymentServiceConfiguration{
			ServiceTemplate:      specService.ServiceTemplate + nameDiff,
			ServiceConfiguration: specService.ServiceConfiguration + nameDiff,
		}

	}
	dpuDeployment.Spec.Services = newSpecServiceConfig
	return dpuDeployment
}

// getDPUServiceNameToApplicationName returns a map that has as key the DPUService name and as value the Application name
// associated with this DPUService. This function is not namespace safe.
func getDPUServiceNameToApplication(dpuServices []dpuservicev1.DPUService, applications []argov1.Application) map[string]argov1.Application {
	dpuServiceNameToApplicationName := make(map[string]argov1.Application)
	for _, app := range applications {
		dpuServiceName, ok := app.Labels[dpuservicev1.DPUServiceNameLabelKey]
		if !ok {
			continue
		}
		dpuServiceNamespace, ok := app.Labels[dpuservicev1.DPUServiceNamespaceLabelKey]
		if !ok {
			continue
		}
		for _, dpuService := range dpuServices {
			if dpuServiceName == dpuService.Name && dpuServiceNamespace == dpuService.Namespace {
				dpuServiceNameToApplicationName[dpuService.Name] = app
			}
		}
	}
	return dpuServiceNameToApplicationName
}
