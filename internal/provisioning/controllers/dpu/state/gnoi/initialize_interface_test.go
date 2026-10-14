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
	"net"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/gnoi"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Phase InitializeInterface", func() {
	var (
		defaultDPUName    = "dpu-initialize-interface-test"
		defaultFlavorName = "flavor-initialize-interface-test"
	)

	var getSucc = func(_ context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
		path := req.GetPath()
		if len(path) == 0 {
			return nil, status.Error(codes.InvalidArgument, "path is empty")
		}
		return &gnmi.GetResponse{
			Notification: []*gnmi.Notification{
				{
					Update: []*gnmi.Update{
						{
							Path: path[0],
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_StringVal{
									// a description of the DPU, copied from a real DPU.
									StringVal: "Description: NVIDIA BlueField-3 B3220 P-Series FHHL DPU; 200GbE (default mode) / NDR200 IB; Dual-port QSFP112; PCIe Gen5.0 x16 with x16 PCIe extension option; 16 Arm cores; 32GB on-board DDR; integrated BMC; Crypto Enabled",
								},
							},
						},
					},
				},
			},
		}, nil
	}

	var getErr = func(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
		return nil, status.Error(codes.Internal, "error")
	}

	var getUnparseable = func(_ context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
		path := req.GetPath()
		if len(path) == 0 {
			return nil, status.Error(codes.InvalidArgument, "path is empty")
		}
		return &gnmi.GetResponse{
			Notification: []*gnmi.Notification{
				{
					Update: []*gnmi.Update{
						{
							Path: path[0],
							Val: &gnmi.TypedValue{
								Value: &gnmi.TypedValue_StringVal{
									StringVal: "unparseable string",
								},
							},
						},
					},
				},
			},
		}, nil
	}

	Context("capacity check", func() {
		It("sufficient capacity", func() {
			setupDMS(&localDMS{get: getSucc})

			testFlavor := flavorObj(defaultFlavorName)
			createObject(testFlavor)
			testDPU := dpuObj(defaultDPUName, testDPUNode.Name, testFlavor.Name)
			testDPU.Status.Phase = provisioningv1.DPUInitializeInterface

			status, err := gnoi.InitializeInterface(ctx, testDPU,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).Should(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "DMSInitialized"),
				),
			))
		})

		It("insufficient capacity", func() {
			setupDMS(&localDMS{get: getSucc})

			testFlavor := flavorObj(defaultFlavorName)
			testFlavor.Spec.DPUResources = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("33Gi"),
			}
			createObject(testFlavor)
			testDPU := dpuObj(defaultDPUName, testDPUNode.Name, testFlavor.Name)
			testDPU.Status.Phase = provisioningv1.DPUInitializeInterface

			status, err := gnoi.InitializeInterface(ctx, testDPU,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).Should(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "InsufficientResources"),
				),
			))
		})

		It("retry if DMS is unaccessable", func() {
			dmsIP := "127.0.0.1"
			listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", dmsIP))
			Expect(err).To(Succeed())
			testDPUNode = dpuNodeObj("dpu-node-test", listener)
			createObject(testDPUNode)

			By("close the listener to simulate unaccessable DMS")
			listener.Close() // nolint:errcheck

			// create CRs
			testFlavor := flavorObj(defaultFlavorName)
			createObject(testFlavor)

			testDPU := dpuObj(defaultDPUName, testDPUNode.Name, testFlavor.Name)
			testDPU.Status.Phase = provisioningv1.DPUInitializeInterface
			status, err := gnoi.InitializeInterface(ctx, testDPU,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).Should(Equal(provisioningv1.DPUInitializeInterface))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToGetDPUCapacity"),
				),
			))
		})

		It("retry if DMS returns error", func() {
			setupDMS(&localDMS{get: getErr})

			testFlavor := flavorObj(defaultFlavorName)
			createObject(testFlavor)
			testDPU := dpuObj(defaultDPUName, testDPUNode.Name, testFlavor.Name)
			testDPU.Status.Phase = provisioningv1.DPUInitializeInterface

			status, err := gnoi.InitializeInterface(ctx, testDPU,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).Should(Equal(provisioningv1.DPUInitializeInterface))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToGetDPUCapacity"),
				),
			))
		})

		It("skip capacity check if reply is unparseable", func() {
			setupDMS(&localDMS{get: getUnparseable})

			testFlavor := flavorObj(defaultFlavorName)
			createObject(testFlavor)
			testDPU := dpuObj(defaultDPUName, testDPUNode.Name, testFlavor.Name)
			testDPU.Status.Phase = provisioningv1.DPUInitializeInterface

			status, err := gnoi.InitializeInterface(ctx, testDPU,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).Should(Equal(provisioningv1.DPUConfigFWParameters))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "UnableToCheckCapacity"),
				),
			))
		})

		It("Error if PCIAddress is not set", func() {
			testFlavor := flavorObj(defaultFlavorName)
			createObject(testFlavor)
			testDPU := dpuObj(defaultDPUName, testDPUNode.Name, testFlavor.Name)
			testDPU.Spec.PCIAddress = nil
			testDPU.Status.Phase = provisioningv1.DPUInitializeInterface
			status, err := gnoi.InitializeInterface(ctx, testDPU,
				&dutil.ControllerContext{Client: k8sClient})
			Expect(err).To(Succeed())
			Expect(status.Phase).Should(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInterfaceInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "InvalidPCIAddress"),
				),
			))
		})
	})
})
