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

package redfish

import (
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	redfishmock "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const testBFBFile = "/bfb/dpf-operator-system-bfb-bundle.bfb"

var _ = Describe("Installing", func() {
	Context("concatBFBAndBFCFGPath", func() {
		It("should return the correct path", func() {
			registry := "10.0.110.1:8080"
			bfbFile := testBFBFile
			bfcfgFile := "/bfb/bfcfg/dpf-operator-system_dpu-node-mt25066004be-mt25066004be_ea091a0e-f0ae-4033-9db3-2ecf9a1dfe61"
			expected := "10.0.110.1:8080/bfb/??dpf-operator-system-bfb-bundle.bfb,bfcfg/dpf-operator-system_dpu-node-mt25066004be-mt25066004be_ea091a0e-f0ae-4033-9db3-2ecf9a1dfe61?/bfb-to-install"

			Expect(concatBFBAndBFCFGPath(registry, bfbFile, bfcfgFile)).To(Equal(expected))
			Expect(concatBFBAndBFCFGPath("http://"+registry, bfbFile, bfcfgFile)).To(Equal(expected))
			Expect(concatBFBAndBFCFGPath("https://"+registry, bfbFile, bfcfgFile)).To(Equal(expected))
		})
	})

	Context("checkInstallationTimeout", func() {
		var (
			logger = zap.New(zap.UseDevMode(true))
		)

		It("should return nil when timeout is zero", func() {
			state := &provisioningv1.DPUStatus{}
			err := checkInstallationTimeout(state, 0, logger)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil when timeout is negative", func() {
			state := &provisioningv1.DPUStatus{}
			err := checkInstallationTimeout(state, -1*time.Minute, logger)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil when BFBPrepared condition is not set", func() {
			state := &provisioningv1.DPUStatus{}
			err := checkInstallationTimeout(state, 45*time.Minute, logger)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil when timeout has not been exceeded", func() {
			state := &provisioningv1.DPUStatus{}
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBPrepared), nil, "Prepared", ""))
			err := checkInstallationTimeout(state, 45*time.Minute, logger)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when timeout has been exceeded", func() {
			state := &provisioningv1.DPUStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUCondBFBPrepared),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-50 * time.Minute)},
						Reason:             "Prepared",
					},
				},
			}
			err := checkInstallationTimeout(state, 45*time.Minute, logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("OS installation timeout exceeded"))
		})
	})

	It("should submit and monitor BFB installation task", func() {
		By("prepare mock Redfish server")
		mockServer, err := redfishmock.CreateMockRedfishServer("BF-24.10", "password")
		mockServer.SetNicMode("DpuMode")
		Expect(err).NotTo(HaveOccurred())
		defer mockServer.Stop()

		By("create BMC credentials secret")
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bmc-shared-password",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"password": []byte("password"),
			},
		}
		Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

		By("create CA and client certificate secrets for mTLS")
		// Generate mTLS certificates for testing
		caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())

		// Create CA certificate secret
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-ca-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": caCrt,
			},
		}
		Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())

		// Create client certificate secret
		clientSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-redfish-client-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": clientCrt,
				"tls.key": clientKey,
			},
		}
		Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())

		By("prepare DPUDevice CR with Redfish BMC")
		dpuDevice := dpuDeviceObj("dpu-device-installing-test")
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Status.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

		By("prepare DPU CR in Installing phase")
		dpu := dpuObj("dpu-installing-test")
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		dpu.Status.BFBFile = testBFBFile
		dpu.Status.BFCFGFile = "/bfb/bfcfg/dpf-operator-system_dpu-config"
		createObject(dpu)
		// Update the status separately
		patch = client.MergeFrom(dpu.DeepCopy())
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

		By("set POD_NAMESPACE and create bfb-registry Service for getBFBRegistryAddress")
		prevNS := os.Getenv("POD_NAMESPACE")
		Expect(os.Setenv("POD_NAMESPACE", testNS.Name)).To(Succeed())
		defer func() { _ = os.Setenv("POD_NAMESPACE", prevNS) }()
		bfbRegistrySvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bfbregistry.PodName,
				Namespace: testNS.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: int32(bfbregistry.ContainerPort), TargetPort: intstr.FromInt32(bfbregistry.ContainerPort)},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bfbRegistrySvc)).To(Succeed())

		By("Step 1: Call Installing to submit BFB install task")
		ctrlCtx := &dutil.ControllerContext{
			Client: k8sClient,
			Options: dutil.DPUOptions{
				BFBRegistry: "10.0.110.1",
			},
			DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
		}

		status, err := Installing(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

		// Update DPU status with the returned status
		patch = client.MergeFrom(dpu.DeepCopy())
		dpu.Status = status
		Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

		By("Step 2: Verify taskId is stored in DPU Status")
		Eventually(func() *string {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)
			if err != nil {
				return nil
			}
			return dpu.Status.RedfishTaskID
		}, "2s", "100ms").Should(Not(BeNil()), "taskId should be stored in DPU Status")
		Expect(*dpu.Status.RedfishTaskID).To(Equal("0"))

		By("Step 3: Call Installing again to check progress")
		dpu.Status = status
		status, err = Installing(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

		// Update DPU status with the returned status after task completion
		patch = client.MergeFrom(dpu.DeepCopy())
		dpu.Status = status
		Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

		By("Step 4: Verify BFB transfer condition is set to true")
		_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))

		By("Step 5: Verify taskId is cleared from DPU Status after task completion")
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)
		Expect(err).NotTo(HaveOccurred())
		Expect(dpu.Status.RedfishTaskID).To(BeNil(), "taskId should be cleared from DPU after task completion")
	})

	It("should handle Exception task state during BFB installation", func() {
		By("prepare mock Redfish server")
		mockServer, err := redfishmock.CreateMockRedfishServer("BF-24.10", "password")
		mockServer.SetNicMode("DpuMode")
		Expect(err).NotTo(HaveOccurred())
		defer mockServer.Stop()

		By("create BMC credentials secret")
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bmc-shared-password",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"password": []byte("password"),
			},
		}
		Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

		By("create CA and client certificate secrets for mTLS")
		// Generate mTLS certificates for testing
		caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())

		// Create CA certificate secret
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-ca-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": caCrt,
			},
		}
		Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())

		// Create client certificate secret
		clientSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpf-provisioning-redfish-client-secret",
				Namespace: testNS.Name,
			},
			Data: map[string][]byte{
				"tls.crt": clientCrt,
				"tls.key": clientKey,
			},
		}
		Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())

		By("prepare DPUDevice CR with Redfish BMC")
		dpuDevice := dpuDeviceObj("dpu-device-exception-test")
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Status.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

		By("prepare DPU CR in Installing phase with task ID")
		dpu := dpuObj("dpu-exception-test")
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		dpu.Status.BFBFile = testBFBFile
		dpu.Status.BFCFGFile = "/bfb/bfcfg/dpf-operator-system_dpu-config"
		taskID := "0"
		dpu.Status.RedfishTaskID = &taskID
		createObject(dpu)
		// Update the status separately
		patch = client.MergeFrom(dpu.DeepCopy())
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		dpu.Status.RedfishTaskID = &taskID
		Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

		By("Configure mock server to return Exception task state")
		mockServer.SetTaskState("Exception")
		mockServer.SetTaskMessages([]map[string]interface{}{
			{
				"Message":   "Installation failed due to network error",
				"MessageId": "Base.1.0.GeneralError",
				"Severity":  "Critical",
			},
		})

		By("Call Installing to check progress and handle Exception")
		ctrlCtx := &dutil.ControllerContext{
			Client: k8sClient,
			Options: dutil.DPUOptions{
				BFBRegistry: "10.0.110.1:8080",
			},
			DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
		}

		status, err := Installing(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())

		By("Verify DPU phase transitions to Error")
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))

		By("Verify BFBTransferred condition is set with error details")
		_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("FailToInstall"))
		Expect(cond.Message).To(ContainSubstring("Exception state"))
		Expect(cond.Message).To(ContainSubstring("Installation failed due to network error"))
	})

	Context("OSInstalled condition semantics", func() {
		var (
			mockServer *redfishmock.RedfishMockServer
			dpuDevice  *provisioningv1.DPUDevice
			ctrlCtx    *dutil.ControllerContext
		)

		setupBMCAndCerts := func() {
			bmcSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "bmc-shared-password", Namespace: testNS.Name},
				Data:       map[string][]byte{"password": []byte("password")},
			}
			Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

			caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())
			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "dpf-provisioning-ca-secret", Namespace: testNS.Name},
				Data:       map[string][]byte{"tls.crt": caCrt},
			}
			Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())
			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "dpf-provisioning-redfish-client-secret", Namespace: testNS.Name},
				Data:       map[string][]byte{"tls.crt": clientCrt, "tls.key": clientKey},
			}
			Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())
		}

		BeforeEach(func() {
			By("prepare mock Redfish server")
			var err error
			mockServer, err = redfishmock.CreateMockRedfishServer("BF-24.10", "password")
			Expect(err).NotTo(HaveOccurred())
			mockServer.SetNicMode("DpuMode")

			setupBMCAndCerts()

			By("prepare DPUDevice CR with Redfish BMC")
			dpuDevice = dpuDeviceObj("dpu-device-osinstalled-test")
			dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			createObject(dpuDevice)
			patch := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Status.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

			ctrlCtx = &dutil.ControllerContext{
				Client:               k8sClient,
				Options:              dutil.DPUOptions{BFBRegistry: "10.0.110.1"},
				DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
			}
		})

		AfterEach(func() {
			if mockServer != nil {
				mockServer.Stop()
			}
		})

		dpuWithBFBTransferred := func(name string) *provisioningv1.DPU {
			dpu := dpuObj(name)
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondBFBTransferred, "", ""))
			return dpu
		}

		It("should report OSInstalled=False while booting and flip to True when the DPU agent starts", func() {
			By("Step 1: OS still booting -> OSInstalled=False with descriptive message")
			mockServer.SetOemLastState("DdrTraining")
			dpu := dpuWithBFBTransferred("dpu-osinstalled-flip-test")

			status, err := Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			_, falseCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
			Expect(falseCond).NotTo(BeNil())
			Expect(falseCond.Status).To(Equal(metav1.ConditionFalse), "OSInstalled must be False while the DPU agent has not started")
			Expect(falseCond.Reason).To(Equal("OSNotRunning"))
			Expect(falseCond.Message).To(ContainSubstring("Waiting for DPU OS to finish booting"))
			Expect(falseCond.Message).To(ContainSubstring(`"DdrTraining"`))

			By("Step 2: backdate the False transition so we can verify the True transition resets it forward")
			for i := range status.Conditions {
				if status.Conditions[i].Type == string(provisioningv1.DPUCondOSInstalled) {
					status.Conditions[i].LastTransitionTime = metav1.Time{Time: time.Now().Add(-30 * time.Minute)}
				}
			}
			t1 := time.Now().Add(-30 * time.Minute)
			dpu.Status = status

			By("Step 3: DPU agent reports startup -> OSInstalled flips to True even if OemLastState is not OsIsRunning")
			mockServer.SetOemLastState("DdrTraining")
			now := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{LastStartupTime: &now}
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig), "Installing should hand off to DPUConfig once the DPU agent has started")

			_, trueCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
			Expect(trueCond).NotTo(BeNil())
			Expect(trueCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(trueCond.Reason).To(Equal("OsInstalled"))
			Expect(trueCond.LastTransitionTime.After(t1)).To(BeTrue(), "LastTransitionTime must advance on the False->True transition")
		})
	})
})
