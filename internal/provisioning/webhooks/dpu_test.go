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

package webhooks

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

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
				Namespace: "default",
			},
			Spec:   provisioningv1.DPUSpec{DPUDeviceName: "dpudevice-1", SerialNumber: "MT25066004C7", DPUFlavor: "dummy-flavor"},
			Status: provisioningv1.DPUStatus{},
		}
	}

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("create and get object", func() {
			obj := createObj("obj-1")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("delete object", func() {
			obj := createObj("obj-2")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("update object", func() {
			obj := createObj("obj-3")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("spec.nodeEffect default", func() {
			obj := createObj("obj-4")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.NodeEffect.IsDrain()).To(BeTrue())
		})

		It("spec.nodeEffect assign nil", func() {
			refValue := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					CustomLabel: map[string]string{
						"foo": "bar",
					},
					Force: ptr.To(false),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(false),
				},
			}

			obj := createObj("obj-node-effect-nil")
			obj.Spec.NodeEffect = &refValue
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(*objFetched.Spec.NodeEffect).To(Equal(refValue))

			// Assigning a nil value sets the value to the default value.
			obj.Spec.NodeEffect = nil
			Expect(k8sClient.Update(ctx, obj)).To(Succeed())
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(*objFetched.Spec.NodeEffect).To(Equal(provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true), Force: ptr.To(false)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: ptr.To(false)}}))
		})

		It("spec.nodeEffect.applyOnLabelChange defaults to false", func() {
			obj := createObj("obj-apply-on-label-change-default")
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				// ApplyOnLabelChange not set, should default to false
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
		})

		It("spec.nodeEffect.applyOnLabelChange is mutable", func() {
			obj := createObj("obj-apply-on-label-change-mutable")
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(false),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			// Update the field using Patch
			patch := client.MergeFrom(obj.DeepCopy())
			obj.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
			Expect(k8sClient.Patch(ctx, obj, patch)).To(Succeed())

			objFetched := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
		})

		It("spec.nodeEffect.nodeMaintenanceAdditionalRequestors is mutable", func() {
			obj := createObj("obj-additional-requestors-mutable")
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					NodeMaintenanceAdditionalRequestors: []string{"req-1"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			// Update the field using Patch
			patch := client.MergeFrom(obj.DeepCopy())
			obj.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{"req-1", "req-2"}
			Expect(k8sClient.Patch(ctx, obj, patch)).To(Succeed())

			objFetched := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(HaveLen(2))
			Expect(objFetched.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(ContainElements("req-1", "req-2"))
		})

		It("only one field may be set in spec.nodeEffect", func() {
			obj := createObj("checking-node-effect")
			// Error when creating a DPU with a nodeEffect setting taint and customLabel.
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "foo",
						Effect: corev1.TaintEffectNoSchedule,
					},
					CustomLabel: map[string]string{
						"foo": "bar",
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())

			// Error when creating a DPU with a nodeEffect setting taint and drain.
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "foo",
						Effect: corev1.TaintEffectNoSchedule,
					},
					Drain: ptr.To(true),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())

			// Error when creating a DPU with a nodeeffect setting Drain and NoEffect
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Drain: ptr.To(true),
					CustomLabel: map[string]string{
						"foo": "bar",
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())

			// Error when creating a DPU with a nodeeffect setting Drain and NoEffect
			obj.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
					CustomLabel: map[string]string{
						"foo": "bar",
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())
		})

		It("spec.dpuNodeName is immutable", func() {
			refValue := "dummy_node"

			obj := createObj("obj-5")
			obj.Spec.DPUNodeName = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.DPUNodeName = "dummy_new_node"
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.DPUNodeName).To(Equal(refValue))
		})

		It("spec.serialNumber is immutable", func() {
			obj := createObj("obj-serial-number-immutable")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			refValue := obj.Spec.SerialNumber

			updatedValue := refValue + "1"
			obj.Spec.SerialNumber = updatedValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.SerialNumber).To(Equal(refValue))
		})

		It("spec.pciAddress is mutable", func() {
			refValue := "0000-aa-00"

			obj := createObj("obj-6")
			obj.Spec.PCIAddress = ptr.To(refValue)
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			updatedValue := "0000-bb-00"
			obj.Spec.PCIAddress = &updatedValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(*objFetched.Spec.PCIAddress).To(Equal(updatedValue))
		})

		It("spec.DPUDeviceName is immutable", func() {

			obj := createObj("obj-7")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.DPUDeviceName = "dummy_new_device"
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.DPUDeviceName).To(Equal("dpudevice-1"))
		})

		It("spec.DPUFlavor is immutable", func() {
			refValue := "initial-flavor"

			obj := createObj("obj-dpuflavor-immutable")
			obj.Spec.DPUFlavor = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.DPUFlavor = "updated-flavor"
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.DPUFlavor).To(Equal(refValue))
		})

		It("spec.Cluster is immutable once assigned", func() {
			refValue := `dummy_cluster`
			newValue := `dummy_new_cluster`
			ns := "default"

			obj := createObj("obj-8")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.Cluster = provisioningv1.K8sCluster{
				Name:      refValue,
				Namespace: ns,
			}
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.Cluster.Name = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Cluster.Name).To(Equal(refValue))
			Expect(objFetched.Spec.Cluster.Namespace).To(Equal(ns))
		})

		It("spec.cluster can be updated from unassigned state", func() {
			newValueName := `dummy_new_cluster_name`
			newValueNamespace := `dummy_new_cluster_namespace`

			obj := createObj("obj-dpu")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Cluster.Name).To(Equal(""))
			Expect(objFetched.Spec.Cluster.Namespace).To(Equal(""))

			obj.Spec.Cluster.Name = newValueName
			obj.Spec.Cluster.Namespace = newValueNamespace
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Cluster.Name).To(Equal(newValueName))
			Expect(objFetched.Spec.Cluster.Namespace).To(Equal(newValueNamespace))
		})

		It("create from yaml", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPU
metadata:
  name: obj-9
  namespace: default
spec:
  dpuNodeName: "dpu-bf2"
  dpuDeviceName: "dpudevice-1"
  bfb: "doca-24.04"
  serialNumber: "MT25066004C7"
  pciAddress: "0000-04-00"
  dpuFlavor: "dpu-flavor"
  cluster:
    name: "tenant-00"
    namespace: "tenant-00-ns"
    nodeLabels:
      "dpf.node.dpu/role": "worker"
  nodeEffect:
    taint:
      key: "dpu"
      value: "provisioning"
      effect: NoSchedule
    applyOnLabelChange: true
    nodeMaintenanceAdditionalRequestors:
      - "dpu-provisioning"
      - "dpu-provisioning-2"
`)
			obj := &provisioningv1.DPU{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("create from yaml minimal", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPU
metadata:
  name: obj-10
  namespace: default
spec:
  dpuDeviceName: "dpudevice-1"
  serialNumber: "MT25066004C7"
  dpuFlavor: "dpu-flavor"
`)
			obj := &provisioningv1.DPU{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("status.phase default", func() {
			obj := createObj("obj-11")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Status.Phase).To(BeEquivalentTo(provisioningv1.DPUInitializing))

			objFetched := &provisioningv1.DPU{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Status.Phase).To(BeEquivalentTo(provisioningv1.DPUInitializing))
		})
	})
})
