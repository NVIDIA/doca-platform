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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"
	nvipamv1 "github.com/nvidia/doca-platform/internal/nvipam/api/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("DPUServiceIPAM Controller", func() {
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		It("should successfully reconcile the DPUServiceIPAM", func() {
			By("Reconciling the created resource")
			dpuServiceIPAM := getMinimalDPUServiceIPAM(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAM)

			By("checking that finalizer is added")
			Eventually(func(g Gomega) []string {
				got := &dpuservicev1.DPUServiceIPAM{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceIPAM), got)).To(Succeed())
				return got.Finalizers
			}).WithTimeout(10 * time.Second).Should(ConsistOf([]string{dpuservicev1.DPUServiceIPAMFinalizer}))
		})
	})
	Context("When checking the behavior on the DPU cluster", func() {
		var testNS *corev1.Namespace
		var dpuClusterClient client.Client
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Adding fake kamaji cluster")
			dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
			kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)

			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)
			dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should reconcile NVIPAM IPPool in DPU cluster when ipv4Subnet is set", func() {
			By("Creating the DPUServiceIPAM resource")
			dpuServiceIPAM := getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAM.Name = "pool-1"
			dpuServiceIPAM.Spec.Labels = map[string]string{
				"some-label": "someValue",
			}
			dpuServiceIPAM.Spec.Annotations = map[string]string{
				"some-annot": "someValue",
			}
			dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.0.0/20",
				Gateway:        "192.168.0.1",
				PerNodeIPCount: 256,
				DefaultGateway: true,
				Routes:         []dpuservicev1.Route{{Dst: "5.5.5.0/16"}},
			}
			dpuServiceIPAM.Spec.NodeSelector = &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "some-key",
								Operator: corev1.NodeSelectorOpExists,
							},
						},
					},
				},
			}

			Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAM)

			Eventually(func(g Gomega) {
				got := &nvipamv1.IPPool{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, got)).To(Succeed())
				g.Expect(got.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-name", "pool-1"))
				g.Expect(got.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-namespace", testNS.Name))
				g.Expect(got.Labels).To(HaveKeyWithValue("some-label", "someValue"))
				g.Expect(got.Annotations).To(HaveKeyWithValue("some-annot", "someValue"))
				g.Expect(got.Spec.Subnet).To(Equal("192.168.0.0/20"))
				g.Expect(got.Spec.PerNodeBlockSize).To(Equal(256))
				g.Expect(got.Spec.Gateway).To(Equal("192.168.0.1"))
				g.Expect(got.Spec.DefaultGateway).To(BeTrue())
				g.Expect(got.Spec.Routes).To(ConsistOf([]nvipamv1.Route{
					{Dst: "5.5.5.0/16"},
				}))
				g.Expect(got.Spec.NodeSelector).To(BeComparableTo(&corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "some-key",
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				}))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Removing the DPUServiceIPAM resource")
			Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &nvipamv1.IPPoolList{}
				g.Expect(dpuClusterClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(BeEmpty())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		It("should delete only the NVIPAM IPPool related to the DPUServiceIPAM that is deleted", func() {
			By("Creating 2 DPUServiceIPAM resources")
			dpuServiceIPAMOne := getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAMOne.Name = "resource-1"
			dpuServiceIPAMOne.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.0.0/20",
				Gateway:        "192.168.0.1",
				PerNodeIPCount: 256,
			}
			Expect(testClient.Create(ctx, dpuServiceIPAMOne)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAMOne)

			dpuServiceIPAMTwo := getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAMTwo.Name = "resource-2"
			dpuServiceIPAMTwo.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.32.0/20",
				Gateway:        "192.168.32.1",
				PerNodeIPCount: 256,
			}
			Expect(testClient.Create(ctx, dpuServiceIPAMTwo)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAMTwo)

			By("Removing the DPUServiceIPAM resource")
			Expect(testClient.Delete(ctx, dpuServiceIPAMOne)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &nvipamv1.IPPoolList{}
				g.Expect(dpuClusterClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(ConsistOf(
					HaveField("ObjectMeta.Name", "resource-2"),
				))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		It("should reconcile NVIPAM CIDRPool in DPU cluster when ipv4Network is set", func() {
			By("Creating the DPUServiceIPAM resource")
			dpuServiceIPAM := getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAM.Name = "pool-1"
			dpuServiceIPAM.Spec.Labels = map[string]string{
				"some-label": "someValue",
			}
			dpuServiceIPAM.Spec.Annotations = map[string]string{
				"some-annot": "someValue",
			}
			dpuServiceIPAM.Spec.IPV4Network = &dpuservicev1.IPV4Network{
				Network:      "192.168.0.0/20",
				GatewayIndex: ptr.To[int32](1),
				PrefixSize:   24,
				Exclusions:   []string{"192.168.0.1", "192.168.0.2"},
				Allocations: map[string]string{
					"node-1": "192.168.1.0/24",
					"node-2": "192.168.2.0/24",
				},
				DefaultGateway: true,
				Routes:         []dpuservicev1.Route{{Dst: "5.5.5.0/16"}},
			}
			dpuServiceIPAM.Spec.NodeSelector = &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "some-key",
								Operator: corev1.NodeSelectorOpExists,
							},
						},
					},
				},
			}

			Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAM)

			Eventually(func(g Gomega) {
				got := &nvipamv1.CIDRPool{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, got)).To(Succeed())
				g.Expect(got.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-name", "pool-1"))
				g.Expect(got.Labels).To(HaveKeyWithValue("dpu.nvidia.com/dpuserviceipam-namespace", testNS.Name))
				g.Expect(got.Labels).To(HaveKeyWithValue("some-label", "someValue"))
				g.Expect(got.Annotations).To(HaveKeyWithValue("some-annot", "someValue"))
				g.Expect(got.Spec.CIDR).To(Equal("192.168.0.0/20"))
				g.Expect(got.Spec.PerNodeNetworkPrefix).To(Equal(int32(24)))
				g.Expect(got.Spec.GatewayIndex).To(Equal(ptr.To[int32](1)))
				g.Expect(got.Spec.Exclusions).To(ConsistOf([]nvipamv1.ExcludeRange{
					{StartIP: "192.168.0.1", EndIP: "192.168.0.1"},
					{StartIP: "192.168.0.2", EndIP: "192.168.0.2"},
				}))
				g.Expect(got.Spec.StaticAllocations).To(ConsistOf([]nvipamv1.CIDRPoolStaticAllocation{
					{NodeName: "node-1", Prefix: "192.168.1.0/24"},
					{NodeName: "node-2", Prefix: "192.168.2.0/24"},
				}))
				g.Expect(got.Spec.DefaultGateway).To(BeTrue())
				g.Expect(got.Spec.Routes).To(ConsistOf([]nvipamv1.Route{
					{Dst: "5.5.5.0/16"},
				}))
				g.Expect(got.Spec.NodeSelector).To(BeComparableTo(&corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "some-key",
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				}))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Removing the DPUServiceIPAM resource")
			Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &nvipamv1.CIDRPoolList{}
				g.Expect(dpuClusterClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(BeEmpty())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		It("should remove NVIPAM IPPool in DPU cluster when ipv4Subnet is updated to unset and ipv4Network is set", func() {
			By("Creating the DPUServiceIPAM resource")
			dpuServiceIPAM := getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAM.Name = "pool-1"
			dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.0.0/20",
				Gateway:        "192.168.0.1",
				PerNodeIPCount: 256,
			}
			dpuServiceIPAM.SetManagedFields(nil)
			dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
			// FieldOwner must be the same as the controller so that we can set a field to nil later
			Expect(testClient.Patch(ctx, dpuServiceIPAM, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceIPAMControllerName))).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAM)

			Eventually(func(g Gomega) {
				gotCIDRPool := &nvipamv1.CIDRPool{}
				g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotCIDRPool))).To(BeTrue())
				gotIPPool := &nvipamv1.IPPool{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Updating the spec to unset ipv4Subnet and set ipv4Network")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceIPAM), dpuServiceIPAM)).To(Succeed())
				dpuServiceIPAM.Spec.IPV4Subnet = nil
				dpuServiceIPAM.Spec.IPV4Network = &dpuservicev1.IPV4Network{
					Network:      "192.168.0.0/20",
					GatewayIndex: ptr.To[int32](1),
					PrefixSize:   24,
				}
				dpuServiceIPAM.SetManagedFields(nil)
				dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
				// FieldOwner must be the same as the controller because we modify a field that the controller owns (see
				// defer in the main reconcile function)
				g.Expect(testClient.Patch(ctx, dpuServiceIPAM, client.Apply, client.FieldOwner(dpuServiceIPAMControllerName))).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				gotIPPool := &nvipamv1.IPPool{}
				g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool))).To(BeTrue())
				gotCIDRPool := &nvipamv1.CIDRPool{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotCIDRPool)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		It("should remove NVIPAM CIDRPool in DPU cluster when ipv4Network is updated to unset and ipv4Subnet is set", func() {
			By("Creating the DPUServiceIPAM resource")
			dpuServiceIPAM := getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAM.Name = "pool-1"
			dpuServiceIPAM.Spec.IPV4Network = &dpuservicev1.IPV4Network{
				Network:      "192.168.0.0/20",
				GatewayIndex: ptr.To[int32](1),
				PrefixSize:   24,
			}

			dpuServiceIPAM.SetManagedFields(nil)
			dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
			// FieldOwner must be the same as the controlller so that we can set a field to nil later
			Expect(testClient.Patch(ctx, dpuServiceIPAM, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceIPAMControllerName))).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAM)

			Eventually(func(g Gomega) {
				gotIPPool := &nvipamv1.IPPool{}
				g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool))).To(BeTrue())
				gotCIDRPool := &nvipamv1.CIDRPool{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotCIDRPool)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Updating the spec to unset ipv4Network and set ipv4Subnet")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceIPAM), dpuServiceIPAM)).To(Succeed())
				dpuServiceIPAM.Spec.IPV4Network = nil
				dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
					Subnet:         "192.168.0.0/20",
					Gateway:        "192.168.0.1",
					PerNodeIPCount: 256,
				}

				dpuServiceIPAM.SetManagedFields(nil)
				dpuServiceIPAM.SetGroupVersionKind(dpuservicev1.DPUServiceIPAMGroupVersionKind)
				// FieldOwner must be the same as the controller because we modify a field that the controller owns (see
				// defer in the main reconcile function)
				g.Expect(testClient.Patch(ctx, dpuServiceIPAM, client.Apply, client.FieldOwner(dpuServiceIPAMControllerName))).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				gotCIDRPool := &nvipamv1.CIDRPool{}
				g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotCIDRPool))).To(BeTrue())
				gotIPPool := &nvipamv1.IPPool{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
	})
	Context("When checking the status transitions", func() {
		var (
			testNS           *corev1.Namespace
			dpuServiceIPAM   *dpuservicev1.DPUServiceIPAM
			dpuCluster       provisioningv1.DPUCluster
			kamajiSecret     *corev1.Secret
			dpuClusterClient client.Client
			i                *informer.TestInformer
		)

		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Adding fake kamaji cluster")
			dpuCluster = testutils.GetTestDPUCluster(testNS.Name, "envtest")
			var err error
			kamajiSecret, err = testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)

			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)
			dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("Creating the informer infrastructure for DPUServiceIPAM")
			i = informer.NewInformer(cfg, dpuservicev1.DPUServiceIPAMGroupVersionKind, testNS.Name, "dpuserviceipams")
			DeferCleanup(i.Cleanup)
			go i.Run()

			By("Creating a DPUServiceIPAM")
			dpuServiceIPAM = getMinimalDPUServiceIPAM(testNS.Name)
			dpuServiceIPAM.Name = "pool-1"
			dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.0.0/20",
				Gateway:        "192.168.0.1",
				PerNodeIPCount: 256,
			}
			Expect(testClient.Create(ctx, dpuServiceIPAM)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceIPAM)

		})
		It("DPUServiceIPAM has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceIPAM{}
				newObj := &dpuservicev1.DPUServiceIPAM{}
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
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				// We have success here because there is no object in the cluster to watch on
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUServiceIPAM has condition DPUIPAMObjectReconciled with Success Reason at end of successful reconciliation loop but DPUIPAMObjectReady with Pending reason on underlying object not ready", func() {
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceIPAM{}
				newObj := &dpuservicev1.DPUServiceIPAM{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
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
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceIPAM has all conditions with Success Reason at end of successful reconciliation loop and underlying object ready", func() {
			By("Patching the status of the underlying object to indicate success")
			gotIPPool := &nvipamv1.IPPool{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool).Should(Succeed())
			gotIPPool.Status.Allocations = []nvipamv1.Allocation{
				{
					NodeName: "bla",
					StartIP:  "10.0.0.1",
					EndIP:    "10.0.0.2",
				},
			}
			gotIPPool.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(nvipamv1.IPPoolKind))
			gotIPPool.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, gotIPPool, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceIPAM{}
				newObj := &dpuservicev1.DPUServiceIPAM{}
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
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUServiceIPAM has condition DPUIPAMObjectReconciled with Error Reason at the end of first reconciliation loop that failed", func() {
			By("Setting the DPUCluster to an invalid state")
			Expect(testClient.Delete(ctx, kamajiSecret)).To(Succeed())

			DeferCleanup(func() {
				By("Reverting the DPUCluster to ready to ensure DPUServiceIPAM deletion can be done")
				kamajiSecret.ResourceVersion = ""
				Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			})

			By("Checking condition")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceIPAM{}
				newObj := &dpuservicev1.DPUServiceIPAM{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
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
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonError)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceIPAM has condition Deleting with ObjectsExistInDPUClusters Reason when there are still objects in the DPUCluster", func() {
			By("Ensuring that the DPUServiceIPAM has been reconciled successfully")
			Eventually(func(g Gomega) []metav1.Condition {
				got := &dpuservicev1.DPUServiceIPAM{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceIPAM), got)).To(Succeed())
				return got.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))

			By("Adding finalizer to the underlying object")
			gotIPPool := &nvipamv1.IPPool{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool).Should(Succeed())
			gotIPPool.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
			gotIPPool.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(nvipamv1.IPPoolKind))
			gotIPPool.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, gotIPPool, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Deleting the DPUServiceIPAM")
			Expect(testClient.Delete(ctx, dpuServiceIPAM)).To(Succeed())

			By("Checking the deleted condition is added")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceIPAM{}
				newObj := &dpuservicev1.DPUServiceIPAM{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionDPUIPAMObjectReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))

			By("Removing finalizer from the underlying object to ensure deletion")
			gotIPPool = &nvipamv1.IPPool{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "pool-1"}, gotIPPool).Should(Succeed())
			gotIPPool.SetFinalizers([]string{})
			gotIPPool.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(nvipamv1.IPPoolKind))
			gotIPPool.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, gotIPPool, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			// Trigger reconcile to avoid waiting the duration we have specified when objects are not yet deleted in the
			// underlying cluster.
			// TODO: consider if there's ways to speed up this reconcile.
			Eventually(func(g Gomega) {
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuServiceIPAM)).To(Succeed())
			}).Should(Succeed())
		})
	})
})

func getMinimalDPUServiceIPAM(namespace string) *dpuservicev1.DPUServiceIPAM {
	return &dpuservicev1.DPUServiceIPAM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpuserviceipam",
			Namespace: namespace,
		},
	}
}
