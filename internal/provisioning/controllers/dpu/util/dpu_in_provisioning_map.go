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

package util

import (
	"context"
	"fmt"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DPUID is a type alias for string that represents a DPU's unique identifier
type DPUID string

// DPUInProvisioningMap tracks the number of DPUs in provisioning states
// This map is used to limit the number of DPUs that can be in provisioning at once.
type DPUInProvisioningMap struct {
	mu sync.Mutex
	// dpus is a map of DPU UIDs that are currently in provisioning states
	dpus map[DPUID]struct{}
	// max is the maximum number of DPUs that can be in provisioning at once
	max int32
}

// NewDPUInProvisioningMap creates a new map with the specified maximum value
func NewDPUInProvisioningMap(max int32) *DPUInProvisioningMap {
	return &DPUInProvisioningMap{
		dpus: make(map[DPUID]struct{}),
		max:  max,
	}
}

// Initialize counts current DPUs in provisioning state
func (c *DPUInProvisioningMap) Initialize(ctx context.Context, client client.Client) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dpuList := &provisioningv1.DPUList{}
	if err := client.List(ctx, dpuList); err != nil {
		return fmt.Errorf("failed to list DPUs: %w", err)
	}

	// Clear existing map
	c.dpus = make(map[DPUID]struct{})

	// Add current DPUs in provisioning state
	for _, dpu := range dpuList.Items {
		if cutil.IsDPUInProvisioningPhase(dpu.Status.Phase) {
			c.dpus[DPUID(dpu.UID)] = struct{}{}
		}
	}

	return nil
}

// CanProceed checks if a new DPU can enter provisioning and inserts if possible
func (c *DPUInProvisioningMap) CanProceed(dpuUID DPUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If DPU is already in the map, allow it to proceed
	if _, exists := c.dpus[dpuUID]; exists {
		return true
	}

	// Otherwise check capacity and insert if possible
	if int32(len(c.dpus)) >= c.max {
		return false
	}
	c.insert(dpuUID)
	return true
}

// insert is a helper function that should only be called from CanProceed which holds the mutex lock
func (c *DPUInProvisioningMap) insert(dpuUID DPUID) {
	c.dpus[dpuUID] = struct{}{}
}

// Remove removes a DPU from the map
func (c *DPUInProvisioningMap) Remove(dpuUID DPUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.dpus, dpuUID)
}

// GetMax returns max allowed
func (c *DPUInProvisioningMap) GetMax() int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max
}
