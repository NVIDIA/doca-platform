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

package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("InstallationService", func() {
	var testNS *corev1.Namespace
	var installationService *InstallationService
	// Use the constant from installation_service.go
	address := localhostAddr

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
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "installation-service-testns-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())

		installationService = NewInstallationService(k8sClient)
		Expect(installationService.Start(false)).To(Succeed())
		// Start() runs the server in a goroutine; wait until it is listening to avoid connection refused.
		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://%s/healthz", address))
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("healthz returned %d", resp.StatusCode)
			}
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
	})

	AfterEach(func() {
		installationService.Stop()
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("update status", func() {
		It("should update status", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			lastStartupTime := metav1.NewTime(time.Now().Truncate(time.Second))
			request := types.UpdateStatusRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				AgentStatus: provisioningv1.AgentStatus{
					LastStartupTime:    &lastStartupTime,
					HostRebootRequired: ptr.To(true),
					InitialBootID:      ptr.To("test-initial-boot-id"),
					Conditions: []metav1.Condition{
						{
							Type:    string(provisioningv1.ReadyForReboot),
							Status:  metav1.ConditionTrue,
							Reason:  string(provisioningv1.ReadyForReboot),
							Message: string(provisioningv1.ReadyForReboot),
						},
					},
				},
			}
			req, err := json.Marshal(request)
			Expect(err).To(Succeed())

			resp, err := http.Post(fmt.Sprintf("http://%s/update-status", address), "application/json", bytes.NewBuffer(req))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			updatedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, updatedDPU)).To(Succeed())
			Expect(updatedDPU.Status.Phase).To(Equal(dpu.Status.Phase))
			Expect(updatedDPU.Status.Conditions).To(ContainElement(dpu.Status.Conditions[0]))
			Expect(updatedDPU.Status.AgentStatus).NotTo(BeNil())
			Expect(updatedDPU.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
			Expect(updatedDPU.Status.AgentStatus.LastStartupTime.Equal(&lastStartupTime)).To(BeTrue())
			Expect(updatedDPU.Status.AgentStatus.HostRebootRequired).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.HostRebootRequired).To(Equal(*request.AgentStatus.HostRebootRequired))
			Expect(updatedDPU.Status.AgentStatus.InitialBootID).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.InitialBootID).To(Equal(*request.AgentStatus.InitialBootID))
			Expect(updatedDPU.Status.AgentStatus.Conditions[0].Type).To(Equal(request.AgentStatus.Conditions[0].Type))
			Expect(updatedDPU.Status.AgentStatus.Conditions[0].Status).To(Equal(request.AgentStatus.Conditions[0].Status))
			Expect(updatedDPU.Status.AgentStatus.Conditions[0].Reason).To(Equal(request.AgentStatus.Conditions[0].Reason))
			Expect(updatedDPU.Status.AgentStatus.Conditions[0].Message).To(Equal(request.AgentStatus.Conditions[0].Message))

			By("last transition time should be set automatically")
			Expect(updatedDPU.Status.AgentStatus.Conditions[0].LastTransitionTime).NotTo(BeNil())
		})

		It("non-specified fields should not be updated", func() {
			dpu := createDPU("test-dpu", testNS.Name)
			latestDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, latestDPU)).To(Succeed())
			lastStartupTime := metav1.NewTime(time.Now().Truncate(time.Second))
			hostRebootRequired := true
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
			latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime:    &lastStartupTime,
				HostRebootRequired: ptr.To(hostRebootRequired),
				InitialBootID:      ptr.To("test-initial-boot-id"),
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
			request := types.UpdateStatusRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				AgentStatus: provisioningv1.AgentStatus{
					Conditions: []metav1.Condition{
						newCond,
					},
				},
			}
			req, err := json.Marshal(request)
			Expect(err).To(Succeed())
			resp, err := http.Post(fmt.Sprintf("http://%s/update-status", address), "application/json", bytes.NewBuffer(req))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			updatedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, updatedDPU)).To(Succeed())
			By("lastStartupTime should not be updated")
			Expect(updatedDPU.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
			Expect(updatedDPU.Status.AgentStatus.LastStartupTime.Equal(&lastStartupTime)).To(BeTrue())

			By("hostRebootRequired should not be updated")
			Expect(updatedDPU.Status.AgentStatus.HostRebootRequired).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.HostRebootRequired).To(Equal(hostRebootRequired))

			By("initialBootID should not be updated")
			Expect(updatedDPU.Status.AgentStatus.InitialBootID).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.InitialBootID).To(Equal("test-initial-boot-id"))

			By("existing conditions should not be removed")
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(HaveLen(3))
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(cond1))
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(cond2))

			By("new condition should be added")
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(newCond))
		})

		It("should fail if DPU not found", func() {
			request := types.UpdateStatusRequest{
				DPUName:      "test-dpu-not-found",
				DPUNamespace: testNS.Name,
				AgentStatus: provisioningv1.AgentStatus{
					HostRebootRequired: ptr.To(true),
					Conditions: []metav1.Condition{
						{
							Type:    string(provisioningv1.ReadyForReboot),
							Status:  metav1.ConditionTrue,
							Reason:  string(provisioningv1.ReadyForReboot),
							Message: string(provisioningv1.ReadyForReboot),
						},
					},
				},
			}
			req, err := json.Marshal(request)
			Expect(err).To(Succeed())
			resp, err := http.Post(fmt.Sprintf("http://%s/update-status", address), "application/json", bytes.NewBuffer(req))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Context("get object", func() {
		It("should get DPU (namespaced object)", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			// GET /get-object?group=...&version=...&kind=...&namespace=...&name=...
			url := fmt.Sprintf("http://%s/get-object?group=%s&version=%s&kind=%s&namespace=%s&name=%s",
				address,
				provisioningv1.GroupVersion.Group,
				provisioningv1.GroupVersion.Version,
				provisioningv1.DPUKind,
				testNS.Name,
				dpu.Name)
			resp, err := http.Get(url)
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			responseDPU := &provisioningv1.DPU{}
			Expect(json.NewDecoder(resp.Body).Decode(&responseDPU)).To(Succeed())
			Expect(responseDPU.Name).To(Equal(dpu.Name))
			Expect(responseDPU.Namespace).To(Equal(dpu.Namespace))
			Expect(responseDPU.Spec).To(Equal(dpu.Spec))
			Expect(responseDPU.Status).To(Equal(dpu.Status))
		})

		It("should get namespace (cluster-scoped object)", func() {
			// Namespace: core API group (empty), v1, Namespace
			// No gourp and namespace param for cluster-scoped objects
			url := fmt.Sprintf("http://%s/get-object?version=v1&kind=Namespace&name=%s", address, testNS.Name)
			resp, err := http.Get(url)
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			responseNamespace := &corev1.Namespace{}
			Expect(json.NewDecoder(resp.Body).Decode(&responseNamespace)).To(Succeed())
			Expect(responseNamespace.Name).To(Equal(testNS.Name))
			Expect(responseNamespace.Spec).To(Equal(testNS.Spec))
			Expect(responseNamespace.Status).To(Equal(testNS.Status))
		})
	})

	Context("health check", func() {
		It("should return OK", func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/healthz", address))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

})
