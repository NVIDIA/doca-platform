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

//nolint:goconst,gocyclo,dupl
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/metrics"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	dpuDeployment.SetLabels(CleanupScope.It)
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
	dpuDeployment.SetLabels(CleanupScope.It)
	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	By("verify DPUDeployment and DPUServiceInterface metrics are in KSM")
	expectedMetricsNames := map[string][]string{
		"dpudeployment": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
	}

	Eventually(func(g Gomega) {
		actualMetricsNames := metrics.GetKSMMetrics(g, ctx, hostClusterRESTClient, metricsURI)
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
	gotDPUServiceList := &dpuservicev1.DPUServiceList{}
	Eventually(func(g Gomega) {
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
	}).WithTimeout(30 * time.Second).Should(Succeed())

	Eventually(func(g Gomega) {
		gotApplicationList := &argov1.ApplicationList{}
		g.Expect(input.client.List(ctx, gotApplicationList, client.InNamespace(dpuDeployment.GetNamespace()))).To(Succeed())
		dpuServiceNameToApplication := getDPUServiceNameToApplication(gotDPUServiceList.Items, gotApplicationList.Items)

		// Modify Application to have no malformedPullPolicy in their values
		for _, application := range dpuServiceNameToApplication {
			if bytes.Contains(application.Spec.Source.Helm.ValuesObject.Raw, []byte("malformedPullPolicy")) {
				origApp := application.DeepCopy()
				// Set valid helm values.
				application.Spec.Source.Helm.ValuesObject = &machineryruntime.RawExtension{Raw: []byte(`{}`)}
				// Set maximum backoff duration to 1s for the case if it was not yet reconciled or we re-create the application.
				// Set limit to 0 to ensure that operation is retried.
				if application.Spec.SyncPolicy == nil {
					application.Spec.SyncPolicy = &argov1.SyncPolicy{}
				}
				if application.Spec.SyncPolicy.Retry == nil {
					application.Spec.SyncPolicy.Retry = &argov1.RetryStrategy{}
				}
				if application.Spec.SyncPolicy.Retry.Backoff == nil {
					application.Spec.SyncPolicy.Retry.Backoff = &argov1.Backoff{}
				}
				// Set limit to 0 to ensure that operation is retried.
				application.Spec.SyncPolicy.Retry.Limit = 0
				// Set maximum backoff duration to 1s for the existing operation to ensure it is not waiting for a long backoff duration.
				application.Spec.SyncPolicy.Retry.Backoff.MaxDuration = "1s"
				// Refresh ensures we use the updated values.
				application.Spec.SyncPolicy.Retry.Refresh = true

				// Force a new operation
				application.Operation = &argov1.Operation{
					InitiatedBy: argov1.OperationInitiator{
						Username: "ginkgo",
					},
					Sync: &argov1.SyncOperation{
						SyncStrategy: &argov1.SyncStrategy{
							Hook: &argov1.SyncStrategyHook{},
						},
					},
					Retry: argov1.RetryStrategy{
						// Refresh ensures we use the updated values.
						Refresh: true,
						Limit:   0,
					},
				}

				// Overwrite an existing operation.
				if application.Status.OperationState != nil {
					application.Status.OperationState.Operation.Retry.Refresh = true
					application.Status.OperationState.Operation.Retry.Limit = 0
					if application.Status.OperationState.Operation.Retry.Backoff == nil {
						application.Status.OperationState.Operation.Retry.Backoff = &argov1.Backoff{}
					}
					application.Status.OperationState.Operation.Retry.Backoff.MaxDuration = "1s"
				}

				// Use optimistic locking to ensure that we patch the latest version of the application to not forget a operation which was just triggered.
				g.Expect(input.client.Patch(ctx, &application, client.MergeFromWithOptions(origApp, client.MergeFromWithOptimisticLock{}))).To(Succeed())
				// Delete the application to ensure that we haven't recreated the application in the meantime with the
				// patch above
				g.Expect(input.client.Delete(ctx, &application)).To(Succeed())
			}
		}

		// Ensure Applications don't have malformedPullPolicy in their values and on-going operation.
		gotApplicationList = &argov1.ApplicationList{}
		g.Expect(input.client.List(ctx, gotApplicationList, client.InNamespace(dpuDeployment.GetNamespace()))).To(Succeed())
		dpuServiceNameToApplication = getDPUServiceNameToApplication(gotDPUServiceList.Items, gotApplicationList.Items)

		for _, application := range dpuServiceNameToApplication {
			g.Expect(bytes.Contains(application.Spec.Source.Helm.ValuesObject.Raw, []byte("malformedPullPolicy"))).To(BeFalse())
			if application.Status.OperationState == nil ||
				application.Status.OperationState.SyncResult == nil ||
				application.Status.OperationState.SyncResult.Source.Helm == nil ||
				application.Status.OperationState.SyncResult.Source.Helm.ValuesObject == nil {
				continue
			}
			g.Expect(bytes.Contains(application.Status.OperationState.SyncResult.Source.Helm.ValuesObject.Raw, []byte("malformedPullPolicy"))).To(BeFalse())
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
	serviceInterfaceCount := 0
	for _, config := range dpuDeployment.Spec.Services {
		serviceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpuDeployment.GetNamespace(), Name: config.ServiceConfiguration}, serviceConfiguration)).To(Succeed())
		serviceInterfaceCount += len(serviceConfiguration.Spec.Interfaces)
	}
	gotDPUSetList := &provisioningv1.DPUSetList{}
	g.Expect(testClient.List(ctx,
		gotDPUSetList,
		client.InNamespace(dpuDeployment.GetNamespace()),
		client.MatchingLabels{
			"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
		})).To(Succeed())
	g.Expect(gotDPUSetList.Items).To(HaveLen(len(dpuDeployment.Spec.DPUs.DPUSets)))

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
	return g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(serviceInterfaceCount))
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
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).To(Succeed())
		By(fmt.Sprintf("Expected number of nodes %d to equal %d", len(nodes.Items), 0))
		g.Expect(nodes.Items).To(BeEmpty())
		// The timeout is so long here because for Zero Trust provisioning takes longer
	}).WithTimeout(45 * time.Minute).Should(Succeed())

	By("create DPUServiceIPAM to be used by dpuDeployment")
	dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetLabels(CleanupScope.Suite)
	dpuServiceIPAM.SetName("dpudeployment-ipam-pool1")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	// Remove selectors so it applies to all nodes
	dpuServiceIPAM.Spec.NodeSelector = nil
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

	inClusterDPUServiceTemplate := input.dpuServiceTemplate.DeepCopy()
	inClusterDPUServiceTemplate.SetLabels(CleanupScope.Suite)
	inClusterDPUServiceTemplate.SetName("dpudeployment-example-in-cluster-servicetemplate")
	inClusterDPUServiceTemplate.Spec.DeploymentServiceName = "example-in-cluster"

	inClusterDPUServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
	inClusterDPUServiceConfiguration.SetLabels(CleanupScope.Suite)
	inClusterDPUServiceConfiguration.SetName("dpudeployment-example-in-cluster-serviceconfiguration")
	inClusterDPUServiceConfiguration.Spec.Interfaces = nil
	inClusterDPUServiceConfiguration.Spec.DeploymentServiceName = "example-in-cluster"
	inClusterDPUServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To(true)

	dpuDeployment := testutils.GenerateDPUObj("dpf-dpudeployment", input.dpuDeployment.DeepCopy().Namespace, input.dpuDeployment.DeepCopy(), CleanupScope.Suite)
	// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
	// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
	//nolint:staticcheck
	dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"feature.node.kubernetes.io/dpu-enabled": "true"},
	}
	// Add example2 service to the DPUDeployment
	dpuDeployment.Spec.Services["example-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
		ServiceTemplate:      "dpudeployment-example-servicetemplate-2",
		ServiceConfiguration: "dpudeployment-example-serviceconfiguration-2",
	}

	if !isGinkgoLabelApplied(Domain.ZeroTrust) {
		Expect(input.client.Create(ctx, inClusterDPUServiceTemplate)).To(Succeed())
		Expect(input.client.Create(ctx, inClusterDPUServiceConfiguration)).To(Succeed())
		dpuDeployment.Spec.Services["example-in-cluster"] = dpuservicev1.DPUDeploymentServiceConfiguration{
			ServiceTemplate:      inClusterDPUServiceTemplate.GetName(),
			ServiceConfiguration: inClusterDPUServiceConfiguration.GetName(),
		}
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

	if Label(Domain.Scale).MatchesLabelFilter(GinkgoLabelFilter()) {
		// mock-DMS / scale scenario: serviceChains and ServiceInterfaces cannot
		// reach ready state in mock-dms nodes
		return
	}

	if !input.hasDpuNodes() {
		return
	}

	By("Verifying DPUs are provisioned")
	VerifyDPUClusterWithNodes(ctx, ProvisionDPUClustersInput{
		numberOfDPUNodes:    input.numberOfDPUNodes,
		numberOfDPUsPerNode: input.numberOfDPUsPerNode,
		client:              input.client,
		HostRebootScript:    input.HostRebootScript,
	})

	By(fmt.Sprintf("verify ServiceInterface is created in %d nodes", input.totalDPUs()))
	Eventually(func(g Gomega) {
		serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
		g.Expect(dpuClusterClient[0].List(ctx, serviceInterfaceList, client.MatchingLabels(serviceInterfaceLabels))).To(Succeed())
		g.Expect(serviceInterfaceList.Items).To(HaveLen(input.totalDPUs()))
	}).WithTimeout(15 * time.Minute).WithPolling(120 * time.Second).Should(Succeed())

	By("Verify service pods have the service reference label")
	Eventually(func(g Gomega) {
		for serviceName, svcConfig := range dpuDeployment.Spec.Services {
			dpuSvcConfig := &dpuservicev1.DPUServiceConfiguration{}
			g.Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpuDeployment.GetNamespace(), Name: svcConfig.ServiceConfiguration}, dpuSvcConfig)).To(Succeed())
			podList := &corev1.PodList{}
			// We currently don't have an in-cluster DPUService that matches the contract for e2e tests, but in theory
			// could do the same check as below for those services.
			if dpuSvcConfig.Spec.ServiceConfiguration.ShouldDeployInCluster() {
				continue
			}

			g.Expect(dpuClusterClient[0].List(ctx, podList,
				client.MatchingLabels{dpuservicev1.ServiceReferenceInDPUDeploymentLabelKey: serviceName},
				client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
			g.Expect(podList.Items).To(HaveLen(input.totalDPUs()), "expected %d pods for service %s", input.totalDPUs(), serviceName)
		}
	}).WithTimeout(15 * time.Minute).WithPolling(120 * time.Second).Should(Succeed())
}

