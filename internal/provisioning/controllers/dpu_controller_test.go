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
	"os"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	dpuctrl "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("DPU", func() {
	const (
		DefaultNS                      = "dpf-provisioning-test"
		DefaultBFB                     = "dpf-provisioning-bfb-test"
		DefaultNode                    = "dpf-provisinoning-dpu-controller-node-test"
		DefaultDPUCluster              = "dpf-provisioning-dpu-cluster-test"
		DefaultPCIAddress              = "0000-aa-00"
		DefaultSerialNumberPrefix      = "MT25066004C"
		DefaultDPUInProvisioningMapMax = 3
	)

	var (
		testNS         *corev1.Namespace
		testBFB        *provisioningv1.BFB
		testDPUCluster *provisioningv1.DPUCluster
		testNode       *corev1.Node
		testDPUNode    *provisioningv1.DPUNode
		testDPUDevice  *provisioningv1.DPUDevice
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
			Spec: provisioningv1.DPUSpec{
				SerialNumber: DefaultSerialNumberPrefix + utilrand.String(5),
				DPUFlavor:    "dpu-flavor",
				BFB:          ptr.To(DefaultBFB),
				NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
			},
			Status: provisioningv1.DPUStatus{},
		}
	}

	var createBFB = func(ctx context.Context, name string, serverURL string, unready bool) *provisioningv1.BFB {
		By("creating the obj")
		obj := &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
		}
		obj.Spec.URL = serverURL + BFB8KBPath
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		if unready {
			By("expecting the Status (Error)")
			patch := client.MergeFrom(obj.DeepCopy())

			obj.Status.Phase = provisioningv1.BFBError
			Expect(k8sClient.Status().Patch(ctx, obj, patch)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())

			return obj
		}

		objFetched := &provisioningv1.BFB{}

		By("expecting the Status (BFBReady)")
		Eventually(func(g Gomega) provisioningv1.BFBPhase {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), objFetched)).To(Succeed())
			return objFetched.Status.Phase
		}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(provisioningv1.BFBReady))
		_, err := os.Stat(cutil.GenerateBFBFilePath(objFetched.Status.FileName))
		Expect(err).NotTo(HaveOccurred())

		return obj
	}

	var destroyBFB = func(ctx context.Context, obj *provisioningv1.BFB) {
		By("Cleaning the bfb")
		Expect(testutils.CleanupAndWait(ctx, k8sClient, obj)).To(Succeed())
	}

	var createDPUCluster = func(ctx context.Context, name string) *provisioningv1.DPUCluster {
		By("creating the cluster object")
		cluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type:       string(provisioningv1.StaticCluster),
				Kubeconfig: fmt.Sprintf("%s-admin-kubeconfig", name),
			},
			Status: provisioningv1.DPUClusterStatus{},
		}
		Expect(k8sClient.Create(ctx, cluster)).NotTo(HaveOccurred())

		By("setting the cluster`s status ready")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			patch := client.MergeFrom(cluster.DeepCopy())
			cluster.Status.Phase = provisioningv1.PhaseReady
			cluster.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionCreated),
					Status:             metav1.ConditionTrue,
					Reason:             "Created",
					Message:            "dpu_controller_test",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
				{
					Type:               string(provisioningv1.ConditionReady),
					Status:             metav1.ConditionTrue,
					Reason:             "HealthCheckPassed",
					Message:            "dpu_controller_test",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			}
			g.Expect(k8sClient.Status().Patch(ctx, cluster, patch)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			g.Expect(cluster.Status.Phase).To(Equal(provisioningv1.PhaseReady))
		}).WithTimeout(30 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

		return cluster
	}

	var createNode = func(ctx context.Context, name string) *corev1.Node {
		By("creating the node object")
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
				Labels: map[string]string{
					cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
				},
			},
		}

		Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		taintErrorObj := corev1.Taint{
			Key:       "node.kubernetes.io/not-ready",
			Value:     "",
			Effect:    corev1.TaintEffectNoSchedule,
			TimeAdded: nil,
		}
		Expect(node.Spec.Taints).To(HaveLen(1))
		Expect(node.Spec.Taints[0]).Should(Equal(taintErrorObj))

		By("removing the node`s taints")
		node.Spec.Taints = nil
		Expect(k8sClient.Update(ctx, node)).To(Succeed())

		By("setting the node`s status ready")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		patch := client.MergeFrom(node.DeepCopy())

		// See https://kubernetes.io/docs/reference/node/node-status/
		node.Status.Phase = corev1.NodeRunning
		node.Status.Conditions = append(node.Status.Conditions, []corev1.NodeCondition{
			{
				Type:               "Ready",
				Status:             corev1.ConditionTrue,
				Reason:             "KubeletReady",
				Message:            "kubelet is posting ready status",
				LastTransitionTime: metav1.Time{Time: time.Now()},
			},
		}...)
		Expect(k8sClient.Status().Patch(ctx, node, patch)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		Expect(node.Status.Phase).To(Equal(corev1.NodeRunning))
		return node
	}

	var createDPUNode = func(ctx context.Context, name string) *provisioningv1.DPUNode {
		dpuNode := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
				Labels: map[string]string{
					cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: operatorv1.GroupVersion.String(),
						Kind:       operatorv1.DPFOperatorConfigKind,
						Name:       "fake-dpf-operator-config",
						UID:        "fake-uid-123",
						Controller: ptr.To(false),
					},
				},
			},
			Spec: provisioningv1.DPUNodeSpec{
				NodeRebootMethod: &provisioningv1.NodeRebootMethod{
					GNOI: &provisioningv1.GNOI{},
				},
				NodeDMSAddress: &provisioningv1.DMSAddress{IP: "1.1.1.1", Port: 1234},
				DPUs: []provisioningv1.DPURef{
					{
						Name: testDPUDevice.Name,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dpuNode)).NotTo(HaveOccurred())
		return dpuNode
	}

	var createDPUDevice = func(ctx context.Context, namespace string, name string) *provisioningv1.DPUDevice {
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					provisioningv1.DPUNodeNameLabel: DefaultNode,
				},
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber: DefaultSerialNumberPrefix + utilrand.String(5),
			},
		}
		Expect(k8sClient.Create(ctx, dpuDevice)).NotTo(HaveOccurred())
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.PCIAddress = ptr.To(DefaultPCIAddress)
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
	}

	BeforeEach(func() {
		By("creating location for bfb files")
		// Notes:
		// 1. Namespace usage limitation:
		// EnvTest does not support namespace deletion. Deleting a namespace will seem to succeed,
		// but the namespace will just be put in a Terminating state, and never actually be reclaimed.
		// See: https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		// 2. the value in GenerateName is not defined as a constant intentionally,
		// because it shouldn't be referenced directly.
		// 3. testNS is the only way to reference the namespace in the test.
		// 4. always create a new namespace for each test, never reuse an existing namespace
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpu-controller-test"}}
		Eventually(func() error {
			return k8sClient.Create(ctx, testNS)
		}).WithTimeout(10 * time.Second).Should(Succeed())

		By("creating the bfb")
		testBFB = createBFB(ctx, DefaultBFB, bfbServerURL, false)

		By("creating the dpucluster")
		testDPUCluster = createDPUCluster(ctx, DefaultDPUCluster)

		By("creating the node")
		testNode = createNode(ctx, DefaultNode)

		By("creating the dpuDevice")
		testDPUDevice = createDPUDevice(ctx, testNS.Name, DefaultNode)

		By("creating the dpuNode")
		testDPUNode = createDPUNode(ctx, DefaultNode)

		By("Creating the informer infrastructure for DPU")
		i = informer.NewInformer(cfg, provisioningv1.DPUGroupVersionKind, testNS.Name, "dpus")
		DeferCleanup(i.Cleanup)
		go i.Run()
		Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())

		// By("clear DPUInProvisioningMap")
		// dpuReconciler.DPUInProvisioningMap = util.NewDPUInProvisioningMap(DefaultDPUInProvisioningMapMax)
	})

	AfterEach(func() {
		// TODO: Adjust this cleanup to ensure that we test the finalizer removal correctly. This breaks a lot of tests
		// and since we are time constraint, it was not possible to fix in this PR. The DPUNode finalizer removal is
		// also checked in e2e tests.
		if testDPUNode != nil {
			By("Manually removing the DPUNode finalizer - get DPUNode")
			dpuNodeFetched := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), dpuNodeFetched)).To(Succeed())
			By("Manually removing the DPUNode finalizer - remove finalizer")
			patcher := patch.NewSerialPatcher(dpuNodeFetched, k8sClient)
			controllerutil.RemoveFinalizer(dpuNodeFetched, provisioningv1.DPUNodeFinalizer)
			Expect(patcher.Patch(ctx, dpuNodeFetched)).To(Succeed())
			By("Deleting the DPUNode")
			Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUNode)).To(Succeed())
		}

		By("Cleaning the node")
		Expect(testutils.CleanupAndWait(ctx, k8sClient, testNode)).To(Succeed())

		By("deleting the dpucluster")
		Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUCluster)).To(Succeed())

		// Delete all DPUs before deleting the BFB
		By("Deleting all DPUs in the test namespace before deleting the BFB")
		Expect(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPU{}, client.InNamespace(testNS.Name))).To(Succeed())
		Eventually(func() int {
			dpuList := &provisioningv1.DPUList{}
			Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
			return len(dpuList.Items)
		}).WithTimeout(30 * time.Second).Should(Equal(0))

		By("Cleaning the bfb")
		destroyBFB(ctx, testBFB)

		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("DPU: a DPU with empty status should be handled as Initializing", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing for 10sec")
			Consistently(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))
		})

		It("DPU: a Ready DPU is reconciled when its spec is updated and is not polled again", func() {
			By("creating the obj")
			obj := createObj("ready-update-" + utilrand.String(5))
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("waiting until the controller has admitted the DPU")
			objFetched := &provisioningv1.DPU{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(objFetched, provisioningv1.DPUFinalizer)).To(BeTrue())
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("moving the DPU to Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				if objFetched.Status.Phase != provisioningv1.DPUReady {
					patch := client.MergeFrom(objFetched.DeepCopy())
					objFetched.Status.Phase = provisioningv1.DPUReady
					g.Expect(k8sClient.Status().Patch(ctx, objFetched, patch)).To(Succeed())
				}
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("letting any RequeueAfter scheduled before Ready expire")
			// Earlier phases requeue on RequeueInterval. Outlast that timer so the only
			// thing left that can reconcile this DPU is a watch event.
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
			}).WithTimeout(cutil.RequeueInterval + 2*time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			By("expecting a Ready reconcile whose Node cannot be read to requeue on the provisioning interval")
			res, err := dpuReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: getObjKey(obj)})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(cutil.RequeueInterval))

			By("updating the spec")
			// NodeLabels is a spec field, so the apiserver bumps metadata.generation and the
			// DPU watch enqueues another reconcile. An annotation-only write would not.
			// Retry: the controller may patch the object between Get and Patch.
			var updatedFrom int64
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				updatedFrom = objFetched.Generation
				base := objFetched.DeepCopy()
				objFetched.Spec.Cluster.NodeLabels = map[string]string{"dpu-controller-test": "updated"}
				g.Expect(k8sClient.Patch(ctx, objFetched, client.MergeFrom(base))).To(Succeed())
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("expecting the update event to enqueue a reconcile while the DPU stays Ready")
			// Only the DPU controller's status patch writes observedGeneration, and the
			// Ready DPU has no timer left, so catching up to the new generation means the
			// update event queued a reconcile and it ran.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Generation).To(BeNumerically(">", updatedFrom))
				g.Expect(objFetched.Status.ObservedGeneration).To(Equal(objFetched.Generation))
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
			}).WithTimeout(20 * time.Second).Should(Succeed())
		})

		It("DPU: a Ready DPU is reconciled when it is deleted", func() {
			By("creating the obj")
			obj := createObj("ready-delete-" + utilrand.String(5))
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("waiting until the controller has admitted the DPU")
			objFetched := &provisioningv1.DPU{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(objFetched, provisioningv1.DPUFinalizer)).To(BeTrue())
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("moving the DPU to Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				if objFetched.Status.Phase != provisioningv1.DPUReady {
					patch := client.MergeFrom(objFetched.DeepCopy())
					objFetched.Status.Phase = provisioningv1.DPUReady
					g.Expect(k8sClient.Status().Patch(ctx, objFetched, patch)).To(Succeed())
				}
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("deleting the DPU")
			Expect(k8sClient.Delete(ctx, objFetched)).To(Succeed())

			By("expecting the delete to be reconciled into Deleting, or the object to be removed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, getObjKey(obj), objFetched)
				if apierrors.IsNotFound(err) {
					return
				}
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(objFetched.DeletionTimestamp.IsZero()).To(BeFalse())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUDeleting))
			}).WithTimeout(20 * time.Second).Should(Succeed())
		})

		// startReadyDPUWithRemoteNode creates a DPUCluster, a Ready DPU assigned to it, and a
		// remote Node with the given address. The returned DPU has already been reconciled by
		// state.Ready, so a later Node change is what enqueues the next reconcile.
		var startReadyDPUWithRemoteNode = func(name, ip string) (*provisioningv1.DPU, *corev1.Node) {
			cluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-watch-" + utilrand.String(5),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type: string(provisioningv1.StaticCluster),
				},
			}
			cluster.Spec.Kubeconfig = cluster.Name + "-admin-kubeconfig"
			secret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(*cluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
				patch := client.MergeFrom(cluster.DeepCopy())
				cluster.Status.Phase = provisioningv1.PhaseReady
				cluster.Status.Conditions = []metav1.Condition{{
					Type:               string(provisioningv1.ConditionCreated),
					Status:             metav1.ConditionTrue,
					Reason:             "Created",
					Message:            "dpu_controller_test",
					LastTransitionTime: metav1.Time{Time: time.Now()},
				}}
				g.Expect(k8sClient.Status().Patch(ctx, cluster, patch)).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
				g.Expect(cluster.Status.Phase).To(Equal(provisioningv1.PhaseReady))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			DeferCleanup(func() {
				Expect(testutils.CleanupAndWait(ctx, k8sClient, cluster)).To(Succeed())
				Expect(testutils.CleanupAndWait(ctx, k8sClient, secret)).To(Succeed())
			})

			obj := createObj(name)
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.Cluster.Name = cluster.Name
			obj.Spec.Cluster.Namespace = cluster.Namespace
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(objFetched, provisioningv1.DPUFinalizer)).To(BeTrue())
			}).WithTimeout(20 * time.Second).Should(Succeed())

			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: obj.Name}}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			DeferCleanup(func() {
				Expect(testutils.CleanupAndWait(ctx, k8sClient, node)).To(Succeed())
			})
			setTestNodeStatus(ctx, node.Name, corev1.ConditionTrue, ip)

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				if objFetched.Status.Phase != provisioningv1.DPUReady {
					patch := client.MergeFrom(objFetched.DeepCopy())
					objFetched.Status.Phase = provisioningv1.DPUReady
					g.Expect(k8sClient.Status().Patch(ctx, objFetched, patch)).To(Succeed())
				}
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
				g.Expect(objFetched.Status.Addresses).To(ContainElement(corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: ip}))
				cond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.DPUCondReady))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			return objFetched, node
		}

		var expectSettledReady = func(obj *provisioningv1.DPU) {
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), obj)).To(Succeed())
				g.Expect(obj.Status.Phase).To(Equal(provisioningv1.DPUReady))
				cond := meta.FindStatusCondition(obj.Status.Conditions, string(provisioningv1.DPUCondReady))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(cutil.RequeueInterval + 2*time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		}

		It("DPU: a Ready DPU is reconciled when its DPU-cluster Node changes", func() {
			By("creating a Ready DPU whose remote Node is Ready")
			objFetched, node := startReadyDPUWithRemoteNode("ready-node-"+utilrand.String(5), "10.1.0.1")

			By("letting any RequeueAfter scheduled before Ready expire")
			expectSettledReady(objFetched)

			By("making the remote Node NotReady")
			setTestNodeStatus(ctx, node.Name, corev1.ConditionFalse, "10.1.0.1")

			By("expecting the Node informer to enqueue the DPU")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(objFetched), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
				cond := meta.FindStatusCondition(objFetched.Status.Conditions, string(provisioningv1.DPUCondReady))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("NodeNotReady"))
			}).WithTimeout(20 * time.Second).Should(Succeed())
		})

		It("DPU: a Ready DPU is reconciled when its DPU-cluster Node address changes", func() {
			objFetched, node := startReadyDPUWithRemoteNode("ready-addr-"+utilrand.String(5), "10.1.0.1")
			expectSettledReady(objFetched)

			By("changing the remote Node address")
			setTestNodeStatus(ctx, node.Name, corev1.ConditionTrue, "10.2.0.2")

			By("expecting state.Ready to copy the new address")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(objFetched), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.Phase).To(Equal(provisioningv1.DPUReady))
				g.Expect(objFetched.Status.Addresses).To(ContainElement(corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.2.0.2"}))
			}).WithTimeout(20 * time.Second).Should(Succeed())
		})

		It("DPU: a Ready DPU is reconciled when DPU-cluster Node last-applied labels change", func() {
			objFetched, node := startReadyDPUWithRemoteNode("ready-labels-"+utilrand.String(5), "10.1.0.1")
			expectSettledReady(objFetched)

			By("changing the last-applied labels annotation")
			setTestNodeAnnotation(ctx, node.Name, cutil.LastAppliedLabelsOnDPUKey, `{"zone":"a"}`)

			By("expecting state.Ready to leave Ready for cluster config")
			expectClusterConfigFromNode(objFetched)
		})

		It("DPU: a Ready DPU is reconciled when DPU-cluster Node last-applied annotations change", func() {
			objFetched, node := startReadyDPUWithRemoteNode("ready-ann-"+utilrand.String(5), "10.1.0.1")
			expectSettledReady(objFetched)

			By("changing the last-applied annotations annotation")
			setTestNodeAnnotation(ctx, node.Name, cutil.LastAppliedAnnotationsOnDPUKey, `{"note":"a"}`)

			By("expecting state.Ready to leave Ready for cluster config")
			expectClusterConfigFromNode(objFetched)
		})

		It("DPU: a DPU should have set a DPF version in the status and NodeLabels", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}

			By("expecting the DPF version to be set in the status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				g.Expect(objFetched.Status.DPFVersion).To(Equal(ptr.To(release.DPFVersion())))
			}, 10*time.Second).Should(Succeed())
		})
		Describe("DPUInProvisioningMap", func() {
			var (
				fakeMapClient client.Client
			)

			var createFakeDPU = func(name string) *provisioningv1.DPU {
				return &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: testNS.Name,
					},
					Spec: provisioningv1.DPUSpec{
						DPUNodeName: DefaultNode,
						BFB:         ptr.To(DefaultBFB),
						NodeEffect: provisioningv1.NodeEffect{
							Action: provisioningv1.Action{
								Hold: ptr.To(true),
							},
						},
						SerialNumber: DefaultSerialNumberPrefix + utilrand.String(5),
					},
					Status: provisioningv1.DPUStatus{},
				}
			}

			var patchFakePhase = func(name string, phase provisioningv1.DPUPhase) {
				key := client.ObjectKey{Namespace: testNS.Name, Name: name}
				dpu := &provisioningv1.DPU{}
				Expect(fakeMapClient.Get(ctx, key, dpu)).To(Succeed())
				orig := dpu.DeepCopy()
				dpu.Status.Phase = phase
				Expect(fakeMapClient.Status().Patch(ctx, dpu, client.MergeFrom(orig))).To(Succeed())
			}

			BeforeEach(func() {
				By("creating isolated fake client for DPUInProvisioningMap tests")
				fakeMapClient = fake.NewClientBuilder().
					WithScheme(k8sClient.Scheme()).
					WithStatusSubresource(&provisioningv1.DPU{}).
					Build()
			})

			It("DPUInProvisioningMap: should initialize with existing DPUs in provisioning", func() {
				By("creating DPUs in provisioning state")
				dpu := createFakeDPU("dpu-1")
				dpu.Spec.DPUDeviceName = testDPUDevice.Name
				Expect(fakeMapClient.Create(ctx, dpu)).To(Succeed())

				By("setting DPU phases to provisioning states")
				patchFakePhase(dpu.Name, provisioningv1.DPUNodeEffect)

				By("initializing the map")
				Expect(dpuReconciler.DPUInProvisioningMap.Initialize(ctx, fakeMapClient)).To(Succeed())

				By("verifying map state")
				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-extra"))).To(HaveOccurred())
			})

			It("DPUInProvisioningMap: should handle empty initialization", func() {
				By("initializing the map with empty fake client")
				Expect(dpuReconciler.DPUInProvisioningMap.Initialize(ctx, fakeMapClient)).To(Succeed())

				By("verifying map state")
				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-extra"))).To(Succeed())
			})

			It("DPUInProvisioningMap: should handle initialization with non-provisioning DPUs", func() {
				By("creating DPUs in non-provisioning state")
				dpu1 := createFakeDPU("dpu-1")
				dpu1.Spec.DPUDeviceName = testDPUDevice.Name
				Expect(fakeMapClient.Create(ctx, dpu1)).To(Succeed())

				dpu2 := createFakeDPU("dpu-2")
				dpu2.Spec.DPUDeviceName = testDPUDevice.Name
				Expect(fakeMapClient.Create(ctx, dpu2)).To(Succeed())

				By("setting DPU phase to non-provisioning state")
				patchFakePhase(dpu1.Name, provisioningv1.DPUReady)
				patchFakePhase(dpu2.Name, provisioningv1.DPUInitializing)

				By("initializing the map")
				Expect(dpuReconciler.DPUInProvisioningMap.Initialize(ctx, fakeMapClient)).To(Succeed())

				By("verifying map state")
				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-1"))).To(Succeed())
			})

			It("DPUInProvisioningMap: should handle phase transitions - provisioning to deleting", func() {
				By("creating a DPU")
				dpu := createFakeDPU("dpu-phase")
				dpu.Spec.DPUDeviceName = testDPUDevice.Name
				dpu.Spec.BFB = ptr.To(testBFB.Name)
				Expect(fakeMapClient.Create(ctx, dpu)).To(Succeed())

				By("setting initial state to Provisioning state")
				patchFakePhase(dpu.Name, provisioningv1.DPUNodeEffect)

				By("initializing the map")
				Expect(dpuReconciler.DPUInProvisioningMap.Initialize(ctx, fakeMapClient)).To(Succeed())

				By("verifying CanProceed returns error in Provisioning state")
				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-1"))).To(HaveOccurred())

				By("transitioning to Deleting state")
				patchFakePhase(dpu.Name, provisioningv1.DPUDeleting)

				By("removing DPU from map as it's no longer in provisioning state")
				dpuReconciler.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))

				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-1"))).To(Succeed())
			})

			It("DPUInProvisioningMap: should handle phase transitions - provisioning to Error", func() {
				By("creating a DPU")
				dpu := createFakeDPU("dpu-phase")
				dpu.Spec.DPUDeviceName = testDPUDevice.Name
				dpu.Spec.BFB = ptr.To(testBFB.Name)
				Expect(fakeMapClient.Create(ctx, dpu)).To(Succeed())

				By("setting initial state to Provisioning state")
				patchFakePhase(dpu.Name, provisioningv1.DPUNodeEffect)

				By("initializing the map")
				Expect(dpuReconciler.DPUInProvisioningMap.Initialize(ctx, fakeMapClient)).To(Succeed())

				By("verifying CanProceed returns error in Provisioning state")
				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-1"))).To(HaveOccurred())

				By("transitioning to Error state")
				patchFakePhase(dpu.Name, provisioningv1.DPUError)

				By("removing DPU from map as it's no longer in provisioning state")
				dpuReconciler.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))

				Expect(dpuReconciler.DPUInProvisioningMap.CanProceed(dutil.DPUID("test-dpu-1"))).To(Succeed())
			})

			It("Adding/Removing Additional Requestors to DPU", func() {
				By("creating a DPU")
				dpu := createFakeDPU("dpu-phase")
				dpu.Spec.DPUDeviceName = testDPUDevice.Name
				dpu.Spec.BFB = ptr.To(testBFB.Name)
				Expect(fakeMapClient.Create(ctx, dpu)).To(Succeed())

				By("setting initial state to Provisioning state")
				patchFakePhase(dpu.Name, provisioningv1.DPUNodeEffect)

				By("creating a DPUNodeMaintenance")
				dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
				Expect(err).ToNot(HaveOccurred())
				lastAppliedAdditionalRequestorsOnDPUKey := cutil.GenerateLastAppliedAdditionalRequestorsOnDPUAnnotationKey(dpu.Name)
				dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dpunodemaintenanceName,
						Namespace: testNS.Name,
						Annotations: map[string]string{
							lastAppliedAdditionalRequestorsOnDPUKey: "[]",
						},
					},
					Spec: provisioningv1.DPUNodeMaintenanceSpec{
						NodeEffect: &provisioningv1.NodeEffect{
							Action: provisioningv1.Action{
								Hold: ptr.To(true),
							},
						},
						Requestor: []string{"test-dpu-1"},
					},
				}
				Expect(fakeMapClient.Create(ctx, dpunodemaintenance)).To(Succeed())

				By("updating DPU to add NodeMaintenanceAdditionalRequestors")
				patcher := patch.NewSerialPatcher(dpu, fakeMapClient)
				dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{"service-1", "service-2", "service-3"}
				Expect(patcher.Patch(ctx, dpu)).To(Succeed())

				err = dpuReconciler.UpdateDPUNodeMaintenanceRequestors(ctx, dpu, fakeMapClient)
				Expect(err).ToNot(HaveOccurred())

				By("verifying the requestor")
				fetchedDPUNodeMaintenance := &provisioningv1.DPUNodeMaintenance{}
				Expect(fakeMapClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: dpunodemaintenanceName}, fetchedDPUNodeMaintenance)).To(Succeed())
				Expect(fetchedDPUNodeMaintenance.Spec.Requestor).To(HaveLen(4))
				Expect(fetchedDPUNodeMaintenance.Spec.Requestor).To(ContainElements("service-1", "service-2", "service-3", "test-dpu-1"))

				By("updating DPU to remove service-1 from DPU NodeMaintenanceAdditionalRequestors")
				patcher = patch.NewSerialPatcher(dpu, fakeMapClient)
				dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{"service-2", "service-3"}
				Expect(patcher.Patch(ctx, dpu)).To(Succeed())

				err = dpuReconciler.UpdateDPUNodeMaintenanceRequestors(ctx, dpu, fakeMapClient)
				Expect(err).ToNot(HaveOccurred())

				By("verifying the requestor")
				fetchedDPUNodeMaintenance = &provisioningv1.DPUNodeMaintenance{}
				Expect(fakeMapClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: dpunodemaintenanceName}, fetchedDPUNodeMaintenance)).To(Succeed())
				Expect(fetchedDPUNodeMaintenance.Spec.Requestor).To(HaveLen(3))
				Expect(fetchedDPUNodeMaintenance.Spec.Requestor).To(ContainElements("service-2", "service-3", "test-dpu-1"))

				By("updating DPU to remove test-dpu-1 from DPUNodeMaintenance Requestors")
				patcher = patch.NewSerialPatcher(fetchedDPUNodeMaintenance, fakeMapClient)
				fetchedDPUNodeMaintenance.Spec.Requestor = []string{"service-2", "service-3"}
				Expect(patcher.Patch(ctx, fetchedDPUNodeMaintenance)).To(Succeed())

				err = dpuReconciler.UpdateDPUNodeMaintenanceRequestors(ctx, dpu, fakeMapClient)
				Expect(err).ToNot(HaveOccurred())

				By("verifying the requestor")
				fetchedDPUNodeMaintenance = &provisioningv1.DPUNodeMaintenance{}
				Expect(fakeMapClient.Get(ctx, types.NamespacedName{Namespace: testNS.Name, Name: dpunodemaintenanceName}, fetchedDPUNodeMaintenance)).To(Succeed())
				Expect(fetchedDPUNodeMaintenance.Spec.Requestor).To(HaveLen(2))
				Expect(fetchedDPUNodeMaintenance.Spec.Requestor).To(ContainElements("service-2", "service-3"))
			})
		})

		Context("bfb-registry", func() {
			const (
				leaderPodName = "provisioning-leader"
				nodeName      = "node-1"
				registryImage = "registry:8082"
			)

			It("Reconcile creates bfb-registry pod and service when request is for bfb-registry and env is set", func() {
				ctx := context.Background()
				restore := setEnvForBFBRegistry(leaderPodName, nodeName, registryImage)
				defer restore()

				By("deleting any stale bfb-registry Service/Pod so reconcile can create fresh objects")
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: bfbregistry.PodName, Namespace: testNS.Name},
				}))).To(Succeed())
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: bfbregistry.PodName, Namespace: testNS.Name},
				}))).To(Succeed())

				By("creating the leader pod in test namespace")
				leaderPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      leaderPodName,
						Namespace: testNS.Name,
						UID:       "leader-uid",
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName,
						Containers: []corev1.Container{
							{Name: "manager", Image: "provisioning-controller:test"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, leaderPod)).To(Succeed())

				By("calling Reconcile for bfb-registry")
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{Namespace: testNS.Name, Name: bfbregistry.PodName},
				}
				Eventually(func(g Gomega) {
					_, err := dpuReconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
				}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

				By("verifying bfb-registry pod and service exist")
				pod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: bfbregistry.PodName}, pod)).To(Succeed())
				Expect(pod.Labels[bfbregistry.LabelDPUComponent]).To(Equal(bfbregistry.LabelValue))
				svc := &corev1.Service{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: bfbregistry.PodName}, svc)).To(Succeed())
				Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))

				By("releasing bfb-registry resources for other tests")
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: bfbregistry.PodName, Namespace: testNS.Name}})
					_ = k8sClient.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: bfbregistry.PodName, Namespace: testNS.Name}})
				})
			})

			It("Reconcile for bfb-registry returns no error when env is unset (skip non-leader)", func() {
				ctx := context.Background()
				restore := setEnvForBFBRegistry("", "", "")
				defer restore()

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{Namespace: testNS.Name, Name: bfbregistry.PodName},
				}
				_, err := dpuReconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})

