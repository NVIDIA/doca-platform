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
				NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
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
		ztClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, string(dpu.UID))
		Expect(err).NotTo(HaveOccurred())
		Expect(ztClient.HealthCheck()).To(Succeed())
	})

	Context("update status", func() {
		It("should be able to update status", func() {
			agentClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, string(dpu.UID))
			Expect(err).NotTo(HaveOccurred())
			lastStartupTime := metav1.NewTime(time.Now().Truncate(time.Second))
			pending := &provisioningv1.PendingNVConfigState{
				BootID: "boot-1",
				Devices: []provisioningv1.PendingNVConfigDevice{
					{
						Device: "0000:03:00.0",
						Entries: []provisioningv1.PendingNVConfigEntry{
							{
								Name:     "INTERNAL_CPU_MODEL",
								Default:  "0",
								Current:  "0",
								NextBoot: "1",
							},
						},
					},
				},
			}
			agentStatus := provisioningv1.AgentStatus{
				LastStartupTime:             &lastStartupTime,
				InitialBootID:               ptr.To("test-initial-boot-id"),
				LastObservedPendingNVConfig: pending,
				Conditions: []metav1.Condition{
					{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "TestReason",
						Message: "TestMessage",
					},
				},
			}
			Expect(agentClient.UpdateStatus(ctx, agentStatus)).To(Succeed())

			latestDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.LastStartupTime.Equal(&lastStartupTime)).To(BeTrue())
			Expect(latestDPU.Status.AgentStatus.InitialBootID).NotTo(BeNil())
			Expect(*latestDPU.Status.AgentStatus.InitialBootID).To(Equal("test-initial-boot-id"))
			Expect(latestDPU.Status.AgentStatus.LastObservedPendingNVConfig).To(Equal(pending))
			Expect(latestDPU.Status.AgentStatus.Conditions).To(HaveLen(1))
			Expect(latestDPU.Status.AgentStatus.Conditions[0].Type).To(Equal("Ready"))
			Expect(latestDPU.Status.AgentStatus.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(latestDPU.Status.AgentStatus.Conditions[0].Reason).To(Equal("TestReason"))
			Expect(latestDPU.Status.AgentStatus.Conditions[0].Message).To(Equal("TestMessage"))

			By("last transition time should be set automatically")
			Expect(latestDPU.Status.AgentStatus.Conditions[0].LastTransitionTime).NotTo(BeNil())

			By("last transition time should not be updated if condition is not changed")
			originalLastTransitionTime := latestDPU.Status.AgentStatus.Conditions[0].LastTransitionTime
			Expect(agentClient.UpdateStatus(ctx, agentStatus)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus.Conditions[0].LastTransitionTime).To(Equal(originalLastTransitionTime))

			By("last transition time should be updated if condition is changed")
			time.Sleep(2 * time.Second)
			agentStatus.Conditions[0].Status = metav1.ConditionFalse
			Expect(agentClient.UpdateStatus(ctx, agentStatus)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus.Conditions[0].LastTransitionTime).NotTo(Equal(originalLastTransitionTime))
			Expect(latestDPU.Status.AgentStatus.Conditions[0].LastTransitionTime.After(originalLastTransitionTime.Time)).To(BeTrue())
		})

		It("should reject status update with mismatched DPU UID", func() {
			agentClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, "stale-uid")
			Expect(err).NotTo(HaveOccurred())
			agentStatus := provisioningv1.AgentStatus{
				InitialBootID: ptr.To("test-boot-id"),
			}
			err = agentClient.UpdateStatus(ctx, agentStatus)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("stale DPU object"))
		})

		It("non-specified fields should not be updated", func() {
			agentClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, string(dpu.UID))
			Expect(err).NotTo(HaveOccurred())
			latestDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
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
			pending := &provisioningv1.PendingNVConfigState{
				BootID: "boot-1",
				Devices: []provisioningv1.PendingNVConfigDevice{
					{
						Device:  "0000:03:00.0",
						Entries: []provisioningv1.PendingNVConfigEntry{},
					},
				},
			}
			latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime:             &lastStartupTime,
				InitialBootID:               ptr.To(initialBootID),
				LastObservedPendingNVConfig: pending,
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
			internalStatus := provisioningv1.AgentStatus{
				Conditions: []metav1.Condition{
					newCond,
				},
			}
			Expect(agentClient.UpdateStatus(ctx, internalStatus)).To(Succeed())

			updatedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, updatedDPU)).To(Succeed())
			By("lastStartupTime should not be updated")
			Expect(updatedDPU.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
			Expect(updatedDPU.Status.AgentStatus.LastStartupTime.Equal(&lastStartupTime)).To(BeTrue())

			By("initialBootID should not be updated")
			Expect(updatedDPU.Status.AgentStatus.InitialBootID).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.InitialBootID).To(Equal(initialBootID))

			By("lastObservedPendingNVConfig should not be updated")
			Expect(updatedDPU.Status.AgentStatus.LastObservedPendingNVConfig).To(Equal(pending))

			By("existing conditions should not be removed")
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(HaveLen(3))
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(cond1))
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(cond2))

			By("new condition should be added")
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(newCond))
		})
	})

	It("should be able to get cluster scoped object", func() {
		agentClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, string(dpu.UID))
		Expect(err).NotTo(HaveOccurred())
		object := &corev1.Namespace{}
		Expect(agentClient.GetObject(ctx, testNS.Name, testNS.Name, object)).To(Succeed())
		Expect(object.Name).To(Equal(testNS.Name))
		Expect(object.Spec).To(Equal(testNS.Spec))
		Expect(object.Status).To(Equal(testNS.Status))
	})

	It("should be able to get namespaced object", func() {
		agentClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, string(dpu.UID))
		Expect(err).NotTo(HaveOccurred())
		object := &provisioningv1.DPU{}
		Expect(agentClient.GetObject(ctx, testNS.Name, dpu.Name, object)).To(Succeed())
		Expect(object.Name).To(Equal(dpu.Name))
		Expect(object.Namespace).To(Equal(dpu.Namespace))
		Expect(object.Spec).To(Equal(dpu.Spec))
		Expect(object.Status).To(Equal(dpu.Status))
	})

	It("health check should return true if k8s server is reachable", func() {
		agentClient, err := NewZerotrustClient(testCfg, dpu.Name, dpu.Namespace, string(dpu.UID))
		Expect(err).NotTo(HaveOccurred())
		Expect(agentClient.HealthCheck()).To(Succeed())
	})
})
