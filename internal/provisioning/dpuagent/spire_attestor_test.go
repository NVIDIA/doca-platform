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

package dpuagent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/spire/attestor"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const agentConfigWithoutAttestor = `plugins {
  WorkloadAttestor "unix" {
    plugin_data {}
  }
  # spire-k8s-workload-attestor
}
`

const agentConfigWithAttestor = `plugins {
  WorkloadAttestor "unix" {
    plugin_data {}
  }
  WorkloadAttestor "k8s" {
    plugin_data {}
  }
}
`

// stubRunner stands in for openssl, spire-agent and systemd.
type stubRunner struct {
	verifyErr error
	restarted bool
}

func (s *stubRunner) VerifyCert(_, _ string) error  { return s.verifyErr }
func (s *stubRunner) ValidateConfig(_ string) error { return nil }
func (s *stubRunner) RestartAgent() error           { s.restarted = true; return nil }

func writeAgentConfig(t *testing.T, contents string) attestor.Config {
	t.Helper()
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.conf")
	pluginPath := filepath.Join(dir, "plugin.conf")
	if err := os.WriteFile(agentPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("WorkloadAttestor \"k8s\" {\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := spireAttestorConfig()
	cfg.AgentConfigPath = agentPath
	cfg.PluginConfigPath = pluginPath
	return cfg
}

func TestSpireAttestorCondition(t *testing.T) {
	condType := provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled

	t.Run("true once the attestor is configured", func(t *testing.T) {
		got := spireAttestorCondition(writeAgentConfig(t, agentConfigWithAttestor), &stubRunner{})
		if got.Type != condType || got.Status != metav1.ConditionTrue {
			t.Errorf("condition = %+v, want %s=True", got, condType)
		}
	})

	// Enabling happens here, so a tick with usable certificates flips the condition.
	t.Run("enables the attestor and reports true", func(t *testing.T) {
		cfg := writeAgentConfig(t, agentConfigWithoutAttestor)
		runner := &stubRunner{}

		got := spireAttestorCondition(cfg, runner)
		if got.Status != metav1.ConditionTrue {
			t.Errorf("status = %v, want True: %s", got.Status, got.Message)
		}
		if !runner.restarted {
			t.Error("SPIRE agent was not restarted after enabling the attestor")
		}
		merged, err := os.ReadFile(cfg.AgentConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(merged), `WorkloadAttestor "k8s"`) {
			t.Errorf("attestor not spliced in:\n%s", merged)
		}
	})

	// Waiting for kubelet is the normal early state and must not read as a failure.
	t.Run("distinct reason while kubelet certificates are unusable", func(t *testing.T) {
		cfg := writeAgentConfig(t, agentConfigWithoutAttestor)
		runner := &stubRunner{verifyErr: errors.New("unable to get local issuer certificate")}

		got := spireAttestorCondition(cfg, runner)
		if got.Status != metav1.ConditionFalse {
			t.Errorf("status = %v, want False", got.Status)
		}
		if got.Reason != "WaitingForKubeletCertificates" {
			t.Errorf("reason = %q, want WaitingForKubeletCertificates", got.Reason)
		}
		if runner.restarted {
			t.Error("restarted the agent despite the certificates being unusable")
		}
	})

	t.Run("distinct reason when enabling fails outright", func(t *testing.T) {
		cfg := writeAgentConfig(t, agentConfigWithoutAttestor)
		cfg.AgentConfigPath = filepath.Join(t.TempDir(), "missing.conf")

		got := spireAttestorCondition(cfg, &stubRunner{})
		if got.Status != metav1.ConditionFalse {
			t.Errorf("status = %v, want False", got.Status)
		}
		if got.Reason != "EnableFailed" {
			t.Errorf("reason = %q, want EnableFailed", got.Reason)
		}
	})
}

// A tick that fails to patch must not record the condition as published, otherwise
// the next tick dedupes against it and the condition is never written.
func TestReporterRetriesUntilPublished(t *testing.T) {
	condType := provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled
	condition := metav1.Condition{
		Type:   condType,
		Status: metav1.ConditionTrue,
		Reason: condType,
	}

	r := &spireAttestorReporter{}
	if r.published != nil {
		t.Fatal("nothing has been published yet")
	}

	// A failed patch leaves published unset, so the same condition is still pending.
	if r.published != nil && !conditionChanged([]metav1.Condition{*r.published}, condition) {
		t.Error("the next tick would skip publishing after a failed patch")
	}

	// Once published, an unchanged condition is not rewritten every interval.
	r.published = &condition
	if conditionChanged([]metav1.Condition{*r.published}, condition) {
		t.Error("an unchanged condition must not be republished")
	}

	// A real transition is still published.
	waiting := metav1.Condition{
		Type:   condType,
		Status: metav1.ConditionFalse,
		Reason: "WaitingForKubeletCertificates",
	}
	if !conditionChanged([]metav1.Condition{*r.published}, waiting) {
		t.Error("a changed condition must be published")
	}
}

// The loop runs every interval, so it must only write status when something changed.
func TestConditionChanged(t *testing.T) {
	condType := provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled
	waiting := metav1.Condition{
		Type:    condType,
		Status:  metav1.ConditionFalse,
		Reason:  "WaitingForWorkloadAttestor",
		Message: "not yet",
	}

	if !conditionChanged(nil, waiting) {
		t.Error("a condition that is not present yet must be reported")
	}
	if conditionChanged([]metav1.Condition{waiting}, waiting) {
		t.Error("an unchanged condition must not be republished every interval")
	}

	enabled := waiting
	enabled.Status = metav1.ConditionTrue
	enabled.Reason = condType
	if !conditionChanged([]metav1.Condition{waiting}, enabled) {
		t.Error("the transition to enabled must be reported")
	}

	reworded := waiting
	reworded.Reason = "ConfigurationUnreadable"
	if !conditionChanged([]metav1.Condition{waiting}, reworded) {
		t.Error("a changed reason must be reported")
	}

	other := waiting
	other.Type = provisioningv1.DPUAgentConditionNVConfigApplied
	if !conditionChanged([]metav1.Condition{other}, waiting) {
		t.Error("a different condition type must not suppress reporting")
	}
}