var _ = Describe("DPU UpdateDPUStatus", func() {
	It("leaves PreviousPhase unset when Phase first becomes non-empty from empty (optional)", func() {
		dpu := &provisioningv1.DPU{}
		dpu.Status = provisioningv1.DPUStatus{Phase: ""}
		next := provisioningv1.DPUStatus{Phase: provisioningv1.DPUInitializing}
		Expect(dpuctrl.UpdateDPUStatus(dpu, next)).To(BeTrue())
		Expect(dpu.Status.PreviousPhase).To(BeEmpty())
		Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUInitializing))
	})
	It("does not set PreviousPhase when handler status equals current (even if both omit PreviousPhase)", func() {
		dpu := &provisioningv1.DPU{}
		dpu.Status = provisioningv1.DPUStatus{Phase: provisioningv1.DPUInitializing}
		next := provisioningv1.DPUStatus{Phase: provisioningv1.DPUInitializing}
		Expect(dpuctrl.UpdateDPUStatus(dpu, next)).To(BeFalse())
		Expect(dpu.Status.PreviousPhase).To(BeEmpty())
	})
	It("records prior phase when Phase changes from a non-empty Phase", func() {
		dpu := &provisioningv1.DPU{}
		dpu.Status = provisioningv1.DPUStatus{Phase: provisioningv1.DPUInitializing}
		next := provisioningv1.DPUStatus{Phase: provisioningv1.DPUPending}
		Expect(dpuctrl.UpdateDPUStatus(dpu, next)).To(BeTrue())
		Expect(dpu.Status.PreviousPhase).To(Equal(provisioningv1.DPUInitializing))
		Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUPending))
	})
	It("does not overwrite PreviousPhase when only non-phase status fields change", func() {
		dpu := &provisioningv1.DPU{}
		dpu.Status = provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUPending, PreviousPhase: provisioningv1.DPUInitializing,
			Conditions: []metav1.Condition{{Type: "A", Status: metav1.ConditionFalse}},
		}
		next := provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUPending, PreviousPhase: provisioningv1.DPUInitializing,
			Conditions: []metav1.Condition{{Type: "A", Status: metav1.ConditionTrue}},
		}
		Expect(dpuctrl.UpdateDPUStatus(dpu, next)).To(BeFalse())
		Expect(dpu.Status.PreviousPhase).To(Equal(provisioningv1.DPUInitializing))
		Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUPending))
		Expect(dpu.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
	})
	It("preserves PreviousPhase when phase unchanged", func() {
		dpu := &provisioningv1.DPU{}
		dpu.Status = provisioningv1.DPUStatus{Phase: provisioningv1.DPUPending, PreviousPhase: provisioningv1.DPUInitializing}
		next := provisioningv1.DPUStatus{Phase: provisioningv1.DPUPending, PreviousPhase: provisioningv1.DPUInitializing}
		Expect(dpuctrl.UpdateDPUStatus(dpu, next)).To(BeFalse())
		Expect(dpu.Status.PreviousPhase).To(Equal(provisioningv1.DPUInitializing))
	})
})

