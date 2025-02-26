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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("BFB", func() {
	const (
		DefaultNS            = "dpf-provisioning-test"
		BFBPathFileSize512KB = "/BlueField/BFBs/bf-bundle-dummy-512KB.bfb"
		BFBPathFileSize8KB   = "/BlueField/BFBs/bf-bundle-dummy-8KB.bfb"
		BFBPathFileNotFound  = "/BlueField/BFBs/bf-bundle-dummy-notfound.bfb"
		DefaultBFBFileName   = "dummy.bfb"
	)

	var (
		testNS *corev1.Namespace
		server *httptest.Server
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
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, testNS))).To(Succeed())

		By("creating location for bfb files")
		_, err := os.Stat(cutil.BFBBaseDir)
		if err == nil {
			AbortSuite("Setup is not suitable. Remove " + cutil.BFBBaseDir + " that is used by test and rerun.")
		}

		err = os.Mkdir(cutil.BFBBaseDir, 0755)
		Expect(err).ToNot(HaveOccurred())

		By("creating server for bfb download")
		data512KB := make([]byte, 512*1024)
		data8KB := make([]byte, 8*1024)

		mux := http.NewServeMux()
		handler512KB := func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("GET"))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data512KB)
		}
		mux.HandleFunc(BFBPathFileSize512KB, handler512KB)
		handler8KB := func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("GET"))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data8KB)
		}
		mux.HandleFunc(BFBPathFileSize8KB, handler8KB)
		server = httptest.NewUnstartedServer(mux)
		server.Start()
		Expect(server).ToNot(BeNil())
		By("server is listening:" + server.URL)
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())

		By("closing server")
		// Sleep is needed to overcome potential race with http handler initialization
		t := time.AfterFunc(2000*time.Millisecond, server.Close)
		defer t.Stop()

		By("cleanup location for bfb files")
		// Remove folder after closing http server
		Expect(os.RemoveAll(cutil.BFBBaseDir)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("check status (Initializing) and destroy", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize512KB
			obj.Spec.FileName = DefaultBFBFileName
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("checking the finalizer")
			Eventually(func(g Gomega) []string {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Finalizers
			}).WithTimeout(10 * time.Second).Should(ConsistOf([]string{provisioningv1.BFBFinalizer}))
			Expect(objFetched.Spec.FileName).To(BeEquivalentTo(DefaultBFBFileName))
		})

		It("check status (Downloading) and destroy", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize512KB
			obj.Spec.FileName = DefaultBFBFileName
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.BFBInitializing))

			By("checking the finalizer")
			Eventually(func(g Gomega) []string {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Finalizers
			}).WithTimeout(10 * time.Second).Should(ConsistOf([]string{provisioningv1.BFBFinalizer}))
			Expect(objFetched.Spec.FileName).To(BeEquivalentTo(DefaultBFBFileName))

			By("expecting the Status (Downloading)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBDownloading))
		})

		It("check (Downloading)->(Error) when URL is not valid (status 404)", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileNotFound
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

		It("check status (Ready)", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize512KB
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
			file, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 512*1024))
		})

		It("check status (Ready) in case bfb file is cached manually", func() {
			BFBFileName := cutil.GenerateBFBFilePath(DefaultBFBFileName)

			By("caching bfb file before start")
			f, err := os.Create(BFBFileName)
			Expect(err).NotTo(HaveOccurred())
			fileSize, err := f.Write(make([]byte, 512*1024))
			Expect(err).NotTo(HaveOccurred())
			Expect(fileSize).Should(BeNumerically("==", 512*1024))
			Expect(f.Close()).To(Succeed())
			file, err := os.Stat(BFBFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 512*1024))

			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize512KB
			obj.Spec.FileName = DefaultBFBFileName
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}

			By("expecting the Status (Ready)")
			Eventually(func(g Gomega) provisioningv1.BFBPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.BFBReady))
			file, err = os.Stat(BFBFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 512*1024))
		})

		It("cleanup cached file on obj deletion", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize8KB
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
			file, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 8*1024))

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
			_, err = os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
			Expect(err).To(HaveOccurred())
		})

		It("remove cached bfb file from Status (Ready)", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize512KB
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
			file, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 512*1024))

			By("removing cached bfb file")
			Expect(os.Remove(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))).NotTo(HaveOccurred())

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
			file, err = os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 512*1024))
		})

		It("remove cached bfb file from Status (Ready) when server is down", func() {
			By("creating the obj")
			obj := createObj("obj-bfb")
			obj.Spec.URL = server.URL + BFBPathFileSize512KB
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
			file, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Size()).Should(BeNumerically("==", 512*1024))

			By("stopping server")
			server.Close()

			By("removing cached bfb file")
			Expect(os.Remove(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))).NotTo(HaveOccurred())

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

		It("creating number of objs", func() {
			const numObjs = 64
			var objs []*provisioningv1.BFB

			By("creating the objs")
			for i := 1; i < numObjs; i++ {
				index := fmt.Sprintf("%d", i)
				obj := createObj("obj-bfb" + index)
				obj.Spec.URL = server.URL + BFBPathFileSize512KB
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
				_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
				Expect(err).To(HaveOccurred())
			}
		})
	})
})
