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

package ovsscript

import (
	"context"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OVSscriptOperation", func() {
	var (
		tempDir   string
		operation *RunOVSScript
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "ovsscript-test-*")
		Expect(err).NotTo(HaveOccurred())
		operation = &RunOVSScript{
			doneMarker: filepath.Join(tempDir, "run", "dpu-agent", "ovs-script-complete"),
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("ShouldSkip", func() {
		It("should skip if SkipOVSRawScript is true", func() {
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipOVSRawScript: true,
				},
				DPUFlavor: dpuFlavorWithOVSScript(),
			})).To(BeTrue())
		})

		It("should skip if RawConfigScript is empty", func() {
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipOVSRawScript: false,
				},
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{},
				},
			})).To(BeTrue())
		})

		It("should skip if a completion marker exists", func() {
			Expect(os.MkdirAll(filepath.Dir(operation.doneMarker), 0755)).To(Succeed())
			Expect(os.WriteFile(operation.doneMarker, nil, 0644)).To(Succeed())
			Expect(operation.ShouldSkip(&operations.Context{
				DPUFlavor: dpuFlavorWithOVSScript(),
			})).To(BeTrue())
		})

		It("should not skip if the completion marker is missing", func() {
			Expect(operation.ShouldSkip(&operations.Context{
				DPUFlavor: dpuFlavorWithOVSScript(),
			})).To(BeFalse())
		})
	})

	Context("Execute", func() {
		It("should run the script and write a completion marker", func() {
			mockScript := `
#!/bin/bash
set -e
TEST_FILE=$(dirname "$0")/test
echo -n "hello world" > "$TEST_FILE"
`
			mockScriptPath := filepath.Join(tempDir, "mock.sh")
			Expect(os.WriteFile(mockScriptPath, []byte(mockScript), 0755)).To(Succeed())
			operation.scriptPath = mockScriptPath
			err := operation.Execute(context.Background(), &operations.Context{
				Options: opts.Options{
					SkipOVSRawScript: false,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			data, err := os.ReadFile(filepath.Join(tempDir, "test"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("hello world"))
			marker, err := os.ReadFile(operation.doneMarker)
			Expect(err).NotTo(HaveOccurred())
			Expect(marker).To(BeEmpty())
		})

		It("should not write a completion marker if the script fails", func() {
			mockScriptPath := filepath.Join(tempDir, "fail.sh")
			Expect(os.WriteFile(mockScriptPath, []byte("#!/bin/bash\nexit 1\n"), 0755)).To(Succeed())
			operation.scriptPath = mockScriptPath
			err := operation.Execute(context.Background(), &operations.Context{
				CurrentBootID: "boot-1",
			})
			Expect(err).To(HaveOccurred())
			Expect(operation.doneMarker).NotTo(BeAnExistingFile())
		})
	})
})

func dpuFlavorWithOVSScript() provisioningv1.DPUFlavor {
	return provisioningv1.DPUFlavor{
		Spec: provisioningv1.DPUFlavorSpec{
			OVS: provisioningv1.DPUFlavorOVS{
				RawConfigScript: "ovs-vsctl add-br br-hbn",
			},
		},
	}
}