var _ = Describe("DPU StayReadyWithoutRequeue", func() {
	DescribeTable("skips the interval only when a Ready DPU stays Ready",
		func(phase, next provisioningv1.DPUPhase, skip bool) {
			Expect(dpuctrl.StayReadyWithoutRequeue(phase, next)).To(Equal(skip))
		},
		Entry("spec or status update that leaves the phase Ready does not schedule another poll", provisioningv1.DPUReady, provisioningv1.DPUReady, true),
		Entry("delete requeues by moving the dpu to Deleting", provisioningv1.DPUReady, provisioningv1.DPUDeleting, false),
		Entry("spec update that must rerun cluster config requeues", provisioningv1.DPUReady, provisioningv1.DPUClusterConfig, false),
		Entry("spec update that must rerun node effect requeues", provisioningv1.DPUReady, provisioningv1.DPUNodeEffect, false),
		Entry("entering ready requeues once so the ready handler runs", provisioningv1.DPUNodeEffectRemoval, provisioningv1.DPUReady, false),
		Entry("provisioning phases keep the interval", provisioningv1.DPUOSInstalling, provisioningv1.DPUOSInstalling, false),
		Entry("error phase is not treated as a settled ready dpu", provisioningv1.DPUOSInstalling, provisioningv1.DPUError, false),
		Entry("ready that moves to error is not treated as settled", provisioningv1.DPUReady, provisioningv1.DPUError, false),
	)
})

