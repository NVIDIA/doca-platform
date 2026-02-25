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
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/bfcfg"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
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
						NodeEffect: &provisioningv1.NodeEffect{
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
				dpu.Spec.BFB = testBFB.Name
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
				dpu.Spec.BFB = testBFB.Name
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
				dpu.Spec.BFB = testBFB.Name
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

			data1, err := bfcfg.Generate(obj1, DefaultDPUName, "", false, bfcfg.DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
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

			data2, err := bfcfg.Generate(obj2, DefaultDPUName, "", false, bfcfg.DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
			Expect(err).NotTo(HaveOccurred())
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
        - LINK_TYPE_P1=ETH
        - LINK_TYPE_P2=ETH
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

			data, err := bfcfg.Generate(obj, DefaultDPUName, "", false, bfcfg.DefaultBFCFGTemplateData, string(provisioningv1.InstallViaGNOI), 1500, 2)
			Expect(err).To(Succeed())
			Expect(data).ShouldNot(BeNil())
		})
	})
})
