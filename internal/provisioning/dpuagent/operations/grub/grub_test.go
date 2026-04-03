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

package grub

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
)

var _ = Describe("Grub Operation", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "grub-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("ShouldSkip", func() {
		It("should be skipped if SkipKernelCmdLine is true", func() {
			operation := &ConfigureKernelCmdLine{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipKernelCmdLine: true,
				},
			})).To(BeTrue())
		})

		It("should be skipped if no kernel parameters are specified", func() {
			operation := &ConfigureKernelCmdLine{}
			Expect(operation.ShouldSkip(&operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{},
						},
					},
				},
			})).To(BeTrue())
		})

		It("should not be skipped if kernel parameters are specified", func() {
			operation := &ConfigureKernelCmdLine{}
			Expect(operation.ShouldSkip(&operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			})).To(BeFalse())
		})
	})

	Context("Execute", func() {
		var procCmdlinePath string

		// writeFakeCmdline creates a fake /proc/cmdline file with the given content.
		writeFakeCmdline := func(content string) {
			procCmdlinePath = filepath.Join(tempDir, "cmdline")
			Expect(os.WriteFile(procCmdlinePath, []byte(content), 0644)).To(Succeed())
		}

		It("should write kernel parameters to grub config file", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz root=UUID=xxx ro quiet\n")
			grubConfigDir := filepath.Join(tempDir, "grub.d")
			updateGrubCalled := false
			syncCalled := false

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					switch cmd {
					case "update-grub":
						updateGrubCalled = true
					case "sync":
						syncCalled = true
					default:
						Fail("unexpected command: " + cmd)
					}
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt", "intel_iommu=on"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())

			By("verify grub config file content")
			configPath := filepath.Join(grubConfigDir, grubConfigFileName)
			content, err := os.ReadFile(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("GRUB_CMDLINE_LINUX=\"iommu=pt intel_iommu=on\"\n"))

			By("verify update-grub and sync were called")
			Expect(updateGrubCalled).To(BeTrue())
			Expect(syncCalled).To(BeTrue())

			By("verify GrubConfigChanged flag is set")
			Expect(optCtx.GrubConfigChanged).To(BeTrue())
		})

		It("should skip if grub config exists and all parameters are already in cmdline", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz iommu=pt intel_iommu=on ro quiet\n")
			grubConfigDir := filepath.Join(tempDir, "grub.d")

			By("pre-create grub config file with expected content")
			Expect(os.MkdirAll(grubConfigDir, 0755)).To(Succeed())
			configPath := filepath.Join(grubConfigDir, grubConfigFileName)
			Expect(os.WriteFile(configPath, []byte("GRUB_CMDLINE_LINUX=\"iommu=pt intel_iommu=on\"\n"), 0644)).To(Succeed())

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Fail("update-grub should not be called when config exists and params already present")
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt", "intel_iommu=on"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(optCtx.GrubConfigChanged).To(BeFalse())
		})

		It("should proceed if params are in cmdline but grub config does not exist", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz iommu=pt intel_iommu=on ro quiet\n")
			grubConfigDir := filepath.Join(tempDir, "grub.d")
			updateGrubCalled := false

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					updateGrubCalled = true
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt", "intel_iommu=on"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateGrubCalled).To(BeTrue())

			By("verify grub config file was written")
			configPath := filepath.Join(grubConfigDir, grubConfigFileName)
			content, err := os.ReadFile(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("GRUB_CMDLINE_LINUX=\"iommu=pt intel_iommu=on\"\n"))
			Expect(optCtx.GrubConfigChanged).To(BeTrue())
		})

		It("should proceed if only some parameters are present in cmdline", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz iommu=pt ro quiet\n")
			grubConfigDir := filepath.Join(tempDir, "grub.d")
			updateGrubCalled := false

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					updateGrubCalled = true
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt", "intel_iommu=on"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateGrubCalled).To(BeTrue())
			Expect(optCtx.GrubConfigChanged).To(BeTrue())
		})

		It("should handle single kernel parameter", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz ro\n")
			grubConfigDir := filepath.Join(tempDir, "grub.d")

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"quiet"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())

			configPath := filepath.Join(grubConfigDir, grubConfigFileName)
			content, err := os.ReadFile(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("GRUB_CMDLINE_LINUX=\"quiet\"\n"))
			Expect(optCtx.GrubConfigChanged).To(BeTrue())
		})

		It("should return error if update-grub fails", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz ro\n")
			grubConfigDir := filepath.Join(tempDir, "grub.d")

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stderr bytes.Buffer
					stderr.WriteString("update-grub: command not found")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 127")
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to update grub"))
			Expect(err.Error()).To(ContainSubstring("update-grub: command not found"))
			Expect(optCtx.GrubConfigChanged).To(BeFalse())
		})

		It("should create grub config directory if it does not exist", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz ro\n")
			grubConfigDir := filepath.Join(tempDir, "nested", "grub.d")

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: procCmdlinePath,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())

			By("verify directory was created")
			info, err := os.Stat(grubConfigDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
			Expect(optCtx.GrubConfigChanged).To(BeTrue())
		})

		It("should proceed with grub update if proc cmdline file does not exist", func() {
			grubConfigDir := filepath.Join(tempDir, "grub.d")
			updateGrubCalled := false

			operation := &ConfigureKernelCmdLine{
				grubConfigDir:   grubConfigDir,
				procCmdlinePath: filepath.Join(tempDir, "nonexistent-cmdline"),
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					updateGrubCalled = true
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateGrubCalled).To(BeTrue())
			Expect(optCtx.GrubConfigChanged).To(BeTrue())
		})
	})
})

var _ = Describe("CheckKernelCmdLine Operation", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "grub-check-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("ShouldSkip", func() {
		It("should not be skipped even if grub configuration is skipped", func() {
			operation := &CheckKernelCmdLine{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipKernelCmdLine: true,
				},
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			})).To(BeFalse())
		})

		It("should be skipped if no kernel parameters are specified", func() {
			operation := &CheckKernelCmdLine{}
			Expect(operation.ShouldSkip(&operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{},
						},
					},
				},
			})).To(BeTrue())
		})

		It("should not be skipped if kernel parameters are specified", func() {
			operation := &CheckKernelCmdLine{}
			Expect(operation.ShouldSkip(&operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			})).To(BeFalse())
		})
	})

	Context("Execute", func() {
		var procCmdlinePath string

		writeFakeCmdline := func(content string) {
			procCmdlinePath = filepath.Join(tempDir, "cmdline")
			Expect(os.WriteFile(procCmdlinePath, []byte(content), 0644)).To(Succeed())
		}

		It("should succeed when all desired parameters are active", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz iommu=pt intel_iommu=on ro quiet\n")

			operation := &CheckKernelCmdLine{
				procCmdlinePath: procCmdlinePath,
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt", "intel_iommu=on"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when some parameters are missing", func() {
			writeFakeCmdline("BOOT_IMAGE=/boot/vmlinuz iommu=pt ro quiet\n")

			operation := &CheckKernelCmdLine{
				procCmdlinePath: procCmdlinePath,
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt", "intel_iommu=on"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not all desired kernel parameters are active"))
		})

		It("should return error when proc cmdline file does not exist", func() {
			operation := &CheckKernelCmdLine{
				procCmdlinePath: filepath.Join(tempDir, "nonexistent-cmdline"),
			}

			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Grub: provisioningv1.DPUFlavorGrub{
							KernelParameters: []string{"iommu=pt"},
						},
					},
				},
			}

			err := operation.Execute(context.Background(), optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not all desired kernel parameters are active"))
		})
	})
})
