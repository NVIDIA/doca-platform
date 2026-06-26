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
	"net/http"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
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

const (
	testBFBFile     = "/bfb/dpf-operator-system-bfb-bundle.bfb"
	testBFCFGFile   = "/bfb/bfcfg/dpf-operator-system_dpu-config"
	testBFBRegistry = "10.0.110.1:8080"
)

// installingTestEnv bundles the boilerplate needed for the install-flow tests:
// mock Redfish server, BMC + mTLS secrets, DPUDevice with status, DPU CR, and
// the bfb-registry Service. Returned objects are leak-cleaned by AfterEach.
type installingTestEnv struct {
	mockServer *redfishmock.RedfishMockServer
	dpu        *provisioningv1.DPU
	dpuDevice  *provisioningv1.DPUDevice
	ctrlCtx    *dutil.ControllerContext
	prevPodNS  string
}

// setupInstallingEnv prepares the env for a single Installing test. The dpuName
// must be unique per test to avoid envtest namespace collisions across cases.
func setupInstallingEnv(dpuName string) *installingTestEnv {
	mockServer, err := redfishmock.CreateMockRedfishServer("BF-24.10", "password")
	Expect(err).NotTo(HaveOccurred())
	mockServer.SetNicMode("DpuMode")

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

	dpuDevice := dpuDeviceObj(dpuName + "-device")
	dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
	dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
	createObject(dpuDevice)
	patch := client.MergeFrom(dpuDevice.DeepCopy())
	dpuDevice.Status.BMCIP = ptr.To(mockServer.GetIPAddress())
	dpuDevice.Status.BMCPort = ptr.To(uint32(mockServer.GetPort()))
	Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

	dpu := dpuObj(dpuName)
	dpu.Spec.DPUDeviceName = dpuDevice.Name
	dpu.Status.Phase = provisioningv1.DPUOSInstalling
	dpu.Status.BFBFile = testBFBFile
	dpu.Status.BFCFGFile = testBFCFGFile
	taskID := "0"
	dpu.Status.RedfishTaskID = &taskID
	createObject(dpu)
	patch = client.MergeFrom(dpu.DeepCopy())
	dpu.Status.Phase = provisioningv1.DPUOSInstalling
	dpu.Status.RedfishTaskID = &taskID
	Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

	prevNS := os.Getenv("POD_NAMESPACE")
	Expect(os.Setenv("POD_NAMESPACE", testNS.Name)).To(Succeed())
	bfbRegistrySvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: bfbregistry.PodName, Namespace: testNS.Name},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{{Name: "http", Port: int32(bfbregistry.ContainerPort), TargetPort: intstr.FromInt32(bfbregistry.ContainerPort)}},
		},
	}
	Expect(k8sClient.Create(ctx, bfbRegistrySvc)).To(Succeed())

	return &installingTestEnv{
		mockServer: mockServer,
		dpu:        dpu,
		dpuDevice:  dpuDevice,
		ctrlCtx: &dutil.ControllerContext{
			Client:               k8sClient,
			Options:              dutil.DPUOptions{BFBRegistry: testBFBRegistry},
			DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
		},
		prevPodNS: prevNS,
	}
}

func (e *installingTestEnv) teardown() {
	if e.mockServer != nil {
		e.mockServer.Stop()
	}
	_ = os.Setenv("POD_NAMESPACE", e.prevPodNS)
}

// atxLowSELEntry returns a synthetic SEL entry mimicking the documented BMC
// example for a 12V_ATX low-threshold event.
func atxLowSELEntry() rc.SELEntry {
	return rc.SELEntry{
		ID:          "1",
		Created:     "2026-04-29T10:00:00+00:00",
		Severity:    "Critical",
		Message:     "12V_ATX sensor crossed a warning low threshold going low. Reading=6.048000 Threshold=10.400000.",
		MessageID:   "OpenBMC.0.1.SensorThresholdWarningLowGoingLow",
		MessageArgs: []string{"12V_ATX", "6.048000", "10.400000"},
		Resolution:  "Check the sensor or subsystem for errors.",
	}
}

func pcieLowSELEntry() rc.SELEntry {
	return rc.SELEntry{
		ID:          "2",
		Created:     "2026-04-29T10:00:00+00:00",
		Severity:    "Critical",
		Message:     "12V_PCIe sensor crossed a warning low threshold going low.",
		MessageID:   "OpenBMC.0.1.SensorThresholdWarningLowGoingLow",
		MessageArgs: []string{"12V_PCIe", "5.0", "10.0"},
	}
}

