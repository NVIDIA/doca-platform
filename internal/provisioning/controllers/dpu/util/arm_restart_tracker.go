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

package util

import (
	"encoding/json"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

const (
	// StaleTrackerTimeout defines when a tracker is considered stale (controller restart recovery).
	// Must be longer than (MaxSafetyLimit * MinRestartInterval = 10 * 90s = 15min) to allow
	// the full restart cycle to complete before declaring the tracker stale.
	StaleTrackerTimeout = 20 * time.Minute
)

// ArmRestartTracker tracks ARM restart operations across reconcile loops.
// Stored as JSON annotation on the DPU object.
type ArmRestartTracker struct {
	// Attempt is the current attempt count (1-based, 0 = not started)
	Attempt int `json:"attempt"`
	// MaxAttempts is set by caller (e.g., 2 for Secure Boot)
	MaxAttempts int `json:"maxAttempts"`
	// LastRestartTime is used for timeout/interval checks
	LastRestartTime time.Time `json:"lastRestartTime"`
	// InitialGeneration detects spec changes during flow
	InitialGeneration int64 `json:"initialGeneration"`
}

// IsStale returns true if the tracker is older than StaleTrackerTimeout
func (t *ArmRestartTracker) IsStale() bool {
	if t.LastRestartTime.IsZero() {
		return false // Not started yet
	}
	return time.Since(t.LastRestartTime) > StaleTrackerTimeout
}

// IncrementAttempt increments attempt counter and updates timestamp
func (t *ArmRestartTracker) IncrementAttempt() {
	t.Attempt++
	t.LastRestartTime = time.Now()
}

// AllRestartsDone returns true if all required restarts have been triggered
func (t *ArmRestartTracker) AllRestartsDone() bool {
	return t.Attempt >= t.MaxAttempts
}

// SaveArmRestartTracker serializes tracker to DPU annotation
func SaveArmRestartTracker(dpu *provisioningv1.DPU, tracker *ArmRestartTracker) error {
	if dpu.Annotations == nil {
		dpu.Annotations = make(map[string]string)
	}
	data, err := json.Marshal(tracker)
	if err != nil {
		return fmt.Errorf("failed to marshal ArmRestartTracker: %w", err)
	}
	dpu.Annotations[provisioningv1.AnnotationArmRestartTracker] = string(data)
	return nil
}

// LoadArmRestartTracker deserializes tracker from DPU annotation.
// Returns nil if annotation doesn't exist (not an error).
// Validates loaded data to guard against corrupted or tampered annotations.
func LoadArmRestartTracker(dpu *provisioningv1.DPU) (*ArmRestartTracker, error) {
	if dpu.Annotations == nil {
		return nil, nil
	}
	data, exists := dpu.Annotations[provisioningv1.AnnotationArmRestartTracker]
	if !exists {
		return nil, nil
	}
	var tracker ArmRestartTracker
	if err := json.Unmarshal([]byte(data), &tracker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ArmRestartTracker: %w", err)
	}
	// Validate loaded data (defense-in-depth against corrupted annotations)
	if tracker.MaxAttempts <= 0 {
		return nil, fmt.Errorf("invalid tracker: MaxAttempts must be positive, got %d", tracker.MaxAttempts)
	}
	if tracker.Attempt < 0 {
		return nil, fmt.Errorf("invalid tracker: Attempt cannot be negative, got %d", tracker.Attempt)
	}
	return &tracker, nil
}

// ClearArmRestartTracker removes the tracker annotation from DPU
func ClearArmRestartTracker(dpu *provisioningv1.DPU) {
	if dpu.Annotations != nil {
		delete(dpu.Annotations, provisioningv1.AnnotationArmRestartTracker)
	}
}
