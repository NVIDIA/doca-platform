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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

type mockNetworkConfigurator struct {
	addNetworkRequestFunc func(dpu *provisioningv1.DPU, vfCount *int) error
	rebindHostDriverFunc  func(ctx context.Context, dpu *provisioningv1.DPU) error
}

func (m *mockNetworkConfigurator) AddNetworkRequest(dpu *provisioningv1.DPU, vfCount *int) error {
	if m.addNetworkRequestFunc != nil {
		return m.addNetworkRequestFunc(dpu, vfCount)
	}
	return nil
}

func (m *mockNetworkConfigurator) RebindHostDriver(ctx context.Context, dpu *provisioningv1.DPU) error {
	if m.rebindHostDriverFunc != nil {
		return m.rebindHostDriverFunc(ctx, dpu)
	}
	return nil
}

type mockRebootHandler struct {
	runSLRFunc        func(context.Context, []provisioningv1.DPU) error
	runPowerCycleFunc func(*provisioningv1.DPUNode, []provisioningv1.DPU) error
}

func (m *mockRebootHandler) RunSLR(ctx context.Context, dpus []provisioningv1.DPU) error {
	if m.runSLRFunc != nil {
		return m.runSLRFunc(ctx, dpus)
	}
	return nil
}

func (m *mockRebootHandler) RunPowerCycle(dpuNode *provisioningv1.DPUNode, dpus []provisioningv1.DPU) error {
	if m.runPowerCycleFunc != nil {
		return m.runPowerCycleFunc(dpuNode, dpus)
	}
	return nil
}

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

	var createDPUNode = func(name string, namespace string) *provisioningv1.DPUNode {
		dpuNode := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: provisioningv1.DPUNodeSpec{
				NodeRebootMethod: &provisioningv1.NodeRebootMethod{
					HostAgent: &provisioningv1.HostAgent{},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())
		return dpuNode
	}

	BeforeEach(func() {
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "installation-service-testns-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())

		installationService = NewInstallationService(k8sClient, nil, nil)
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
			request := types.UpdateStatusRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				DPUUID:       string(dpu.UID),
				AgentStatus: provisioningv1.AgentStatus{
					LastStartupTime:             &lastStartupTime,
					InitialBootID:               ptr.To("test-initial-boot-id"),
					LastObservedPendingNVConfig: pending,
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
			Expect(updatedDPU.Status.AgentStatus.InitialBootID).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.InitialBootID).To(Equal(*request.AgentStatus.InitialBootID))
			Expect(updatedDPU.Status.AgentStatus.LastObservedPendingNVConfig).To(Equal(pending))
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
				LastStartupTime: &lastStartupTime,
				InitialBootID:   ptr.To("test-initial-boot-id"),
				LastObservedPendingNVConfig: &provisioningv1.PendingNVConfigState{
					BootID: "boot-1",
					Devices: []provisioningv1.PendingNVConfigDevice{
						{
							Device:  "0000:03:00.0",
							Entries: []provisioningv1.PendingNVConfigEntry{},
						},
					},
				},
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
				DPUUID:       string(dpu.UID),
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

			By("initialBootID should not be updated")
			Expect(updatedDPU.Status.AgentStatus.InitialBootID).NotTo(BeNil())
			Expect(*updatedDPU.Status.AgentStatus.InitialBootID).To(Equal("test-initial-boot-id"))

			By("lastObservedPendingNVConfig should not be updated")
			Expect(updatedDPU.Status.AgentStatus.LastObservedPendingNVConfig).To(Equal(latestDPU.Status.AgentStatus.LastObservedPendingNVConfig))

			By("existing conditions should not be removed")
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(HaveLen(3))
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(cond1))
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(cond2))

			By("new condition should be added")
			Expect(updatedDPU.Status.AgentStatus.Conditions).To(ContainElement(newCond))
		})

		It("should reject status update with mismatched DPU UID", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			request := types.UpdateStatusRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				DPUUID:       "stale-uid-from-old-agent",
				AgentStatus: provisioningv1.AgentStatus{
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
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))

			updatedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, updatedDPU)).To(Succeed())
			Expect(updatedDPU.Status.AgentStatus).To(BeNil())
		})

		It("should fail if DPU not found", func() {
			request := types.UpdateStatusRequest{
				DPUName:      "test-dpu-not-found",
				DPUNamespace: testNS.Name,
				DPUUID:       "some-uid",
				AgentStatus: provisioningv1.AgentStatus{
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

	Context("configure host VF", func() {
		It("should return 400 when request body is malformed", func() {
			resp, err := http.Post(fmt.Sprintf("http://%s/configure-host-vfs", address), "application/json", bytes.NewBufferString("not-json"))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should return 503 when network manager is not configured", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			request := types.ConfigureHostVFsRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
			}
			req, err := json.Marshal(request)
			Expect(err).To(Succeed())

			resp, err := http.Post(fmt.Sprintf("http://%s/configure-host-vfs", address), "application/json", bytes.NewBuffer(req))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		Context("when network manager is configured", func() {
			var mockNM *mockNetworkConfigurator

			BeforeEach(func() {
				mockNM = &mockNetworkConfigurator{}
				installationService.networkManager = mockNM
			})

			It("should successfully configure host VF with VFCount", func() {
				dpu := createDPU("test-dpu", testNS.Name)

				var receivedDPU *provisioningv1.DPU
				var receivedVFCount *int
				mockNM.addNetworkRequestFunc = func(dpu *provisioningv1.DPU, vfCount *int) error {
					receivedDPU = dpu
					receivedVFCount = vfCount
					return nil
				}

				vfCount := 16
				request := types.ConfigureHostVFsRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					VFCount:      &vfCount,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(fmt.Sprintf("http://%s/configure-host-vfs", address), "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(receivedDPU).NotTo(BeNil())
				Expect(receivedDPU.Name).To(Equal(dpu.Name))
				Expect(receivedDPU.Namespace).To(Equal(dpu.Namespace))

				By("the full DPU spec should be passed to AddNetworkRequest")
				Expect(receivedDPU.Spec.SerialNumber).To(Equal(dpu.Spec.SerialNumber))
				Expect(receivedDPU.Spec.DPUFlavor).To(Equal(dpu.Spec.DPUFlavor))
				Expect(receivedDPU.Spec.BFB).To(Equal(dpu.Spec.BFB))

				By("VFCount should be passed through to AddNetworkRequest")
				Expect(receivedVFCount).NotTo(BeNil())
				Expect(*receivedVFCount).To(Equal(16))
			})

			It("should return 404 when DPU not found", func() {
				request := types.ConfigureHostVFsRequest{
					DPUName:      "non-existent-dpu",
					DPUNamespace: testNS.Name,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(fmt.Sprintf("http://%s/configure-host-vfs", address), "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("should return 500 when AddNetworkRequest fails", func() {
				dpu := createDPU("test-dpu", testNS.Name)

				mockNM.addNetworkRequestFunc = func(dpu *provisioningv1.DPU, vfCount *int) error {
					return fmt.Errorf("network manager is not initialized")
				}

				request := types.ConfigureHostVFsRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(fmt.Sprintf("http://%s/configure-host-vfs", address), "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Context("trigger reboot", func() {
		var triggerServer *httptest.Server
		var triggerRebootURL string

		BeforeEach(func() {
			triggerServer = httptest.NewServer(installationService.handler)
			triggerRebootURL = triggerServer.URL + "/trigger-reboot"
		})

		AfterEach(func() {
			triggerServer.Close()
		})

		It("should return 400 when request body is malformed", func() {
			resp, err := http.Post(triggerRebootURL, "application/json", bytes.NewBufferString("not-json"))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should return 503 when reboot handler is not configured", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			request := types.TriggerRebootRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				DPUUID:       string(dpu.UID),
				RebootMethod: provisioningv1.RebootMethodPowerCycle,
			}
			req, err := json.Marshal(request)
			Expect(err).To(Succeed())

			resp, err := http.Post(triggerRebootURL, "application/json", bytes.NewBuffer(req))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		Context("when reboot handler is configured", func() {
			var mockRH *mockRebootHandler

			BeforeEach(func() {
				mockRH = &mockRebootHandler{}
				installationService.rebootHandler = mockRH
			})

			It("should return 404 when DPU is not found", func() {
				request := types.TriggerRebootRequest{
					DPUName:      "non-existent-dpu",
					DPUNamespace: testNS.Name,
					DPUUID:       "some-uid",
					RebootMethod: provisioningv1.RebootMethodSystemLevelReset,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(triggerRebootURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("should return 409 when DPU UID does not match", func() {
				dpu := createDPU("test-dpu", testNS.Name)

				request := types.TriggerRebootRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       "stale-uid-from-old-agent",
					RebootMethod: provisioningv1.RebootMethodSystemLevelReset,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(triggerRebootURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("should dispatch SLR reboot methods to the reboot handler", func() {
				dpu := createDPU("test-dpu", testNS.Name)

				var receivedDPUs []provisioningv1.DPU
				mockRH.runSLRFunc = func(_ context.Context, dpus []provisioningv1.DPU) error {
					receivedDPUs = dpus
					return nil
				}

				request := types.TriggerRebootRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       string(dpu.UID),
					RebootMethod: provisioningv1.RebootMethodSystemLevelReset,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(triggerRebootURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(receivedDPUs).To(HaveLen(1))
				Expect(receivedDPUs[0].Name).To(Equal(dpu.Name))
				Expect(receivedDPUs[0].Namespace).To(Equal(dpu.Namespace))
			})

			It("should dispatch PowerCycle reboot method with the DPUNode", func() {
				dpu := createDPU("test-dpu", testNS.Name)
				dpuNode := createDPUNode(dpu.Spec.DPUNodeName, dpu.Namespace)

				var receivedDPUNode *provisioningv1.DPUNode
				var receivedDPUs []provisioningv1.DPU
				mockRH.runPowerCycleFunc = func(node *provisioningv1.DPUNode, dpus []provisioningv1.DPU) error {
					receivedDPUNode = node
					receivedDPUs = dpus
					return nil
				}

				request := types.TriggerRebootRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       string(dpu.UID),
					RebootMethod: provisioningv1.RebootMethodPowerCycle,
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(triggerRebootURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(receivedDPUNode).NotTo(BeNil())
				Expect(receivedDPUNode.Name).To(Equal(dpuNode.Name))
				Expect(receivedDPUNode.Namespace).To(Equal(dpuNode.Namespace))
				Expect(receivedDPUs).To(HaveLen(1))
				Expect(receivedDPUs[0].Name).To(Equal(dpu.Name))
			})
		})
	})

	Context("rebind host driver", func() {
		var rebindServer *httptest.Server
		var rebindURL string

		BeforeEach(func() {
			rebindServer = httptest.NewServer(installationService.handler)
			rebindURL = rebindServer.URL + "/rebind-host-driver"
		})

		AfterEach(func() {
			rebindServer.Close()
		})

		It("should return 400 when request body is malformed", func() {
			resp, err := http.Post(rebindURL, "application/json", bytes.NewBufferString("not-json"))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should return 503 when network manager is not configured", func() {
			dpu := createDPU("test-dpu", testNS.Name)

			request := types.RebindHostDriverRequest{
				DPUName:      dpu.Name,
				DPUNamespace: dpu.Namespace,
				DPUUID:       string(dpu.UID),
			}
			req, err := json.Marshal(request)
			Expect(err).To(Succeed())

			resp, err := http.Post(rebindURL, "application/json", bytes.NewBuffer(req))
			Expect(err).To(Succeed())
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		Context("when network manager is configured", func() {
			var mockNM *mockNetworkConfigurator

			BeforeEach(func() {
				mockNM = &mockNetworkConfigurator{}
				installationService.networkManager = mockNM
			})

			It("should return 404 when DPU is not found", func() {
				request := types.RebindHostDriverRequest{
					DPUName:      "missing-dpu",
					DPUNamespace: testNS.Name,
					DPUUID:       "some-uid",
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(rebindURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})

			It("should return 409 when DPU UID does not match", func() {
				dpu := createDPU("test-dpu", testNS.Name)

				request := types.RebindHostDriverRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       "stale-uid",
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(rebindURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})

			It("should return 200 when rebind succeeds", func() {
				dpu := createDPU("test-dpu", testNS.Name)
				var rebound *provisioningv1.DPU
				mockNM.rebindHostDriverFunc = func(_ context.Context, got *provisioningv1.DPU) error {
					rebound = got
					return nil
				}

				request := types.RebindHostDriverRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       string(dpu.UID),
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(rebindURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(rebound).NotTo(BeNil())
				Expect(rebound.Name).To(Equal(dpu.Name))
				Expect(rebound.UID).To(Equal(dpu.UID))
			})

			It("should return 500 when rebind fails", func() {
				dpu := createDPU("test-dpu", testNS.Name)
				mockNM.rebindHostDriverFunc = func(_ context.Context, _ *provisioningv1.DPU) error {
					return fmt.Errorf("rebind failed")
				}

				request := types.RebindHostDriverRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       string(dpu.UID),
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(rebindURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			})

			It("should return 409 when rebind is already in progress", func() {
				dpu := createDPU("test-dpu", testNS.Name)
				mockNM.rebindHostDriverFunc = func(_ context.Context, _ *provisioningv1.DPU) error {
					return fmt.Errorf("%w for DPU %s/%s", types.ErrRebindInProgress, dpu.Namespace, dpu.Name)
				}

				request := types.RebindHostDriverRequest{
					DPUName:      dpu.Name,
					DPUNamespace: dpu.Namespace,
					DPUUID:       string(dpu.UID),
				}
				req, err := json.Marshal(request)
				Expect(err).To(Succeed())

				resp, err := http.Post(rebindURL, "application/json", bytes.NewBuffer(req))
				Expect(err).To(Succeed())
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			})
		})
	})

})
