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

package controllers

import (
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("DPUDeployment Node Controller", func() {
	defaultPauseDPUServiceReconciler := pauseDPUServiceReconciler.Load()
	defaultPauseDPUServiceTemplateReconciler := pauseDPUServiceTemplateReconciler.Load()
	defaultPauseDPUDeploymentReconciler := pauseDPUDeploymentReconciler.Load()
	defaultDPUDeploymentReconcileDeleteRequeueDuration := reconcileRequeueDuration.Load()
	BeforeEach(func() {
		DeferCleanup(func() {
			pauseDPUServiceReconciler.Store(defaultPauseDPUServiceReconciler)
			pauseDPUServiceTemplateReconciler.Store(defaultPauseDPUServiceTemplateReconciler)
			pauseDPUDeploymentReconciler.Store(defaultPauseDPUDeploymentReconciler)
			reconcileRequeueDuration.Store(defaultDPUDeploymentReconcileDeleteRequeueDuration)
		})

		// These are modified to speed up the testing suite and also simplify the deletion logic
		pauseDPUServiceReconciler.Store(true)
		pauseDPUServiceTemplateReconciler.Store(true)
		pauseDPUDeploymentReconciler.Store(true)
		reconcileRequeueDuration.Store(int64(1 * time.Second))
	})
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		var testNode *corev1.Node
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpudeployment-node-testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Creating a node")
			testNode = &corev1.Node{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpudeployment-node-testnode-"}}
			Expect(testClient.Create(ctx, testNode)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNode)
		})

		It("should update the node label when one dpuservice matches one requestor and the DPUNodeMaintenance has NodeEffectApplied", func() {
			By("Creating a dpuservice")
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Creating a dpunodemaintenance with NodeEffectApplied condition")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuNodeMaintenance.Spec.Requestor = []string{fmt.Sprintf("%s_%s", parentLabel, dpuService.Name)}
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)
			dpuNodeMaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuNodeMaintenance.Generation,
				},
			}
			dpuNodeMaintenance.SetGroupVersionKind(provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			dpuNodeMaintenance.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNodeMaintenance, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node labels are updated")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", dpuService.Name))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that the requestor is removed from DPUNodeMaintenance")
			Eventually(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				g.Expect(gotMaintenance.Spec.Requestor).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should update the node label when multiple dpuservices match requestor and the DPUNodeMaintenance has NodeEffectApplied", func() {
			By("Creating multiple dpuservices")
			dpuService1 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2.Name = "dpu-service-xasdca"
			dpuService2.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-dpu-service-two-version"
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"dpu-service-xasdca"}

			Expect(testClient.Create(ctx, dpuService1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService1)
			Expect(testClient.Create(ctx, dpuService2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService2)

			By("Creating a dpunodemaintenance with NodeEffectApplied condition")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuNodeMaintenance.Spec.Requestor = []string{
				fmt.Sprintf("%s_%s", parentLabel, dpuService1.Name),
				fmt.Sprintf("%s_%s", parentLabel, dpuService2.Name),
			}
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)
			dpuNodeMaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuNodeMaintenance.Generation,
				},
			}
			dpuNodeMaintenance.SetGroupVersionKind(provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			dpuNodeMaintenance.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNodeMaintenance, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node labels are updated")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", dpuService1.Name))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-two-version", dpuService2.Name))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that all requestors are removed from DPUNodeMaintenance")
			Eventually(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				g.Expect(gotMaintenance.Spec.Requestor).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should not remove other requestors from DPUNodeMaintenance when there is more than one dpuservice matching the requestors and the DPUNodeMaintenance has NodeEffectApplied", func() {
			By("Creating multiple dpuservices")
			dpuService1 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2.Name = "dpu-service-xasdca"
			dpuService2.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			// Update the NodeSelector to match the new name
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-dpu-service-two-version"
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"dpu-service-xasdca"}

			Expect(testClient.Create(ctx, dpuService1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService1)
			Expect(testClient.Create(ctx, dpuService2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService2)

			By("Creating a dpunodemaintenance with NodeEffectApplied condition and mixed requestors")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			// Add both matching and non-matching requestors
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			matchingRequestor1 := fmt.Sprintf("%s_%s", parentLabel, dpuService1.Name)
			matchingRequestor2 := fmt.Sprintf("%s_%s", parentLabel, dpuService2.Name)
			nonMatchingRequestor := "non-matching-requestor"
			dpuNodeMaintenance.Spec.Requestor = []string{
				matchingRequestor1,
				matchingRequestor2,
				nonMatchingRequestor,
			}
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)
			dpuNodeMaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuNodeMaintenance.Generation,
				},
			}
			dpuNodeMaintenance.SetGroupVersionKind(provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			dpuNodeMaintenance.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNodeMaintenance, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node labels are updated for matching dpuservices")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", dpuService1.Name))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-two-version", dpuService2.Name))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that only matching requestors are removed from DPUNodeMaintenance")
			Eventually(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				// Only the non-matching requestor should remain
				g.Expect(gotMaintenance.Spec.Requestor).To(ConsistOf(nonMatchingRequestor))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should remove the requestor when there are 2 controllers removing requestors from the DPUNodeMaintenance", func() {
			By("Creating multiple dpuservices")
			dpuService1 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)

			Expect(testClient.Create(ctx, dpuService1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService1)

			By("Creating a dpunodemaintenance with mixed requestors")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			matchingRequestor := fmt.Sprintf("%s_%s", parentLabel, dpuService1.Name)
			otherRequestor := "non-matching-requestor"
			dpuNodeMaintenance.Spec.Requestor = []string{
				matchingRequestor,
				otherRequestor,
			}
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)

			By("Simulating another controller removing the requestor with flux patcher")
			patcher := patch.NewSerialPatcher(dpuNodeMaintenance.DeepCopy(), testClient)
			dpuNodeMaintenance.Spec.Requestor = []string{matchingRequestor}
			Expect(patcher.Patch(ctx, dpuNodeMaintenance, patch.WithFieldOwner("other-controller"))).ToNot(HaveOccurred())

			// We do it this way so that we allow the other controller to remove the requestor first
			By("Patching the DPUNodeMaintenance with the correct NodeEffectApplied to make the controller do the job")
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), dpuNodeMaintenance)).ToNot(HaveOccurred())
			dpuNodeMaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuNodeMaintenance.Generation,
				},
			}
			dpuNodeMaintenance.SetGroupVersionKind(provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			dpuNodeMaintenance.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNodeMaintenance, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node labels are updated for matching dpuservices")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", dpuService1.Name))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that the DPUNodeMaintenance has no requestor")
			Eventually(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				g.Expect(gotMaintenance.Spec.Requestor).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should not take into account standard DPUServices when handling DPUNodeMaintenance", func() {
			By("Creating an in-cluster DPUService")
			inClusterDPUService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, inClusterDPUService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, inClusterDPUService)

			By("Creating a standard DPUService")
			standardDPUService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			standardDPUService.Name = "dpu-service-xasdca"
			standardDPUService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			standardDPUService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-dpu-service-two-version"
			standardDPUService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"dpu-service-xasdca"}
			standardDPUService.Spec.DeployInCluster = ptr.To(false)
			standardDPUService.Spec.Security = &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(false)}
			Expect(testClient.Create(ctx, standardDPUService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, standardDPUService)

			By("Creating a DPUNodeMaintenance with NodeEffectApplied condition")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuNodeMaintenance.Spec.Requestor = []string{
				fmt.Sprintf("%s_%s", parentLabel, inClusterDPUService.Name),
				fmt.Sprintf("%s_%s", parentLabel, standardDPUService.Name),
			}
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)
			dpuNodeMaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuNodeMaintenance.Generation,
				},
			}
			dpuNodeMaintenance.SetGroupVersionKind(provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			dpuNodeMaintenance.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNodeMaintenance, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that only the in-cluster DPUService label is added to the node")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", inClusterDPUService.Name))
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-dpu-service-two-version"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that only the in-cluster DPUService requestor is removed from DPUNodeMaintenance")
			Eventually(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				expectedNonInClusterRequestor := fmt.Sprintf("%s_%s", parentLabel, standardDPUService.Name)
				g.Expect(gotMaintenance.Spec.Requestor).To(ConsistOf(expectedNonInClusterRequestor))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should not update the node labels when there is no dpuservice matching the requestors and the DPUNodeMaintenance has NodeEffectApplied", func() {
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)

			By("Updating the node with label")
			testNode.Labels = make(map[string]string)
			testNode.Labels[dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key] = "some-value"
			testNode.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Node"))
			testNode.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, testNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a dpuservice")
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Creating a dpunodemaintenance with NodeEffectApplied condition but non-matching requestor")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			// Add non matching requestor
			dpuNodeMaintenance.Spec.Requestor = []string{"non-matching-requestor"}
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)
			dpuNodeMaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuNodeMaintenance.Generation,
				},
			}
			dpuNodeMaintenance.SetGroupVersionKind(provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			dpuNodeMaintenance.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNodeMaintenance, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node labels are not updated")
			Consistently(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", "some-value"))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Checking that the requestor is not removed from DPUNodeMaintenance")
			Consistently(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				g.Expect(gotMaintenance.Spec.Requestor).To(ConsistOf("non-matching-requestor"))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should not update the node label when one dpuservice matches one requestor and the DPUNodeMaintenance does not have NodeEffectApplied", func() {
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)

			By("Updating the node with label")
			testNode.Labels = make(map[string]string)
			testNode.Labels[dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key] = "some-value"
			testNode.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Node"))
			testNode.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, testNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a dpuservice")
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Creating a dpunodemaintenance without NodeEffectApplied condition")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuNodeMaintenance.Spec.Requestor = []string{fmt.Sprintf("%s_%s", parentLabel, dpuService.Name)}
			// No NodeEffectApplied condition is present
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)

			By("Trigger reconcile of the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Checking that the node labels are not updated")
			Consistently(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", "some-value"))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Checking that the requestor is not removed from DPUNodeMaintenance")
			Consistently(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				expectedRequestor := fmt.Sprintf("%s_%s", parentLabel, dpuService.Name)
				g.Expect(gotMaintenance.Spec.Requestor).To(ConsistOf(expectedRequestor))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should not update the node label when multiple dpuservices match requestor and the DPUNodeMaintenance does not have NodeEffectApplied", func() {
			dpuService1 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2.Name = "dpu-service-xasdca"
			dpuService2.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-dpu-service-two-version"
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"dpu-service-xasdca"}

			By("Updating the node with labels")
			testNode.Labels = make(map[string]string)
			testNode.Labels[dpuService1.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key] = "some-value1"
			testNode.Labels[dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key] = "some-value2"
			testNode.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Node"))
			testNode.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, testNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating multiple dpuservices")
			Expect(testClient.Create(ctx, dpuService1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService1)
			Expect(testClient.Create(ctx, dpuService2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService2)

			By("Creating a dpunodemaintenance without NodeEffectApplied condition")
			dpuNodeMaintenance := getMinimalDPUNodeMaintenance(testNS.Name)
			dpuNodeMaintenance.Spec.DPUNodeName = testNode.Name
			parentLabel := fmt.Sprintf("%s_test-dpu-deployment", testNS.Name)
			dpuNodeMaintenance.Spec.Requestor = []string{
				fmt.Sprintf("%s_%s", parentLabel, dpuService1.Name),
				fmt.Sprintf("%s_%s", parentLabel, dpuService2.Name),
			}
			// No NodeEffectApplied condition is present
			Expect(testClient.Create(ctx, dpuNodeMaintenance)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeMaintenance)

			By("Checking that the node labels are not updated")
			Consistently(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", "some-value1"))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-two-version", "some-value2"))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Checking that the requestors are not removed from DPUNodeMaintenance")
			Consistently(func(g Gomega) {
				gotMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNodeMaintenance), gotMaintenance)).To(Succeed())
				expectedRequestor1 := fmt.Sprintf("%s_%s", parentLabel, dpuService1.Name)
				expectedRequestor2 := fmt.Sprintf("%s_%s", parentLabel, dpuService2.Name)
				g.Expect(gotMaintenance.Spec.Requestor).To(ConsistOf(expectedRequestor1, expectedRequestor2))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should handle the lifecycle of the label on the node when a service managed by a DPUDeployment is created and no DPUNodeMaintenance object is involved", func() {
			By("Creating a DPUNode for the corev1.Node")
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-name",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "target",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNode)
			dpuNode.Status.KubeNodeRef = ptr.To(testNode.Name)
			dpuNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			dpuNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "test-dpuset",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-type": "target",
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			// This simulates the finalizer removal from the DPUDeployment controller.
			By("Patching the DPUDeployment with a fake finalizer to prevent deletion")
			DeferCleanup(func() {
				By("Cleaning up the finalizers so that the DPUDeployment can be deleted")
				Expect(client.IgnoreNotFound(testClient.Patch(ctx, dpuDeployment, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
			})

			finalizers := dpuDeployment.GetFinalizers()
			finalizers = append(finalizers, "test.io/some-finalizer")
			dpuDeployment.SetFinalizers(finalizers)
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUService that is associated with the DPUDeployment")
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Patching the DPUDeployment with DPUServiceReconciled Condition")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node labels are initially added")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version", dpuService.Name))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Deleting the DPUDeployment")
			Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

			By("Checking that the node labels are removed after DPUDeployment deletion")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-dpu-service-one-version"))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should remove node labels from terminating DPUDeployments while preserving labels from active deployments", func() {
			By("Creating a DPUNode for the corev1.Node")
			dpuNode := &provisioningv1.DPUNode{}
			dpuNode.Name = "some-name"
			dpuNode.Namespace = testNS.Name
			Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNode)
			dpuNode.Status.KubeNodeRef = ptr.To(testNode.Name)
			dpuNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			dpuNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating an active DPUDeployment")
			activeDPUDeployment := getMinimalDPUDeployment(testNS.Name)
			activeDPUDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			activeDPUDeployment.Name = "active-deploy"
			activeDPUDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{{NameSuffix: "test-dpuset"}}
			Expect(testClient.Create(ctx, activeDPUDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, activeDPUDeployment)

			By("Creating a terminating DPUDeployment")
			terminatingDPUDeployment := getMinimalDPUDeployment(testNS.Name)
			terminatingDPUDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			terminatingDPUDeployment.Name = "terminating-deploy"
			terminatingDPUDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{{NameSuffix: "test-dpuset"}}
			Expect(testClient.Create(ctx, terminatingDPUDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, terminatingDPUDeployment)

			// Add finalizers to both deployments to prevent immediate deletion
			By("Adding finalizers to both DPUDeployments")
			for _, deployment := range []*dpuservicev1.DPUDeployment{activeDPUDeployment, terminatingDPUDeployment} {
				finalizers := deployment.GetFinalizers()
				finalizers = append(finalizers, "test.io/some-finalizer")
				deployment.SetFinalizers(finalizers)
				deployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
				deployment.SetManagedFields(nil)
				Expect(testClient.Patch(ctx, deployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}

			DeferCleanup(func() {
				By("Cleaning up the finalizers so that the DPUDeployments can be deleted")
				for _, deployment := range []*dpuservicev1.DPUDeployment{activeDPUDeployment, terminatingDPUDeployment} {
					Expect(client.IgnoreNotFound(testClient.Patch(ctx, deployment, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
				}
			})

			By("Creating DPUServices for both deployments")
			activeDPUService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			activeDPUService.Name = "active-service"
			activeDPUService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, activeDPUDeployment.Name)
			activeDPUService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-active-version"
			activeDPUService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"active-service"}
			Expect(testClient.Create(ctx, activeDPUService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, activeDPUService)

			terminatingDPUService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			terminatingDPUService.Name = "terminating-service"
			terminatingDPUService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, terminatingDPUDeployment.Name)
			terminatingDPUService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-terminating-version"
			terminatingDPUService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"terminating-service"}
			Expect(testClient.Create(ctx, terminatingDPUService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, terminatingDPUService)

			By("Setting DPUServiceReconciled condition on both deployments")
			for _, deployment := range []*dpuservicev1.DPUDeployment{activeDPUDeployment, terminatingDPUDeployment} {
				deployment.Status.Conditions = []metav1.Condition{
					{
						Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: deployment.Generation,
					},
				}
				deployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
				deployment.SetManagedFields(nil)
				Expect(testClient.Status().Patch(ctx, deployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}

			By("Checking that both node labels are initially added")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-active-version", "active-service"))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-terminating-version", "terminating-service"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Marking the terminating DPUDeployment for deletion")
			Expect(testClient.Delete(ctx, terminatingDPUDeployment)).To(Succeed())

			By("Checking that only the terminating deployment's label is removed while the active deployment's label remains")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-active-version", "active-service"))
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-terminating-version"))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should remove all node labels from multiple DPUServices belonging to a terminating DPUDeployment", func() {
			By("Creating a DPUNode for the corev1.Node")
			dpuNode := &provisioningv1.DPUNode{}
			dpuNode.Name = "some-name"
			dpuNode.Namespace = testNS.Name
			Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNode)
			dpuNode.Status.KubeNodeRef = ptr.To(testNode.Name)
			dpuNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			dpuNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDeployment with multiple DPUServices")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Name = "multi-service-deploy"
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{{NameSuffix: "test-dpuset"}}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			// Add finalizer to prevent immediate deletion
			By("Adding finalizer to DPUDeployment")
			finalizers := dpuDeployment.GetFinalizers()
			finalizers = append(finalizers, "test.io/some-finalizer")
			dpuDeployment.SetFinalizers(finalizers)
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			DeferCleanup(func() {
				By("Cleaning up the finalizer so that the DPUDeployment can be deleted")
				Expect(client.IgnoreNotFound(testClient.Patch(ctx, dpuDeployment, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
			})

			By("Creating multiple DPUServices for the same deployment")
			dpuService1 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService1.Name = "service-one"
			dpuService1.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService1.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-service-one-version"
			dpuService1.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"service-one"}
			Expect(testClient.Create(ctx, dpuService1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService1)

			dpuService2 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2.Name = "service-two"
			dpuService2.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-service-two-version"
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"service-two"}
			Expect(testClient.Create(ctx, dpuService2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService2)

			dpuService3 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService3.Name = "service-three"
			dpuService3.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService3.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-service-three-version"
			dpuService3.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"service-three"}
			Expect(testClient.Create(ctx, dpuService3)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService3)

			By("Setting DPUServiceReconciled condition on the deployment")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that all node labels are initially added")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-service-one-version", "service-one"))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-service-two-version", "service-two"))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-service-three-version", "service-three"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Marking the DPUDeployment for deletion")
			Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

			By("Checking that all labels from the terminating deployment are removed")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-service-one-version"))
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-service-two-version"))
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-service-three-version"))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should only add labels to nodes that match the DPUDeployment's DPUSet NodeSelector", func() {
			By("Creating multiple nodes")
			matchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{GenerateName: "matching-node-"}}
			Expect(testClient.Create(ctx, matchingNode)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, matchingNode)

			nonMatchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{GenerateName: "non-matching-node-"}}
			Expect(testClient.Create(ctx, nonMatchingNode)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, nonMatchingNode)

			By("Creating DPUNodes with different labels")
			matchingDPUNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "matching-dpu-node",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "target",
						"region":    "us-west",
					},
				},
			}
			Expect(testClient.Create(ctx, matchingDPUNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, matchingDPUNode)
			matchingDPUNode.Status.KubeNodeRef = ptr.To(matchingNode.Name)
			matchingDPUNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			matchingDPUNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, matchingDPUNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			nonMatchingDPUNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-matching-dpu-node",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "other",
						"region":    "us-east",
					},
				},
			}
			Expect(testClient.Create(ctx, nonMatchingDPUNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, nonMatchingDPUNode)
			nonMatchingDPUNode.Status.KubeNodeRef = ptr.To(nonMatchingNode.Name)
			nonMatchingDPUNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			nonMatchingDPUNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nonMatchingDPUNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDeployment that targets only the matching DPUNode")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Name = "selective-deploy"
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "test-dpuset",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-type": "target",
							"region":    "us-west",
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Creating a DPUService for the deployment")
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService.Name = "selective-service"
			dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-selective-version"
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"selective-service"}
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Setting DPUServiceReconciled condition on the deployment")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that only the matching node gets the label")
			Eventually(func(g Gomega) {
				gotMatchingNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(matchingNode), gotMatchingNode)).To(Succeed())
				g.Expect(gotMatchingNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-selective-version", "selective-service"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that the non-matching node does not get the label")
			Consistently(func(g Gomega) {
				gotNonMatchingNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nonMatchingNode), gotNonMatchingNode)).To(Succeed())
				g.Expect(gotNonMatchingNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-selective-version"))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should only add labels to nodes that have DPUDevices matching the DPUSet DPUDeviceSelector", func() {
			By("Creating multiple nodes")
			matchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{GenerateName: "matching-node-"}}
			Expect(testClient.Create(ctx, matchingNode)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, matchingNode)

			nonMatchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{GenerateName: "non-matching-node-"}}
			Expect(testClient.Create(ctx, nonMatchingNode)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, nonMatchingNode)

			By("Creating DPUNodes - both matching the DPUNodeSelector")
			matchingDPUNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-selector-matching-dpu-node",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "device-selector-target",
					},
				},
			}
			Expect(testClient.Create(ctx, matchingDPUNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, matchingDPUNode)
			matchingDPUNode.Status.KubeNodeRef = ptr.To(matchingNode.Name)
			matchingDPUNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			matchingDPUNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, matchingDPUNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			nonMatchingDPUNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-selector-non-matching-dpu-node",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "device-selector-target",
					},
				},
			}
			Expect(testClient.Create(ctx, nonMatchingDPUNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, nonMatchingDPUNode)
			nonMatchingDPUNode.Status.KubeNodeRef = ptr.To(nonMatchingNode.Name)
			nonMatchingDPUNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			nonMatchingDPUNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nonMatchingDPUNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDevice matching the DPUDeviceSelector attached to the matching DPUNode")
			matchingDPUDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-selector-matching-device",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"device-type": "accelerator",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{SerialNumber: "SN-matching"},
			}
			Expect(testClient.Create(ctx, matchingDPUDevice)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, matchingDPUDevice)

			By("Creating a DPUDevice NOT matching the DPUDeviceSelector attached to the non-matching DPUNode")
			nonMatchingDPUDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-selector-non-matching-device",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"device-type": "standard",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{SerialNumber: "SN-non-matching"},
			}
			Expect(testClient.Create(ctx, nonMatchingDPUDevice)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, nonMatchingDPUDevice)

			By("Adding DPUDevices to their respective DPUNodes' spec.dpus")
			originalMatchingDPUNode := matchingDPUNode.DeepCopy()
			matchingDPUNode.Spec.DPUs = []provisioningv1.DPURef{{Name: matchingDPUDevice.Name}}
			Expect(testClient.Patch(ctx, matchingDPUNode, client.MergeFrom(originalMatchingDPUNode))).To(Succeed())

			originalNonMatchingDPUNode := nonMatchingDPUNode.DeepCopy()
			nonMatchingDPUNode.Spec.DPUs = []provisioningv1.DPURef{{Name: nonMatchingDPUDevice.Name}}
			Expect(testClient.Patch(ctx, nonMatchingDPUNode, client.MergeFrom(originalNonMatchingDPUNode))).To(Succeed())

			By("Creating a DPUDeployment targeting all DPUNodes but filtering by DPUDeviceSelector")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Name = "device-sel-deploy"
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "test-dpuset",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-type": "device-selector-target",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"device-type": "accelerator",
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Creating a DPUService for the deployment")
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService.Name = "device-selector-service"
			dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-device-selector-version"
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"device-selector-service"}
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Setting DPUServiceReconciled condition on the deployment")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that only the node with a matching DPUDevice gets the label")
			Eventually(func(g Gomega) {
				gotMatchingNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(matchingNode), gotMatchingNode)).To(Succeed())
				g.Expect(gotMatchingNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-device-selector-version", "device-selector-service"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking that the node without a matching DPUDevice does not get the label")
			Consistently(func(g Gomega) {
				gotNonMatchingNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nonMatchingNode), gotNonMatchingNode)).To(Succeed())
				g.Expect(gotNonMatchingNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-device-selector-version"))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should handle the lifecycle of the label on the node when a DPUDevice label is added and then removed", func() {
			By("Creating a DPUNode for the corev1.Node")
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpudevice-label-lifecycle-dpu-node",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "dpudevice-label-lifecycle-target",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNode)
			dpuNode.Status.KubeNodeRef = ptr.To(testNode.Name)
			dpuNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			dpuNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDevice with a matching label")
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpudevice-label-lifecycle-device",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"device-type": "dpudevice-label-lifecycle",
					},
				},
				Spec: provisioningv1.DPUDeviceSpec{SerialNumber: "SN-dpudevice-label-lifecycle"},
			}
			Expect(testClient.Create(ctx, dpuDevice)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDevice)

			By("Adding the DPUDevice to the DPUNode's spec.DPUs")
			originalDPUNode := dpuNode.DeepCopy()
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
			Expect(testClient.Patch(ctx, dpuNode, client.MergeFrom(originalDPUNode))).To(Succeed())

			By("Creating a DPUDeployment targeting DPUNodes with DPUDeviceSelector")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Name = "dev-lbl-lc"
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "test-dpuset",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-type": "dpudevice-label-lifecycle-target",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"device-type": "dpudevice-label-lifecycle",
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Patching the DPUDeployment with a fake finalizer to prevent deletion")
			DeferCleanup(func() {
				By("Cleaning up the finalizers so that the DPUDeployment can be deleted")
				Expect(client.IgnoreNotFound(testClient.Patch(ctx, dpuDeployment, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
			})
			finalizers := dpuDeployment.GetFinalizers()
			finalizers = append(finalizers, "test.io/some-finalizer")
			dpuDeployment.SetFinalizers(finalizers)
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUService associated with the DPUDeployment")
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService.Name = "dpudevice-label-lifecycle-svc"
			dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-dpudevice-label-lifecycle-version"
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"dpudevice-label-lifecycle-svc"}
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Setting DPUServiceReconciled condition on the DPUDeployment")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node label is added")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpudevice-label-lifecycle-version", "dpudevice-label-lifecycle-svc"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Removing the matching label from the DPUDevice")
			originalDPUDevice := dpuDevice.DeepCopy()
			dpuDevice.Labels = map[string]string{}
			Expect(testClient.Patch(ctx, dpuDevice, client.MergeFrom(originalDPUDevice))).To(Succeed())

			By("Checking that the node label is removed")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-dpudevice-label-lifecycle-version"))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should handle the lifecycle of the label on the node when a DPUNode label is added and then removed", func() {
			By("Creating a DPUNode with a matching label for the corev1.Node")
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpunode-label-lifecycle-dpu-node",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "dpunode-label-lifecycle-target",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNode)
			dpuNode.Status.KubeNodeRef = ptr.To(testNode.Name)
			dpuNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			dpuNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDeployment with a DPUNodeSelector matching the DPUNode's label")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Name = "node-lbl-lc"
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "test-dpuset",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-type": "dpunode-label-lifecycle-target",
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Patching the DPUDeployment with a fake finalizer to prevent deletion")
			DeferCleanup(func() {
				By("Cleaning up the finalizers so that the DPUDeployment can be deleted")
				Expect(client.IgnoreNotFound(testClient.Patch(ctx, dpuDeployment, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
			})
			finalizers := dpuDeployment.GetFinalizers()
			finalizers = append(finalizers, "test.io/some-finalizer")
			dpuDeployment.SetFinalizers(finalizers)
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUService associated with the DPUDeployment")
			dpuService := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService.Name = "dpunode-label-lifecycle-svc"
			dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-dpunode-label-lifecycle-version"
			dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"dpunode-label-lifecycle-svc"}
			Expect(testClient.Create(ctx, dpuService)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService)

			By("Setting DPUServiceReconciled condition on the DPUDeployment")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that the node label is added")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-dpunode-label-lifecycle-version", "dpunode-label-lifecycle-svc"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Removing the matching label from the DPUNode")
			originalDPUNode := dpuNode.DeepCopy()
			dpuNode.Labels = map[string]string{}
			Expect(testClient.Patch(ctx, dpuNode, client.MergeFrom(originalDPUNode))).To(Succeed())

			By("Checking that the node label is removed")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-dpunode-label-lifecycle-version"))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should remove stale node labels when DPUServices are deleted", func() {
			By("Creating a DPUNode for the corev1.Node")
			dpuNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-name",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"node-type": "target",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNode)
			dpuNode.Status.KubeNodeRef = ptr.To(testNode.Name)
			dpuNode.SetGroupVersionKind(provisioningv1.DPUNodeGroupVersionKind)
			dpuNode.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuNode, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Creating a DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.BFB = ptr.To("somebfb")
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "test-dpuset",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-type": "target",
						},
					},
				},
			}
			dpuDeployment.Spec.Services = map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
				"service-one": {
					ServiceTemplate:      "service-template-one",
					ServiceConfiguration: "service-configuration-one",
				},
				"service-two": {
					ServiceTemplate:      "service-template-one",
					ServiceConfiguration: "service-configuration-one",
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Creating multiple DPUServices for the deployment")
			dpuService1 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService1.Name = "service-one"
			dpuService1.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService1.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-in-cluster-version-abc123"
			dpuService1.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"service-one"}
			Expect(testClient.Create(ctx, dpuService1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService1)

			dpuService2 := getMinimalInClusterDPUServiceCreatedByDPUDeployment(testNS.Name)
			dpuService2.Name = "service-two"
			dpuService2.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] = fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Key = "svc.dpu.nvidia.com/dpuservice-in-cluster-version-def456"
			dpuService2.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"service-two"}
			Expect(testClient.Create(ctx, dpuService2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuService2)

			By("Setting DPUServiceReconciled condition on the deployment")
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that both node labels are initially added")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-in-cluster-version-abc123", "service-one"))
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-in-cluster-version-def456", "service-two"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Simulating a DPUDeployment change that removes the service")
			// Patching the DPUDeployment without the first service
			originalDPUDeployment := dpuDeployment.DeepCopy()
			dpuDeployment.Spec.Services = map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
				"service-two": {
					ServiceTemplate:      "service-template-one",
					ServiceConfiguration: "service-configuration-one",
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			// client.MergeFrom to avoid SSA issues with removing the service from the original DPUDeployment
			Expect(testClient.Patch(ctx, dpuDeployment, client.MergeFrom(originalDPUDeployment))).To(Succeed())

			// Deleting the DPUService
			Expect(testutils.CleanupAndWait(ctx, testClient, dpuService1)).To(Succeed())

			// Patching the DPUDeployment with DPUServiceReconciled condition
			dpuDeployment.Status.Conditions = []metav1.Condition{
				{
					Type:               string(dpuservicev1.ConditionDPUServicesReconciled),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: dpuDeployment.Generation,
				},
			}
			dpuDeployment.SetGroupVersionKind(dpuservicev1.DPUDeploymentGroupVersionKind)
			dpuDeployment.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, dpuDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking that stale labels are removed while active labels remain")
			Eventually(func(g Gomega) {
				gotNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testNode), gotNode)).To(Succeed())
				// The stale label should be removed
				// The deleted service's label should be removed
				g.Expect(gotNode.Labels).ToNot(HaveKey("svc.dpu.nvidia.com/dpuservice-in-cluster-version-abc123"))
				// The remaining service's label should still be present
				g.Expect(gotNode.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-in-cluster-version-def456", "service-two"))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})
	})
})

