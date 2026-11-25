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

package reboot

import (
	"errors"
	"fmt"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statestore"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statusmanager"

	"k8s.io/utils/ptr"
)

const (
	// maxRebootRequests caps rebootState.Requests. A request over maxRebootSequenceCount is refused
	// and not recorded, so the trailing non-NoAction requests never exceed maxRebootSequenceCount
	// entries; keeping twice as many means they are never trimmed.
	maxRebootRequests = 2 * int(maxRebootSequenceCount)

	// stateFileName is the state file HandleReboot owns.
	stateFileName = "reboot"
)

// rebootState is what HandleReboot keeps in its state file across agent restarts and reboots.
type rebootState struct {
	// InitialBootID is the boot ID when this file was created or last reset by ResetState. The
	// legacy boot-ID path reboots exactly once after that and uses it to tell whether that reboot
	// happened.
	InitialBootID string `json:"initialBootID"`
	// Requests holds the conclusion of each boot, oldest first, at most maxRebootRequests entries.
	// Running again in the same boot (a retry or an agent restart) replaces that boot's entry.
	Requests []rebootRequest `json:"requests,omitempty"`
	// Pending holds the pending NVConfig observations of the last two different boots.
	Pending pendingNVConfigState `json:"pending"`
}

// rebootRequest is the reboot method HandleReboot concluded on one boot.
type rebootRequest struct {
	BootID string                          `json:"bootID"`
	Method provisioningv1.RebootMethodType `json:"method"`
	At     time.Time                       `json:"at"`
}

// record sets the conclusion of r.BootID: it replaces the last entry when that is the same boot
// and appends otherwise, dropping the oldest entries beyond maxRebootRequests.
func (s *rebootState) record(r rebootRequest) {
	if n := len(s.Requests); n > 0 && s.Requests[n-1].BootID == r.BootID {
		s.Requests[n-1] = r
	} else {
		s.Requests = append(s.Requests, r)
	}
	if len(s.Requests) > maxRebootRequests {
		s.Requests = s.Requests[len(s.Requests)-maxRebootRequests:]
	}
}

// sequenceCount returns the number of trailing requests whose method is not NoAction.
func (s rebootState) sequenceCount() int32 {
	var count int32
	for i := len(s.Requests) - 1; i >= 0 && s.Requests[i].Method != provisioningv1.RebootMethodNoAction; i-- {
		count++
	}
	return count
}

// pendingNVConfigState keeps two slots so that a discovery never compares against its own boot.
type pendingNVConfigState struct {
	Previous *pendingObservation `json:"previous,omitempty"`
	Latest   *pendingObservation `json:"latest,omitempty"`
}

// pendingObservation is the pending NVConfig parameters every device reported on one boot.
type pendingObservation struct {
	BootID     string          `json:"bootID"`
	ObservedAt time.Time       `json:"observedAt"`
	Devices    []pendingDevice `json:"devices"`
}

type pendingDevice struct {
	Device  string         `json:"device"`
	Entries []pendingEntry `json:"entries"`
}

// pendingEntry is one entry of mlxfwreset pending_nvconfig_parameters. The JSON tags also parse
// the mlxfwreset output.
type pendingEntry struct {
	Name     string `json:"name,omitempty"`
	Default  string `json:"default,omitempty"`
	Current  string `json:"current,omitempty"`
	NextBoot string `json:"next_boot,omitempty"`
}

// fromPreviousBoot returns the newest observation made on a boot other than currentBootID. It
// never returns an observation of the current boot, so a discovery repeated on the same boot
// compares against the same baseline as the first one.
func (p pendingNVConfigState) fromPreviousBoot(currentBootID string) *pendingObservation {
	if p.Latest == nil {
		return nil
	}
	if p.Latest.BootID != currentBootID {
		return p.Latest
	}
	return p.Previous
}

// commit stores observed. A new boot moves Latest to Previous; the same boot only replaces Latest.
func (p pendingNVConfigState) commit(observed pendingObservation) pendingNVConfigState {
	if p.Latest != nil && p.Latest.BootID != observed.BootID {
		return pendingNVConfigState{Previous: p.Latest, Latest: &observed}
	}
	return pendingNVConfigState{Previous: p.Previous, Latest: &observed}
}

// loadState reads the state file. Without a file it starts a new state that records the
// current boot as the initial boot.
func loadState(store *statestore.Store, currentBootID string) (rebootState, error) {
	var state rebootState
	err := store.Load(stateFileName, &state)
	if errors.Is(err, os.ErrNotExist) {
		return rebootState{InitialBootID: currentBootID}, nil
	}
	if err != nil {
		return rebootState{}, fmt.Errorf("load reboot state: %w", err)
	}
	return state, nil
}

func saveState(store *statestore.Store, state rebootState) error {
	if err := store.Save(stateFileName, state); err != nil {
		return fmt.Errorf("save reboot state: %w", err)
	}
	return nil
}

// ResetState starts the reboot bookkeeping over for an in-place reconfig with an updated
// DPUFlavor: currentBootID becomes the initial boot and the sequence count restarts from zero.
// The pending NVConfig observations are kept: they are the baseline that tells a parameter that
// stays pending across boots from one that needs a reboot, and without them a reconfig that
// changes no NVConfig would reboot the DPU for the stuck parameters.
func ResetState(store *statestore.Store, currentBootID string) error {
	var state rebootState
	err := store.Load(stateFileName, &state)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reset reboot state: %w", err)
	}
	reset := rebootState{
		InitialBootID: currentBootID,
		Pending:       state.Pending,
	}
	if err := store.Save(stateFileName, reset); err != nil {
		return fmt.Errorf("reset reboot state: %w", err)
	}
	return nil
}

// SequenceCountSource reports agentStatus.rebootSequenceCount from the state file on every
// status push. The field stays unset until HandleReboot records a conclusion: the host agent tells
// an unset count from 0.
func SequenceCountSource(store *statestore.Store) statusmanager.Source {
	return func(s *provisioningv1.AgentStatus) error {
		var state rebootState
		if err := store.Load(stateFileName, &state); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("load reboot state: %w", err)
		}
		if len(state.Requests) == 0 {
			return nil
		}
		s.RebootSequenceCount = ptr.To(state.sequenceCount())
		return nil
	}
}
