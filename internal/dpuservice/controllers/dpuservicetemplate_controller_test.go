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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPUServiceTemplate Controller", func() {
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		It("should successfully reconcile the DPUServiceTemplate", func() {
			By("reconciling the created resource")
			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("checking that finalizer is added")
			Eventually(func(g Gomega) []string {
				got := &dpuservicev1.DPUServiceTemplate{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceTemplate), got)).To(Succeed())
				return got.Finalizers
			}).WithTimeout(30 * time.Second).Should(ConsistOf([]string{dpuservicev1.DPUServiceTemplateFinalizer}))

			By("checking that the resource can be deleted (finalizer is removed)")
			Expect(testutils.CleanupAndWait(ctx, testClient, dpuServiceTemplate)).To(Succeed())
		})
	})
	Context("When checking the status transitions", func() {
		var testNS *corev1.Namespace
		var i *informer.TestInformer
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Creating the informer infrastructure for DPUServiceTemplate")
			i = informer.NewInformer(cfg, dpuservicev1.DPUServiceTemplateGroupVersionKind, testNS.Name, "dpuservicetemplates")
			DeferCleanup(i.Cleanup)
			go i.Run()
		})
		It("DPUServiceTemplate has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceTemplate{}
				newObj := &dpuservicev1.DPUServiceTemplate{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(BeEmpty())
				g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceTemplateReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceTemplate has all the conditions with Success Reason at the end of a successful reconciliation loop", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceTemplate{}
				newObj := &dpuservicev1.DPUServiceTemplate{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionDPUServiceTemplateReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceTemplateReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUServiceTemplate has DPUServiceTemplateReconciled error if extracting annotations from the chart failed for some reason", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)

			chartHelper.ReturnErrorForChart(dpuServiceTemplate.Spec.HelmChart.Source)
			DeferCleanup(chartHelper.Reset)

			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceTemplate{}
				newObj := &dpuservicev1.DPUServiceTemplate{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionDPUServiceTemplateReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceTemplateReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonError)),
					HaveField("Message", ContainSubstring("error for")),
				),
			))
		})
	})
	Context("When testing the internal cache of versions", func() {
		var namespacedNameToHash map[types.NamespacedName]string
		var versionsForChart map[string]map[string]string
		BeforeEach(func() {
			namespacedNameToHash = make(map[types.NamespacedName]string)
			versionsForChart = make(map[string]map[string]string)
		})
		It("should add, retrieve and remove the versions for a DPUServiceTemplate", func() {
			By("Adding the versions")
			dpuServiceTemplate := getMinimalDPUServiceTemplate("myns")
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate, map[string]string{"doca-version": "2.9"})

			By("Retrieving the versions")
			versions, found := getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate)
			Expect(found).To(BeTrue())
			Expect(versions).To(BeEquivalentTo(map[string]string{"doca-version": "2.9"}))
			Expect(versionsForChart).To(HaveLen(1))
			Expect(namespacedNameToHash).To(HaveLen(1))

			By("Removing the versions")
			deleteVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate)
			Expect(versionsForChart).To(BeEmpty())
			Expect(namespacedNameToHash).To(BeEmpty())
		})
		It("should not return any version if DPUServiceTemplate was not processed before", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate("myns")
			_, found := getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate)
			Expect(found).To(BeFalse())
		})
		It("should retrieve the correct versions for an unchanged, processed DPUServiceTemplate", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate("myns")
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate, map[string]string{"doca-version": "2.9"})
			versions, found := getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate)
			Expect(found).To(BeTrue())
			Expect(versions).To(BeEquivalentTo(map[string]string{"doca-version": "2.9"}))
		})
		It("should cleanup the outdated version for an updated DPUServiceTemplate", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate("myns")
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate, map[string]string{"doca-version": "2.9"})
			dpuServiceTemplate.Spec.HelmChart.Source.Chart = "some-other-chart"
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate, map[string]string{"doca-version": "2.10"})
			versions, found := getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate)
			Expect(found).To(BeTrue())
			Expect(versions).To(BeEquivalentTo(map[string]string{"doca-version": "2.10"}))
			Expect(versionsForChart).To(HaveLen(1))
		})
		It("should cleanup the versions for an updated DPUServiceTemplate", func() {
			dpuServiceTemplate := getMinimalDPUServiceTemplate("myns")
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate, map[string]string{"doca-version": "2.9"})
			dpuServiceTemplate.Spec.HelmChart.Source.Chart = "some-other-chart"
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate, map[string]string{"doca-version": "2.10"})
			versions, found := getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate)
			Expect(found).To(BeTrue())
			Expect(versions).To(BeEquivalentTo(map[string]string{"doca-version": "2.10"}))
			Expect(versionsForChart).To(HaveLen(1))
		})
		It("should handle 2 DPUServiceTemplates with the same chart", func() {
			By("Adding the versions")
			dpuServiceTemplate1 := getMinimalDPUServiceTemplate("myns")
			dpuServiceTemplate1.SetName("template1")
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate1, map[string]string{"doca-version": "2.9"})
			dpuServiceTemplate2 := getMinimalDPUServiceTemplate("myns")
			dpuServiceTemplate2.SetName("template2")
			setVersionsInMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate2, map[string]string{"doca-version": "2.9"})

			By("Retrieving the versions")
			versions, found := getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate1)
			Expect(found).To(BeTrue())
			Expect(versions).To(BeEquivalentTo(map[string]string{"doca-version": "2.9"}))
			versions, found = getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate2)
			Expect(found).To(BeTrue())
			Expect(versions).To(BeEquivalentTo(map[string]string{"doca-version": "2.9"}))
			Expect(versionsForChart).To(HaveLen(1))
			Expect(namespacedNameToHash).To(HaveLen(2))

			By("Removing one of the DPUServiceTemplates")
			deleteVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate1)
			Expect(versionsForChart).To(BeEmpty())
			Expect(namespacedNameToHash).To(HaveLen(1))

			By("Retrieving the version for the remaining DPUServiceTemplate")
			_, found = getVersionsFromMemory(namespacedNameToHash, versionsForChart, dpuServiceTemplate2)
			Expect(found).To(BeFalse())
		})
	})
	Context("When testing the version filtering", func() {
		DescribeTable("filters the versions correctly", func(input map[string]string, expectedOutput map[string]string) {
			Expect(filterVersions(input)).To(BeComparableTo(expectedOutput))
		},
			Entry("nil input", nil, nil),
			Entry("empty input", map[string]string{}, map[string]string{}),
			Entry("no supported versions", map[string]string{"one": "1", "two": "2"}, map[string]string{}),
			Entry("some supported versions", map[string]string{"dpu.nvidia.com/doca-version": "1.2.3", "one": "1"}, map[string]string{"dpu.nvidia.com/doca-version": "1.2.3"}),
			Entry("only supported versions", map[string]string{"dpu.nvidia.com/doca-version": "1.2.3"}, map[string]string{"dpu.nvidia.com/doca-version": "1.2.3"}),
		)
	})
})
