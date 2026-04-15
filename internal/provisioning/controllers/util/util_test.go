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

package util

import (
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Util Suite")
}

var _ = Describe("Util", func() {
	Context("GetDPUCondition", func() {
		It("should return -1, nil when status is nil", func() {
			idx, cond := GetDPUCondition(nil, "test-condition")
			Expect(idx).To(Equal(-1))
			Expect(cond).To(BeNil())
		})

		It("should return -1, nil when conditions is nil", func() {
			status := &provisioningv1.DPUStatus{
				Conditions: nil,
			}
			idx, cond := GetDPUCondition(status, "test-condition")
			Expect(idx).To(Equal(-1))
			Expect(cond).To(BeNil())
		})

		It("should return -1, nil when condition not found", func() {
			status := &provisioningv1.DPUStatus{
				Conditions: []metav1.Condition{
					{Type: "other-condition", Status: metav1.ConditionTrue},
				},
			}
			idx, cond := GetDPUCondition(status, "test-condition")
			Expect(idx).To(Equal(-1))
			Expect(cond).To(BeNil())
		})

		It("should return index and condition when found", func() {
			status := &provisioningv1.DPUStatus{
				Conditions: []metav1.Condition{
					{Type: "first-condition", Status: metav1.ConditionFalse},
					{Type: "test-condition", Status: metav1.ConditionTrue, Reason: "TestReason"},
					{Type: "third-condition", Status: metav1.ConditionFalse},
				},
			}
			idx, cond := GetDPUCondition(status, "test-condition")
			Expect(idx).To(Equal(1))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Type).To(Equal("test-condition"))
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("TestReason"))
		})
	})

	Context("GetDPUDeviceCondition", func() {
		It("should return -1, nil when conditions is nil", func() {
			dpuDevice := &provisioningv1.DPUDevice{}
			idx, cond := GetDPUDeviceCondition(dpuDevice, "test-condition")
			Expect(idx).To(Equal(-1))
			Expect(cond).To(BeNil())
		})

		It("should return -1, nil when condition not found", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Status: provisioningv1.DPUDeviceStatus{
					Conditions: []metav1.Condition{
						{Type: "other-condition", Status: metav1.ConditionTrue},
					},
				},
			}
			idx, cond := GetDPUDeviceCondition(dpuDevice, "test-condition")
			Expect(idx).To(Equal(-1))
			Expect(cond).To(BeNil())
		})

		It("should return index and condition when found", func() {
			dpuDevice := &provisioningv1.DPUDevice{
				Status: provisioningv1.DPUDeviceStatus{
					Conditions: []metav1.Condition{
						{Type: "first-condition", Status: metav1.ConditionFalse},
						{Type: "test-condition", Status: metav1.ConditionTrue, Reason: "DeviceReady"},
					},
				},
			}
			idx, cond := GetDPUDeviceCondition(dpuDevice, "test-condition")
			Expect(idx).To(Equal(1))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Type).To(Equal("test-condition"))
			Expect(cond.Reason).To(Equal("DeviceReady"))
		})
	})

	Context("NeedUpdateLabels", func() {
		It("should return false when labels are identical", func() {
			label1 := map[string]string{"key1": "value1", "key2": "value2"}
			label2 := map[string]string{"key1": "value1", "key2": "value2"}
			Expect(NeedUpdateLabels(label1, label2)).To(BeFalse())
		})

		It("should return true when label value differs", func() {
			label1 := map[string]string{"key1": "value1"}
			label2 := map[string]string{"key1": "different-value"}
			Expect(NeedUpdateLabels(label1, label2)).To(BeTrue())
		})

		It("should return true when label key missing in label2", func() {
			label1 := map[string]string{"key1": "value1", "key2": "value2"}
			label2 := map[string]string{"key1": "value1"}
			Expect(NeedUpdateLabels(label1, label2)).To(BeTrue())
		})

		It("should return false when label1 is empty", func() {
			label1 := map[string]string{}
			label2 := map[string]string{"key1": "value1"}
			Expect(NeedUpdateLabels(label1, label2)).To(BeFalse())
		})

		It("should return false when both are empty", func() {
			label1 := map[string]string{}
			label2 := map[string]string{}
			Expect(NeedUpdateLabels(label1, label2)).To(BeFalse())
		})

		It("should return false when label1 is nil", func() {
			var label1 map[string]string = nil
			label2 := map[string]string{"key1": "value1"}
			Expect(NeedUpdateLabels(label1, label2)).To(BeFalse())
		})
	})

	Context("IsDPUBeforeProvisioningPhase", func() {
		It("should return true for empty phase", func() {
			Expect(IsDPUBeforeProvisioningPhase("")).To(BeTrue())
		})

		It("should return true for Initializing phase", func() {
			Expect(IsDPUBeforeProvisioningPhase(provisioningv1.DPUInitializing)).To(BeTrue())
		})

		It("should return true for Pending phase", func() {
			Expect(IsDPUBeforeProvisioningPhase(provisioningv1.DPUPending)).To(BeTrue())
		})

		It("should return false for Ready phase", func() {
			Expect(IsDPUBeforeProvisioningPhase(provisioningv1.DPUReady)).To(BeFalse())
		})

		It("should return false for provisioning phases", func() {
			provisioningPhases := []provisioningv1.DPUPhase{
				provisioningv1.DPUOSInstalling,
				provisioningv1.DPUNodeEffect,
				provisioningv1.DPUPrepareBFB,
			}
			for _, phase := range provisioningPhases {
				Expect(IsDPUBeforeProvisioningPhase(phase)).To(BeFalse(), "Phase %s should not be before provisioning", phase)
			}
		})
	})

	Context("IsDPUAfterProvisioningPhase", func() {
		It("should return true for Ready phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUReady)).To(BeTrue())
		})

		It("should return true for Rebooting phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPURebooting)).To(BeTrue())
		})

		It("should return true for Error phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUError)).To(BeTrue())
		})

		It("should return true for ClusterConfig phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUClusterConfig)).To(BeTrue())
		})

		It("should return true for Deleting phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUDeleting)).To(BeTrue())
		})

		It("should return true for DPUConfig phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUConfig)).To(BeTrue())
		})

		It("should return false for Initializing phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUInitializing)).To(BeFalse())
		})

		It("should return false for Pending phase", func() {
			Expect(IsDPUAfterProvisioningPhase(provisioningv1.DPUPending)).To(BeFalse())
		})

		It("should return false for empty phase", func() {
			Expect(IsDPUAfterProvisioningPhase("")).To(BeFalse())
		})
	})

	Context("GenerateDPUNodeMaintenanceObjectName", func() {
		It("should return error when nodeEffect has no effect type set", func() {
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", provisioningv1.NodeEffect{})
			Expect(err).To(HaveOccurred())
			Expect(name).To(BeEmpty())
		})

		It("should generate name for Drain effect", func() {
			drain := true
			nodeEffect := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Drain: &drain,
				},
			}
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", nodeEffect)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-k8sdrain"))
		})

		It("should generate name for Hold effect", func() {
			hold := true
			nodeEffect := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Hold: &hold,
				},
			}
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", nodeEffect)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-hold"))
		})

		It("should generate name for CustomAction effect", func() {
			customAction := "my-action"
			nodeEffect := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					CustomAction: &customAction,
				},
			}
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", nodeEffect)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-custom-action-my-action"))
		})

		It("should generate name for Taint effect with hash", func() {
			nodeEffect := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "test-key",
						Value:  "test-value",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			}
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", nodeEffect)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(HavePrefix("test-node-taint-"))
			Expect(name).To(HaveLen(len("test-node-taint-") + 8)) // 8 char hash
		})

		It("should generate name for NoEffect", func() {
			noEffect := true
			nodeEffect := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: &noEffect,
				},
			}
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", nodeEffect)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-noeffect"))
		})

		It("should generate name for CustomLabel effect with hash", func() {
			nodeEffect := provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					CustomLabel: map[string]string{
						"label-key": "label-value",
					},
				},
			}
			name, err := GenerateDPUNodeMaintenanceObjectName("test-node", nodeEffect)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(HavePrefix("test-node-cl-"))
			Expect(name).To(HaveLen(len("test-node-cl-") + 8)) // 8 char hash
		})
	})

	Context("IsDPUNodeReady", func() {
		It("should return false when no conditions", func() {
			dpuNode := &provisioningv1.DPUNode{
				Status: provisioningv1.DPUNodeStatus{
					Conditions: nil,
				},
			}
			Expect(IsDPUNodeReady(dpuNode)).To(BeFalse())
		})

		It("should return false when Ready condition not found", func() {
			dpuNode := &provisioningv1.DPUNode{
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{Type: "OtherCondition", Status: metav1.ConditionTrue},
					},
				},
			}
			Expect(IsDPUNodeReady(dpuNode)).To(BeFalse())
		})

		It("should return false when Ready condition is False", func() {
			dpuNode := &provisioningv1.DPUNode{
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{Type: provisioningv1.DPUNodeConditionReady.String(), Status: metav1.ConditionFalse},
					},
				},
			}
			Expect(IsDPUNodeReady(dpuNode)).To(BeFalse())
		})

		It("should return true when Ready condition is True", func() {
			dpuNode := &provisioningv1.DPUNode{
				Status: provisioningv1.DPUNodeStatus{
					Conditions: []metav1.Condition{
						{Type: provisioningv1.DPUNodeConditionReady.String(), Status: metav1.ConditionTrue},
					},
				},
			}
			Expect(IsDPUNodeReady(dpuNode)).To(BeTrue())
		})
	})

	Context("RemoveDuplicates", func() {
		It("should return empty slice for empty input", func() {
			result := RemoveDuplicates([]string{})
			Expect(result).To(BeEmpty())
		})

		It("should return empty slice for nil input", func() {
			result := RemoveDuplicates(nil)
			Expect(result).To(BeEmpty())
		})

		It("should return same slice when no duplicates", func() {
			input := []string{"a", "b", "c"}
			result := RemoveDuplicates(input)
			Expect(result).To(Equal([]string{"a", "b", "c"}))
		})

		It("should remove duplicates", func() {
			input := []string{"a", "b", "a", "c", "b", "d"}
			result := RemoveDuplicates(input)
			Expect(result).To(Equal([]string{"a", "b", "c", "d"}))
		})

		It("should preserve order", func() {
			input := []string{"c", "a", "b", "a", "c"}
			result := RemoveDuplicates(input)
			Expect(result).To(Equal([]string{"c", "a", "b"}))
		})

		It("should handle single element", func() {
			input := []string{"only"}
			result := RemoveDuplicates(input)
			Expect(result).To(Equal([]string{"only"}))
		})

		It("should handle all duplicates", func() {
			input := []string{"same", "same", "same"}
			result := RemoveDuplicates(input)
			Expect(result).To(Equal([]string{"same"}))
		})
	})

	Context("GenerateBFBTaskName", func() {
		It("should include UID in task name", func() {
			bfb := provisioningv1.BFB{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test-bfb",
					UID:       types.UID("uid-123"),
				},
			}
			Expect(GenerateBFBTaskName(bfb)).To(Equal("default-test-bfb-uid-123"))
		})

		It("should produce different task names for same namespace/name with different UIDs", func() {
			baseMeta := metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-bfb",
			}
			bfb1 := provisioningv1.BFB{ObjectMeta: baseMeta}
			bfb1.UID = types.UID("uid-1")
			bfb2 := provisioningv1.BFB{ObjectMeta: baseMeta}
			bfb2.UID = types.UID("uid-2")

			Expect(GenerateBFBTaskName(bfb1)).NotTo(Equal(GenerateBFBTaskName(bfb2)))
		})
	})
})
