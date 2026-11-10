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
	"time"

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
	// inactive is the staged inactive firmware slot of a dual-slot CMX member (BMC, ERoT, SBIOS, SMA).
	// CX9, MCU, CPLD and drive firmware have no slot; those staged versions live in staged.
	// A host reset promotes SBIOS and the CX9s. A Node AC cycle promotes every staged member.
	// There is no UpdateService.Activate. CommitImage only marks an inactive slot pending.
	inactive  map[string]string
	staged    map[string]string
	committed map[string]bool

	mediaImage map[string]string
	truststore map[int]string
	nextTrust  int

	pendingSeedISO []byte

	lastResetTime time.Time // Manager LastResetTime, moved by Manager.Reset
	bmcDownUntil  time.Time // The Manager resource answers 503 until then, like a rebooting BMC
}

// bmcRebootWindow is how long the Manager resource stays unreachable after Manager.Reset on BF4.
const bmcRebootWindow = 20 * time.Second

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
		mediaImage:        map[string]string{},
		truststore:        map[int]string{},
		nextTrust:         1,
		lastResetTime:     time.Now().UTC(),
	}
	s.firmware[p.InvBMC] = opts.Firmware.BMC
	s.firmware[p.InvUEFI] = opts.Firmware.UEFI
	s.firmware[p.InvNIC] = opts.Firmware.NIC
	for _, nic := range p.NICs {
		if nic.FirmwareID != "" {
			s.firmware[nic.FirmwareID] = opts.Firmware.NIC
		}
	}
	switch {
	case p.IsBF4():
		s.firmware[p.InvERoT] = opts.Firmware.ERoT
		s.firmware[p.InvOSImage] = config.PlaceholderVersion
		s.firmware[p.InvOSConfig] = config.PlaceholderVersion
		s.firmware[p.InvPendingBundle] = ""
	case p.IsCMX():
		s.firmware[p.InvERoT] = opts.Firmware.ERoT
		for _, member := range p.Firmware {
			if _, ok := s.firmware[member.ID]; !ok {
				s.firmware[member.ID] = config.PlaceholderVersion
			}
		}
		s.inactive = map[string]string{}
		s.staged = map[string]string{}
		s.committed = map[string]bool{}
	default:
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
// On BF4-CMX, BMC, ERoT and SBIOS go into InactiveFirmwareSlot. Both CX9s take the bundle's single
// NIC version in staged state, because FW_CX_n has no inactive slot. There is no Pending_Bundle
// and no UpdateService.Activate.
func (s *State) SetPendingBundle(v artifact.PLDMVersions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p.IsCMX() {
		s.stageInactiveLocked(v, nil)
		return
	}
	pending := v
	s.pendingBundle = &pending
	s.firmware[s.p.InvPendingBundle] = v.BMC
}

// SetPendingBundleTargets stages a PLDM bundle on BF4-CMX for the inventory members in only, as a
// multipart update with Targets does. A nil only stages every member the bundle carries.
func (s *State) SetPendingBundleTargets(v artifact.PLDMVersions, only map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stageInactiveLocked(v, only)
}

// bundleCarries reports whether a CMX PLDM bundle has a version for inventory member id. These are
// the members stageInactiveLocked records.
func (p Personality) bundleCarries(v artifact.PLDMVersions, id string) bool {
	switch id {
	case p.InvBMC:
		return v.BMC != ""
	case p.InvERoT:
		return v.ERoT != ""
	case p.InvUEFI:
		return v.SBIOS != ""
	}
	if _, ok := p.nicSlot(id); ok {
		return v.NIC != ""
	}
	return len(p.NICs) == 0 && id == p.InvNIC && v.NIC != ""
}

// stageInactiveLocked records a PLDM bundle. Dual-slot members get an inactive slot, and a
// replacement clears the previous commit so the new image is not pending until CommitImage. Other
// members get a staged version that is not visible as InactiveFirmwareSlot. Members missing from a
// non-nil only are left alone. The caller holds s.mu.
func (s *State) stageInactiveLocked(v artifact.PLDMVersions, only map[string]struct{}) {
	if s.inactive == nil {
		s.inactive = map[string]string{}
	}
	if s.staged == nil {
		s.staged = map[string]string{}
	}
	set := func(id, version string) {
		if id == "" || version == "" {
			return
		}
		if only != nil {
			if _, ok := only[id]; !ok {
				return
			}
		}
		if member, ok := s.p.firmwareMember(id); ok && member.DualSlot {
			s.inactive[id] = version
			delete(s.committed, id)
			return
		}
		s.staged[id] = version
	}
	set(s.p.InvBMC, v.BMC)
	set(s.p.InvERoT, v.ERoT)
	set(s.p.InvUEFI, v.SBIOS)
	for _, nic := range s.p.NICs {
		set(nic.FirmwareID, v.NIC)
	}
	if len(s.p.NICs) == 0 {
		set(s.p.InvNIC, v.NIC)
	}
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
// bundle versions. On BF4-CMX a host reboot promotes only SBIOS and the CX9s. BMC, ERoT and the
// other board firmware stay staged until a Node AC cycle. Called when the host reboots. Returns
// whether anything changed.
func (s *State) ApplyActivatedBundle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p.IsCMX() {
		return s.promoteLocked(s.p.hostResetFirmwareIDs(), false)
	}
	changed := false
	if s.activatedBundle != nil {
		v := s.activatedBundle
		s.firmware[s.p.InvBMC] = v.BMC
		s.firmware[s.p.InvERoT] = v.ERoT
		s.firmware[s.p.InvUEFI] = v.SBIOS
		s.firmware[s.p.InvNIC] = v.NIC
		s.firmware[s.p.InvPendingBundle] = ""
		s.activatedBundle = nil
		changed = true
	}
	return changed
}

// ApplyHostResetFirmware promotes the CMX firmware a ComputerSystem.Reset activates.
func (s *State) ApplyHostResetFirmware() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.promoteLocked(s.p.hostResetFirmwareIDs(), false)
}

