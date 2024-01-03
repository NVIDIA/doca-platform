/*
Copyright 2024 NVIDIA

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
	"github.com/nvidia/doca-platform/internal/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//nolint:goconst
var _ = Describe("DPUDeployment Controller", func() {
	defaultPauseDPUServiceReconciler := pauseDPUServiceReconciler
	defaultDPUDeploymentReconcileDeleteRequeueDuration := dpuDeploymentReconcileDeleteRequeueDuration
	BeforeEach(func() {
		DeferCleanup(func() {
			pauseDPUServiceReconciler = defaultPauseDPUServiceReconciler
			dpuDeploymentReconcileDeleteRequeueDuration = defaultDPUDeploymentReconcileDeleteRequeueDuration
		})

		// These are modified to speed up the testing suite and also simplify the deletion logic
		pauseDPUServiceReconciler = true
		dpuDeploymentReconcileDeleteRequeueDuration = 1 * time.Second
	})
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		It("should successfully reconcile the DPUDeployment", func() {
			By("reconciling the created resource")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("checking that finalizer is added")
			Eventually(func(g Gomega) []string {
				got := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), got)).To(Succeed())
				return got.Finalizers
			}).WithTimeout(30 * time.Second).Should(ConsistOf([]string{dpuservicev1.DPUDeploymentFinalizer}))

			By("checking that the resource can be deleted (finalizer is removed)")
			Expect(testutils.CleanupAndWait(ctx, testClient, dpuDeployment)).To(Succeed())
		})
		It("should not create DPUSet, DPUService, DPUServiceChain and DPUServiceInterface if any of the dependencies does not exist", func() {
			By("reconciling the created resource")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("checking that no object is created")
			Consistently(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(BeEmpty())

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(BeEmpty())

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
		It("should cleanup child objects on delete", func() {
			By("Creating the dependencies")
			bfb := getMinimalBFB("somebfb", testNS.Name)
			Expect(testClient.Create(ctx, bfb)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)

			By("creating the dpudeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
					DPUAnnotations: map[string]string{
						"annotationkey1": "annotationvalue1",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("checking that dependencies are marked")
			Eventually(func(g Gomega) {
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					g.Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetFinalizers()).To(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetLabels()).To(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("checking that objects are created")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(HaveLen(1))

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("deleting the resource")
			Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

			By("checking that the child resources are removed")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(BeEmpty())

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(BeEmpty())

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("checking that the dependencies are released")
			Eventually(func(g Gomega) {
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					g.Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetFinalizers()).ToNot(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
		It("should not delete the DPUSets until the rest of the child objects are deleted", func() {
			By("Creating the dependencies")
			bfb := getMinimalBFB("somebfb", testNS.Name)
			Expect(testClient.Create(ctx, bfb)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)

			By("creating the dpudeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
					DPUAnnotations: map[string]string{
						"annotationkey1": "annotationvalue1",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			objs := make(map[client.Object]interface{})
			By("checking that objects are created")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(HaveLen(1))

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				objs[&gotDPUServiceList.Items[0]] = struct{}{}

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
				objs[&gotDPUServiceChainList.Items[0]] = struct{}{}

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
				objs[&gotDPUServiceInterfaceList.Items[0]] = struct{}{}
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Patching the objects with a fake finalizer to prevent deletion")
			DeferCleanup(func() {
				By("Cleaning up the finalizers so that objects can be deleted")
				for obj := range objs {
					Expect(client.IgnoreNotFound(testClient.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
				}
			})
			gotDPUServiceList := &dpuservicev1.DPUServiceList{}
			Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())

			gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
			Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())

			gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
			Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
			for obj, gvk := range map[client.Object]schema.GroupVersionKind{
				&gotDPUServiceList.Items[0]:          dpuservicev1.DPUServiceGroupVersionKind,
				&gotDPUServiceChainList.Items[0]:     dpuservicev1.DPUServiceChainGroupVersionKind,
				&gotDPUServiceInterfaceList.Items[0]: dpuservicev1.DPUServiceInterfaceGroupVersionKind,
			} {
				finalizers := obj.GetFinalizers()
				finalizers = append(finalizers, "test.io/some-finalizer")
				obj.SetFinalizers(finalizers)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				obj.SetManagedFields(nil)
				Expect(testClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}

			By("deleting the resource")
			Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

			By("checking that all child objects but the DPUSets have deletion timestamp")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(HaveLen(1))
				g.Expect(gotDPUSetList.Items[0].DeletionTimestamp).To(BeNil())

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				g.Expect(gotDPUServiceList.Items[0].DeletionTimestamp).ToNot(BeNil())

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
				g.Expect(gotDPUServiceChainList.Items[0].DeletionTimestamp).ToNot(BeNil())

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
				g.Expect(gotDPUServiceInterfaceList.Items[0].DeletionTimestamp).ToNot(BeNil())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Removing the fake finalizer from the objects")
			gotDPUServiceList = &dpuservicev1.DPUServiceList{}
			Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())

			gotDPUServiceChainList = &dpuservicev1.DPUServiceChainList{}
			Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())

			gotDPUServiceInterfaceList = &dpuservicev1.DPUServiceInterfaceList{}
			Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
			for obj, gvk := range map[client.Object]schema.GroupVersionKind{
				&gotDPUServiceList.Items[0]:          dpuservicev1.DPUServiceGroupVersionKind,
				&gotDPUServiceChainList.Items[0]:     dpuservicev1.DPUServiceChainGroupVersionKind,
				&gotDPUServiceInterfaceList.Items[0]: dpuservicev1.DPUServiceInterfaceGroupVersionKind,
			} {
				obj.SetFinalizers([]string{})
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				obj.SetManagedFields(nil)
				Expect(testClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}

			By("checking that the child resources are removed")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(BeEmpty())

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(BeEmpty())

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})
	})
	Context("When unit testing individual functions", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		Context("When checking getDependencies()", func() {
			It("should return the correct object", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Checking the output of the function")
				deps, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).ToNot(HaveOccurred())
				Expect(deps).To(BeComparableTo(&dpuDeploymentDependencies{
					DPUFlavor: dpuFlavor,
					BFB:       bfb,
					DPUServiceConfigurations: map[string]*dpuservicev1.DPUServiceConfiguration{
						"someservice": dpuServiceConfiguration,
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"someservice": dpuServiceTemplate,
					},
				}))
			})
			It("should error if a dependency doesn't exist", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Checking the output of the function")
				_, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).To(HaveOccurred())
			})
			It("should error if a DPUServiceConfiguration doesn't match DPUDeployment service", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.DeploymentServiceName = "wrong-service"
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Checking the output of the function")
				_, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).To(HaveOccurred())
			})
			It("should error if a DPUServiceTemplate doesn't match DPUDeployment service", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Spec.DeploymentServiceName = "wrong-service"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Checking the output of the function")
				_, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).To(HaveOccurred())
			})
		})
		Context("When checking updateDependencies()", func() {
			var (
				dpuDeployment                *dpuservicev1.DPUDeployment
				bfb                          *provisioningv1.BFB
				extraBFB                     *provisioningv1.BFB
				dpuFlavor                    *provisioningv1.DPUFlavor
				extraDPUFlavor               *provisioningv1.DPUFlavor
				dpuServiceConfiguration      *dpuservicev1.DPUServiceConfiguration
				extraDPUServiceConfiguration *dpuservicev1.DPUServiceConfiguration
				dpuServiceTemplate           *dpuservicev1.DPUServiceTemplate
				extraDPUServiceTemplate      *dpuservicev1.DPUServiceTemplate
				objGVK                       map[client.Object]schema.GroupVersionKind
			)
			BeforeEach(func() {
				dpuDeployment = getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb = getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				extraBFB = getMinimalBFB("somebfb", testNS.Name)
				extraBFB.Name = "extra-bfb"
				Expect(testClient.Create(ctx, extraBFB)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, extraBFB)

				dpuFlavor = getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				extraDPUFlavor = getMinimalDPUFlavor(testNS.Name)
				extraDPUFlavor.Name = "extra-dpuflavor"
				Expect(testClient.Create(ctx, extraDPUFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, extraDPUFlavor)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				extraDPUServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				extraDPUServiceConfiguration.Name = "extra-dpuserviceconfiguration"
				Expect(testClient.Create(ctx, extraDPUServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, extraDPUServiceConfiguration)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				extraDPUServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				extraDPUServiceTemplate.Name = "extra-dpuservicetemplate"
				Expect(testClient.Create(ctx, extraDPUServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, extraDPUServiceTemplate)

				objGVK = map[client.Object]schema.GroupVersionKind{
					bfb:                          provisioningv1.BFBGroupVersionKind,
					extraBFB:                     provisioningv1.BFBGroupVersionKind,
					dpuFlavor:                    provisioningv1.DPUFlavorGroupVersionKind,
					extraDPUFlavor:               provisioningv1.DPUFlavorGroupVersionKind,
					dpuServiceConfiguration:      dpuservicev1.DPUServiceConfigurationGroupVersionKind,
					extraDPUServiceConfiguration: dpuservicev1.DPUServiceConfigurationGroupVersionKind,
					dpuServiceTemplate:           dpuservicev1.DPUServiceTemplateGroupVersionKind,
					extraDPUServiceTemplate:      dpuservicev1.DPUServiceTemplateGroupVersionKind,
				}
				DeferCleanup(func() {
					By("Cleaning up the finalizers so that objects can be deleted")
					for obj := range objGVK {
						Expect(testClient.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))).To(Succeed())
					}
				})
			})
			It("should mark only the current dependencies", func() {
				By("Constructing the dependencies object")
				deps, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).ToNot(HaveOccurred())

				By("Updating the dependencies")
				Expect(updateDependencies(ctx, testClient, dpuDeployment, deps)).To(Succeed())

				By("Checking the current dependencies after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}

				By("Checking the rest of the objects after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(extraBFB),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(extraDPUFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(extraDPUServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(extraDPUServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).ToNot(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}
			})
			It("should clean only the stale dependencies", func() {
				By("Constructing the dependencies object")
				deps, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).ToNot(HaveOccurred())

				By("Updating the dependencies")
				Expect(updateDependencies(ctx, testClient, dpuDeployment, deps)).To(Succeed())

				By("Checking the current dependencies after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}

				By("Checking the rest of the objects after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(extraBFB),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(extraDPUFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(extraDPUServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(extraDPUServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).ToNot(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}
				By("Updating the DPUDeployment deps")
				svc := dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      extraDPUServiceTemplate.Name,
					ServiceConfiguration: extraDPUServiceConfiguration.Name,
				}
				dpuDeployment.Spec.Services["someservice"] = svc
				dpuDeployment.Spec.DPUs.BFB = extraBFB.Name
				dpuDeployment.Spec.DPUs.Flavor = extraDPUFlavor.Name

				By("Constructing the dependencies object")
				deps, err = getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).ToNot(HaveOccurred())

				By("Updating the dependencies")
				Expect(updateDependencies(ctx, testClient, dpuDeployment, deps)).To(Succeed())

				By("Checking the current dependencies after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(extraBFB),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(extraDPUFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(extraDPUServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(extraDPUServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}

				By("Checking the rest of the objects after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).ToNot(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
				}
			})
			It("should be able to mark and clean stale dependencies that other controller have applied finalizers and labels to", func() {
				By("Service side applying the dependencies with finalizers and labels")
				for obj, gvk := range objGVK {
					obj.SetFinalizers([]string{"test.io/some-finalizer"})
					obj.SetLabels(map[string]string{"some": "label"})
					obj.GetObjectKind().SetGroupVersionKind(gvk)
					obj.SetManagedFields(nil)
					Expect(testClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("Constructing the dependencies object")
				deps, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).ToNot(HaveOccurred())

				By("Updating the dependencies")
				Expect(updateDependencies(ctx, testClient, dpuDeployment, deps)).To(Succeed())

				By("Checking the current dependencies after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElements(dpuservicev1.DPUDeploymentFinalizer, "test.io/some-finalizer"), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(And(
						HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name),
						HaveKeyWithValue("some", "label"),
					), fmt.Sprintf("%T", obj))
				}

				By("Checking the rest of the objects after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(extraBFB),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(extraDPUFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(extraDPUServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(extraDPUServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).ToNot(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElement("test.io/some-finalizer"), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(HaveKeyWithValue("some", "label"), fmt.Sprintf("%T", obj))
				}

				By("Service side applying the dependencies again with finalizers and labels")
				for obj, gvk := range objGVK {
					Expect(testClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
					controllerutil.AddFinalizer(obj, "test.io/some-finalizer")
					labels := obj.GetLabels()
					labels["some"] = "label"
					obj.SetLabels(labels)
					obj.GetObjectKind().SetGroupVersionKind(gvk)
					obj.SetManagedFields(nil)
					Expect(testClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("Updating the DPUDeployment deps")
				svc := dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      extraDPUServiceTemplate.Name,
					ServiceConfiguration: extraDPUServiceConfiguration.Name,
				}
				dpuDeployment.Spec.Services["someservice"] = svc
				dpuDeployment.Spec.DPUs.BFB = extraBFB.Name
				dpuDeployment.Spec.DPUs.Flavor = extraDPUFlavor.Name

				By("Constructing the dependencies object")
				deps, err = getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).ToNot(HaveOccurred())

				By("Updating the dependencies")
				Expect(updateDependencies(ctx, testClient, dpuDeployment, deps)).To(Succeed())

				By("Checking the current dependencies after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(extraBFB),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(extraDPUFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(extraDPUServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(extraDPUServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElements(dpuservicev1.DPUDeploymentFinalizer, "test.io/some-finalizer"), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(And(
						HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name),
						HaveKeyWithValue("some", "label"),
					), fmt.Sprintf("%T", obj))
				}

				By("Checking the rest of the objects after update")
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).ToNot(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					Expect(obj.GetFinalizers()).To(ContainElement("test.io/some-finalizer"), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(DependentDPUDeploymentNameLabel, dpuDeployment.Name), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(HaveKeyWithValue("some", "label"), fmt.Sprintf("%T", obj))
				}
			})
		})
		Context("When checking reconcileDPUSets()", func() {
			var (
				initialDPUSetSettings []dpuservicev1.DPUSet
				expectedDPUSetSpecs   []provisioningv1.DPUSetSpec
				bfb                   *provisioningv1.BFB
				dpuFlavor             *provisioningv1.DPUFlavor
			)
			BeforeEach(func() {
				bfb = getMinimalBFB("somebfb", testNS.Name)
				dpuFlavor = getMinimalDPUFlavor(testNS.Name)
				initialDPUSetSettings = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
						DPUSelector: map[string]string{
							"dpukey1": "dpuvalue1",
						},
						DPUAnnotations: map[string]string{
							"annotationkey1": "annotationvalue1",
						},
					},
					{
						NameSuffix: "dpuset2",
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey2": "nodevalue2",
							},
						},
						DPUSelector: map[string]string{
							"dpukey2": "dpuvalue2",
						},
						DPUAnnotations: map[string]string{
							"annotationkey2": "annotationvalue2",
						},
					},
				}

				expectedDPUSetSpecs = []provisioningv1.DPUSetSpec{
					{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
						DPUSelector: map[string]string{
							"dpukey1": "dpuvalue1",
						},
						Strategy: &provisioningv1.DPUSetStrategy{
							Type: provisioningv1.RollingUpdateStrategyType,
						},
						DPUTemplate: provisioningv1.DPUTemplate{
							Annotations: map[string]string{
								"annotationkey1": "annotationvalue1",
							},
							Spec: provisioningv1.DPUTemplateSpec{
								BFB: provisioningv1.BFBReference{
									Name: "somebfb",
								},
								DPUFlavor: "someflavor",
								NodeEffect: &provisioningv1.NodeEffect{
									Drain: &provisioningv1.Drain{
										AutomaticNodeReboot: true,
									},
								},
								AutomaticNodeReboot: ptr.To(true),
							},
						},
					},
					{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey2": "nodevalue2",
							},
						},
						DPUSelector: map[string]string{
							"dpukey2": "dpuvalue2",
						},
						Strategy: &provisioningv1.DPUSetStrategy{
							Type: provisioningv1.RollingUpdateStrategyType,
						},
						DPUTemplate: provisioningv1.DPUTemplate{
							Annotations: map[string]string{
								"annotationkey2": "annotationvalue2",
							},
							Spec: provisioningv1.DPUTemplateSpec{
								BFB: provisioningv1.BFBReference{
									Name: "somebfb",
								},
								DPUFlavor: "someflavor",
								NodeEffect: &provisioningv1.NodeEffect{
									Drain: &provisioningv1.Drain{
										AutomaticNodeReboot: true,
									},
								},
								AutomaticNodeReboot: ptr.To(true),
							},
						},
					},
				}

				By("Creating the dependencies")
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
			})
			It("should create the correct DPUSets", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that correct DPUSets are created")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						g.Expect(dpuSet.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing DPUSets on update of the .spec.dpus in the DPUDeployment", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("waiting for the initial DPUSets to be applied")
				firstDPUSetUIDs := make([]types.UID, 0, 2)
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
					for _, dpuSet := range gotDPUSetList.Items {
						firstDPUSetUIDs = append(firstDPUSetUIDs, dpuSet.UID)
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				dpuDeployment.Spec.DPUs.DPUSets[1].DPUAnnotations["newkey"] = "newvalue"
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())
				By("checking that correct DPUSets are created")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						// Validate that the same object is updated
						g.Expect(firstDPUSetUIDs).To(ContainElement(dpuSet.UID))

						g.Expect(dpuSet.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					expectedDPUSetSpecs[1].DPUTemplate.Annotations["newkey"] = "newvalue"
					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing DPUSets on update of the referenced BFB", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("waiting for the initial DPUSets to be applied")
				firstDPUSetUIDs := make([]types.UID, 0, 2)
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
					for _, dpuSet := range gotDPUSetList.Items {
						firstDPUSetUIDs = append(firstDPUSetUIDs, dpuSet.UID)
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("creating a new BFB object and checking the outcome")
				bfb2 := getMinimalBFB("somebfb2", testNS.Name)
				Expect(testClient.Create(ctx, bfb2)).To(Succeed())

				By("Updating the DPUDeployment object to reference the new BFB")
				dpuDeployment.Spec.DPUs.BFB = bfb2.Name
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				// Update the expected DPUSetSpecs
				expectedDPUSetSpecs[0].DPUTemplate.Spec.BFB.Name = bfb2.Name
				expectedDPUSetSpecs[1].DPUTemplate.Spec.BFB.Name = bfb2.Name

				By("checking that the DPUSets are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						// Validate that the same object is updated
						g.Expect(firstDPUSetUIDs).To(ContainElement(dpuSet.UID))

						g.Expect(dpuSet.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update existing and create new DPUSets on update of the .spec.dpus in the DPUDeployment", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("waiting for the initial DPUSets to be applied")
				firstDPUSetUIDs := make(map[types.UID]interface{})
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
					for _, dpuSet := range gotDPUSetList.Items {
						firstDPUSetUIDs[dpuSet.UID] = struct{}{}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				dpuDeployment.Spec.DPUs.DPUSets[1].DPUAnnotations["newkey"] = "newvalue"
				dpuDeployment.Spec.DPUs.DPUSets = append(dpuDeployment.Spec.DPUs.DPUSets, dpuservicev1.DPUSet{
					NameSuffix: "dpuset3",
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey3": "nodevalue3",
						},
					},
					DPUSelector: map[string]string{
						"dpukey3": "dpuvalue3",
					},
					DPUAnnotations: map[string]string{
						"annotationkey3": "annotationvalue3",
					},
				})
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())
				By("checking that correct DPUSets are created")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(3))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))

						delete(firstDPUSetUIDs, dpuSet.UID)

						g.Expect(dpuSet.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					// Validate that all original objects are there and not recreated
					g.Expect(firstDPUSetUIDs).To(BeEmpty())

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					expectedDPUSetSpecs[1].DPUTemplate.Annotations["newkey"] = "newvalue"
					expectedDPUSetSpecs = append(expectedDPUSetSpecs, provisioningv1.DPUSetSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey3": "nodevalue3",
							},
						},
						DPUSelector: map[string]string{
							"dpukey3": "dpuvalue3",
						},
						Strategy: &provisioningv1.DPUSetStrategy{
							Type: provisioningv1.RollingUpdateStrategyType,
						},
						DPUTemplate: provisioningv1.DPUTemplate{
							Annotations: map[string]string{
								"annotationkey3": "annotationvalue3",
							},
							Spec: provisioningv1.DPUTemplateSpec{
								BFB: provisioningv1.BFBReference{
									Name: "somebfb",
								},
								DPUFlavor: "someflavor",
								NodeEffect: &provisioningv1.NodeEffect{
									Drain: &provisioningv1.Drain{
										AutomaticNodeReboot: true,
									},
								},
								AutomaticNodeReboot: ptr.To(true),
							},
						},
					})

					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())

			})
			It("should delete DPUSets that are no longer part of the DPUDeployment", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("waiting for the initial DPUSets to be applied")
				firstDPUSetUIDs := make([]types.UID, 0, 2)
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
					for _, dpuSet := range gotDPUSetList.Items {
						firstDPUSetUIDs = append(firstDPUSetUIDs, dpuSet.UID)
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				dpuDeployment.Spec.DPUs.DPUSets = dpuDeployment.Spec.DPUs.DPUSets[1:]
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())
				By("checking that correct DPUSets are created")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						// Validate that the object was not recreated
						g.Expect(firstDPUSetUIDs).To(ContainElement(dpuSet.UID))

						g.Expect(dpuSet.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					expectedDPUSetSpecs = expectedDPUSetSpecs[1:]
					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUSets on manual deletion of the DPUSets", func() {
				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUSets to be applied")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("manually deleting the DPUSets")
				Consistently(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					if len(gotDPUSetList.Items) == 0 {
						return
					}
					objs := []client.Object{}
					for _, dpuSet := range gotDPUSetList.Items {
						objs = append(objs, &dpuSet)
					}
					g.Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("checking that the DPUSets is created")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
		})
		Context("When checking reconcileDPUServiceInterfaces()", func() {
			BeforeEach(func() {
				By("Creating the dependencies")
				bfb := getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
			})
			It("should create the correct DPUServiceInterfaces", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "someotherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that correct DPUServiceInterfaces are created")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(1))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad1",
												InterfaceName: "someinterface",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someotherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad2",
												InterfaceName: "someotherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing DPUServiceInterfaces on update of the DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "someotherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServiceInterface to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad3",
					},
					{
						Name:    "someotherinterface",
						Network: "nad4",
					},
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(1))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad3",
												InterfaceName: "someinterface",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someotherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad4",
												InterfaceName: "someotherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should delete DPUServiceInterfaces that are no longer part of the DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "someotherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServiceInterface to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(1))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad1",
												InterfaceName: "someinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServiceInterfaces on update of the DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServiceInterface to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "someotherinterface",
						Network: "nad2",
					},
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(1))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad1",
												InterfaceName: "someinterface",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "someotherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad2",
												InterfaceName: "someotherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should delete the DPUServiceInterfaces on manual delete of the DPUServiceInterfaces", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServiceInterface to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("manually deleting the DPUServiceInterface")
				Consistently(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					if len(gotDPUServiceInterfaceList.Items) == 0 {
						return
					}
					g.Expect(testutils.CleanupAndWait(ctx, testClient, &gotDPUServiceInterfaceList.Items[0])).To(Succeed())
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("checking that the DPUServiceInterface are created")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
		})
		Context("When checking reconcileDPUServices()", func() {
			BeforeEach(func() {
				By("Creating the dependencies")
				bfb := getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
			})
			It("should create the correct DPUServices", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-1"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-1"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations = make(map[string]string)
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations["annkey1"] = "annval1"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = make(map[string]string)
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels["labelkey1"] = "labelval1"
				dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To[bool](true)
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key1":"value1"}`)}
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "if1", Network: "nad1"}}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-2"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-2"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations = make(map[string]string)
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations["annkey2"] = "annval2"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = make(map[string]string)
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels["labelkey2"] = "labelval2"
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key2":"value2"}`)}
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "if2", Network: "nad2"}, {Name: "if3", Network: "nad3"}}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-3"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-3"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations = make(map[string]string)
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations["annkey3"] = "annval3"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = make(map[string]string)
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels["labelkey3"] = "labelval3"
				dpuServiceConfiguration.Spec.Interfaces = nil
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-1"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-1"
				dpuServiceTemplate.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key1":"someothervalue"}`)}
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-2"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-2"
				dpuServiceTemplate.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key3":"value3"}`)}

				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-3"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-3"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				dpuDeployment.Spec.Services["service-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-2",
					ServiceConfiguration: "service-2",
				}
				dpuDeployment.Spec.Services["service-3"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-3",
					ServiceConfiguration: "service-3",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that correct DPUServices are created")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(3))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(dpuService.Labels).To(HaveLen(1))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
						g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceSpec, 0, len(gotDPUServiceList.Items))
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceSpec{
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
								Values: &runtime.RawExtension{Raw: []byte(`{"key1":"value1"}`)},
							},
							ServiceID: ptr.To[string]("dpudeployment_dpudeployment_service-1"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels:      map[string]string{"labelkey1": "labelval1"},
								Annotations: map[string]string{"annkey1": "annval1"},
							},
							DeployInCluster: ptr.To[bool](true),
							Interfaces:      []string{"dpudeployment-service-1-if1"},
						},
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
								Values: &runtime.RawExtension{Raw: []byte(`{"key2":"value2","key3":"value3"}`)},
							},
							ServiceID: ptr.To[string]("dpudeployment_dpudeployment_service-2"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels:      map[string]string{"labelkey2": "labelval2"},
								Annotations: map[string]string{"annkey2": "annval2"},
							},
							Interfaces: []string{"dpudeployment-service-2-if2", "dpudeployment-service-2-if3"},
						},
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID: ptr.To[string]("dpudeployment_dpudeployment_service-3"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels:      map[string]string{"labelkey3": "labelval3"},
								Annotations: map[string]string{"annkey3": "annval3"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing DPUService on update of the DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				createReconcileDPUServicesDependencies(testNS.Name)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				dpuDeployment.Spec.Services["service-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-2",
					ServiceConfiguration: "service-2",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServices to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
				Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: "service-2"}, dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To[bool](true)
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that the DPUService is updated as expected")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceSpec, 0, len(gotDPUServiceList.Items))
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceSpec{
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID:  ptr.To[string]("dpudeployment_dpudeployment_service-1"),
							Interfaces: []string{"dpudeployment-service-1-someinterface"},
						},
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID:       ptr.To[string]("dpudeployment_dpudeployment_service-2"),
							DeployInCluster: ptr.To[bool](true),
							Interfaces:      []string{"dpudeployment-service-2-someinterface"},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should delete DPUServices that are no longer part of the DPUDeployment", func() {
				By("Creating the dependencies")
				createReconcileDPUServicesDependencies(testNS.Name)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				dpuDeployment.Spec.Services["service-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-2",
					ServiceConfiguration: "service-2",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("waiting for the initial DPUServices to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				delete(dpuDeployment.Spec.Services, "service-1")
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that only one DPUService exists")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

					By("checking the spec")
					g.Expect(gotDPUServiceList.Items[0].Spec).To(BeComparableTo(dpuservicev1.DPUServiceSpec{
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://someurl/repo",
								Path:    "somepath",
								Version: "someversion",
								Chart:   "somechart",
							},
						},
						ServiceID:  ptr.To[string]("dpudeployment_dpudeployment_service-2"),
						Interfaces: []string{"dpudeployment-service-2-someinterface"},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServices on update of the .spec.services in the DPUDeployment", func() {
				By("Creating the dependencies")
				createReconcileDPUServicesDependencies(testNS.Name)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("waiting for the initial DPUService to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				dpuDeployment.Spec.Services["service-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-2",
					ServiceConfiguration: "service-2",
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that two DPUServices exist")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceSpec, 0, len(gotDPUServiceList.Items))
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
					}

					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceSpec{
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID:  ptr.To[string]("dpudeployment_dpudeployment_service-1"),
							Interfaces: []string{"dpudeployment-service-1-someinterface"},
						},
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID:  ptr.To[string]("dpudeployment_dpudeployment_service-2"),
							Interfaces: []string{"dpudeployment-service-2-someinterface"},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServices on manual deletion of the DPUServices", func() {
				By("Creating the dependencies")
				createReconcileDPUServicesDependencies(testNS.Name)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUService to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("manually deleting the DPUServices")
				Consistently(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					if len(gotDPUServiceList.Items) == 0 {
						return
					}
					g.Expect(testutils.CleanupAndWait(ctx, testClient, &gotDPUServiceList.Items[0])).To(Succeed())
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("checking that the DPUServices are created")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
		})
		Context("When checking verifyResourceFitting()", func() {
			DescribeTable("behaves as expected", func(deps *dpuDeploymentDependencies, expectError bool) {
				err := verifyResourceFitting(deps)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
				Entry("DPUFlavor doesn't specify dpuResources", &dpuDeploymentDependencies{
					DPUFlavor: &provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{},
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("3Gi"),
								},
							},
						},
						"service-2": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				}, false),
				Entry("DPUFlavor specifies dpuResources that fit but not systemReservedResources", &dpuDeploymentDependencies{
					DPUFlavor: &provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{
							DPUResources: corev1.ResourceList{
								"cpu":    resource.MustParse("2"),
								"memory": resource.MustParse("4Gi"),
							},
						},
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("3Gi"),
								},
							},
						},
						"service-2": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				}, false),
				Entry("requested resources fit leaving buffer", &dpuDeploymentDependencies{
					DPUFlavor: &provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{
							DPUResources: corev1.ResourceList{
								"cpu":    resource.MustParse("2"),
								"memory": resource.MustParse("4Gi"),
							},
							SystemReservedResources: corev1.ResourceList{
								"cpu":    resource.MustParse("1"),
								"memory": resource.MustParse("2Gi"),
							},
						},
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("0.5"),
									"memory": resource.MustParse("100Mi"),
								},
							},
						},
						"service-2": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("0.2"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				}, false),
				Entry("requested resources fit exactly", &dpuDeploymentDependencies{
					DPUFlavor: &provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{
							DPUResources: corev1.ResourceList{
								"cpu":    resource.MustParse("3"),
								"memory": resource.MustParse("3Gi"),
							},
							SystemReservedResources: corev1.ResourceList{
								"cpu":    resource.MustParse("1"),
								"memory": resource.MustParse("1Gi"),
							},
						},
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
						"service-2": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				}, false),
				Entry("requested resources don't fit", &dpuDeploymentDependencies{
					DPUFlavor: &provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{
							DPUResources: corev1.ResourceList{
								"cpu":    resource.MustParse("1"),
								"memory": resource.MustParse("2Gi"),
							},
							SystemReservedResources: corev1.ResourceList{
								"cpu":    resource.MustParse("0.5"),
								"memory": resource.MustParse("1Gi"),
							},
						},
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("3Gi"),
								},
							},
						},
						"service-2": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				}, true),
				Entry("requested resource doesn't exist", &dpuDeploymentDependencies{
					DPUFlavor: &provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{
							DPUResources: corev1.ResourceList{
								"cpu": resource.MustParse("1"),
							},
							SystemReservedResources: corev1.ResourceList{
								"cpu": resource.MustParse("0.5"),
							},
						},
					},
					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("3Gi"),
								},
							},
						},
						"service-2": {
							Spec: dpuservicev1.DPUServiceTemplateSpec{
								ResourceRequirements: corev1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				}, true),
			)
		})

		Context("When checking reconcileDPUServiceChains()", func() {
			BeforeEach(func() {
				By("Creating the dependencies")
				bfb := getMinimalBFB("somebfb", testNS.Name)
				Expect(testClient.Create(ctx, bfb)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
			})
			It("should create the correct DPUServiceChain", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.ServiceChains = []dpuservicev1.DPUDeploymentSwitch{
					{
						Ports: []dpuservicev1.DPUDeploymentPort{
							{
								Service: &dpuservicev1.DPUDeploymentService{
									InterfaceName: "someinterface",
									Name:          "somedpuservice",
								},
							},
							{
								Service: &dpuservicev1.DPUDeploymentService{
									InterfaceName: "someinterface2",
									Name:          "somedpuservice2",
									IPAM: &dpuservicev1.IPAM{
										MatchLabels: map[string]string{
											"ipamkey1": "ipamvalue1",
										},
									},
								},
							},
						},
					},
					{
						Ports: []dpuservicev1.DPUDeploymentPort{
							{
								Service: &dpuservicev1.DPUDeploymentService{
									InterfaceName: "someotherinterface",
									Name:          "someotherservice",
								},
							},
						},
					},
					{
						Ports: []dpuservicev1.DPUDeploymentPort{
							{
								ServiceInterface: &dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{
										"key": "value",
									},
									IPAM: &dpuservicev1.IPAM{
										MatchLabels: map[string]string{
											"ipamkey2": "ipamvalue2",
										},
									},
								},
							},
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that correct DPUServiceChain is created")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					By("checking the object metadata")
					obj := gotDPUServiceChainList.Items[0]

					g.Expect(obj.Labels).To(HaveLen(1))
					g.Expect(obj.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpudeployment-name", "dpudeployment"))
					g.Expect(obj.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

					By("checking the spec")
					g.Expect(obj.Spec).To(BeComparableTo(dpuservicev1.DPUServiceChainSpec{
						// TODO: Derive and add cluster selector
						Template: dpuservicev1.ServiceChainSetSpecTemplate{
							Spec: dpuservicev1.ServiceChainSetSpec{
								// TODO: Figure out what to do with NodeSelector
								Template: dpuservicev1.ServiceChainSpecTemplate{
									Spec: dpuservicev1.ServiceChainSpec{
										Switches: []dpuservicev1.Switch{
											{
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_somedpuservice",
																ServiceInterfaceInterfaceNameLabel: "someinterface",
															},
														},
													},
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_somedpuservice2",
																ServiceInterfaceInterfaceNameLabel: "someinterface2",
															},
															IPAM: &dpuservicev1.IPAM{
																MatchLabels: map[string]string{
																	"ipamkey1": "ipamvalue1",
																},
															},
														},
													},
												},
											},
											{
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someotherservice",
																ServiceInterfaceInterfaceNameLabel: "someotherinterface",
															},
														},
													},
												},
											},
											{
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																"key": "value",
															},
															IPAM: &dpuservicev1.IPAM{
																MatchLabels: map[string]string{
																	"ipamkey2": "ipamvalue2",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServiceChain on manual deletion of the DPUServiceChain", func() {
				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServiceChain to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("manually deleting the DPUServiceChain")
				Consistently(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					if len(gotDPUServiceChainList.Items) == 0 {
						return
					}
					g.Expect(testutils.CleanupAndWait(ctx, testClient, &gotDPUServiceChainList.Items[0])).To(Succeed())
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("checking that the DPUServiceChain is created")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
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

			By("Creating the informer infrastructure for DPUDeployment")
			i = informer.NewInformer(cfg, dpuservicev1.DPUDeploymentGroupVersionKind, testNS.Name, "dpudeployments")
			DeferCleanup(i.Cleanup)
			go i.Run()

			DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
		})
		It("DPUDeployment has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
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
					HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionResourceFittingReady)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),

				// We have success at the following conditions because there is no object in the cluster to watch on
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUDeployment has all *Reconciled conditions with Success Reason at the end of a successful reconciliation loop but *Ready with Pending reason on underlying object not ready", func() {
			By("Creating the dependencies")
			bfb := getMinimalBFB("somebfb", testNS.Name)
			Expect(testClient.Create(ctx, bfb)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
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
					HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionResourceFittingReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),

				// This one is always true because we don't watch on the DPUSet status yet. See code for reasoning.
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUDeployment has all conditions with Success Reason at the end of a successful reconciliation loop and underlying object ready", func() {
			By("Creating the dependencies")
			bfb := getMinimalBFB("somebfb", testNS.Name)
			Expect(testClient.Create(ctx, bfb)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Updating the status of the underlying dependencies")
			Eventually(func(g Gomega) {
				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).ToNot(BeEmpty())
				for _, dpuService := range gotDPUServiceList.Items {
					dpuService.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					}
					dpuService.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
					dpuService.SetManagedFields(nil)
					g.Expect(testClient.Status().Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).ToNot(BeEmpty())
				for _, dpuServiceChain := range gotDPUServiceChainList.Items {
					dpuServiceChain.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					}
					dpuServiceChain.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
					dpuServiceChain.SetManagedFields(nil)
					g.Expect(testClient.Status().Patch(ctx, &dpuServiceChain, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).ToNot(BeEmpty())
				for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
					dpuServiceInterface.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
						},
					}
					dpuServiceInterface.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
					dpuServiceInterface.SetManagedFields(nil)
					g.Expect(testClient.Status().Patch(ctx, &dpuServiceInterface, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionResourceFittingReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),

				// This one is always true because we don't watch on the DPUSet status yet. See code for reasoning.
				// Also, we don't create one DPUSet with the minimal DPUDeployment.
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUDeployment has condition ResourceFittingReady with Failed Reason when the resources of the underlying DPUServices can't fit the selected DPUs", func() {
			By("Creating the dependencies")
			bfb := getMinimalBFB("somebfb", testNS.Name)
			Expect(testClient.Create(ctx, bfb)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			dpuFlavor.Spec.DPUResources = corev1.ResourceList{"cpu": resource.MustParse("5")}
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			dpuServiceTemplate.Spec.ResourceRequirements = make(corev1.ResourceList)
			dpuServiceTemplate.Spec.ResourceRequirements["cpu"] = resource.MustParse("6")
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionResourceFittingReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonFailure)),
				),
			))
		})
		It("DPUDeployment has condition PrerequisitesReady with Error Reason at the end of first reconciliation loop that failed on dependencies", func() {
			// Add DPUDeployment
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionPreReqsReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUDeployment has condition Deleting with AwaitingDeletion Reason when there are still objects in the cluster", func() {
			By("Creating the dependencies")
			bfb := getMinimalBFB("somebfb", testNS.Name)
			Expect(testClient.Create(ctx, bfb)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
			objs := make(map[client.Object]interface{})

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
					DPUAnnotations: map[string]string{
						"annotationkey1": "annotationvalue1",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Checking that the underlying resources are created and adding fake finalizer")
			DeferCleanup(func() {
				By("Cleaning up the finalizers so that objects can be deleted")
				for obj := range objs {
					Expect(client.IgnoreNotFound(testClient.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
				}
			})
			Eventually(func(g Gomega) {
				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				g.Expect(gotDPUServiceList.Items).ToNot(BeEmpty())
				for _, dpuService := range gotDPUServiceList.Items {
					objs[&dpuService] = struct{}{}
					dpuService.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
					dpuService.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
					dpuService.SetManagedFields(nil)
					g.Expect(testClient.Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).ToNot(BeEmpty())
				for _, dpuServiceChain := range gotDPUServiceChainList.Items {
					objs[&dpuServiceChain] = struct{}{}
					dpuServiceChain.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
					dpuServiceChain.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
					dpuServiceChain.SetManagedFields(nil)
					g.Expect(testClient.Patch(ctx, &dpuServiceChain, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}
				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).ToNot(BeEmpty())
				for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
					objs[&dpuServiceInterface] = struct{}{}
					dpuServiceInterface.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
					dpuServiceInterface.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
					dpuServiceInterface.SetManagedFields(nil)
					g.Expect(testClient.Patch(ctx, &dpuServiceInterface, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					objs[&dpuSet] = struct{}{}
					dpuSet.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					g.Expect(testClient.Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Deleting the DPUDeployment")
			Expect(testClient.Delete(ctx, dpuDeployment)).To(Succeed())

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElements(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
			))

			By("Removing finalizer from all the underlying objects but the DPUSets to check the next status")
			Eventually(func(g Gomega) {
				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
				for _, dpuService := range gotDPUServiceList.Items {
					g.Expect(testClient.Patch(ctx, &dpuService, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))).To(Succeed())
				}

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				for _, dpuServiceChain := range gotDPUServiceChainList.Items {
					g.Expect(testClient.Patch(ctx, &dpuServiceChain, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))).To(Succeed())
				}

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
					g.Expect(testClient.Patch(ctx, &dpuServiceInterface, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))).To(Succeed())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUDeployment{}
				newObj := &dpuservicev1.DPUDeployment{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElements(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServicesReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("are deleted")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("are deleted")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("are deleted")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
			))

			By("Removing finalizer from the DPUSets to ensure deletion")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				for _, dpuSet := range gotDPUSetList.Items {
					g.Expect(testClient.Patch(ctx, &dpuSet, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))).To(Succeed())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})
	})
})

func getMinimalDPUDeployment(namespace string) *dpuservicev1.DPUDeployment {
	return &dpuservicev1.DPUDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpudeployment",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUDeploymentSpec{
			DPUs: dpuservicev1.DPUs{
				BFB:    "somebfb",
				Flavor: "someflavor",
			},
			Services: map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
				"someservice": {
					ServiceTemplate:      "sometemplate",
					ServiceConfiguration: "someconfiguration",
				},
			},
			ServiceChains: []dpuservicev1.DPUDeploymentSwitch{
				{
					Ports: []dpuservicev1.DPUDeploymentPort{
						{
							Service: &dpuservicev1.DPUDeploymentService{
								InterfaceName: "someinterface",
								Name:          "someservice",
							},
						},
					},
				},
			},
		},
	}
}

func getMinimalBFB(name, namespace string) *provisioningv1.BFB {
	return &provisioningv1.BFB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: provisioningv1.BFBSpec{
			URL: fmt.Sprintf("http://somewebserver/%s.bfb", name),
		},
	}
}

func getMinimalDPUFlavor(namespace string) *provisioningv1.DPUFlavor {
	return &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someflavor",
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUFlavorSpec{
			// TODO: Remove those required fields from DPUFlavor
			Grub: provisioningv1.DPUFlavorGrub{
				KernelParameters: []string{},
			},
			Sysctl: provisioningv1.DPUFLavorSysctl{
				Parameters: []string{},
			},
		},
	}
}

func getMinimalDPUServiceTemplate(namespace string) *dpuservicev1.DPUServiceTemplate {
	return &dpuservicev1.DPUServiceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sometemplate",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceTemplateSpec{
			DeploymentServiceName: "someservice",
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					RepoURL: "oci://someurl/repo",
					Path:    "somepath",
					Version: "someversion",
					Chart:   "somechart",
				},
			},
		},
	}
}

func getMinimalDPUServiceConfiguration(namespace string) *dpuservicev1.DPUServiceConfiguration {
	return &dpuservicev1.DPUServiceConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someconfiguration",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceConfigurationSpec{
			DeploymentServiceName: "someservice",
			Interfaces: []dpuservicev1.ServiceInterfaceTemplate{
				{
					Name:    "someinterface",
					Network: "somenad",
				},
			},
		},
	}
}

// cleanDPUDeploymentDerivatives removes all the objects that a DPUDeployment creates in a particular namespace
func cleanDPUDeploymentDerivatives(namespace string) {
	By("Ensuring DPUSets, DPUServiceChains, DPUServiceInterfaces and DPUServices are deleted")
	dpuSetList := &provisioningv1.DPUSetList{}
	Expect(testClient.List(ctx, dpuSetList, client.InNamespace(namespace))).To(Succeed())
	objs := []client.Object{}
	for i := range dpuSetList.Items {
		objs = append(objs, &dpuSetList.Items[i])
	}
	dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
	Expect(testClient.List(ctx, dpuServiceChainList, client.InNamespace(namespace))).To(Succeed())
	for i := range dpuServiceChainList.Items {
		objs = append(objs, &dpuServiceChainList.Items[i])
	}
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	Expect(testClient.List(ctx, dpuServiceInterfaceList, client.InNamespace(namespace))).To(Succeed())
	for i := range dpuServiceInterfaceList.Items {
		objs = append(objs, &dpuServiceInterfaceList.Items[i])
	}
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	Expect(testClient.List(ctx, dpuServiceList, client.InNamespace(namespace))).To(Succeed())
	for i := range dpuServiceList.Items {
		objs = append(objs, &dpuServiceList.Items[i])
	}

	Eventually(func(g Gomega) {
		g.Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

// createReconcileDPUServicesDependencies creates 2 sets of dependencies that are used for the majority of the
// reconcileDPUSets tests
func createReconcileDPUServicesDependencies(namespace string) {
	dpuServiceConfiguration := getMinimalDPUServiceConfiguration(namespace)
	dpuServiceConfiguration.Name = "service-1"
	dpuServiceConfiguration.Spec.DeploymentServiceName = "service-1"
	Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
	DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

	dpuServiceConfiguration = getMinimalDPUServiceConfiguration(namespace)
	dpuServiceConfiguration.Name = "service-2"
	dpuServiceConfiguration.Spec.DeploymentServiceName = "service-2"
	Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
	DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

	dpuServiceTemplate := getMinimalDPUServiceTemplate(namespace)
	dpuServiceTemplate.Name = "service-1"
	dpuServiceTemplate.Spec.DeploymentServiceName = "service-1"
	Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
	DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

	dpuServiceTemplate = getMinimalDPUServiceTemplate(namespace)
	dpuServiceTemplate.Name = "service-2"
	dpuServiceTemplate.Spec.DeploymentServiceName = "service-2"
	Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
	DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
}