var _ = Describe("DPU cluster Node watch", func() {
	It("relists on the controller-runtime default period", func() {
		Expect(dpuctrl.ClusterNodeResyncPeriod).To(Equal(10 * time.Hour))
	})

	It("accepts remote Node changes that Ready records", func() {
		ready := func(rv, ip, readyStatus, labels string) *corev1.Node {
			return &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "dpu-1",
					ResourceVersion: rv,
					Annotations: map[string]string{
						cutil.LastAppliedLabelsOnDPUKey: labels,
					},
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{{
						Type:    corev1.NodeInternalIP,
						Address: ip,
					}},
					Conditions: []corev1.NodeCondition{{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionStatus(readyStatus),
					}},
				},
			}
		}

		base := ready("1", "10.0.0.1", "True", `{"a":"b"}`)
		Expect(dpuctrl.NodeChangeAffectsDPU(nil, base)).To(BeTrue())
		Expect(dpuctrl.NodeChangeAffectsDPU(base, nil)).To(BeTrue())
		Expect(dpuctrl.NodeChangeAffectsDPU(base, base.DeepCopy())).To(BeTrue(), "resync keeps the resourceVersion")

		unchanged := base.DeepCopy()
		unchanged.ResourceVersion = "2"
		unchanged.Status.Allocatable = corev1.ResourceList{}
		Expect(dpuctrl.NodeChangeAffectsDPU(base, unchanged)).To(BeFalse())

		notReady := unchanged.DeepCopy()
		notReady.ResourceVersion = "3"
		notReady.Status.Conditions[0].Status = corev1.ConditionFalse
		Expect(dpuctrl.NodeChangeAffectsDPU(base, notReady)).To(BeTrue())

		address := unchanged.DeepCopy()
		address.ResourceVersion = "4"
		address.Status.Addresses[0].Address = "10.0.0.2"
		Expect(dpuctrl.NodeChangeAffectsDPU(base, address)).To(BeTrue())

		labels := unchanged.DeepCopy()
		labels.ResourceVersion = "5"
		labels.Annotations[cutil.LastAppliedLabelsOnDPUKey] = `{"a":"c"}`
		Expect(dpuctrl.NodeChangeAffectsDPU(base, labels)).To(BeTrue())

		annotations := unchanged.DeepCopy()
		annotations.ResourceVersion = "6"
		annotations.Annotations[cutil.LastAppliedAnnotationsOnDPUKey] = `{"a":"c"}`
		Expect(dpuctrl.NodeChangeAffectsDPU(base, annotations)).To(BeTrue())
	})

	It("selects only a Ready DPU with the Node name and cluster", func() {
		cluster := types.NamespacedName{Namespace: "tenant", Name: "cluster-a"}
		dpus := []provisioningv1.DPU{
			dpuForNodeWatch("dpf", "dpu-1", "cluster-a", provisioningv1.DPUReady),
			dpuForNodeWatch("provisioning", "dpu-1", "cluster-a", provisioningv1.DPUOSInstalling),
			dpuForNodeWatch("other", "dpu-1", "cluster-b", provisioningv1.DPUReady),
			dpuForNodeWatch("dpf", "dpu-2", "cluster-a", provisioningv1.DPUReady),
		}
		Expect(dpuctrl.DPUsForClusterNode(dpus, cluster, "dpu-1")).To(Equal([]*provisioningv1.DPU{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpu-1",
					Namespace: "dpf",
				},
			},
		}))
	})

	It("selects every Ready DPU in a disconnected cluster", func() {
		cluster := types.NamespacedName{Namespace: "tenant", Name: "cluster-a"}
		dpus := []provisioningv1.DPU{
			dpuForNodeWatch("dpf", "dpu-1", "cluster-a", provisioningv1.DPUReady),
			dpuForNodeWatch("provisioning", "dpu-2", "cluster-a", provisioningv1.DPUOSInstalling),
			dpuForNodeWatch("other", "dpu-3", "cluster-b", provisioningv1.DPUReady),
			dpuForNodeWatch("dpf", "dpu-4", "cluster-a", provisioningv1.DPUReady),
		}
		Expect(dpuctrl.ReadyDPUsForCluster(dpus, cluster)).To(Equal([]*provisioningv1.DPU{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpu-1",
					Namespace: "dpf",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpu-4",
					Namespace: "dpf",
				},
			},
		}))
	})
})

