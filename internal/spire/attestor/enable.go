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

// Package attestor enables the SPIRE Kubernetes workload attestor on a DPU.
//
// The attestor authenticates with the kubelet client certificate, which only exists
// after TLS bootstrap, so it cannot be configured when the SPIRE agent starts. Cloud
// init writes the agent configuration with a marker line that this package replaces
// once the certificates are usable.
package attestor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMarker is the placeholder line the attestor plugin replaces.
const DefaultMarker = "# spire-k8s-workload-attestor"

// agentConfigMode keeps the merged configuration readable only by root.
const agentConfigMode os.FileMode = 0o600

// Config describes where the pieces of the SPIRE agent configuration live.
type Config struct {
	AgentConfigPath   string
	PluginConfigPath  string
	Marker            string
	ClusterCAPath     string
	KubeletClientPath string
	KubeletServerPath string
}

// Runner performs the actions that need external tools, so the merge logic can be
// tested without openssl, spire-agent or systemd.
type Runner interface {
	VerifyCert(caPath, certPath string) error
	ValidateConfig(configPath string) error
	RestartAgent() error
}

// ErrMarkerNotFound is returned when the agent configuration has no marker line.
// Continuing would restart the agent with an unchanged configuration and report success.
var ErrMarkerNotFound = errors.New("marker not found in SPIRE agent configuration")

// IsEnabled reports whether the Kubernetes workload attestor is configured.
func IsEnabled(agentConfigPath string) (bool, error) {
	agentConfig, err := os.ReadFile(agentConfigPath)
	if err != nil {
		return false, fmt.Errorf("reading SPIRE agent configuration: %w", err)
	}
	return isEnabled(string(agentConfig)), nil
}

func isEnabled(agentConfig string) bool {
	return strings.Contains(agentConfig, `WorkloadAttestor "k8s"`)
}

// CertificatesReady reports whether kubelet has produced certificates the workload
// attestor can use. They are expected to be missing for a while after boot.
func CertificatesReady(cfg Config, runner Runner) error {
	if err := runner.VerifyCert(cfg.ClusterCAPath, cfg.KubeletClientPath); err != nil {
		return fmt.Errorf("kubelet client certificate is not usable: %w", err)
	}
	// The serving certificate is self-signed unless rotation is enabled, so it is
	// verified against itself.
	if err := runner.VerifyCert(cfg.KubeletServerPath, cfg.KubeletServerPath); err != nil {
		return fmt.Errorf("kubelet serving certificate is not usable: %w", err)
	}
	return nil
}

// Enable splices the workload attestor into the SPIRE agent configuration and
// restarts the agent. It is a no-op once configured, and checks the certificates
// once rather than waiting, so callers can simply retry.
func Enable(cfg Config, runner Runner) error {
	agentConfig, err := os.ReadFile(cfg.AgentConfigPath)
	if err != nil {
		return fmt.Errorf("reading SPIRE agent configuration: %w", err)
	}

	if isEnabled(string(agentConfig)) {
		return nil
	}

	if err := CertificatesReady(cfg, runner); err != nil {
		return err
	}

	pluginConfig, err := os.ReadFile(cfg.PluginConfigPath)
	if err != nil {
		return fmt.Errorf("reading workload attestor configuration: %w", err)
	}

	merged, err := merge(string(agentConfig), string(pluginConfig), cfg.Marker)
	if err != nil {
		return err
	}

	if err := writeConfig(cfg.AgentConfigPath, merged, runner); err != nil {
		return err
	}

	if err := runner.RestartAgent(); err != nil {
		return fmt.Errorf("restarting SPIRE agent: %w", err)
	}
	return nil
}

// merge replaces the marker line with the workload attestor configuration. Matching
// ignores indentation so reformatting the agent configuration cannot silently break it.
func merge(agentConfig, pluginConfig, marker string) (string, error) {
	lines := strings.Split(agentConfig, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}
		merged := append([]string{}, lines[:i]...)
		merged = append(merged, strings.Split(strings.TrimRight(pluginConfig, "\n"), "\n")...)
		merged = append(merged, lines[i+1:]...)
		return strings.Join(merged, "\n"), nil
	}
	return "", fmt.Errorf("%w: %q", ErrMarkerNotFound, marker)
}

// writeConfig validates before replacing, so a rejected configuration never reaches
// the agent, and renames so the agent never sees a partially written file.
func writeConfig(path, contents string, runner Runner) error {
	candidate, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating candidate configuration: %w", err)
	}
	// Best effort: the file is already gone on the success path.
	defer func() { _ = os.Remove(candidate.Name()) }()

	if _, err := candidate.WriteString(contents); err != nil {
		_ = candidate.Close()
		return fmt.Errorf("writing candidate configuration: %w", err)
	}
	if err := candidate.Close(); err != nil {
		return fmt.Errorf("closing candidate configuration: %w", err)
	}
	// CreateTemp already uses 0600; set it so the mode does not depend on that.
	if err := os.Chmod(candidate.Name(), agentConfigMode); err != nil {
		return fmt.Errorf("setting candidate configuration mode: %w", err)
	}

	if err := runner.ValidateConfig(candidate.Name()); err != nil {
		return fmt.Errorf("validating merged configuration: %w", err)
	}

	if err := os.Rename(candidate.Name(), path); err != nil {
		return fmt.Errorf("installing merged configuration: %w", err)
	}
	return nil
}
