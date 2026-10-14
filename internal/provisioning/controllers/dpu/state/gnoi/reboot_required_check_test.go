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
	"regexp"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/gnoi"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type getFunc func(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error)

var _ = Describe("Phase RebootRequiredCheck", func() {
	var (
		defaultDPUName = "dpu-rebootrequiredcheck-test"
		supported      = "Supported"
		errorString    = "!!error!!"
	)

	// pathToString converts a gnmi.Path to a string. This is a copy of the function in DMS source code.
	var pathToString = func(path *gnmi.Path) (string, []string, error) {
		if path == nil || len(path.Elem) == 0 {
			return "", nil, grpcstatus.Errorf(codes.NotFound, "path to string failed: path is nil")
		}
		str := ""
		keys := make([]string, 1)
		for _, elem := range path.Elem {
			str += "/"
			str += elem.Name
			for key, val := range elem.Key {
				str += "[" + key + "=$" + strconv.Itoa(len(keys)) + "]"
				keys = append(keys, val)
			}
		}
		return str, keys, nil
	}

	var rescan string
	var curVer string
	var runnerVer string
	var resetLevel3 string
	var syncLevel1 string
	var fwReset string
	var apiBuilder = func() getFunc {
		return func(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error) {
			path := req.GetPath()
			if len(path) == 0 {
				return nil, grpcstatus.Error(codes.InvalidArgument, "path is empty")
			}
			_, cmd, err := pathToString(path[0])
			if err != nil {
				return nil, grpcstatus.Errorf(codes.InvalidArgument, "path to string failed: %v", err)
			}
			command := strings.Join(cmd, "")
			patterns := map[string]string{
				`pci_rescan_required`:               rescan,
				`'FW Version'`:                      curVer,
				`'FW Version\(Running\)'`:           runnerVer,
				`'3: Driver restart and PCI reset'`: resetLevel3,
				`'1: Driver is the owner'`:          syncLevel1,
				`\-y \-l 3 \-\-sync 1 r`:            fwReset,
			}
			for pattern, val := range patterns {
				com := regexp.MustCompile(pattern)
				if !com.MatchString(command) {
					continue
				}
				if val == errorString {
					return nil, grpcstatus.Errorf(codes.Internal, "error")
				}
				return &gnmi.GetResponse{
					Notification: []*gnmi.Notification{
						{
							Update: []*gnmi.Update{
								{
									Val: &gnmi.TypedValue{
										Value: &gnmi.TypedValue_StringVal{
											StringVal: val,
										},
									},
								},
							},
						},
					},
				}, nil
			}
			return nil, grpcstatus.Errorf(codes.Internal, "no match for command: %s", command)
		}
	}

	AfterEach(func() {
		rescan = ""
		curVer = ""
		runnerVer = ""
		resetLevel3 = ""
		syncLevel1 = ""
		fwReset = ""
	})

	Context("successful cases", func() {
		It("rescan=1", func() {
			rescan = "1"
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
				),
			))
		})
		It("rescan=0 + same FW version", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = "v1"
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
				),
			))
		})
		It("rescan=0 + different FW version + can reset", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = "v2"
			resetLevel3 = supported
			syncLevel1 = supported
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
				),
			))
		})
		It("rescan=0 + different FW version + can't reset", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = "v2"
			resetLevel3 = supported
			syncLevel1 = "Not Supported"
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
				),
			))
		})
	})

	Context("error handling", func() {
		// TODO: should read PCI address from DPU.spec
		It("retry if the PCI address label is missing from DPU object", func() {
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUObjectMissingPCIAddressLabel"),
				),
			))
		})
		It("retry if rescan returns invalid respons", func() {
			rescan = "not a number"
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToConvertHexToDec"),
				),
			))
		})
		It("retry if rescan fails", func() {
			rescan = errorString
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToCheckPCIRescanRequired"),
				),
			))
		})

		It("retry if current version checking fails", func() {
			rescan = "0"
			curVer = errorString
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToCheckCurrentFirmwareVersion"),
				),
			))
		})
		It("retry if running version checking fails", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = errorString
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToCheckRunningFirmwareVersion"),
				),
			))
		})
		It("retry if reset level 3 checking fails", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = "v2"
			resetLevel3 = errorString
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToCheckResetLevel"),
				),
			))
		})
		It("retry if sync level 1 checking fails", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = "v2"
			resetLevel3 = supported
			syncLevel1 = errorString
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToCheckSyncLevel"),
				),
			))
		})
		It("retry if fw reset fails", func() {
			rescan = "0"
			curVer = "v1"
			runnerVer = "v2"
			resetLevel3 = supported
			syncLevel1 = supported
			fwReset = errorString
			setupDMS(&localDMS{get: apiBuilder()})
			dpu := dpuObj(defaultDPUName, testDPUNode.Name, "no-flavor-is-need-for-this-phase")
			dpu.Labels[cutil.DPUDevicePCIAddressLabel] = *dpu.Spec.PCIAddress
			dpu.Status.Phase = provisioningv1.DPUCheckingHostRebootNeed
			status, err := gnoi.RebootRequiredCheck(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUCheckingHostRebootNeed))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondCheckedHostRebootNeed.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "FailedToResetFirmware"),
				),
			))
		})
	})
})
