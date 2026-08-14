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

package attestor

import (
	"fmt"
	"os/exec"
	"strings"
)

// ExecRunner runs the real tools on the DPU.
type ExecRunner struct {
	OpenSSLPath    string
	SPIREAgentPath string
	SystemctlPath  string
	AgentUnit      string
}

var _ Runner = &ExecRunner{}

func (e *ExecRunner) VerifyCert(caPath, certPath string) error {
	return run(e.OpenSSLPath, "verify", "-CAfile", caPath, certPath)
}

func (e *ExecRunner) ValidateConfig(configPath string) error {
	return run(e.SPIREAgentPath, "validate", "-config", configPath)
}

// RestartAgent restarts the SPIRE agent. The caller is ordered after
// spire-agent.service, so it does not wait for the job and risk a systemd deadlock.
func (e *ExecRunner) RestartAgent() error {
	return run(e.SystemctlPath, "--no-block", "restart", e.AgentUnit)
}

// run keeps the command output, otherwise failures surface as a bare exit status.
func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
