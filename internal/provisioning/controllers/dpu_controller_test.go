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
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/bfcfg"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/hostnetwork"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPU", func() {
	const (
		DefaultNS  = "dpf-provisioning-test"
		DefaultBFB = "dpf-provisioning-bfb-test"
	)

	var (
		testNS         *corev1.Namespace
		testDPUCluster *provisioningv1.DPUCluster
		testNode       *corev1.Node
		i              *informer.TestInformer
	)

	var getObjKey = func(obj *provisioningv1.DPU) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.DPU {
		return &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec:   provisioningv1.DPUSpec{},
			Status: provisioningv1.DPUStatus{},
		}
	}

	var createBFB = func(ctx context.Context, name string) *provisioningv1.BFB {
		const BFBPathFileSize = "/bf-bundle-dummy.bfb"
		var (
			server *httptest.Server
		)

		By("creating location for bfb files")
		_, err := os.Stat(cutil.BFBBaseDir)
		if err == nil {
			AbortSuite("Setup is not suitable. Remove " + cutil.BFBBaseDir + " that is used by test and rerun.")
		}

		err = os.Mkdir(cutil.BFBBaseDir, 0755)
		Expect(err).ToNot(HaveOccurred())

		mux := http.NewServeMux()
		handler := func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("GET"))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 1024))
		}
		mux.HandleFunc(BFBPathFileSize, handler)
		server = httptest.NewUnstartedServer(mux)
		server.Start()
		Expect(server).ToNot(BeNil())
		By("server is listening:" + server.URL)

		By("creating the obj")
		obj := &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
		}
		obj.Spec.URL = server.URL + BFBPathFileSize
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		objFetched := &provisioningv1.BFB{}

		By("expecting the Status (Ready)")
		Eventually(func(g Gomega) provisioningv1.BFBPhase {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), objFetched)).To(Succeed())
			return objFetched.Status.Phase
		}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBReady))
		_, err = os.Stat(cutil.GenerateBFBFilePath(objFetched.Spec.FileName))
		Expect(err).NotTo(HaveOccurred())

		By("closing server")
		// Sleep is needed to overcome potential race with http handler initialization
		t := time.AfterFunc(2000*time.Millisecond, server.Close)
		defer t.Stop()

		return obj
	}

	var destroyBFB = func(ctx context.Context, obj *provisioningv1.BFB) {

		By("Cleaning the bfb")
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

		By("cleanup location for bfb files")
		// Remove folder after closing http server
		Expect(os.RemoveAll(cutil.BFBBaseDir)).To(Succeed())
	}

	var createDPUCluster = func(ctx context.Context, name string) *provisioningv1.DPUCluster {
		cluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.StaticCluster),
			},
			Status: provisioningv1.DPUClusterStatus{},
		}
		Expect(k8sClient.Create(ctx, cluster)).NotTo(HaveOccurred())
		return cluster
	}

	var createNode = func(ctx context.Context, name string, labels map[string]string) *corev1.Node {
		labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = ""
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
		Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())
		return node
	}

	BeforeEach(func() {
		By("creating the namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, testNS))).To(Succeed())

		By("creating the dpucluster")
		testDPUCluster = createDPUCluster(ctx, "dpucluster-default")

		By("creating the node")
		testNode = createNode(ctx, "node-default", map[string]string{})

		By("Creating the informer infrastructure for DPU")
		i = informer.NewInformer(cfg, provisioningv1.DPUGroupVersionKind, testNS.Name, "dpus")
		DeferCleanup(i.Cleanup)
		go i.Run()
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())

		By("deleting the dpucluster")
		Expect(k8sClient.Delete(ctx, testDPUCluster)).To(Succeed())

		By("Cleaning the node")
		Expect(k8sClient.Delete(ctx, testNode)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("check status (Initializing) and destroy", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Consistently(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))
		})

		It("check status (Initializing) with DPUOOBBridgeNotConfigured", func() {
			By("Remove labels from Node to simulate a non-existing OOB bridge")
			testNode.SetLabels(nil)
			Expect(k8sClient.Update(ctx, testNode)).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.NodeName = testNode.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: true,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing / DPUOOBBridgeNotConfigured)")
			Eventually(func(g Gomega) []metav1.Condition {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUOOBBridgeNotConfigured"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("Add OOB label and it should still fail")
			testNode.SetLabels(map[string]string{
				cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "",
			})
			Expect(k8sClient.Update(ctx, testNode)).To(Succeed())

			By("expecting the Status (Initializing)")
			Consistently(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))
		})

		It("check status (Initializing) and cluster NotReady", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.NodeName = testNode.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: true,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			By("expecting the Condition (DPUClusterNotReady)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUClusterNotReady"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))
		})

		It("attaching to node w/o bfb object (Node Effect) is NoEffect", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.NodeName = testNode.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: true,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			Skip("TODO: DPUCluter should be in Ready state to proceed")

			By("expecting the Status (Node Effect)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("expecting the Status (Pending)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUPending))
		})

		It("attaching to node (Node Effect) is Drain", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.NodeName = testNode.Name
			obj.Spec.BFB = DefaultBFB
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Drain: &provisioningv1.Drain{
					AutomaticNodeReboot: true,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			Skip("TODO: DPUCluter should be in Ready state to proceed")

			By("expecting the Status (Node Effect)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("expecting the Status (Error)")
			Eventually(func(g Gomega) []metav1.Condition {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUError))
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))

			By("deleting objs")
			cleanupObjs := []client.Object{}
			cleanupObjs = append(cleanupObjs, obj)
			Expect(testutils.CleanupAndWait(ctx, k8sClient, cleanupObjs...)).To(Succeed())
		})

		It("attaching to node (Node Effect) is NoEffect", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.BFB = DefaultBFB
			obj.Spec.NodeName = testNode.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: true,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			Skip("TODO: DPUCluter should be in Ready state to proceed")

			By("expecting the Status (Node Effect)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("expecting the Status (Pending)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUPending))

			By("creating the bfb")
			testBFB := createBFB(ctx, DefaultBFB)

			By("expecting the Status (DMSRunning)")
			Eventually(func(g Gomega) []metav1.Condition {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondDMSRunning.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUDMSDeployment))

			By("deleting objs")
			cleanupObjs := []client.Object{}
			cleanupObjs = append(cleanupObjs, obj)
			Expect(testutils.CleanupAndWait(ctx, k8sClient, cleanupObjs...)).To(Succeed())

			By("Cleaning the bfb")
			destroyBFB(ctx, testBFB)
		})

		It("attaching to node w/o Labels (Node Effect) is CustomLabel", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.BFB = DefaultBFB
			obj.Spec.NodeName = testNode.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				CustomLabel: map[string]string{
					"provisioning.dpu.nvidia.com/bfb": "dummy.bfb",
					"version":                         "1.2.3",
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			Skip("TODO: DPUCluter should be in Ready state to proceed")

			By("expecting the Status (Node Effect)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("expecting the Status (Pending)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUPending))

			By("checking the node`s Labels")
			nodeFetched := &corev1.Node{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testNode), nodeFetched)).To(Succeed())
			Expect(nodeFetched.Labels).To(HaveLen(2))
			Expect(nodeFetched.Labels).To(HaveKeyWithValue("provisioning.dpu.nvidia.com/bfb", "dummy.bfb"))
			Expect(nodeFetched.Labels).To(HaveKeyWithValue("version", "1.2.3"))

			By("creating the bfb")
			testBFB := createBFB(ctx, DefaultBFB)

			By("expecting the Status (DMSRunning)")
			Eventually(func(g Gomega) []metav1.Condition {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondDMSRunning.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUDMSDeployment))

			By("deleting objs")
			cleanupObjs := []client.Object{}
			cleanupObjs = append(cleanupObjs, obj)
			Expect(testutils.CleanupAndWait(ctx, k8sClient, cleanupObjs...)).To(Succeed())

			By("Cleaning the bfb")
			destroyBFB(ctx, testBFB)
		})

		It("attaching to node with Labels (Node Effect) is CustomLabel", func() {
			By("creating the node obj with Labels")
			nodeObj := createNode(ctx, "node-with-labels", map[string]string{
				"label1": "value1",
			})
			DeferCleanup(k8sClient.Delete, ctx, nodeObj)

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.BFB = DefaultBFB
			obj.Spec.NodeName = nodeObj.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				CustomLabel: map[string]string{
					"provisioning.dpu.nvidia.com/bfb": "dummy.bfb",
					"version":                         "1.2.3",
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			Skip("TODO: DPUCluter should be in Ready state to proceed")

			By("expecting the Status (Node Effect)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("expecting the Status (Pending)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUPending))

			By("checking the node`s Labels")
			nodeFetched := &corev1.Node{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nodeObj), nodeFetched)).To(Succeed())
			Expect(nodeFetched.Labels).To(HaveLen(3))
			Expect(nodeFetched.Labels).To(HaveKeyWithValue("label1", "value1"))
			Expect(nodeFetched.Labels).To(HaveKeyWithValue("provisioning.dpu.nvidia.com/bfb", "dummy.bfb"))
			Expect(nodeFetched.Labels).To(HaveKeyWithValue("version", "1.2.3"))

			By("creating the bfb")
			testBFB := createBFB(ctx, DefaultBFB)

			By("expecting the Status (DMSRunning)")
			Eventually(func(g Gomega) []metav1.Condition {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondDMSRunning.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUDMSDeployment))

			By("deleting objs")
			cleanupObjs := []client.Object{}
			cleanupObjs = append(cleanupObjs, obj)
			Expect(testutils.CleanupAndWait(ctx, k8sClient, cleanupObjs...)).To(Succeed())

			By("Cleaning the bfb")
			destroyBFB(ctx, testBFB)
		})

		It("attaching to node w/o Taints (Node Effect) is Taint", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.BFB = DefaultBFB
			obj.Spec.NodeName = testNode.Name
			taintObj := &corev1.Taint{
				Key:       "testTaint1",
				Value:     "value1",
				Effect:    corev1.TaintEffectNoSchedule,
				TimeAdded: nil,
			}
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Taint: taintObj,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status (Initializing)")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))

			Skip("TODO: DPUCluter should be in Ready state to proceed")

			By("expecting the Status (Node Effect)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUInitializing))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))

			By("expecting the Status (Pending)")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUPending))

			By("checking the node`s Taints")
			// The error typically comes if there is a taint on nodes for which you don't have corresponding toleration in pod spec.
			// node.kubernetes.io/not-ready: Node is not ready. This corresponds to the NodeCondition Ready being "False".
			taintErrorObj := corev1.Taint{
				Key:       "node.kubernetes.io/not-ready",
				Value:     "",
				Effect:    corev1.TaintEffectNoSchedule,
				TimeAdded: nil,
			}
			nodeFetched := &corev1.Node{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testNode), nodeFetched)).To(Succeed())
			Expect(testNode.Spec.Taints).To(HaveLen(1))
			Expect(testNode.Spec.Taints[0]).Should(Equal(taintErrorObj))

			By("creating the bfb")
			testBFB := createBFB(ctx, DefaultBFB)

			By("expecting the Status (DMSRunning)")
			Eventually(func(g Gomega) []metav1.Condition {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondDMSRunning.String()),
					HaveField("Status", metav1.ConditionFalse),
				),
			))
			Expect(objFetched.Status.Phase).Should(Equal(provisioningv1.DPUDMSDeployment))

			By("deleting objs")
			cleanupObjs := []client.Object{}
			cleanupObjs = append(cleanupObjs, obj)
			Expect(testutils.CleanupAndWait(ctx, k8sClient, cleanupObjs...)).To(Succeed())

			By("Cleaning the bfb")
			destroyBFB(ctx, testBFB)
		})
	})
})

