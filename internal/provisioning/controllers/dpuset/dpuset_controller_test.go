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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUNodeSelector:   nil, // Select all nodes
					DPUDeviceSelector: nil, // Select all devices
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
						},
					},
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
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUNodeSelector:   nil,
					DPUDeviceSelector: nil,
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
						},
					},
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
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
						},
					},
				},
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
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "production",
						},
					},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
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
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"device-type": "accelerator",
						},
					},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
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
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
						},
					},
				},
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

var _ = Describe("DPUSetReconciler BlueFieldSoftware finalizer", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	const bfsName = "test-bfs"

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	})

	newDPUSet := func() *provisioningv1.DPUSet {
		return &provisioningv1.DPUSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpuset",
				Namespace: testNamespace,
			},
			Spec: provisioningv1.DPUSetSpec{
				DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{
						DPUFlavor:         "test-flavor",
						BlueFieldSoftware: &provisioningv1.BlueFieldSoftwareReference{Name: bfsName},
						Cluster:           &provisioningv1.ClusterSpec{},
						NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				},
			},
		}
	}

	Context("createDPU", func() {
		It("should add the finalizer to the referenced BlueFieldSoftware and set it on the DPU", func() {
			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{Name: bfsName, Namespace: testNamespace},
			}
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-1",
					Namespace: testNamespace,
					Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "node-1"},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bfs, dpuDevice).
				Build()
			reconciler := &DPUSetReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(reconciler.createDPU(ctx, newDPUSet(), dpuDevice)).To(Succeed())

			updatedBFS := &provisioningv1.BlueFieldSoftware{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(bfs), updatedBFS)).To(Succeed())
			Expect(updatedBFS.Finalizers).To(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer))

			dpuList := &provisioningv1.DPUList{}
			Expect(fakeClient.List(ctx, dpuList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(dpuList.Items).To(HaveLen(1))
			Expect(dpuList.Items[0].Spec.BlueFieldSoftware).To(HaveValue(Equal(bfsName)))
		})

		It("should return an error when the referenced BlueFieldSoftware does not exist", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "device-1",
					Namespace: testNamespace,
					Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "node-1"},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dpuDevice).
				Build()
			reconciler := &DPUSetReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(reconciler.createDPU(ctx, newDPUSet(), dpuDevice)).To(HaveOccurred())
		})
	})

	Context("reconcileDelete", func() {
		It("should remove the finalizer from the referenced BlueFieldSoftware once all DPUs are gone", func() {
			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bfsName,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BlueFieldSoftwareFinalizer},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bfs).
				Build()
			reconciler := &DPUSetReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(reconciler.reconcileDelete(ctx, newDPUSet())).To(Succeed())

			updatedBFS := &provisioningv1.BlueFieldSoftware{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(bfs), updatedBFS)).To(Succeed())
			Expect(updatedBFS.Finalizers).ToNot(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer))
		})

		It("should skip gracefully when the BlueFieldSoftware is already gone", func() {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()
			reconciler := &DPUSetReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(reconciler.reconcileDelete(ctx, newDPUSet())).To(Succeed())
		})

		It("should keep the finalizer when another DPUSet still references the same BlueFieldSoftware", func() {
			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bfsName,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BlueFieldSoftwareFinalizer},
				},
			}
			otherDPUSet := newDPUSet()
			otherDPUSet.Name = "other-dpuset"

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bfs, otherDPUSet).
				Build()
			reconciler := &DPUSetReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(reconciler.reconcileDelete(ctx, newDPUSet())).To(Succeed())

			updatedBFS := &provisioningv1.BlueFieldSoftware{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(bfs), updatedBFS)).To(Succeed())
			Expect(updatedBFS.Finalizers).To(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer))
		})

		It("should not touch the BlueFieldSoftware finalizer while owned DPUs still exist", func() {
			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bfsName,
					Namespace:  testNamespace,
					Finalizers: []string{provisioningv1.BlueFieldSoftwareFinalizer},
				},
			}
			ownedDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "owned-dpu",
					Namespace: testNamespace,
					Labels: map[string]string{
						cutil.DPUSetNameLabel:      "test-dpuset",
						cutil.DPUSetNamespaceLabel: testNamespace,
					},
					// Keep the DPU around after the delete call so cleanup is still pending.
					Finalizers: []string{"test-finalizer"},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(bfs, ownedDPU).
				Build()
			reconciler := &DPUSetReconciler{
				Client:   fakeClient,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(reconciler.reconcileDelete(ctx, newDPUSet())).To(HaveOccurred())

			updatedBFS := &provisioningv1.BlueFieldSoftware{}
			Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(bfs), updatedBFS)).To(Succeed())
			Expect(updatedBFS.Finalizers).To(ContainElement(provisioningv1.BlueFieldSoftwareFinalizer))
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
							BFB:        &provisioningv1.BFBReference{Name: tc.dpuSetBFB},
							DPUFlavor:  tc.dpuSetFlavor,
							SecureBoot: tc.dpuSetSB,
						},
					},
				},
			}
			dpu := provisioningv1.DPU{
				Spec: provisioningv1.DPUSpec{
					BFB:        ptr.To(tc.dpuBFB),
					DPUFlavor:  tc.dpuFlavor,
					SecureBoot: tc.dpuSB,
				},
			}
			Expect(reconciler.needDisruptDPU(dpuSet, dpu, nil, templateEval{})).To(Equal(tc.expectedResult))
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

	DescribeTable("BF4 DPUSet with BlueFieldSoftware only (no BFB)",
		func(dpuSetBFS, dpuBFS string, expectedResult bool) {
			dpuSet := provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BlueFieldSoftware: &provisioningv1.BlueFieldSoftwareReference{Name: dpuSetBFS},
							DPUFlavor:         "bf4-flavor",
						},
					},
				},
			}
			dpu := provisioningv1.DPU{
				Spec: provisioningv1.DPUSpec{
					BlueFieldSoftware: ptr.To(dpuBFS),
					DPUFlavor:         "bf4-flavor",
				},
			}
			Expect(reconciler.needDisruptDPU(dpuSet, dpu, nil, templateEval{})).To(Equal(expectedResult))
		},
		Entry("no changes - BlueFieldSoftware matches, BFB omitted", "bfsw-v1", "bfsw-v1", false),
		Entry("BlueFieldSoftware changed", "bfsw-v2", "bfsw-v1", true),
		Entry("DPU missing BlueFieldSoftware", "bfsw-v1", "", true),
	)
})

