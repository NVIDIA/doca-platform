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
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/spire/attestor"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// spireAttestorInterval is how often enablement is attempted. The loop is the retry:
// each tick makes one attempt, so certificates that are not ready yet just mean
// trying again next tick.
const spireAttestorInterval = time.Minute

// spireAttestorConfig returns the paths cloud-init writes on a SPIFFE DPU.
func spireAttestorConfig() attestor.Config {
	return attestor.Config{
		AgentConfigPath:   "/etc/spire/agent/agent.conf",
		PluginConfigPath:  "/etc/spire/agent/k8s-workload-attestor.conf",
		Marker:            attestor.DefaultMarker,
		ClusterCAPath:     "/etc/kubernetes/pki/ca.crt",
		KubeletClientPath: "/var/lib/kubelet/pki/kubelet-client-current.pem",
		KubeletServerPath: "/var/lib/kubelet/pki/kubelet.crt",
	}
}

func spireAttestorRunner() attestor.Runner {
	return &attestor.ExecRunner{
		OpenSSLPath:    "openssl",
		SPIREAgentPath: "/usr/bin/spire-agent",
		SystemctlPath:  "systemctl",
		AgentUnit:      "spire-agent.service",
	}
}

// StartSPIREAttestorLoop enables the SPIRE Kubernetes workload attestor and reports
// whether it is configured.
//
// This is a loop rather than an operation because the attestor needs the kubelet
// client certificate, which only appears after TLS bootstrap, so waiting for it in
// the operation sequence would block everything behind it.
//
// Restarting the SPIRE agent from here is safe: the DPU agent's JWT-SVID is already
// on disk and stays valid while the agent re-attests, and the client re-reads the
// token file.
func (d *DPUAgent) StartSPIREAttestorLoop(ctx context.Context) {
	if !d.optCtx.Options.SpiffeMode {
		return
	}
	reporter := &spireAttestorReporter{agent: d}
	go wait.UntilWithContext(ctx, reporter.reconcile, spireAttestorInterval)
}

// spireAttestorReporter remembers what it last published so a failed patch is retried
// on the next tick. Deduplicating against the in-memory conditions instead would
// compare against the value the failed patch already wrote there.
type spireAttestorReporter struct {
	agent     *DPUAgent
	published *metav1.Condition
}

// reconcile makes one enablement attempt and publishes the outcome, patching only on
// change so a DPU waiting for certificates does not write status every interval.
func (r *spireAttestorReporter) reconcile(ctx context.Context) {
	condition := spireAttestorCondition(spireAttestorConfig(), spireAttestorRunner())
	if r.published != nil && !conditionChanged([]metav1.Condition{*r.published}, condition) {
		return
	}

	// updateStatus patches from this slice, so the condition is set before the call and
	// left in place: a later status update carries it even if this patch fails.
	meta.SetStatusCondition(&r.agent.optCtx.Status.Conditions, condition)
	if err := r.agent.updateStatus(ctx); err != nil {
		klog.Warningf("Failed to report the SPIRE workload attestor condition: %v", err)
		return
	}
	r.published = &condition
}

// spireAttestorCondition attempts enablement and describes the outcome.
func spireAttestorCondition(cfg attestor.Config, runner attestor.Runner) metav1.Condition {
	condType := provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled

	if err := attestor.Enable(cfg, runner); err != nil {
		// Waiting for kubelet is expected early on, so it gets its own reason.
		reason := "EnableFailed"
		if certErr := attestor.CertificatesReady(cfg, runner); certErr != nil {
			reason = "WaitingForKubeletCertificates"
		}
		return metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: err.Error(),
		}
	}

	return metav1.Condition{
		Type:    condType,
		Status:  metav1.ConditionTrue,
		Reason:  condType,
		Message: "SPIRE Kubernetes workload attestation is enabled",
	}
}

// conditionChanged reports whether setting next would change anything meaningful.
func conditionChanged(current []metav1.Condition, next metav1.Condition) bool {
	existing := meta.FindStatusCondition(current, next.Type)
	if existing == nil {
		return true
	}
	return existing.Status != next.Status ||
		existing.Reason != next.Reason ||
		existing.Message != next.Message
}