var _ = Describe("DPUFlavor", func() {

	const (
		DefaultNS      = "dpf-provisioning-test"
		DefaultDPUName = "dpf-dpu"
	)

	var (
		testNS *corev1.Namespace
	)

	var getObjKey = func(obj *provisioningv1.DPUFlavor) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.DPUFlavor {
		return &provisioningv1.DPUFlavor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUFlavorSpec{},
		}
	}

	BeforeEach(func() {
		By("creating the namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, testNS))).To(Succeed())
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("create and get object minimal", func() {
			By("creating the obj-1")
			obj1 := createObj("obj-dpuflavor-1")
			err := k8sClient.Create(ctx, obj1)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj1), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj1))

			data1, err := bfcfg.Generate(obj1, DefaultDPUName, "", false)
			Expect(err).To(Succeed())
			Expect(data1).ShouldNot(BeNil())

			By("creating the obj-2")
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: obj-dpuflavor-2
  namespace: default
`)
			obj2 := &provisioningv1.DPUFlavor{}
			err = yaml.UnmarshalStrict(yml, obj2)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj2)
			Expect(err).NotTo(HaveOccurred())

			data2, err := bfcfg.Generate(obj2, DefaultDPUName, "", false)
			Expect(err).To(Succeed())
			Expect(data2).ShouldNot(BeNil())

			By("compare the obj-1 and obj-2")
			Expect(data1).Should(Equal(data2))
		})

		It("create obj", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: obj
  namespace: default
spec:
  grub:
    kernelParameters:
      - console=hvc0
      - console=ttyAMA0
      - earlycon=pl011,0x13010000
      - fixrttc
      - net.ifnames=0
      - biosdevname=0
      - iommu.passthrough=1
      - cgroup_no_v1=net_prio,net_cls
      - hugepagesz=2048kB
      - hugepages=3072
  sysctl:
    parameters:
    - net.ipv4.ip_forward=1
    - net.ipv4.ip_forward_update_priority=0
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=40
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - SRIOV_EN=1
        - NUM_OF_VFS=30
        - LAG_RESOURCE_ALLOCATION=1
  ovs:
    rawConfigScript: |
      ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones="50000"
      ovs-vsctl set Open_vSwitch . other_config:hw-offload="true"
      ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
  bfcfgParameters:
    - ubuntu_PASSWORD=$1$rvRv4qpw$mS6kYODr8oMxORt.TkiTB0
    - WITH_NIC_FW_UPDATE=yes
    - ENABLE_SFC_HBN=no
  configFiles:
  - path: /etc/bla/blabla.cfg
    operation: append
    raw: |
        CREATE_OVS_BRIDGES="no"
        CREATE_OVS_BRIDGES="no"
    permissions: "0755"
`)
			obj := &provisioningv1.DPUFlavor{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			data, err := bfcfg.Generate(obj, DefaultDPUName, "", false)
			Expect(err).To(Succeed())
			Expect(data).ShouldNot(BeNil())
		})
	})
})

