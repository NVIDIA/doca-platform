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

package kubelet

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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockClient struct {
	getObjectFunc    func(ctx context.Context, namespace, name string, obj client.Object) error
	updateStatusFunc func(ctx context.Context, status provisioningv1.DPUInternalStatus) error
	healthCheckFunc  func() error
}

func (m *mockClient) GetObject(ctx context.Context, namespace, name string, obj client.Object) error {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, namespace, name, obj)
	}
	return nil
}

func (m *mockClient) UpdateStatus(ctx context.Context, status provisioningv1.DPUInternalStatus) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, status)
	}
	return nil
}

func (m *mockClient) HealthCheck() error {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc()
	}
	return nil
}

var _ = Describe("Kubelet", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "kubelet-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("when built-in kubelet config is present", func() {
		It("should never be skipped", func() {
			operation := &RemoveBuiltinKubelet{}
			Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
		})

		It("should remove the built-in kubelet config", func() {
			mockFile := filepath.Join(tempDir, "/usr/lib/systemd/system/kubelet.service.d/90-kubelet-bluefield.conf")
			Expect(os.MkdirAll(filepath.Dir(mockFile), 0755)).To(Succeed())
			Expect(os.WriteFile(mockFile, []byte(""), 0644)).To(Succeed())
			_, err := os.Stat(mockFile)
			Expect(err).NotTo(HaveOccurred())
			operation := &RemoveBuiltinKubelet{
				rootFS: tempDir,
				stopKubelet: func() error {
					return nil
				},
			}
			err = operation.Execute(ctx, &operations.Context{})
			Expect(err).NotTo(HaveOccurred())
			_, err = os.Stat(mockFile)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should not stop kubelet if the built-in kubelet config is not present", func() {
			mockFile := filepath.Join(tempDir, "/usr/lib/systemd/system/kubelet.service.d/90-kubelet-bluefield.conf")
			_, err := os.Stat(mockFile)
			Expect(os.IsNotExist(err)).To(BeTrue())
			operation := &RemoveBuiltinKubelet{
				rootFS: tempDir,
				stopKubelet: func() error {
					Fail("should not stop kubelet")
					return nil
				},
			}
			err = operation.Execute(ctx, &operations.Context{})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("ConfigureKubelet", func() {
		It("should return error if LatestDPU is nil", func() {
			operation := &ConfigureKubelet{}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: nil,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("latest DPU not retrieved"))
		})

		It("should skip if kubelet is already configured", func() {
			stopKubeletCalled := false
			operation := &ConfigureKubelet{
				stopKubelet: func() error {
					stopKubeletCalled = true
					return nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUInternalStatus: &provisioningv1.DPUInternalStatus{
							Conditions: []metav1.Condition{
								{
									Type:   conditionType,
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(stopKubeletCalled).To(BeFalse())
		})

		It("should return error if GetObject fails", func() {
			operation := &ConfigureKubelet{
				caPath:           filepath.Join(tempDir, "ca.crt"),
				bootstrapPath:    filepath.Join(tempDir, "bootstrap.conf"),
				systemdDropInDir: filepath.Join(tempDir, "systemd"),
				stopKubelet: func() error {
					return nil
				},
			}
			mockCli := &mockClient{
				getObjectFunc: func(ctx context.Context, namespace, name string, obj client.Object) error {
					return fmt.Errorf("failed to get secret")
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    mockCli,
				Options: opts.Options{
					KubeadmSecretNamespace: "default",
					KubeadmSecretName:      "kubeadm-join",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get kubeadm secret"))
		})

		It("should return error if secret does not contain join key", func() {
			operation := &ConfigureKubelet{
				caPath:           filepath.Join(tempDir, "ca.crt"),
				bootstrapPath:    filepath.Join(tempDir, "bootstrap.conf"),
				systemdDropInDir: filepath.Join(tempDir, "systemd"),
				stopKubelet: func() error {
					return nil
				},
			}
			mockCli := &mockClient{
				getObjectFunc: func(ctx context.Context, namespace, name string, obj client.Object) error {
					secret := obj.(*corev1.Secret)
					secret.Data = map[string][]byte{
						"other-key": []byte("some-value"),
					}
					return nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    mockCli,
				Options: opts.Options{
					KubeadmSecretNamespace: "default",
					KubeadmSecretName:      "kubeadm-join",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain key"))
		})

		It("should return error if join command fails", func() {
			operation := &ConfigureKubelet{
				caPath:           filepath.Join(tempDir, "ca.crt"),
				bootstrapPath:    filepath.Join(tempDir, "bootstrap.conf"),
				systemdDropInDir: filepath.Join(tempDir, "systemd"),
				stopKubelet: func() error {
					return nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("join failed")
				},
			}
			mockCli := &mockClient{
				getObjectFunc: func(ctx context.Context, namespace, name string, obj client.Object) error {
					secret := obj.(*corev1.Secret)
					secret.Data = map[string][]byte{
						"join": []byte("kubeadm join --token xxx"),
					}
					return nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    mockCli,
				Options: opts.Options{
					KubeadmSecretNamespace: "default",
					KubeadmSecretName:      "kubeadm-join",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to run join command"))
		})

		It("should successfully execute join command", func() {
			joinCmdExecuted := ""
			systemdDropInDir := filepath.Join(tempDir, "systemd")
			operation := &ConfigureKubelet{
				caPath:           filepath.Join(tempDir, "ca.crt"),
				bootstrapPath:    filepath.Join(tempDir, "bootstrap.conf"),
				systemdDropInDir: systemdDropInDir,
				stopKubelet: func() error {
					return nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					joinCmdExecuted = cmd
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			mockCli := &mockClient{
				getObjectFunc: func(ctx context.Context, namespace, name string, obj client.Object) error {
					Expect(namespace).To(Equal("kube-system"))
					Expect(name).To(Equal("kubeadm-join-secret"))
					secret := obj.(*corev1.Secret)
					secret.Data = map[string][]byte{
						"join": []byte("kubeadm join 10.0.0.1:6443 --token abcdef"),
					}
					return nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    mockCli,
				Options: opts.Options{
					KubeadmSecretNamespace: "kube-system",
					KubeadmSecretName:      "kubeadm-join-secret",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying join command was executed")
			Expect(joinCmdExecuted).To(Equal("kubeadm join 10.0.0.1:6443 --token abcdef"))

			By("verifying systemd drop-in directory was created")
			info, err := os.Stat(systemdDropInDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())

			By("verifying systemd drop-in file was created with correct content")
			dropInPath := filepath.Join(systemdDropInDir, kubeletSystemdDropInFile)
			content, err := os.ReadFile(dropInPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(kubeletSystemdDropInConfig))
		})

		It("should clean up before joining", func() {
			caPath := filepath.Join(tempDir, "ca.crt")
			bootstrapPath := filepath.Join(tempDir, "bootstrap.conf")
			kubeletConfPath := filepath.Join(tempDir, "kubelet.conf")

			By("creating existing CA, bootstrap and kubelet config files")
			Expect(os.WriteFile(caPath, []byte("old-ca"), 0644)).To(Succeed())
			Expect(os.WriteFile(bootstrapPath, []byte("old-bootstrap"), 0644)).To(Succeed())
			Expect(os.WriteFile(kubeletConfPath, []byte("old-kubelet-config"), 0644)).To(Succeed())

			stopKubeletCalled := false
			operation := &ConfigureKubelet{
				caPath:           caPath,
				bootstrapPath:    bootstrapPath,
				kubeletConfPath:  kubeletConfPath,
				systemdDropInDir: filepath.Join(tempDir, "systemd"),
				stopKubelet: func() error {
					stopKubeletCalled = true
					return nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			mockCli := &mockClient{
				getObjectFunc: func(ctx context.Context, namespace, name string, obj client.Object) error {
					secret := obj.(*corev1.Secret)
					secret.Data = map[string][]byte{
						"join": []byte("kubeadm join"),
					}
					return nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    mockCli,
				Options: opts.Options{
					KubeadmSecretNamespace: "default",
					KubeadmSecretName:      "kubeadm-join",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying stopKubelet was called")
			Expect(stopKubeletCalled).To(BeTrue())

			By("verifying CA and bootstrap files are removed")
			_, err = os.Stat(caPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
			_, err = os.Stat(bootstrapPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
			_, err = os.Stat(kubeletConfPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

	})
})
