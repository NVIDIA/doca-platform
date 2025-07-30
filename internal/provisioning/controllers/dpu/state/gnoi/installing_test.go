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
	"os"
	"path/filepath"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/gnoi"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gnoios "github.com/openconfig/gnoi/os"
	"github.com/openconfig/gnoi/system"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Phase Installing", func() {
	var (
		defaultDPUName        = "dpu-installing-test"
		defaultDPUDeviceName  = "dpu-device-installing-test"
		defaultFlavorName     = "flavor-installing-test"
		defaultBFBName        = "bfb-installing-test"
		defaultDPUClusterName = "dpu-cluster-installing-test"
	)

	var prepareDPUDevice = func() *provisioningv1.DPUDevice {
		dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
		createObject(dpuDevice)
		dpuNode := &provisioningv1.DPUNode{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      defaultDPUNodeName,
			Namespace: testNS.Name,
		}, dpuNode)).To(Succeed())
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Spec.DPUs = []provisioningv1.DPURef{
			{
				Name: dpuDevice.Name,
			},
		}
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())
		return dpuDevice
	}

	var prepareBFB = func() *provisioningv1.BFB {
		bfb := bfbObj(defaultBFBName)
		createObject(bfb)
		patch := client.MergeFrom(bfb.DeepCopy())
		mockBFBFile := filepath.Join(os.TempDir(), "bfb-file")
		Expect(os.WriteFile(mockBFBFile, []byte("mock bfb file"), 0644)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Remove(mockBFBFile)
		})
		bfb.Status = provisioningv1.BFBStatus{
			Phase:    provisioningv1.BFBReady,
			FileName: mockBFBFile,
		}
		Expect(k8sClient.Status().Patch(ctx, bfb, patch)).To(Succeed())
		return bfb
	}

	var prepareDPUFlavor = func() *provisioningv1.DPUFlavor {
		flavor := flavorObj(defaultFlavorName)
		createObject(flavor)
		return flavor
	}

	var prepareDPUCluster = func() *provisioningv1.DPUCluster {
		dpuCluster := dpuClusterObj(defaultDPUClusterName)
		createObject(dpuCluster)
		patch := client.MergeFrom(dpuCluster.DeepCopy())
		dpuCluster.Spec.Kubeconfig = "kubeconfig"
		Expect(k8sClient.Patch(ctx, dpuCluster, patch)).To(Succeed())
		dpuCluster.Status.Phase = provisioningv1.PhaseReady
		Expect(k8sClient.Status().Patch(ctx, dpuCluster, patch)).To(Succeed())
		return dpuCluster
	}

	var prepareDPFOperatorConfig = func() *operatorv1.DPFOperatorConfig {
		dpfOperatorConfig := dpfOperatorConfigObj(defaultDPFOperatorConfig)
		createObject(dpfOperatorConfig)
		return dpfOperatorConfig
	}

	var prepareDPU = func(dpuDevice *provisioningv1.DPUDevice, bfb *provisioningv1.BFB, flavor *provisioningv1.DPUFlavor, dpuCluster *provisioningv1.DPUCluster) *provisioningv1.DPU {
		dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
		dpu.Spec.BFB = bfb.Name
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      dpuCluster.Name,
			Namespace: dpuCluster.Namespace,
		}
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		// the DPU CR must be created before running Installing as Installing patches the DPU CR at the end of its execution
		createObject(dpu)
		patch := client.MergeFrom(dpu.DeepCopy())
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		dpu.Status.BFBFile = bfb.Status.FileName
		dpu.Status.DPFVersion = ptr.To(release.DPFVersion())
		Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())
		return dpu
	}

	var installSucc = func(req gnoios.OS_InstallServer) error {
		return req.Send(&gnoios.InstallResponse{Response: &gnoios.InstallResponse_Validated{
			Validated: &gnoios.Validated{
				Version: "one",
			},
		}})
	}

	var activateSucc = func(_ context.Context, _ *gnoios.ActivateRequest) (*gnoios.ActivateResponse, error) { //nolint:unparam
		return &gnoios.ActivateResponse{
			Response: &gnoios.ActivateResponse_ActivateOk{
				ActivateOk: &gnoios.ActivateOK{},
			},
		}, nil
	}

	var rebootStatusSucc = func(_ context.Context, _ *system.RebootStatusRequest) (*system.RebootStatusResponse, error) { //nolint:unparam
		return &system.RebootStatusResponse{Active: false, Status: &system.RebootStatus{Status: system.RebootStatus_STATUS_SUCCESS}}, nil
	}

	Context("successful cases", func() {
		It("should install the DPU", func() {
			// prepare dpuNode and dpuDevice
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)
			dpuMap := dutil.NewDPUInProvisioningMap(1)

			// set the sleep time for installing BFB (the default sleep time is 90s)
			Expect(os.Setenv("CLOUD_INIT_TIMEOUT", "1")).To(Succeed())
			DeferCleanup(func() {
				Expect(os.Unsetenv("CLOUD_INIT_TIMEOUT")).To(Succeed())
			})

			// first run, an async installing task should be created
			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
				DPUInProvisioningMap: dpuMap,
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("verifying the DPU is in the provisioning map")
			Expect(dpuMap.Initialize(ctx, k8sClient)).To(Succeed())
			Expect(dpuMap.CanProceed(dutil.DPUID("test-dpu"))).To(BeFalse())

			// follow up runs, dpu should transition to the next phase after the sleep time
			Eventually(func() provisioningv1.DPUPhase {
				status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
					Client:               k8sClient,
					JoinCommandGenerator: &mockJoinCommandGenerator{},
					DPUInProvisioningMap: dpuMap,
				})
				Expect(err).To(Succeed())
				return status.Phase
			}, 100*time.Second, 1*time.Second).Should(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondOSInstalled.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondOSInstalled.String()),
				),
			))

			By("verifying the DPU is not in the provisioning map")
			Expect(dpuMap.CanProceed(dutil.DPUID("test-dpu"))).To(BeTrue())

			dpuFetched := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpuFetched)).To(Succeed())
			Expect(dpuFetched.Spec.Cluster.NodeLabels).To(
				And(
					HaveKeyWithValue("provisioning.dpu.nvidia.com/DOCA-BFB-version", "bfb-file"),
					HaveKeyWithValue(release.DPFVersionLabelKey, release.DPFVersion()),
				))
			Expect(dpuFetched.Spec.Cluster.NodeLabels).To(HaveKeyWithValue(
				gnoi.HostNameDPULabelKey, defaultDPUNodeName,
			))
		})
	})

	Context("error handling", func() {
		It("should retry if BFB CR not found", func() {
			By("a DPU CR without corresponding BFB CR")
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, defaultFlavorName)
			dpu.Spec.BFB = defaultBFBName
			dpu.Status.Phase = provisioningv1.DPUOSInstalling
			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.Conditions).Should(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondOSInstalled.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "BFBNotFound"),
				),
			))
		})
		It("should retry if BFB file is not found", func() {
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)

			By("delete the BFB file to trigger the error")
			Expect(os.Remove(bfb.Status.FileName)).To(Succeed())

			By("first run, an async task should be added immediately")
			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("waiting for the async task to be finished")
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("failed to open file")))

			By("running again, should not transition to Error phase")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		})

		It("should retry until the retry count exceeds the limit if dmsHandler fails", func() {
			By("simulating a situation that DMS connection can not be initialized")
			bfb := prepareBFB()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, defaultFlavorName)
			dpu.Spec.BFB = bfb.Name
			dpu.Status.Phase = provisioningv1.DPUOSInstalling

			By("first run, an async task should be added immediately")
			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))

			By("waiting for the async task to be finished, and check the error message")
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("error creating gRPC connection")))

			By("running again (should not exceed the retry count limit)")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			task, ok = dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask = task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(1))

			By("checking the error message again, should have the same error message as the first run")
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("error creating gRPC connection")))

			By("keep running it over and over until the retry count exceeds the limit")
			Eventually(func() provisioningv1.DPUPhase {
				status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
					Client:               k8sClient,
					JoinCommandGenerator: &mockJoinCommandGenerator{},
				})
				return status.Phase
			}, 20*time.Second, 1*time.Second).Should(Equal(provisioningv1.DPUError))
			Expect(err).To(HaveOccurred())
			Expect(status.Conditions).Should(ContainElement(
				And(
					HaveField("Type", provisioningv1.DPUCondOSInstalled.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "InstallationFailed"),
				),
			))
		})

		It("should retry if DMS is inaccessible", func() {
			By("simulating a situation that DMS is inaccessible")
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})
			dmsServer.Stop()

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)

			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("waiting for the async task to be finished")
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("failed to perform InstallOperation")))

			By("running again, should not transition to Error phase")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		})

		It("should retry if DPUFlavor CR not found", func() {
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)
			By("delete the DPUFlavor CR")
			Expect(k8sClient.Delete(ctx, flavor)).To(Succeed())

			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("waiting for the async task to be finished")
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("failed to get DPUFlavor")))

			By("running again, should not transition to Error phase")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		})

		It("should retry if DPUCluster CR not found", func() {
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)
			By("delete the DPUCluster CR")
			Expect(k8sClient.Delete(ctx, dpuCluster)).To(Succeed())

			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("waiting for the async task to be finished")
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("failed to get DPUCluster")))

			By("running again, should not transition to Error phase")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		})

		It("should retry if DPUDevice CR not found", func() {
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)
			By("delete the DPUDeviceCR")
			Expect(k8sClient.Delete(ctx, dpuDevice)).To(Succeed())

			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("waiting for the async task to be finished")
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("failed to get DPUDevice")))

			By("running again, should not transition to Error phase")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		})

		It("should retry if join command generation fails", func() {
			setupDMS(&localDMS{install: installSucc, activate: activateSucc, rebootStatus: rebootStatusSucc})

			dpuDevice := prepareDPUDevice()
			bfb := prepareBFB()
			flavor := prepareDPUFlavor()
			dpuCluster := prepareDPUCluster()
			_ = prepareDPFOperatorConfig()
			dpu := prepareDPU(dpuDevice, bfb, flavor, dpuCluster)

			By("simulating a situation that join command generation fails")
			status, err := gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &errorJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))

			By("waiting for the async task to be finished")
			dmsTaskName := dms.GenerateDMSTaskName(dpu.Namespace, dpu.Name)
			task, ok := dutil.OsInstallTaskMap.Load(dmsTaskName)
			Expect(ok).To(BeTrue())
			Expect(task).NotTo(BeNil())
			dmsTask := task.(dutil.TaskWithRetry)
			Expect(dmsTask.RetryCount).To(Equal(0))
			Eventually(func() error {
				_, err := dmsTask.Task.GetResult()
				return err
			}, 10*time.Second, 1*time.Second).Should(MatchError(ContainSubstring("failed to generate KubeadmJoin command")))

			By("running again, should not transition to Error phase")
			status, err = gnoi.Installing(ctx, dpu, &dutil.ControllerContext{
				Client:               k8sClient,
				JoinCommandGenerator: &errorJoinCommandGenerator{},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		})
	})
})

type mockJoinCommandGenerator struct{}

func (m *mockJoinCommandGenerator) GenerateJoinCommand(ctx context.Context, dpuCluster *provisioningv1.DPUCluster) (string, error) {
	return "mock join command", nil
}

type errorJoinCommandGenerator struct{}

func (m *errorJoinCommandGenerator) GenerateJoinCommand(ctx context.Context, dpuCluster *provisioningv1.DPUCluster) (string, error) {
	return "", fmt.Errorf("failed to generate join command")
}
