/*
Copyright 2026 NVIDIA

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

package zerotrust

import (
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ZerotrustClient", func() {
	var dpu *provisioningv1.DPU
	var testNS *corev1.Namespace

	var createDPU = func(name string, namespace string) *provisioningv1.DPU {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   "test-dpu-node",
				DPUDeviceName: "test-dpu-device",
				BFB:           "bfb-test",
				SerialNumber:  "test-dpu-serial-number",
				DPUFlavor:     "test-dpu-flavor",
			},
		}
		Expect(k8sClient.Create(ctx, dpu)).To(Succeed())
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		dpu.Status.Conditions = []metav1.Condition{
			{
				LastTransitionTime: metav1.Now(),
				Type:               string(provisioningv1.DPUCondOSInstalled),
				Status:             metav1.ConditionFalse,
				Reason:             string(provisioningv1.DPUCondOSInstalled),
				Message:            string(provisioningv1.DPUCondOSInstalled),
			},
		}
		Expect(k8sClient.Status().Update(ctx, dpu)).To(Succeed())
		return dpu
	}

	BeforeEach(func() {
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "zerotrust-client-testns-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
		dpu = createDPU("test-dpu", testNS.Name)
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testNS))).To(Succeed())
	})

	It("should be able to health check", func() {
		client := NewZerotrustClient(kubeconfigPath, dpu.Name, dpu.Namespace)
		Expect(client.HealthCheck()).To(Succeed())
	})

	Context("update status", func() {
		It("should be able to update status", func() {
			agentClient := NewZerotrustClient(kubeconfigPath, dpu.Name, dpu.Namespace)
			lastStartupTime := metav1.NewTime(time.Now().Truncate(time.Second))
			dpuInfo := provisioningv1.DPUInternalStatus{
				LastStartupTime:    &lastStartupTime,
				HostRebootRequired: ptr.To(true),
				InitialBootID:      ptr.To("test-initial-boot-id"),
				Conditions: []metav1.Condition{
					{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "TestReason",
						Message: "TestMessage",
					},
				},
			}
			Expect(agentClient.UpdateStatus(ctx, dpuInfo)).To(Succeed())

			latestDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.DPUInternalStatus).NotTo(BeNil())
			Expect(latestDPU.Status.DPUInternalStatus.LastStartupTime).NotTo(BeNil())
			Expect(latestDPU.Status.DPUInternalStatus.LastStartupTime.Equal(&lastStartupTime)).To(BeTrue())
			Expect(latestDPU.Status.DPUInternalStatus.HostRebootRequired).NotTo(BeNil())
			Expect(*latestDPU.Status.DPUInternalStatus.HostRebootRequired).To(BeTrue())
			Expect(latestDPU.Status.DPUInternalStatus.InitialBootID).NotTo(BeNil())
			Expect(*latestDPU.Status.DPUInternalStatus.InitialBootID).To(Equal("test-initial-boot-id"))
			Expect(latestDPU.Status.DPUInternalStatus.Conditions).To(HaveLen(1))
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].Type).To(Equal("Ready"))
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].Reason).To(Equal("TestReason"))
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].Message).To(Equal("TestMessage"))

			By("last transition time should be set automatically")
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].LastTransitionTime).NotTo(BeNil())

			By("last transition time should not be updated if condition is not changed")
			originalLastTransitionTime := latestDPU.Status.DPUInternalStatus.Conditions[0].LastTransitionTime
			Expect(agentClient.UpdateStatus(ctx, dpuInfo)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].LastTransitionTime).To(Equal(originalLastTransitionTime))

			By("last transition time should be updated if condition is changed")
			time.Sleep(2 * time.Second)
			dpuInfo.Conditions[0].Status = metav1.ConditionFalse
			Expect(agentClient.UpdateStatus(ctx, dpuInfo)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].LastTransitionTime).NotTo(Equal(originalLastTransitionTime))
			Expect(latestDPU.Status.DPUInternalStatus.Conditions[0].LastTransitionTime.After(originalLastTransitionTime.Time)).To(BeTrue())
		})

		It("non-specified fields should not be updated", func() {
			agentClient := NewZerotrustClient(kubeconfigPath, dpu.Name, dpu.Namespace)
			latestDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			hostRebootRequired := true
			initialBootID := "test-initial-boot-id"
			cond1 := metav1.Condition{
				Type:               "Cond1",
				Status:             metav1.ConditionTrue,
				Reason:             "TestReason",
				Message:            "TestMessage",
				LastTransitionTime: metav1.NewTime(time.Now().Truncate(time.Second)),
			}
			cond2 := metav1.Condition{
				Type:               "Cond2",
				Status:             metav1.ConditionTrue,
				Reason:             "TestReason",
				Message:            "TestMessage",
				LastTransitionTime: metav1.NewTime(time.Now().Truncate(time.Second)),
			}
			lastStartupTime := metav1.NewTime(time.Now().Truncate(time.Second))
			latestDPU.Status.DPUInternalStatus = &provisioningv1.DPUInternalStatus{
				LastStartupTime:    &lastStartupTime,
				HostRebootRequired: ptr.To(hostRebootRequired),
				InitialBootID:      ptr.To(initialBootID),
				Conditions: []metav1.Condition{
					cond1,
					cond2,
				},
			}
			Expect(k8sClient.Status().Update(ctx, latestDPU)).To(Succeed())

			newCond := metav1.Condition{
				Type:               "NewCond",
				Status:             metav1.ConditionTrue,
				Reason:             "TestReason",
				Message:            "TestMessage",
				LastTransitionTime: metav1.NewTime(time.Now().Truncate(time.Second)),
			}
			internalStatus := provisioningv1.DPUInternalStatus{
				Conditions: []metav1.Condition{
					newCond,
				},
			}
			Expect(agentClient.UpdateStatus(ctx, internalStatus)).To(Succeed())

			updatedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, updatedDPU)).To(Succeed())
			By("lastStartupTime should not be updated")
			Expect(updatedDPU.Status.DPUInternalStatus.LastStartupTime).NotTo(BeNil())
			Expect(updatedDPU.Status.DPUInternalStatus.LastStartupTime.Equal(&lastStartupTime)).To(BeTrue())

			By("hostRebootRequired should not be updated")
			Expect(updatedDPU.Status.DPUInternalStatus.HostRebootRequired).NotTo(BeNil())
			Expect(*updatedDPU.Status.DPUInternalStatus.HostRebootRequired).To(Equal(hostRebootRequired))

			By("initialBootID should not be updated")
			Expect(updatedDPU.Status.DPUInternalStatus.InitialBootID).NotTo(BeNil())
			Expect(*updatedDPU.Status.DPUInternalStatus.InitialBootID).To(Equal(initialBootID))

			By("existing conditions should not be removed")
			Expect(updatedDPU.Status.DPUInternalStatus.Conditions).To(HaveLen(3))
			Expect(updatedDPU.Status.DPUInternalStatus.Conditions).To(ContainElement(cond1))
			Expect(updatedDPU.Status.DPUInternalStatus.Conditions).To(ContainElement(cond2))

			By("new condition should be added")
			Expect(updatedDPU.Status.DPUInternalStatus.Conditions).To(ContainElement(newCond))
		})
	})

	It("should be able to get cluster scoped object", func() {
		agentClient := NewZerotrustClient(kubeconfigPath, dpu.Name, dpu.Namespace)
		object := &corev1.Namespace{}
		Expect(agentClient.GetObject(ctx, testNS.Name, testNS.Name, object)).To(Succeed())
		Expect(object.Name).To(Equal(testNS.Name))
		Expect(object.Spec).To(Equal(testNS.Spec))
		Expect(object.Status).To(Equal(testNS.Status))
	})

	It("should be able to get namespaced object", func() {
		agentClient := NewZerotrustClient(kubeconfigPath, dpu.Name, dpu.Namespace)
		object := &provisioningv1.DPU{}
		Expect(agentClient.GetObject(ctx, testNS.Name, dpu.Name, object)).To(Succeed())
		Expect(object.Name).To(Equal(dpu.Name))
		Expect(object.Namespace).To(Equal(dpu.Namespace))
		Expect(object.Spec).To(Equal(dpu.Spec))
		Expect(object.Status).To(Equal(dpu.Status))
	})

	It("health check should return true if k8s server is reachable", func() {
		agentClient := NewZerotrustClient(kubeconfigPath, dpu.Name, dpu.Namespace)
		Expect(agentClient.HealthCheck()).To(Succeed())
	})
})
