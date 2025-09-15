/*
Copyright 2025 NVIDIA

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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

//nolint:dupl
var _ = Describe("External DPF tests", Labels{externalTestLabel}, func() {
	BeforeEach(func() {
		for _, label := range CurrentSpecReport().Labels() {
			if label == requiresNodesLabel {
				By("Waiting for provisioning")
				VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
				By("Waiting for DPU cluster pods to be ready")
				VerifyDPUClusterPods(ctx, systemPodsToVerify)
			}
		}
	})

	Context("External DFP tests", Labels{}, func() {
		It("Executes external bash command(s) from environment variable", func() {
			By("DPF system is configured, provisioned and ready for external testing")

			if len(externalTestCommands) == 0 {
				Skip("EXTERNAL_TEST_COMMAND environment variable is empty, skipping external test")
			}

			By(fmt.Sprintf("Executing %d external command(s)", len(externalTestCommands)))

			// Execute each command
			for i, cmdStr := range externalTestCommands {
				By(fmt.Sprintf("Executing command %d/%d: %s", i+1, len(externalTestCommands), cmdStr))

				// Split the command into command and arguments
				cmdParts := strings.Fields(cmdStr)
				if len(cmdParts) == 0 {
					By(fmt.Sprintf("Skipping empty command %d", i+1))
					continue
				}

				// Create and execute the command
				cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
				output, err := cmd.CombinedOutput()

				// Log the output for debugging
				if len(output) > 0 {
					By(fmt.Sprintf("Command %d output: %s", i+1, string(output)))
				}

				// Assert that the command executed successfully
				Expect(err).NotTo(HaveOccurred(), "External command %d failed with error: %v", i+1, err)
			}
		})
	})
})