// ApplyNodeACFirmware promotes every staged CMX firmware image. That is NvidiaChassis.AuxPowerReset.
func (s *State) ApplyNodeACFirmware() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.promoteLocked(nil, true)
}

// CommitStagedImage marks inactive slots pending activation. It does not change the running version.
func (s *State) CommitStagedImage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.committed == nil {
		s.committed = map[string]bool{}
	}
	for id := range s.inactive {
		s.committed[id] = true
	}
}

// promoteLocked copies staged versions into the running inventory. all promotes every member.
// Otherwise only ids are promoted. Promoted members leave the staged slot. The caller holds s.mu.
func (s *State) promoteLocked(ids []string, all bool) bool {
	allow := map[string]struct{}{}
	for _, id := range ids {
		allow[id] = struct{}{}
	}
	want := func(id string) bool {
		_, ok := allow[id]
		return all || ok
	}
	changed := false
	for id, version := range s.inactive {
		if !want(id) {
			continue
		}
		s.firmware[id] = version
		delete(s.inactive, id)
		delete(s.committed, id)
		changed = true
	}
	for id, version := range s.staged {
		if !want(id) {
			continue
		}
		s.firmware[id] = version
		delete(s.staged, id)
		changed = true
	}
	return changed
}

// InactiveSlot returns the staged inactive-slot version and FirmwareState of a dual-slot member.
func (s *State) InactiveSlot(id string) (version, state string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	version, ok = s.inactive[id]
	if !ok || version == "" {
		return "", "", false
	}
	state = "Inactive"
	if s.committed[id] {
		state = "PendingActivation"
	}
	return version, state, true
}

// FirmwareIDs lists firmware inventory member ids in ascending order.
func (s *State) FirmwareIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.firmware))
	for id := range s.firmware {
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// MarkBMCFirmwareUploaded records a BF3 BMC firmware push; Manager.Reset installs it.
func (s *State) MarkBMCFirmwareUploaded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bmcFWUploaded = true
}

// ResetBMC emulates Manager.Reset: an uploaded BF3 BMC firmware becomes the running one, at the
// minimum version the DPUDevice controller requires, and LastResetTime moves to now. On BF4 the
// Manager resource is also unreachable for bmcRebootWindow, like a BMC that is rebooting; the
// BF3 DPUDevice flow reads the Manager right after its own reset and keeps its current behavior.
func (s *State) ResetBMC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bmcFWUploaded {
		s.firmware[s.p.InvBMC] = config.BMCMinSupportedVersion
		s.bmcFWUploaded = false
	}
	now := time.Now().UTC()
	s.lastResetTime = now
	if s.p.IsBF4() {
		s.bmcDownUntil = now.Add(bmcRebootWindow)
	}
}

// LastResetTime returns the Manager LastResetTime.
func (s *State) LastResetTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastResetTime
}

// BMCDown reports whether the Manager resource is still in its post-reset reboot window.
func (s *State) BMCDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Now().Before(s.bmcDownUntil)
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

// VirtualMediaImage returns the Image URI of the media inserted in a virtual media slot, empty when
// none was recorded.
func (s *State) VirtualMediaImage(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mediaImage[id]
}

// SetVirtualMediaImage records the Image URI of an InsertMedia action. An empty image forgets it.
func (s *State) SetVirtualMediaImage(id, image string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if image == "" {
		delete(s.mediaImage, id)
		return
	}
	s.mediaImage[id] = image
}

// TruststoreCapacity is how many certificates the truststore holds, as the BMC does: a BlueField
// BMC rejects the install of an eleventh rather than making room for it.
const TruststoreCapacity = 10

// AddTruststoreCert stores a CA certificate and returns its member index. The second return is
// false when the truststore is full, which is the BMC's answer too.
func (s *State) AddTruststoreCert(pem string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.truststore) >= TruststoreCapacity {
		return 0, false
	}
	id := s.nextTrust
	s.nextTrust++
	s.truststore[id] = pem
	return id, true
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

// ClearPendingSeedISO drops a downloaded config image that the caller ejected.
func (s *State) ClearPendingSeedISO() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingSeedISO = nil
}

// TakePendingSeedISO returns and clears the downloaded BF4 config image.
func (s *State) TakePendingSeedISO() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	iso := s.pendingSeedISO
	s.pendingSeedISO = nil
	return iso
}
