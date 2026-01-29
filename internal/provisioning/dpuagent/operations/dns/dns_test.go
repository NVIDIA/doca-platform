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

package dns

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DNS", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "dns-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("Configure DNS", func() {
		It("should skip if SkipDNSConfig is true", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: true,
				},
			})).To(BeTrue())
		})

		It("should create the necessary files", func() {
			operation := &ConfigureDNS{
				rootFS: tempDir,
			}
			err := operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			expectedFiles := []struct {
				path    string
				content string
			}{
				{
					path: filepath.Join(tempDir, "/etc/systemd/resolved.conf.d/01-dpf.conf"),
					content: `[Resolve]
DNSStubListener=no
`,
				},
				{
					path: filepath.Join(tempDir, "/etc/NetworkManager/conf.d/90-dpf.conf"),
					content: `[main]
dns=none
`,
				},
			}
			for _, file := range expectedFiles {
				By(fmt.Sprintf("checking file %s", file.path))
				content, err := os.ReadFile(file.path)
				Expect(err).NotTo(HaveOccurred())
				By(fmt.Sprintf("content: %s", content))
				By(fmt.Sprintf("expected content: %s", file.content))
				Expect(string(content)).To(Equal(file.content))
			}
		})
	})
})
