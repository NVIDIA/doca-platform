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

package gnoi_test

import (
	"context"
	"fmt"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/gnoi"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Phase HostNetworkConfig", func() {
	var (
		defaultFlavorName = "flavor-hostnetworkconfig-test"
		defaultDPUName    = "dpu-hostnetworkconfig-test"
	)

	var setupSucc = func(_ context.Context, _ *gnmi.GetRequest) (*gnmi.GetResponse, error) { //nolint:unparam
		return &gnmi.GetResponse{
			Notification: []*gnmi.Notification{
				{
					Update: []*gnmi.Update{
						{
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_IntVal{
									IntVal: 0,
								},
							},
						},
					},
				},
			},
		}, nil
	}

	var prepareDPUFlavor = func() *provisioningv1.DPUFlavor {
		flavor := flavorObj(defaultFlavorName)
		createObject(flavor)
		return flavor
	}

	var prepareDPFOperatorConfig = func() *operatorv1.DPFOperatorConfig {
		dpfOperatorConfig := dpfOperatorConfigObj(defaultDPFOperatorConfig)
		createObject(dpfOperatorConfig)
		return dpfOperatorConfig
	}

	Context("successful cases", func() {
		It("should set the host network config to the DPU", func() {
			setupDMS(&localDMS{get: setupSucc})
			flavor := prepareDPUFlavor()
			_ = prepareDPFOperatorConfig()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
			var status provisioningv1.DPUStatus
			var err error
			Eventually(func() provisioningv1.DPUPhase {
				status, err = gnoi.SetupNetwork(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
						},
					})
				return status.Phase
			}).WithTimeout(10 * time.Second).Should(Equal(provisioningv1.DPUClusterConfig))
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondHostNetworkReady.String()),
				),
			))
		})
		It("DPFOperatorConfig networking comes with a default value", func() {
			setupDMS(&localDMS{get: setupSucc})
			flavor := prepareDPUFlavor()

			dpfOperatorConfig := dpfOperatorConfigObj(defaultDPFOperatorConfig)
			By("even if we set Networking to nil, a default value should be assigned by the CRD")
			dpfOperatorConfig.Spec.Networking = nil
			createObject(dpfOperatorConfig)

			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
			var status provisioningv1.DPUStatus
			var err error
			Eventually(func() provisioningv1.DPUPhase {
				status, err = gnoi.SetupNetwork(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
						},
					})
				return status.Phase
			}).WithTimeout(10 * time.Second).Should(Equal(provisioningv1.DPUClusterConfig))
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondHostNetworkReady.String()),
				),
			))
		})
	})

	Context("error handling", func() {
		It("should retry if flavor is not found", func() {
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "a-flavor-that-does-not-exist")
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
			status, err := gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUFlavorNotFound"),
				),
			))
		})
		It("should retry if no pci address is found", func() {
			flavor := prepareDPUFlavor()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Spec.PCIAddress = nil
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
			status, err := gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "GetPCIAddrFromDPUError"),
				),
			))
		})
		It("should retry if no DPFOperatorConfig is found", func() {
			flavor := prepareDPUFlavor()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
			status, err := gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPFOperatorConfigCountError"),
				),
			))
		})
		It("should retry if task is in progress", func() {
			ch := make(chan struct{})
			defer close(ch) //nolint: errcheck
			hangingGet := func(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
				close(ch)
				return setupSucc(ctx, req)
			}
			setupDMS(&localDMS{get: hangingGet})
			flavor := prepareDPUFlavor()
			prepareDPFOperatorConfig()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration

			By("first run, an async task should be created")
			status, err := gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))

			By("second run, should return an inProgressError")
			status, err = gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "HostNetworkSetupInProgress"),
				),
			))
		})

		It("should retry if hostNetworkHandler fails", func() {
			By("simulating a situation that DMS is not accessible")
			setupDMS(&localDMS{get: setupSucc})
			dmsServer.Stop()

			flavor := prepareDPUFlavor()
			prepareDPFOperatorConfig()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration

			By("first run, an async task should be created")
			status, err := gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))

			By("the task should return an error")
			hostNetworkTaskName := fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name)
			task, ok := dutil.HostNetworkTaskMap.Load(hostNetworkTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			taskWithRetry := task.(dutil.TaskWithRetry)
			Expect(taskWithRetry.RetryCount).To(Equal(0))
			Eventually(func() future.FurtureTaskState {
				return taskWithRetry.Task.GetState()
			}).WithTimeout(10 * time.Second).Should(Equal(future.Ready))
			_, err = taskWithRetry.Task.GetResult()
			Expect(err).To(HaveOccurred())

			By("second run, a new task should be created with a retry count of 1")
			status, err = gnoi.SetupNetwork(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("operation in progress"))
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "HostNetworkSetupInProgress"),
				),
			))
			task, ok = dutil.HostNetworkTaskMap.Load(hostNetworkTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			taskWithRetry = task.(dutil.TaskWithRetry)
			Expect(taskWithRetry.RetryCount).To(Equal(1))
		})
		It("should Error if the retry count exceeds the max retry count", func() {
			By("simulating a situation that DMS is not accessible, and calculate the number of API calls")
			cnt := 0
			getCnt := func(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
				cnt++
				return nil, grpcstatus.Errorf(codes.Unavailable, "DMS is not accessible")
			}
			setupDMS(&localDMS{get: getCnt})

			flavor := prepareDPUFlavor()
			prepareDPFOperatorConfig()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration

			By("keep running until the retry count exceeds the max retry count")
			var status provisioningv1.DPUStatus
			var err error
			Eventually(func() provisioningv1.DPUPhase {
				status, err = gnoi.SetupNetwork(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
					})
				return status.Phase
			}, 30*time.Second, 1*time.Second).Should(Equal(provisioningv1.DPUError))
			Expect(err).To(Succeed())
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondHostNetworkReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "HostNetworkSetupFailed"),
				),
			))
			Expect(cnt).To(Equal(dutil.MaxRetryCount + 1))
		})
	})
})
