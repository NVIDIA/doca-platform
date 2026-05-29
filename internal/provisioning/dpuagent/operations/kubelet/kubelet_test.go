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
	"fmt"
	"os"
	"path/filepath"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(provisioningv1.AddToScheme(s))
	return s
}

// minimalKubeletConfigYAML is a valid KubeletConfiguration stub for tests.
const minimalKubeletConfigYAML = `apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
`

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
		It("should be skipped if SkipRemoveBuiltinKubelet is true", func() {
			operation := &RemoveBuiltinKubelet{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipRemoveBuiltinKubelet: true,
				},
			})).To(BeTrue())
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

	Context("StartKubelet", func() {
		It("should be skipped if SkipStartKubelet is true", func() {
			operation := &StartKubelet{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipStartKubelet: true,
				},
			})).To(BeTrue())
		})

		It("should disable and start kubelet", func() {
			var executedCmds []string
			operation := &StartKubelet{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{})
			Expect(err).NotTo(HaveOccurred())
			Expect(executedCmds).To(Equal([]string{"systemctl disable kubelet", "systemctl start kubelet"}))
		})

		It("should return error if disable fails", func() {
			operation := &StartKubelet{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stderr bytes.Buffer
					stderr.WriteString("Failed to disable kubelet.service")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
				},
			}
			err := operation.Execute(ctx, &operations.Context{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to disable kubelet"))
		})

		It("should return error if start fails", func() {
			operation := &StartKubelet{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if cmd == "systemctl disable kubelet" {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					}
					var stderr bytes.Buffer
					stderr.WriteString("Failed to start kubelet.service")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 5")
				},
			}
			err := operation.Execute(ctx, &operations.Context{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to start kubelet"))
		})
	})

	Context("ConfigureKubelet", func() {
		It("should be skipped if SkipConfigureKubelet is true", func() {
			operation := &ConfigureKubelet{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipConfigureKubelet: true,
				},
			})).To(BeTrue())
		})

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
						AgentStatus: &provisioningv1.AgentStatus{
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
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    fakeClient,
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
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeadm-join", Namespace: "default"},
				Data:       map[string][]byte{"other-key": []byte("some-value")},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    fakeClient,
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
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeadm-join", Namespace: "default"},
				Data:       map[string][]byte{"join": []byte("kubeadm join --token xxx")},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    fakeClient,
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
			kubeletDataConfig := filepath.Join(tempDir, "config.yaml")
			Expect(os.WriteFile(kubeletDataConfig, []byte(minimalKubeletConfigYAML), 0644)).To(Succeed())

			operation := &ConfigureKubelet{
				caPath:            filepath.Join(tempDir, "ca.crt"),
				bootstrapPath:     filepath.Join(tempDir, "bootstrap.conf"),
				kubeletDataConfig: kubeletDataConfig,
				systemdDropInDir:  systemdDropInDir,
				stopKubelet: func() error {
					return nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if cmd == "kubelet --version" {
						var stdout bytes.Buffer
						stdout.WriteString("Kubernetes v1.33.3")
						return stdout, bytes.Buffer{}, nil
					}
					joinCmdExecuted = cmd
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeadm-join-secret", Namespace: "kube-system"},
				Data:       map[string][]byte{"join": []byte("kubeadm join 10.0.0.1:6443 --token abcdef")},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    fakeClient,
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

		It("should set all hardening fields and keep existing settings in kubelet config", func() {
			kubeletDataConfig := filepath.Join(tempDir, "config.yaml")
			initial := minimalKubeletConfigYAML + "clusterDNS:\n- 10.96.0.10\n"
			Expect(os.WriteFile(kubeletDataConfig, []byte(initial), 0644)).To(Succeed())

			operation := &ConfigureKubelet{kubeletDataConfig: kubeletDataConfig}
			Expect(operation.addKubeletCustomizedConfig()).To(Succeed())

			merged, err := os.ReadFile(kubeletDataConfig)
			Expect(err).NotTo(HaveOccurred())
			cfg := &kubeletconfigv1beta1.KubeletConfiguration{}
			Expect(yaml.Unmarshal(merged, cfg)).To(Succeed())
			Expect(cfg.ProtectKernelDefaults).To(BeTrue())
			Expect(cfg.SeccompDefault).NotTo(BeNil())
			Expect(*cfg.SeccompDefault).To(BeTrue())
			Expect(cfg.StreamingConnectionIdleTimeout.Duration).To(Equal(5 * time.Minute))
			Expect(cfg.EventRecordQPS).NotTo(BeNil())
			Expect(*cfg.EventRecordQPS).To(Equal(int32(50)))
			Expect(cfg.ClusterDNS).To(Equal([]string{"10.96.0.10"}))
		})

		It("should return an error if the kubelet config file does not exist", func() {
			missing := filepath.Join(tempDir, "nonexistent-config.yaml")
			operation := &ConfigureKubelet{kubeletDataConfig: missing}
			err := operation.addKubeletCustomizedConfig()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should return an error if the kubelet config file is not valid YAML", func() {
			kubeletDataConfig := filepath.Join(tempDir, "bad.yaml")
			Expect(os.WriteFile(kubeletDataConfig, []byte("{invalid"), 0644)).To(Succeed())
			operation := &ConfigureKubelet{kubeletDataConfig: kubeletDataConfig}
			err := operation.addKubeletCustomizedConfig()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse kubelet config"))
		})

		It("should clean up before joining", func() {
			caPath := filepath.Join(tempDir, "ca.crt")
			bootstrapPath := filepath.Join(tempDir, "bootstrap.conf")
			kubeletConfPath := filepath.Join(tempDir, "kubelet.conf")

			By("creating existing CA, bootstrap and kubelet config files")
			Expect(os.WriteFile(caPath, []byte("old-ca"), 0644)).To(Succeed())
			Expect(os.WriteFile(bootstrapPath, []byte("old-bootstrap"), 0644)).To(Succeed())
			Expect(os.WriteFile(kubeletConfPath, []byte("old-kubelet-config"), 0644)).To(Succeed())

			kubeletDataConfig := filepath.Join(tempDir, "config.yaml")
			Expect(os.WriteFile(kubeletDataConfig, []byte(minimalKubeletConfigYAML), 0644)).To(Succeed())

			stopKubeletCalled := false
			operation := &ConfigureKubelet{
				caPath:            caPath,
				bootstrapPath:     bootstrapPath,
				kubeletConfPath:   kubeletConfPath,
				kubeletDataConfig: kubeletDataConfig,
				systemdDropInDir:  filepath.Join(tempDir, "systemd"),
				stopKubelet: func() error {
					stopKubeletCalled = true
					return nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if cmd == "kubelet --version" {
						var stdout bytes.Buffer
						stdout.WriteString("Kubernetes v1.33.3")
						return stdout, bytes.Buffer{}, nil
					}
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kubeadm-join", Namespace: "default"},
				Data:       map[string][]byte{"join": []byte("kubeadm join")},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(secret).Build()
			err := operation.Execute(ctx, &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
				Client:    fakeClient,
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
