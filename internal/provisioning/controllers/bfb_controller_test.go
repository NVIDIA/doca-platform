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

package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	bfbutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
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

var _ = Describe("BFB", func() {
	const (
		DefaultBFBFileNamePrefix = "dummy-"
	)

	var (
		testNS *corev1.Namespace
	)

	var getObjKey = func(obj *provisioningv1.BFB) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.BFB {
		return &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec:   provisioningv1.BFBSpec{},
			Status: provisioningv1.BFBStatus{},
		}
	}

	BeforeEach(func() {
		By("creating the namespaces")
		// Notes:
		// 1. Namespace usage limitation:
		// EnvTest does not support namespace deletion. Deleting a namespace will seem to succeed,
		// but the namespace will just be put in a Terminating state, and never actually be reclaimed.
		// See: https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		// 2. the value in GenerateName is not defined as a constant intentionally,
		// because it shouldn't be referenced directly.
		// 3. testNS is the only way to reference the namespace in the test.
		// 4. always create a new namespace for each test, never reuse an existing namespace
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "bfb-controller-test"}}
		Eventually(func() error {
			return k8sClient.Create(ctx, testNS)
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()
		It("BFB: check filename is correctly defaulted", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("expecting the filename is correctly defaulted")
			objFetched := &provisioningv1.BFB{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.FileName).To(Equal(bfbutil.DefaultBFBFilename(obj)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("BFB: check (Downloading)->(Error) when URL is not valid (status 404)", func() {
			By("creating the obj")
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + "/bf-notfound.bfb"
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}
			By("expecting the Status (Error) with condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBError))
				downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondDownloaded))
				g.Expect(downloadedCond).NotTo(BeNil())
				g.Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonError)))
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("BFB: check status (Ready)", func() {
			By("creating the obj")
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("checking the finalizer")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BFBFinalizer))
				g.Expect(objFetched.Status.FileName).To(Equal(fileName))
			}).Should(Succeed())

			By("expecting the Status (Ready) with condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBReady))
				// Wait for Ready condition to be set with Status=True
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
			Expect(err).NotTo(HaveOccurred())

			By("verifying lifecycle conditions (Initialized, Downloaded, Ready)")
			initializedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondInitialized))
			Expect(initializedCond).NotTo(BeNil())
			Expect(initializedCond.Status).To(Equal(metav1.ConditionTrue))

			downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondDownloaded))
			Expect(downloadedCond).NotTo(BeNil())
			Expect(downloadedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonSuccess)))
			Expect(downloadedCond.Message).To(BeEmpty())

			readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Reason).To(Equal(string(conditions.ReasonSuccess)))
			Expect(readyCond.Message).To(BeEmpty())

			// Verify ObservedGeneration is tracked correctly
			Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
			Expect(downloadedCond.ObservedGeneration).To(Equal(objFetched.Generation))
			Expect(readyCond.ObservedGeneration).To(Equal(objFetched.Generation))
		})

		It("BFB: check status (Ready) with maximum length name", func() {
			By("creating the obj")
			testNSWithMaximumLength := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: utilrand.String(63)}}
			Expect(k8sClient.Create(ctx, testNSWithMaximumLength)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, testNSWithMaximumLength)
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj(utilrand.String(187))
			obj.Namespace = testNSWithMaximumLength.Name
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
			Expect(err).NotTo(HaveOccurred())
		})

		It("BFB: check status (Ready) in case bfb file is cached manually", func() {
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			BFBFileName := cutil.GenerateBFBFilePath(fileName)

			By("caching bfb file before start")
			f, err := os.Create(BFBFileName)
			Expect(err).NotTo(HaveOccurred())
			_, err = f.Write(testBFB)
			Expect(err).NotTo(HaveOccurred())

			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Ready) with condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBReady))
				// Wait for Ready condition to be set with Status=True
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			_, err = os.Stat(BFBFileName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("BFB: cleanup cached file on obj deletion", func() {
			By("creating the obj")
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB8KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
			Expect(err).NotTo(HaveOccurred())

			By("removing obj")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

			By("verifying Deleted condition during deletion")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, getObjKey(obj), objFetched)
				if err != nil && !apierrors.IsNotFound(err) {
					g.Expect(err).NotTo(HaveOccurred())
				}
				if err == nil {
					deletedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondDeleted))
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
			_, err = os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
			Expect(err).To(HaveOccurred())
		})

		It("BFB: remove cached bfb file from Status (Ready)", func() {
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			bfbFileName := cutil.GenerateBFBFilePath(fileName)

			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}
			By("expecting the Status (Ready) with condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBReady))
				// Wait for Ready condition to be set with Status=True
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			_, err := os.Stat(bfbFileName)
			Expect(err).NotTo(HaveOccurred())

			By("removing cached bfb file")
			Expect(os.Remove(bfbFileName)).NotTo(HaveOccurred())

			By("verifying Ready condition becomes False when file is missing (detected on next requeue)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Reason).To(Equal(string(conditions.ReasonError)))
				// Verify ObservedGeneration is still tracked
				g.Expect(readyCond.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())

			By("expecting the Status (Ready) after re-download")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBReady))
				// Wait for Ready condition to be set with Status=True
				readyCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondReady))
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				// Verify ObservedGeneration is still tracked after recovery
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
				g.Expect(readyCond.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			_, err = os.Stat(bfbFileName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("BFB: remove cached bfb file from Status (Ready) when server is down", func() {
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			bfbFileName := cutil.GenerateBFBFilePath(fileName)
			bfbPath := "/BlueField/BFBs/bf-dummy.bfb"

			By("creating server for bfb download")
			mux := http.NewServeMux()
			handler := func(w http.ResponseWriter, r *http.Request) {
				// Support both HEAD (for size verification) and GET (for download)
				Expect(r.Method).To(SatisfyAny(Equal("GET"), Equal("HEAD")))
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testBFB)))
				w.WriteHeader(http.StatusOK)
				if r.Method == "GET" {
					_, _ = w.Write(testBFB)
				}
			}
			mux.HandleFunc(bfbPath, handler)
			server := httptest.NewUnstartedServer(mux)
			server.Start()
			Expect(server).ToNot(BeNil())
			By("server is listening:" + server.URL)

			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + bfbPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}
			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			_, err := os.Stat(bfbFileName)
			Expect(err).NotTo(HaveOccurred())

			By("stopping server")
			server.Close()

			By("removing cached bfb file")
			Expect(os.Remove(bfbFileName)).NotTo(HaveOccurred())

			By("expecting the Status (Downloading) after controller detects missing file on requeue")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Error) with Downloaded and Error conditions")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBError))

				// Verify Error condition with full details
				errorCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondError))
				g.Expect(errorCond).NotTo(BeNil())
				g.Expect(errorCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(errorCond.Reason).To(Equal(string(conditions.ReasonSuccess)))
				g.Expect(errorCond.ObservedGeneration).To(Equal(objFetched.Generation))

				// Verify Downloaded condition is False with Error reason
				downloadedCond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondDownloaded))
				g.Expect(downloadedCond).NotTo(BeNil())
				g.Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonError)))
				g.Expect(downloadedCond.ObservedGeneration).To(Equal(objFetched.Generation))

				// Verify ObservedGeneration is tracked in error state
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
		})

		It("BFB: creating number of objs", func() {
			const numObjs = 64
			var objs []*provisioningv1.BFB

			By("creating the objs")
			for i := 1; i < numObjs; i++ {
				index := fmt.Sprintf("%d", i)
				fileName := DefaultBFBFileNamePrefix + index + ".bfb"
				obj := createObj("obj-bfb" + index)
				obj.Spec.URL = bfbServerURL + BFB512KBPath
				obj.Spec.FileName = ptr.To(fileName)
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
				objs = append(objs, obj)
			}

			By("checking the objs have Status (Ready)")
			objFetched := &provisioningv1.BFB{}
			for _, o := range objs {
				Eventually(func(g Gomega) provisioningv1.BFBPhase {
					g.Expect(k8sClient.Get(ctx, getObjKey(o), objFetched)).To(Succeed())
					return objFetched.Status.Phase
				}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			}

			By("removing all objs")
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
				_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
				Expect(err).To(HaveOccurred())
			}
		})
		It("BFB: fail to create with name exceeding the maximum length", func() {
			By("creating the obj")
			obj := createObj(utilrand.String(188))
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(HaveOccurred())
		})

		It("BFB: patcher should set observedGeneration and handle finalizer", func() {
			By("creating the BFB")
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj("obj-bfb-patcher-test")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("verifying patcher sets observedGeneration, adds finalizer, and updates status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BFBFinalizer),
					"patcher should add finalizer")
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation),
					"patcher should set observedGeneration to match generation")
				g.Expect(objFetched.Status.Phase).NotTo(BeEmpty(),
					"patcher should update status")
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
	})

	Context("Deletion with References", func() {
		It("BFB: should block deletion when DPUSet references it", func() {
			fileName := fmt.Sprintf("%s%s.bfb", DefaultBFBFileNamePrefix, utilrand.String(5))
			obj := createObj("bfb-dpuset-ref")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				bfbFile := cutil.GenerateBFBFilePath(fileName)
				_ = os.Remove(bfbFile)
			})

			objFetched := &provisioningv1.BFB{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("creating DPUSet that references BFB")
			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset-" + utilrand.String(5),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUSetSpec{
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:       provisioningv1.BFBReference{Name: obj.Name},
							DPUFlavor: "dummy-flavor",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dpuSet)).To(Succeed())

			By("verifying BFB deletion is blocked")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				cond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondDeleted))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(cond.Message).To(ContainSubstring(dpuSet.Name))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("deleting DPUSet and waiting for it to be gone")
			Expect(k8sClient.Delete(ctx, dpuSet)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuSet.Name,
					Namespace: dpuSet.Namespace,
				}, &provisioningv1.DPUSet{}))
			}).WithTimeout(30*time.Second).Should(BeTrue(), "DPUSet should be deleted")

			By("verifying BFB is deleted after DPUSet is removed")
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, getObjKey(obj), objFetched))
			}).WithTimeout(60*time.Second).WithPolling(1*time.Second).Should(BeTrue(), "BFB should be deleted after DPUSet is removed")
		})

		It("BFB: should block deletion when DPU references it", func() {
			fileName := fmt.Sprintf("%s%s.bfb", DefaultBFBFileNamePrefix, utilrand.String(5))
			obj := createObj("bfb-dpu-ref")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				bfbFile := cutil.GenerateBFBFilePath(fileName)
				_ = os.Remove(bfbFile)
			})

			objFetched := &provisioningv1.BFB{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("creating DPU in different namespace (should NOT block deletion)")
			otherNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-" + utilrand.String(5),
				},
			}
			Expect(k8sClient.Create(ctx, otherNS)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, otherNS)).To(Succeed())
			})

			otherDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-dpu-" + utilrand.String(5),
					Namespace: otherNS.Name,
				},
				Spec: provisioningv1.DPUSpec{
					BFB:           obj.Name, // Same BFB name, different namespace
					DPUFlavor:     "dummy-flavor",
					SerialNumber:  "SN-" + utilrand.String(5),
					DPUDeviceName: "device-" + utilrand.String(5),
					DPUNodeName:   "node-" + utilrand.String(5),
				},
			}
			Expect(k8sClient.Create(ctx, otherDPU)).To(Succeed())

			By("creating DPU in same namespace that references BFB")
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-" + utilrand.String(5),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUSpec{
					BFB:           obj.Name,
					DPUFlavor:     "dummy-flavor",
					SerialNumber:  "SN-" + utilrand.String(5),
					DPUDeviceName: "device-" + utilrand.String(5),
					DPUNodeName:   "node-" + utilrand.String(5),
				},
			}
			Expect(k8sClient.Create(ctx, dpu)).To(Succeed())

			By("verifying BFB deletion is blocked by same-namespace DPU")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				cond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.BFBCondDeleted))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(cond.Message).To(ContainSubstring(dpu.Name))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("deleting same-namespace DPU and waiting for it to be gone")
			Expect(k8sClient.Delete(ctx, dpu)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpu.Name,
					Namespace: dpu.Namespace,
				}, &provisioningv1.DPU{}))
			}).WithTimeout(30*time.Second).Should(BeTrue(), "DPU should be deleted")

			By("verifying BFB is deleted despite DPU in different namespace")
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, getObjKey(obj), objFetched))
			}).WithTimeout(60*time.Second).WithPolling(1*time.Second).Should(BeTrue(), "BFB should be deleted (other namespace DPU should not block)")
		})
	})
})
