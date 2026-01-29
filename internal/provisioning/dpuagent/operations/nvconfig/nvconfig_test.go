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

package nvconfig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NVConfig Operation", func() {
	var testNS *corev1.Namespace

	var createDPU = func(name string, namespace string) *provisioningv1.DPU {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   "test-dpu-node",
				DPUDeviceName: "test-dpu-device",
				BFB:           "bfb-test",
				SerialNumber:  "test-dpu-serial-number",
				DPUFlavor:     "test-dpu-flavor",
			},
		}
		Expect(k8sClient.Create(ctx, dpu)).To(Succeed())
		return dpu
	}

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "nvconfig-test-*")
		Expect(err).NotTo(HaveOccurred())

		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "nvconfig-testns-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
		_ = k8sClient.Delete(ctx, testNS)
	})

	Context("Get Latest DPU", func() {
		It("should never be skipped", func() {
			operation := &GetLatestDPU{}
			Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
		})

		It("should assign the latestDPU gloval variable", func() {
			dpu := createDPU("test-dpu", testNS.Name)
			operation := &GetLatestDPU{}
			operationCtx := operations.Context{
				Options: opts.Options{
					DPUNamespace: testNS.Name,
					DPUName:      dpu.Name,
				},
				Client: &mockClient{
					getObjectFunc: func(execCtx context.Context, namespace, name string, obj client.Object) error {
						Expect(k8sClient.Get(execCtx, client.ObjectKey{Namespace: namespace, Name: name}, obj)).To(Succeed())
						return nil
					},
				},
			}
			Expect(operation.Execute(ctx, &operationCtx)).To(Succeed())
			Expect(operationCtx.LatestDPU).NotTo(BeNil())
			Expect(operationCtx.LatestDPU.Name).To(Equal(dpu.Name))
			Expect(operationCtx.LatestDPU.Namespace).To(Equal(dpu.Namespace))
			Expect(operationCtx.LatestDPU.Spec).To(Equal(dpu.Spec))
			Expect(operationCtx.LatestDPU.Status).To(Equal(dpu.Status))
		})
	})

	Context("Set NVConfig", func() {
		It("should skip if NVConfig is not specified in DPUFlavor", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{},
				},
			}
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				Fail("should not run any bash commands")
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				mstDevicesPath: tempDir,
				runBash:        runBash,
			}
			Expect(operation.ShouldSkip(&operations.Context{DPUFlavor: dpuFlavor})).To(BeTrue())
		})

		It("should skip if NVConfig is already configured", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PARAM1=VALUE1", "PARAM2=VALUE2"},
						},
					},
				},
			}
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				Fail("should not run any bash commands")
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				mstDevicesPath: tempDir,
				runBash:        runBash,
			}
			operationCtx := &operations.Context{
				DPUFlavor: dpuFlavor,
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
						Conditions: []metav1.Condition{
							{
								Type:   CondNVConfigApplied,
								Status: metav1.ConditionTrue,
								Reason: CondNVConfigApplied,
							},
						},
					},
				},
			}
			Expect(operation.ShouldSkip(operationCtx)).To(BeTrue())
		})

		It("should succeed", func() {
			By("create mock mst devices")
			Expect(os.WriteFile(filepath.Join(tempDir, "dev1"), []byte(""), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "dev2"), []byte(""), 0600)).To(Succeed())

			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Device:     ptr.To("*"),
							Parameters: []string{"PARAM1=VALUE1", "PARAM2=VALUE2"},
						},
						{
							Parameters: []string{"PARAM3=VALUE3", "PARAM4=VALUE4"},
						},
						{
							Device:     ptr.To(filepath.Join(tempDir, "dev1")),
							Parameters: []string{"PARAM5=VALUE5", "PARAM6=VALUE6"},
						},
						{
							Device:     ptr.To(filepath.Join(tempDir, "dev2")),
							Parameters: []string{"PARAM7=VALUE7", "PARAM8=VALUE8"},
						},
					},
				},
			}

			expectedCommands := []string{
				fmt.Sprintf("mlxconfig -d %s -y reset", filepath.Join(tempDir, "dev1")),
				fmt.Sprintf("mlxconfig -d %s -y reset", filepath.Join(tempDir, "dev2")),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM1=VALUE1 PARAM2=VALUE2", filepath.Join(tempDir, "dev1")),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM1=VALUE1 PARAM2=VALUE2", filepath.Join(tempDir, "dev2")),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM3=VALUE3 PARAM4=VALUE4", filepath.Join(tempDir, "dev1")),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM3=VALUE3 PARAM4=VALUE4", filepath.Join(tempDir, "dev2")),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM5=VALUE5 PARAM6=VALUE6", filepath.Join(tempDir, "dev1")),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM7=VALUE7 PARAM8=VALUE8", filepath.Join(tempDir, "dev2")),
			}
			cmdIdx := 0
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				By(fmt.Sprintf("expecting command: %s, received: %s", expectedCommands[cmdIdx], cmd))
				Expect(cmd).To(Equal(expectedCommands[cmdIdx]))
				cmdIdx++
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				mstDevicesPath: tempDir,
				runBash:        runBash,
			}
			operationCtx := &operations.Context{
				DPUFlavor: dpuFlavor,
				Client: &mockClient{
					updateStatusFunc: func(execCtx context.Context, status provisioningv1.DPUInternalStatus) error {
						return nil
					},
					healthCheckFunc: func() error {
						return nil
					},
				},
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
						Conditions: []metav1.Condition{
							{
								Type:   CondNVConfigApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			}

			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
		})
	})
})

type mockClient struct {
	getObjectFunc    func(execCtx context.Context, namespace, name string, obj client.Object) error
	updateStatusFunc func(execCtx context.Context, status provisioningv1.DPUInternalStatus) error
	healthCheckFunc  func() error
}

func (m *mockClient) GetObject(execCtx context.Context, namespace, name string, obj client.Object) error {
	return m.getObjectFunc(execCtx, namespace, name, obj)
}

func (m *mockClient) UpdateStatus(execCtx context.Context, status provisioningv1.DPUInternalStatus) error {
	return m.updateStatusFunc(execCtx, status)
}

func (m *mockClient) HealthCheck() error {
	return m.healthCheckFunc()
}
