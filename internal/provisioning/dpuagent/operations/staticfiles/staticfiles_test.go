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
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StaticFiles", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "staticfiles-test-*")
		Expect(err).NotTo(HaveOccurred())
		files := []string{
			"/file",
			"/dir/file",
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
	It("should never be skipped", func() {
		operation := &VerifyStaticFiles{}
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
	})

	It("should succeed only if all files exist", func() {
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
})
