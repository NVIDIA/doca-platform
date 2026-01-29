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

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OVSscriptOperation", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "ovsscript-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("Execute", func() {
		It("should skip if SkipOVSRawScript is true", func() {
			operation := &RunOVSScript{
				scriptPath: tempDir,
			}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipOVSRawScript: true,
				},
			})).To(BeTrue())
		})

		It("should run the script", func() {
			mockScript := `
#!/bin/bash
set -e
TEST_FILE=$(dirname "$0")/test
echo -n "hello world" > "$TEST_FILE"
`
			mockScriptPath := filepath.Join(tempDir, "mock.sh")
			Expect(os.WriteFile(mockScriptPath, []byte(mockScript), 0755)).To(Succeed())
			operation := &RunOVSScript{
				scriptPath: mockScriptPath,
			}
			err := operation.Execute(context.Background(), &operations.Context{
				Options: opts.Options{
					SkipOVSRawScript: false,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			data, err := os.ReadFile(filepath.Join(tempDir, "test"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("hello world"))
		})
	})
})