var _ = Describe("DPUSetReconciler rolloutRolling", func() {
	var reconciler *DPUSetReconciler

	BeforeEach(func() {
		reconciler = &DPUSetReconciler{}
	})

	DescribeTable("should handle nil RollingUpdate and MaxUnavailable fields",
		func(strategy provisioningv1.DPUSetStrategy, expectErr bool) {
			dpuSet := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: strategy,
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							DPUFlavor:  "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
						},
					},
				},
			}
			err := reconciler.rolloutRolling(context.Background(), dpuSet, map[string]provisioningv1.DPU{}, 0, nil, nil)
			if expectErr {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("nil RollingUpdate", provisioningv1.DPUSetStrategy{
			Type: provisioningv1.RollingUpdateStrategyType,
		}, false),
		Entry("nil MaxUnavailable", provisioningv1.DPUSetStrategy{
			Type:          provisioningv1.RollingUpdateStrategyType,
			RollingUpdate: &provisioningv1.RollingUpdateDPU{},
		}, false),
		Entry("explicit MaxUnavailable", provisioningv1.DPUSetStrategy{
			Type: provisioningv1.RollingUpdateStrategyType,
			RollingUpdate: &provisioningv1.RollingUpdateDPU{
				MaxUnavailable: ptr.To(intstr.FromInt(2)),
			},
		}, false),
		Entry("invalid MaxUnavailable string", provisioningv1.DPUSetStrategy{
			Type: provisioningv1.RollingUpdateStrategyType,
			RollingUpdate: &provisioningv1.RollingUpdateDPU{
				MaxUnavailable: ptr.To(intstr.FromString("invalid")),
			},
		}, true),
	)
})

