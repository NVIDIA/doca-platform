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

package state_test

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Phase Initializing", func() {
	var (
		defaultDPUName        = "dpu-initializing-test"
		defaultDPUNodeName    = "dpu-node-initializing-test"
		defaultDPUDeviceName  = "dpu-device-initializing-test"
		defaultDPUClusterName = "dpu-cluster-initializing-test"
		strTrue               = "true"
	)

	Context("successful cases", func() {
		It("should transition to the next phase", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPUCluster CR")
			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			// redfish does not need PCI address. Here, it is assigned to simplify the test
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
					),
				))
			}
			runForEachInterface(run)
		})
	})

	Context("error handling", func() {
		It("should retry if DPUNode is not found", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = "non-existent-dpu-node"
			dpu.Status.Phase = provisioningv1.DPUInitializing
			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					})
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUNodeNotFound"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should retry if DPUDevice is not found", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			createObject(dpuNode)
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = "non-existent-dpu-device"
			dpu.Status.Phase = provisioningv1.DPUInitializing
			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					})
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUDeviceNotFound"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should requeue if PCI address is not specified - exclusive for gNOI", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			status, err := state.Initializing(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "PCIAddressNotProvided"),
				),
			))
		})
		It("should retry if OOB bridge label is missing", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			delete(dpuNode.Labels, cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel)
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUOOBBridgeNotConfigured"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should retry if fails to allocate DPUCluster", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			// redfish does not need PCI address. Here, it is assigned to simplify the test
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client:           k8sClient,
						ClusterAllocator: &allocateFail{},
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUClusterNotAllocated"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should retry if DPUCluster is not found", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			// redfish does not need PCI address. Here, it is assigned to simplify the test
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = "non-existent-namespace"
			dpu.Spec.Cluster.Name = "non-existent-dpu-cluster"
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client:           k8sClient,
						ClusterAllocator: &allocateFail{},
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUClusterNotFound"),
					),
				))
			}
			runForEachInterface(run)

		})
	})
})

type allocateFail struct{}

func (a *allocateFail) Allocate(ctx context.Context, dpu *provisioningv1.DPU) (allocator.AllocateResult, error) {
	return types.NamespacedName{}, fmt.Errorf("allocate failed")
}

func (a *allocateFail) SaveAssignedDPU(*provisioningv1.DPU) {
}

func (a *allocateFail) SaveCluster(*provisioningv1.DPUCluster) {
}

func (a *allocateFail) ReleaseDPU(*provisioningv1.DPU) {
}

func (a *allocateFail) RemoveCluster(*provisioningv1.DPUCluster) {
}

// type allocateSuccess struct {
// 	obj client.Object
// }

// func (a *allocateSuccess) Allocate(ctx context.Context, dpu *provisioningv1.DPU) (allocator.AllocateResult, error) {
// 	return types.NamespacedName{
// 		Namespace: a.obj.GetNamespace(),
// 		Name:      a.obj.GetName(),
// 	}, nil
// }

// func (a *allocateSuccess) SaveAssignedDPU(*provisioningv1.DPU) {
// }

// func (a *allocateSuccess) SaveCluster(*provisioningv1.DPUCluster) {
// }

// func (a *allocateSuccess) ReleaseDPU(*provisioningv1.DPU) {
// }

// func (a *allocateSuccess) RemoveCluster(*provisioningv1.DPUCluster) {
// }
