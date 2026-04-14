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
	"slices"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//nolint:goconst
var _ = Describe("DPUDeployment Controller", func() {
	defaultPauseDPUServiceReconciler := pauseDPUServiceReconciler.Load()
	defaultPauseDPUServiceTemplateReconciler := pauseDPUServiceTemplateReconciler.Load()
	defaultPauseDPUDeploymentNodeReconciler := pauseDPUDeploymentNodeReconciler.Load()
	defaultDPUDeploymentReconcileDeleteRequeueDuration := reconcileRequeueDuration.Load()
	BeforeEach(func() {
		DeferCleanup(func() {
			pauseDPUServiceReconciler.Store(defaultPauseDPUServiceReconciler)
			pauseDPUServiceTemplateReconciler.Store(defaultPauseDPUServiceTemplateReconciler)
			pauseDPUDeploymentNodeReconciler.Store(defaultPauseDPUDeploymentNodeReconciler)
			reconcileRequeueDuration.Store(defaultDPUDeploymentReconcileDeleteRequeueDuration)
		})

		// These are modified to speed up the testing suite and also simplify the deletion logic
		pauseDPUServiceReconciler.Store(true)
		pauseDPUServiceTemplateReconciler.Store(true)
		pauseDPUDeploymentNodeReconciler.Store(true)
		reconcileRequeueDuration.Store(int64(1 * time.Second))
	})
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpudeployment-testns-"}}
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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(BeEmpty())

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(BeEmpty())
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
		It("should create child objects when using the maximum field length", func() {
			namespaceNameMaxLength := utilrand.String(len("dpf-operator-system")) // 19 chars
			dpuDeploymentNameMaxLength := utilrand.String(20)
			deploymentServiceNameMaxLength := utilrand.String(28)
			serviceInterfaceMaxLength := utilrand.String(15)
			dpuSetNameSuffixMaxLength := utilrand.String(24)

			By("Creating the namespace with fixed name length")
			testNSMaxLength := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceNameMaxLength}}
			Expect(testClient.Create(ctx, testNSMaxLength)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNSMaxLength)

			By("Creating the dependencies")
			bfb := createMinimalBFBWithStatus("somebfb", testNSMaxLength.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNSMaxLength.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNSMaxLength.Name)
			dpuServiceConfiguration.Spec.DeploymentServiceName = deploymentServiceNameMaxLength
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: serviceInterfaceMaxLength, Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := getMinimalDPUServiceTemplate(testNSMaxLength.Name)
			dpuServiceTemplate.Spec.DeploymentServiceName = deploymentServiceNameMaxLength
			Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
			patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			DeferCleanup(cleanDPUDeploymentDerivatives, testNSMaxLength.Name)

			By("creating the dpudeployment")
			dpuDeployment := getMinimalDPUDeployment(testNSMaxLength.Name)
			dpuDeployment.Name = dpuDeploymentNameMaxLength
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: dpuSetNameSuffixMaxLength,
				},
			}
			dpuDeployment.Spec.Services[deploymentServiceNameMaxLength] = dpuDeployment.Spec.Services["someservice"]
			delete(dpuDeployment.Spec.Services, "someservice")
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
				Switches: []dpuservicev1.DPUDeploymentSwitch{
					{
						Ports: []dpuservicev1.DPUDeploymentPort{
							{
								Service: &dpuservicev1.DPUDeploymentService{
									InterfaceName: serviceInterfaceMaxLength,
									Name:          serviceInterfaceMaxLength,
								},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("checking that objects are created")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(HaveLen(1))

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNSMaxLength.Name,
				})).To(Succeed())
				g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

				gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
				g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
		It("should cleanup child objects on delete", func() {
			By("Creating the dependencies")
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)

			By("creating the dpudeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
				UpgradePolicy: dpuservicev1.UpgradePolicy{
					ApplyNodeEffect: ptr.To(false),
				},
				Switches: []dpuservicev1.DPUDeploymentSwitch{
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
			}
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey1": "dpuvalue1",
						},
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
					g.Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("checking that objects are created")
			Eventually(func(g Gomega) {
				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).To(HaveLen(1))

				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
					g.Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
		It("should not delete the DPUSets until the rest of the child objects are deleted", func() {
			By("Creating the dependencies")
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)

			By("creating the dpudeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
				UpgradePolicy: dpuservicev1.UpgradePolicy{
					ApplyNodeEffect: ptr.To(false),
				},
				Switches: []dpuservicev1.DPUDeploymentSwitch{
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
			}
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey1": "dpuvalue1",
						},
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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
			Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
				Namespace: testNS.Name,
			})).To(Succeed())

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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
			Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
				Namespace: testNS.Name,
			})).To(Succeed())

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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
	Context("When reconciling multiple resources", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		It("should cleanup child objects on delete", func() {
			By("Creating the dependencies")
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)

			By("creating the dpudeployments")
			dpusets := []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey1": "dpuvalue1",
						},
					},
					DPUAnnotations: map[string]string{
						"annotationkey1": "annotationvalue1",
					},
				},
			}

			dpuDeployment1 := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment1.Name = "dpudeployment1"
			dpuDeployment1.Spec.DPUs.DPUSets = dpusets
			Expect(testClient.Create(ctx, dpuDeployment1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment1)

			dpuDeployment2 := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment2.Name = "dpudeployment2"
			dpuDeployment2.Spec.DPUs.DPUSets = dpusets
			Expect(testClient.Create(ctx, dpuDeployment2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment1)

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
					g.Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment1.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment2.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Deleting all the dependencies")
			Expect(testClient.Delete(ctx, bfb)).To(Succeed())
			Expect(testClient.Delete(ctx, dpuFlavor)).To(Succeed())
			Expect(testClient.Delete(ctx, dpuServiceConfiguration)).To(Succeed())
			Expect(testClient.Delete(ctx, dpuServiceTemplate)).To(Succeed())

			By("Deleting the first dpudeployment")
			Expect(testClient.Delete(ctx, dpuDeployment1)).To(Succeed())

			By("checking that dependencies are marked only for the second dpudeployment and finalizer still in place")
			Eventually(func(g Gomega) {
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					g.Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetFinalizers()).To(ContainElement(dpuservicev1.DPUDeploymentFinalizer), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment1.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
					g.Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment2.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("checking that the dependencies still exist")
			Consistently(func(g Gomega) {
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					g.Expect(testClient.Get(ctx, key, obj)).To(Succeed(), fmt.Sprintf("%T", obj))
				}
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Deleting the second dpudeployment and verify that this is the time the finalizer is removed")
			Expect(testClient.Delete(ctx, dpuDeployment2)).To(Succeed())

			By("checking that the dependencies are removed")
			Eventually(func(g Gomega) {
				for obj, key := range map[client.Object]client.ObjectKey{
					&provisioningv1.BFB{}:                   client.ObjectKeyFromObject(bfb),
					&provisioningv1.DPUFlavor{}:             client.ObjectKeyFromObject(dpuFlavor),
					&dpuservicev1.DPUServiceConfiguration{}: client.ObjectKeyFromObject(dpuServiceConfiguration),
					&dpuservicev1.DPUServiceTemplate{}:      client.ObjectKeyFromObject(dpuServiceTemplate),
				} {
					g.Expect(apierrors.IsNotFound(testClient.Get(ctx, key, obj))).To(BeTrue(), fmt.Sprintf("%T", obj))
				}
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
		Context("DPUService name matching", func() {
			DescribeTable("Validate dpuService name matching", func(dpuServiceName, TargetName string, expected bool) {
				Expect(matchDPUServiceName(dpuServiceName, TargetName)).To(Equal(expected))
			},
				Entry("matching name with suffix", fmt.Sprintf("someservice-%s", utilrand.String(resourceNameGeneratedSuffixLength)), "someservice", true),
				Entry("matching complex name", fmt.Sprintf("someservice-foo-%s", utilrand.String(resourceNameGeneratedSuffixLength)), "someservice-foo", true),
				Entry("matching legacy name", "somedpudeployment-someservice", "somedpudeployment-someservice", true),
				Entry("non matching names", "someotherservice", "someservice", false),
				Entry("non matching name with same prefix", "someservice-otherservice", "someservice", false),
				Entry("non matching name with same prefix and suffix", fmt.Sprintf("someservice-otherservice-%s", utilrand.String(resourceNameGeneratedSuffixLength)), "someservice", false),
				Entry("non matching name with different prefix and suffix", fmt.Sprintf("someservice-%s-%s", utilrand.String(resourceNameGeneratedSuffixLength), utilrand.String(resourceNameGeneratedSuffixLength)), "someservice", false),
				Entry("non matching legacy name", "somedpudeployment-someotherservice", "somedpudeployment-someservice", false),
			)
		})
		Context("DPUDeployment Topological Sort", func() {
			DescribeTable("Validate DPUDeployment topological Sort", func(dpuDeployment *dpuservicev1.DPUDeployment, expected []string, expectedErr bool) {
				res, err := servicesTopologicalSort(dpuDeployment)
				if expectedErr {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(Equal(expected))
			},
				Entry("dpuDeployment with no dependencies", getMinimalDPUDeployment("my-ns"), []string{"someservice"}, false),
				Entry("dpuDeployment with one dependency", func() *dpuservicev1.DPUDeployment {
					dpuDeployment := getMinimalDPUDeployment("my-ns")
					dpuDeployment.Spec.Services["some-otherservice"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
						},
					}
					return dpuDeployment
				}(), []string{"someservice", "some-otherservice"}, false),
				Entry("dpuDeployment with multiple dependencies", func() *dpuservicev1.DPUDeployment {
					dpuDeployment := getMinimalDPUDeployment("my-ns")
					dpuDeployment.Spec.Services["some-otherservice"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
							{
								Name: "someservice2",
							},
						},
					}
					dpuDeployment.Spec.Services["someservice2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
						},
					}
					dpuDeployment.Spec.Services["someservice3"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice2",
							},
							{
								Name: "some-otherservice",
							},
						},
					}
					return dpuDeployment
				}(), []string{"someservice", "someservice2", "some-otherservice", "someservice3"}, false),
				Entry("dpuDeployment with circular dependency", func() *dpuservicev1.DPUDeployment {
					dpuDeployment := getMinimalDPUDeployment("my-ns")
					dpuDeployment.Spec.Services["some-otherservice"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
							{
								Name: "someservice2",
							},
						},
					}
					dpuDeployment.Spec.Services["someservice2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
							{
								Name: "some-otherservice",
							},
						},
					}
					dpuDeployment.Spec.Services["someservice3"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice2",
							},
						},
					}
					return dpuDeployment
				}(), nil, true),
				Entry("dpuDeployment with non-existing dependency", func() *dpuservicev1.DPUDeployment {
					dpuDeployment := getMinimalDPUDeployment("my-ns")
					dpuDeployment.Spec.Services["some-otherservice"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
							{
								Name: "someservice2",
							},
						},
					}
					dpuDeployment.Spec.Services["someservice2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice",
							},
							{
								Name: "some-otherservice",
							},
						},
					}
					dpuDeployment.Spec.Services["someservice3"] = dpuservicev1.DPUDeploymentServiceConfiguration{
						DependsOn: []dpuservicev1.LocalObjectDependency{
							{
								Name: "someservice2",
							},
							{
								Name: "some-otherservice",
							},
							{
								Name: "non-existing-service",
							},
						},
					}
					return dpuDeployment
				}(), nil, true),
			)
		})
		Context("Get Aggregated DPUClusterSelector", func() {
			DescribeTable("Validate",
				func(dpuSets []dpuservicev1.DPUSet, expectedSelector *metav1.LabelSelector) {
					dpuDeployment := &dpuservicev1.DPUDeployment{
						Spec: dpuservicev1.DPUDeploymentSpec{
							DPUs: dpuservicev1.DPUs{
								DPUSets: dpuSets,
							},
						},
					}

					result := getAggregatedDPUClusterSelector(dpuDeployment)

					if expectedSelector == nil {
						Expect(result).To(BeNil())
						return
					}

					Expect(result).ToNot(BeNil())
					Expect(result).To(BeComparableTo(expectedSelector))
				},
				Entry("nil DPUSets", nil, nil),
				Entry("empty DPUSets", []dpuservicev1.DPUSet{}, nil),
				Entry("any DPUSet has nil selector",
					[]dpuservicev1.DPUSet{
						{
							NameSuffix: "set1",
							DPUClusterSelector: map[string]string{
								"region": "us-west",
							},
						},
						{
							NameSuffix:         "set2",
							DPUClusterSelector: nil,
						},
					},
					nil,
				),
				Entry("any DPUSet has empty selector",
					[]dpuservicev1.DPUSet{
						{
							NameSuffix: "set1",
							DPUClusterSelector: map[string]string{
								"region": "us-west",
							},
						},
						{
							NameSuffix:         "set2",
							DPUClusterSelector: map[string]string{},
						},
					},
					nil,
				),
				Entry("single DPUSet with multiple labels",
					[]dpuservicev1.DPUSet{
						{
							NameSuffix: "set1",
							DPUClusterSelector: map[string]string{
								"region": "us-west",
								"env":    "prod",
							},
						},
					},
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"prod"},
							},
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-west"},
							},
						},
					},
				),
				Entry("aggregate values for same key across multiple DPUSets",
					[]dpuservicev1.DPUSet{
						{
							NameSuffix: "set1",
							DPUClusterSelector: map[string]string{
								"region": "us-west",
							},
						},
						{
							NameSuffix: "set2",
							DPUClusterSelector: map[string]string{
								"region": "us-east",
							},
						},
						{
							NameSuffix: "set3",
							DPUClusterSelector: map[string]string{
								"region": "eu-west",
							},
						},
					},
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"eu-west", "us-east", "us-west"},
							},
						},
					},
				),
				Entry("deduplicate values for same key",
					[]dpuservicev1.DPUSet{
						{
							NameSuffix: "set1",
							DPUClusterSelector: map[string]string{
								"region": "us-west",
								"env":    "prod",
							},
						},
						{
							NameSuffix: "set2",
							DPUClusterSelector: map[string]string{
								"region": "us-west", // Duplicate
								"env":    "dev",
							},
						},
					},
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"dev", "prod"},
							},
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-west"},
							},
						},
					},
				),
				// TODO: This is problematic as it will create objects in clusters where we should potentially don't create, consider
				// a different implementation.
				Entry("create superset with different key combinations",
					[]dpuservicev1.DPUSet{
						{
							NameSuffix: "set1",
							DPUClusterSelector: map[string]string{
								"region": "us-west",
								"env":    "prod",
							},
						},
						{
							NameSuffix: "set2",
							DPUClusterSelector: map[string]string{
								"region": "us-east",
								"env":    "dev",
							},
						},
						{
							NameSuffix: "set3",
							DPUClusterSelector: map[string]string{
								"region": "eu-west",
								"tier":   "frontend",
							},
						},
					},
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"dev", "prod"},
							},
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"eu-west", "us-east", "us-west"},
							},
							{
								Key:      "tier",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"frontend"},
							},
						},
					},
				),
			)
			It("produces idempotent results", func() {
				dpuDeployment := &dpuservicev1.DPUDeployment{
					Spec: dpuservicev1.DPUDeploymentSpec{
						DPUs: dpuservicev1.DPUs{
							DPUSets: []dpuservicev1.DPUSet{
								{
									NameSuffix: "set1",
									DPUClusterSelector: map[string]string{
										"region": "us-west",
										"env":    "prod",
										"tier":   "frontend",
									},
								},
								{
									NameSuffix: "set2",
									DPUClusterSelector: map[string]string{
										"region": "us-east",
										"env":    "dev",
									},
								},
								{
									NameSuffix: "set3",
									DPUClusterSelector: map[string]string{
										"region": "eu-west",
										"tier":   "backend",
									},
								},
							},
						},
					},
				}

				results := make([]*metav1.LabelSelector, 10)
				for i := 0; i < 10; i++ {
					results[i] = getAggregatedDPUClusterSelector(dpuDeployment)
				}

				for i := 1; i < len(results); i++ {
					// Equal is used instead of BeComparableTo to avoid the issue of the order of the match expressions.
					Expect(results[i]).To(Equal(results[0]), "all iterations should produce the same result")
				}
			})
		})
		Context("When checking getDependencies()", func() {
			It("should return the correct object", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				bfb.SetGroupVersionKind(schema.EmptyObjectKind.GroupVersionKind())
				dpuServiceTemplate.SetGroupVersionKind(schema.EmptyObjectKind.GroupVersionKind())

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
			It("should error if bfb is not ready", func() {
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

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Checking the output of the function")
				_, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).To(HaveOccurred())
			})
			It("should error if a DPUServiceConfiguration doesn't match DPUDeployment service", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.DeploymentServiceName = "wrong-service"
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Checking the output of the function")
				_, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).To(HaveOccurred())
			})
			It("should error if a DPUServiceTemplate doesn't match DPUDeployment service", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
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
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

				By("Checking the output of the function")
				_, err := getDependencies(ctx, testClient, dpuDeployment)
				Expect(err).To(HaveOccurred())
			})
			It("should error if a DPUServiceTemplate is not ready", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				By("Creating the dependencies")
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
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
				bfb = createMinimalBFBWithStatus("somebfb", testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				extraBFB = createMinimalBFBWithStatus("extra-bfb", testNS.Name)
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

				dpuServiceTemplate = createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				extraDPUServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				extraDPUServiceTemplate.Name = "extra-dpuservicetemplate"
				Expect(testClient.Create(ctx, extraDPUServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, extraDPUServiceTemplate)
				patchDPUServiceTemplateWithStatus(extraDPUServiceTemplate)

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
					Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
					Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
					Expect(obj.GetLabels()).To(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
						HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue),
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
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
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
						HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue),
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
					Expect(obj.GetLabels()).ToNot(HaveKeyWithValue(dpuDeployment.GetDependentLabelKey(), dpuservicev1.DependentDPUDeploymentLabelValue), fmt.Sprintf("%T", obj))
					Expect(obj.GetLabels()).To(HaveKeyWithValue("some", "label"), fmt.Sprintf("%T", obj))
				}
			})
		})
		Context("When checking reconcileDPUSets()", func() {
			var (
				initialDPUSetSettings        []dpuservicev1.DPUSet
				expectedDPUSetSpecs          []provisioningv1.DPUSetSpec
				initialServiceChainsSettings *dpuservicev1.ServiceChains
				bfb                          *provisioningv1.BFB
				dpuFlavor                    *provisioningv1.DPUFlavor
			)
			BeforeEach(func() {
				dpuFlavor = getMinimalDPUFlavor(testNS.Name)
				initialDPUSetSettings = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
						DPUDeviceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"dpukey1": "dpuvalue1",
							},
						},
						DPUAnnotations: map[string]string{
							"annotationkey1": "annotationvalue1",
						},
					},
					{
						NameSuffix: "dpuset2",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey2": "nodevalue2",
							},
						},
						DPUDeviceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"dpukey2": "dpuvalue2",
							},
						},
						DPUAnnotations: map[string]string{
							"annotationkey2": "annotationvalue2",
						},
						DPUClusterSelector: map[string]string{
							"clusterkey1": "clustervalue1",
						},
					},
				}

				expectedDPUSetSpecs = []provisioningv1.DPUSetSpec{
					{
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
						DPUDeviceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"dpukey1": "dpuvalue1",
							},
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
									Action: provisioningv1.Action{
										Drain: ptr.To(true),
										Force: ptr.To(false),
									},
									UpgradePolicy: provisioningv1.UpgradePolicy{
										ApplyOnLabelChange: ptr.To(false),
									},
								},
							},
						},
					},
					{
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey2": "nodevalue2",
							},
						},
						DPUDeviceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"dpukey2": "dpuvalue2",
							},
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
									Action: provisioningv1.Action{
										Drain: ptr.To(true),
										Force: ptr.To(false),
									},
									UpgradePolicy: provisioningv1.UpgradePolicy{
										ApplyOnLabelChange: ptr.To(false),
									},
								},
								Cluster: &provisioningv1.ClusterSpec{
									Selector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"clusterkey1": "clustervalue1",
										},
									},
								},
							},
						},
					},
				}
				initialServiceChainsSettings = &dpuservicev1.ServiceChains{
					UpgradePolicy: dpuservicev1.UpgradePolicy{
						ApplyNodeEffect: ptr.To(false),
					},
					Switches: []dpuservicev1.DPUDeploymentSwitch{
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
				}

				By("Creating the dependencies")
				bfb = createMinimalBFBWithStatus("somebfb", testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{ApplyNodeEffect: ptr.To(false)}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
			})
			It("should create the correct DPUSets", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.DPUs.SecureBoot = ptr.To(true)
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the DPUServiceChain and DPUService")
				var dpuServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					dpuServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(dpuServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var dpuService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					dpuService = &dpuServiceList.Items[0]
					g.Expect(dpuService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservicechain-version":        dpuServiceChain.Name,
						"svc.dpu.nvidia.com/dpuservice-someservice-version": dpuService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuService.Name),
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.SecureBoot = ptr.To(true)
				}

				By("checking that correct DPUSets are created")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("retrieving the DPUServiceChain and DPUService")
				var dpuServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					dpuServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(dpuServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservicechain-version":        dpuServiceChain.Name,
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

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
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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
			It("should update the DPUSets on setting and unsetting the .spec.dpus.nodeEffect in the DPUDeployment", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("retrieving the DPUServiceChain and DPUService")
				var dpuServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					dpuServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(dpuServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservicechain-version":        dpuServiceChain.Name,
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

				By("waiting for the initial DPUSets to be applied")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object to use a different nodeEffect and checking the outcome")
				dpuDeployment.Spec.DPUs.NodeEffect = &provisioningv1.Action{
					NoEffect: ptr.To(true),
					Force:    ptr.To(false),
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that DPUSets are updated")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					expectedDPUSetSpecs[0].DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							NoEffect: ptr.To(true),
							Force:    ptr.To(false),
						},
						UpgradePolicy: provisioningv1.UpgradePolicy{
							ApplyOnLabelChange: ptr.To(false),
							NodeMaintenanceAdditionalRequestors: []string{
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
							},
						}}
					expectedDPUSetSpecs[1].DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							NoEffect: ptr.To(true),
							Force:    ptr.To(false),
						},
						UpgradePolicy: provisioningv1.UpgradePolicy{
							ApplyOnLabelChange: ptr.To(false),
							NodeMaintenanceAdditionalRequestors: []string{
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
							},
						}}
					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object with no nodeEffect and checking the outcome")
				dpuDeployment.Spec.DPUs.NodeEffect = nil
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that DPUSets are updated")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the specs")
					specs := make([]provisioningv1.DPUSetSpec, 0, len(gotDPUSetList.Items))
					for _, dpuSet := range gotDPUSetList.Items {
						specs = append(specs, dpuSet.Spec)
					}
					expectedDPUSetSpecs[0].DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							Drain: ptr.To(true),
							Force: ptr.To(false),
						},
						UpgradePolicy: provisioningv1.UpgradePolicy{
							ApplyOnLabelChange: ptr.To(false),
							NodeMaintenanceAdditionalRequestors: []string{
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
							},
						},
					}
					expectedDPUSetSpecs[1].DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							Drain: ptr.To(true),
							Force: ptr.To(false),
						},
						UpgradePolicy: provisioningv1.UpgradePolicy{
							ApplyOnLabelChange: ptr.To(false),
							NodeMaintenanceAdditionalRequestors: []string{
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), dpuServiceChain.Name),
								fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
							},
						},
					}
					g.Expect(specs).To(ConsistOf(expectedDPUSetSpecs))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should keep the existing DPUSets labels on update of a DPUServiceConfiguration", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the DPUServiceChain and DPUService")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					gotDPUServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(gotDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

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

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
				Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: "someconfiguration"}, dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
				Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: "sometemplate"}, dpuServiceTemplate)).To(Succeed())

				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				By("checking that the DPUServices are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUServices := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
					g.Expect(gotDPUServices.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServices.Items {
						g.Expect(dpuService.Labels).To(HaveLen(2))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest2))
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the DPUSets are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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
			It("should update the existing DPUSets labels on update of a disruptive DPUServiceConfiguration", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the DPUServiceChain and DPUService")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					gotDPUServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(gotDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotInitialDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotInitialDPUService = &dpuServiceList.Items[0]
					g.Expect(gotInitialDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotInitialDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotInitialDPUService.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
					}
				}

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

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
				Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: "someconfiguration"}, dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
				// make it disruptive
				dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{
					ApplyNodeEffect: ptr.To(true),
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
				Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: "sometemplate"}, dpuServiceTemplate)).To(Succeed())

				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				var gotDPUService *dpuservicev1.DPUService
				By("checking that the DPUServices are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUServices := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
					g.Expect(gotDPUServices.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServices.Items {
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						if dpuService.Annotations["svc.dpu.nvidia.com/dpuservice-version"] == versionDigest2 {
							gotDPUService = &dpuService
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("Adding a finalizer to the DPUServices to simulate slow deletion")
				gotDPUServicesList := &dpuservicev1.DPUServiceList{}
				Expect(testClient.List(ctx, gotDPUServicesList)).To(Succeed())
				Expect(gotDPUServicesList.Items).To(HaveLen(2))
				DeferCleanup(func() {
					By("Cleaning up the finalizers so that objects can be deleted")
					for _, dpuService := range gotDPUServicesList.Items {
						Expect(client.IgnoreNotFound(testClient.Patch(ctx, &dpuService, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
					}
				})

				for _, dpuService := range gotDPUServicesList.Items {
					finalizers := dpuService.GetFinalizers()
					finalizers = append(finalizers, "test.io/some-finalizer")
					dpuService.SetFinalizers(finalizers)
					dpuService.GetObjectKind().SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
					dpuService.SetManagedFields(nil)
					Expect(testClient.Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				Expect(gotDPUService).ToNot(Equal(gotInitialDPUService))
				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}

					// ApplyOnLabelChange should be true because we are modifying a disruptive DPUServiceConfiguration
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
				}

				By("checking that the DPUSets are correctly updated")
				dpuSetGenerationAfterModification := make(map[string]int64, 2)
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						dpuSetGenerationAfterModification[dpuSet.Name] = dpuSet.Generation
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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

				By("triggering additional DPUDeployment reconciliations and ensuring the DPUSet generation doesn't change and ApplyOnLabelChange is still true")
				Consistently(func(g Gomega) {
					updatedDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), updatedDPUDeployment)).To(Succeed())
					g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedDPUDeployment)).To(Succeed())

					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					for _, dpuSet := range gotDPUSetList.Items {
						// Validate that the generation hasn't changed
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name]))
						// Validate that ApplyOnLabelChange is still true
						g.Expect(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
					}
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("marking the DPUService ready")
				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
				Expect(gotDPUServiceList.Items).To(HaveLen(2))
				for _, dpuService := range gotDPUServiceList.Items {
					if dpuService.Annotations["svc.dpu.nvidia.com/dpuservice-version"] == versionDigest2 {
						dpuService.Status.Conditions = []metav1.Condition{
							{
								Type:               string(conditions.TypeReady),
								Status:             metav1.ConditionTrue,
								Reason:             string(conditions.ReasonSuccess),
								LastTransitionTime: metav1.NewTime(time.Now()),
								ObservedGeneration: dpuService.Generation,
							},
						}
						dpuService.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
						dpuService.SetManagedFields(nil)
						Expect(testClient.Status().Patch(ctx, &dpuService, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
					}
				}

				By("marking the DPUSet ready")
				gotDPUSetList := &provisioningv1.DPUSetList{}
				Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					dpuSet.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuSet.Generation,
						},
					}
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					Expect(testClient.Status().Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("triggering additional DPUDeployment reconciliations and ensuring the DPUSet generation doesn't change and ApplyOnLabelChange is still true")
				Consistently(func(g Gomega) {
					updatedDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), updatedDPUDeployment)).To(Succeed())
					g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedDPUDeployment)).To(Succeed())

					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					for _, dpuSet := range gotDPUSetList.Items {
						// Validate that the generation hasn't changed
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name]))
						// Validate that ApplyOnLabelChange is still true
						g.Expect(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
					}
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("Removing the fake finalizers")
				gotDPUServicesList = &dpuservicev1.DPUServiceList{}
				Expect(testClient.List(ctx, gotDPUServicesList)).To(Succeed())
				Expect(gotDPUServicesList.Items).To(HaveLen(2))
				for _, dpuService := range gotDPUServicesList.Items {
					Expect(client.IgnoreNotFound(testClient.Patch(ctx, &dpuService, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
				}

				By("checking that the DPUServices are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUServices := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
					g.Expect(gotDPUServices.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServices.Items {
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest2))
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the DPUSets are correctly updated")
				for i := range expectedDPUSetSpecs {
					// The old DPUService has been deleted and the upgrade is complete, so the ApplyOnLabelChange should
					// be false
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(false)
				}
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name] + 1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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

				By("triggering additional DPUDeployment reconciliations and ensuring the DPUSet generation doesn't change and ApplyOnLabelChange is still false")
				Consistently(func(g Gomega) {
					updatedDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), updatedDPUDeployment)).To(Succeed())
					g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedDPUDeployment)).To(Succeed())

					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					for _, dpuSet := range gotDPUSetList.Items {
						// Validate that the generation hasn't changed
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name] + 1))
						// Validate that ApplyOnLabelChange is still false
						g.Expect(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
					}
				}).WithTimeout(5 * time.Second).Should(Succeed())

			})
			It("should update the existing DPUSets labels on update of a disruptive DPUServiceChain", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("retrieving the DPUServiceChain and DPUService")
				var gotInitialDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					gotInitialDPUServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(gotInitialDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotInitialDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotInitialDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

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

				By("modifying the dpudeployment service chain and checking the outcome")
				dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
					// make the chain disruptive
					UpgradePolicy: dpuservicev1.UpgradePolicy{
						ApplyNodeEffect: ptr.To(true),
					},
					Switches: []dpuservicev1.DPUDeploymentSwitch{
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
										InterfaceName: "otherinterface",
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
					},
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				// We need to get the object to calculate the digest taking into account the defaults added by the API server
				gotDPUDeployment := &dpuservicev1.DPUDeployment{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), gotDPUDeployment)).To(Succeed())
				chainDigest2 := calculateDPUServiceChainVersionDigest(gotDPUDeployment.Spec.ServiceChains.Switches)
				Expect(chainDigest2).NotTo(Equal(chainDigest))

				By("checking that the DPUServiceChain is correctly updated")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					gotDPUServiceChains := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChains)).To(Succeed())
					g.Expect(gotDPUServiceChains.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuServiceChain := range gotDPUServiceChains.Items {
						g.Expect(dpuServiceChain.Labels).To(HaveLen(1))
						g.Expect(dpuServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						if dpuServiceChain.Annotations["svc.dpu.nvidia.com/dpuservicechain-version"] == chainDigest2 {
							gotDPUServiceChain = &dpuServiceChain
						}
					}
					g.Expect(gotDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("Adding a finalizer to the old DPUServiceChain to simulate slow deletion")
				gotDPUServiceChains := &dpuservicev1.DPUServiceChainList{}
				Expect(testClient.List(ctx, gotDPUServiceChains)).To(Succeed())
				Expect(gotDPUServiceChains.Items).To(HaveLen(2))
				DeferCleanup(func() {
					By("Cleaning up the finalizers so that objects can be deleted")
					for _, dpuServiceChain := range gotDPUServiceChains.Items {
						Expect(client.IgnoreNotFound(testClient.Patch(ctx, &dpuServiceChain, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
					}
				})

				for _, dpuServiceChain := range gotDPUServiceChains.Items {
					finalizers := dpuServiceChain.GetFinalizers()
					finalizers = append(finalizers, "test.io/some-finalizer")
					dpuServiceChain.SetFinalizers(finalizers)
					dpuServiceChain.GetObjectKind().SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
					dpuServiceChain.SetManagedFields(nil)
					Expect(testClient.Patch(ctx, &dpuServiceChain, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}

					// ApplyOnLabelChange should be true because we are modifying a disruptive DPUServiceChain
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
				}

				By("checking that the DPUSets are correctly updated")
				dpuSetGenerationAfterModification := make(map[string]int64, 2)
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						dpuSetGenerationAfterModification[dpuSet.Name] = dpuSet.Generation
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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

				By("triggering additional DPUDeployment reconciliations and ensuring the DPUSet generation doesn't change and ApplyOnLabelChange is still true")
				Consistently(func(g Gomega) {
					updatedDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), updatedDPUDeployment)).To(Succeed())
					g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedDPUDeployment)).To(Succeed())

					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					for _, dpuSet := range gotDPUSetList.Items {
						// Validate that the generation hasn't changed
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name]))
						// Validate that ApplyOnLabelChange is still true
						g.Expect(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
					}
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("marking the DPUServiceChain ready")
				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				Expect(testClient.List(ctx, gotDPUServiceChainList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
				Expect(gotDPUServiceChainList.Items).To(HaveLen(2))
				for _, dpuServiceChain := range gotDPUServiceChainList.Items {
					if dpuServiceChain.Annotations["svc.dpu.nvidia.com/dpuservicechain-version"] == chainDigest2 {
						dpuServiceChain.Status.Conditions = []metav1.Condition{
							{
								Type:               string(conditions.TypeReady),
								Status:             metav1.ConditionTrue,
								Reason:             string(conditions.ReasonSuccess),
								LastTransitionTime: metav1.NewTime(time.Now()),
								ObservedGeneration: dpuServiceChain.Generation,
							},
						}
						dpuServiceChain.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
						dpuServiceChain.SetManagedFields(nil)
						Expect(testClient.Status().Patch(ctx, &dpuServiceChain, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
					}
				}
				By("marking the DPUSet ready")
				gotDPUSetList := &provisioningv1.DPUSetList{}
				Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					dpuSet.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuSet.Generation,
						},
					}
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					Expect(testClient.Status().Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("triggering additional DPUDeployment reconciliations and ensuring the DPUSet generation doesn't change and ApplyOnLabelChange is still true")
				Consistently(func(g Gomega) {
					updatedDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), updatedDPUDeployment)).To(Succeed())
					g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedDPUDeployment)).To(Succeed())

					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					for _, dpuSet := range gotDPUSetList.Items {
						// Validate that the generation hasn't changed
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name]))
						// Validate that ApplyOnLabelChange is still true
						g.Expect(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
					}
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("Removing the fake finalizers")
				gotDPUServiceChains = &dpuservicev1.DPUServiceChainList{}
				Expect(testClient.List(ctx, gotDPUServiceChains)).To(Succeed())
				Expect(gotDPUServiceChains.Items).To(HaveLen(2))
				for _, dpuServiceChain := range gotDPUServiceChains.Items {
					Expect(client.IgnoreNotFound(testClient.Patch(ctx, &dpuServiceChain, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`))))).To(Succeed())
				}

				By("checking that the DPUServiceChains are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUServiceChains := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChains)).To(Succeed())
					g.Expect(gotDPUServiceChains.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuServiceChain := range gotDPUServiceChains.Items {
						g.Expect(dpuServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceChain.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservicechain-version", chainDigest2))
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the DPUSets are correctly updated")
				for i := range expectedDPUSetSpecs {
					// The old DPUServiceChain has been deleted and the upgrade is complete, so the ApplyOnLabelChange
					// should be false
					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(false)
				}
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name] + 1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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

				By("triggering additional DPUDeployment reconciliations and ensuring the DPUSet generation doesn't change and ApplyOnLabelChange is still false")
				Consistently(func(g Gomega) {
					updatedDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), updatedDPUDeployment)).To(Succeed())
					g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedDPUDeployment)).To(Succeed())

					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					for _, dpuSet := range gotDPUSetList.Items {
						// Validate that the generation hasn't changed
						g.Expect(dpuSet.Generation).To(Equal(dpuSetGenerationAfterModification[dpuSet.Name] + 1))
						// Validate that ApplyOnLabelChange is still false
						g.Expect(dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
					}
				}).WithTimeout(5 * time.Second).Should(Succeed())

			})
			It("should keep the existing DPUSets labels on update of a dpudeployment service chain", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				chainDigestOriginal := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("retrieving the DPUServiceChain and DPUService")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					gotDPUServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(gotDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

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

				By("modifying the dpudeployment service chain and checking the outcome")
				dpuDeployment.Spec.ServiceChains.Switches = []dpuservicev1.DPUDeploymentSwitch{
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
									InterfaceName: "otherinterface",
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
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				// We need to get the object to calculate the digest taking into account the defaults added by the API server
				gotDPUDeployment := &dpuservicev1.DPUDeployment{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), gotDPUDeployment)).To(Succeed())
				chainDigestModified := calculateDPUServiceChainVersionDigest(gotDPUDeployment.Spec.ServiceChains.Switches)
				Expect(chainDigestModified).NotTo(Equal(chainDigestOriginal))

				By("checking that the DPUServiceChain is correctly updated")
				Eventually(func(g Gomega) {
					gotDPUServiceChains := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChains)).To(Succeed())
					g.Expect(gotDPUServiceChains.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuServiceChain := range gotDPUServiceChains.Items {
						g.Expect(dpuServiceChain.Labels).To(HaveLen(1))
						g.Expect(dpuServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigestModified))
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the DPUSets are correctly updated")
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuSet := range gotDPUSetList.Items {
						g.Expect(dpuSet.Labels).To(HaveLen(1))
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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
			It("should update the existing DPUSets on update of the referenced BFB", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = initialDPUSetSettings
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("retrieving the DPUServiceChain and DPUService")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					gotDPUServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(gotDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

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
				bfb2 := createMinimalBFBWithStatus("somebfb2", testNS.Name)

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
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
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
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("retrieving the DPUServiceChain and DPUService")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					dpuServiceChainList := getDPUServiceChainList()
					g.Expect(dpuServiceChainList.Items).To(HaveLen(1))
					gotDPUServiceChain = &dpuServiceChainList.Items[0]
					g.Expect(gotDPUServiceChain).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				for i := range expectedDPUSetSpecs {
					if expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster == nil {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{}
					}
					expectedDPUSetSpecs[i].DPUTemplate.Spec.Cluster.NodeLabels = map[string]string{
						"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
						dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
						"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
					}

					expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
						fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
					}
				}

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
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey3": "nodevalue3",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey3": "dpuvalue3",
						},
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
						g.Expect(dpuSet.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))

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
					// All existing DPUSetSpecs should have ApplyOnLabelChange set to false
					// Because we are not modifying the DPUServices and DPUServiceChains
					for i := range expectedDPUSetSpecs {
						expectedDPUSetSpecs[i].DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(false)
					}
					expectedDPUSetSpecs = append(expectedDPUSetSpecs, provisioningv1.DPUSetSpec{
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey3": "nodevalue3",
							},
						},
						DPUDeviceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"dpukey3": "dpuvalue3",
							},
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
									Action: provisioningv1.Action{
										Drain: ptr.To(true),
										Force: ptr.To(false),
									},
									UpgradePolicy: provisioningv1.UpgradePolicy{
										// ApplyOnLabelChange should be false because we are not modifying the DPUServices and DPUServiceChains
										// A nodeEffect should still be applied as this is a new DPUSet, and by default
										// we add our owned objects to the NodeMaintenanceAdditionalRequestors list
										ApplyOnLabelChange: ptr.To(false),
										NodeMaintenanceAdditionalRequestors: []string{
											fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUServiceChain.Name),
											fmt.Sprintf("%s_%s", getParentDPUDeploymentLabelValue(types.NamespacedName{Namespace: dpuDeployment.Namespace, Name: dpuDeployment.Name}), gotDPUService.Name),
										},
									},
								},
								Cluster: &provisioningv1.ClusterSpec{
									NodeLabels: map[string]string{
										"svc.dpu.nvidia.com/dpuservice-someservice-version": gotDPUService.Name,
										dpuservicev1.ParentDPUDeploymentNameLabel:           fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name),
										"svc.dpu.nvidia.com/dpuservicechain-version":        gotDPUServiceChain.Name,
									},
								},
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
				expectedDPUSetSpecs = expectedDPUSetSpecs[1:]
				Eventually(func(g Gomega) {
					gotDPUSetList := &provisioningv1.DPUSetList{}
					g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
					g.Expect(gotDPUSetList.Items).To(HaveLen(1))
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
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
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
						Name:    "some_interface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
					{
						Name:           "virt_interface",
						Network:        "nad3",
						VirtualNetwork: ptr.To("vnet1"),
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
				By("checking that correct DPUServiceInterfaces are created")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(3))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("retrieving the DPUService")
					var gotDPUService *dpuservicev1.DPUService
					Eventually(func(g Gomega) {
						dpuServiceList := getDPUServiceList()
						g.Expect(dpuServiceList.Items).To(HaveLen(1))
						gotDPUService = &dpuServiceList.Items[0]
						g.Expect(gotDPUService).ToNot(BeNil())
					}).WithTimeout(30 * time.Second).Should(Succeed())

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}

					genExpectedDPUServiceInterfaceSpecs := func(dpuserviceName, networkName, ifName string, virtualNetworkName *string) dpuservicev1.DPUServiceInterfaceSpec {
						return dpuservicev1.DPUServiceInterfaceSpec{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{dpuserviceName},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: ifName,
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:      "dpudeployment_dpudeployment_someservice",
												Network:        networkName,
												InterfaceName:  ifName,
												VirtualNetwork: virtualNetworkName,
											},
										},
									},
								},
							},
						}
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						genExpectedDPUServiceInterfaceSpecs(gotDPUService.Name, "nad1", "some_interface", nil),
						genExpectedDPUServiceInterfaceSpecs(gotDPUService.Name, "nad2", "otherinterface", nil),
						genExpectedDPUServiceInterfaceSpecs(gotDPUService.Name, "nad3", "virt_interface", ptr.To("vnet1")),
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create the correct DPUServiceInterfaces when DPUDeployment specifies multiple DPUSets", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "some_interface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
					{
						Name:           "virt_interface",
						Network:        "nad3",
						VirtualNetwork: ptr.To("vnet1"),
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment with multiple DPUSets")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
					{
						NameSuffix: "set2",
						DPUClusterSelector: map[string]string{
							"region": "us-east",
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
				By("checking that correct DPUServiceInterfaces are created")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(3))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("retrieving the DPUService")
					var gotDPUService *dpuservicev1.DPUService
					Eventually(func(g Gomega) {
						dpuServiceList := getDPUServiceList()
						g.Expect(dpuServiceList.Items).To(HaveLen(1))
						gotDPUService = &dpuServiceList.Items[0]
						g.Expect(gotDPUService).ToNot(BeNil())
					}).WithTimeout(30 * time.Second).Should(Succeed())

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}

					genExpectedDPUServiceInterfaceSpecs := func(dpuserviceName, networkName, ifName string, virtualNetworkName *string) dpuservicev1.DPUServiceInterfaceSpec {
						return dpuservicev1.DPUServiceInterfaceSpec{
							DPUClusterSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "region",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"us-east", "us-west"},
									},
								},
							},
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{dpuserviceName},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: ifName,
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:      "dpudeployment_dpudeployment_someservice",
												Network:        networkName,
												InterfaceName:  ifName,
												VirtualNetwork: virtualNetworkName,
											},
										},
									},
								},
							},
						}
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						genExpectedDPUServiceInterfaceSpecs(gotDPUService.Name, "nad1", "some_interface", nil),
						genExpectedDPUServiceInterfaceSpecs(gotDPUService.Name, "nad2", "otherinterface", nil),
						genExpectedDPUServiceInterfaceSpecs(gotDPUService.Name, "nad3", "virt_interface", ptr.To("vnet1")),
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should patch a manually modified DPUServiceInterface as long as the modification is not on the version annotation", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "some_interface",
						Network: "nad1",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that the DPUServiceInterface is created")
				var gotDPUServiceInterface *dpuservicev1.DPUServiceInterface
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
					gotDPUServiceInterface = &gotDPUServiceInterfaceList.Items[0]
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
					gotDPUService = &gotDPUServiceList.Items[0]

					By("checking the original value of the network")
					g.Expect(gotDPUServiceInterface.Spec.Template.Spec.Template.Spec.Service.Network).To(Equal("nad1"))

					By("checking the nodeSelector")
					g.Expect(gotDPUServiceInterface.Spec.Template.Spec.NodeSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{gotDPUService.Name},
							},
							{
								Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceInterface manually")
				gotDPUServiceInterface.Spec.Template.Spec.Template.Spec.Service.Network = "nad15"
				gotDPUServiceInterface.SetManagedFields(nil)
				gotDPUServiceInterface.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
				Expect(testClient.Patch(ctx, gotDPUServiceInterface, client.Apply, client.ForceOwnership, client.FieldOwner("some-test-controller"))).To(Succeed())

				By("checking that the DPUServiceInterface is reverted to the original")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
					g.Expect(gotDPUServiceInterfaceList.Items[0].UID).To(Equal(gotDPUServiceInterface.UID))

					By("checking the original value of the network")
					g.Expect(gotDPUServiceInterfaceList.Items[0].Spec.Template.Spec.Template.Spec.Service.Network).To(Equal("nad1"))

					By("checking the nodeSelector")
					g.Expect(gotDPUServiceInterfaceList.Items[0].Spec.Template.Spec.NodeSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{gotDPUService.Name},
							},
							{
								Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing DPUServiceInterfaces on update of non-disruptive DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{ApplyNodeEffect: ptr.To(false)}
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "some_interface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the DPUService")
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("waiting for the initial DPUServiceInterfaces to be applied")
				firstDPUServiceInterfaceUIDs := make([]types.UID, 0, 2)
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuService := range gotDPUServiceInterfaceList.Items {
						firstDPUServiceInterfaceUIDs = append(firstDPUServiceInterfaceUIDs, dpuService.UID)
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration), dpuServiceConfiguration)).To(Succeed())
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "some_interface",
						Network: "nad3",
					},
					{
						Name:    "otherinterface",
						Network: "nad4",
					},
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))

					// Validate that these are the same objects
					for _, serviceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(firstDPUServiceInterfaceUIDs).To(ContainElement(serviceInterface.UID))
					}

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "some_interface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad3",
												InterfaceName: "some_interface",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad4",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServiceInterfaces on update of disruptive DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the initial DPUService")
				var gotInitialDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotInitialDPUService = &dpuServiceList.Items[0]
					g.Expect(gotInitialDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

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
						Name:    "otherinterface",
						Network: "nad4",
					},
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
				Expect(versionDigest).ToNot(Equal(versionDigest2))

				By("retrieving the new DPUService")
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))
					for _, dpuService := range gotDPUServiceList.Items {
						if dpuService.GetAnnotations()[dpuServiceVersionAnnotationKey] == versionDigest2 {
							gotDPUService = &dpuService
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the old DPUServiceInterfaces co-exist with the new ones until the current DPUService becomes ready")
				Eventually(func(g Gomega) {
					instancesThatShouldExistPerVersionDigest := map[string]int{
						versionDigest:  2,
						versionDigest2: 2,
					}
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(4))

					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

						versionAnnotationKey := "svc.dpu.nvidia.com/dpuservice-version"
						g.Expect(dpuServiceInterface.Annotations).To(HaveKey(versionAnnotationKey))
						versionAnnotationValue := dpuServiceInterface.Annotations["svc.dpu.nvidia.com/dpuservice-version"]
						g.Expect(instancesThatShouldExistPerVersionDigest).To(HaveKey(versionAnnotationValue))
						instancesThatShouldExistPerVersionDigest[versionAnnotationValue]--
					}
					for k := range instancesThatShouldExistPerVersionDigest {
						g.Expect(instancesThatShouldExistPerVersionDigest[k]).To(BeZero())
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						// Old
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
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
						// Old
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad2",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
						// New
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
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
						// New
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad4",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).MustPassRepeatedly(10).Should(Succeed())

				By("Marking the DPUService ready")
				patcher := patch.NewSerialPatcher(gotDPUService, testClient)
				gotDPUService.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: gotDPUService.Generation,
					},
				}
				Expect(patcher.Patch(ctx, gotDPUService, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that old DPUServiceInterfaces are not deleted until DPUSet is ready")
				Consistently(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(4))
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("marking the DPUSet ready")
				gotDPUSetList := &provisioningv1.DPUSetList{}
				Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					dpuSet.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuSet.Generation,
						},
					}
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					Expect(testClient.Status().Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest2))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad4",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should not delete the old DPUServiceInterfaces on update of disruptive DPUServiceConfiguration "+
				"that changes the name of an interface", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the initial DPUService")
				var gotInitialDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotInitialDPUService = &dpuServiceList.Items[0]
					g.Expect(gotInitialDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

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
						Name:    "newinterface",
						Network: "nad3",
					},
					{
						Name:    "otherinterface",
						Network: "nad4",
					},
				}
				dpuServiceConfiguration.SetManagedFields(nil)
				dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
				Expect(versionDigest).ToNot(Equal(versionDigest2))

				By("retrieving the new DPUService")
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))
					for _, dpuService := range gotDPUServiceList.Items {
						if dpuService.GetAnnotations()[dpuServiceVersionAnnotationKey] == versionDigest2 {
							gotDPUService = &dpuService
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the old DPUServiceInterfaces co-exist with the new ones until the current DPUService becomes ready")
				Eventually(func(g Gomega) {
					instancesThatShouldExistPerVersionDigest := map[string]int{
						versionDigest:  2,
						versionDigest2: 2,
					}
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(4))

					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

						versionAnnotationKey := "svc.dpu.nvidia.com/dpuservice-version"
						g.Expect(dpuServiceInterface.Annotations).To(HaveKey(versionAnnotationKey))
						versionAnnotationValue := dpuServiceInterface.Annotations["svc.dpu.nvidia.com/dpuservice-version"]
						g.Expect(instancesThatShouldExistPerVersionDigest).To(HaveKey(versionAnnotationValue))
						instancesThatShouldExistPerVersionDigest[versionAnnotationValue]--
					}
					for k := range instancesThatShouldExistPerVersionDigest {
						g.Expect(instancesThatShouldExistPerVersionDigest[k]).To(BeZero())
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						// Old
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
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
						// Old
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad2",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
						// New
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "newinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad3",
												InterfaceName: "newinterface",
											},
										},
									},
								},
							},
						},
						// New
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad4",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).MustPassRepeatedly(10).Should(Succeed())

				By("Marking the DPUService ready")
				patcher := patch.NewSerialPatcher(gotDPUService, testClient)
				gotDPUService.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: gotDPUService.Generation,
					},
				}
				Expect(patcher.Patch(ctx, gotDPUService, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest2))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "newinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad3",
												InterfaceName: "newinterface",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad4",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should not delete any of the DPUServiceInterfaces associated with service 2 on update of disruptive "+
				"DPUServiceConfiguration that changes the name of an interface that is associated with service 1", func() {
				By("Creating the dependencies for service 1")
				dpuServiceConfiguration1 := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration1.Name = "service-1"
				dpuServiceConfiguration1.Spec.DeploymentServiceName = "service-1"
				dpuServiceConfiguration1.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration1)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration1)

				dpuServiceTemplate1 := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate1.Name = "service-1"
				dpuServiceTemplate1.Spec.DeploymentServiceName = "service-1"
				Expect(testClient.Create(ctx, dpuServiceTemplate1)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate1)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate1)

				versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration1, dpuServiceTemplate1)

				By("Creating the dependencies for service 2")
				dpuServiceConfiguration2 := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration2.Name = "service-2"
				dpuServiceConfiguration2.Spec.DeploymentServiceName = "service-2"
				dpuServiceConfiguration2.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "if1",
						Network: "nad3",
					},
					{
						Name:    "if2",
						Network: "nad4",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration2)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration2)

				dpuServiceTemplate2 := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate2.Name = "service-2"
				dpuServiceTemplate2.Spec.DeploymentServiceName = "service-2"
				Expect(testClient.Create(ctx, dpuServiceTemplate2)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate2)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate2)

				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration2, dpuServiceTemplate2)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
					"service-1": {
						ServiceTemplate:      dpuServiceTemplate1.Name,
						ServiceConfiguration: dpuServiceConfiguration1.Name,
					},
					"service-2": {
						ServiceTemplate:      dpuServiceTemplate2.Name,
						ServiceConfiguration: dpuServiceConfiguration2.Name,
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the initial DPUServices and DPUServiceInterfaces")
				gotInitialDPUServices := make(map[string]dpuservicev1.DPUService)
				gotInitialDPUServiceInterfaces := make(map[string][]dpuservicev1.DPUServiceInterface)
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(2))
					for _, dpuService := range dpuServiceList.Items {
						versionAnnotationKey := "svc.dpu.nvidia.com/dpuservice-version"
						g.Expect(dpuService.Annotations).To(HaveKey(versionAnnotationKey))
						gotInitialDPUServices[dpuService.Annotations[versionAnnotationKey]] = dpuService
					}

					dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, dpuServiceInterfaceList)).To(Succeed())
					g.Expect(dpuServiceInterfaceList.Items).To(HaveLen(4))
					for _, dpuServiceInterface := range dpuServiceInterfaceList.Items {
						versionAnnotationKey := "svc.dpu.nvidia.com/dpuservice-version"
						g.Expect(dpuServiceInterface.Annotations).To(HaveKey(versionAnnotationKey))
						gotInitialDPUServiceInterfaces[dpuServiceInterface.Annotations[versionAnnotationKey]] = append(gotInitialDPUServiceInterfaces[dpuServiceInterface.Annotations[versionAnnotationKey]], dpuServiceInterface)
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("waiting for the initial DPUServiceInterfaces to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(4))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object and checking the outcome")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceConfiguration1), dpuServiceConfiguration1)).To(Succeed())
				dpuServiceConfiguration1.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "newnameif1",
						Network: "nad15",
					},
					{
						Name:    "newnameif2",
						Network: "nad23",
					},
				}
				dpuServiceConfiguration1.SetManagedFields(nil)
				dpuServiceConfiguration1.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
				Expect(testClient.Patch(ctx, dpuServiceConfiguration1, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())

				versionDigest1b := calculateDPUServiceVersionDigest(dpuServiceConfiguration1, dpuServiceTemplate1)
				Expect(versionDigest1b).ToNot(Equal(versionDigest1))

				By("retrieving the updated DPUServices")
				gotUpdatedDPUServices := make(map[string]dpuservicev1.DPUService)
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(3))
					for _, dpuService := range dpuServiceList.Items {
						versionAnnotationKey := "svc.dpu.nvidia.com/dpuservice-version"
						g.Expect(dpuService.Annotations).To(HaveKey(versionAnnotationKey))
						gotUpdatedDPUServices[dpuService.Annotations[versionAnnotationKey]] = dpuService
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("checking that the old DPUServiceInterfaces co-exist with the new ones until the current DPUService becomes ready")
				Eventually(func(g Gomega) {
					instancesThatShouldExistPerVersionDigest := map[string]int{
						versionDigest1:  2,
						versionDigest2:  2,
						versionDigest1b: 2,
					}
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(6))

					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta.Labels[dpuservicev1.DPFServiceIDLabelKey], "_", 3)[2]
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

						versionAnnotationKey := "svc.dpu.nvidia.com/dpuservice-version"
						g.Expect(dpuServiceInterface.Annotations).To(HaveKey(versionAnnotationKey))
						versionAnnotationValue := dpuServiceInterface.Annotations["svc.dpu.nvidia.com/dpuservice-version"]
						g.Expect(instancesThatShouldExistPerVersionDigest).To(HaveKey(versionAnnotationValue))
						instancesThatShouldExistPerVersionDigest[versionAnnotationValue]--
					}
					for k := range instancesThatShouldExistPerVersionDigest {
						g.Expect(instancesThatShouldExistPerVersionDigest[k]).To(BeZero())
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceInterfaceSpec, 0, len(gotDPUServiceInterfaceList.Items))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						specs = append(specs, dpuServiceInterface.Spec)
					}
					g.Expect(specs).To(ConsistOf([]dpuservicev1.DPUServiceInterfaceSpec{
						// service 2 - intact
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUServices[versionDigest2].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-2",
												ServiceInterfaceInterfaceNameLabel: "if1",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-2",
												Network:       "nad3",
												InterfaceName: "if1",
											},
										},
									},
								},
							},
						},
						// service 2 - intact
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUServices[versionDigest2].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-2",
												ServiceInterfaceInterfaceNameLabel: "if2",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-2",
												Network:       "nad4",
												InterfaceName: "if2",
											},
										},
									},
								},
							},
						},
						// service 1 - old
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUServices[versionDigest1].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-1",
												ServiceInterfaceInterfaceNameLabel: "someinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-1",
												Network:       "nad1",
												InterfaceName: "someinterface",
											},
										},
									},
								},
							},
						},
						// service 1 - old
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUServices[versionDigest1].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-1",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-1",
												Network:       "nad2",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
						// service 1 - new
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotUpdatedDPUServices[versionDigest1b].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-1",
												ServiceInterfaceInterfaceNameLabel: "newnameif1",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-1",
												Network:       "nad15",
												InterfaceName: "newnameif1",
											},
										},
									},
								},
							},
						},
						// service 1 - new
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotUpdatedDPUServices[versionDigest1b].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-1",
												ServiceInterfaceInterfaceNameLabel: "newnameif2",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-1",
												Network:       "nad23",
												InterfaceName: "newnameif2",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).MustPassRepeatedly(10).Should(Succeed())

				By("Marking the DPUService ready")
				currentDPUService := gotUpdatedDPUServices[versionDigest1b]
				patcher := patch.NewSerialPatcher(&currentDPUService, testClient)
				currentDPUService.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: currentDPUService.Generation,
					},
				}
				Expect(patcher.Patch(ctx, &currentDPUService, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(4))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta.Labels[dpuservicev1.DPFServiceIDLabelKey], "_", 3)[2]
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuServiceInterface.Annotations).To(HaveKey("svc.dpu.nvidia.com/dpuservice-version"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("Checking that original objects associated with service-2 are not deleted")
					for _, originalDPUServiceInterface := range gotInitialDPUServiceInterfaces[versionDigest2] {
						var found bool
						for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
							if originalDPUServiceInterface.UID == dpuServiceInterface.UID {
								found = true
							}
						}
						g.Expect(found).To(BeTrue())
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUServices[versionDigest2].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-2",
												ServiceInterfaceInterfaceNameLabel: "if1",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-2",
												Network:       "nad3",
												InterfaceName: "if1",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotInitialDPUServices[versionDigest2].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-2",
												ServiceInterfaceInterfaceNameLabel: "if2",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-2",
												Network:       "nad4",
												InterfaceName: "if2",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotUpdatedDPUServices[versionDigest1b].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-1",
												ServiceInterfaceInterfaceNameLabel: "newnameif1",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-1",
												Network:       "nad15",
												InterfaceName: "newnameif1",
											},
										},
									},
								},
							},
						},
						{
							Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
								Spec: dpuservicev1.ServiceInterfaceSetSpec{
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotUpdatedDPUServices[versionDigest1b].Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_service-1",
												ServiceInterfaceInterfaceNameLabel: "newnameif2",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_service-1",
												Network:       "nad23",
												InterfaceName: "newnameif2",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should not create new DPUServiceInterfaces, when service is disruptive, on update of the .spec.dpuClusterSelector in the DPUDeployment", func() {
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

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				By("Creating the DPUDeployment with initial DPUClusterSelector")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServiceInterface to be applied")
				var initialDPUServiceInterfaceUID types.UID
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
					initialDPUServiceInterfaceUID = gotDPUServiceInterfaceList.Items[0].UID
					g.Expect(gotDPUServiceInterfaceList.Items[0].Annotations).To(HaveKeyWithValue(dpuServiceVersionAnnotationKey, versionDigest))
					g.Expect(gotDPUServiceInterfaceList.Items[0].Spec.DPUClusterSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-west"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUClusterSelector in the DPUDeployment by updating DPUSets")
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
					{
						NameSuffix: "set2",
						DPUClusterSelector: map[string]string{
							"region": "us-east",
						},
					},
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that the DPUServiceInterface is NOT recreated but updated in place")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))

					dpuServiceInterface := gotDPUServiceInterfaceList.Items[0]
					g.Expect(dpuServiceInterface.UID).To(Equal(initialDPUServiceInterfaceUID))
					g.Expect(dpuServiceInterface.Annotations).To(HaveKeyWithValue(dpuServiceVersionAnnotationKey, versionDigest))

					By("verifying the DPUClusterSelector is updated with aggregated values")
					g.Expect(dpuServiceInterface.Spec.DPUClusterSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-east", "us-west"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should delete DPUServiceInterfaces that are no longer part of the DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{ApplyNodeEffect: ptr.To(false)}
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
					{
						Name:    "otherinterface",
						Network: "nad2",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the DPUService")
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

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

				By("Marking the DPUService ready")
				patcher := patch.NewSerialPatcher(gotDPUService, testClient)
				gotDPUService.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: gotDPUService.Generation,
					},
				}
				Expect(patcher.Patch(ctx, gotDPUService, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that the DPUServiceInterfaces are updated")
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))

					By("checking the object metadata")
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
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
				dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{ApplyNodeEffect: ptr.To(false)}
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{
					{
						Name:    "someinterface",
						Network: "nad1",
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("retrieving the DPUService")
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					dpuServiceList := getDPUServiceList()
					g.Expect(dpuServiceList.Items).To(HaveLen(1))
					gotDPUService = &dpuServiceList.Items[0]
					g.Expect(gotDPUService).ToNot(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())

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
						Name:    "otherinterface",
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
						g.Expect(dpuServiceInterface.Labels).To(HaveLen(2))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						g.Expect(dpuServiceInterface.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "someservice"))
						g.Expect(dpuServiceInterface.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
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
									NodeSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
									Template: dpuservicev1.ServiceInterfaceSpecTemplate{
										ObjectMeta: dpuservicev1.ObjectMeta{
											Labels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someservice",
												ServiceInterfaceInterfaceNameLabel: "otherinterface",
											},
										},
										Spec: dpuservicev1.ServiceInterfaceSpec{
											InterfaceType: dpuservicev1.InterfaceTypeService,
											Service: &dpuservicev1.ServiceDef{
												ServiceID:     "dpudeployment_dpudeployment_someservice",
												Network:       "nad2",
												InterfaceName: "otherinterface",
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should recreate the DPUServiceInterfaces on manual delete of the DPUServiceInterfaces", func() {
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

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
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
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
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
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet = &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
					Annotations: map[string]string{"annkey1": "annval1"},
					Labels:      map[string]string{"labelkey1": "labelval1"},
					Resources: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("100Mi"),
					},
					UpdateStrategy: &appsv1.DaemonSetUpdateStrategy{
						RollingUpdate: &appsv1.RollingUpdateDaemonSet{
							MaxUnavailable: ptr.To(intstr.FromInt(1)),
						},
					},
				}
				dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To(true)
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key1":"value1"}`)}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-1"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-1"
				dpuServiceTemplate.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key1":"someothervalue"}`)}
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-2"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-2"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet = &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
					Annotations: map[string]string{"annkey2": "annval2"},
					Labels:      map[string]string{"labelkey2": "labelval2"},
				}
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key2":"value2"}`)}
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "if2", Network: "nad2"}, {Name: "if3", Network: "nad3"}}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-2"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-2"
				dpuServiceTemplate.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key3":"value3"}`)}
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-3"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-3"
				dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet = &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
					Annotations: map[string]string{"annkey3": "annval3"},
					Labels:      map[string]string{"labelkey3": "labelval3"},
				}
				dpuServiceConfiguration.Spec.ServiceConfiguration.ConfigPorts = &dpuservicev1.ConfigPorts{
					ServiceType: corev1.ServiceTypeNodePort,
					Ports: []dpuservicev1.ConfigPort{
						{
							Name:     "port1",
							Port:     1234,
							Protocol: corev1.ProtocolTCP,
						},
					},
				}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-3"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-3"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest3 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

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

				versions := map[string]string{
					"service-1": versionDigest1,
					"service-2": versionDigest2,
					"service-3": versionDigest3}
				By("checking that correct DPUServices are created")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}

					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(3))

					var gotInClusterDPUService *dpuservicev1.DPUService
					for _, dpuService := range gotDPUServiceList.Items {
						if dpuService.Spec.DeployInCluster != nil && *dpuService.Spec.DeployInCluster == true {
							gotInClusterDPUService = &dpuService
							break
						}
					}
					g.Expect(gotInClusterDPUService).ToNot(BeNil())

					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(dpuService.Labels).To(HaveLen(2))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versions[serviceName]))
						g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					names := make(map[string]string)
					specs := make([]dpuservicev1.DPUServiceSpec, 0, 3)
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						names[serviceName] = dpuService.Name
					}
					g.Expect(specs).To(BeComparableTo([]dpuservicev1.DPUServiceSpec{
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-1"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels:      map[string]string{"labelkey1": "labelval1", "svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
								Annotations: map[string]string{"annkey1": "annval1"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-1"), 10)),
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotInClusterDPUService.Name},
												},
											},
										},
									},
								},
								Resources: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
								UpdateStrategy: &appsv1.DaemonSetUpdateStrategy{
									RollingUpdate: &appsv1.RollingUpdateDaemonSet{
										MaxUnavailable: ptr.To(intstr.FromInt(1)),
									},
								},
							},
							DeployInCluster: ptr.To(true),
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-2"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels:      map[string]string{"labelkey2": "labelval2", "svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
								Annotations: map[string]string{"annkey2": "annval2"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{names["service-2"]},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
							Interfaces: gotDPUServiceInterfaceNames["service-2"],
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-3"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels:      map[string]string{"labelkey3": "labelval3", "svc.dpu.nvidia.com/dpudeployment-service": "service-3"},
								Annotations: map[string]string{"annkey3": "annval3"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-3-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{names["service-3"]},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
							ConfigPorts: &dpuservicev1.ConfigPorts{
								ServiceType: corev1.ServiceTypeNodePort,
								Ports: []dpuservicev1.ConfigPort{
									{
										Name:     "port1",
										Port:     1234,
										Protocol: corev1.ProtocolTCP,
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})

			It("should create the correct DPUServices when DPUDeployment specifies multiple DPUSets and one of them has no nodeSelector", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-1"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-1"
				dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To(true)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-1"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-1"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-2"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-2"
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-2"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-2"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
					{
						NameSuffix: "dpuset2",
						DPUClusterSelector: map[string]string{
							"region": "us-east",
						},
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey2": "nodevalue2",
							},
						},
					},
				}
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

				versions := map[string]string{
					"service-1": versionDigest1,
					"service-2": versionDigest2,
				}
				By("checking that correct DPUServices are created")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

					var gotInClusterDPUService *dpuservicev1.DPUService
					for _, dpuService := range gotDPUServiceList.Items {
						if dpuService.Spec.DeployInCluster != nil && *dpuService.Spec.DeployInCluster == true {
							gotInClusterDPUService = &dpuService
							break
						}
					}
					g.Expect(gotInClusterDPUService).ToNot(BeNil())

					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(dpuService.Labels).To(HaveLen(2))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versions[serviceName]))
						g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					names := make(map[string]string)
					specs := make([]dpuservicev1.DPUServiceSpec, 0, 3)
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						names[serviceName] = dpuService.Name
					}
					g.Expect(specs).To(BeComparableTo([]dpuservicev1.DPUServiceSpec{
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-1"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-1"), 10)),
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotInClusterDPUService.Name},
												},
											},
										},
									},
								},
							},
							DeployInCluster: ptr.To(true),
						},
						{
							DPUClusterSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "region",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"us-east", "us-west"},
									},
								},
							},
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-2"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{names["service-2"]},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create the correct dependent DPUServices", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service1"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service1"
				dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To(true)
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key1":"value1"}`)}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service1"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service1"
				dpuServiceTemplate.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key1":"someothervalue"}`)}
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service2"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service2"
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key2":"value2"}`)}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service2"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service2"
				dpuServiceTemplate.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"key3":"value3"}`)}
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				dpuServiceConfiguration = getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service3"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service3"
				dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
				service3Value := `{"service2Name":"{{.Services.service2.Name}}","service2Namespace":"{{.Services.service2.Namespace}}"}`
				dpuServiceConfiguration.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(service3Value)}
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate = getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service3"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service3"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service1",
					ServiceConfiguration: "service1",
				}
				dpuDeployment.Spec.Services["service2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service2",
					ServiceConfiguration: "service2",
					DependsOn: []dpuservicev1.LocalObjectDependency{
						{
							Name: "service1",
						},
					},
				}
				dpuDeployment.Spec.Services["service3"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service3",
					ServiceConfiguration: "service3",
					DependsOn: []dpuservicev1.LocalObjectDependency{
						{
							Name: "service2",
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				versions := map[string]string{
					"service1": versionDigest1,
					"service2": versionDigest2,
				}
				var (
					gotDPUService3 dpuservicev1.DPUService
					gotDPUService2 dpuservicev1.DPUService
				)
				By("checking that correct DPUServices are created")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterface")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(3))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						if !strings.HasPrefix(dpuService.GetName(), "service3") {
							// we discard service3 here, as it values is mutated by template rendering
							// so we don't know the exact digest
							g.Expect(dpuService.Labels).To(HaveLen(2))
							g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
							serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
							g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
							g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versions[serviceName]))
							g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
						}

						if strings.HasPrefix(dpuService.GetName(), "service2") {
							// service2 should have its interface created
							g.Expect(gotDPUServiceInterfaceNames).To(HaveKey("service2"))
							g.Expect(gotDPUServiceInterfaceNames["service2"]).To(HaveLen(1))
							g.Expect(dpuService.Spec.Interfaces).To(ConsistOf(gotDPUServiceInterfaceNames["service2"]))
							gotDPUService2 = dpuService
						}

						if strings.HasPrefix(dpuService.GetName(), "service3") {
							g.Expect(gotDPUServiceInterfaceNames).To(HaveKey("service3"))
							g.Expect(gotDPUServiceInterfaceNames["service3"]).To(HaveLen(1))
							g.Expect(dpuService.Spec.Interfaces).To(ConsistOf(gotDPUServiceInterfaceNames["service3"]))
							gotDPUService3 = dpuService
						}
					}

					g.Expect(gotDPUService2).ToNot(BeNil())
					g.Expect(gotDPUService3).ToNot(BeNil())
					gotValue := `{"service2Name":"` + gotDPUService2.Name + `","service2Namespace":"` + gotDPUService2.Namespace + `"}`
					g.Expect(gotDPUService3.Spec.HelmChart.Values.Raw).To(Equal([]byte(gotValue)))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should patch a manually modified DPUService as long as the modification is not on the version annotation", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that the DPUService is created")
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
					gotDPUService = &gotDPUServiceList.Items[0]

					By("checking the original value of the chart version")
					g.Expect(gotDPUService.Spec.HelmChart.Source.Version).To(Equal("someversion"))

					By("checking the nodeSelector")
					g.Expect(gotDPUService.Spec.ServiceDaemonSet.NodeSelector).To(BeComparableTo(&corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{gotDPUService.Name},
									},
									{
										Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUService manually")
				gotDPUService.Spec.HelmChart.Source.Version = "someotherversion"
				gotDPUService.SetManagedFields(nil)
				gotDPUService.SetGroupVersionKind(dpuservicev1.DPUServiceGroupVersionKind)
				Expect(testClient.Patch(ctx, gotDPUService, client.Apply, client.ForceOwnership, client.FieldOwner("some-test-controller"))).To(Succeed())

				By("checking that the DPUService is reverted to the original")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
					g.Expect(gotDPUServiceList.Items[0].UID).To(Equal(gotDPUService.UID))

					By("checking the chart version")
					g.Expect(gotDPUServiceList.Items[0].Spec.HelmChart.Source.Version).To(Equal("someversion"))

					By("checking the nodeSelector")
					g.Expect(gotDPUServiceList.Items[0].Spec.ServiceDaemonSet.NodeSelector).To(BeComparableTo(&corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "svc.dpu.nvidia.com/dpuservice-someservice-version",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{gotDPUService.Name},
									},
									{
										Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing DPUService on update of non-disruptive DPUServiceConfiguration", func() {
				By("Creating the dependencies")
				service1VersionDigest1, service2VersionDigest1 := createReconcileDPUServicesNonDisruptiveDependencies(testNS.Name)
				service3VersionDigest1, service4VersionDigest1 := createReconcileDPUServicesInClusterNonDisruptiveDependencies(testNS.Name)
				versionDigest1ForService := map[string]string{
					"service-1": service1VersionDigest1,
					"service-2": service2VersionDigest1,
					"service-3": service3VersionDigest1,
					"service-4": service4VersionDigest1,
				}

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
				dpuDeployment.Spec.Services["service-4"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-4",
					ServiceConfiguration: "service-4",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServices to be applied")
				firstDPUServiceUIDs := make([]types.UID, 0, 4)
				gotFirstDPUServiceNames := make(map[string]string)
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(4))
					for _, dpuService := range gotDPUServiceList.Items {
						firstDPUServiceUIDs = append(firstDPUServiceUIDs, dpuService.UID)
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuService.Name, serviceName) {
								gotFirstDPUServiceNames[serviceName] = dpuService.Name
							}
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object for service-2 and service-4 services by making a label change")
				modifiedServices := []string{"service-2", "service-4"}
				for _, serviceName := range modifiedServices {
					dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: serviceName}, dpuServiceConfiguration)).To(Succeed())
					dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet = &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
						Labels: map[string]string{"newlabel2": fmt.Sprintf("newvalue-%s", serviceName)},
					}
					dpuServiceConfiguration.SetManagedFields(nil)
					dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
					Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())
				}

				By("checking that the version digests are updated only for service-2 and service-4")
				versionDigest2ForService := make(map[string]string)
				for serviceName, versionDigest := range versionDigest1ForService {
					dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: serviceName}, dpuServiceConfiguration)).To(Succeed())
					dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: serviceName}, dpuServiceTemplate)).To(Succeed())
					versionDigest2ForService[serviceName] = calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
					if slices.Contains(modifiedServices, serviceName) {
						Expect(versionDigest).ToNot(Equal(versionDigest2ForService[serviceName]))
					} else {
						Expect(versionDigest).To(Equal(versionDigest2ForService[serviceName]))
					}
				}

				By("checking that the DPUServices are updated as expected")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}

					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(4))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(dpuService.Labels).To(HaveLen(2))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest2ForService[serviceName]))
						// Validate that the object was not recreated
						g.Expect(firstDPUServiceUIDs).To(ContainElement(dpuService.UID))

						g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceSpec, 0, len(gotDPUServiceList.Items))
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
					}
					g.Expect(specs).To(BeComparableTo([]dpuservicev1.DPUServiceSpec{
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID:  ptr.To("dpudeployment_dpudeployment_service-1"),
							Interfaces: gotDPUServiceInterfaceNames["service-1"],
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-1"]},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
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
							ServiceID:  ptr.To("dpudeployment_dpudeployment_service-2"),
							Interfaces: gotDPUServiceInterfaceNames["service-2"],
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"newlabel2": "newvalue-service-2", "svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-2"]},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-3"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-3"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-3"), 10)),
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-3"]},
												},
											},
										},
									},
								},
							},
							DeployInCluster: ptr.To(true),
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-4"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"newlabel2": "newvalue-service-4", "svc.dpu.nvidia.com/dpudeployment-service": "service-4"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-4"), 10)),
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-4"]},
												},
											},
										},
									},
								},
							},
							DeployInCluster: ptr.To(true),
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the existing disruptive DPUService to non disruptive", func() {
				By("Creating the dependencies")
				service1VersionDigest1, service2VersionDigest1 := createReconcileDPUServicesDisruptiveDependencies(testNS.Name)
				service3VersionDigest1, service4VersionDigest1 := createReconcileDPUServicesInClusterDisruptiveDependencies(testNS.Name)
				versionDigest1ForService := map[string]string{
					"service-1": service1VersionDigest1,
					"service-2": service2VersionDigest1,
					"service-3": service3VersionDigest1,
					"service-4": service4VersionDigest1,
				}

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
				dpuDeployment.Spec.Services["service-4"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-4",
					ServiceConfiguration: "service-4",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServices to be applied")
				firstDPUServiceUIDs := make([]types.UID, 0, 4)
				gotFirstDPUServiceNames := make(map[string]string)
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(4))
					for _, dpuService := range gotDPUServiceList.Items {
						firstDPUServiceUIDs = append(firstDPUServiceUIDs, dpuService.UID)
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuService.Name, serviceName) {
								gotFirstDPUServiceNames[serviceName] = dpuService.Name
							}
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object for service-2 and service-4 services to non-disruptive and making a label change")
				modifiedServices := []string{"service-2", "service-4"}
				for _, serviceName := range modifiedServices {
					dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: serviceName}, dpuServiceConfiguration)).To(Succeed())
					dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet = &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
						Labels: map[string]string{"newlabel2": fmt.Sprintf("newvalue-%s", serviceName)},
					}
					// make non-disruptive
					dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{ApplyNodeEffect: ptr.To(false)}
					dpuServiceConfiguration.SetManagedFields(nil)
					dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
					Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.Apply, client.ForceOwnership, client.FieldOwner(dpuDeploymentControllerName))).To(Succeed())
				}

				By("checking that the version digests are updated only for service-2 and service-4")
				versionDigest2ForService := make(map[string]string)
				for serviceName, versionDigest := range versionDigest1ForService {
					dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: serviceName}, dpuServiceConfiguration)).To(Succeed())
					dpuServiceTemplate := &dpuservicev1.DPUServiceTemplate{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: serviceName}, dpuServiceTemplate)).To(Succeed())
					versionDigest2ForService[serviceName] = calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)
					if slices.Contains(modifiedServices, serviceName) {
						Expect(versionDigest).ToNot(Equal(versionDigest2ForService[serviceName]))
					} else {
						Expect(versionDigest).To(Equal(versionDigest2ForService[serviceName]))
					}
				}

				By("checking that the DPUService is updated as expected")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(4))

					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(dpuService.Labels).To(HaveLen(2))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest2ForService[serviceName]))
						// Validate that the object was not recreated
						g.Expect(firstDPUServiceUIDs).To(ContainElement(dpuService.UID))

						g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					By("checking the specs")
					specs := make([]dpuservicev1.DPUServiceSpec, 0, len(gotDPUServiceList.Items))
					for _, dpuService := range gotDPUServiceList.Items {
						specs = append(specs, dpuService.Spec)
					}
					g.Expect(specs).To(BeComparableTo([]dpuservicev1.DPUServiceSpec{
						{
							HelmChart: dpuservicev1.HelmChart{
								Source: dpuservicev1.ApplicationSource{
									RepoURL: "oci://someurl/repo",
									Path:    "somepath",
									Version: "someversion",
									Chart:   "somechart",
								},
							},
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-1"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-1"]},
												},
												{
													Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
							Interfaces: gotDPUServiceInterfaceNames["service-1"],
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-2"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"newlabel2": "newvalue-service-2", "svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-2"]},
												},
												{
													Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
							Interfaces: gotDPUServiceInterfaceNames["service-2"],
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-3"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-3"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-3"), 10)),
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-3"]},
												},
											},
										},
									},
								},
							},
							DeployInCluster: ptr.To(true),
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-4"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"newlabel2": "newvalue-service-4", "svc.dpu.nvidia.com/dpudeployment-service": "service-4"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-4"), 10)),
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotFirstDPUServiceNames["service-4"]},
												},
											},
										},
									},
								},
							},
							DeployInCluster: ptr.To(true),
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("ensuring that no new DPUServices are created or deleted")
				Consistently(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(4))
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(firstDPUServiceUIDs).To(ContainElement(dpuService.UID))
					}
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
			// TODO: Split the test for disruptive upgrade and revision history check
			It("should create new DPUService on update of disruptive DPUServiceConfiguration and respect revision history", func() {
				revisionHistoryLimit := 5
				By("Creating the dependencies")
				versionDigest1, _ := createReconcileDPUServicesDisruptiveDependencies(testNS.Name)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.RevisionHistoryLimit = ptr.To(int32(revisionHistoryLimit))
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				dpuDeployment.Spec.Services["service-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-2",
					ServiceConfiguration: "service-2",
				}
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUServices to be applied")
				firstDPUServiceUIDs := make(map[types.UID]interface{})
				names := make(map[string]string)
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))
					for _, dpuService := range gotDPUServiceList.Items {
						firstDPUServiceUIDs[dpuService.UID] = struct{}{}
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						names[serviceName] = dpuService.Name
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("capturing the created DPUServiceInterfaces")
				gotDPUServiceInterfaceNames := make(map[string][]string)
				Eventually(func(g Gomega) {
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceConfiguration object")
				expectedDPUServiceSpecs := []dpuservicev1.DPUServiceSpec{
					{
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://someurl/repo",
								Path:    "somepath",
								Version: "someversion",
								Chart:   "somechart",
							},
						},
						ServiceID: ptr.To("dpudeployment_dpudeployment_service-1"),
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
							NodeSelector: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{names["service-1"]},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
								},
							},
						},
						Interfaces: gotDPUServiceInterfaceNames["service-1"],
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
						ServiceID: ptr.To("dpudeployment_dpudeployment_service-2"),
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
							NodeSelector: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{names["service-2"]},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
								},
							},
						},
						Interfaces: gotDPUServiceInterfaceNames["service-2"],
						// paused while waiting for the new version to be ready
						Paused: ptr.To(true),
					},
				}

				// This is a hack because object creationTimeStamp use rfc3339 format
				// which has second precision, so we need to wait for 1 second to make sure
				// the creationTimeStamp is different for the first new version at least
				time.Sleep(1 * time.Second)
				var (
					versionDigest  string
					dpuServiceName string
				)
				for i := 0; i < revisionHistoryLimit; i++ {
					dpuServiceConfiguration := &dpuservicev1.DPUServiceConfiguration{}
					Expect(testClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: "service-2"}, dpuServiceConfiguration)).To(Succeed())
					initialConfig := dpuServiceConfiguration.DeepCopy()
					dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet = &dpuservicev1.DPUServiceConfigurationServiceDaemonSetValues{
						Labels: map[string]string{fmt.Sprintf("somelabel%d", i): "val"},
					}
					dpuServiceConfiguration.SetManagedFields(nil)
					dpuServiceConfiguration.SetGroupVersionKind(dpuservicev1.DPUServiceConfigurationGroupVersionKind)
					Expect(testClient.Patch(ctx, dpuServiceConfiguration, client.MergeFrom(initialConfig))).To(Succeed())
					newVersionDigest := calculateVersionDigest("service-2", testNS.Name)
					Expect(newVersionDigest).ToNot(Equal(versionDigest))
					versionDigest = newVersionDigest
					var newDPUServiceName string
					var newDPUServiceInterface string

					Eventually(func(g Gomega) {
						gotDPUServiceList := &dpuservicev1.DPUServiceList{}
						g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
							Namespace: testNS.Name,
						})).To(Succeed())
						for _, dpuService := range gotDPUServiceList.Items {
							if dpuService.GetAnnotations()[dpuServiceVersionAnnotationKey] == versionDigest {
								newDPUServiceName = dpuService.Name
							}
						}
						g.Expect(newDPUServiceName).ToNot(BeEmpty())
						gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
						g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
						for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
							if dpuServiceInterface.GetAnnotations()[dpuServiceVersionAnnotationKey] == versionDigest {
								newDPUServiceInterface = dpuServiceInterface.Name
							}
						}
						g.Expect(newDPUServiceInterface).ToNot(BeEmpty())
					}).WithTimeout(30 * time.Second).Should(Succeed())

					Expect(newDPUServiceName).ToNot(Equal(dpuServiceName))
					dpuServiceName = newDPUServiceName
					svc := dpuservicev1.DPUServiceSpec{
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://someurl/repo",
								Path:    "somepath",
								Version: "someversion",
								Chart:   "somechart",
							},
						},
						ServiceID: ptr.To("dpudeployment_dpudeployment_service-2"),
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{fmt.Sprintf("somelabel%d", i): "val", "svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
							NodeSelector: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{dpuServiceName},
											},
											{
												Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
											},
										},
									},
								},
							},
						},
						Interfaces: []string{newDPUServiceInterface},
					}

					// remove the oldest version from the expected list
					// if the list has reached the revisionHistoryLimit
					if len(expectedDPUServiceSpecs) == revisionHistoryLimit+1 {
						expectedDPUServiceSpecs = append(expectedDPUServiceSpecs[:1], expectedDPUServiceSpecs[2:]...)
					}

					expectedDPUServiceSpecs = append(expectedDPUServiceSpecs, svc)

					for i := 1; i < len(expectedDPUServiceSpecs)-1; i++ {
						// paused while waiting for the new version to be ready
						expectedDPUServiceSpecs[i].Paused = ptr.To(true)
					}

					By("checking that the DPUService is updated as expected and that we have as many DPUServiceInterfaces")
					Eventually(func(g Gomega) {
						gotDPUServiceList := &dpuservicev1.DPUServiceList{}
						g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
							Namespace: testNS.Name,
						})).To(Succeed())
						g.Expect(gotDPUServiceList.Items).To(HaveLen(len(expectedDPUServiceSpecs)))

						By("Checking that the DPUServiceInterfaces that have exceeded the revision history have been deleted")
						gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
						g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
						g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(len(expectedDPUServiceSpecs)))

						By("checking the specs")
						specs := make([]dpuservicev1.DPUServiceSpec, 0, len(gotDPUServiceList.Items))
						for _, dpuService := range gotDPUServiceList.Items {
							specs = append(specs, dpuService.Spec)
						}
						g.Expect(specs).To(ConsistOf(expectedDPUServiceSpecs))
					}).WithTimeout(30 * time.Second).Should(Succeed())
				}

				By("making the new DPUService ready")
				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
				Expect(gotDPUServiceList.Items).To(HaveLen(len(expectedDPUServiceSpecs)))

				var currentSvc *dpuservicev1.DPUService
				for _, dpuService := range gotDPUServiceList.Items {
					if dpuService.GetAnnotations()[dpuServiceVersionAnnotationKey] == versionDigest {
						currentSvc = &dpuService
					}
				}
				Expect(currentSvc).NotTo(BeNil())
				patcher := patch.NewSerialPatcher(currentSvc, testClient)
				currentSvc.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: currentSvc.Generation,
					},
				}
				Expect(patcher.Patch(ctx, currentSvc, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that old DPUServices are not deleted until DPUSet is ready")
				Consistently(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList)).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(len(expectedDPUServiceSpecs)))
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("marking the DPUSet ready")
				gotDPUSetList := &provisioningv1.DPUSetList{}
				Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					dpuSet.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuSet.Generation,
						},
					}
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					Expect(testClient.Status().Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("checking that the DPUService is updated as expected")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}

					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

					serviceUIDs := firstDPUServiceUIDs
					versions := map[string]string{
						"service-1": versionDigest1,
						"service-2": versionDigest}
					By("checking the object metadata")
					for _, dpuService := range gotDPUServiceList.Items {
						g.Expect(dpuService.Labels).To(HaveLen(2))
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
						serviceName := strings.SplitN(*dpuService.Spec.ServiceID, "_", 3)[2]
						g.Expect(dpuService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", serviceName))
						g.Expect(dpuService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versions[serviceName]))
						delete(serviceUIDs, dpuService.UID)

						g.Expect(dpuService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					}

					// Validate that all original objects are there and not recreated
					g.Expect(serviceUIDs).To(HaveLen(1))

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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-1"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{names["service-1"]},
												},
												{
													Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
							Interfaces: gotDPUServiceInterfaceNames["service-1"],
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
							ServiceID: ptr.To("dpudeployment_dpudeployment_service-2"),
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"somelabel4": "val", "svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{currentSvc.Name},
												},
												{
													Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", testNS.Name, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
							Interfaces: gotDPUServiceInterfaceNames["service-2"],
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should not create a new DPUService, when service is disruptive, on update of the .spec.dpuClusterSelector in the DPUDeployment", func() {
				By("Creating the dependencies")
				versionDigest1, _ := createReconcileDPUServicesDisruptiveDependencies(testNS.Name)

				By("Creating the DPUDeployment with initial DPUClusterSelector")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("waiting for the initial DPUService to be applied")
				var initialDPUServiceUID types.UID
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
					initialDPUServiceUID = gotDPUServiceList.Items[0].UID
					g.Expect(gotDPUServiceList.Items[0].Annotations).To(HaveKeyWithValue(dpuServiceVersionAnnotationKey, versionDigest1))
					g.Expect(gotDPUServiceList.Items[0].Spec.DPUClusterSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-west"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUClusterSelector in the DPUDeployment by updating DPUSets")
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
					{
						NameSuffix: "set2",
						DPUClusterSelector: map[string]string{
							"region": "us-east",
						},
					},
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that the DPUService is NOT recreated but updated in place")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))

					dpuService := gotDPUServiceList.Items[0]
					g.Expect(dpuService.UID).To(Equal(initialDPUServiceUID))
					g.Expect(dpuService.Annotations).To(HaveKeyWithValue(dpuServiceVersionAnnotationKey, versionDigest1))

					By("verifying the DPUClusterSelector is updated with aggregated values")
					g.Expect(dpuService.Spec.DPUClusterSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-east", "us-west"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should delete DPUServices that are no longer part of the DPUDeployment", func() {
				By("Creating the dependencies")
				_, versionDigest2 := createReconcileDPUServicesNonDisruptiveDependencies(testNS.Name)

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
				var gotDPUService *dpuservicev1.DPUService
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))
					for _, dpuService := range gotDPUServiceList.Items {
						if dpuService.GetAnnotations()[dpuServiceVersionAnnotationKey] == versionDigest2 {
							gotDPUService = &dpuService
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				delete(dpuDeployment.Spec.Services, "service-1")
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("Marking the DPUService ready")
				patcher = patch.NewSerialPatcher(gotDPUService, testClient)
				gotDPUService.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: gotDPUService.Generation,
					},
				}
				Expect(patcher.Patch(ctx, gotDPUService, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that only one DPUService exists")
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(1))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}

					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
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
						ServiceID:  ptr.To("dpudeployment_dpudeployment_service-2"),
						Interfaces: gotDPUServiceInterfaceNames["service-2"],
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
							NodeSelector: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{gotDPUService.Name},
											},
											{
												Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServices on update of the .spec.services in the DPUDeployment", func() {
				By("Creating the dependencies")
				createReconcileDPUServicesNonDisruptiveDependencies(testNS.Name)

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
				var gotInitialDPUServiceName string
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
					gotInitialDPUServiceName = gotDPUServiceList.Items[0].Name
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment object and checking the outcome")
				dpuDeployment.Spec.Services["service-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-2",
					ServiceConfiguration: "service-2",
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that two DPUServices exist")
				var gotDPUServiceName string
				Eventually(func(g Gomega) {
					By("capturing the created DPUServiceInterfaces")
					gotDPUServiceInterfaceNames := make(map[string][]string)
					gotDPUServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceInterfaceList)).To(Succeed())
					g.Expect(gotDPUServiceInterfaceList.Items).To(HaveLen(2))
					for _, dpuServiceInterface := range gotDPUServiceInterfaceList.Items {
						for serviceName := range dpuDeployment.Spec.Services {
							if strings.Contains(dpuServiceInterface.Name, serviceName) {
								gotDPUServiceInterfaceNames[serviceName] = append(gotDPUServiceInterfaceNames[serviceName], dpuServiceInterface.Name)
								slices.Sort(gotDPUServiceInterfaceNames[serviceName])
							}
						}
					}

					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(2))

					for _, dpuService := range gotDPUServiceList.Items {
						if dpuService.Name != gotInitialDPUServiceName {
							gotDPUServiceName = dpuService.Name
							break
						}
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
							},
							ServiceID:  ptr.To("dpudeployment_dpudeployment_service-1"),
							Interfaces: gotDPUServiceInterfaceNames["service-1"],
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-1-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotInitialDPUServiceName},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
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
							ServiceID:  ptr.To("dpudeployment_dpudeployment_service-2"),
							Interfaces: gotDPUServiceInterfaceNames["service-2"],
							ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
								Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-2"},
								NodeSelector: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "svc.dpu.nvidia.com/dpuservice-service-2-version",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{gotDPUServiceName},
												},
												{
													Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{fmt.Sprintf("%s_%s", dpuDeployment.Namespace, dpuDeployment.Name)},
												},
											},
										},
									},
								},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create new DPUServices on manual deletion of the DPUServices", func() {
				By("Creating the dependencies")
				createReconcileDPUServicesNonDisruptiveDependencies(testNS.Name)

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
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("manually deleting the DPUServices")
				Consistently(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					if len(gotDPUServiceList.Items) == 0 {
						return
					}
					g.Expect(testutils.CleanupAndWait(ctx, testClient, &gotDPUServiceList.Items[0])).To(Succeed())
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("checking that the DPUServices are created")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should create in-cluster DPUServices with NodeSelectors of the DPUSet", func() {
				By("Creating the dependencies")
				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				dpuServiceConfiguration.Name = "service-1"
				dpuServiceConfiguration.Spec.DeploymentServiceName = "service-1"
				dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To(true)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := getMinimalDPUServiceTemplate(testNS.Name)
				dpuServiceTemplate.Name = "service-1"
				dpuServiceTemplate.Spec.DeploymentServiceName = "service-1"
				Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
				patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
				versionDigest := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.Services = make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
					},
					{
						NameSuffix: "dpuset2",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey2": "nodevalue2",
							},
						},
					},
				}
				dpuDeployment.Spec.Services["service-1"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "service-1",
					ServiceConfiguration: "service-1",
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that correct DPUServices are created")
				Eventually(func(g Gomega) {
					gotDPUServiceList := &dpuservicev1.DPUServiceList{}
					g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
						Namespace: testNS.Name,
					})).To(Succeed())
					g.Expect(gotDPUServiceList.Items).To(HaveLen(1))
					gotDPUService := gotDPUServiceList.Items[0]

					By("checking the object metadata")
					g.Expect(gotDPUService.Labels).To(HaveLen(2))
					g.Expect(gotDPUService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					g.Expect(gotDPUService.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpudeployment-service", "service-1"))
					g.Expect(gotDPUService.Annotations).To(HaveKeyWithValue("svc.dpu.nvidia.com/dpuservice-version", versionDigest))
					g.Expect(gotDPUService.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

					By("checking the specs")
					g.Expect(gotDPUService.Spec).To(BeComparableTo(dpuservicev1.DPUServiceSpec{
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://someurl/repo",
								Path:    "somepath",
								Version: "someversion",
								Chart:   "somechart",
							},
						},
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{"svc.dpu.nvidia.com/dpudeployment-service": "service-1"},
							NodeSelector: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-1"), 10)),
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{gotDPUServiceList.Items[0].Name},
											},
											{
												Key:      "nodekey1",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"nodevalue1"},
											},
										},
									},
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      fmt.Sprintf("svc.dpu.nvidia.com/dpuservice-in-cluster-version-%s", digest.Short(digest.FromObjects(client.ObjectKeyFromObject(dpuDeployment), "service-1"), 10)),
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{gotDPUServiceList.Items[0].Name},
											},
											{
												Key:      "nodekey2",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"nodevalue2"},
											},
										},
									},
								},
							},
						},
						ServiceID:       ptr.To("dpudeployment_dpudeployment_service-1"),
						DeployInCluster: ptr.To(true),
					}))
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

		Context("When checking verifyVersionMatching()", func() {
			DescribeTable("behaves as expected", func(deps *dpuDeploymentDependencies, expectError bool) {
				err := verifyVersionMatching(deps)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
				Entry("DPUServiceTemplates have no version constraint", &dpuDeploymentDependencies{
					BFB: &provisioningv1.BFB{
						Status: provisioningv1.BFBStatus{
							Versions: provisioningv1.BFBVersions{
								DOCA: "2.9.1",
							},
						},
					},

					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Status: dpuservicev1.DPUServiceTemplateStatus{},
						},
						"service-2": {
							Status: dpuservicev1.DPUServiceTemplateStatus{},
						},
					},
				}, false),
				Entry("DPUServiceTemplates have valid version constraints", &dpuDeploymentDependencies{
					BFB: &provisioningv1.BFB{
						Status: provisioningv1.BFBStatus{
							Versions: provisioningv1.BFBVersions{
								DOCA: "2.9.1",
							},
						},
					},

					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.8",
								},
							},
						},
						"service-2": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.5",
								},
							},
						},
					},
				}, false),
				Entry("DPUServiceTemplate has version constraints that are not satisfied", &dpuDeploymentDependencies{
					BFB: &provisioningv1.BFB{
						Status: provisioningv1.BFBStatus{
							Versions: provisioningv1.BFBVersions{
								DOCA: "2.9.1",
							},
						},
					},

					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.10",
								},
							},
						},
						"service-2": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.5",
								},
							},
						},
					},
				}, true),
				Entry("BFB has invalid version", &dpuDeploymentDependencies{
					BFB: &provisioningv1.BFB{
						Status: provisioningv1.BFBStatus{
							Versions: provisioningv1.BFBVersions{
								DOCA: "blabla",
							},
						},
					},

					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.8",
								},
							},
						},
						"service-2": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.5",
								},
							},
						},
					},
				}, true),
				Entry("DPUServiceTemplates have version constraints for a type of version that is unsupported", &dpuDeploymentDependencies{
					BFB: &provisioningv1.BFB{
						Status: provisioningv1.BFBStatus{
							Versions: provisioningv1.BFBVersions{
								DOCA: "2.9.1",
							},
						},
					},

					DPUServiceTemplates: map[string]*dpuservicev1.DPUServiceTemplate{
						"service-1": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/bsp-version": ">=2.10",
								},
							},
						},
						"service-2": {
							Status: dpuservicev1.DPUServiceTemplateStatus{
								Versions: map[string]string{
									"dpu.nvidia.com/doca-version": ">=2.5",
								},
							},
						},
					},
				}, false),
			)
		})

		Context("When checking reconcileDPUServiceChains()", func() {
			var initialServiceChainsSettings *dpuservicev1.ServiceChains
			BeforeEach(func() {
				initialServiceChainsSettings = &dpuservicev1.ServiceChains{
					UpgradePolicy: dpuservicev1.UpgradePolicy{
						ApplyNodeEffect: ptr.To(false),
					},
					Switches: []dpuservicev1.DPUDeploymentSwitch{
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
				}

				By("Creating the dependencies")
				bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

				dpuFlavor := getMinimalDPUFlavor(testNS.Name)
				Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

				dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
				Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

				dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

				DeferCleanup(cleanDPUDeploymentDerivatives, testNS.Name)
			})
			It("should create the correct DPUServiceChain", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
					Switches: []dpuservicev1.DPUDeploymentSwitch{
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
										InterfaceName: "otherinterface",
										Name:          "someotherservice",
									},
								},
							},
							ServiceMTU: ptr.To(3000),
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
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("checking that correct DPUServiceChain is created")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					By("checking the object metadata")
					gotDPUServiceChain := gotDPUServiceChainList.Items[0]

					g.Expect(gotDPUServiceChain.Labels).To(HaveLen(1))
					g.Expect(gotDPUServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					g.Expect(gotDPUServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest))
					g.Expect(gotDPUServiceChain.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

					By("checking the spec")
					g.Expect(gotDPUServiceChain.Spec).To(BeComparableTo(dpuservicev1.DPUServiceChainSpec{
						Template: dpuservicev1.ServiceChainSetSpecTemplate{
							Spec: dpuservicev1.ServiceChainSetSpec{
								NodeSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      dpuServiceChainVersionLabelAnnotationKey,
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{gotDPUServiceChain.Name},
										},
										{
											Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
										},
									},
								},
								Template: dpuservicev1.ServiceChainSpecTemplate{
									Spec: dpuservicev1.ServiceChainSpec{
										Switches: []dpuservicev1.Switch{
											{
												ServiceMTU: ptr.To(1500),
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
												ServiceMTU: ptr.To(3000),
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someotherservice",
																ServiceInterfaceInterfaceNameLabel: "otherinterface",
															},
														},
													},
												},
											},
											{
												ServiceMTU: ptr.To(1500),
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

			It("should create the correct DPUServiceChain when DPUDeployment specifies multiple DPUSets", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
					{
						NameSuffix: "set2",
						DPUClusterSelector: map[string]string{
							"region": "us-east",
						},
					},
				}
				dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
					Switches: []dpuservicev1.DPUDeploymentSwitch{
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
										InterfaceName: "otherinterface",
										Name:          "someotherservice",
									},
								},
							},
							ServiceMTU: ptr.To(3000),
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
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("checking that correct DPUServiceChain is created")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					By("checking the object metadata")
					gotDPUServiceChain := gotDPUServiceChainList.Items[0]

					g.Expect(gotDPUServiceChain.Labels).To(HaveLen(1))
					g.Expect(gotDPUServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					g.Expect(gotDPUServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest))
					g.Expect(gotDPUServiceChain.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

					By("checking the spec")
					g.Expect(gotDPUServiceChain.Spec).To(BeComparableTo(dpuservicev1.DPUServiceChainSpec{
						DPUClusterSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "region",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"us-east", "us-west"},
								},
							},
						},
						Template: dpuservicev1.ServiceChainSetSpecTemplate{
							Spec: dpuservicev1.ServiceChainSetSpec{
								NodeSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      dpuServiceChainVersionLabelAnnotationKey,
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{gotDPUServiceChain.Name},
										},
										{
											Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
										},
									},
								},
								Template: dpuservicev1.ServiceChainSpecTemplate{
									Spec: dpuservicev1.ServiceChainSpec{
										Switches: []dpuservicev1.Switch{
											{
												ServiceMTU: ptr.To(1500),
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
												ServiceMTU: ptr.To(3000),
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someotherservice",
																ServiceInterfaceInterfaceNameLabel: "otherinterface",
															},
														},
													},
												},
											},
											{
												ServiceMTU: ptr.To(1500),
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

			It("should not create the DPUServiceChain if none specified", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that DPUServiceChainReconciled condition is successful and that no DPUServiceChain created")
				Eventually(func(g Gomega) {
					gotDPUDeployment := &dpuservicev1.DPUDeployment{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), gotDPUDeployment)).To(Succeed())
					g.Expect(gotDPUDeployment.Status.Conditions).To(ContainElement(
						And(
							HaveField("Type", string(dpuservicev1.ConditionDPUServiceChainsReconciled)),
							HaveField("Status", metav1.ConditionTrue),
							HaveField("Reason", string(conditions.ReasonSuccess)),
						),
					))

					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should delete the DPUServiceChain if DPUDeployment is updated with no serviceChain specified", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)

				By("checking that the DPUServiceChain is created")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUDeployment to not include a serviceChain anymore")
				dpuDeployment.Spec.ServiceChains = nil
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				By("checking that no DPUServiceChain exists")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(BeEmpty())
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should patch a manually modified DPUServiceChain as long as the modification is not on the version annotation", func() {
				By("Creating the DPUDeployment")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				By("checking that the DPUServiceChain is created")
				var gotDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
					gotDPUServiceChain = &gotDPUServiceChainList.Items[0]

					By("checking the original value of the switches")
					g.Expect(gotDPUServiceChain.Spec.Template.Spec.Template.Spec.Switches).To(BeComparableTo([]dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(1500),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											"svc.dpu.nvidia.com/service":   "dpudeployment_dpudeployment_someservice",
											"svc.dpu.nvidia.com/interface": "someinterface",
										},
									},
								},
							},
						},
					}))

					By("checking the nodeSelector")
					g.Expect(gotDPUServiceChain.Spec.Template.Spec.NodeSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      dpuServiceChainVersionLabelAnnotationKey,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{gotDPUServiceChain.Name},
							},
							{
								Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUServiceChain manually")
				gotDPUServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{
										"somelabel": "somevalue",
									},
								},
							},
						},
					},
				}
				gotDPUServiceChain.SetManagedFields(nil)
				gotDPUServiceChain.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
				Expect(testClient.Patch(ctx, gotDPUServiceChain, client.Apply, client.ForceOwnership, client.FieldOwner("some-test-controller"))).To(Succeed())

				By("checking that the DPUServiceChain is reverted to the original")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
					g.Expect(gotDPUServiceChainList.Items[0].UID).To(Equal(gotDPUServiceChain.UID))

					By("checking the value of the switches")
					g.Expect(gotDPUServiceChainList.Items[0].Spec.Template.Spec.Template.Spec.Switches).To(BeComparableTo([]dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(1500),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											"svc.dpu.nvidia.com/service":   "dpudeployment_dpudeployment_someservice",
											"svc.dpu.nvidia.com/interface": "someinterface",
										},
									},
								},
							},
						},
					}))

					By("checking the nodeSelector")
					g.Expect(gotDPUServiceChainList.Items[0].Spec.Template.Spec.NodeSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      dpuServiceChainVersionLabelAnnotationKey,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{gotDPUServiceChain.Name},
							},
							{
								Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the disruptive DPUServiceChain on update of dpuDeployment.Spec.ServiceChains.Switches", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				// make the DPUServiceChain disruptive
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				dpuDeployment.Spec.ServiceChains.UpgradePolicy.ApplyNodeEffect = ptr.To(true)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "dpuset1",
						DPUNodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodekey1": "nodevalue1",
							},
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("waiting for the initial DPUServiceChains to be applied")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying dpuDeployment.Spec.ServiceChains.Switches")
				dpuDeployment.Spec.ServiceChains.Switches = []dpuservicev1.DPUDeploymentSwitch{
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
									InterfaceName: "otherinterface",
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

				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				// We need to get the object to calculate the digest taking into account the defaults added by the API server
				gotDPUDeployment := &dpuservicev1.DPUDeployment{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), gotDPUDeployment)).To(Succeed())
				chainDigest2 := calculateDPUServiceChainVersionDigest(gotDPUDeployment.Spec.ServiceChains.Switches)
				Expect(chainDigest2).NotTo(Equal(chainDigest))

				versions := []string{chainDigest, chainDigest2}

				By("checking that the DPUServiceChain is updated as expected")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(2))

					By("checking the object metadata")
					obj := gotDPUServiceChainList.Items[0]

					g.Expect(obj.Labels).To(HaveLen(1))
					g.Expect(obj.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					matchCount := 0
					for _, obj := range gotDPUServiceChainList.Items {
						for _, version := range versions {
							if obj.GetAnnotations()[dpuServiceChainVersionLabelAnnotationKey] == version {
								matchCount++
							}
						}
					}
					// expect the two versions to be present
					g.Expect(matchCount).To(Equal(len(versions)))
					g.Expect(obj.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("making the new version ready")
				gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
				Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
				Expect(gotDPUServiceChainList.Items).To(HaveLen(2))

				var currentSvcChain *dpuservicev1.DPUServiceChain
				for _, dpuServiceChain := range gotDPUServiceChainList.Items {
					if dpuServiceChain.GetAnnotations()[dpuServiceChainVersionLabelAnnotationKey] == chainDigest2 {
						currentSvcChain = &dpuServiceChain
					}
				}
				Expect(currentSvcChain).NotTo(BeNil())
				patcher = patch.NewSerialPatcher(currentSvcChain, testClient)
				currentSvcChain.Status.Conditions = []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             metav1.ConditionTrue,
						Reason:             string(conditions.ReasonSuccess),
						LastTransitionTime: metav1.NewTime(time.Now()),
						ObservedGeneration: currentSvcChain.Generation,
					},
				}
				Expect(patcher.Patch(ctx, currentSvcChain, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that old DPUServiceChains are not deleted until DPUSet is ready")
				Consistently(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					Expect(gotDPUServiceChainList.Items).To(HaveLen(2))
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("marking the DPUSet ready")
				gotDPUSetList := &provisioningv1.DPUSetList{}
				Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					dpuSet.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuSet.Generation,
						},
					}
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					Expect(testClient.Status().Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				By("checking that the DPUServiceChain is updated as expected")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					By("checking the object metadata")
					gotDPUServiceChain := gotDPUServiceChainList.Items[0]

					g.Expect(gotDPUServiceChain.Labels).To(HaveLen(1))
					g.Expect(gotDPUServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					g.Expect(gotDPUServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest2))
					g.Expect(gotDPUServiceChain.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

					By("checking the spec")
					g.Expect(gotDPUServiceChain.Spec).To(BeComparableTo(dpuservicev1.DPUServiceChainSpec{
						Template: dpuservicev1.ServiceChainSetSpecTemplate{
							Spec: dpuservicev1.ServiceChainSetSpec{
								NodeSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      dpuServiceChainVersionLabelAnnotationKey,
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{gotDPUServiceChain.Name},
										},
										{
											Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
										},
									},
								},
								Template: dpuservicev1.ServiceChainSpecTemplate{
									Spec: dpuservicev1.ServiceChainSpec{
										Switches: []dpuservicev1.Switch{
											{
												ServiceMTU: ptr.To(1500),
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
												ServiceMTU: ptr.To(1500),
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_someotherservice",
																ServiceInterfaceInterfaceNameLabel: "otherinterface",
															},
														},
													},
												},
											},
											{
												ServiceMTU: ptr.To(1500),
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
			It("should not create new DPUServiceChain, when chain is disruptive, on update of the .spec.dpuClusterSelector in the DPUDeployment", func() {
				By("Creating the DPUDeployment with initial DPUClusterSelector")
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				dpuDeployment.Spec.ServiceChains.UpgradePolicy.ApplyNodeEffect = ptr.To(true)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
				}
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("waiting for the initial DPUServiceChain to be applied")
				var initialDPUServiceChainUID types.UID
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := getDPUServiceChainList()
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
					initialDPUServiceChainUID = gotDPUServiceChainList.Items[0].UID
					g.Expect(gotDPUServiceChainList.Items[0].Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest))
					g.Expect(gotDPUServiceChainList.Items[0].Spec.DPUClusterSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-west"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying the DPUClusterSelector in the DPUDeployment by updating DPUSets")
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
					{
						NameSuffix: "set1",
						DPUClusterSelector: map[string]string{
							"region": "us-west",
						},
					},
					{
						NameSuffix: "set2",
						DPUClusterSelector: map[string]string{
							"region": "us-east",
						},
					},
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner("test"))).To(Succeed())

				By("checking that the DPUServiceChain is NOT recreated but updated in place")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := getDPUServiceChainList()
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					dpuServiceChain := gotDPUServiceChainList.Items[0]
					g.Expect(dpuServiceChain.UID).To(Equal(initialDPUServiceChainUID))
					g.Expect(dpuServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest))

					By("verifying the DPUClusterSelector is updated with aggregated values")
					g.Expect(dpuServiceChain.Spec.DPUClusterSelector).To(BeComparableTo(&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "region",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-east", "us-west"},
							},
						},
					}))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})
			It("should update the disruptive DPUServiceChain to non-diruptive", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				// make the DPUServiceChain disruptive
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				dpuDeployment.Spec.ServiceChains.UpgradePolicy.ApplyNodeEffect = ptr.To(true)
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("waiting for the initial DPUServiceChains to be applied")
				var firstDPUServiceChain *dpuservicev1.DPUServiceChain
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := getDPUServiceChainList()
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
					firstDPUServiceChain = &gotDPUServiceChainList.Items[0]
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying dpuDeployment.Spec.ServiceChains.Switches")
				dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
					// make the DPUServiceChain non-disruptive
					UpgradePolicy: dpuservicev1.UpgradePolicy{
						ApplyNodeEffect: ptr.To(false),
					},
					Switches: []dpuservicev1.DPUDeploymentSwitch{
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
										InterfaceName: "otherinterface",
										Name:          "otherservice",
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
					},
				}
				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				// We need to get the object to calculate the digest taking into account the defaults added by the API server
				gotDPUDeployment := &dpuservicev1.DPUDeployment{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), gotDPUDeployment)).To(Succeed())
				chainDigest2 := calculateDPUServiceChainVersionDigest(gotDPUDeployment.Spec.ServiceChains.Switches)
				Expect(chainDigest2).NotTo(Equal(chainDigest))

				By("checking that the DPUServiceChain is updated as expected")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					By("checking the object metadata")
					gotDPUServiceChain := gotDPUServiceChainList.Items[0]

					g.Expect(gotDPUServiceChain.Labels).To(HaveLen(1))
					g.Expect(gotDPUServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					g.Expect(gotDPUServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest2))
					g.Expect(gotDPUServiceChain.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))

					By("checking the spec")
					g.Expect(gotDPUServiceChain.Spec).To(BeComparableTo(dpuservicev1.DPUServiceChainSpec{
						Template: dpuservicev1.ServiceChainSetSpecTemplate{
							Spec: dpuservicev1.ServiceChainSetSpec{
								NodeSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      dpuServiceChainVersionLabelAnnotationKey,
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{firstDPUServiceChain.Name},
										},
										{
											Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
										},
									},
								},
								Template: dpuservicev1.ServiceChainSpecTemplate{
									Spec: dpuservicev1.ServiceChainSpec{
										Switches: []dpuservicev1.Switch{
											{
												ServiceMTU: ptr.To(1500),
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
												ServiceMTU: ptr.To(1500),
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_otherservice",
																ServiceInterfaceInterfaceNameLabel: "otherinterface",
															},
														},
													},
												},
											},
											{
												ServiceMTU: ptr.To(1500),
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
			It("should update the DPUServiceChain on update of dpuDeployment.Spec.ServiceChains.Switches", func() {
				dpuDeployment := getMinimalDPUDeployment(testNS.Name)
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
				Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
				DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)
				patcher := patch.NewSerialPatcher(dpuDeployment, testClient)
				chainDigest := calculateDPUServiceChainVersionDigest(dpuDeployment.Spec.ServiceChains.Switches)

				By("waiting for the initial DPUServiceChains to be applied")
				var dpuServiceChainUID types.UID
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))
					for _, dpuServiceChain := range gotDPUServiceChainList.Items {
						dpuServiceChainUID = dpuServiceChain.UID
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())

				By("modifying dpuDeployment.Spec.ServiceChains.Switches")
				dpuDeployment.Spec.ServiceChains.Switches = []dpuservicev1.DPUDeploymentSwitch{
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
									InterfaceName: "otherinterface",
									Name:          "otherservice",
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

				Expect(patcher.Patch(ctx, dpuDeployment, patch.WithFieldOwner(dpuDeploymentControllerName))).To(Succeed())

				// We need to get the object to calculate the digest taking into account the defaults added by the API server
				gotDPUDeployment := &dpuservicev1.DPUDeployment{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuDeployment), gotDPUDeployment)).To(Succeed())
				chainDigest2 := calculateDPUServiceChainVersionDigest(gotDPUDeployment.Spec.ServiceChains.Switches)
				Expect(chainDigest2).NotTo(Equal(chainDigest))

				By("checking that the DPUServiceChain is updated as expected")
				Eventually(func(g Gomega) {
					gotDPUServiceChainList := &dpuservicev1.DPUServiceChainList{}
					g.Expect(testClient.List(ctx, gotDPUServiceChainList)).To(Succeed())
					g.Expect(gotDPUServiceChainList.Items).To(HaveLen(1))

					By("checking the object metadata")
					gotDPUServiceChain := gotDPUServiceChainList.Items[0]

					g.Expect(gotDPUServiceChain.Labels).To(HaveLen(1))
					g.Expect(gotDPUServiceChain.Labels).To(HaveKeyWithValue("svc.dpu.nvidia.com/owned-by-dpudeployment", fmt.Sprintf("%s_dpudeployment", testNS.Name)))
					g.Expect(gotDPUServiceChain.Annotations).To(HaveKeyWithValue(dpuServiceChainVersionLabelAnnotationKey, chainDigest2))
					g.Expect(gotDPUServiceChain.OwnerReferences).To(ConsistOf(*metav1.NewControllerRef(dpuDeployment, dpuservicev1.DPUDeploymentGroupVersionKind)))
					g.Expect(dpuServiceChainUID).To(Equal(gotDPUServiceChain.UID))

					By("checking the spec")
					g.Expect(gotDPUServiceChain.Spec).To(BeComparableTo(dpuservicev1.DPUServiceChainSpec{
						Template: dpuservicev1.ServiceChainSetSpecTemplate{
							Spec: dpuservicev1.ServiceChainSetSpec{
								NodeSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      dpuServiceChainVersionLabelAnnotationKey,
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{gotDPUServiceChain.Name},
										},
										{
											Key:      "svc.dpu.nvidia.com/owned-by-dpudeployment",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{fmt.Sprintf("%s_dpudeployment", testNS.Name)},
										},
									},
								},
								Template: dpuservicev1.ServiceChainSpecTemplate{
									Spec: dpuservicev1.ServiceChainSpec{
										Switches: []dpuservicev1.Switch{
											{
												ServiceMTU: ptr.To(1500),
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
												ServiceMTU: ptr.To(1500),
												Ports: []dpuservicev1.Port{
													{
														ServiceInterface: dpuservicev1.ServiceIfc{
															MatchLabels: map[string]string{
																dpuservicev1.DPFServiceIDLabelKey:  "dpudeployment_dpudeployment_otherservice",
																ServiceInterfaceInterfaceNameLabel: "otherinterface",
															},
														},
													},
												},
											},
											{
												ServiceMTU: ptr.To(1500),
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
				dpuDeployment.Spec.ServiceChains = initialServiceChainsSettings
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
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())

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
					HaveField("Type", string(dpuservicev1.ConditionVersionMatchingReady)),
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
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "some",
				},
			}
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
				Switches: []dpuservicev1.DPUDeploymentSwitch{
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
			}
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
					HaveField("Type", string(dpuservicev1.ConditionVersionMatchingReady)),
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
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUSetsReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
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
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "some",
				},
			}
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
				Switches: []dpuservicev1.DPUDeploymentSwitch{
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
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDeployment)

			By("Updating the status of the underlying dependencies")
			Eventually(func(g Gomega) {
				gotDPUServiceList := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
				g.Expect(gotDPUServiceList.Items).ToNot(BeEmpty())
				for _, dpuService := range gotDPUServiceList.Items {
					dpuService.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuService.Generation,
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
							ObservedGeneration: dpuServiceChain.Generation,
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
							ObservedGeneration: dpuServiceInterface.Generation,
						},
					}
					dpuServiceInterface.SetGroupVersionKind(dpuservicev1.DPUServiceInterfaceGroupVersionKind)
					dpuServiceInterface.SetManagedFields(nil)
					g.Expect(testClient.Status().Patch(ctx, &dpuServiceInterface, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
				}

				gotDPUSetList := &provisioningv1.DPUSetList{}
				g.Expect(testClient.List(ctx, gotDPUSetList)).To(Succeed())
				g.Expect(gotDPUSetList.Items).ToNot(BeEmpty())
				for _, dpuSet := range gotDPUSetList.Items {
					dpuSet.Status.Conditions = []metav1.Condition{
						{
							Type:               string(conditions.TypeReady),
							Status:             metav1.ConditionTrue,
							Reason:             string(conditions.ReasonSuccess),
							LastTransitionTime: metav1.NewTime(time.Now()),
							ObservedGeneration: dpuSet.Generation,
						},
					}
					dpuSet.SetGroupVersionKind(provisioningv1.DPUSetGroupVersionKind)
					dpuSet.SetManagedFields(nil)
					g.Expect(testClient.Status().Patch(ctx, &dpuSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
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
					HaveField("Type", string(dpuservicev1.ConditionVersionMatchingReady)),
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
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
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
			patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

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
		It("DPUDeployment has condition VersionMatchingReady with Error Reason at the end of first reconciliation loop that failed on dependencies", func() {
			By("Creating the dependencies")
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
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
			dpuServiceTemplate.Status.Versions = make(map[string]string)
			dpuServiceTemplate.Status.Versions["dpu.nvidia.com/doca-version"] = ">=2.10"
			patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

			By("Creating the DPUDeployment")
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
						HaveField("Type", string(dpuservicev1.ConditionVersionMatchingReady)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionVersionMatchingReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonError)),
				),
			))
		})
		It("DPUDeployment has condition Deleting with AwaitingDeletion Reason when there are still objects in the cluster", func() {
			By("Creating the dependencies")
			bfb := createMinimalBFBWithStatus("somebfb", testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, bfb)

			dpuFlavor := getMinimalDPUFlavor(testNS.Name)
			Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuFlavor)

			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

			dpuServiceTemplate := createMinimalDPUServiceTemplateWithStatus(testNS.Name)
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
			objs := make(map[client.Object]interface{})

			By("Creating the DPUDeployment")
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey1": "dpuvalue1",
						},
					},
					DPUAnnotations: map[string]string{
						"annotationkey1": "annotationvalue1",
					},
				},
			}
			dpuDeployment.Spec.ServiceChains = &dpuservicev1.ServiceChains{
				Switches: []dpuservicev1.DPUDeploymentSwitch{
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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
				g.Expect(testClient.List(ctx, gotDPUServiceList, &client.ListOptions{
					Namespace: testNS.Name,
				})).To(Succeed())
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
		},
	}
}

func createMinimalBFBWithStatus(name, namespace string) *provisioningv1.BFB {
	bfb := getMinimalBFB(name, namespace)
	Expect(testClient.Create(ctx, bfb)).To(Succeed())
	bfb.Status.Phase = provisioningv1.BFBReady
	bfb.Status.Versions = provisioningv1.BFBVersions{DOCA: "2.9.1"}
	bfb.SetGroupVersionKind(provisioningv1.BFBGroupVersionKind)
	bfb.SetManagedFields(nil)
	Expect(testClient.Status().Patch(ctx, bfb, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
	return bfb
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

func createMinimalDPUServiceTemplateWithStatus(namespace string) *dpuservicev1.DPUServiceTemplate {
	dpuServiceTemplate := getMinimalDPUServiceTemplate(namespace)
	Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
	patchDPUServiceTemplateWithStatus(dpuServiceTemplate)
	return dpuServiceTemplate
}

func patchDPUServiceTemplateWithStatus(dpuServiceTemplate *dpuservicev1.DPUServiceTemplate) {
	dpuServiceTemplate.Status.Conditions = []metav1.Condition{
		{
			Type:               string(conditions.TypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             string(conditions.ReasonSuccess),
			LastTransitionTime: metav1.NewTime(time.Now()),
			ObservedGeneration: dpuServiceTemplate.Generation,
		},
	}
	dpuServiceTemplate.Status.ObservedGeneration = dpuServiceTemplate.Generation
	dpuServiceTemplate.SetGroupVersionKind(dpuservicev1.DPUServiceTemplateGroupVersionKind)
	dpuServiceTemplate.SetManagedFields(nil)
	Expect(testClient.Status().Patch(ctx, dpuServiceTemplate, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
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

// createReconcileDPUServicesNonDisruptiveDependencies creates 2 sets of dependencies that are used for the majority
// of the reconcileDPUServices tests. These are for non-in-cluster, non-disruptive services.
func createReconcileDPUServicesNonDisruptiveDependencies(namespace string) (string, string) {
	dpuServiceConfiguration, dpuServiceTemplate := createDPUServicesDependencies(namespace, "service-1", false, false)
	versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	dpuServiceConfiguration, dpuServiceTemplate = createDPUServicesDependencies(namespace, "service-2", false, false)
	versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	return versionDigest1, versionDigest2
}

// createReconcileDPUServicesInClusterNonDisruptiveDependencies creates 2 sets of dependencies that are used for the
// majority of the reconcileDPUServices tests. These are for in-cluster, non-disruptive services.
func createReconcileDPUServicesInClusterNonDisruptiveDependencies(namespace string) (string, string) {
	dpuServiceConfiguration, dpuServiceTemplate := createDPUServicesDependencies(namespace, "service-3", true, false)
	versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	dpuServiceConfiguration, dpuServiceTemplate = createDPUServicesDependencies(namespace, "service-4", true, false)
	versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	return versionDigest1, versionDigest2
}

// createReconcileDPUServicesDisruptiveDependencies creates 2 sets of dependencies that are used for the majority
// of the reconcileDPUServices tests. These are for non-in-cluster, disruptive services.
func createReconcileDPUServicesDisruptiveDependencies(namespace string) (string, string) {
	dpuServiceConfiguration, dpuServiceTemplate := createDPUServicesDependencies(namespace, "service-1", false, true)
	versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	dpuServiceConfiguration, dpuServiceTemplate = createDPUServicesDependencies(namespace, "service-2", false, true)
	versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	return versionDigest1, versionDigest2
}

// createReconcileDPUServicesInClusterDisruptiveDependencies creates 2 sets of dependencies that are used for the majority
// of the reconcileDPUServices tests. These are for in-cluster, disruptive services.
func createReconcileDPUServicesInClusterDisruptiveDependencies(namespace string) (string, string) {
	dpuServiceConfiguration, dpuServiceTemplate := createDPUServicesDependencies(namespace, "service-3", true, true)
	versionDigest1 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	dpuServiceConfiguration, dpuServiceTemplate = createDPUServicesDependencies(namespace, "service-4", true, true)
	versionDigest2 := calculateDPUServiceVersionDigest(dpuServiceConfiguration, dpuServiceTemplate)

	return versionDigest1, versionDigest2
}

func createDPUServicesDependencies(namespace, name string, isInCluster bool, isDisruptive bool) (*dpuservicev1.DPUServiceConfiguration, *dpuservicev1.DPUServiceTemplate) {
	dpuServiceConfiguration := getMinimalDPUServiceConfiguration(namespace)
	dpuServiceConfiguration.Name = name
	dpuServiceConfiguration.Spec.DeploymentServiceName = name
	dpuServiceConfiguration.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "someinterface", Network: "somenad"}}
	if !isDisruptive {
		dpuServiceConfiguration.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{ApplyNodeEffect: ptr.To(false)}
	}
	if isInCluster {
		dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = ptr.To(true)
		dpuServiceConfiguration.Spec.Interfaces = nil
	}
	Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
	DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceConfiguration)

	dpuServiceTemplate := getMinimalDPUServiceTemplate(namespace)
	dpuServiceTemplate.Name = name
	dpuServiceTemplate.Spec.DeploymentServiceName = name
	Expect(testClient.Create(ctx, dpuServiceTemplate)).To(Succeed())
	DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceTemplate)
	patchDPUServiceTemplateWithStatus(dpuServiceTemplate)

	return dpuServiceConfiguration, dpuServiceTemplate
}

func calculateVersionDigest(name, namespace string) string {
	config := &dpuservicev1.DPUServiceConfiguration{}
	Expect(testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, config)).To(Succeed())
	template := &dpuservicev1.DPUServiceTemplate{}
	Expect(testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, template)).To(Succeed())
	versionDigest := calculateDPUServiceVersionDigest(config, template)
	return versionDigest
}

func getDPUServiceChainList() *dpuservicev1.DPUServiceChainList {
	dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
	Expect(testClient.List(ctx, dpuServiceChainList)).To(Succeed())

	return dpuServiceChainList
}

func getDPUServiceList() *dpuservicev1.DPUServiceList {
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	Expect(testClient.List(ctx, dpuServiceList)).To(Succeed())

	return dpuServiceList
}