var _ = Describe("DPUSetReconciler collision labels", func() {
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

	Context("DPU owned by another DPUSet", func() {
		const collisionDPUSetName = "colliding-dpuset"

		Context("cleanupCollisionLabels", func() {
			It("should remove the collision label from every DPU that has it", func() {
				collisionKey := cutil.GenerateDPUSetCollisionLabelKey(collisionDPUSetName)

				dpuWithCollision1 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-with-collision-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
							collisionKey:          "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-1",
					},
				}

				dpuWithCollision2 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-with-collision-2",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
							collisionKey:          "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-2",
					},
				}

				dpuWithoutCollision := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-without-collision",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-3",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpuWithCollision1, dpuWithCollision2, dpuWithoutCollision).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.cleanupCollisionLabels(ctx, dpuSet)).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuWithCollision1), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))

				dpu = &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuWithCollision2), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))

				dpu = &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuWithoutCollision), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))
				Expect(dpu.Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, "owner-dpuset"))
			})

			It("should succeed without modifying DPUs when none have the collision label", func() {
				dpuNoLabel := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-no-collision",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-1",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpuNoLabel).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.cleanupCollisionLabels(ctx, dpuSet)).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuNoLabel), dpu)).To(Succeed())
				Expect(dpu.Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, "owner-dpuset"))
			})

			It("should only remove collision labels in the DPUSet's namespace", func() {
				collisionKey := cutil.GenerateDPUSetCollisionLabelKey(collisionDPUSetName)

				dpuSameNamespace := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-same-ns",
						Namespace: testNamespace,
						Labels: map[string]string{
							collisionKey: "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-1",
					},
				}

				dpuOtherNamespace := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-other-ns",
						Namespace: "other-namespace",
						Labels: map[string]string{
							collisionKey: "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-2",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpuSameNamespace, dpuOtherNamespace).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.cleanupCollisionLabels(ctx, dpuSet)).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuSameNamespace), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))

				dpu = &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuOtherNamespace), dpu)).To(Succeed())
				Expect(dpu.Labels).To(HaveKeyWithValue(collisionKey, "true"))
			})
		})

		Context("removeStaleCollisionLabels", func() {
			It("should remove collision label from DPUs whose device is no longer targeted", func() {
				collisionKey := cutil.GenerateDPUSetCollisionLabelKey(collisionDPUSetName)

				dpuStale := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-stale",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
							collisionKey:          "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-no-longer-targeted",
					},
				}

				dpuStillTargeted := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-still-targeted",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
							collisionKey:          "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-still-targeted",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				dpuDeviceMap := map[string]provisioningv1.DPUDevice{
					"device-still-targeted": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "device-still-targeted",
							Namespace: testNamespace,
						},
					},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpuStale, dpuStillTargeted).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.removeStaleCollisionLabels(ctx, dpuSet, dpuDeviceMap)).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuStale), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))
				Expect(dpu.Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, "owner-dpuset"))

				dpu = &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuStillTargeted), dpu)).To(Succeed())
				Expect(dpu.Labels).To(HaveKeyWithValue(collisionKey, "true"))
			})

			It("should remove all collision labels when dpuDeviceMap is empty", func() {
				collisionKey := cutil.GenerateDPUSetCollisionLabelKey(collisionDPUSetName)

				dpu1 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							collisionKey: "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-1",
					},
				}

				dpu2 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-2",
						Namespace: testNamespace,
						Labels: map[string]string{
							collisionKey: "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-2",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpu1, dpu2).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.removeStaleCollisionLabels(ctx, dpuSet, map[string]provisioningv1.DPUDevice{})).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu1), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))

				dpu = &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu2), dpu)).To(Succeed())
				Expect(dpu.Labels).NotTo(HaveKey(collisionKey))
			})

			It("should succeed without modifying DPUs when none have the collision label", func() {
				dpuNoLabel := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-no-collision",
						Namespace: testNamespace,
						Labels: map[string]string{
							cutil.DPUSetNameLabel: "owner-dpuset",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-1",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				dpuDeviceMap := map[string]provisioningv1.DPUDevice{
					"device-1": {},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpuNoLabel).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.removeStaleCollisionLabels(ctx, dpuSet, dpuDeviceMap)).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpuNoLabel), dpu)).To(Succeed())
				Expect(dpu.Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, "owner-dpuset"))
			})

			It("should keep collision labels when all labeled DPUs are still targeted", func() {
				collisionKey := cutil.GenerateDPUSetCollisionLabelKey(collisionDPUSetName)

				dpu1 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							collisionKey: "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-1",
					},
				}

				dpu2 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpu-2",
						Namespace: testNamespace,
						Labels: map[string]string{
							collisionKey: "true",
						},
					},
					Spec: provisioningv1.DPUSpec{
						DPUDeviceName: "device-2",
					},
				}

				dpuSet := &provisioningv1.DPUSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      collisionDPUSetName,
						Namespace: testNamespace,
					},
				}

				dpuDeviceMap := map[string]provisioningv1.DPUDevice{
					"device-1": {},
					"device-2": {},
				}

				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(dpu1, dpu2).
					Build()

				reconciler = &DPUSetReconciler{
					Client: fakeClient,
					Scheme: scheme,
				}

				Expect(reconciler.removeStaleCollisionLabels(ctx, dpuSet, dpuDeviceMap)).To(Succeed())

				dpu := &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu1), dpu)).To(Succeed())
				Expect(dpu.Labels).To(HaveKeyWithValue(collisionKey, "true"))

				dpu = &provisioningv1.DPU{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(dpu2), dpu)).To(Succeed())
				Expect(dpu.Labels).To(HaveKeyWithValue(collisionKey, "true"))
			})
		})
	})
})
