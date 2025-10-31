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
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// NodeUnschedulableTaintKey is the well known taint key that indicates a node is unschedulable, typically used when
	// draining a node
	NodeUnschedulableTaintKey = "node.kubernetes.io/unschedulable"
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
		actualMetricsNames := metrics.GetKSMMetrics(ctx, hostClusterRESTClient, metricsURI)
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
	// TODO: Delete DPUSet not owned by DPUDeployment
	By("delete DPUs and DPUSets and ensure they are deleted for a clean test condition")
	Eventually(func(g Gomega) {
		dpuSetList := &provisioningv1.DPUSetList{}

		g.Expect(client.IgnoreNotFound(input.client.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())
		g.Expect(input.client.List(ctx, dpuSetList)).To(Succeed())
		g.Expect(dpuSetList.Items).To(BeEmpty())

		// Expect all DPUs to have been deleted.
		dpuList := &provisioningv1.DPUList{}
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
	dpuServiceTemplate := generateDPUServiceTemplate(input, "")
	useDummyDPUServiceChart(dpuServiceTemplate)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceTemplate))).To(Succeed())

	dpuServiceConfiguration := generateServiceConfiguration(input, "")
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceConfiguration))).To(Succeed())

	dpuServiceTemplate2 := generateDPUServiceTemplate(input, "2")
	useDummyDPUServiceChart(dpuServiceTemplate2)
	Expect(input.client.Create(ctx, dpuServiceTemplate2)).To(Succeed())

	dpuServiceConfiguration2 := generateServiceConfiguration(input, "2")
	dpuServiceConfiguration2.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "net2", Network: "mybrsfc"}}
	Expect(input.client.Create(ctx, dpuServiceConfiguration2)).To(Succeed())

	dpuDeployment := generateDPUObj("dpf-dpudeployment", input.dpuDeployment.DeepCopy().Namespace, input.dpuDeployment.DeepCopy(), afterAllCleanupLabels)
	dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"feature.node.kubernetes.io/dpu-enabled": "true"},
	}
	// Add example2 service to the DPUDeployment
	dpuDeployment.Spec.Services["example-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
		ServiceTemplate:      "dpudeployment-example-servicetemplate-2",
		ServiceConfiguration: "dpudeployment-example-serviceconfiguration-2",
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
					Name:          "example-2",
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

// ValidateDPUDeploymentDPUServiceDisruptiveUpgrade validates that DPUDeployment disruptive upgrade flow for standard
// DPUServices works as expected
func ValidateDPUDeploymentDPUServiceDisruptiveUpgrade(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	By("Patching the provisioning controller to apply node effect sequentially")
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, dpfOperatorConfig)).To(Succeed())
	originalDPFOperatorConfig := dpfOperatorConfig.DeepCopy()

	// Set MaxUnavailableDPUNodes to 1 to ensure only one node is upgraded at a time
	if dpfOperatorConfig.Spec.ProvisioningController == nil {
		dpfOperatorConfig.Spec.ProvisioningController = &operatorv1.ProvisioningControllerConfiguration{}
	}
	dpfOperatorConfig.Spec.ProvisioningController.MaxUnavailableDPUNodes = ptr.To(int32(1))
	Expect(input.client.Patch(ctx, dpfOperatorConfig, client.MergeFrom(originalDPFOperatorConfig))).To(Succeed())

	By("Validating that the DPFOperatorConfig is ready for the current generation")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		// TODO: Replace with conditions.IsReady() when we start checking for correct generation in the function
		readyCondition := conditions.Get(dpfOperatorConfig, conditions.TypeReady)
		g.Expect(readyCondition).NotTo(BeNil())
		g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(readyCondition.ObservedGeneration).To(Equal(dpfOperatorConfig.Generation))
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Getting the existing DPUDeployment")
	// Get the DPUDeployment created in ValidateDPUDeploymentFullCreation
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	dpuDeployment.SetName("dpf-dpudeployment")
	dpuDeployment.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())

	By("Getting the DPUServiceConfiguration for example service")
	dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
	dpuServiceConfiguration.SetName("dpudeployment-example-serviceconfiguration")
	dpuServiceConfiguration.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())

	serviceIDForExample := "dpudeployment_dpf-dpudeployment_example"
	By("Getting initial pods for example service in DPU cluster")
	var initialPods []corev1.Pod
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient.List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.numberOfDPUNodes))
		initialPods = podList.Items
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Getting the mapping between host nodes and pods running on the DPU cluster on a DPU that is part of that node")
	// Get all nodes in the DPU cluster
	dpuClusterNodes := &corev1.NodeList{}
	Expect(dpuClusterClient.List(ctx, dpuClusterNodes)).To(Succeed())

	// Create a map from DPU cluster node name to host node name using the provisioning.dpu.nvidia.com/host label
	dpuClusterNodeToHostNodeMap := make(map[string]string)
	for _, node := range dpuClusterNodes.Items {
		if hostNodeName, ok := node.Labels["provisioning.dpu.nvidia.com/host"]; ok {
			dpuClusterNodeToHostNodeMap[node.Name] = hostNodeName
		}
	}

	// Create a map from host node name to pods running on the DPU cluster
	hostNodeToPodMap := make(map[string]corev1.Pod)
	for _, pod := range initialPods {
		// pod.Spec.NodeName is the name of the node in the DPU cluster
		hostNodeName, ok := dpuClusterNodeToHostNodeMap[pod.Spec.NodeName]
		if !ok {
			continue
		}
		hostNodeToPodMap[hostNodeName] = pod
	}
	Expect(hostNodeToPodMap).To(HaveLen(input.numberOfDPUNodes), "Expected to find a pod for each host node")

	By("Modifying the DPUServiceConfiguration by adding an extra label")
	originalDPUServiceConfiguration := dpuServiceConfiguration.DeepCopy()
	if dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels == nil {
		dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = make(map[string]string)
	}
	dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels["test-disruptive-upgrade"] = "true"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(originalDPUServiceConfiguration))).To(Succeed())

	By("Checking that one of the nodes is drained")
	var drainedHostNode *corev1.Node
	Eventually(func(g Gomega) {
		gotHostNodeList := &corev1.NodeList{}
		labelSelectorForNodes, err := utils.LabelSelectorAsSelector(dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector)
		Expect(err).ToNot(HaveOccurred())
		Expect(input.client.List(ctx, gotHostNodeList, client.MatchingLabelsSelector{Selector: labelSelectorForNodes})).To(Succeed())
		Expect(gotHostNodeList.Items).To(HaveLen(input.numberOfDPUNodes))

		for _, node := range gotHostNodeList.Items {
			// Check if node has the unschedulable tain (drain)
			for _, taint := range node.Spec.Taints {
				if taint.Key == NodeUnschedulableTaintKey && taint.Effect == corev1.TaintEffectNoSchedule {
					drainedHostNode = &node
					break
				}
			}
		}
		g.Expect(drainedHostNode).ToNot(BeNil(), "At least one node should be drained")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Checking that the pod running on the DPU which belongs to the drained node is replaced by a new one while the other DPU has its pod intact")
	var newPod *corev1.Pod
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient.List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.numberOfDPUNodes))

		// Verify that the old pod on the drained node is replaced with a new one
		foundOldPodOnDrainedNode := false
		foundNewPodOnDrainedNode := false
		// Verify that non-drained nodes still have their original pods (without the new label)
		foundOldPodOnNonDrainedNode := false
		foundNewPodOnNonDrainedNode := false

		for _, pod := range podList.Items {
			// Find the host node name that the DPU cluster node is related to
			podHostNodeName, hostExists := dpuClusterNodeToHostNodeMap[pod.Spec.NodeName]
			if !hostExists {
				continue
			}

			// Check if pod has the new label (new pod) or not (old pod)
			_, podHasNewLabel := pod.Labels["test-disruptive-upgrade"]
			podIsOnDrainedNode := podHostNodeName == drainedHostNode.Name

			switch {
			case podIsOnDrainedNode && podHasNewLabel:
				foundNewPodOnDrainedNode = true
				newPod = &pod
			case podIsOnDrainedNode && !podHasNewLabel:
				foundOldPodOnDrainedNode = true
			case !podIsOnDrainedNode && podHasNewLabel:
				foundNewPodOnNonDrainedNode = true
			case !podIsOnDrainedNode && !podHasNewLabel:
				foundOldPodOnNonDrainedNode = true
			}
		}

		// Drained node should have new pod, not old pod
		g.Expect(foundOldPodOnDrainedNode).To(BeFalse(), "Old pod without new label should be removed from drained node")
		g.Expect(foundNewPodOnDrainedNode).To(BeTrue(), "New pod with new label should exist on drained node")
		// Non-drained nodes should have old pods, not new pods
		g.Expect(foundOldPodOnNonDrainedNode).To(BeTrue(), "Old pod without new label should still exist on non-drained nodes")
		g.Expect(foundNewPodOnNonDrainedNode).To(BeFalse(), "New pod with new label should not exist on non-drained nodes yet")
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	By("Check that the drain is not removed until the new pod is ready")
	Eventually(func(g Gomega) {
		// Get the pod to understand if it's ready or not
		gotPod := &corev1.Pod{}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(newPod), gotPod)).To(Succeed())

		// Determine whether pod is ready
		isPodReady := false
		for _, condition := range gotPod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				isPodReady = true
				break
			}
		}

		// Get the node to understand if it's tainted or not
		node := &corev1.Node{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Name: drainedHostNode.Name}, node)).To(Succeed())

		// Determine whether node is drained
		isNodeDrained := false
		for _, taint := range node.Spec.Taints {
			if taint.Key == NodeUnschedulableTaintKey && taint.Effect == corev1.TaintEffectNoSchedule {
				isNodeDrained = true
				break
			}
		}

		if isPodReady {
			g.Expect(isNodeDrained).To(BeFalse(), "Drain should be removed from the node when new pod is ready")
		} else {
			g.Expect(isNodeDrained).To(BeTrue(), "Drain should not be removed from the node until new pod is ready")
		}

		// Get out of this loop when the pod finally becomes ready and the node is not drained anymore
		g.Expect(isPodReady).To(BeTrue())
		g.Expect(isNodeDrained).To(BeFalse())
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying that the DPUDeployment becomes ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying all pods are running the new configuration")
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient.List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.numberOfDPUNodes))

		// Verify all pods have the new label
		for _, pod := range podList.Items {
			g.Expect(pod.Labels).To(HaveKeyWithValue("test-disruptive-upgrade", "true"))
		}
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		resetConfig := dpfOperatorConfig.DeepCopy()
		resetConfig.Spec = originalDPFOperatorConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(dpfOperatorConfig))).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Validating that the DPFOperatorConfig is ready for the current generation")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		// TODO: Replace with conditions.IsReady() when we start checking for correct generation in the function
		readyCondition := conditions.Get(dpfOperatorConfig, conditions.TypeReady)
		g.Expect(readyCondition).NotTo(BeNil())
		g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(readyCondition.ObservedGeneration).To(Equal(dpfOperatorConfig.Generation))
	}).WithTimeout(2 * time.Minute).Should(Succeed())
}

// ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade validates that DPUDeployment disruptive upgrade flow for
// in-cluster DPUServices works as expected
func ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade(ctx context.Context, input *systemTestInput) {
}

// ValidateDPUDeploymentDPUServiceChainDisruptiveUpgrade validates that DPUDeployment disruptive upgrade flow for
// DPUServiceChain works as expected
func ValidateDPUDeploymentDPUServiceChainDisruptiveUpgrade(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	By("Patching the provisioning controller to apply node effect sequentially")
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, dpfOperatorConfig)).To(Succeed())
	originalDPFOperatorConfig := dpfOperatorConfig.DeepCopy()

	// Set MaxUnavailableDPUNodes to 1 to ensure only one node is upgraded at a time
	if dpfOperatorConfig.Spec.ProvisioningController == nil {
		dpfOperatorConfig.Spec.ProvisioningController = &operatorv1.ProvisioningControllerConfiguration{}
	}
	dpfOperatorConfig.Spec.ProvisioningController.MaxUnavailableDPUNodes = ptr.To(int32(1))
	Expect(input.client.Patch(ctx, dpfOperatorConfig, client.MergeFrom(originalDPFOperatorConfig))).To(Succeed())

	By("Validating that the DPFOperatorConfig is ready for the current generation")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		// TODO: Replace with conditions.IsReady() when we start checking for correct generation in the function
		readyCondition := conditions.Get(dpfOperatorConfig, conditions.TypeReady)
		g.Expect(readyCondition).NotTo(BeNil())
		g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(readyCondition.ObservedGeneration).To(Equal(dpfOperatorConfig.Generation))
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Getting the existing DPUDeployment")
	// Get the DPUDeployment created in ValidateDPUDeploymentFullCreation
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	dpuDeployment.SetName("dpf-dpudeployment")
	dpuDeployment.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())

	By("Getting initial ServiceChains in DPU cluster")
	var initialServiceChains []dpuservicev1.ServiceChain
	Eventually(func(g Gomega) {
		serviceChainList := &dpuservicev1.ServiceChainList{}
		g.Expect(dpuClusterClient.List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(serviceChainList.Items).To(HaveLen(input.numberOfDPUNodes))
		initialServiceChains = serviceChainList.Items
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Getting the mapping between host nodes and ServiceChains existing in the DPU cluster on a DPU that is part of that node")
	// Get all DPUs in the system
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	// Get all DPUNodes in the system
	dpuNodeList := &provisioningv1.DPUNodeList{}
	Expect(input.client.List(ctx, dpuNodeList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	// Create a map from DPUNode name to host node name (via KubeNodeRef)
	dpuNodeToHostNodeMap := make(map[string]string)
	for _, dpuNode := range dpuNodeList.Items {
		if dpuNode.Status.KubeNodeRef != nil {
			dpuNodeToHostNodeMap[dpuNode.Name] = *dpuNode.Status.KubeNodeRef
		}
	}

	// Create a map from DPU name to DPUNode name
	dpuToDPUNodeMap := make(map[string]string)
	for _, dpu := range dpuList.Items {
		dpuToDPUNodeMap[dpu.Name] = dpu.Spec.DPUNodeName
	}

	// Create a map from host node name to ServiceChains running on the DPU cluster
	// The node name in the ServiceChain spec is the same as the DPU object name
	hostNodeToServiceChainMap := make(map[string]dpuservicev1.ServiceChain)
	for _, serviceChain := range initialServiceChains {
		Expect(serviceChain.Spec.Node).ToNot(BeNil())
		dpuNodeName, ok := dpuToDPUNodeMap[*serviceChain.Spec.Node]
		if !ok {
			continue
		}
		hostNodeName, ok := dpuNodeToHostNodeMap[dpuNodeName]
		if !ok {
			continue
		}
		hostNodeToServiceChainMap[hostNodeName] = serviceChain
	}
	Expect(hostNodeToServiceChainMap).To(HaveLen(input.numberOfDPUNodes), "Expected to find a ServiceChain for each host node")

	By("Modifying the DPUDeployment ServiceChains by changing ServiceMTU")
	originalDPUDeployment := dpuDeployment.DeepCopy()
	// Change the ServiceMTU to trigger a disruptive upgrade
	newMTU := 1300
	dpuDeployment.Spec.ServiceChains.Switches[0].ServiceMTU = &newMTU
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(originalDPUDeployment))).To(Succeed())

	By("Checking that one of the nodes is drained")
	var drainedHostNode *corev1.Node
	Eventually(func(g Gomega) {
		gotHostNodeList := &corev1.NodeList{}
		labelSelectorForNodes, err := utils.LabelSelectorAsSelector(dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector)
		Expect(err).ToNot(HaveOccurred())
		Expect(input.client.List(ctx, gotHostNodeList, client.MatchingLabelsSelector{Selector: labelSelectorForNodes})).To(Succeed())
		Expect(gotHostNodeList.Items).To(HaveLen(input.numberOfDPUNodes))

		for _, node := range gotHostNodeList.Items {
			// Check if node has the unschedulable taint (drain)
			for _, taint := range node.Spec.Taints {
				if taint.Key == NodeUnschedulableTaintKey && taint.Effect == corev1.TaintEffectNoSchedule {
					drainedHostNode = &node
					break
				}
			}
		}
		g.Expect(drainedHostNode).ToNot(BeNil(), "At least one node should be drained")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Checking that the ServiceChain on the DPU cluster is updated on the drained node while others remain unchanged")
	var newServiceChain *dpuservicev1.ServiceChain
	Eventually(func(g Gomega) {
		serviceChainList := &dpuservicev1.ServiceChainList{}
		g.Expect(dpuClusterClient.List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Verify that we have service chains on both nodes
		g.Expect(serviceChainList.Items).To(HaveLen(input.numberOfDPUNodes))

		// Track which nodes have new vs old service chains
		foundOldServiceChainOnDrainedNode := false
		foundNewServiceChainOnDrainedNode := false
		foundOldServiceChainOnNonDrainedNode := false
		foundNewServiceChainOnNonDrainedNode := false

		for _, serviceChain := range serviceChainList.Items {
			g.Expect(serviceChain.Spec.Node).ToNot(BeNil())

			// Find the DPUNode name that is related to the DPU this ServiceChain belongs to
			serviceChainDPUNodeName, dpuNodeExists := dpuToDPUNodeMap[*serviceChain.Spec.Node]
			if !dpuNodeExists {
				continue
			}

			// Find the host node name that the above DPUNode is related to
			serviceChainHostNodeName, hostExists := dpuNodeToHostNodeMap[serviceChainDPUNodeName]
			if !hostExists {
				continue
			}

			// Check if this is a new ServiceChain
			previousServiceChain := hostNodeToServiceChainMap[serviceChainHostNodeName]
			isNewServiceChain := previousServiceChain.UID != serviceChain.UID
			serviceChainIsOnDrainedNode := serviceChainHostNodeName == drainedHostNode.Name

			switch {
			case serviceChainIsOnDrainedNode && isNewServiceChain:
				foundNewServiceChainOnDrainedNode = true
				newServiceChain = &serviceChain
			case serviceChainIsOnDrainedNode && !isNewServiceChain:
				foundOldServiceChainOnDrainedNode = true
			case !serviceChainIsOnDrainedNode && isNewServiceChain:
				foundNewServiceChainOnNonDrainedNode = true
			case !serviceChainIsOnDrainedNode && !isNewServiceChain:
				foundOldServiceChainOnNonDrainedNode = true
			}
		}

		// Drained node should have new ServiceChain, not old ServiceChain
		g.Expect(foundOldServiceChainOnDrainedNode).To(BeFalse(), "Old ServiceChain should be removed from drained node")
		g.Expect(foundNewServiceChainOnDrainedNode).To(BeTrue(), "New ServiceChain should exist on drained node")
		// Non-drained nodes should have old ServiceChains, not new ServiceChains
		g.Expect(foundOldServiceChainOnNonDrainedNode).To(BeTrue(), "Old ServiceChain should still exist on non-drained nodes")
		g.Expect(foundNewServiceChainOnNonDrainedNode).To(BeFalse(), "New ServiceChain should not exist on non-drained nodes yet")
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Check that the drain is not removed until the new ServiceChain is ready")
	Eventually(func(g Gomega) {
		// Get the ServiceChain to understand if it's ready or not
		gotServiceChain := &dpuservicev1.ServiceChain{}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(newServiceChain), gotServiceChain)).To(Succeed())

		// Determine whether ServiceChain is ready
		isServiceChainReady := conditions.IsTrue(gotServiceChain, conditions.TypeReady)

		// Get the node to understand if it's tainted or not
		node := &corev1.Node{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Name: drainedHostNode.Name}, node)).To(Succeed())

		// Determine whether node is drained
		isNodeDrained := false
		for _, taint := range node.Spec.Taints {
			if taint.Key == NodeUnschedulableTaintKey && taint.Effect == corev1.TaintEffectNoSchedule {
				isNodeDrained = true
				break
			}
		}

		if isServiceChainReady {
			g.Expect(isNodeDrained).To(BeFalse(), "Drain should be removed from the node when new ServiceChain is ready")
		} else {
			g.Expect(isNodeDrained).To(BeTrue(), "Drain should not be removed from the node until new ServiceChain is ready")
		}

		// Get out of this loop when the ServiceChain finally becomes ready and the drain is removed
		g.Expect(isServiceChainReady).To(BeTrue())
		g.Expect(isNodeDrained).To(BeFalse())
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying that the DPUDeployment becomes ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying all ServiceChains are running the new configuration")
	Eventually(func(g Gomega) {
		serviceChainList := &dpuservicev1.ServiceChainList{}
		g.Expect(dpuClusterClient.List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(serviceChainList.Items).To(HaveLen(input.numberOfDPUNodes))

		// Verify all ServiceChains have the new MTU
		for _, serviceChain := range serviceChainList.Items {
			g.Expect(serviceChain.Spec.Switches).To(HaveLen(1))
			g.Expect(serviceChain.Spec.Switches[0].ServiceMTU).ToNot(BeNil())
			g.Expect(*serviceChain.Spec.Switches[0].ServiceMTU).To(Equal(1300))
		}
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		resetConfig := dpfOperatorConfig.DeepCopy()
		resetConfig.Spec = originalDPFOperatorConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(dpfOperatorConfig))).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Validating that the DPFOperatorConfig is ready for the current generation")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		// TODO: Replace with conditions.IsReady() when we start checking for correct generation in the function
		readyCondition := conditions.Get(dpfOperatorConfig, conditions.TypeReady)
		g.Expect(readyCondition).NotTo(BeNil())
		g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(readyCondition.ObservedGeneration).To(Equal(dpfOperatorConfig.Generation))
	}).WithTimeout(2 * time.Minute).Should(Succeed())
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
	if ngcAPIKey != "" {
		dpuServiceTemplate.Spec.HelmChart.Values = &machineryruntime.RawExtension{
			Raw: []byte(fmt.Sprintf(`{"imagePullSecrets": [{"name": "%s"}]}`, ngcPullSecretName)),
		}
	}
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
