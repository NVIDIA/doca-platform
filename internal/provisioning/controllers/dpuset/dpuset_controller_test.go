/*
  COPYRIGHT 2026 NVIDIA
  Licensed under the Apache License, Version 2.0 (the License);
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an AS IS BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
*/

package dpuset

import (
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "test-namespace"

var _ = Describe("DPUSetReconciler getDPUDeviceMap", func() {
	var (
		ctx        context.Context
		reconciler *DPUSetReconciler
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	})

	Context("when listing DPU devices", func() {
		It("should exclude DPU devices from deleted DPUNodes", func() {
			// Create test data
			namespace := testNamespace
			now := metav1.Now()

			// Create a normal DPUNode (not being deleted)
			normalDPUNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-dpunode",
					Namespace: namespace,
					Labels: map[string]string{
						"node-type": "normal",
					},
				},
			}

			// Create a DPUNode being deleted (has DeletionTimestamp and Finalizers)
			deletingDPUNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-dpunode",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"test-finalizer"},
					Labels: map[string]string{
						"node-type": "deleting",
					},
				},
			}

			// Create DPUDevices for the normal DPUNode
			normalDPUDevice1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-device-1",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "normal-dpunode",
					},
				},
			}

			normalDPUDevice2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-device-2",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "normal-dpunode",
					},
				},
			}

			// Create DPUDevices for the deleting DPUNode
			deletingDPUDevice1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleting-device-1",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "deleting-dpunode",
					},
				},
			}

			deletingDPUDevice2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleting-device-2",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "deleting-dpunode",
					},
				},
			}

			// Create DPUSet with no specific selectors (select all)
			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset",
					Namespace: namespace,
				},
				Spec: provisioningv1.DPUSetSpec{
					DPUNodeSelector:   nil, // Select all nodes
					DPUDeviceSelector: nil, // Select all devices
				},
			}

			// Create fake client with all objects
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(normalDPUNode, deletingDPUNode,
					normalDPUDevice1, normalDPUDevice2,
					deletingDPUDevice1, deletingDPUDevice2).
				Build()

			reconciler = &DPUSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Call getDPUDeviceMap
			dpuDeviceMap, err := reconciler.getDPUDeviceMap(ctx, dpuSet)

			// Assertions
			Expect(err).ToNot(HaveOccurred())
			// Should only contain devices from the normal DPUNode
			Expect(dpuDeviceMap).To(HaveLen(2))
			Expect(dpuDeviceMap).To(HaveKey("normal-device-1"))
			Expect(dpuDeviceMap).To(HaveKey("normal-device-2"))
			// Should NOT contain devices from the deleting DPUNode
			Expect(dpuDeviceMap).ToNot(HaveKey("deleting-device-1"))
			Expect(dpuDeviceMap).ToNot(HaveKey("deleting-device-2"))
		})

		It("should include DPU devices from all non-deleted DPUNodes when no selector is specified", func() {
			namespace := testNamespace

			// Create multiple normal DPUNodes
			dpuNode1 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpunode-1",
					Namespace: namespace,
				},
			}

			dpuNode2 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpunode-2",
					Namespace: namespace,
				},
			}

			// Create DPUDevices for both nodes
			device1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-1",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "dpunode-1",
					},
				},
			}

			device2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-2",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "dpunode-2",
					},
				},
			}

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset",
					Namespace: namespace,
				},
				Spec: provisioningv1.DPUSetSpec{
					DPUNodeSelector:   nil,
					DPUDeviceSelector: nil,
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuNode1, dpuNode2, device1, device2).
				Build()

			reconciler = &DPUSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			dpuDeviceMap, err := reconciler.getDPUDeviceMap(ctx, dpuSet)

			Expect(err).ToNot(HaveOccurred())
			Expect(dpuDeviceMap).To(HaveLen(2))
			Expect(dpuDeviceMap).To(HaveKey("device-1"))
			Expect(dpuDeviceMap).To(HaveKey("device-2"))
		})

		It("should return empty map when all DPUNodes are being deleted", func() {
			namespace := testNamespace
			now := metav1.Now()

			// Create only deleting DPUNodes
			deletingNode1 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-node-1",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"test-finalizer"},
				},
			}

			deletingNode2 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-node-2",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"test-finalizer"},
				},
			}

			// Create devices for the deleting nodes
			device1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-1",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "deleting-node-1",
					},
				},
			}

			device2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-2",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "deleting-node-2",
					},
				},
			}

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset",
					Namespace: namespace,
				},
				Spec: provisioningv1.DPUSetSpec{},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(deletingNode1, deletingNode2, device1, device2).
				Build()

			reconciler = &DPUSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			dpuDeviceMap, err := reconciler.getDPUDeviceMap(ctx, dpuSet)

			Expect(err).ToNot(HaveOccurred())
			// Should return empty map since all nodes are being deleted
			Expect(dpuDeviceMap).To(BeEmpty())
		})

		It("should respect DPUNodeSelector and exclude deleted nodes", func() {
			namespace := testNamespace
			now := metav1.Now()

			// Create a normal DPUNode with label
			normalNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "labeled-node",
					Namespace: namespace,
					Labels: map[string]string{
						"environment": "production",
					},
				},
			}

			// Create a deleting DPUNode with same label
			deletingNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-labeled-node",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"test-finalizer"},
					Labels: map[string]string{
						"environment": "production",
					},
				},
			}

			// Create devices
			normalDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-device",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "labeled-node",
					},
				},
			}

			deletingDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleting-device",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "deleting-labeled-node",
					},
				},
			}

			// Create DPUSet with node selector
			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset",
					Namespace: namespace,
				},
				Spec: provisioningv1.DPUSetSpec{
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "production",
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(normalNode, deletingNode, normalDevice, deletingDevice).
				Build()

			reconciler = &DPUSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			dpuDeviceMap, err := reconciler.getDPUDeviceMap(ctx, dpuSet)

			Expect(err).ToNot(HaveOccurred())
			// Should only include device from the normal node
			Expect(dpuDeviceMap).To(HaveLen(1))
			Expect(dpuDeviceMap).To(HaveKey("normal-device"))
			Expect(dpuDeviceMap).ToNot(HaveKey("deleting-device"))
		})

		It("should respect DPUDeviceSelector and exclude devices from deleted nodes", func() {
			namespace := testNamespace
			now := metav1.Now()

			normalNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-node",
					Namespace: namespace,
				},
			}

			deletingNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "deleting-node",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{"test-finalizer"},
				},
			}

			// Create devices with specific labels
			normalDeviceWithLabel := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-device-with-label",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "normal-node",
						"device-type":                   "accelerator",
					},
				},
			}

			normalDeviceWithoutLabel := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "normal-device-without-label",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "normal-node",
					},
				},
			}

			deletingDeviceWithLabel := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleting-device-with-label",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "deleting-node",
						"device-type":                   "accelerator",
					},
				},
			}

			// DPUSet with device selector
			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset",
					Namespace: namespace,
				},
				Spec: provisioningv1.DPUSetSpec{
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"device-type": "accelerator",
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(normalNode, deletingNode,
					normalDeviceWithLabel, normalDeviceWithoutLabel, deletingDeviceWithLabel).
				Build()

			reconciler = &DPUSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			dpuDeviceMap, err := reconciler.getDPUDeviceMap(ctx, dpuSet)

			Expect(err).ToNot(HaveOccurred())
			// Should only include the device with label from normal node
			Expect(dpuDeviceMap).To(HaveLen(1))
			Expect(dpuDeviceMap).To(HaveKey("normal-device-with-label"))
			// Should not include device without label or from deleting node
			Expect(dpuDeviceMap).ToNot(HaveKey("normal-device-without-label"))
			Expect(dpuDeviceMap).ToNot(HaveKey("deleting-device-with-label"))
		})

		It("should handle DPUNode with recent DeletionTimestamp", func() {
			namespace := testNamespace
			// Use a time that's very close to now to ensure the logic is based on
			// DeletionTimestamp existence, not time comparison
			recentTime := metav1.NewTime(time.Now().Add(-1 * time.Second))

			recentlyDeletedNode := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "recently-deleted-node",
					Namespace:         namespace,
					DeletionTimestamp: &recentTime,
					Finalizers:        []string{"test-finalizer"},
				},
			}

			device := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device",
					Namespace: namespace,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "recently-deleted-node",
					},
				},
			}

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset",
					Namespace: namespace,
				},
				Spec: provisioningv1.DPUSetSpec{},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(recentlyDeletedNode, device).
				Build()

			reconciler = &DPUSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			dpuDeviceMap, err := reconciler.getDPUDeviceMap(ctx, dpuSet)

			Expect(err).ToNot(HaveOccurred())
			// Should return empty map even for recently deleted node
			Expect(dpuDeviceMap).To(BeEmpty())
		})
	})
})

