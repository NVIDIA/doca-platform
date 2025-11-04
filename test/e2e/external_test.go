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
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

//nolint:dupl
var _ = Describe("External DPF tests", Labels{externalTestLabel}, func() {
	Context("External DPF tests - OVNK based", func() {
		BeforeEach(func() {
			By("wait for OVNK HBN deployment to be ready")
			WaitForOVNKHBNDeploymentReady(ctx, input)
		})

		It("Executes external bash script from the externalTestScript argument", Labels{ovnkPrimaryLabel, requiresNodesLabel}, func() {
			By("DPF system is configured, provisioned and ready for external testing")
			runExternalTestScript()
		})
	})
})

func runExternalTestScript() {
	if len(externalTest) == 0 {
		Skip("externalTest path is empty, skipping external test")
	}
	// Create the command (script itself will respect shebang if executable)
	cmd := exec.Command(externalTest)

	// Stream stdout/stderr directly
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	Expect(err).NotTo(HaveOccurred(), "Script failed with error: %v", err)
}
