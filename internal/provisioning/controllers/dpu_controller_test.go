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
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/bfcfg"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("DPU", func() {
	const (
		DefaultNS         = "dpf-provisioning-test"
		DefaultBFB        = "dpf-provisioning-bfb-test"
		DefaultNode       = "dpf-provisinoning-dpu-controller-node-test"
		DefaultDPUCluster = "dpf-provisioning-dpu-cluster-test"
		DefaultPCIAddress = "0000-aa-00"
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
			Spec:   provisioningv1.DPUSpec{},
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
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		patch := client.MergeFrom(cluster.DeepCopy())

		cluster.Status.Phase = provisioningv1.PhaseReady
		cluster.Status.Conditions = append(cluster.Status.Conditions, []metav1.Condition{
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
		}...)
		Expect(k8sClient.Status().Patch(ctx, cluster, patch)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		Expect(cluster.Status.Phase).To(Equal(provisioningv1.PhaseReady))

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
				Annotations: map[string]string{
					reboot.RebootCmdKey: reboot.Skip,
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
				Annotations: map[string]string{
					reboot.RebootCmdKey: reboot.Skip,
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

	var createNodeMaintenance = func(ctx context.Context, obj *provisioningv1.DPU) *maintenancev1alpha1.NodeMaintenance {
		By("creating the maintenance node object")
		owner := metav1.NewControllerRef(obj, provisioningv1.DPUGroupVersionKind)
		node := &maintenancev1alpha1.NodeMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:            obj.Spec.DPUNodeName,
				Namespace:       obj.Namespace,
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
			Spec: maintenancev1alpha1.NodeMaintenanceSpec{
				RequestorID: cutil.NodeMaintenanceRequestorID,
				NodeName:    obj.Spec.DPUNodeName,
				DrainSpec: &maintenancev1alpha1.DrainSpec{
					Force:          true,
					DeleteEmptyDir: true,
				},
			},
		}
		Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())

		By("setting the maintenance node`s status ready")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		patch := client.MergeFrom(node.DeepCopy())

		node.Status.Conditions = append(node.Status.Conditions, []metav1.Condition{
			{
				Type:               maintenancev1alpha1.ConditionTypeReady,
				Status:             metav1.ConditionTrue,
				Reason:             maintenancev1alpha1.ConditionReasonReady,
				LastTransitionTime: metav1.Time{Time: time.Now()},
			},
		}...)
		Expect(k8sClient.Status().Patch(ctx, node, patch)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		return node
	}

	var createPod = func(ctx context.Context, name string, obj *provisioningv1.DPU) *corev1.Pod {
		By("creating the pod object")
		grace := int64(0)
		owner := metav1.NewControllerRef(obj,
			provisioningv1.GroupVersion.WithKind("DPU"))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:                       name,
				Namespace:                  obj.Namespace,
				OwnerReferences:            []metav1.OwnerReference{*owner},
				DeletionGracePeriodSeconds: &grace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "ctr1", Image: "bash", Command: []string{"true"}},
				},
			},
		}

		Expect(k8sClient.Create(ctx, pod)).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
		return pod
	}

	var createDPUDevice = func(ctx context.Context, namespace string, name string) *provisioningv1.DPUDevice {
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					cutil.DPUNodeNameLabel: DefaultNode,
				},
			},
			Spec: provisioningv1.DPUDeviceSpec{
				PCIAddress: ptr.To(DefaultPCIAddress),
			},
		}
		Expect(k8sClient.Create(ctx, dpuDevice)).NotTo(HaveOccurred())
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
	})

	AfterEach(func() {
		// TODO: Adjust this cleanup to ensure that we test the finalizer removal correctly. This breaks a lot of tests
		// and since we are time constraint, it was not possible to fix in this PR. The DPUNode finalizer removal is
		// also checked in e2e tests.
		By("Manually removing the DPUNode finalizer")
		dpuNodeFetched := &provisioningv1.DPUNode{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), dpuNodeFetched)).To(Succeed())
		patcher := patch.NewSerialPatcher(dpuNodeFetched, k8sClient)
		controllerutil.RemoveFinalizer(dpuNodeFetched, provisioningv1.DPUNodeFinalizer)
		Expect(patcher.Patch(ctx, dpuNodeFetched)).To(Succeed())
		By("Deleting the DPUNode")
		Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUNode)).To(Succeed())

		By("Cleaning the node")
		Expect(testutils.CleanupAndWait(ctx, k8sClient, testNode)).To(Succeed())

		By("deleting the dpucluster")
		Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUCluster)).To(Succeed())

		By("Cleaning the bfb")
		destroyBFB(ctx, testBFB)

		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("DPU: check state (Initializing) and destroy", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing for 10sec")
			Consistently(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(provisioningv1.DPUInitializing))
		})

		It("DPU: check state (Initializing) and fail with [DPUNodeNotFound]", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = "dummy-invalid-dpu-node"
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing [DPUNodeNotFound]")
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
					HaveField("Reason", "DPUNodeNotFound"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))
		})

		It("DPU: check state (Initializing) and fail with [DPUDeviceNotFound]", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = "dummy-invalid-dpu-device"
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing [DPUDeviceNotFound]")
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
					HaveField("Reason", "DPUDeviceNotFound"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))
		})

		It("DPU: check state (Initializing) and fail with [PCIAddressNotProvided]", func() {
			By("Setting DPUNode.Status.DPUInstallInterface to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing [PCIAddressNotProvided]")
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
					HaveField("Reason", "PCIAddressNotProvided"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))
		})

		It("DPU: check state (Initializing) and fail with [DPUOOBBridgeNotConfigured]", func() {
			By("Remove labels from DPUNode to simulate a non-existing OOB bridge")
			testDPUNode.SetLabels(nil)
			Expect(k8sClient.Update(ctx, testDPUNode)).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing [DPUOOBBridgeNotConfigured]")
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

			By("Add OOB label to DPUNode and it should move to the Status (DPUPending)")
			testDPUNodeFetched := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), testDPUNodeFetched)).To(Succeed())
			patcher := patch.NewSerialPatcher(testDPUNodeFetched, k8sClient)
			testDPUNodeFetched.SetLabels(map[string]string{
				cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
			})
			Expect(patcher.Patch(ctx, testDPUNodeFetched)).To(Succeed())

			Eventually(func(g Gomega) map[string]string {
				dpuNodeFetched := &provisioningv1.DPUNode{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testDPUNode.Namespace, Name: testDPUNode.Name}, dpuNodeFetched)).To(Succeed())
				return dpuNodeFetched.Labels
			}).WithTimeout(30 * time.Second).Should(HaveKey(cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel))

			By("expecting the Status: DPUReady")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(60 * time.Second).Should(Equal(provisioningv1.DPUReady))
		})

		It("DPU: check state (Initializing) and fail with [DPUClusterNotAllocated]", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = "dummy-invalid-namespace"
			obj.Spec.Cluster.Name = ""
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing [DPUClusterNotAllocated]")
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
					HaveField("Reason", "DPUClusterNotAllocated"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))
		})

		It("DPU: check state (Initializing) and fail with [DPUClusterNotFound]", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = "dummy-invalid-dpu-cluster"
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUInitializing [DPUClusterNotFound]")
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
					HaveField("Reason", "DPUClusterNotFound"),
				),
			))
			Expect(objFetched.Finalizers).Should(ConsistOf([]string{provisioningv1.DPUFinalizer}))
		})

		It("DPU: check state (Pending) w/o bfb object and fail with [BFBNotFound]", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = "dummy-not-existing-bfb"
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUPending [BFBNotFound]")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUPending))
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "BFBNotFound"),
				),
			))
		})

		It("DPU: check state (Pending) in case bfb object is not ready and fail with [BFBIsNotReady]", func() {
			By("creating the bfb")
			testBFBNotReady := createBFB(ctx, fmt.Sprintf("%s-not-ready", testBFB.Name), "https://example.com/oop.bfb", true)

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFBNotReady.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUPending [BFBIsNotReady]")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				objFetched = newObj
				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUPending))
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "BFBIsNotReady"),
				),
			))
		})

		It("DPU: check state (Node Effect) in mode NoEffect", func() {
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUNodeEffect")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(BeElementOf(
					provisioningv1.DPUNodeEffect,
					provisioningv1.DPUInitializeInterface,
				))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))
		})

		It("DPU: k8s env: check state (Node Effect) in mode Drain NodeMaintenance is not ready", func() {
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Drain: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("creating the DMS Pod")
			pod := createPod(ctx, cutil.GenerateDMSPodName(testNode.Name), obj)
			DeferCleanup(k8sClient.Delete, ctx, pod)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUNodeEffect")
			Eventually(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).Should(Equal(provisioningv1.DPUNodeEffect))

			By("getting stuck with Status: DPUNodeEffect for 10 sec (NodeMaintenance is not ready)")
			Consistently(func(g Gomega) provisioningv1.DPUPhase {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Status.Phase
			}).WithTimeout(10 * time.Second).Should(Equal(provisioningv1.DPUNodeEffect))
			Expect(objFetched.Status.Conditions).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeMaintenanceIsNotReady"),
				),
			))
		})

		It("DPU: k8s env: check state (Node Effect) in mode Drain NodeMaintenance is ready", func() {
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Drain: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("creating the DMS Pod")
			pod := createPod(ctx, cutil.GenerateDMSPodName(testNode.Name), obj)
			DeferCleanup(k8sClient.Delete, ctx, pod)

			By("creating the NodeMaintenance")
			node := createNodeMaintenance(ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUNodeEffect")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(BeElementOf(
					provisioningv1.DPUNodeEffect,
					provisioningv1.DPUInitializeInterface,
				))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))

			By("checking NodeMaintenance presence")
			Eventually(func(g Gomega) {
				nodeFetched := &maintenancev1alpha1.NodeMaintenance{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), nodeFetched)).To(Succeed())
			}).Should(Succeed())
		})

		It("DPU: k8s env: check state (Node Effect) in mode CustomLabel", func() {
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				CustomLabel: map[string]string{
					"provisioning.dpu.nvidia.com/bfb": "dummy.bfb",
					"version":                         "1.2.3",
				},
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("creating the DMS Pod")
			pod := createPod(ctx, cutil.GenerateDMSPodName(testNode.Name), obj)
			DeferCleanup(k8sClient.Delete, ctx, pod)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUNodeEffect")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(BeElementOf(
					provisioningv1.DPUNodeEffect,
					provisioningv1.DPUInitializeInterface,
				))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))

			By("checking the DPUNode`s Labels")
			dpuNodeFetched := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), dpuNodeFetched)).To(Succeed())
			Expect(dpuNodeFetched.Labels).To(HaveLen(3))
			Expect(dpuNodeFetched.Labels).To(HaveKeyWithValue(
				cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel, "true"))
			Expect(dpuNodeFetched.Labels).To(HaveKeyWithValue("provisioning.dpu.nvidia.com/bfb", "dummy.bfb"))
			Expect(dpuNodeFetched.Labels).To(HaveKeyWithValue("version", "1.2.3"))
		})

		It("DPU: k8s env: check state (Node Effect) in mode Taint", func() {
			// TODO: We need to find a better way to run this test.
			// This test was meant to ensure that the taint is added to the node.
			// Getting the node and checking the number of taints on the node is not a good way to check this.
			// As the DPU controller deletes the taint in NodeEffect from the Node during DPUReady phase, the test will never pass after then.
			// As a result, the result of this test highly depends on the DPU phase at the moment we get the node, which makes this test unstable.
			Skip("this test is unstable")
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			taintObj := corev1.Taint{
				Key:       "testTaint1",
				Value:     "value1",
				Effect:    corev1.TaintEffectNoSchedule,
				TimeAdded: nil,
			}
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Taint: &taintObj,
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("creating the DMS Pod")
			pod := createPod(ctx, cutil.GenerateDMSPodName(testNode.Name), obj)
			DeferCleanup(k8sClient.Delete, ctx, pod)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUNodeEffect")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(BeElementOf(
					provisioningv1.DPUNodeEffect,
					provisioningv1.DPUInitializeInterface,
				))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))

			By("checking the node`s Taints in case node initially w/o Taints")
			Eventually(func(g Gomega) {
				nodeFetched := &corev1.Node{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testNode), nodeFetched)).To(Succeed())

				// sometimes, the test fails due to the node has 0 taints.
				// This is because the DPU phase has reached DPUReady, and the taint in NodeEffect is removed from the node.
				// We need to find a better way to check the taint on Node.
				g.Expect(nodeFetched.Spec.Taints).To(HaveLen(1))
				g.Expect(nodeFetched.Spec.Taints[0]).Should(Equal(taintObj))
			}).Should(Succeed())
		})

		It("DPU: k8s env: check state (Node Effect) in mode Taint in case Taint duplication", func() {
			// TODO: We need to find a better way to run this test.
			// This test was meant to ensure that the duplicated taint is never added to the node.
			// Getting the node and checking the number of taints on the node is not a good way to check this.
			// As the DPU controller deletes the taint in NodeEffect from the Node during DPUReady phase, the test may still pass even if the duplicated taint is added to the node.
			// On the other hand, the test may fail due to the node has only 2 taints.
			// As a result, the result of this test highly depends on the DPU phase at the moment we get the node, which makes this test unstable.
			Skip("this test is unstable")
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			testNode.Spec.Taints = append(testNode.Spec.Taints, []corev1.Taint{
				{
					Key:       "testTaint1",
					Value:     "value1",
					Effect:    corev1.TaintEffectNoSchedule,
					TimeAdded: nil,
				},
				{
					Key:       "testTaint2",
					Value:     "value2",
					Effect:    corev1.TaintEffectNoSchedule,
					TimeAdded: nil,
				},
				{
					Key:       "testTaint3",
					Value:     "value3",
					Effect:    corev1.TaintEffectNoSchedule,
					TimeAdded: nil,
				},
			}...)
			taintObj := testNode.Spec.Taints[1]
			Expect(k8sClient.Update(ctx, testNode)).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Taint: &taintObj,
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUNodeEffect")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(BeElementOf(
					provisioningv1.DPUNodeEffect,
					provisioningv1.DPUInitializeInterface,
				))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))

			By("checking the node`s Taints in case node initially w/ Taints")
			Eventually(func(gomega Gomega) {
				nodeFetched := &corev1.Node{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testNode), nodeFetched)).To(Succeed())

				// sometimes, the test fails due to the node has only 2 taints.
				// This is because the DPU phase has reached DPUReady, and the taint in NodeEffect is removed from the node.
				// We need to find a better way to check the taint on Node.
				Expect(nodeFetched.Spec.Taints).To(HaveLen(3))
				Expect(nodeFetched.Spec.Taints[1]).Should(Equal(taintObj))
			}, 30*time.Second).Should(Succeed())
		})

		It("DPU: check state (Ready) in mode NoEffect", func() {
			// Check all conditions set in Ready state
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				NoEffect: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUReady with spinning in DPUClusterClientGetError")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUReady))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondInterfaceInitialized.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondFWConfigured.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondHostNetworkReady.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondBFBPrepared.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondOSInstalled.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondOSInstalled.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondRebooted.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondRebooted.String()),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUClusterClientGetError"),
				),
			))
		})

		It("DPU: k8s env: check state (Ready) in mode Drain", func() {
			// Cleanup NodeMaintenance in Ready state using mode Drain
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Drain: ptr.To(true),
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("creating the DMS Pod")
			pod := createPod(ctx, cutil.GenerateDMSPodName(testNode.Name), obj)
			DeferCleanup(k8sClient.Delete, ctx, pod)

			objFetched := &provisioningv1.DPU{}

			By("creating the NodeMaintenance")
			node := createNodeMaintenance(ctx, obj)

			By("expecting the Status: DPUReady with spinning in DPUClusterClientGetError")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUReady))
				objFetched = newObj
				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUClusterClientGetError"),
				),
			))

			By("checking NodeMaintenance absence")
			nodeFetched := &maintenancev1alpha1.NodeMaintenance{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), nodeFetched)).To(HaveOccurred())
		})

		It("DPU: k8s env: check state (Ready) in mode Taint", func() {
			By("Setting DPUNode.Status.KubeNodeRef to the testNode")
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUNode), fetchedDPUNode)).To(Succeed())
			fetchedDPUNode.Status.KubeNodeRef = &testNode.Name
			Expect(k8sClient.Status().Patch(ctx, fetchedDPUNode, client.MergeFrom(testDPUNode))).To(Succeed())

			// Cleanup Taint in Ready state using mode Taint
			By("creating the obj")
			obj := createObj("obj-dpu")
			obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
			obj.Spec.Cluster.Name = testDPUCluster.Name
			obj.Spec.DPUNodeName = testDPUNode.Name
			obj.Spec.DPUDeviceName = testDPUDevice.Name
			obj.Spec.BFB = testBFB.Name
			taintObj := corev1.Taint{
				Key:       "testTaint1",
				Value:     "value1",
				Effect:    corev1.TaintEffectNoSchedule,
				TimeAdded: nil,
			}
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Taint: &taintObj,
			}
			obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			By("creating the DMS Pod")
			pod := createPod(ctx, cutil.GenerateDMSPodName(testNode.Name), obj)
			DeferCleanup(k8sClient.Delete, ctx, pod)

			objFetched := &provisioningv1.DPU{}

			By("expecting the Status: DPUReady with spinning in DPUClusterClientGetError")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &provisioningv1.DPU{}
				newObj := &provisioningv1.DPU{}
				g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Phase).Should(Equal(provisioningv1.DPUReady))
				objFetched = newObj

				return objFetched.Status.Conditions
			}).WithTimeout(30 * time.Second).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUClusterClientGetError"),
				),
			))

			By("checking Taint absence")
			nodeFetched := &corev1.Node{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testNode), nodeFetched)).To(Succeed())
			Expect(nodeFetched.Spec.Taints).To(BeEmpty())
		})
	})
	It("DPU: test customAction job name is no more than 63 symbols", func() {
		obj := createObj(fmt.Sprintf("worker2-%s", strings.Repeat("0", 55)))
		nodeEffect := &provisioningv1.NodeEffect{
			CustomAction: ptr.To(fmt.Sprintf("dpu-%s", strings.Repeat("0", 59))),
		}

		Expect(len(state.GetCustomActionJobName(nodeEffect, obj))).Should(BeNumerically("<", 63))
	})
	It("DPU: check state (Node Effect) in mode CustomAction", func() {
		yml := []byte(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: dpu-custom-action
  namespace: %s
data:
  pod.yaml: |-
    apiVersion: v1
    kind: Pod
    metadata:
      name: dpf-test-pod
    spec:
      restartPolicy: "Never"
      nodeSelector: 
        noderole: control-plane
      containers:
        - name: dpp-test-container
          image: alpine
          command: ["/bin/sh"]
          args: ["-c", "echo 'DPF custom action' | tee /tmp/sucess "]`, testDPUCluster.Namespace))
		configMap := &corev1.ConfigMap{}
		err := yaml.UnmarshalStrict(yml, configMap)
		Expect(err).To(Succeed())
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		By("creating the obj")
		obj := createObj("worker2-0000-08-00")
		obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
		obj.Spec.Cluster.Name = testDPUCluster.Name
		obj.Spec.DPUNodeName = testDPUNode.Name
		obj.Spec.DPUDeviceName = testDPUDevice.Name
		obj.Spec.BFB = testBFB.Name
		obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
			CustomAction: ptr.To("dpu-custom-action"),
		}
		obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, obj)

		objFetched := &provisioningv1.DPU{}
		jobsBefore := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, jobsBefore)).To(Succeed())

		By("expecting the Status (NodeEffect)")
		Eventually(func(g Gomega) provisioningv1.DPUPhase {
			g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			return objFetched.Status.Phase
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUNodeEffect))

		Eventually(func(g Gomega) int {
			jobsAfter := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobsAfter)).To(Succeed())
			return len(jobsAfter.Items)
		}).WithTimeout(30 * time.Second).Should(Equal(len(jobsBefore.Items) + 1))

	})

	It("DPU: check state (Node Effect) in mode Hold", func() {
		By("creating the obj")
		obj := createObj("obj-dpu")
		obj.Spec.Cluster.Namespace = testDPUCluster.Namespace
		obj.Spec.Cluster.Name = testDPUCluster.Name
		obj.Spec.DPUNodeName = testDPUNode.Name
		obj.Spec.DPUDeviceName = testDPUDevice.Name
		obj.Spec.BFB = testBFB.Name
		obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
			Hold: ptr.To(true),
		}
		obj.Spec.PCIAddress = ptr.To(DefaultPCIAddress)
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, obj)

		objFetched := &provisioningv1.DPU{}

		By("expecting the Status (Node Effect)")
		Eventually(func(g Gomega) provisioningv1.DPUPhase {
			g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			return objFetched.Status.Phase
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(provisioningv1.DPUNodeEffect))

		Eventually(func(g Gomega) string {
			g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			g.Expect(objFetched.Annotations).To(HaveKey(cutil.HoldNodeEffectKey))
			return objFetched.Annotations[cutil.HoldNodeEffectKey]
		}).WithTimeout(30 * time.Second).Should(Equal("true"))

		By("updating the DPU to release the Hold")
		dpuObjFetched := &provisioningv1.DPU{}
		Expect(k8sClient.Get(ctx, getObjKey(obj), dpuObjFetched)).To(Succeed())
		patcher := patch.NewSerialPatcher(dpuObjFetched, k8sClient)
		dpuObjFetched.Annotations[cutil.HoldNodeEffectKey] = "false"
		Expect(patcher.Patch(ctx, dpuObjFetched)).To(Succeed())

		By("expecting the Status (Ready)")
		Eventually(func(g Gomega) provisioningv1.DPUPhase {
			g.Expect(k8sClient.Get(ctx, getObjKey(obj), dpuObjFetched)).To(Succeed())
			return dpuObjFetched.Status.Phase
		}).WithTimeout(30 * time.Second).Should(Equal(provisioningv1.DPUReady))
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

		It("DPUFlavor: create and get object minimal", func() {
			By("creating the obj-1")
			obj1 := createObj("obj-dpuflavor-1")
			err := k8sClient.Create(ctx, obj1)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUFlavor{}
			err = k8sClient.Get(ctx, getObjKey(obj1), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj1))

			data1, err := bfcfg.Generate(obj1, DefaultDPUName, "", false, "", string(provisioningv1.InstallViaGNOI), 1500, 2)
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

			data2, err := bfcfg.Generate(obj2, DefaultDPUName, "", false, "", string(provisioningv1.InstallViaGNOI), 1500, 2)
			Expect(err).To(Succeed())
			Expect(data2).ShouldNot(BeNil())

			By("compare the obj-1 and obj-2")
			Expect(data1).Should(Equal(data2))
		})

		It("DPUFlavor: create obj", func() {
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

			data, err := bfcfg.Generate(obj, DefaultDPUName, "", false, "", string(provisioningv1.InstallViaGNOI), 1500, 2)
			Expect(err).To(Succeed())
			Expect(data).ShouldNot(BeNil())
		})
	})
})
