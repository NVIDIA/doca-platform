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

package redfish

import (
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	redfishmock "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Rebooting", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "default"
	})

	DescribeTable("should reject host-agent reboot methods in the Redfish handler",
		func(method provisioningv1.NodeRebootMethod) {
			dpuNode := redfishTestDPUNode(namespace, "test-dpunode", method)
			dpu := redfishTestDPU(namespace, dpuNode.Name)
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

			status, err := Rebooting(ctx, dpu, redfishTestControllerContext(dpuNode))

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).To(ContainElement(And(
				HaveField("Type", provisioningv1.DPUCondRebooted.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "HostAgentRebootMethodNotSupported"),
			)))
		},
		Entry("hostAgent", provisioningv1.NodeRebootMethod{HostAgent: &provisioningv1.HostAgent{}}),
		Entry("gNOI", provisioningv1.NodeRebootMethod{GNOI: &provisioningv1.GNOI{}}), //nolint:staticcheck // GNOI is invalid for Redfish but still part of the API.
	)

	It("should reject nodeRebootMethod none for a non-hostless DPU", func() {
		dpuNode := redfishTestDPUNode(namespace, "test-dpunode", provisioningv1.NodeRebootMethod{None: &provisioningv1.None{}})
		dpu := redfishTestDPU(namespace, dpuNode.Name)
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

		status, err := Rebooting(ctx, dpu, redfishTestControllerContext(dpuNode))

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(status.Conditions).To(ContainElement(And(
			HaveField("Type", provisioningv1.DPUCondRebooted.String()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "HostlessStatusNotSet"),
		)))
	})

	It("should move to DPUError for unsupported nodeRebootMethod", func() {
		dpuNode := redfishTestDPUNode(namespace, "test-dpunode", provisioningv1.NodeRebootMethod{})
		dpu := redfishTestDPU(namespace, dpuNode.Name)
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

		status, err := Rebooting(ctx, dpu, redfishTestControllerContext(dpuNode))

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(status.Conditions).To(ContainElement(And(
			HaveField("Type", provisioningv1.DPUCondRebooted.String()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "UnsupportedNodeRebootMethod"),
		)))
	})

	It("should require a fresh DPU agent startup before completing hostless reboot", func() {
		oldStartup := metav1.NewTime(time.Now().Add(-time.Minute))
		newStartup := metav1.NewTime(time.Now())
		dpu := redfishTestDPU(namespace, "test-dpunode")
		dpu.Status.AgentLastStartupTime = &oldStartup

		Expect(hasFreshDPUAgentStartup(dpu)).To(BeFalse())

		dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
			LastStartupTime: &oldStartup,
		}
		Expect(hasFreshDPUAgentStartup(dpu)).To(BeFalse())

		dpu.Status.AgentStatus.LastStartupTime = &newStartup
		Expect(hasFreshDPUAgentStartup(dpu)).To(BeTrue())
	})
})

var _ = Describe("Hostless Rebooting via Redfish", func() {
	var (
		mockServer *redfishmock.RedfishMockServer
		dpu        *provisioningv1.DPU
		dpuDevice  *provisioningv1.DPUDevice
		dpuNode    *provisioningv1.DPUNode
		ctrlCtx    *dutil.ControllerContext
	)

	setupObjects := func(method provisioningv1.RebootMethodType) {
		var err error
		mockServer, err = redfishmock.CreateMockRedfishServer("BF-24.10", "password")
		Expect(err).NotTo(HaveOccurred())

		dpuDevice = dpuDeviceObj("dpu-device-hostless-reboot")
		dpuDevice.Spec.BMCIP = ptr.To(mockServer.GetIPAddress())
		dpuDevice.Spec.BMCPort = ptr.To(uint32(mockServer.GetPort()))
		createObject(dpuDevice)
		devicePatch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.BMCIP = dpuDevice.Spec.BMCIP
		dpuDevice.Status.BMCPort = dpuDevice.Spec.BMCPort
		dpuDevice.Status.DPUMode = provisioningv1.DpuMode
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, devicePatch)).To(Succeed())

		dpuNode = dpuNodeObj("dpu-node-hostless-reboot")
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{None: &provisioningv1.None{}}
		createObject(dpuNode)

		oldStartup := metav1.NewTime(time.Now().Add(-time.Minute))
		dpu = dpuObj("dpu-hostless-reboot")
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Status.Phase = provisioningv1.DPURebooting
		dpu.Status.DPUMode = provisioningv1.DpuMode
		dpu.Status.Hostless = true
		dpu.Status.AgentLastStartupTime = &oldStartup
		dpu.Status.RebootStatus = &provisioningv1.RebootStatus{
			Phase:  provisioningv1.RebootStatusPending,
			Method: ptr.To(method),
			Reason: "RebootRequested",
		}
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))
	}

	BeforeEach(func() {
		ctrlCtx = &dutil.ControllerContext{
			Client: k8sClient,
			Options: dutil.DPUOptions{
				DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
			},
		}
	})

	AfterEach(func() {
		if mockServer != nil {
			mockServer.Stop()
			mockServer = nil
		}
	})

	It("should ForceRestart for hostless PowerCycle", func() {
		setupObjects(provisioningv1.RebootMethodPowerCycle)
		status, err := Rebooting(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(mockServer.GetLastResetType()).To(Equal("ForceRestart"))
		Expect(status.RebootStatus).NotTo(BeNil())
		Expect(status.RebootStatus.Reason).To(Equal(hostlessForceRestartTriggered))
		Expect(status.RebootStatus.Message).To(ContainSubstring("ForceRestart"))
	})

	It("should ForceRestart for hostless SystemLevelReset", func() {
		setupObjects(provisioningv1.RebootMethodSystemLevelReset)
		status, err := Rebooting(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(mockServer.GetLastResetType()).To(Equal("ForceRestart"))
		Expect(status.RebootStatus).NotTo(BeNil())
		Expect(status.RebootStatus.Reason).To(Equal(hostlessForceRestartTriggered))
	})
})

func redfishTestDPU(namespace, dpuNodeName string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dpu",
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName: dpuNodeName,
		},
		Status: provisioningv1.DPUStatus{
			Phase:   provisioningv1.DPURebooting,
			DPUMode: provisioningv1.DpuMode,
		},
	}
}

func redfishTestDPUNode(namespace, name string, rebootMethod provisioningv1.NodeRebootMethod) *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &rebootMethod,
		},
	}
}

func redfishTestControllerContext(objects ...runtime.Object) *dutil.ControllerContext {
	scheme := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	return &dutil.ControllerContext{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(objects...).
			Build(),
		Scheme: scheme,
	}
}
