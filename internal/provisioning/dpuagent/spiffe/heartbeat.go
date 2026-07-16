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

// Package spiffe implements the DPU Agent SPIFFE heartbeat writer.
package spiffe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpuheartbeat"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	probeInterval      = 30 * time.Second
	maxProbeMessageLen = 256
)

// Config holds the heartbeat writer dependencies.
type Config struct {
	Client       crclient.Client
	DPUName      string
	DPUNamespace string
	DPUUID       string
}

// Run probes the management-cluster API and SSA-patches status.agentStatus.spiffe until ctx is canceled.
func Run(ctx context.Context, cfg Config) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()

	for {
		cfg.probeOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (cfg Config) probeOnce(ctx context.Context) {
	// On shutdown the context is canceled; skip the probe so a canceled-context Get failure
	// doesn't write a misleading failure status as the agent stops.
	if ctx.Err() != nil {
		return
	}
	dpu := &provisioningv1.DPU{}
	key := types.NamespacedName{Namespace: cfg.DPUNamespace, Name: cfg.DPUName}
	if err := cfg.Client.Get(ctx, key, dpu); err != nil {
		// If the context was canceled mid-Get (shutdown), don't stamp a spurious failure.
		if ctx.Err() != nil {
			return
		}
		klog.ErrorS(err, "SPIFFE heartbeat probe failed to get DPU")
		cfg.applyFailure(ctx, fmt.Sprintf("get DPU: %v", err))
		return
	}
	// UID guard against delete+recreate: cfg.DPUUID is captured at process start, so a replaced
	// object's UID never matches and this agent stops stamping it. No resourceVersion precondition
	// on the Get->Patch window on purpose: LastProbeTime is observability-only and self-corrects,
	// and an object-wide precondition would conflict with unrelated SSA writes.
	if string(dpu.UID) != cfg.DPUUID {
		klog.Errorf("SPIFFE heartbeat: stale DPU object, expected UID %s but got %s; skipping status update", cfg.DPUUID, dpu.UID)
		return
	}

	if err := cfg.applySuccess(ctx); err != nil {
		klog.ErrorS(err, "SPIFFE heartbeat status apply failed")
	}
}

func (cfg Config) applySuccess(ctx context.Context) error {
	now := metav1.Now()
	// Apply only the spiffe subtree as unstructured: a typed DPU apply serializes a zero-value
	// status.phase and null conditions (not omitempty), which the apiserver rejects.
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(provisioningv1.GroupVersion.WithKind(provisioningv1.DPUKind))
	u.SetName(cfg.DPUName)
	u.SetNamespace(cfg.DPUNamespace)
	if err := unstructured.SetNestedMap(u.Object, map[string]any{
		"lastProbeTime":    now.UTC().Format(time.RFC3339),
		"lastProbeMessage": "",
	}, "status", "agentStatus", "spiffe"); err != nil {
		return fmt.Errorf("building spiffe heartbeat status: %w", err)
	}
	return cfg.Client.Status().Patch(ctx, u, crclient.Apply, crclient.FieldOwner(dpuheartbeat.HeartbeatFieldManager), crclient.ForceOwnership)
}

func (cfg Config) applyFailure(ctx context.Context, message string) {
	msg := truncateProbeMessage(message)
	// Merge-patch (not SSA Apply) so lastProbeTime is preserved: an apply omitting it would
	// prune it, flipping Freshness to NeverAttested after a success->failure.
	body, err := json.Marshal(map[string]any{
		"status": map[string]any{
			"agentStatus": map[string]any{
				"spiffe": map[string]any{"lastProbeMessage": msg},
			},
		},
	})
	if err != nil {
		klog.ErrorS(err, "SPIFFE heartbeat: marshaling failure patch", "message", msg)
		return
	}
	obj := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: cfg.DPUName, Namespace: cfg.DPUNamespace}}
	obj.SetGroupVersionKind(provisioningv1.GroupVersion.WithKind(provisioningv1.DPUKind))
	if err := cfg.Client.Status().Patch(ctx, obj, crclient.RawPatch(types.MergePatchType, body)); err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			klog.V(2).ErrorS(err, "SPIFFE heartbeat failure status apply denied", "message", msg)
			return
		}
		klog.ErrorS(err, "SPIFFE heartbeat failure status apply failed", "message", msg)
	}
}

func truncateProbeMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= maxProbeMessageLen {
		return message
	}
	return message[:maxProbeMessageLen]
}
