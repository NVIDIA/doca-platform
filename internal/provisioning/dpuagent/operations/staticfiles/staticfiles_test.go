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

package staticfiles

import (
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testDPUNamespace = "test-ns"
	testConfigMap    = "doca-profile"
)

var _ = Describe("StaticFiles", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "staticfiles-test-*")
		Expect(err).NotTo(HaveOccurred())
		files := []string{
			"file",
			"dir/file",
		}
		for _, file := range files {
			filePath := filepath.Join(tempDir, file)
			Expect(os.MkdirAll(filepath.Dir(filePath), 0755)).To(Succeed())
			Expect(os.WriteFile(filePath, []byte(""), 0644)).To(Succeed())
			_, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	newTestScheme := func() *runtime.Scheme {
		s := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(s))
		utilruntime.Must(provisioningv1.AddToScheme(s))
		return s
	}

	It("should never be skipped", func() {
		operation := &VerifyStaticFiles{}
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
	})

	It("should succeed only if all raw files exist", func() {
		operation := &VerifyStaticFiles{
			rootFS: tempDir,
		}

		By("should succeed if all files exist")
		err := operation.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Path: "/file",
						},
						{
							Path: "/dir/file",
						},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("should fail if any file does not exist")
		err = operation.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Path: "/nonexistent/file",
						},
					},
				},
			},
		})
		Expect(err).To(HaveOccurred())
	})

	It("should resolve contentFrom and write override files", func() {
		operation := &VerifyStaticFiles{
			rootFS: tempDir,
		}
		cm := &corev1.ConfigMap{
			Data: map[string]string{
				"profile.conf": "key=value\n",
			},
		}
		cm.Namespace = testDPUNamespace
		cm.Name = testConfigMap
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(cm).Build()

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{DPUNamespace: testDPUNamespace},
			Client:  fakeClient,
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type:        ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							Path:        "/etc/doca/profile.conf",
							Operation:   provisioningv1.FileOverride,
							Permissions: "0640",
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: testConfigMap},
									Key:                  "profile.conf",
								},
							},
						},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		data, err := os.ReadFile(filepath.Join(tempDir, "etc/doca/profile.conf"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("key=value\n"))
	})

	It("should resolve contentFrom from BinaryData and write override files", func() {
		operation := &VerifyStaticFiles{
			rootFS: tempDir,
		}
		cm := &corev1.ConfigMap{
			BinaryData: map[string][]byte{
				"profile.conf": []byte("key=value\n"),
			},
		}
		cm.Namespace = testDPUNamespace
		cm.Name = testConfigMap
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(cm).Build()

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{DPUNamespace: testDPUNamespace},
			Client:  fakeClient,
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type:        ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							Path:        "/etc/doca/profile.conf",
							Operation:   provisioningv1.FileOverride,
							Permissions: "0640",
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: testConfigMap},
									Key:                  "profile.conf",
								},
							},
						},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		data, err := os.ReadFile(filepath.Join(tempDir, "etc/doca/profile.conf"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("key=value\n"))
	})

	It("should fail when referenced ConfigMap key is missing", func() {
		operation := &VerifyStaticFiles{
			rootFS: tempDir,
		}
		cm := &corev1.ConfigMap{}
		cm.Namespace = testDPUNamespace
		cm.Name = testConfigMap
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(cm).Build()

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{DPUNamespace: testDPUNamespace},
			Client:  fakeClient,
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type: ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							Path: "/etc/doca/profile.conf",
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: testConfigMap},
									Key:                  "missing",
								},
							},
						},
					},
				},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("key not found"))
	})

	It("should fail when contentFrom is missing for agent-applied file", func() {
		operation := &VerifyStaticFiles{
			rootFS: tempDir,
		}
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{DPUNamespace: testDPUNamespace},
			Client:  fakeClient,
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type: ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							Path: "/etc/doca/profile.conf",
						},
					},
				},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("contentFrom.configMapKeyRef is required"))
	})

	It("should create files with requested mode without transient 0644", func() {
		operation := &VerifyStaticFiles{
			rootFS: tempDir,
		}
		cm := &corev1.ConfigMap{
			Data: map[string]string{
				"secure.conf": "token=secret\n",
			},
		}
		cm.Namespace = testDPUNamespace
		cm.Name = testConfigMap
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(cm).Build()

		err := operation.Execute(ctx, &operations.Context{
			Options: opts.Options{DPUNamespace: testDPUNamespace},
			Client:  fakeClient,
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type:        ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							Path:        "/etc/doca/secure.conf",
							Operation:   provisioningv1.FileOverride,
							Permissions: "0600",
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: testConfigMap},
									Key:                  "secure.conf",
								},
							},
						},
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		info, err := os.Stat(filepath.Join(tempDir, "etc/doca/secure.conf"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
	})
})
