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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/gnoi"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Phase ConfigFWParams", func() {
	var (
		defaultFlavorName = "flavor-configfwparams-test"
		defaultDPUName    = "dpu-configfwparams-test"
	)
	var prepareDPUFlavor = func() *provisioningv1.DPUFlavor {
		flavor := flavorObj(defaultFlavorName)
		flavor.Spec.DpuMode = provisioningv1.DpuMode
		createObject(flavor)
		return flavor
	}
	var succSet = func(_ context.Context, _ *gnmi.SetRequest) (*gnmi.SetResponse, error) { // nolint:unparam
		return &gnmi.SetResponse{}, nil
	}
	var getDPUMode = func(_ context.Context, _ *gnmi.GetRequest) (*gnmi.GetResponse, error) { // nolint:unparam
		return &gnmi.GetResponse{
			Notification: []*gnmi.Notification{
				{
					Update: []*gnmi.Update{
						{
							Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "platform"}, {Name: "mode"}}},
							Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: "DPU"}},
						},
					},
				},
			},
		}, nil
	}
	var internalErrorGet = func(_ context.Context, _ *gnmi.GetRequest) (*gnmi.GetResponse, error) { // nolint:unparam
		return nil, grpcstatus.Error(codes.Internal, "internal error")
	}

	Context("successful cases", func() {
		It("should set the fw params config", func() {
			setupDMS(&localDMS{set: succSet, get: getDPUMode})
			flavor := prepareDPUFlavor()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "ConfigureFWParameters"),
				),
			))
		})
		It("should set the fw with empty dpu mode in flavor", func() {
			setupDMS(&localDMS{set: succSet, get: getDPUMode})
			flavor := flavorObj(defaultFlavorName)
			flavor.Spec.DpuMode = ""
			createObject(flavor)
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "ConfigureFWParameters"),
				),
			))
		})
	})

	Context("error handling", func() {
		It("should retry if flavor is not found", func() {
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "a-flavor-that-does-not-exist")
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUFlavorNotFound"),
				),
			))
		})
		It("Error if flavor is in nic mode", func() {
			flavor := flavorObj(defaultFlavorName)
			flavor.Spec.DpuMode = provisioningv1.NicMode
			createObject(flavor)
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "RequestedDPUModeUnsupported"),
				),
			))
		})
		It("Error if flavor is in zero-trust mode", func() {
			flavor := flavorObj(defaultFlavorName)
			flavor.Spec.DpuMode = provisioningv1.ZeroTrustMode
			createObject(flavor)
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "RequestedDPUModeUnsupported"),
				),
			))
		})
		It("retry if DMS is inaccessible", func() {
			setupDMS(&localDMS{set: succSet, get: getDPUMode})
			dmsServer.Stop()
			flavor := prepareDPUFlavor()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToGetDPUMode"),
				),
			))
		})
		It("retry if DMS returns an error", func() {
			By("start a dms server with unimplemented APIs")
			setupDMS(&localDMS{get: internalErrorGet})
			flavor := prepareDPUFlavor()
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, flavor.Name)
			dpu.Status.Phase = provisioningv1.DPUConfigFWParameters
			status, err := gnoi.ConfigFWParameters(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondFWConfigured.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToGetDPUMode"),
				),
			))
		})
	})
})
