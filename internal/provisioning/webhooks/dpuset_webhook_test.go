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

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dpuFlavor = "test-flavor"
)

var _ = Describe("DPUSet", func() {

	var getObjKey = func(obj *provisioningv1.DPUSet) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.DPUSet {
		return &provisioningv1.DPUSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: provisioningv1.DPUSetSpec{
				Strategy: provisioningv1.DPUSetStrategy{
					Type: provisioningv1.OnDeleteStrategyType,
				},
				DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{
						BFB:        &provisioningv1.BFBReference{Name: "test-bfb"},
						DPUFlavor:  dpuFlavor,
						NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				},
			},
			Status: provisioningv1.DPUSetStatus{},
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
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUSet{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("delete object", func() {
			obj := createObj("obj-2")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
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
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUSet{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("spec.dpuTemplate.spec.cluster.nodeSelector is mutable", func() {
			refValue := map[string]string{"k1": "v1"}
			newValue := map[string]string{"k1": "v11", "k2": "v2"}

			obj := createObj("obj-4")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{
				NodeLabels: refValue,
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.DPUTemplate.Spec.Cluster.NodeLabels = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUSet{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.DPUTemplate.Spec.Cluster.NodeLabels).To(Equal(newValue))
		})

		It("nodeEffect is updated", func() {
			refValue := map[string]string{"k1": "v1"}
			newValue := map[string]string{"k1": "v11", "k2": "v2"}

			obj := createObj("node-effect-object")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.Cluster = &provisioningv1.ClusterSpec{
				NodeLabels: refValue,
			}
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "foo",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.DPUTemplate.Spec.Cluster.NodeLabels = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUSet{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.DPUTemplate.Spec.Cluster.NodeLabels).To(Equal(newValue))
		})

		It("spec.nodeEffect assign nil should be rejected", func() {
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
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.NodeEffect = refValue
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPUSet{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.DPUTemplate.Spec.NodeEffect).To(Equal(refValue))
		})

		It("spec.dpuTemplate.spec.nodeEffect.applyOnLabelChange defaults to false", func() {
			obj := createObj("obj-apply-on-label-change-default")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				// ApplyOnLabelChange not set, should default to false
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPUSet{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
		})

		It("spec.dpuTemplate.spec.nodeEffect.applyOnLabelChange is mutable", func() {
			obj := createObj("obj-apply-on-label-change-mutable")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
			Expect(k8sClient.Patch(ctx, obj, patch)).To(Succeed())

			objFetched := &provisioningv1.DPUSet{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
		})

		It("spec.dpuTemplate.spec.nodeEffect.nodeMaintenanceAdditionalRequestors is mutable", func() {
			obj := createObj("obj-additional-requestors-mutable")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{"req-1", "req-2"}
			Expect(k8sClient.Patch(ctx, obj, patch)).To(Succeed())

			objFetched := &provisioningv1.DPUSet{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(HaveLen(2))
			Expect(objFetched.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(ContainElements("req-1", "req-2"))
		})

		It("only one field may be set in spec.nodeEffect", func() {
			obj := createObj("checking-node-effect")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			// Error when creating a DPUSet with a nodeEffect setting taint and customLabel.
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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

			// Error when creating a DPUSet with a nodeEffect setting taint and drain.
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "foo",
						Effect: corev1.TaintEffectNoSchedule,
					},
					Drain: ptr.To(true),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())

			// Error when creating a DPUSet with a nodeeffect setting Drain and NoEffect
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Drain: ptr.To(true),
					CustomLabel: map[string]string{
						"foo": "bar",
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())

			// Error when creating a DPUSet with a nodeeffect setting Drain and NoEffect
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
					CustomLabel: map[string]string{
						"foo": "bar",
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(Succeed())
		})

		It("create from yaml", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: obj-5
  namespace: default
spec:
  dpuNodeSelector:
  strategy:
    rollingUpdate:
      maxUnavailable: 10%
    type: RollingUpdate
  dpuTemplate:
    annotations:
      nvidia.com/dpuOperator-override-powercycle-command: "cycle"
    spec:
      dpuFlavor: "hbn"
      bfb:
        name: "doca-24.04"
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
        applyOnLabelChange: true
        nodeMaintenanceAdditionalRequestors:
          - "dpu-provisioning"
          - "dpu-provisioning-2"
      Cluster:
        nodeLabels:
          "dpf.node.dpu/role": "worker"
`)
			obj := &provisioningv1.DPUSet{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("create from yaml minimal", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: obj-6
  namespace: default
spec:
  strategy:
    type: OnDelete
  dpuTemplate:
    spec:
      bfb:
        name: "test-bfb"
      dpuFlavor: "test-flavor"
      nodeEffect:
        noEffect: true
`)
			obj := &provisioningv1.DPUSet{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully create DPUSet with valid dpuFlavor", func() {
			obj := createObj("obj-valid-dpuflavor")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject DPUSet with empty spec.dpuTemplate.spec.bfb.name", func() {
			obj := createObj("obj-empty-bfb-name")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.BFB = &provisioningv1.BFBReference{Name: ""}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("should reject DPUSet with empty spec.dpuTemplate.spec.dpuFlavor", func() {
			obj := createObj("obj-empty-dpuflavor")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = ""
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("should successfully update DPUSet with valid dpuFlavor", func() {
			obj := createObj("obj-update-valid-dpuflavor")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = "initial-flavor"
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			// Update with new valid DPUFlavor
			obj.Spec.DPUTemplate.Spec.DPUFlavor = "updated-flavor"
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUSet{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.DPUTemplate.Spec.DPUFlavor).To(Equal("updated-flavor"))
		})

		It("should create DPUSet with dpuFlavorTemplate and no dpuFlavor", func() {
			obj := createObj("obj-flavor-template")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = ""
			obj.Spec.DPUTemplate.Spec.DPUFlavorTemplate = "some-template"
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			objFetched := &provisioningv1.DPUSet{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(objFetched.Spec.DPUTemplate.Spec.DPUFlavorTemplate).To(Equal("some-template"))
			Expect(objFetched.Spec.DPUTemplate.Spec.DPUFlavor).To(BeEmpty())
		})

		It("should reject DPUSet with both dpuFlavor and dpuFlavorTemplate set", func() {
			obj := createObj("obj-flavor-and-template")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = dpuFlavor
			obj.Spec.DPUTemplate.Spec.DPUFlavorTemplate = "some-template"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		// Tests for validateStrategy() - nil pointer safety
		It("should accept RollingUpdate with nil rollingUpdate field", func() {
			obj := createObj("obj-rolling-nil-details")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})

		It("should accept RollingUpdate with nil maxUnavailable", func() {
			obj := createObj("obj-rolling-nil-maxunavailable")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type:          provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})

		It("should accept OnDelete strategy", func() {
			obj := createObj("obj-ondelete")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.OnDeleteStrategyType,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})

		// Tests for validateStrategy() - integer validation
		It("should reject RollingUpdate with maxUnavailable=0", func() {
			obj := createObj("obj-invalid-maxunavailable-zero")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("should reject RollingUpdate with negative maxUnavailable", func() {
			obj := createObj("obj-invalid-maxunavailable-negative")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		// Tests for validateStrategy() - percentage validation
		It("should reject RollingUpdate with maxUnavailable=0%", func() {
			obj := createObj("obj-invalid-maxunavailable-zero-percent")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "0%"},
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("should reject RollingUpdate with maxUnavailable>100%", func() {
			obj := createObj("obj-invalid-maxunavailable-over-100")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "150%"},
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("should accept RollingUpdate with valid percentage", func() {
			obj := createObj("obj-valid-maxunavailable-percent")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "50%"},
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept RollingUpdate with maxUnavailable=100%", func() {
			obj := createObj("obj-valid-maxunavailable-100-percent")
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "100%"},
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		// Tests for ValidateUpdate() - strategy validation on update
		It("should reject update with invalid strategy", func() {
			obj := createObj("obj-update-invalid-strategy")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			// Update with invalid strategy
			obj.Spec.Strategy = provisioningv1.DPUSetStrategy{
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
				},
			}
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		// Tests for ValidateCreate() - host power cycle annotation validation
		It("should reject invalid host-power-cycle-required annotation", func() {
			obj := createObj("obj-invalid-powercycle-annotation")
			obj.Spec.DPUTemplate.Annotations = map[string]string{
				"provisioning.dpu.nvidia.com/host-power-cycle-required": "invalid-value",
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("should accept valid host-power-cycle-required annotation with true", func() {
			obj := createObj("obj-valid-powercycle-annotation-true")
			obj.Spec.DPUTemplate.Annotations = map[string]string{
				"provisioning.dpu.nvidia.com/host-power-cycle-required": "true",
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept valid host-power-cycle-required annotation with false", func() {
			obj := createObj("obj-valid-powercycle-annotation-false")
			obj.Spec.DPUTemplate.Annotations = map[string]string{
				"provisioning.dpu.nvidia.com/host-power-cycle-required": "false",
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

	})

	// Unit tests - direct webhook method calls
	Context("webhook unit tests", func() {
		ctx := context.Background()

		It("ValidateDelete should return nil", func() {
			webhook := &DPUSet{}
			warnings, err := webhook.ValidateDelete(ctx, &provisioningv1.DPUSet{})
			Expect(warnings).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})

		// Tests for type assertion error handling (!ok branches)
		It("Default should return error for invalid object type", func() {
			webhook := &DPUSet{}
			err := webhook.Default(ctx, &provisioningv1.DPU{}) // Wrong type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateCreate should return error for invalid object type", func() {
			webhook := &DPUSet{}
			_, err := webhook.ValidateCreate(ctx, &provisioningv1.DPU{}) // Wrong type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateUpdate should return error for invalid object type", func() {
			webhook := &DPUSet{}
			_, err := webhook.ValidateUpdate(ctx, &provisioningv1.DPUSet{}, &provisioningv1.DPU{}) // Wrong newObj type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateCreate should reject empty spec.dpuTemplate.spec.bfb.name", func() {
			webhook := &DPUSet{}
			obj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:       &provisioningv1.BFBReference{Name: ""},
							DPUFlavor: "flavor",
						},
					},
				},
			}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		// The webhook no longer enforces dpuFlavor non-emptiness: exclusivity of
		// dpuFlavor/dpuFlavorTemplate is enforced by the CRD CEL rule. The webhook must
		// therefore accept a template-only DPUSet (no dpuFlavor).
		It("ValidateCreate should accept dpuFlavorTemplate without dpuFlavor", func() {
			webhook := &DPUSet{}
			obj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:               &provisioningv1.BFBReference{Name: "bfb"},
							DPUFlavorTemplate: "some-template",
						},
					},
				},
			}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		// Object decoded from YAML/JSON with explicit empty bfb.name or dpuFlavor (e.g. kubectl apply
		// or raw API payload). UnmarshalStrict ensures we match the same decoding path as other tests.
		It("ValidateCreate should reject YAML with empty spec.dpuTemplate.spec.bfb.name", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: test
  namespace: default
spec:
  strategy:
    type: OnDelete
  dpuTemplate:
    spec:
      bfb:
        name: ""
      dpuFlavor: "flavor"
`)
			obj := &provisioningv1.DPUSet{}
			Expect(yaml.UnmarshalStrict(yml, obj)).To(Succeed())
			webhook := &DPUSet{}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("ValidateCreate should accept YAML with dpuFlavorTemplate and no dpuFlavor", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: test
  namespace: default
spec:
  strategy:
    type: OnDelete
  dpuTemplate:
    spec:
      bfb:
        name: "bfb"
      dpuFlavorTemplate: "some-template"
`)
			obj := &provisioningv1.DPUSet{}
			Expect(yaml.UnmarshalStrict(yml, obj)).To(Succeed())
			webhook := &DPUSet{}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("ValidateCreate should reject astraEnabled=true when deploymentMode is not zero-trust", func() {
			webhook := &DPUSet{DeploymentMode: string(operatorv1.DeploymentModeHostTrusted)}
			obj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:          &provisioningv1.BFBReference{Name: "bfb"},
							DPUFlavor:    "flavor",
							AstraEnabled: ptr.To(true),
						},
					},
				},
			}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("ValidateCreate should allow astraEnabled=true when deploymentMode is zero-trust", func() {
			webhook := &DPUSet{DeploymentMode: string(operatorv1.DeploymentModeZeroTrust)}
			obj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:          &provisioningv1.BFBReference{Name: "bfb"},
							DPUFlavor:    "flavor",
							AstraEnabled: ptr.To(true),
						},
					},
				},
			}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("ValidateCreate should reject astraEnabled=true when deploymentMode is host-trusted", func() {
			webhook := &DPUSet{DeploymentMode: string(operatorv1.DeploymentModeHostTrusted)}
			obj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:          &provisioningv1.BFBReference{Name: "bfb"},
							DPUFlavor:    "flavor",
							AstraEnabled: ptr.To(true),
						},
					},
				},
			}
			_, err := webhook.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("ValidateUpdate should reject astraEnabled=true when deploymentMode is not zero-trust", func() {
			webhook := &DPUSet{DeploymentMode: string(operatorv1.DeploymentModeHostTrusted)}
			oldObj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:       &provisioningv1.BFBReference{Name: "bfb"},
							DPUFlavor: "flavor",
						},
					},
				},
			}
			newObj := oldObj.DeepCopy()
			newObj.Spec.DPUTemplate.Spec.AstraEnabled = ptr.To(true)

			_, err := webhook.ValidateUpdate(ctx, oldObj, newObj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("ValidateUpdate should allow astraEnabled=true when deploymentMode is zero-trust", func() {
			webhook := &DPUSet{DeploymentMode: string(operatorv1.DeploymentModeZeroTrust)}
			oldObj := &provisioningv1.DPUSet{
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{Type: provisioningv1.OnDeleteStrategyType},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:       &provisioningv1.BFBReference{Name: "bfb"},
							DPUFlavor: "flavor",
						},
					},
				},
			}
			newObj := oldObj.DeepCopy()
			newObj.Spec.DPUTemplate.Spec.AstraEnabled = ptr.To(true)

			_, err := webhook.ValidateUpdate(ctx, oldObj, newObj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
