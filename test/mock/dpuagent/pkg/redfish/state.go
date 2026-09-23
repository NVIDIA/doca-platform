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

package redfish

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
)

// Redfish task states and Arm boot progress values the controller reads.
const (
	TaskStateRunning   = "Running"
	TaskStateCompleted = "Completed"
	TaskStateException = "Exception"

	PowerStateOn       = "On"
	OemLastStateOSUp   = "OsIsRunning"
	OemLastStateOSBoot = "OsStarting"
	OemLastStateOff    = "SystemHardwareInitializationComplete"
	statusStateEnabled = "Enabled"
	statusStateOffline = "StandbyOffline"
)

// Task is a Redfish TaskService task.
type Task struct {
	ID       string
	Name     string
	State    string
	Percent  int
	Messages []map[string]interface{}
}

// Options configure the BMC state.
type Options struct {
	Personality  Personality
	SerialNumber string
	PSID         string
	PF0MAC       string
	Firmware     config.Firmware
}

// State is everything the Redfish handlers read and write. It is safe for concurrent use.
type State struct {
	mu sync.RWMutex

	p      Personality
	serial string
	psid   string
	mac    string

	firmware        map[string]string
	pendingBundle   *artifact.PLDMVersions
	activatedBundle *artifact.PLDMVersions
	bmcFWUploaded   bool

	tasks    map[string]*Task
	nextTask int

	powerState   string
	oemLastState string

	dpuMode           string
	hostPrivilegeMode string
	secureBootEnable  bool
	secureBootCurrent bool
	bmcRShimEnabled   bool
	bootTarget        string
	bootEnabled       string
	virtualMedia      map[string]bool

	truststore map[int]string
	nextTrust  int

	pendingSeedISO []byte
}

// NewState returns the power-on state of a healthy DPU in DpuMode with the configured firmware.
func NewState(opts Options) *State {
	p := opts.Personality
	s := &State{
		p:                 p,
		serial:            opts.SerialNumber,
		psid:              opts.PSID,
		mac:               opts.PF0MAC,
		firmware:          map[string]string{},
		tasks:             map[string]*Task{},
		nextTask:          1,
		powerState:        PowerStateOn,
		oemLastState:      OemLastStateOSUp,
		dpuMode:           string(rfclient.DpuMode),
		hostPrivilegeMode: "Privileged",
		secureBootEnable:  false,
		secureBootCurrent: false,
		bmcRShimEnabled:   false,
		bootTarget:        "None",
		bootEnabled:       "Disabled",
		virtualMedia:      map[string]bool{},
		truststore:        map[int]string{},
		nextTrust:         1,
	}
	s.firmware[p.InvBMC] = opts.Firmware.BMC
	s.firmware[p.InvUEFI] = opts.Firmware.UEFI
	s.firmware[p.InvNIC] = opts.Firmware.NIC
	if p.IsBF4() {
		s.firmware[p.InvERoT] = opts.Firmware.ERoT
		s.firmware[p.InvOSImage] = config.PlaceholderVersion
		s.firmware[p.InvOSConfig] = config.PlaceholderVersion
		s.firmware[p.InvPendingBundle] = ""
	} else {
		s.firmware[p.InvBSP] = config.PlaceholderVersion
		s.firmware[p.InvOS] = config.PlaceholderVersion
		s.firmware[p.InvBoard] = opts.PSID
	}
	return s
}

// Personality returns the DPU generation constants.
func (s *State) Personality() Personality { return s.p }

// SerialNumber returns the chassis serial number.
func (s *State) SerialNumber() string { return s.serial }

// PSID returns the board PSID.
func (s *State) PSID() string { return s.psid }

// PF0MAC returns the PF0 MAC address.
func (s *State) PF0MAC() string { return s.mac }

// Power reports the ComputerSystem PowerState, Status.State and BootProgress.OemLastState.
func (s *State) Power() (powerState, statusState, oemLastState string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	statusState = statusStateEnabled
	if s.powerState != PowerStateOn {
		statusState = statusStateOffline
	}
	return s.powerState, statusState, s.oemLastState
}

