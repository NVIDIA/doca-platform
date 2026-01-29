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

package kernelmodule

import (
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Kernel Module", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "kernelmodule-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("loading kernel module", func() {
		It("should never be skipped", func() {
			operation := &LoadModule{}
			Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
		})

		It("should load and persist", func() {
			loadModuleCalled := false
			operation := &LoadModule{
				rootFS: tempDir,
				loadModule: func(module string) error {
					loadModuleCalled = true
					return nil
				},
			}
			By("check that the file does not exist (should be created by the operation)")
			_, err := os.Stat(filepath.Join(tempDir, confPath))
			Expect(os.IsNotExist(err)).To(BeTrue())

			err = operation.Execute(ctx, &operations.Context{})
			Expect(err).NotTo(HaveOccurred())
			Expect(loadModuleCalled).To(BeTrue())
			content, err := os.ReadFile(filepath.Join(tempDir, confPath))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(moduleName))
		})
	})
})
