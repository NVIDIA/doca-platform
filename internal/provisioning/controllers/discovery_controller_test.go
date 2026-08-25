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

package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Redfish Mock Server Tests", func() {
	const (
		RedfishTestNS          = "dpf-redfish-test"
		RedfishTestBMCPassword = "TestPassword123"
		RedfishTestBMCVersion  = "BF-24.10"
		timeout                = time.Second * 30
		interval               = time.Millisecond * 250
	)

	var (
		testNS            *corev1.Namespace
		dpfOperatorConfig *operatorv1.DPFOperatorConfig
		mockServer        *mock.RedfishMockServer
		redfishClient     *rfclient.Client
		bmcSecret         *corev1.Secret
	)

	BeforeEach(func() {
		By("Setting up test namespace")
		testNS = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "dpu-redfish-test-",
			},
		}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())

		By("Creating mock Redfish server")
		var err error
		mockServer, err = mock.CreateMockRedfishServer(RedfishTestBMCVersion, RedfishTestBMCPassword)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			if mockServer != nil {
				mockServer.Stop()
			}
		})

		By("Creating Redfish client")
		redfishClient, err = mockServer.GetClient()
		Expect(err).NotTo(HaveOccurred())

		By("Setting up DpfOperatorConfig")
		dpfOperatorConfig = &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-operator-config",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeZeroTrust,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("bfb-pvc"),
					InstallInterface: &operatorv1.ProvisioningInstallInterface{
						InstallViaRedfish: &operatorv1.InstallViaRedfish{
							BFBRegistryAddress: "http://localhost:8080",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dpfOperatorConfig)).To(Succeed())

		By("Setting up BMC shared password secret")
		bmcSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rfclient.BMCPasswordSecret,
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"password": []byte(RedfishTestBMCPassword),
			},
		}
		Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

	})

	AfterEach(func() {
		By("Cleaning up test resources")

		if bmcSecret != nil {
			Expect(k8sClient.Delete(ctx, bmcSecret)).To(Succeed())
		}

		if dpfOperatorConfig != nil {
			Expect(k8sClient.Delete(ctx, dpfOperatorConfig)).To(Succeed())
		}

		if testNS != nil {
			Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
		}
	})

	Context("When testing Redfish mock server functionality", func() {
		It("Should successfully connect to Redfish mock server", func() {
			By("Testing Redfish client connection")
			Expect(redfishClient).NotTo(BeNil())
			Expect(mockServer.URL()).NotTo(BeEmpty())

			By("Verifying Redfish client can make API calls")
			// Test a simple API call to verify connectivity
			resp, service, err := redfishClient.GetRootService()
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.StatusCode()).To(Equal(200))
			Expect(service).NotTo(BeNil())
			Expect(service.Product).To(Equal("BlueField-3 DPU"))

			By("Verifying mock server configuration")
			Expect(mockServer.GetIPAddress()).NotTo(BeEmpty())
			Expect(mockServer.GetPort()).To(BeNumerically(">", 0))
		})

		It("DPU discovery should successfully discover DPU devices", func() {
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())
			dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.SkipDPUNodeDiscovery = ptr.To(false)
			Expect(k8sClient.Update(ctx, dpfOperatorConfig)).To(Succeed())

			By("verifying the unset factory reset policy remains unset in the DPFOperatorConfig")
			storedConfig := &operatorv1.DPFOperatorConfig{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), storedConfig)).To(Succeed())
			Expect(storedConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.
				DiscoveredDPUDeviceBMCFactoryResetPolicy).To(BeEmpty())

			By("Getting mock server address information")
			Expect(bmcIP).NotTo(BeEmpty())
			Expect(bmcPort).To(BeNumerically(">", 0))

			By("verifying no DpuNodes and DpuDevices exist")
			dpuNodeList := &provisioningv1.DPUNodeList{}
			Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
			Expect(dpuNodeList.Items).To(BeEmpty())

			dpuDeviceList := &provisioningv1.DPUDeviceList{}
			Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
			Expect(dpuDeviceList.Items).To(BeEmpty())

			By("creating DpuDiscovery")
			discovery := &provisioningv1.DPUDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dpu-discovery-",
					Namespace:    testNS.Name,
				},
				Spec: provisioningv1.DPUDiscoverySpec{
					IPRangeSpec: provisioningv1.IPRangeValidationSpec{
						IPRange: provisioningv1.IPRange{
							StartIP: bmcIP,
							EndIP:   bmcIP,
							Port:    &bmcPort,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, discovery)).To(Or(Succeed(), MatchError(ContainSubstring("not found"))))
			})

			By("waiting for DPU discovery to complete and create DPU devices")
			Eventually(func() int {
				dpuDeviceList := &provisioningv1.DPUDeviceList{}
				err := k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))
				Expect(err).NotTo(HaveOccurred())
				return len(dpuDeviceList.Items)
			}, timeout, interval).Should(Equal(1))

			By("verifying the created DPU device")
			Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
			Expect(dpuDeviceList.Items).To(HaveLen(1))

			dpuDevice := dpuDeviceList.Items[0]
			Expect(dpuDevice.Spec.BMCIP).NotTo(BeNil())
			Expect(*dpuDevice.Spec.BMCIP).To(Equal(bmcIP))
			Expect(dpuDevice.Spec.BMCFactoryResetPolicy).To(Equal(provisioningv1.BMCFactoryResetPolicyOnInitialization))

			By("waiting for DPU node to be created")
			Eventually(func() int {
				dpuNodeList := &provisioningv1.DPUNodeList{}
				err := k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))
				Expect(err).NotTo(HaveOccurred())
				return len(dpuNodeList.Items)
			}, timeout, interval).Should(Equal(1))

			By("deleting the DPU device")
			Expect(k8sClient.Delete(ctx, &dpuDevice)).To(Succeed())

			By("waiting for DPU node to be deleted")
			Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
			if len(dpuNodeList.Items) > 0 {
				dpuNode := dpuNodeList.Items[0]
				Expect(k8sClient.Delete(ctx, &dpuNode)).To(Succeed())
			}
		})

		It("DPU discovery should not create DPU nodes if SkipDPUNodeDiscovery is true", func() {
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())
			dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.SkipDPUNodeDiscovery = ptr.To(true)

			By("setting SkipDPUNodeDiscovery to true")
			Expect(k8sClient.Update(ctx, dpfOperatorConfig)).To(Succeed())

			By("verifying no DpuNodes and DpuDevices exist")
			dpuNodeList := &provisioningv1.DPUNodeList{}
			Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
			Expect(dpuNodeList.Items).To(BeEmpty())

			dpuDeviceList := &provisioningv1.DPUDeviceList{}
			Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
			Expect(dpuDeviceList.Items).To(BeEmpty())

			By("Creating DpuDiscovery")
			discovery := &provisioningv1.DPUDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dpu-discovery-",
					Namespace:    testNS.Name,
				},
				Spec: provisioningv1.DPUDiscoverySpec{
					IPRangeSpec: provisioningv1.IPRangeValidationSpec{
						IPRange: provisioningv1.IPRange{
							StartIP: bmcIP,
							EndIP:   bmcIP,
							Port:    &bmcPort,
						},
					},
					ScanInterval: metav1.Duration{Duration: time.Hour * 1},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, discovery)).To(Or(Succeed(), MatchError(ContainSubstring("not found"))))
			})

			By("waiting for DPU discovery to complete and create DPU devices")
			Eventually(func() int {
				err := k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))
				Expect(err).NotTo(HaveOccurred())
				return len(dpuDeviceList.Items)
			}, timeout, interval).Should(Equal(1))

			Expect(k8sClient.List(ctx, dpuNodeList, client.InNamespace(testNS.Name))).To(Succeed())
			Expect(dpuNodeList.Items).To(BeEmpty())

			dpuDevice := dpuDeviceList.Items[0]
			Expect(dpuDevice.Spec.BMCIP).NotTo(BeNil())
			Expect(*dpuDevice.Spec.BMCIP).To(Equal(bmcIP))

			Expect(k8sClient.Delete(ctx, &dpuDevice)).To(Succeed())

			By("trigger reconsile DpuDiscovery by modifying the spec")
			parts := strings.Split(bmcIP, ".")
			Expect(parts).To(HaveLen(4))
			lastOctet, err := strconv.Atoi(parts[3])
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(discovery), discovery)).To(Succeed())
			discovery.Spec.IPRangeSpec.IPRange.EndIP = fmt.Sprintf("%s.%s.%s.%d", parts[0], parts[1], parts[2], lastOctet+1)
			Expect(k8sClient.Update(ctx, discovery)).To(Succeed())

			By("waiting for DPU discovery to complete and create DPU devices")
			Eventually(func() int {
				err := k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))
				Expect(err).NotTo(HaveOccurred())
				return len(dpuDeviceList.Items)
			}, timeout, interval).Should(Equal(1))

			dpuDevice = dpuDeviceList.Items[0]
			Expect(k8sClient.Delete(ctx, &dpuDevice)).To(Succeed())

		})

		It("DPU discovery should stamp the operator config's BMC factory reset policy on the devices it creates", func() {
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())

			By("opting discovered devices out of the BMC factory reset")
			installViaRedfish := dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish
			installViaRedfish.SkipDPUNodeDiscovery = ptr.To(true)
			installViaRedfish.DiscoveredDPUDeviceBMCFactoryResetPolicy = provisioningv1.BMCFactoryResetPolicyNever
			Expect(k8sClient.Update(ctx, dpfOperatorConfig)).To(Succeed())

			By("creating DpuDiscovery")
			discovery := &provisioningv1.DPUDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dpu-discovery-",
					Namespace:    testNS.Name,
				},
				Spec: provisioningv1.DPUDiscoverySpec{
					IPRangeSpec: provisioningv1.IPRangeValidationSpec{
						IPRange: provisioningv1.IPRange{
							StartIP: bmcIP,
							EndIP:   bmcIP,
							Port:    &bmcPort,
						},
					},
					ScanInterval: metav1.Duration{Duration: time.Hour * 1},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, discovery)).To(Or(Succeed(), MatchError(ContainSubstring("not found"))))
			})

			By("waiting for DPU discovery to create the DPU device")
			dpuDeviceList := &provisioningv1.DPUDeviceList{}
			Eventually(func() int {
				Expect(k8sClient.List(ctx, dpuDeviceList, client.InNamespace(testNS.Name))).To(Succeed())
				return len(dpuDeviceList.Items)
			}, timeout, interval).Should(Equal(1))

			dpuDevice := dpuDeviceList.Items[0]
			Expect(dpuDevice.Spec.BMCFactoryResetPolicy).To(Equal(provisioningv1.BMCFactoryResetPolicyNever))

			Expect(k8sClient.Delete(ctx, &dpuDevice)).To(Succeed())
		})
	})
})
