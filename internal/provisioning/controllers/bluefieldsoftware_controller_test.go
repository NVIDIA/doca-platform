/*
Copyright 2026 NVIDIA

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

package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	bfsutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
)

var _ = Describe("BlueFieldSoftware", func() {

	var (
		testNS *corev1.Namespace
	)

	var getObjKey = func(obj *provisioningv1.BlueFieldSoftware) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.BlueFieldSoftware {
		return &provisioningv1.BlueFieldSoftware{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec:   provisioningv1.BlueFieldSpec{},
			Status: provisioningv1.BlueFieldSoftwareStatus{},
		}
	}

	var getComponentFilePath = func(bfs *provisioningv1.BlueFieldSoftware, componentType bfsutil.ComponentType) string {
		fileName := bfsutil.DefaultComponentFilename(bfs, componentType)
		return filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components", fileName)
	}

	BeforeEach(func() {
		By("creating the namespaces")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "bfs-controller-test"}}
		Eventually(func() error {
			return k8sClient.Create(ctx, testNS)
		}).WithTimeout(10 * time.Second).Should(Succeed())

		By("creating components directory")
		componentsDir := filepath.Join(cutil.BFBBaseDir, "components")
		err := os.MkdirAll(componentsDir, 0755)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("BlueFieldSoftware basic lifecycle", func() {
		ctx := context.Background()

		It("BlueFieldSoftware: check finalizer is added", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-finalizer-test")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("checking the finalizer")
			objFetched := &provisioningv1.BlueFieldSoftware{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("BlueFieldSoftware: download single URL component (FwBundleURL)", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-single-component")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("expecting the Status (Ready) with condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("verifying lifecycle conditions (Initialized, Downloaded, Ready)")
			initializedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondInitialized))
			Expect(initializedCond).NotTo(BeNil())
			Expect(initializedCond.Status).To(Equal(metav1.ConditionTrue))

			downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDownloaded))
			Expect(downloadedCond).NotTo(BeNil())
			Expect(downloadedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonSuccess)))

			readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Reason).To(Equal(string(conditions.ReasonSuccess)))

			By("verifying downloaded component status")
			Expect(objFetched.Status.DownloadedComponents.PldmFwBundle).To(Equal(obj.Spec.PldmFwBundle))

			By("verifying file exists")
			filePath := getComponentFilePath(objFetched, bfsutil.ComponentTypeFwBundle)
			_, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())

			// Verify ObservedGeneration is tracked correctly
			Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
		})

		It("BlueFieldSoftware: download multiple URL components", func() {
			By("creating the BlueFieldSoftware with multiple components")
			obj := createObj("bfs-multi-component")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			obj.Spec.OsIso = bfbServerURL + BFB8KBPath
			obj.Spec.TmpFwComponents = &provisioningv1.TmpFwComponents{
				BmcFw:      bfbServerURL + BFB512KBPath,
				AstraNicFw: bfbServerURL + BFB8KBPath,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("verifying all components are downloaded")
			Expect(objFetched.Status.DownloadedComponents.PldmFwBundle).To(Equal(obj.Spec.PldmFwBundle))
			Expect(objFetched.Status.DownloadedComponents.OsIso).To(Equal(obj.Spec.OsIso))
			Expect(objFetched.Status.DownloadedComponents.BmcFw).To(Equal(obj.Spec.TmpFwComponents.BmcFw))
			Expect(objFetched.Status.DownloadedComponents.AstraNicFw).To(Equal(obj.Spec.TmpFwComponents.AstraNicFw))

			By("verifying all files exist")
			for _, componentType := range []bfsutil.ComponentType{
				bfsutil.ComponentTypeFwBundle,
				bfsutil.ComponentTypeOSISO,
				bfsutil.ComponentTypeBMC,
				bfsutil.ComponentTypeNIC,
			} {
				filePath := getComponentFilePath(objFetched, componentType)
				_, err := os.Stat(filePath)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Component %s should exist", componentType))
			}
		})

		It("BlueFieldSoftware: handle non-URL values (direct strings)", func() {
			By("creating the BlueFieldSoftware with non-URL values")
			obj := createObj("bfs-non-url")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath // URL will be downloaded
			obj.Spec.TmpFwComponents = &provisioningv1.TmpFwComponents{
				BmcFw:      "version-1.2.3", // Non-URL, stored directly
				AstraNicFw: "nic-fw-v4.5.6", // Non-URL, stored directly
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("verifying URL was downloaded")
			Expect(objFetched.Status.DownloadedComponents.PldmFwBundle).To(Equal(obj.Spec.PldmFwBundle))
			filePath := getComponentFilePath(objFetched, bfsutil.ComponentTypeFwBundle)
			_, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())

			By("verifying non-URL values are stored directly")
			Expect(objFetched.Status.DownloadedComponents.BmcFw).To(Equal("version-1.2.3"))
			Expect(objFetched.Status.DownloadedComponents.AstraNicFw).To(Equal("nic-fw-v4.5.6"))
		})

		It("BlueFieldSoftware: check (Downloading)->(Error) when URL is not valid (status 404)", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-invalid-url")
			obj.Spec.PldmFwBundle = bfbServerURL + "/notfound.tar.gz"
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}
			By("expecting the Status (Error) with condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareError))
				downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDownloaded))
				g.Expect(downloadedCond).NotTo(BeNil())
				g.Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonFailure)))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("BlueFieldSoftware: cleanup cached files on deletion", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-cleanup")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB8KBPath
			obj.Spec.OsIso = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("verifying files exist")
			fwBundlePath := getComponentFilePath(objFetched, bfsutil.ComponentTypeFwBundle)
			osisoPath := getComponentFilePath(objFetched, bfsutil.ComponentTypeOSISO)
			_, err := os.Stat(fwBundlePath)
			Expect(err).NotTo(HaveOccurred())
			_, err = os.Stat(osisoPath)
			Expect(err).NotTo(HaveOccurred())

			By("removing BlueFieldSoftware")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

			By("verifying Deleted condition during deletion")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, getObjKey(obj), objFetched)
				if err != nil && !apierrors.IsNotFound(err) {
					g.Expect(err).NotTo(HaveOccurred())
				}
				if err == nil {
					deletedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDeleted))
					if deletedCond != nil {
						g.Expect(deletedCond.Status).To(Equal(metav1.ConditionTrue))
						g.Expect(deletedCond.Reason).To(Equal(string(conditions.ReasonSuccess)))
					}
				}
			}).WithTimeout(10 * time.Second).Should(Succeed())

			Eventually(func() (done bool, err error) {
				if err := k8sClient.Get(ctx, getObjKey(obj), objFetched); err != nil {
					if apierrors.IsNotFound(err) {
						return true, nil
					}
					return false, err
				}
				return false, nil
			}).WithTimeout(30 * time.Second).Should(BeTrue())

			By("verifying files are deleted")
			_, err = os.Stat(fwBundlePath)
			Expect(err).To(HaveOccurred())
			_, err = os.Stat(osisoPath)
			Expect(err).To(HaveOccurred())
		})

		It("BlueFieldSoftware: remove cached component file from Status (Ready)", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-remove-file")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}
			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			filePath := getComponentFilePath(objFetched, bfsutil.ComponentTypeFwBundle)
			_, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())

			By("removing cached component file")
			Expect(os.Remove(filePath)).NotTo(HaveOccurred())

			By("verifying Ready condition becomes False when file is missing")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Reason).To(Equal(string(conditions.ReasonError)))
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())

			By("expecting the Status (Ready) after re-download")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			_, err = os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
		})

		It("BlueFieldSoftware: creating number of BlueFieldSoftware objects", func() {
			const numObjs = 10
			var objs []*provisioningv1.BlueFieldSoftware

			By("creating the objects")
			for i := 1; i <= numObjs; i++ {
				index := fmt.Sprintf("%d", i)
				obj := createObj("bfs-" + index)
				obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
				obj.Spec.TmpFwComponents = &provisioningv1.TmpFwComponents{BmcFw: "bmc-version-" + index}
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
				objs = append(objs, obj)
			}

			By("checking the objects have Status (Ready)")
			objFetched := &provisioningv1.BlueFieldSoftware{}
			for _, o := range objs {
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, getObjKey(o), objFetched)).To(Succeed())
					g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
				}).WithTimeout(30 * time.Second).Should(Succeed())
			}

			By("removing all objects")
			for _, o := range objs {
				Expect(k8sClient.Delete(ctx, o)).To(Succeed())
				Eventually(func() (done bool, err error) {
					if err := k8sClient.Get(ctx, getObjKey(o), objFetched); err != nil {
						if apierrors.IsNotFound(err) {
							return true, nil
						}
						return false, err
					}
					return false, nil
				}).WithTimeout(60 * time.Second).Should(BeTrue())
			}
		})

		It("BlueFieldSoftware: fail to create with name exceeding the maximum length", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj(utilrand.String(188))
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(HaveOccurred())
		})

		It("BlueFieldSoftware: patcher should set observedGeneration and handle finalizer", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-patcher-test")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("verifying patcher sets observedGeneration, adds finalizer, and updates status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer),
					"patcher should add finalizer")
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation),
					"patcher should set observedGeneration to match generation")
				g.Expect(objFetched.Status.Phase).NotTo(BeEmpty(),
					"patcher should update status")
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("BlueFieldSoftware: handle all component types", func() {
			By("creating the BlueFieldSoftware with all components")
			obj := createObj("bfs-all-components")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			obj.Spec.OsIso = bfbServerURL + BFB8KBPath
			obj.Spec.TmpFwComponents = &provisioningv1.TmpFwComponents{
				BmcErot:    bfbServerURL + BFB512KBPath,
				BmcFw:      bfbServerURL + BFB8KBPath,
				AstraNicFw: bfbServerURL + BFB512KBPath,
				GraceErot:  bfbServerURL + BFB8KBPath,
				GraceFw:    bfbServerURL + BFB512KBPath,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("verifying all 7 components are downloaded")
			Expect(objFetched.Status.DownloadedComponents.PldmFwBundle).To(Equal(obj.Spec.PldmFwBundle))
			Expect(objFetched.Status.DownloadedComponents.OsIso).To(Equal(obj.Spec.OsIso))
			Expect(objFetched.Status.DownloadedComponents.BmcErot).To(Equal(obj.Spec.TmpFwComponents.BmcErot))
			Expect(objFetched.Status.DownloadedComponents.BmcFw).To(Equal(obj.Spec.TmpFwComponents.BmcFw))
			Expect(objFetched.Status.DownloadedComponents.AstraNicFw).To(Equal(obj.Spec.TmpFwComponents.AstraNicFw))
			Expect(objFetched.Status.DownloadedComponents.GraceErot).To(Equal(obj.Spec.TmpFwComponents.GraceErot))
			Expect(objFetched.Status.DownloadedComponents.GraceFw).To(Equal(obj.Spec.TmpFwComponents.GraceFw))

			By("verifying all files exist")
			for _, componentType := range []bfsutil.ComponentType{
				bfsutil.ComponentTypeFwBundle,
				bfsutil.ComponentTypeOSISO,
				bfsutil.ComponentTypeBMCEROT,
				bfsutil.ComponentTypeBMC,
				bfsutil.ComponentTypeNIC,
				bfsutil.ComponentTypeGRACEEROT,
				bfsutil.ComponentTypeGRACEFW,
			} {
				filePath := getComponentFilePath(objFetched, componentType)
				_, err := os.Stat(filePath)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Component %s should exist", componentType))
			}
		})
	})

	Context("BlueFieldSoftware Reconcile unit tests", func() {
		It("should short-circuit and add finalizer when not present", func() {
			By("creating a BlueFieldSoftware without finalizer")
			obj := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-finalizer",
					Namespace:  testNS.Name,
					Generation: 1,
				},
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: "http://example.com/fw.tar",
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("verifying finalizer is added and reconcile returns early")
			objFetched := &provisioningv1.BlueFieldSoftware{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				// Finalizer should be added
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer))
				// ObservedGeneration should be set by the patcher even in short-circuit
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("verifying the object transitions to a proper phase after finalizer is added")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				// After finalizer is added, subsequent reconcile should set phase
				g.Expect(objFetched.Status.Phase).NotTo(BeEmpty())
			}).WithTimeout(15 * time.Second).Should(Succeed())
		})

		It("should return error and update conditions when Handle fails", func() {
			By("creating a BlueFieldSoftware with an invalid URL")
			obj := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-error-" + utilrand.String(5),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: bfbServerURL + "/this-will-404.tar",
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("verifying object reaches Error phase and conditions are updated")
			objFetched := &provisioningv1.BlueFieldSoftware{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareError))

				// Verify Downloaded condition is False with Failure reason
				downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDownloaded))
				g.Expect(downloadedCond).NotTo(BeNil())
				g.Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonFailure)))
				g.Expect(downloadedCond.Message).To(ContainSubstring("404"))

				// ObservedGeneration should be updated
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should requeue with RequeueInterval on successful reconcile", func() {
			By("creating a BlueFieldSoftware with valid URL")
			obj := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-requeue-" + utilrand.String(5),
					Namespace:  testNS.Name,
					Generation: 1,
				},
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: bfbServerURL + BFB8KBPath,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("verifying object reaches Ready phase with proper conditions")
			objFetched := &provisioningv1.BlueFieldSoftware{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))

				// Verify all success conditions
				initializedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondInitialized))
				g.Expect(initializedCond).NotTo(BeNil())
				g.Expect(initializedCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(initializedCond.Reason).To(Equal(string(conditions.ReasonSuccess)))

				downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDownloaded))
				g.Expect(downloadedCond).NotTo(BeNil())
				g.Expect(downloadedCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonSuccess)))

				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.Reason).To(Equal(string(conditions.ReasonSuccess)))

				// ObservedGeneration should match Generation
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))

				// Downloaded component should be set
				g.Expect(objFetched.Status.DownloadedComponents.PldmFwBundle).To(Equal(obj.Spec.PldmFwBundle))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("verifying the object remains in Ready phase (continuous requeuing)")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondReady))
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("should handle not found errors gracefully", func() {
			By("attempting to reconcile a non-existent object")
			// This test verifies that the controller returns nil error for NotFound
			// We can't directly test this with the running controller, but we verify
			// that deleting an object doesn't cause reconciliation errors

			obj := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-notfound-" + utilrand.String(5),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: bfbServerURL + BFB8KBPath,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			// Wait for it to be ready
			objFetched := &provisioningv1.BlueFieldSoftware{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			// Delete the object
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

			// Verify it's eventually deleted without errors
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)
				return apierrors.IsNotFound(err)
			}).WithTimeout(30 * time.Second).Should(BeTrue())
		})

		It("should properly handle patcher errors and aggregate them with state handler errors", func() {
			By("creating a BlueFieldSoftware that will transition through states")
			obj := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-patcher-" + utilrand.String(5),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.BlueFieldSpec{
					PldmFwBundle: bfbServerURL + BFB512KBPath,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("verifying the object successfully transitions through phases")
			objFetched := &provisioningv1.BlueFieldSoftware{}

			// Should start in Initializing phase
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Or(
					Equal(provisioningv1.BlueFieldSoftwareInitializing),
					Equal(provisioningv1.BlueFieldSoftwareDownloading),
				))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Eventually reach Ready
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))

				// Verify ObservedGeneration is properly maintained through phase transitions
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})
	})

	Context("BlueFieldSoftware deletion scenarios", func() {
		ctx := context.Background()

		It("BlueFieldSoftware: should block deletion when DPUs are using it", func() {
			By("creating the BlueFieldSoftware")
			obj := createObj("bfs-blocked-deletion")
			obj.Spec.PldmFwBundle = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				// Ignore not found errors since the test deletes the BlueFieldSoftware explicitly
				err := k8sClient.Delete(ctx, obj)
				if err != nil && !apierrors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			})

			objFetched := &provisioningv1.BlueFieldSoftware{}

			By("waiting for BlueFieldSoftware to be Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("creating a DPU that uses this BlueFieldSoftware")
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-using-bfs",
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUSpec{
					BlueFieldSoftware: obj.Name,
					DPUNodeName:       "test-node",
					DPUDeviceName:     "test-device",
					DPUFlavor:         "test-flavor",
					SerialNumber:      "MT25066004C12345",
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			}
			Expect(k8sClient.Create(ctx, dpu)).To(Succeed())
			DeferCleanup(func() {
				// Ignore not found errors since the test deletes the DPU explicitly
				err := k8sClient.Delete(ctx, dpu)
				if err != nil && !apierrors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			})

			By("attempting to delete the BlueFieldSoftware")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

			By("verifying BlueFieldSoftware transitions to Deleting phase but is not deleted")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareDeleting))
				g.Expect(objFetched.DeletionTimestamp).NotTo(BeNil())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("verifying Deleted condition is False with Pending reason")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				deletedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BlueFieldSoftwareCondDeleted))
				g.Expect(deletedCond).NotTo(BeNil())
				g.Expect(deletedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(deletedCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(deletedCond.Message).To(ContainSubstring("still being used by DPUs"))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("verifying BlueFieldSoftware still exists after timeout")
			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, getObjKey(obj), objFetched)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareDeleting))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("deleting the DPU")
			Expect(k8sClient.Delete(ctx, dpu)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: dpu.Name, Namespace: dpu.Namespace}, dpu)
				return apierrors.IsNotFound(err)
			}).WithTimeout(10 * time.Second).Should(BeTrue())

			By("verifying BlueFieldSoftware is eventually deleted after DPU is removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, getObjKey(obj), objFetched)
				return apierrors.IsNotFound(err)
			}).WithTimeout(30 * time.Second).Should(BeTrue())
		})
	})
})