func VerifyDPUDeploymentIsReady(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}
	By("Getting the existing DPUDeployment")
	// Get the DPUDeployment created in ValidateDPUDeploymentFullCreation
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	dpuDeployment.SetName("dpf-dpudeployment")
	dpuDeployment.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())

	By(fmt.Sprintf("Verifying that the dpuDeployment %s is ready", dpuDeployment.GetName()))
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

// ValidateDPUDeploymentDPUServiceDisruptiveUpgradeDrain validates that DPUDeployment disruptive upgrade flow for
// standard DPUService works as expected with node effect drain which is the recommendation for Host Trusted
func ValidateDPUDeploymentDPUServiceDisruptiveUpgradeDrain(ctx context.Context, input *systemTestInput) {
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
	VerifyDPFOperatorConfigReady(ctx, input.client, 2*time.Minute)

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
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.totalDPUs()))
		initialPods = podList.Items
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Getting the mapping between host nodes and pods running on the DPU cluster on a DPU that is part of that node")
	// Get all nodes in the DPU cluster
	dpuClusterNodes := &corev1.NodeList{}
	Expect(dpuClusterClient[0].List(ctx, dpuClusterNodes)).To(Succeed())

	// Create a map from DPU cluster node name to DPUNode name using the DPUNodeNameLabel label
	dpuClusterNodeToHostNodeMap := make(map[string]string)
	for _, node := range dpuClusterNodes.Items {
		// The host name and the dpuNode must match as the dpuNode represents the host node.
		if hostNodeName, ok := node.Labels[provisioningv1.DPUNodeNameLabel]; ok {
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
		// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
		// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
		//nolint:staticcheck
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
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.totalDPUs()))

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
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(newPod), gotPod)).To(Succeed())

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
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.totalDPUs()))

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
	VerifyDPFOperatorConfigReady(ctx, input.client, 2*time.Minute)
}