// SetPowerOn reports a running Arm with the OS up.
func (s *State) SetPowerOn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.powerState = PowerStateOn
	s.oemLastState = OemLastStateOSUp
}

// SetOSStarting reports a powered Arm whose OS is still booting.
func (s *State) SetOSStarting() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.powerState = PowerStateOn
	s.oemLastState = OemLastStateOSBoot
}

// SetPowerOff reports a shut down Arm (Off on BF3, Paused on BF4), which releases the controller's
// wait-for-shutdown gate before an external host reboot.
func (s *State) SetPowerOff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.powerState = s.p.PowerOffState
	s.oemLastState = OemLastStateOff
}

// FirmwareVersion returns the version of a firmware inventory member.
func (s *State) FirmwareVersion(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.firmware[id]
	return v, ok
}

// SetFirmwareVersion sets the version of a firmware inventory member.
func (s *State) SetFirmwareVersion(id, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firmware[id] = version
}

// SetInstalledBFBVersions reports the UEFI, BSP and OS versions parsed from an installed BFB.
func (s *State) SetInstalledBFBVersions(v *provisioningv1.BFBVersions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p.InvUEFI != "" {
		s.firmware[s.p.InvUEFI] = v.UEFI
	}
	if s.p.InvBSP != "" {
		s.firmware[s.p.InvBSP] = v.BSP
	}
	if s.p.InvOS != "" {
		s.firmware[s.p.InvOS] = v.DOCA
	}
}

// SetPendingBundle records the versions of an uploaded PLDM bundle that has not been activated.
func (s *State) SetPendingBundle(v artifact.PLDMVersions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := v
	s.pendingBundle = &pending
	s.firmware[s.p.InvPendingBundle] = v.BMC
}

// ActivatePendingBundle marks the uploaded bundle for installation on the next reboot.
func (s *State) ActivatePendingBundle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingBundle == nil {
		return false
	}
	s.activatedBundle = s.pendingBundle
	s.pendingBundle = nil
	return true
}

// ApplyActivatedBundle installs an activated bundle: the four BF4 inventory members take the
// bundle versions. Called when the host reboots. Returns whether anything changed.
func (s *State) ApplyActivatedBundle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activatedBundle == nil {
		return false
	}
	v := s.activatedBundle
	s.firmware[s.p.InvBMC] = v.BMC
	s.firmware[s.p.InvERoT] = v.ERoT
	s.firmware[s.p.InvUEFI] = v.SBIOS
	s.firmware[s.p.InvNIC] = v.NIC
	s.firmware[s.p.InvPendingBundle] = ""
	s.activatedBundle = nil
	return true
}

// MarkBMCFirmwareUploaded records a BF3 BMC firmware push; Manager.Reset installs it.
func (s *State) MarkBMCFirmwareUploaded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bmcFWUploaded = true
}

// ResetBMC emulates Manager.Reset: an uploaded BF3 BMC firmware becomes the running one, at the
// minimum version the DPUDevice controller requires.
func (s *State) ResetBMC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bmcFWUploaded {
		s.firmware[s.p.InvBMC] = config.BMCMinSupportedVersion
		s.bmcFWUploaded = false
	}
}

// NewTask registers a running task and returns it.
func (s *State) NewTask(name string) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &Task{ID: strconv.Itoa(s.nextTask), Name: name, State: TaskStateRunning}
	s.nextTask++
	s.tasks[t.ID] = t
	return t
}

// Task returns a copy of a task.
func (s *State) Task(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	return *t, true
}

// SetTaskProgress updates PercentComplete of a running task.
func (s *State) SetTaskProgress(id string, percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok && t.State == TaskStateRunning {
		t.Percent = percent
	}
}

// CompleteTask marks a task Completed at 100%.
func (s *State) CompleteTask(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.State = TaskStateCompleted
		t.Percent = 100
	}
}

