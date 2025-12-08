/*
Copyright 2025 NVIDIA

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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = Describe("DPUNode", func() {

	var getObjKey = func(obj *provisioningv1.DPUNode) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.DPUNode {
		return &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec:   provisioningv1.DPUNodeSpec{},
			Status: provisioningv1.DPUNodeStatus{},
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

			objFetched := &provisioningv1.DPUNode{}
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

			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})
		It("create from yaml", func() {
			dpudeviceYml := []byte(`
            apiVersion: provisioning.dpu.nvidia.com/v1alpha1
            kind: DPUDevice
            metadata:
              name: dpu-device-1
              namespace: default
            spec:
              bmcIp: 3.3.3.3
              serialNumber: MT25066004E1
              psid: MT_0000000034
              opn: 900-9D3B4-00SV-EA0
            `)
			dpudeviceObj := &provisioningv1.DPUDevice{}
			err := yaml.UnmarshalStrict(dpudeviceYml, dpudeviceObj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, dpudeviceObj)
			Expect(err).NotTo(HaveOccurred())

			dpunodeYml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: obj-4
  namespace: default
spec:
  nodeRebootMethod:
    external: {}
  nodeDMSAddress: 
    ip: 4.4.4.4
    port: 50
  dpus:
  - name: dpu-device-1
`)
			obj := &provisioningv1.DPUNode{}
			err = yaml.UnmarshalStrict(dpunodeYml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("create from yaml minimal", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: obj-5
  namespace: default
spec:
  nodeRebootMethod:
    external: {}
`)
			obj := &provisioningv1.DPUNode{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
		Context("create from yaml with invalid NodeRebootMethod", func() {
			It("should not create a DPUNode with an invalid NodeRebootMethod value", func() {
				dpunodeYml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: obj-6
  namespace: default
spec:
  nodeRebootMethod: {}
  nodeDMSAddress:
    ip: 4.4.4.4
    port: 50
  dpus:
  - name: dpu-device-1
`)
				obj := &provisioningv1.DPUNode{}
				err := yaml.UnmarshalStrict(dpunodeYml, obj)
				Expect(err).To(Succeed())
				err = k8sClient.Create(ctx, obj)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("create from yaml with invalid NodeDMSAddress", func() {
			It("should not create a DPUNode with an invalid NodeDMSAddress value", func() {
				dpunodeYml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: obj-7
  namespace: default
spec:
  nodeRebootMethod:
    external: {}
  nodeDMSAddress:
    ip: invalid-ip
    port: 50
  dpus:
  - name: dpu-device-1
`)
				obj := &provisioningv1.DPUNode{}
				err := yaml.UnmarshalStrict(dpunodeYml, obj)
				Expect(err).To(Succeed())
				err = k8sClient.Create(ctx, obj)
				Expect(err).To(HaveOccurred())
			})
		})

		It("update object - check immutability of kubeNodeRef", func() {
			obj := createObj("obj-8")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			node2Ref := "node-2"
			objFetched.Status.KubeNodeRef = &node2Ref
			err = k8sClient.Status().Update(ctx, objFetched)
			Expect(err).NotTo(HaveOccurred())

			objUpdatedFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objUpdatedFetched)
			Expect(err).NotTo(HaveOccurred())
			node3Ref := "node-3"
			objUpdatedFetched.Status.KubeNodeRef = &node3Ref
			err = k8sClient.Status().Update(ctx, objUpdatedFetched)
			Expect(err).To(HaveOccurred())
		})
		It("create from yaml with multiple DPUs", func() {
			// Create dpu-device-10
			dpudevice10Yml := []byte(`
            apiVersion: provisioning.dpu.nvidia.com/v1alpha1
            kind: DPUDevice
            metadata:
              name: dpu-device-10
              namespace: default
            spec:
              bmcIp: 3.3.3.10
              serialNumber: MT25066004EA
              psid: MT_0000000034
              opn: 900-9D3B4-00SV-EA0
            `)
			dpudevice10Obj := &provisioningv1.DPUDevice{}
			err := yaml.UnmarshalStrict(dpudevice10Yml, dpudevice10Obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, dpudevice10Obj)
			Expect(err).NotTo(HaveOccurred())

			// Create dpu-device-11
			dpudevice11Yml := []byte(`
            apiVersion: provisioning.dpu.nvidia.com/v1alpha1
            kind: DPUDevice
            metadata:
              name: dpu-device-11
              namespace: default
            spec:
              bmcIp: 3.3.3.11
              serialNumber: MT25066004EB
              psid: MT_0000000035
              opn: 900-9D3B4-00SV-EA1
            `)
			dpudevice11Obj := &provisioningv1.DPUDevice{}
			err = yaml.UnmarshalStrict(dpudevice11Yml, dpudevice11Obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, dpudevice11Obj)
			Expect(err).NotTo(HaveOccurred())

			dpunodeYml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: obj-10
  namespace: default
spec:
  nodeRebootMethod:
    external: {}
  nodeDMSAddress:
    ip: 4.4.4.4
    port: 50
  dpus:
  - name: dpu-device-10
  - name: dpu-device-11
`)
			obj := &provisioningv1.DPUNode{}
			err = yaml.UnmarshalStrict(dpunodeYml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).ToNot(HaveOccurred())
		})
		It("create DPUNode with missing NodeRebootMethod", func() {
			obj := createObj("obj-13")
			obj.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("update DPUNode with new NodeRebootMethod", func() {
			obj := createObj("obj-15")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			obj.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				Script: &provisioningv1.Script{
					Name: "test",
				},
			}
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.NodeRebootMethod).To(Equal(obj.Spec.NodeRebootMethod))
		})
		It("create and update status with condition", func() {
			obj := createObj("obj-16")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			condition := metav1.Condition{
				Type:               string(provisioningv1.DPUNodeConditionReady),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Initialized",
				Message:            "DPUNode is ready",
			}
			obj.Status.Conditions = append(obj.Status.Conditions, condition)
			dpuInstallInterface := string(provisioningv1.DPUNodeInstallInterfaceGNOI)
			obj.Status.DPUInstallInterface = &dpuInstallInterface
			err = k8sClient.Status().Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Status.Conditions).To(HaveLen(1))
			Expect(objFetched.Status.Conditions[0].Type).To(Equal(string(provisioningv1.DPUNodeConditionReady)))
			Expect(objFetched.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		})
		It("create and update status with multiple conditions", func() {
			obj := createObj("obj-17")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			condition1 := metav1.Condition{
				Type:               string(provisioningv1.DPUNodeConditionReady),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Initialized",
				Message:            "DPUNode is ready",
			}
			condition2 := metav1.Condition{
				Type:               string(provisioningv1.DPUNodeConditionRebootInProgress),
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "RebootNotStarted",
				Message:            "DPUNode reboot has not started",
			}
			condition3 := metav1.Condition{
				Type:               string(provisioningv1.DPUNodeConditionInvalidDPUDetails),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "InvalidDetails",
				Message:            "Invalid DPU details provided",
			}
			condition4 := metav1.Condition{
				Type:               string(provisioningv1.DPUNodeConditionRebootInProgress),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "RebootInProgress",
				Message:            "DPUNode reboot in progress",
			}
			obj.Status.Conditions = append(obj.Status.Conditions, condition1, condition2, condition3, condition4)
			err = k8sClient.Status().Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Status.Conditions).To(HaveLen(4))
			Expect(objFetched.Status.Conditions[0].Type).To(Equal(string(provisioningv1.DPUNodeConditionReady)))
			Expect(objFetched.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(objFetched.Status.Conditions[1].Type).To(Equal(string(provisioningv1.DPUNodeConditionRebootInProgress)))
			Expect(objFetched.Status.Conditions[1].Status).To(Equal(metav1.ConditionFalse))
			Expect(objFetched.Status.Conditions[2].Type).To(Equal(string(provisioningv1.DPUNodeConditionInvalidDPUDetails)))
			Expect(objFetched.Status.Conditions[2].Status).To(Equal(metav1.ConditionTrue))
			Expect(objFetched.Status.Conditions[3].Type).To(Equal(string(provisioningv1.DPUNodeConditionRebootInProgress)))
			Expect(objFetched.Status.Conditions[3].Status).To(Equal(metav1.ConditionTrue))
		})
		It("create DPUNode with default DPUInstallInterface", func() {
			obj := createObj("obj-18")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Status().Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Status.DPUInstallInterface).To(BeNil())
		})

		It("update DPUNode DPUInstallInterface", func() {
			obj := createObj("obj-19")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			dpuInstallInterface := string(provisioningv1.DPUNodeInstallIntrefaceRedfish)
			obj.Status.DPUInstallInterface = &dpuInstallInterface
			err = k8sClient.Status().Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			objFetched := &provisioningv1.DPUNode{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(*objFetched.Status.DPUInstallInterface).To(Equal(string(provisioningv1.DPUNodeInstallIntrefaceRedfish)))
		})

		It("update DPUNode DPUInstallInterface to invalid value", func() {
			obj := createObj("obj-20")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			dpuInstallInterface := "InvalidInterface"
			obj.Status.DPUInstallInterface = &dpuInstallInterface
			err = k8sClient.Status().Update(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("spec.nodeRebootMethod default", func() {
			obj := createObj("obj-21")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(*objFetched.Spec.NodeRebootMethod).To(Equal(provisioningv1.NodeRebootMethod{HostAgent: &provisioningv1.HostAgent{}}))
		})
		It("spec.nodeRebootMethod can not be updated by nil", func() {
			obj := createObj("obj-22")
			obj.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}}
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(*objFetched.Spec.NodeRebootMethod).To(Equal(provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}}))

			obj.Spec.NodeRebootMethod = nil
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(*objFetched.Spec.NodeRebootMethod).To(Equal(provisioningv1.NodeRebootMethod{HostAgent: &provisioningv1.HostAgent{}}))
		})
		It("create DPUNode with duplicate DPU name should fail", func() {
			// Create a new DPU device for this test
			dpudeviceYml := []byte(`
            apiVersion: provisioning.dpu.nvidia.com/v1alpha1
            kind: DPUDevice
            metadata:
              name: dpu-device-23
              namespace: default
            spec:
              bmcIp: 3.3.3.23
              serialNumber: MT_0000000023
              psid: MT_0000000023
              opn: 900-9D3B4-00SV-EA0
            `)
			dpudeviceObj := &provisioningv1.DPUDevice{}
			err := yaml.UnmarshalStrict(dpudeviceYml, dpudeviceObj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, dpudeviceObj)
			Expect(err).NotTo(HaveOccurred())

			// Create first DPUNode with DPU
			obj1 := createObj("obj-23")
			obj1.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-23"},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Try to create second DPUNode with same DPU name
			obj2 := createObj("obj-24")
			obj2.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-23"}, // Same DPU name
			}
			Expect(k8sClient.Create(ctx, obj2)).To(HaveOccurred())
		})
		It("create DPUNode with duplicate DPU name in different namespace should fail", func() {
			// Create first DPUNode in default namespace
			obj1 := createObj("obj-25")
			obj1.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-2"},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Try to create second DPUNode with same DPU name in different namespace
			obj2 := &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-26",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUNodeSpec{
					DPUs: []provisioningv1.DPURef{
						{Name: "dpu-device-2"}, // Same DPU name
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj2)).To(HaveOccurred())
		})
		It("update DPUNode with duplicate DPU name should fail", func() {
			// Create first DPUNode with DPU
			obj1 := createObj("obj-27")
			obj1.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-3"},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Create second DPUNode with different DPU name
			obj2 := createObj("obj-28")
			obj2.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-4"},
			}
			Expect(k8sClient.Create(ctx, obj2)).NotTo(HaveOccurred())

			// Try to update second DPUNode to have same DPU name as first
			obj2.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-3"}, // Same DPU name as first node
			}
			Expect(k8sClient.Update(ctx, obj2)).To(HaveOccurred())
		})
		It("create DPUNode with multiple DPUs where one is duplicate should fail", func() {
			// Create first DPUNode with DPU
			obj1 := createObj("obj-29")
			obj1.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-5"},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Try to create second DPUNode with multiple DPUs where one is duplicate
			obj2 := createObj("obj-30")
			obj2.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-6"}, // New DPU name
				{Name: "dpu-device-5"}, // Duplicate DPU name
			}
			Expect(k8sClient.Create(ctx, obj2)).To(HaveOccurred())
		})
		It("update DPUNode without changing DPUs should succeed", func() {
			// Create DPUNode with DPU
			obj := createObj("obj-31")
			obj.Spec.DPUs = []provisioningv1.DPURef{
				{Name: "dpu-device-7"},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			// Update DPUNode without changing DPUs
			obj.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			}
			Expect(k8sClient.Update(ctx, obj)).NotTo(HaveOccurred())
		})
	})

	// Unit tests - direct webhook method calls
	Context("webhook unit tests", func() {
		ctx := context.Background()

		It("ValidateDelete should return nil", func() {
			webhook := &DPUNode{}
			warnings, err := webhook.ValidateDelete(ctx, &provisioningv1.DPUNode{})
			Expect(warnings).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})

		// Tests for type assertion error handling (!ok branches)
		It("ValidateCreate should return error for invalid object type", func() {
			webhook := &DPUNode{}
			_, err := webhook.ValidateCreate(ctx, &provisioningv1.DPU{}) // Wrong type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateUpdate should return error for invalid object type for newObj", func() {
			webhook := &DPUNode{}
			_, err := webhook.ValidateUpdate(ctx, &provisioningv1.DPUNode{}, &provisioningv1.DPU{}) // Wrong newObj type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateUpdate should return error for invalid object type for oldObj", func() {
			webhook := &DPUNode{}
			_, err := webhook.ValidateUpdate(ctx, &provisioningv1.DPU{}, &provisioningv1.DPUNode{}) // Wrong oldObj type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid old object type"))
		})
	})
})
