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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const agentConfigTemplate = `agent {
  data_dir = "/var/lib/spire/agent"
  trust_domain = "cs.internal"
}

plugins {
  NodeAttestor "dpu_hw" {
    plugin_data {}
  }
  WorkloadAttestor "unix" {
    plugin_data {}
  }
  # spire-k8s-workload-attestor
}
`

const pluginConfig = `WorkloadAttestor "k8s" {
  plugin_data {
    kubelet_ca_path = "/var/lib/kubelet/pki/kubelet.crt"
  }
}
`

// fakeRunner records what was asked of it and fails on demand.
type fakeRunner struct {
	verifyErr   error
	validateErr error
	restartErr  error

	verifyCalls   int
	validatedBody string
	restarted     bool
}

func (f *fakeRunner) VerifyCert(_, _ string) error {
	f.verifyCalls++
	return f.verifyErr
}

func (f *fakeRunner) ValidateConfig(path string) error {
	// Read it here: the candidate is renamed away on success, so the test cannot
	// inspect what was validated afterwards.
	if body, err := os.ReadFile(path); err == nil {
		f.validatedBody = string(body)
	}
	return f.validateErr
}

func (f *fakeRunner) RestartAgent() error {
	f.restarted = true
	return f.restartErr
}

func writeConfigs(t *testing.T, agent string) Config {
	t.Helper()
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.conf")
	pluginPath := filepath.Join(dir, "k8s-workload-attestor.conf")
	if err := os.WriteFile(agentPath, []byte(agent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(pluginConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{
		AgentConfigPath:   agentPath,
		PluginConfigPath:  pluginPath,
		Marker:            DefaultMarker,
		ClusterCAPath:     filepath.Join(dir, "ca.crt"),
		KubeletClientPath: filepath.Join(dir, "kubelet-client.pem"),
		KubeletServerPath: filepath.Join(dir, "kubelet.crt"),
	}
}

func TestEnableSplicesAttestorAndRestarts(t *testing.T) {
	cfg := writeConfigs(t, agentConfigTemplate)
	runner := &fakeRunner{}

	if err := Enable(cfg, runner); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	merged, err := os.ReadFile(cfg.AgentConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), `WorkloadAttestor "k8s"`) {
		t.Errorf("attestor not spliced in:\n%s", merged)
	}
	if strings.Contains(string(merged), DefaultMarker) {
		t.Errorf("marker not consumed:\n%s", merged)
	}
	// The rest of the configuration must survive untouched.
	if !strings.Contains(string(merged), `NodeAttestor "dpu_hw"`) {
		t.Errorf("existing plugins lost:\n%s", merged)
	}
	if !runner.restarted {
		t.Error("SPIRE agent was not restarted")
	}
	// The agent must never see a configuration that was not validated first.
	if runner.validatedBody != string(merged) {
		t.Error("validated content differs from installed content")
	}

	info, err := os.Stat(cfg.AgentConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != agentConfigMode {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), agentConfigMode)
	}
}

// The loop calls this every tick, so a second call must do nothing at all.
func TestEnableIsIdempotent(t *testing.T) {
	already := strings.Replace(agentConfigTemplate, "  "+DefaultMarker, pluginConfig, 1)
	cfg := writeConfigs(t, already)
	runner := &fakeRunner{}

	if err := Enable(cfg, runner); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if runner.restarted {
		t.Error("restarted the agent despite the attestor already being configured")
	}
	if runner.verifyCalls != 0 {
		t.Errorf("checked certificates unnecessarily: %d calls", runner.verifyCalls)
	}
}

// Enable checks the certificates once and returns; the caller is the retry loop.
func TestEnableReportsUnusableCertificates(t *testing.T) {
	cfg := writeConfigs(t, agentConfigTemplate)
	runner := &fakeRunner{verifyErr: errors.New("unable to get local issuer certificate")}

	err := Enable(cfg, runner)
	if err == nil {
		t.Fatal("expected an error while the certificates are unusable")
	}
	// The reason must survive into the reported condition.
	if !strings.Contains(err.Error(), "unable to get local issuer certificate") {
		t.Errorf("error does not carry the openssl reason: %v", err)
	}
	if runner.restarted {
		t.Error("restarted the agent despite never merging the configuration")
	}
}

func TestCertificatesReadyDistinguishesClientFromServing(t *testing.T) {
	cfg := writeConfigs(t, agentConfigTemplate)

	if err := CertificatesReady(cfg, &fakeRunner{}); err != nil {
		t.Errorf("CertificatesReady: %v", err)
	}

	failing := &fakeRunner{verifyErr: errors.New("boom")}
	err := CertificatesReady(cfg, failing)
	if err == nil || !strings.Contains(err.Error(), "client certificate") {
		t.Errorf("error = %v, want it to name the client certificate", err)
	}
}

// A missing marker must fail rather than restart the agent with an unchanged
// configuration and report success.
func TestEnableFailsWhenMarkerMissing(t *testing.T) {
	withoutMarker := strings.Replace(agentConfigTemplate, "  "+DefaultMarker+"\n", "", 1)
	cfg := writeConfigs(t, withoutMarker)
	runner := &fakeRunner{}

	err := Enable(cfg, runner)
	if !errors.Is(err, ErrMarkerNotFound) {
		t.Fatalf("error = %v, want ErrMarkerNotFound", err)
	}
	if runner.restarted {
		t.Error("restarted the agent without changing the configuration")
	}
}

func TestEnableLeavesConfigIntactWhenValidationFails(t *testing.T) {
	cfg := writeConfigs(t, agentConfigTemplate)
	runner := &fakeRunner{validateErr: errors.New("invalid HCL")}

	if err := Enable(cfg, runner); err == nil {
		t.Fatal("expected a validation error")
	}

	current, err := os.ReadFile(cfg.AgentConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != agentConfigTemplate {
		t.Errorf("agent configuration was modified despite validation failing:\n%s", current)
	}
	if runner.restarted {
		t.Error("restarted the agent with a rejected configuration")
	}

	// The candidate must not be left behind next to the configuration.
	entries, err := os.ReadDir(filepath.Dir(cfg.AgentConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("candidate configuration left behind: %v", entries)
	}
}

func TestIsEnabled(t *testing.T) {
	cfg := writeConfigs(t, agentConfigTemplate)
	enabled, err := IsEnabled(cfg.AgentConfigPath)
	if err != nil || enabled {
		t.Errorf("IsEnabled = %v, %v; want false, nil", enabled, err)
	}

	if _, err := IsEnabled(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Error("expected an error for an unreadable configuration")
	}
}

func TestMergeIgnoresMarkerIndentation(t *testing.T) {
	indented := strings.Replace(agentConfigTemplate, "  "+DefaultMarker, "      "+DefaultMarker, 1)
	merged, err := merge(indented, pluginConfig, DefaultMarker)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(merged, `WorkloadAttestor "k8s"`) {
		t.Errorf("attestor not spliced in:\n%s", merged)
	}
}