var _ = Describe("DPUSetReconciler needDisruptDPU", func() {
	var reconciler *DPUSetReconciler

	BeforeEach(func() {
		reconciler = &DPUSetReconciler{}
	})

	type testCase struct {
		dpuSetBFB      string
		dpuSetFlavor   string
		dpuSetSB       *bool
		dpuBFB         string
		dpuFlavor      string
		dpuSB          *bool
		expectedResult bool
	}

	DescribeTable("should detect immutable field changes that require DPU recreation",
		func(tc testCase) {
			dpuSet := provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:        provisioningv1.BFBReference{Name: tc.dpuSetBFB},
							DPUFlavor:  tc.dpuSetFlavor,
							SecureBoot: tc.dpuSetSB,
						},
					},
				},
			}
			dpu := provisioningv1.DPU{
				Spec: provisioningv1.DPUSpec{
					BFB:        tc.dpuBFB,
					DPUFlavor:  tc.dpuFlavor,
					SecureBoot: tc.dpuSB,
				},
			}
			Expect(reconciler.needDisruptDPU(dpuSet, dpu, nil)).To(Equal(tc.expectedResult))
		},
		Entry("no changes - all fields match", testCase{
			dpuSetBFB: "bfb-v1", dpuSetFlavor: "flavor-a", dpuSetSB: ptr.To(true),
			dpuBFB: "bfb-v1", dpuFlavor: "flavor-a", dpuSB: ptr.To(true),
			expectedResult: false,
		}),
		Entry("no changes - SecureBoot nil on both sides", testCase{
			dpuSetBFB: "bfb-v1", dpuSetFlavor: "flavor-a", dpuSetSB: nil,
			dpuBFB: "bfb-v1", dpuFlavor: "flavor-a", dpuSB: nil,
			expectedResult: false,
		}),
		Entry("BFB changed", testCase{
			dpuSetBFB: "bfb-v2", dpuSetFlavor: "flavor-a", dpuSetSB: nil,
			dpuBFB: "bfb-v1", dpuFlavor: "flavor-a", dpuSB: nil,
			expectedResult: true,
		}),
		Entry("DPUFlavor changed", testCase{
			dpuSetBFB: "bfb-v1", dpuSetFlavor: "flavor-b", dpuSetSB: nil,
			dpuBFB: "bfb-v1", dpuFlavor: "flavor-a", dpuSB: nil,
			expectedResult: true,
		}),
		Entry("SecureBoot changed from nil to non-nil", testCase{
			dpuSetBFB: "bfb-v1", dpuSetFlavor: "flavor-a", dpuSetSB: ptr.To(true),
			dpuBFB: "bfb-v1", dpuFlavor: "flavor-a", dpuSB: nil,
			expectedResult: true,
		}),
		Entry("SecureBoot value changed", testCase{
			dpuSetBFB: "bfb-v1", dpuSetFlavor: "flavor-a", dpuSetSB: ptr.To(false),
			dpuBFB: "bfb-v1", dpuFlavor: "flavor-a", dpuSB: ptr.To(true),
			expectedResult: true,
		}),
	)
})