// FailTask marks a task Exception with a Redfish message the controller surfaces to the user.
func (s *State) FailTask(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.State = TaskStateException
		t.Messages = []map[string]interface{}{{
			"@odata.type":     "#Message.v1_1_1.Message",
			"Message":         fmt.Sprintf("mock-dpuagent: %v", err),
			"MessageId":       "Update.1.0.TransferFailed",
			"MessageSeverity": "Critical",
			"Resolution":      "Check the ImageURI and the bfb-registry.",
		}}
	}
}

// DPUMode returns the NIC/DPU mode.
func (s *State) DPUMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dpuMode
}

// SetDPUMode records a Mode.Set request.
func (s *State) SetDPUMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dpuMode = mode
}

// HostPrivilegeMode returns the BF4 host privilege mode.
func (s *State) HostPrivilegeMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hostPrivilegeMode
}

// SetHostPrivilegeMode records a HostPrivilegeConfig PATCH.
func (s *State) SetHostPrivilegeMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostPrivilegeMode = mode
}

// SecureBoot returns the configured and the current-boot Secure Boot state.
func (s *State) SecureBoot() (enable, current bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secureBootEnable, s.secureBootCurrent
}

// SetSecureBootEnable stages the Secure Boot setting for the next boot.
func (s *State) SetSecureBootEnable(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secureBootEnable = enable
}

// ApplySecureBoot makes the staged Secure Boot setting the current one (called on Arm restart).
func (s *State) ApplySecureBoot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secureBootCurrent = s.secureBootEnable
}

// BMCRShimEnabled reports the BF3 BMC rshim state.
func (s *State) BMCRShimEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bmcRShimEnabled
}

// SetBMCRShimEnabled records the BF3 BMC rshim enable PATCH.
func (s *State) SetBMCRShimEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bmcRShimEnabled = enabled
}

// BootOverride returns BootSourceOverrideTarget and BootSourceOverrideEnabled.
func (s *State) BootOverride() (target, enabled string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootTarget, s.bootEnabled
}

// SetBootOverride records a Systems/{id}/Settings PATCH; empty values are left unchanged.
func (s *State) SetBootOverride(target, enabled string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if target != "" {
		s.bootTarget = target
	}
	if enabled != "" {
		s.bootEnabled = enabled
	}
}

// VirtualMediaInserted reports whether a virtual media slot has media inserted.
func (s *State) VirtualMediaInserted(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.virtualMedia[id]
}

// SetVirtualMediaInserted records an InsertMedia/EjectMedia action.
func (s *State) SetVirtualMediaInserted(id string, inserted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.virtualMedia[id] = inserted
}

// AddTruststoreCert stores a CA certificate and returns its member index.
func (s *State) AddTruststoreCert(pem string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextTrust
	s.nextTrust++
	s.truststore[id] = pem
	return id
}

// TruststoreCert returns a stored CA certificate.
func (s *State) TruststoreCert(id int) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pem, ok := s.truststore[id]
	return pem, ok
}

// ReplaceTruststoreCert overwrites a stored CA certificate, creating the slot if needed.
func (s *State) ReplaceTruststoreCert(id int, pem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.truststore[id] = pem
	if id >= s.nextTrust {
		s.nextTrust = id + 1
	}
}

// DeleteTruststoreCert removes a stored CA certificate.
func (s *State) DeleteTruststoreCert(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.truststore[id]; !ok {
		return false
	}
	delete(s.truststore, id)
	return true
}

// TruststoreIDs lists stored CA certificate indexes in ascending order.
func (s *State) TruststoreIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]int, 0, len(s.truststore))
	for id := range s.truststore {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// SetPendingSeedISO keeps the downloaded BF4 config image until the Arm is reset.
func (s *State) SetPendingSeedISO(iso []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingSeedISO = iso
}

// TakePendingSeedISO returns and clears the downloaded BF4 config image.
func (s *State) TakePendingSeedISO() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	iso := s.pendingSeedISO
	s.pendingSeedISO = nil
	return iso
}
