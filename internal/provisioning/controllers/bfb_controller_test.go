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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

		It("BFB: check status (Initializing) and destroy", func() {
			By("creating the obj")
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB8KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.BFBInitializing))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("checking the finalizer")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BFBFinalizer))
			}).WithTimeout(10 * time.Second).Should(Succeed())

		})

		It("BFB: check filename is correctly defaulted", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("expecting the Status (Initializing)")
			objFetched := &provisioningv1.BFB{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.FileName).To(Equal(bfbutil.DefaultBFBFilename(obj)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("BFB: check status (Downloading) and destroy", func() {
			By("creating the obj")
			fileName := DefaultBFBFileNamePrefix + utilrand.String(8) + ".bfb"
			obj := createObj("obj-bfb")
			obj.Spec.URL = bfbServerURL + BFB512KBPath
			obj.Spec.FileName = ptr.To(fileName)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("checking the finalizer")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Finalizers).To(ContainElement(provisioningv1.BFBFinalizer))
				g.Expect(objFetched.Status.FileName).To(Equal(fileName))
			}).Should(Succeed())

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))
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

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Error)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBError))
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

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
			Expect(err).NotTo(HaveOccurred())
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

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

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

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status).NotTo(BeNil())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
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

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
			Expect(err).NotTo(HaveOccurred())

			By("removing obj")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
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

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			_, err := os.Stat(bfbFileName)
			Expect(err).NotTo(HaveOccurred())

			By("removing cached bfb file")
			Expect(os.Remove(bfbFileName)).NotTo(HaveOccurred())

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
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
				Expect(r.Method).To(Equal("GET"))
				w.Header().Set("Content-Type", "application/octet-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(testBFB)
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

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

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

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))

			By("expecting the Status (Error)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBError))
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
})