var _ = Describe("DMS Pod", func() {

	const (
		DefaultNS       = "dpf-provisioning-test"
		DefaultNodeName = "dpf-node"
		DefaultDPUName  = "dpf-dpu"
	)

	var (
		testNS   *corev1.Namespace
		testNode *corev1.Node
	)

	var createNode = func(ctx context.Context, name string) *corev1.Node {
		obj := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
				Labels: map[string]string{
					cutil.NodeFeatureDiscoveryLabelPrefix + "dpu.features-dpu-pciAddress":     "0000-90-00",
					cutil.NodeFeatureDiscoveryLabelPrefix + "dpu.features-dpu-pf-name":        "ens1f0np0",
					cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "",
				}},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
				Addresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "127.0.0.1",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())
		return obj
	}

	BeforeEach(func() {
		By("creating the namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, testNS))).To(Succeed())

		By("creating the node")
		testNode = createNode(ctx, DefaultNodeName)
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())

		By("Cleaning the node")
		Expect(k8sClient.Delete(ctx, testNode)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("create DMS Pod w/o Node", func() {
			By("creating Issuer")
			obj := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-selfsigned-issuer",
					Namespace: testNS.Name,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			By("creating the dpu")
			objDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUName,
					Namespace: testNS.Name,
				},
				Spec:   provisioningv1.DPUSpec{},
				Status: provisioningv1.DPUStatus{},
			}
			Expect(k8sClient.Create(ctx, objDPU)).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, objDPU)

			By("creating DMS Pod")
			option := util.DPUOptions{}
			err := dms.CreateDMSPod(ctx, k8sClient, objDPU, option)
			Expect(err).To(HaveOccurred())
		})

		It("create DMS Pod w/o options", func() {
			By("creating Issuer")
			obj := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-selfsigned-issuer",
					Namespace: testNS.Name,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			By("creating the dpu")
			objDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUName,
					Namespace: testNS.Name,
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/dpu-pciAddress": "0000-90-00",
						"provisioning.dpu.nvidia.com/dpu-pf-name":    "ens1f0np0",
					},
				},
				Spec: provisioningv1.DPUSpec{
					NodeName: testNode.Name,
				},
				Status: provisioningv1.DPUStatus{},
			}
			Expect(k8sClient.Create(ctx, objDPU)).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, objDPU)

			By("creating DMS Pod w/o options")
			option := util.DPUOptions{}
			err := dms.CreateDMSPod(ctx, k8sClient, objDPU, option)
			Expect(err).To(MatchError(ContainSubstring("persistentVolumeClaim.claimName")))
		})

		It("creating DMS Pod wit minimal options", func() {
			By("creating Issuer")
			obj := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-selfsigned-issuer",
					Namespace: testNS.Name,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			By("creating the dpu")
			objDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUName,
					Namespace: testNS.Name,
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/dpu-pciAddress": "0000-90-00",
						"provisioning.dpu.nvidia.com/dpu-pf-name":    "ens1f0np0",
					},
				},
				Spec: provisioningv1.DPUSpec{
					NodeName: testNode.Name,
				},
				Status: provisioningv1.DPUStatus{},
			}
			Expect(k8sClient.Create(ctx, objDPU)).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, objDPU)

			By("creating DMS Pod")
			option := util.DPUOptions{
				DMSImageWithTag: "example.com/doca-platform-foundation/dpf-provisioning-controller/hostdriver:v0.1.0",
				BFBPVC:          "bfb-pvc",
			}
			err := dms.CreateDMSPod(ctx, k8sClient, objDPU, option)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &corev1.Pod{}

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: objDPU.Namespace,
				Name:      cutil.GenerateDMSPodName(objDPU.Name)},
				objFetched)).To(Succeed())
			Expect(objFetched.OwnerReferences[0].Kind).Should(Equal("DPU"))
			Expect(objFetched.OwnerReferences[0].Name).Should(Equal(objDPU.Name))
		})

		It("creating Hostnetwork Pod with minimal options", func() {
			By("creating Issuer")
			obj := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpf-provisioning-selfsigned-issuer",
					Namespace: testNS.Name,
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			By("creating the dpu flavor")
			objDPUFlavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUFlavorSpec{},
			}
			// We cannot defer the cleanup of the DPUFlavor because it is used by the DPU.
			Expect(k8sClient.Create(ctx, objDPUFlavor)).NotTo(HaveOccurred())

			By("creating the dpu")
			objDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUName,
					Namespace: testNS.Name,
					Labels: map[string]string{
						"provisioning.dpu.nvidia.com/dpu-pciAddress": "0000-90-00",
						"provisioning.dpu.nvidia.com/dpu-pf-name":    "ens1f0np0",
					},
				},
				Spec: provisioningv1.DPUSpec{
					NodeName:  testNode.Name,
					DPUFlavor: DefaultDPUName,
				},
				Status: provisioningv1.DPUStatus{},
			}
			Expect(k8sClient.Create(ctx, objDPU)).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, objDPU)

			By("creating dpf-operator-system namespace")
			err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}})
			if !apierrors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}

			By("creating DPFOperatorConfig")
			dpfOperatorConfig := getMinimalDPFOperatorConfig()
			Expect(k8sClient.Create(ctx, dpfOperatorConfig)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, dpfOperatorConfig)

			By("creating Hostnetwork Pod")
			option := util.DPUOptions{
				HostnetworkImageWithTag: "example.com/doca-platform-foundation/dpf-provisioning-controller/hostdriver:v0.1.0",
			}
			err = hostnetwork.CreateHostNetworkSetupPod(ctx, k8sClient, objDPU, option)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &corev1.Pod{}

			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: objDPU.Namespace,
				Name:      cutil.GenerateHostnetworkPodName(objDPU.Name)},
				objFetched)).To(Succeed())
			Expect(objFetched.Spec.Containers[0].Env).Should(ConsistOf(
				corev1.EnvVar{Name: "device_pci_address", Value: "0000:90:00"},
				corev1.EnvVar{Name: "num_of_vfs", Value: "16"},
				corev1.EnvVar{Name: "vf_mtu", Value: "9000"},
			))
		})
	})
})

func getMinimalDPFOperatorConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultDPFOperatorConfigSingletonName,
			Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "name",
			},
			Networking: &operatorv1.Networking{
				HighSpeedMTU: ptr.To(9000),
			},
		},
	}
}