var _ = Describe("Installing", func() {
	Context("concatBFBAndBFCFGPath", func() {
		It("should return the correct path", func() {
			registry := testBFBRegistry
			bfbFile := testBFBFile
			bfcfgFile := "/bfb/bfcfg/dpf-operator-system_dpu-node-mt25066004be-mt25066004be_ea091a0e-f0ae-4033-9db3-2ecf9a1dfe61"
			expected := testBFBRegistry + "/bfb/??dpf-operator-system-bfb-bundle.bfb,bfcfg/dpf-operator-system_dpu-node-mt25066004be-mt25066004be_ea091a0e-f0ae-4033-9db3-2ecf9a1dfe61?/bfb-to-install"

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
		dpu.Status.BFCFGFile = testBFCFGFile
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
		dpu.Status.BFCFGFile = testBFCFGFile
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
				BFBRegistry: testBFBRegistry,
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

	Context("BFB installation via Redfish transient failures", func() {
		var (
			mockServer *redfishmock.RedfishMockServer
			dpuDevice  *provisioningv1.DPUDevice
			ctrlCtx    *dutil.ControllerContext
		)

		BeforeEach(func() {
			By("prepare mock Redfish server")
			var err error
			mockServer, err = redfishmock.CreateMockRedfishServer("BF-24.10", "password")
			Expect(err).NotTo(HaveOccurred())
			mockServer.SetNicMode("DpuMode")
			DeferCleanup(mockServer.Stop)

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
			caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServer.GetIPAddress())
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
			dpuDevice = dpuDeviceObj("dpu-device-transient-test")
			dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			createObject(dpuDevice)
			patch := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Status.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

			ctrlCtx = &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					BFBRegistry: "10.0.110.1",
				},
				DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
			}
		})

		It("should stay in OSInstalling when Redfish returns 400 'Another update is in progress' and succeed after the concurrent update completes", func() {
			mockServer.SetConcurrentUpdateBusy(2)

			By("prepare DPU CR in Installing phase")
			dpu := dpuObj("dpu-concurrent-update-test")
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.BFBFile = testBFBFile
			dpu.Status.BFCFGFile = testBFCFGFile
			createObject(dpu)
			patch := client.MergeFrom(dpu.DeepCopy())
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

			By("Step 1: First call — mock returns HTTP 400 'Another update is in progress'")
			status, err := Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling),
				"DPU should remain in OSInstalling, not transition to DPUError")
			Expect(status.RedfishTaskID).To(BeNil(),
				"No task should be created when a concurrent update blocks the request")
			Expect(mockServer.GetConcurrentUpdateBusyServed()).To(Equal(1),
				"Mock should have served exactly one HTTP 400 busy response")

			By("Step 2: Verify no BFBTransferred error condition was set")
			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			if cond != nil {
				Expect(cond.Reason).NotTo(Equal("FailToInstall"),
					"BFBTransferred condition should not indicate a terminal failure")
			}

			By("Step 3: Second call — mock still returns busy (remaining=1)")
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.RedfishTaskID).To(BeNil())
			Expect(mockServer.GetConcurrentUpdateBusyServed()).To(Equal(2),
				"Mock should have served two HTTP 400 busy responses total")

			By("Step 4: Third call — concurrent update finished, install proceeds normally")
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.RedfishTaskID).NotTo(BeNil(),
				"A Redfish task should be created once the concurrent update clears")
			Expect(*status.RedfishTaskID).To(Equal("0"))
			Expect(mockServer.GetConcurrentUpdateBusyServed()).To(Equal(2),
				"No additional busy responses should have been served on the successful call")

			By("Step 5: Fourth call — check task progress completes")
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status = status
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

			dpu.Status = status
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			_, cond = cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue),
				"BFB transfer should complete successfully after the concurrent update clears")
		})

		It("should stay in OSInstalling when CheckTaskProgress returns error and recover when BMC is reachable again", func() {
			By("prepare DPU CR in Installing phase with an active task (BFB transfer in progress)")
			dpu := dpuObj("dpu-taskprogress-error-test")
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.BFBFile = testBFBFile
			dpu.Status.BFCFGFile = testBFCFGFile
			taskID := "0"
			dpu.Status.RedfishTaskID = &taskID
			createObject(dpu)
			patch := client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.RedfishTaskID = &taskID
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

			By("Step 1: Simulate BMC task progress endpoint failure")
			mockServer.SetTaskProgressError(true)
			status, err := Installing(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred(), "should return error to trigger controller-runtime requeue")
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling),
				"DPU should stay in OSInstalling, not transition to DPUError")
			Expect(status.RedfishTaskID).NotTo(BeNil(),
				"Task ID should be preserved so progress can be checked on next reconcile")
			_, failCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(failCond).NotTo(BeNil())
			Expect(failCond.Reason).To(Equal("FailToCheckProgress"))

			By("Step 2: Restore BMC task progress endpoint")
			mockServer.SetTaskProgressError(false)

			By("Step 3: Call Installing again — CheckTaskProgress succeeds, BFB transfer completes")
			dpu.Status = status
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			_, transferCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(transferCond).NotTo(BeNil())
			Expect(transferCond.Status).To(Equal(metav1.ConditionTrue),
				"BFB transfer should complete after BMC recovers")
		})
	})

	Context("error-message enrichment", func() {
		It("HTTP 404 from CheckTaskProgress + SEL has 12V_ATX low: classified hint, taskID, rail hint, no DPUError", func() {
			env := setupInstallingEnv("dpu-404-test")
			defer env.teardown()
			env.mockServer.SetTaskHTTPResponse(http.StatusNotFound, `{"error":"task gone"}`)
			env.mockServer.SetSELEntries([]rc.SELEntry{atxLowSELEntry()})

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			Expect(err).To(HaveOccurred()) // non-200 returns err so the controller retries
			Expect(status.Phase).NotTo(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailToCheckProgress"))
			// Backward-compat: the historical "is not OK" prefix must still be present.
			Expect(cond.Message).To(ContainSubstring("is not OK"))
			Expect(cond.Message).To(ContainSubstring("404"))
			Expect(cond.Message).To(ContainSubstring("taskID=0"))
			Expect(cond.Message).To(ContainSubstring("Redfish task no longer exists on the BMC"))
			// Rail hint comes from the best-effort SEL probe (404 triggers ShouldProbeRails).
			Expect(cond.Message).To(ContainSubstring("12V_ATX low"))
			Expect(cond.Message).To(ContainSubstring("check power cable"))
		})

		It("HTTP 404 from InstallBFB submit surfaces the BMC message in the condition (rshim not owned by BMC)", func() {
			env := setupInstallingEnv("dpu-installbfb-404-test")
			defer env.teardown()

			// Submit path: no Redfish task created yet.
			env.dpu.Status.RedfishTaskID = nil
			// Use the load-balancer override so getBFBRegistryAddress returns
			// directly, keeping this test focused on the submit failure path.
			env.ctrlCtx.Options.BFBRegistryLoadBalancer = testBFBRegistry

			// Real BMC error envelope seen when rshim is not owned by the BMC.
			body := `{
  "error": {
    "@Message.ExtendedInfo": [
      {
        "@odata.type": "#Message.v1_1_1.Message",
        "Message": "The requested resource of type Targets named '/dev/rshim0/boot' was not found.",
        "MessageArgs": ["Targets", "/dev/rshim0/boot"],
        "MessageId": "Base.1.18.1.ResourceNotFound",
        "MessageSeverity": "Critical",
        "Resolution": "Provide a valid resource identifier and resubmit the request."
      }
    ],
    "code": "Base.1.18.1.ResourceNotFound",
    "message": "The requested resource of type Targets named '/dev/rshim0/boot' was not found."
  }
}`
			env.mockServer.SetInstallBFBResponse(http.StatusNotFound, body)

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			// Submit failure transitions to Error and returns nil so the next
			// reconcile is driven by the UPDATE event, not a retry.
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailToInstall"))
			// Backward-compat: the historical "get status: <status>" prefix is kept.
			Expect(cond.Message).To(ContainSubstring("get status: 404"))
			// The Redfish error envelope is translated: the BMC's own message,
			// MessageId, and Resolution now reach the condition (previously only
			// in the log), so the actual cause is visible to the operator.
			Expect(cond.Message).To(ContainSubstring("The requested resource of type Targets named '/dev/rshim0/boot' was not found."))
			Expect(cond.Message).To(ContainSubstring("(Base.1.18.1.ResourceNotFound)"))
			Expect(cond.Message).To(ContainSubstring("BMC Resolution: Provide a valid resource identifier and resubmit the request."))
			// The translated form replaces the raw body dump.
			Expect(cond.Message).NotTo(ContainSubstring("body:"))
		})

		It("valid-JSON non-Redfish body from InstallBFB submit falls back to the raw (truncated) body", func() {
			env := setupInstallingEnv("dpu-installbfb-rawbody-test")
			defer env.teardown()

			env.dpu.Status.RedfishTaskID = nil
			env.ctrlCtx.Options.BFBRegistryLoadBalancer = testBFBRegistry

			// Valid JSON, but not a Redfish error envelope. It decodes cleanly in
			// do[TaskInfo] (so we reach buildInstallBFBError), yet rc.ErrorMessages
			// finds no error.message/@Message.ExtendedInfo, so we fall back to the body.
			env.mockServer.SetInstallBFBResponse(http.StatusInternalServerError, `{"unexpected":"payload"}`)

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailToInstall"))
			Expect(cond.Message).To(ContainSubstring("get status: 500"))
			Expect(cond.Message).To(ContainSubstring(`body: {"unexpected":"payload"}`))
		})

		It("HTTP 500 + SEL has 12V_PCIe low: rail hint is appended; phase unchanged", func() {
			env := setupInstallingEnv("dpu-500-sel-test")
			defer env.teardown()
			env.mockServer.SetTaskHTTPResponse(http.StatusInternalServerError, `{"error":"boom"}`)
			env.mockServer.SetSELEntries([]rc.SELEntry{pcieLowSELEntry()})

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).NotTo(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailToCheckProgress"))
			Expect(cond.Message).To(ContainSubstring("is not OK"))
			Expect(cond.Message).To(ContainSubstring("500"))
			Expect(cond.Message).To(ContainSubstring("12V_PCIe low"))
		})

		It("Exception state with documented MessageId (Update.1.0.TransferFailed): hint + Resolution surfaced", func() {
			env := setupInstallingEnv("dpu-exception-translated-test")
			defer env.teardown()
			env.mockServer.SetTaskState("Exception")
			env.mockServer.SetTaskMessages([]map[string]interface{}{
				{
					"Message":    "Transfer of image failed.",
					"MessageId":  "Update.1.0.TransferFailed",
					"Resolution": "Check the network and retry.",
					"Severity":   "Critical",
				},
			})

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailToInstall"))
			Expect(cond.Message).To(ContainSubstring("is in Exception state")) // backward-compat prefix
			Expect(cond.Message).To(ContainSubstring("BMC failed to fetch the BFB image"))
			Expect(cond.Message).To(ContainSubstring("MessageId=Update.1.0.TransferFailed"))
			Expect(cond.Message).To(ContainSubstring("BMC Resolution: Check the network and retry."))
		})

		It("Exception state with unknown MessageId + SEL has 12V_PCIe low: raw body kept, rail hint appended", func() {
			env := setupInstallingEnv("dpu-exception-unknown-sel-test")
			defer env.teardown()
			env.mockServer.SetTaskState("Exception")
			env.mockServer.SetTaskMessages([]map[string]interface{}{
				{
					"Message":   "Some unknown problem",
					"MessageId": "Vendor.1.0.SomethingNew",
					"Severity":  "Critical",
				},
			})
			env.mockServer.SetSELEntries([]rc.SELEntry{pcieLowSELEntry()})

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondBFBTransferred))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("FailToInstall"))
			Expect(cond.Message).To(ContainSubstring("is in Exception state"))
			// Unknown MessageId path keeps the raw messages dump (no regression).
			Expect(cond.Message).To(ContainSubstring("Some unknown problem"))
			Expect(cond.Message).To(ContainSubstring("12V_PCIe low"))
		})

		It("OSInstallTimeout + SEL has 12V_ATX low: timeout message is enriched", func() {
			env := setupInstallingEnv("dpu-timeout-sel-test")
			defer env.teardown()
			env.mockServer.SetSELEntries([]rc.SELEntry{atxLowSELEntry()})

			// Force timeout: backdate the BFBPrepared condition past the configured limit.
			patch := client.MergeFrom(env.dpu.DeepCopy())
			env.dpu.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.DPUCondBFBPrepared),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: time.Now().Add(-50 * time.Minute)},
					Reason:             "Prepared",
				},
			}
			Expect(k8sClient.Status().Patch(ctx, env.dpu, patch)).To(Succeed())
			env.ctrlCtx.Options.OSInstallTimeout = 45 * time.Minute

			status, err := Installing(ctx, env.dpu, env.ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))

			_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal("InstallationTimeout"))
			Expect(cond.Message).To(ContainSubstring("OS installation timeout exceeded"))
			Expect(cond.Message).To(ContainSubstring("12V_ATX low"))
		})
	})

	Context("BlueField 4 OS installation via installOsBf4", func() {
		const (
			bf4DPUName       = "dpu-bf4-installing-test"
			bf4DPUDeviceName = "dpu-device-bf4-installing-test"
			bf4SoftwareName  = "bf4-software-installing-test"
			testOsIso        = "bfb/os/dpf-operator-system.iso"
		)

		createBF4InstallingMockServer := func() *redfishmock.RedfishMockServer {
			server := redfishmock.NewRedfishMockServer("BF-26.04", "password")
			server.SetDpuVersion(redfishmock.BF4)
			server.SetNicMode("DpuMode")
			server.Start()
			return server
		}

		createBF4InstallingSecrets := func(mockServerIP string) {
			bmcSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "bmc-shared-password", Namespace: testNS.Name},
				Data:       map[string][]byte{"password": []byte("password")},
			}
			Expect(k8sClient.Create(ctx, bmcSecret)).To(Succeed())

			caCrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts(mockServerIP)
			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "dpf-provisioning-ca-secret", Namespace: testNS.Name},
				Data:       map[string][]byte{"tls.crt": caCrt},
			}
			Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())
			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "dpf-provisioning-redfish-client-secret-bf4", Namespace: testNS.Name},
				Data:       map[string][]byte{"tls.crt": clientCrt, "tls.key": clientKey},
			}
			Expect(k8sClient.Create(ctx, clientSecret)).To(Succeed())
		}

		createReadyBlueFieldSoftware := func() {
			bfs := &provisioningv1.BlueFieldSoftware{
				ObjectMeta: metav1.ObjectMeta{Name: bf4SoftwareName, Namespace: testNS.Name},
				Spec: provisioningv1.BlueFieldSpec{
					OsIso: "https://test.com/" + testOsIso,
				},
			}
			createObject(bfs)
			patch := client.MergeFrom(bfs.DeepCopy())
			bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
			bfs.Status.DownloadedComponents = provisioningv1.DownloadedComponents{
				OsIso: testOsIso,
			}
			Expect(k8sClient.Status().Patch(ctx, bfs, patch)).To(Succeed())
		}

		It("should transfer ISO and config, insert virtual media, and set VirtualMediaInserted", func() {
			mockServer := createBF4InstallingMockServer()
			defer mockServer.Stop()

			createBF4InstallingSecrets(mockServer.GetIPAddress())
			createReadyBlueFieldSoftware()

			dpuDevice := dpuDeviceObj(bf4DPUDeviceName)
			dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
			dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
			createObject(dpuDevice)
			patch := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
			dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
			dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField4
			Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())

			dpu := dpuObj(bf4DPUName)
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.BFB = nil
			dpu.Spec.BlueFieldSoftware = ptr.To(bf4SoftwareName)
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
			dpu.Status.BFCFGFile = testBFCFGFile
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField4
			dpu.Status.BFCFGFile = testBFCFGFile
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

			prevNS := os.Getenv("POD_NAMESPACE")
			Expect(os.Setenv("POD_NAMESPACE", testNS.Name)).To(Succeed())
			defer func() { _ = os.Setenv("POD_NAMESPACE", prevNS) }()
			bfbRegistrySvc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: bfbregistry.PodName, Namespace: testNS.Name},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{{Name: "http", Port: int32(bfbregistry.ContainerPort), TargetPort: intstr.FromInt32(bfbregistry.ContainerPort)}},
				},
			}
			Expect(k8sClient.Create(ctx, bfbRegistrySvc)).To(Succeed())

			ctrlCtx := &dutil.ControllerContext{
				Client:               k8sClient,
				Options:              dutil.DPUOptions{BFBRegistry: "10.0.110.1"},
				DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
			}

			By("Step 1: submit ISO install task")
			status, err := Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.RedfishTaskID).NotTo(BeNil())
			Expect(*status.RedfishTaskID).To(Equal("0"))
			_, isoCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondIsoTransferred))
			if isoCond != nil {
				Expect(isoCond.Status).NotTo(Equal(metav1.ConditionTrue))
			}

			By("Step 2: complete ISO transfer and submit config install task")
			dpu.Status = status
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			_, isoCond = cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondIsoTransferred))
			Expect(isoCond).NotTo(BeNil())
			Expect(isoCond.Status).To(Equal(metav1.ConditionTrue))
			_, cfgCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondConfigTransferred))
			if cfgCond != nil {
				Expect(cfgCond.Status).NotTo(Equal(metav1.ConditionTrue))
			}

			By("Step 3: complete config transfer, insert virtual media, and set VirtualMediaInserted")
			dpu.Status = status
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.RedfishTaskID).To(BeNil())

			_, cfgCond = cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondConfigTransferred))
			Expect(cfgCond).NotTo(BeNil())
			Expect(cfgCond.Status).To(Equal(metav1.ConditionTrue))

			_, vmCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondVirtualMediaInserted))
			Expect(vmCond).NotTo(BeNil())
			Expect(vmCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(vmCond.Message).To(ContainSubstring("Virtual media inserted"))

			By("Step 4: next reconcile waits for OSRunning instead of re-entering installOsBf4")
			mockServer.SetBootLastState("DdrTraining")
			dpu.Status = status
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			_, osCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
			Expect(osCond).NotTo(BeNil())
			Expect(osCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(osCond.Reason).To(Equal("OSNotRunning"))
			Expect(osCond.Message).To(ContainSubstring("Waiting for DPU OS to finish booting"))
			Expect(osCond.Message).To(ContainSubstring(`"DdrTraining"`))
		})
	})

	Context("OSInstalled condition semantics", func() {
		var (
			mockServer *redfishmock.RedfishMockServer
			dpuDevice  *provisioningv1.DPUDevice
			ctrlCtx    *dutil.ControllerContext
		)

		// setupBMCAndCerts creates the BMC password, CA, and client mTLS secrets
		// the Redfish client needs to talk to the mock server.
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
			DeferCleanup(mockServer.Stop)

			By("create BMC and mTLS secrets")
			setupBMCAndCerts()

			By("prepare DPUDevice CR pointing at the mock BMC")
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

		// dpuWithBFBTransferred returns a DPU pre-seeded with BFBTransferred=True so the
		// Installing reconcile skips submitAndMonitorBfbInstallTask and exercises the
		// post-install OSInstalled / OemLastState branch directly.
		dpuWithBFBTransferred := func(name string) *provisioningv1.DPU {
			dpu := dpuObj(name)
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			dpu.Status.DPUType = provisioningv1.DPUTypeBlueField3
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondBFBTransferred, "", ""))
			return dpu
		}

		It("should report OSInstalled=False while booting and flip to True (resetting LastTransitionTime) when the DPU agent starts", func() {
			By("Step 1: OS still booting -> OSInstalled=False with descriptive message")
			mockServer.SetOemLastState("DdrTraining")
			dpu := dpuWithBFBTransferred("dpu-osinstalled-flip-test")

			status, err := Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			_, falseCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
			Expect(falseCond).NotTo(BeNil())
			Expect(falseCond.Status).To(Equal(metav1.ConditionFalse), "OSInstalled must be False while the DPU OS has not finished booting")
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

			By("Step 3: DPU agent reports startup -> OSInstalled flips to True with fresh LastTransitionTime")
			mockServer.SetOemLastState("OsIsRunning")
			now := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{LastStartupTime: &now}
			status, err = Installing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig), "Installing should hand off to DPUConfig once the DPU agent has started")

			_, trueCond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
			Expect(trueCond).NotTo(BeNil())
			Expect(trueCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(trueCond.Reason).To(Equal("OsInstalled"))
			Expect(trueCond.LastTransitionTime.After(t1)).To(BeTrue(), "LastTransitionTime must advance on the False->True transition to anchor the agent-startup timer")
		})

	})
})
