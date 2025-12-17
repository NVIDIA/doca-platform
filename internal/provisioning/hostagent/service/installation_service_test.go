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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("InstallationService", func() {
	var address string
	var testNS *corev1.Namespace
	var installationService *InstallationService

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

		address = "localhost:11029"
		installationService = NewInstallationService(k8sClient, address)
		Expect(installationService.Start(false)).To(Succeed())
	})

	AfterEach(func() {
		installationService.Stop()
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("update status", func() {
		It("should update status", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			request := UpdateStatusRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				DPUInfo: provisioningv1.DPUInfo{
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
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			updatedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, updatedDPU)).To(Succeed())
			Expect(updatedDPU.Status.Phase).To(Equal(dpu.Status.Phase))
			Expect(updatedDPU.Status.Conditions).To(ContainElement(dpu.Status.Conditions[0]))
			Expect(updatedDPU.Status.DPUInfo).NotTo(BeNil())
			Expect(updatedDPU.Status.DPUInfo.HostRebootRequired).NotTo(BeNil())
			Expect(*updatedDPU.Status.DPUInfo.HostRebootRequired).To(Equal(*request.DPUInfo.HostRebootRequired))
			Expect(updatedDPU.Status.DPUInfo.Conditions[0].Type).To(Equal(request.DPUInfo.Conditions[0].Type))
			Expect(updatedDPU.Status.DPUInfo.Conditions[0].Status).To(Equal(request.DPUInfo.Conditions[0].Status))
			Expect(updatedDPU.Status.DPUInfo.Conditions[0].Reason).To(Equal(request.DPUInfo.Conditions[0].Reason))
			Expect(updatedDPU.Status.DPUInfo.Conditions[0].Message).To(Equal(request.DPUInfo.Conditions[0].Message))
		})

		It("should fail if DPU not found", func() {
			request := UpdateStatusRequest{
				DPUName:      "test-dpu-not-found",
				DPUNamespace: testNS.Name,
				DPUInfo: provisioningv1.DPUInfo{
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
})