// getMinimalDPUNodeMaintenance returns the minimal DPUNodeMaintenance object that can be applied
func getMinimalDPUNodeMaintenance(namespace string) *provisioningv1.DPUNodeMaintenance {
	return &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpunodemaintenance",
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{
			DPUNodeName: "somenode",
		},
	}
}

// getMinimalInClusterDPUServiceCreatedByDPUDeployment creates a minimal in cluster DPUService with a NodeSelector that
// matches what DPUDeployment produces. It doesn't include all the fields, but only the relevant fields for the
// DPUDeployment Node controller
func getMinimalInClusterDPUServiceCreatedByDPUDeployment(namespace string) *dpuservicev1.DPUService {
	parentLabel := fmt.Sprintf("%s_test-dpu-deployment", namespace)
	return &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-service-a1zcz",
			Namespace: namespace,
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: parentLabel,
			},
		},
		Spec: dpuservicev1.DPUServiceSpec{
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					RepoURL:     "oci://repository.com",
					Version:     "v1.2",
					Chart:       "second-chart",
					ReleaseName: "release-two",
				},
			},
			DeployInCluster: ptr.To(true),
			ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
				NodeSelector: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "svc.dpu.nvidia.com/dpuservice-dpu-service-one-version",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"dpu-service-a1zcz"},
								},
								{
									Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{parentLabel},
								},
							},
						},
					},
				},
			},
		},
	}
}
