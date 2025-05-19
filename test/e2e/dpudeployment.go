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
	"github.com/nvidia/doca-platform/test/utils/metrics"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUDeployment(ctx context.Context, input systemTestInput) {
	testClient := input.client
	It("create a DPUDeployment with its dependencies and ensure that the underlying objects are created", func() {
		By("creating the dependencies")
		dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
		dpuServiceTemplate.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
		dpuServiceTemplate.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
			Chart:   "dummydpuservice-chart",
			Version: tag,
			RepoURL: helmRegistry,
		}

		dpuServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
		dpuServiceConfiguration.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())

		By("creating the dpudeployment")
		dpuDeployment := input.dpuDeployment.DeepCopy()
		dpuDeployment.SetLabels(cleanupLabels)
		Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())

		By("checking that the underlying objects are created")
		Eventually(func(g Gomega) {
			g.Expect(VerifyDeploymentUnderlyingObjectsCreated(ctx, g, testClient, dpuDeployment)).To(BeTrue())
		}).WithTimeout(15 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	It("verify DPUDeployment and DPUServiceInterface metrics", func() {
		if !deployKSM {
			Skip("Skip KSM metrics test due to KSM is not deployed")
		}

		By("verify DPUDeployment and DPUServiceInterface metrics are in KSM")
		expectedMetricsNames := map[string][]string{
			"dpudeployment": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
			//TODO implement separate test for a DPUServiceInterface and move this metrics check there
			"dpuserviceinterface": {"created", "info", "status_conditions", "status_condition_last_transition_time"},
		}
		Eventually(func(g Gomega) {
			actualMetricsNames := metrics.GetKSMMetrics(ctx, testRESTClient, metricsURI)
			g.Expect(actualMetricsNames).NotTo(BeEmpty(), "Actual metrics are empty")
			g.Expect(metrics.VerifyMetrics(expectedMetricsNames, actualMetricsNames)).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	// This part is needed so that the we can test that the deletion logic is able to delete all the DPUServices, even
	// stale paused ones.
	It("trigger a disruptive upgrade with bad parameters so that the up to date DPUService never becomes ready", func() {
		By("getting the dpudeployment")
		dpuDeployment := input.dpuDeployment.DeepCopy()
		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), dpuDeployment)).To(Succeed())

		By("triggering the disruptive upgrade with bad parameters")
		dpuServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
		originalDPUServiceConfiguration := dpuServiceConfiguration.DeepCopy()

		dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &machineryruntime.RawExtension{Raw: []byte(`{"image":{"pullPolicy":"malformedPullPolicy"}}`)}
		Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(originalDPUServiceConfiguration))).To(Succeed())

		By("checking that 2 DPUServices exist and one of them is paused")
		Eventually(func(g Gomega) {
			gotDPUServiceList := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
				})).To(Succeed())
			g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

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
	})

	It("delete the DPUDeployment and ensure the underlying objects are gone", func() {
		if input.skipCleanup {
			Skip("Skip cleanup resources")
		}

		By("deleting the dpudeployment")
		dpuDeployment := input.dpuDeployment.DeepCopy()
		Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

		// Failed to apply ArgoCD Application deletion can take up to 5 mins based on the current configuration,
		// therefore we modify the application to have correct configuration so that the deletion goes faster
		// https://github.com/argoproj/argo-cd/blob/e6e92552167ad10ce7ca45c02f5534af6741e710/pkg/apis/application/v1alpha1/types.go#L1473-L1480
		By("HACK: Modify the bad application to ensure that it can be deleted faster")
		Eventually(func(g Gomega) {
			gotApplicationList := &argov1.ApplicationList{}
			g.Expect(testClient.List(ctx,
				gotApplicationList,
				client.InNamespace(dpuDeployment.GetNamespace()),
			)).To(Succeed())
			g.Expect(gotApplicationList.Items).ToNot(BeEmpty())

			for _, application := range gotApplicationList.Items {
				if bytes.Contains(application.Spec.Source.Helm.ValuesObject.Raw, []byte("malformedPullPolicy")) {
					origApp := application.DeepCopy()
					application.Spec.Source.Helm.ValuesObject = &machineryruntime.RawExtension{Raw: []byte(``)}
					g.Expect(testClient.Patch(ctx, &application, client.MergeFrom(origApp))).To(Succeed())
				}
			}
		}).WithTimeout(30 * time.Second).Should(Succeed())

		By("checking that the underlying objects are deleted")
		Eventually(func(g Gomega) {
			gotDPUSetList := &provisioningv1.DPUSetList{}
			g.Expect(testClient.List(ctx,
				gotDPUSetList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
				})).To(Succeed())
			g.Expect(gotDPUSetList.Items).To(BeEmpty())

			gotDPUServiceList := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
				})).To(Succeed())
			g.Expect(gotDPUServiceList.Items).To(BeEmpty())

			gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceChainList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
				})).To(Succeed())
			g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

			gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
			g.Expect(testClient.List(ctx,
				gotDPUServiceInterfaceList,
				client.InNamespace(dpuDeployment.GetNamespace()),
				client.MatchingLabels{
					"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName()),
				})).To(Succeed())
			g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())

			// Expect the DPUDeployment to be deleted
			err := testClient.Get(ctx, client.ObjectKey{Namespace: dpuDeployment.GetNamespace(), Name: dpuDeployment.GetName()}, &dpuservicev1.DPUDeployment{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).WithTimeout(180 * time.Second).Should(Succeed())

		By("cleanup DPUServiceConfiguration and DPUServiceTemplate")
		dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
		Expect(testClient.Delete(ctx, dpuServiceTemplate)).To(Succeed())
		dpuServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
		Expect(testClient.Delete(ctx, dpuServiceConfiguration)).To(Succeed())
	})
}

func VerifyDeploymentUnderlyingObjectsCreated(ctx context.Context, g Gomega, testClient client.Client, dpuDeployment *dpuservicev1.DPUDeployment) bool {
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
	g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

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
	return g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
	// A couple of dependencies must become ready before the DPUDeployment can take action, therefore we have to wait
	// a little longer here.
}