// ValidateDPUDeploymentDPUServiceDisruptiveUpgradeHold validates that DPUDeployment disruptive upgrade flow for
// standard DPUService works as expected with hold node effect which is the recommendation for Zero Trust
func ValidateDPUDeploymentDPUServiceDisruptiveUpgradeHold(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 DPUNodes
		Skip("Skip test as there are not exactly 2 DPUNodes")
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
	VerifyDPFOperatorConfigReady(ctx, input.client, 2*time.Minute)

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

	By("Getting the mapping between DPUs and DPUNodes")
	// Get all DPUs in the system
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	// Create a map from DPU name to DPUNode name
	dpuToDPUNodeMap := make(map[string]string)
	for _, dpu := range dpuList.Items {
		dpuToDPUNodeMap[dpu.Name] = dpu.Spec.DPUNodeName
	}

	By("Modifying the DPUServiceConfiguration by adding an extra label")
	originalDPUServiceConfiguration := dpuServiceConfiguration.DeepCopy()
	if dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels == nil {
		dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = make(map[string]string)
	}
	dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels["test-disruptive-upgrade-zt"] = "true"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(originalDPUServiceConfiguration))).To(Succeed())

	By("Checking that DPUNodeMaintenance is created with hold annotation set to true")
	var dpuUnderNodeEffect string
	var inProgressDPUNodeMaintenance *provisioningv1.DPUNodeMaintenance
	Eventually(func(g Gomega) {
		// Get all DPUNodeMaintenance objects
		dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(input.client.List(ctx, dpuNodeMaintenanceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Find the DPUNodeMaintenance with hold annotation set to "true"
		for i, dpuNodeMaintenance := range dpuNodeMaintenanceList.Items {
			if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[i]) {
				// Verify this DPUNode is targeted by our DPUDeployment
				// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
				// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
				//nolint:staticcheck
				labelSelectorForNodes, err := utils.LabelSelectorAsSelector(dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector)
				g.Expect(err).ToNot(HaveOccurred())

				dpuNode := &provisioningv1.DPUNode{}
				err = input.client.Get(ctx, client.ObjectKey{Name: dpuNodeMaintenance.Spec.DPUNodeName, Namespace: dpuNodeMaintenance.Namespace}, dpuNode)
				if err == nil && labelSelectorForNodes.Matches(labels.Set(dpuNode.Labels)) {
					dpuUnderNodeEffect = dpuNodeMaintenance.Spec.DPUNodeName
					inProgressDPUNodeMaintenance = &dpuNodeMaintenanceList.Items[i]
					break
				}
			}
		}
		g.Expect(dpuUnderNodeEffect).ToNot(BeEmpty(), "At least one DPUNode should have DPUNodeMaintenance with hold annotation set to true")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Verifying pods are NOT updated while hold annotation is true")
	Consistently(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Verify no pods have the new label yet (update hasn't started)
		for _, pod := range podList.Items {
			_, hasNewLabel := pod.Labels["test-disruptive-upgrade-zt"]
			g.Expect(hasNewLabel).To(BeFalse(), "Pods should not be updated while hold annotation is true")
		}
	}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

	By("Simulating user action: setting hold annotation to false to allow update")
	Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.client, inProgressDPUNodeMaintenance).WithTimeout(30 * time.Second).Should(Succeed())

	By("Checking that the pod on the DPU belonging to the DPUNode under node effect is now updated")
	var newPod *corev1.Pod
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.totalDPUs()))

		// Track pods on DPU belonging to DPUNode under node effect (should transition from old to new)
		foundOldPodOnDPUUnderNodeEffect := false
		foundNewPodOnDPUUnderNodeEffect := false
		// Track pods on DPUs not under node effect (should remain old, not yet upgraded)
		foundOldPodOnDPUNotUnderNodeEffect := false
		foundNewPodOnDPUNotUnderNodeEffect := false

		for _, pod := range podList.Items {
			// Find the DPUNode that this pod belongs to
			dpuName := pod.Spec.NodeName
			podDPUNodeName, dpuNodeExists := dpuToDPUNodeMap[dpuName]
			g.Expect(dpuNodeExists).To(BeTrue())

			// Check if pod has the new label (new pod) or not (old pod)
			_, podHasNewLabel := pod.Labels["test-disruptive-upgrade-zt"]
			podIsOnDPUNodeUnderNodeEffect := podDPUNodeName == dpuUnderNodeEffect

			switch {
			case podIsOnDPUNodeUnderNodeEffect && podHasNewLabel:
				foundNewPodOnDPUUnderNodeEffect = true
				newPod = &pod
			case podIsOnDPUNodeUnderNodeEffect && !podHasNewLabel:
				foundOldPodOnDPUUnderNodeEffect = true
			case !podIsOnDPUNodeUnderNodeEffect && podHasNewLabel:
				foundNewPodOnDPUNotUnderNodeEffect = true
			case !podIsOnDPUNodeUnderNodeEffect && !podHasNewLabel:
				foundOldPodOnDPUNotUnderNodeEffect = true
			}
		}

		// DPUNode under node effect should have new pod, not old pod
		g.Expect(foundOldPodOnDPUUnderNodeEffect).To(BeFalse(), "Old pod without new label should be removed from DPU belonging to DPUNode under node effect")
		g.Expect(foundNewPodOnDPUUnderNodeEffect).To(BeTrue(), "New pod with new label should exist on DPU belonging to DPUNode under node effect")
		// DPUNodes not under node effect should have old pods, not new pods
		g.Expect(foundOldPodOnDPUNotUnderNodeEffect).To(BeTrue(), "Old pod without new label should still exist on DPUs belonging to DPUNodes not under node effect")
		g.Expect(foundNewPodOnDPUNotUnderNodeEffect).To(BeFalse(), "New pod with new label should not exist on DPUs belonging to DPUNodes not under node effect yet")
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	By("Verifying the new pod becomes ready on the DPUNode under node effect")
	Eventually(func(g Gomega) {
		gotPod := &corev1.Pod{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(newPod), gotPod)).To(Succeed())

		// Verify pod is ready
		isPodReady := false
		for _, condition := range gotPod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				isPodReady = true
				break
			}
		}
		g.Expect(isPodReady).To(BeTrue(), "New pod should become ready after hold annotation was set to false")
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying the DPU(s) for the updated DPUNode become ready")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Find the DPU(s) belonging to the DPUNode that was updated and verify they're ready
		foundDPU := false
		for _, dpu := range dpus.Items {
			if dpu.Spec.DPUNodeName == dpuUnderNodeEffect {
				foundDPU = true
				g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady),
					fmt.Sprintf("DPU %s should be ready after pod is ready", dpu.Name))
			}
		}
		g.Expect(foundDPU).To(BeTrue(), "Should find one DPU that is ready for the DPUNode")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Finding and releasing the next DPUNode with hold annotation to complete the upgrade")
	// Since MaxUnavailableDPUNodes=1, the next node should now have a dpuNodeMaintenance with hold=true
	var secondMaintenanceWithHold *provisioningv1.DPUNodeMaintenance
	Eventually(func(g Gomega) {
		dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(input.client.List(ctx, dpuNodeMaintenanceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		// We expect only a single DPUNodeMaintenance left
		g.Expect(dpuNodeMaintenanceList.Items).To(HaveLen(1))

		if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[0]) {
			secondMaintenanceWithHold = &dpuNodeMaintenanceList.Items[0]
		}
		g.Expect(secondMaintenanceWithHold).ToNot(BeNil(), "Second DPUNode should have DPUNodeMaintenance with hold annotation set to true")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Simulating user action: Setting hold annotation to false on the second DPUNode")
	Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.client, secondMaintenanceWithHold).WithTimeout(30 * time.Second).Should(Succeed())

	By("Verifying that the DPUDeployment becomes ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying all pods are running the new configuration")
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient[0].List(ctx, podList,
			client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceIDForExample},
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(podList.Items).To(HaveLen(input.totalDPUs()))

		// Verify all pods have the new label
		for _, pod := range podList.Items {
			g.Expect(pod.Labels).To(HaveKeyWithValue("test-disruptive-upgrade-zt", "true"))
		}
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Verifying that all DPUs are ready")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		readyDPUs := 0
		for _, dpu := range dpus.Items {
			if dpu.Status.Phase == provisioningv1.DPUReady {
				readyDPUs++
			}
		}

		g.Expect(readyDPUs).To(Equal(input.totalDPUs()),
			fmt.Sprintf("expected all %d DPUs to be ready, but only %d are ready",
				input.totalDPUs(), readyDPUs))
	}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		resetConfig := dpfOperatorConfig.DeepCopy()
		resetConfig.Spec = originalDPFOperatorConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(dpfOperatorConfig))).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Validating that the DPFOperatorConfig is ready for the current generation")
	VerifyDPFOperatorConfigReady(ctx, input.client, 2*time.Minute)
}

// ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade validates that DPUDeployment disruptive upgrade flow for
// in-cluster DPUServices works as expected
func ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	By("getting the existing DPUDeployment")
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	dpuDeployment.SetName("dpf-dpudeployment")
	dpuDeployment.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())

	parentLabel := fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)

	By("getting the dpuServiceConfiguration for in-cluster service")
	dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
	dpuServiceConfiguration.SetName("dpudeployment-example-in-cluster-serviceconfiguration")
	dpuServiceConfiguration.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())

	dpuDeploymentKey := client.ObjectKeyFromObject(dpuDeployment)
	expectedVersionKey := fmt.Sprintf("%s-%s", "svc.dpu.nvidia.com/dpuservice-in-cluster-version", digest.Short(digest.FromObjects(dpuDeploymentKey, "example-in-cluster"), 10))

	By("getting the in-cluster DPUService")
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	Expect(input.client.List(ctx, dpuServiceList,
		client.InNamespace(dpuDeployment.Namespace),
		client.MatchingLabels{dpuservicev1.ParentDPUDeploymentNameLabel: parentLabel},
	)).To(Succeed())

	inClusterServices := getInClusterDPUServices(dpuServiceList.Items)
	Expect(inClusterServices).To(HaveLen(1))

	originalInClusterService := inClusterServices[0].DeepCopy()

	By("getting the target nodes")
	nodesInfo := getTargetNodesAndDPUNodeNames(ctx, input.client, dpuDeployment)

	By("verifying that the in-cluster services is deployed")
	Eventually(func(g Gomega) {
		allNodes := &corev1.NodeList{}
		g.Expect(input.client.List(ctx, allNodes)).To(Succeed())

		nodesWithLabel := make(map[string]struct{})
		for _, node := range allNodes.Items {
			val, ok := node.Labels[expectedVersionKey]
			if !ok {
				continue
			}
			g.Expect(val).To(Equal(originalInClusterService.Name), fmt.Sprintf("unexpected label value on node %s", node.Name))
			nodesWithLabel[node.Name] = struct{}{}
			g.Expect(nodesInfo.targetNodes).To(HaveKey(node.Name), fmt.Sprintf("node %s should not carry in-cluster label", node.Name))
		}

		g.Expect(nodesWithLabel).To(HaveLen(len(nodesInfo.targetNodes)), "expected number of nodes with label to match targeted nodes")
	}).WithTimeout(15 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("capturing old pod UIDs before update")
	oldPodUIDs := captureOldPodUIDs(ctx, input.client, dpuDeployment.Namespace, originalInClusterService.Name)

	By("capturing initial NodeEffect condition times from DPUs before update")
	initialNodeEffectStates := captureInitialNodeEffectStates(ctx, input.client, nodesInfo.dpuNodeNames)

	By("updating the dpuServiceConfiguration by adding an extra label to trigger disruptive upgrade")
	originalDPUServiceConfiguration := dpuServiceConfiguration.DeepCopy()
	if dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels == nil {
		dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = make(map[string]string)
	}
	dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels["test-disruptive-upgrade"] = "true"
	Expect(input.client.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(originalDPUServiceConfiguration))).To(Succeed())

	By("verifying that a new DPUService is created")
	newInClusterService := waitForNewInClusterDPUService(ctx, input.client, dpuDeployment.Namespace, parentLabel, originalInClusterService.Name)

	By("verifying that all target DPUs went through dpuNodeMaintenance (NodeEffectReady completed and NodeEffectRemoved)")
	verifyDPUsCompletedMaintenance(ctx, input.client, nodesInfo.dpuNodeNames, initialNodeEffectStates)

	By("verifying that pods were recreated")
	verifyPodsRecreated(ctx, input.client, dpuDeployment.Namespace, newInClusterService.Name, oldPodUIDs)

	By("Verifying that the DPUDeployment becomes ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).Should(Succeed())
}

// ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeDrain validates that DPUDeployment disruptive upgrade flow for
// DPUServiceChain works as expected with drain node effect which is the recommendation for Host Trusted
func ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeDrain(ctx context.Context, input *systemTestInput) {
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
		g.Expect(conditions.IsTrue(dpfOperatorConfig, conditions.TypeReady)).To(BeTrue())
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
		g.Expect(dpuClusterClient[0].List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(serviceChainList.Items).To(HaveLen(input.totalDPUs()))
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
		// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
		// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
		//nolint:staticcheck
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
		g.Expect(dpuClusterClient[0].List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Verify that we have service chains on both nodes
		g.Expect(serviceChainList.Items).To(HaveLen(input.totalDPUs()))

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
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(newServiceChain), gotServiceChain)).To(Succeed())

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
		g.Expect(dpuClusterClient[0].List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(serviceChainList.Items).To(HaveLen(input.totalDPUs()))

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
		g.Expect(conditions.IsTrue(dpfOperatorConfig, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(2 * time.Minute).Should(Succeed())
}

// ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeHold validates that DPUDeployment disruptive upgrade flow for
// DPUServiceChain works as expected with hold node effect which is the default recommendation for Zero Trust
func ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeHold(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 DPUNodes
		Skip("Skip test as there are not exactly 2 DPUNodes")
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
	VerifyDPFOperatorConfigReady(ctx, input.client, 2*time.Minute)

	By("Getting the existing DPUDeployment")
	dpuDeployment := &dpuservicev1.DPUDeployment{}
	dpuDeployment.SetName("dpf-dpudeployment")
	dpuDeployment.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())

	By("Creating mapping from DPU to DPUNode")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	// Create a map from DPU name to DPUNode name
	dpuToDPUNodeMap := make(map[string]string)
	for _, dpu := range dpuList.Items {
		dpuToDPUNodeMap[dpu.Name] = dpu.Spec.DPUNodeName
	}

	By("Modifying the DPUDeployment ServiceChains by changing ServiceMTU")
	originalDPUDeployment := dpuDeployment.DeepCopy()
	newMTU := 1300
	dpuDeployment.Spec.ServiceChains.Switches[0].ServiceMTU = &newMTU
	Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(originalDPUDeployment))).To(Succeed())

	By("Checking that DPUNodeMaintenance is created with hold annotation set to true")
	var dpuNodeUnderNodeEffect string
	var inProgressDPUNodeMaintenance *provisioningv1.DPUNodeMaintenance
	Eventually(func(g Gomega) {
		dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(input.client.List(ctx, dpuNodeMaintenanceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		for i, dpuNodeMaintenance := range dpuNodeMaintenanceList.Items {
			if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[i]) {
				// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
				// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
				//nolint:staticcheck
				labelSelectorForNodes, err := utils.LabelSelectorAsSelector(dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector)
				g.Expect(err).ToNot(HaveOccurred())

				dpuNode := &provisioningv1.DPUNode{}
				err = input.client.Get(ctx, client.ObjectKey{Name: dpuNodeMaintenance.Spec.DPUNodeName, Namespace: dpuNodeMaintenance.Namespace}, dpuNode)
				if err == nil && labelSelectorForNodes.Matches(labels.Set(dpuNode.Labels)) {
					dpuNodeUnderNodeEffect = dpuNodeMaintenance.Spec.DPUNodeName
					inProgressDPUNodeMaintenance = &dpuNodeMaintenanceList.Items[i]
					break
				}
			}
		}
		g.Expect(dpuNodeUnderNodeEffect).ToNot(BeEmpty(), "At least one DPUNode should have DPUNodeMaintenance with hold annotation set to true")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Verifying ServiceChains are NOT updated while hold annotation is true")
	Consistently(func(g Gomega) {
		serviceChainList := &dpuservicev1.ServiceChainList{}
		g.Expect(dpuClusterClient[0].List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		// Verify no ServiceChains have the new MTU yet (update hasn't started)
		for _, serviceChain := range serviceChainList.Items {
			if serviceChain.Spec.Switches[0].ServiceMTU != nil {
				g.Expect(*serviceChain.Spec.Switches[0].ServiceMTU).ToNot(Equal(1300), "ServiceChains should not be updated while hold annotation is true")
			}
		}
	}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

	By("Simulating user action: setting hold annotation to false to allow update")
	Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.client, inProgressDPUNodeMaintenance).WithTimeout(30 * time.Second).Should(Succeed())

	By("Checking that the ServiceChain on the DPU under node effect is updated")
	var newServiceChain *dpuservicev1.ServiceChain
	Eventually(func(g Gomega) {
		serviceChainList := &dpuservicev1.ServiceChainList{}
		g.Expect(dpuClusterClient[0].List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(serviceChainList.Items).To(HaveLen(input.totalDPUs()))

		// Track ServiceChains on DPUs under and not under node effect
		foundOldServiceChainOnDPUUnderNodeEffect := false
		foundNewServiceChainOnDPUUnderNodeEffect := false
		foundOldServiceChainOnDPUNotUnderNodeEffect := false
		foundNewServiceChainOnDPUNotUnderNodeEffect := false

		for _, serviceChain := range serviceChainList.Items {
			serviceChainDPUNodeName, dpuNodeExists := dpuToDPUNodeMap[*serviceChain.Spec.Node]
			g.Expect(dpuNodeExists).To(BeTrue())

			// Check if this is a new ServiceChain (has the new MTU)
			hasNewMTU := serviceChain.Spec.Switches[0].ServiceMTU != nil && *serviceChain.Spec.Switches[0].ServiceMTU == 1300
			serviceChainIsOnDPUUnderNodeEffect := serviceChainDPUNodeName == dpuNodeUnderNodeEffect

			switch {
			case serviceChainIsOnDPUUnderNodeEffect && hasNewMTU:
				foundNewServiceChainOnDPUUnderNodeEffect = true
				newServiceChain = &serviceChain
			case serviceChainIsOnDPUUnderNodeEffect && !hasNewMTU:
				foundOldServiceChainOnDPUUnderNodeEffect = true
			case !serviceChainIsOnDPUUnderNodeEffect && hasNewMTU:
				foundNewServiceChainOnDPUNotUnderNodeEffect = true
			case !serviceChainIsOnDPUUnderNodeEffect && !hasNewMTU:
				foundOldServiceChainOnDPUNotUnderNodeEffect = true
			}
		}

		// DPU under node effect should have new ServiceChain, not old ServiceChain
		g.Expect(foundOldServiceChainOnDPUUnderNodeEffect).To(BeFalse(), "Old ServiceChain should be removed from DPU under node effect")
		g.Expect(foundNewServiceChainOnDPUUnderNodeEffect).To(BeTrue(), "New ServiceChain should exist on DPU under node effect")
		// DPUs not under node effect should have old ServiceChains, not new ServiceChains
		g.Expect(foundOldServiceChainOnDPUNotUnderNodeEffect).To(BeTrue(), "Old ServiceChain should still exist on DPUs not under node effect")
		g.Expect(foundNewServiceChainOnDPUNotUnderNodeEffect).To(BeFalse(), "New ServiceChain should not exist on DPUs not under node effect yet")
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	By("Verifying the new ServiceChain becomes ready")
	Eventually(func(g Gomega) {
		gotServiceChain := &dpuservicev1.ServiceChain{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(newServiceChain), gotServiceChain)).To(Succeed())
		g.Expect(conditions.IsTrue(gotServiceChain, conditions.TypeReady)).To(BeTrue(), "New ServiceChain should become ready after hold annotation was set to false")
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying the DPU(s) for the updated DPUNode become ready")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		foundDPU := false
		for _, dpu := range dpus.Items {
			if dpu.Spec.DPUNodeName == dpuNodeUnderNodeEffect {
				foundDPU = true
				g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady),
					fmt.Sprintf("DPU %s should be ready after ServiceChain is ready", dpu.Name))
			}
		}
		g.Expect(foundDPU).To(BeTrue(), "Should find at least one DPU for the updated DPUNode")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Finding and releasing the next DPUNode with hold annotation to complete the upgrade")
	var secondMaintenanceWithHold *provisioningv1.DPUNodeMaintenance
	Eventually(func(g Gomega) {
		dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(input.client.List(ctx, dpuNodeMaintenanceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		// We expect only a single DPUNodeMaintenance left
		g.Expect(dpuNodeMaintenanceList.Items).To(HaveLen(1))

		if isDPUNodeMaintenanceOnHold(&dpuNodeMaintenanceList.Items[0]) {
			secondMaintenanceWithHold = &dpuNodeMaintenanceList.Items[0]
		}
		g.Expect(secondMaintenanceWithHold).ToNot(BeNil(), "Second DPUNode should have DPUNodeMaintenance with hold annotation set to true")
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Setting hold annotation to false on the second DPUNode")
	Eventually(releaseDPUNodeMaintenanceHold).WithArguments(ctx, input.client, secondMaintenanceWithHold).WithTimeout(30 * time.Second).Should(Succeed())

	By("Verifying that the DPUDeployment becomes ready")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())
		g.Expect(conditions.IsTrue(dpuDeployment, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(15 * time.Minute).Should(Succeed())

	By("Verifying all ServiceChains are running the new configuration")
	Eventually(func(g Gomega) {
		serviceChainList := &dpuservicev1.ServiceChainList{}
		g.Expect(dpuClusterClient[0].List(ctx, serviceChainList,
			client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(serviceChainList.Items).To(HaveLen(input.totalDPUs()))

		for _, serviceChain := range serviceChainList.Items {
			g.Expect(serviceChain.Spec.Switches).To(HaveLen(1))
			g.Expect(serviceChain.Spec.Switches[0].ServiceMTU).ToNot(BeNil())
			g.Expect(*serviceChain.Spec.Switches[0].ServiceMTU).To(Equal(1300))
		}
	}).WithTimeout(5 * time.Minute).Should(Succeed())

	By("Verifying that all DPUs are ready")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		readyDPUs := 0
		for _, dpu := range dpus.Items {
			if dpu.Status.Phase == provisioningv1.DPUReady {
				readyDPUs++
			}
		}

		g.Expect(readyDPUs).To(Equal(input.totalDPUs()),
			fmt.Sprintf("expected all %d DPUs to be ready, but only %d are ready",
				input.totalDPUs(), readyDPUs))
	}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig)).To(Succeed())
		resetConfig := dpfOperatorConfig.DeepCopy()
		resetConfig.Spec = originalDPFOperatorConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(dpfOperatorConfig))).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Validating that the DPFOperatorConfig is ready for the current generation")
	VerifyDPFOperatorConfigReady(ctx, input.client, 2*time.Minute)
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
	dpuServiceTemplate.SetLabels(CleanupScope.Suite)
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
	dpuServiceConfiguration.SetLabels(CleanupScope.Suite)
	dpuServiceConfiguration.SetName(dpuServiceConfiguration.GetName() + nameDiff)
	dpuServiceConfiguration.Spec.DeploymentServiceName += nameDiff
	return dpuServiceConfiguration
}

func generateDPUDeployment(input *systemTestInput, nameDiff string) *dpuservicev1.DPUDeployment {
	if nameDiff != "" {
		nameDiff = "-" + nameDiff
	}
	dpuDeployment := input.dpuDeployment.DeepCopy()
	dpuDeployment.SetLabels(CleanupScope.Suite)
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

// dpuNodeEffectState holds the state of NodeEffect conditions for a DPU
type dpuNodeEffectState struct {
	nodeEffectReadyTime   metav1.Time
	nodeEffectRemovedTime metav1.Time
}

// getInClusterDPUServices filters and returns only in-cluster DPU services from a service list
func getInClusterDPUServices(services []dpuservicev1.DPUService) []dpuservicev1.DPUService {
	inClusterServices := make([]dpuservicev1.DPUService, 0)
	for _, svc := range services {
		if ptr.Deref(svc.Spec.DeployInCluster, false) {
			inClusterServices = append(inClusterServices, svc)
		}
	}
	return inClusterServices
}

// DPUDeploymentNodesInfo holds information about target nodes and DPUNodes
type DPUDeploymentNodesInfo struct {
	targetNodes  map[string]corev1.Node
	dpuNodeNames []string
}

// getTargetNodesAndDPUNodeNames gets target nodes and DPU node names based on DPUDeployment selector
func getTargetNodesAndDPUNodeNames(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment) DPUDeploymentNodesInfo {
	targetNodes := make(map[string]corev1.Node)
	// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
	// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
	//nolint:staticcheck
	nodeSelector := dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector
	Expect(nodeSelector).ToNot(BeNil())

	selector, err := metav1.LabelSelectorAsSelector(nodeSelector)
	Expect(err).ToNot(HaveOccurred())

	// List DPUNodes matching the selector
	dpuNodeList := &provisioningv1.DPUNodeList{}
	Expect(c.List(ctx, dpuNodeList, client.MatchingLabelsSelector{Selector: selector})).To(Succeed())
	Expect(dpuNodeList.Items).ToNot(BeEmpty(), fmt.Sprintf("expected DPUNodes matching selector for DPUSet %s", dpuDeployment.Spec.DPUs.DPUSets[0].NameSuffix))

	// Get the corev1.Nodes referenced by the DPUNodes
	dpuNodeNames := make([]string, 0)
	for _, dpuNode := range dpuNodeList.Items {
		if dpuNode.Status.KubeNodeRef == nil {
			continue
		}
		node := &corev1.Node{}
		Expect(c.Get(ctx, client.ObjectKey{Name: *dpuNode.Status.KubeNodeRef}, node)).To(Succeed())
		targetNodes[node.Name] = *node
		dpuNodeNames = append(dpuNodeNames, dpuNode.Name)
	}
	Expect(targetNodes).ToNot(BeEmpty(), "expected DPUDeployment to target nodes")

	return DPUDeploymentNodesInfo{
		targetNodes:  targetNodes,
		dpuNodeNames: dpuNodeNames,
	}
}

// captureOldPodUIDs captures UIDs of old pods before update
func captureOldPodUIDs(ctx context.Context, c client.Client, namespace string, serviceName string) map[string]struct{} {
	oldPods := &corev1.PodList{}
	Expect(c.List(ctx, oldPods,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/instance": "in-cluster-" + serviceName})).To(Succeed())
	Expect(oldPods.Items).ToNot(BeEmpty())
	oldPodUIDs := make(map[string]struct{})
	for _, pod := range oldPods.Items {
		oldPodUIDs[string(pod.UID)] = struct{}{}
	}
	return oldPodUIDs
}

// captureInitialNodeEffectStates captures initial NodeEffect condition states from DPUs
func captureInitialNodeEffectStates(ctx context.Context, c client.Client, dpuNodeNames []string) map[string]dpuNodeEffectState {
	initialNodeEffectStates := make(map[string]dpuNodeEffectState)
	dpuList := &provisioningv1.DPUList{}
	Expect(c.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

	for _, dpu := range dpuList.Items {
		// Only track DPUs that belong to our target DPUNodes
		if !slices.Contains(dpuNodeNames, dpu.Spec.DPUNodeName) {
			continue
		}

		state := dpuNodeEffectState{}
		for _, cond := range dpu.Status.Conditions {
			if cond.Type == string(provisioningv1.DPUCondNodeEffectReady) {
				state.nodeEffectReadyTime = cond.LastTransitionTime
			} else if cond.Type == string(provisioningv1.DPUCondNodeEffectRemoved) {
				state.nodeEffectRemovedTime = cond.LastTransitionTime
			}
		}
		initialNodeEffectStates[dpu.Name] = state
	}
	return initialNodeEffectStates
}

// waitForNewInClusterDPUService waits for a new in-cluster DPU service to be created during disruptive upgrade
func waitForNewInClusterDPUService(ctx context.Context, c client.Client, namespace, parentLabel string, originalServiceName string) *dpuservicev1.DPUService {
	var newInClusterService *dpuservicev1.DPUService
	Eventually(func(g Gomega) {
		dpuServiceList := &dpuservicev1.DPUServiceList{}
		g.Expect(c.List(ctx, dpuServiceList,
			client.InNamespace(namespace),
			client.MatchingLabels{dpuservicev1.ParentDPUDeploymentNameLabel: parentLabel},
		)).To(Succeed())

		inClusterServices := getInClusterDPUServices(dpuServiceList.Items)

		// During disruptive upgrade, we should have 2 services (old, new)
		g.Expect(inClusterServices).To(HaveLen(2), "expected 2 in-cluster services during disruptive upgrade")

		// Find the new service (different name from original)
		for i := range inClusterServices {
			if inClusterServices[i].Name != originalServiceName {
				newInClusterService = &inClusterServices[i]
				break
			}
		}
		g.Expect(newInClusterService).ToNot(BeNil(), "expected new in-cluster service to be created")
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	return newInClusterService
}

// dpuMaintenanceCompleted checks if a DPU has completed its dpuNodeMaintenance cycle
func dpuMaintenanceCompleted(dpu provisioningv1.DPU, initialStates map[string]dpuNodeEffectState) bool {
	initialState, hadInitialState := initialStates[dpu.Name]
	nodeEffectReadyCompleted := false
	nodeEffectRemoved := false

	for _, cond := range dpu.Status.Conditions {
		if cond.Type == string(provisioningv1.DPUCondNodeEffectReady) {
			if cond.Status == metav1.ConditionTrue &&
				cond.Reason == "NodeEffectCompleted" &&
				(!hadInitialState || cond.LastTransitionTime.After(initialState.nodeEffectReadyTime.Time)) {
				nodeEffectReadyCompleted = true
			}
		} else if cond.Type == string(provisioningv1.DPUCondNodeEffectRemoved) {
			if cond.Status == metav1.ConditionTrue &&
				cond.Reason == "NodeEffectRemoved" &&
				(!hadInitialState || cond.LastTransitionTime.After(initialState.nodeEffectRemovedTime.Time)) {
				nodeEffectRemoved = true
			}
		}
	}

	return nodeEffectReadyCompleted && nodeEffectRemoved
}

// verifyDPUsCompletedMaintenance verifies that all target DPUs completed their dpuNodeMaintenance cycle
func verifyDPUsCompletedMaintenance(ctx context.Context, c client.Client, dpuNodeNames []string, initialStates map[string]dpuNodeEffectState) {
	Eventually(func(g Gomega) {
		dpusCompletedMaintenance := make(map[string]struct{})

		dpuList := &provisioningv1.DPUList{}
		g.Expect(c.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())

		for _, dpu := range dpuList.Items {
			// Only check DPUs that belong to our target DPUNodes
			if !slices.Contains(dpuNodeNames, dpu.Spec.DPUNodeName) {
				continue
			}

			if dpuMaintenanceCompleted(dpu, initialStates) {
				dpusCompletedMaintenance[dpu.Spec.DPUNodeName] = struct{}{}
			}
		}

		g.Expect(dpusCompletedMaintenance).To(HaveLen(len(dpuNodeNames)),
			fmt.Sprintf("expected all %d target DPUNodes' DPUs to complete dpuNodeMaintenance cycle (NodeEffectReady=True with reason=NodeEffectCompleted and NodeEffectRemoved updated), but only %d did",
				len(dpuNodeNames), len(dpusCompletedMaintenance)))
	}).WithTimeout(15 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}

// verifyPodsRecreated verifies that pods were recreated with new UIDs
func verifyPodsRecreated(ctx context.Context, c client.Client, namespace, serviceName string, oldPodUIDs map[string]struct{}) {
	Eventually(func(g Gomega) {
		newPods := &corev1.PodList{}
		g.Expect(c.List(ctx, newPods,
			client.InNamespace(namespace),
			client.MatchingLabels{"app.kubernetes.io/instance": "in-cluster-" + serviceName})).To(Succeed())

		g.Expect(newPods.Items).ToNot(BeEmpty(), "expected new pods to be created")

		// Verify these are NEW pods (different UIDs from old ones)
		for _, pod := range newPods.Items {
			_, existsInOld := oldPodUIDs[string(pod.UID)]
			g.Expect(existsInOld).To(BeFalse(),
				fmt.Sprintf("expected new pod, but found old pod UID: %s", pod.UID))
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning),
				fmt.Sprintf("expected pod %s to be running", pod.Name))
		}
	}).WithTimeout(5 * time.Minute).WithPolling(250 * time.Millisecond).Should(Succeed())
}