// dpuForNodeWatch returns a DPU with the given namespace, name, cluster, and phase.
func dpuForNodeWatch(namespace, name, clusterName string, phase provisioningv1.DPUPhase) provisioningv1.DPU {
	return provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUSpec{
			Cluster: provisioningv1.K8sCluster{
				Name:      clusterName,
				Namespace: "tenant",
			},
		},
		Status: provisioningv1.DPUStatus{
			Phase: phase,
		},
	}
}

// setTestNodeStatus patches the Node status with a Ready condition and one internal IP.
func setTestNodeStatus(ctx context.Context, name string, ready corev1.ConditionStatus, ip string) {
	Eventually(func(g Gomega) {
		current := &corev1.Node{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, current)).To(Succeed())
		base := current.DeepCopy()
		current.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ip}}
		current.Status.Conditions = []corev1.NodeCondition{{
			Type:               corev1.NodeReady,
			Status:             ready,
			Reason:             "Test",
			Message:            "dpu controller test",
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
		}}
		g.Expect(k8sClient.Status().Patch(ctx, current, client.MergeFrom(base))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

// setTestNodeAnnotation patches one annotation on the Node.
func setTestNodeAnnotation(ctx context.Context, name, key, value string) {
	Eventually(func(g Gomega) {
		current := &corev1.Node{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, current)).To(Succeed())
		base := current.DeepCopy()
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[key] = value
		g.Expect(k8sClient.Patch(ctx, current, client.MergeFrom(base))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

// expectClusterConfigFromNode reports that a Node metadata change enqueued the Ready DPU.
// The mock ClusterConfig handler then moves the phase to ServiceReadiness on the following reconcile.
func expectClusterConfigFromNode(obj *provisioningv1.DPU) {
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		switch obj.Status.Phase {
		case provisioningv1.DPUClusterConfig:
			cond := meta.FindStatusCondition(obj.Status.Conditions, string(provisioningv1.DPUCondReady))
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Reason).To(Equal("RerunClusterConfig"))
		case provisioningv1.DPUServiceReadiness:
			g.Expect(obj.Status.PreviousPhase).To(Equal(provisioningv1.DPUClusterConfig))
		default:
			g.Expect(obj.Status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
		}
	}).WithTimeout(20 * time.Second).Should(Succeed())
}

// setEnvForBFBRegistry sets POD_NAME, NODE_NAME, NODE_IP, BFB_REGISTRY_IMAGE and returns a restore func.
func setEnvForBFBRegistry(podName, nodeName, registryImage string) func() {
	old := map[string]string{
		"POD_NAME":           os.Getenv("POD_NAME"),
		"NODE_NAME":          os.Getenv("NODE_NAME"),
		"NODE_IP":            os.Getenv("NODE_IP"),
		"BFB_REGISTRY_IMAGE": os.Getenv("BFB_REGISTRY_IMAGE"),
	}
	Expect(os.Setenv("POD_NAME", podName)).To(Succeed())
	Expect(os.Setenv("NODE_NAME", nodeName)).To(Succeed())
	// NODE_IP feeds the server certificate IP SAN; only set it when a leader is simulated.
	if podName != "" {
		Expect(os.Setenv("NODE_IP", "10.0.0.1")).To(Succeed())
	} else {
		Expect(os.Unsetenv("NODE_IP")).To(Succeed())
	}
	Expect(os.Setenv("BFB_REGISTRY_IMAGE", registryImage)).To(Succeed())
	return func() {
		for k, v := range old {
			if v == "" {
				Expect(os.Unsetenv(k)).To(Succeed())
			} else {
				Expect(os.Setenv(k, v)).To(Succeed())
			}
		}
	}
}
