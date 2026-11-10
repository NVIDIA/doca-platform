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
	"net/http"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"

	"k8s.io/klog/v2"
)

// ForceRestart and GracefulRestart are the ResetType values shared by the BlueField and CMX
// ComputerSystem.Reset handlers.
const (
	ForceRestart    = "ForceRestart"
	GracefulRestart = "GracefulRestart"
)

// handleSystemReset implements ComputerSystem.Reset (ForceRestart / GracefulRestart / PowerCycle of
// the Arm). Every accepted reset type restarts the DPU OS and therefore the agent.
func (s *Server) handleSystemReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ResetType string `json:"ResetType"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	if s.state.Personality().IsCMX() {
		s.handleCMXSystemReset(w, body.ResetType)
		return
	}
	switch body.ResetType {
	case ForceRestart, GracefulRestart, "PowerCycle", "ForceOff", "On":
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "Unsupported ResetType "+body.ResetType)
		return
	}
	klog.InfoS("Redfish ComputerSystem.Reset", "resetType", body.ResetType)
	s.state.ApplySecureBoot()
	s.sup.Reboot(s.ctx, provisioningv1.DPUPerformArmForceRestart)
	writeSuccess(w)
}

// handleCMXSystemReset implements ComputerSystem.Reset on the Vera BMC, which accepts every
// ResetType of its ResetActionInfo. ForceOff and GracefulShutdown power the host off. On and
// ForceOn power it on and do nothing when it already runs. The restart types boot it again.
// Every boot activates SBIOS and both CX9s; BMC, ERoT, SMA, MCU and CPLD stay staged until a Node AC
// cycle. CX9 firmware has no inactive slot, so only the running Version changes.
func (s *Server) handleCMXSystemReset(w http.ResponseWriter, resetType string) {
	switch resetType {
	case "ForceOff", "GracefulShutdown":
		klog.InfoS("Redfish ComputerSystem.Reset", "resetType", resetType)
		s.sup.PowerOff()
	case "On", "ForceOn":
		if powerState, _, _ := s.state.Power(); powerState == PowerStateOn {
			klog.InfoS("Redfish ComputerSystem.Reset: host already on", "resetType", resetType)
			break
		}
		klog.InfoS("Redfish ComputerSystem.Reset", "resetType", resetType)
		s.bootCMXHost()
	case ForceRestart, GracefulRestart, "PowerCycle":
		klog.InfoS("Redfish ComputerSystem.Reset", "resetType", resetType)
		s.bootCMXHost()
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "Unsupported ResetType "+resetType)
		return
	}
	writeSuccess(w)
}

// bootCMXHost boots the CMX host: the staged SBIOS and CX9 firmware takes effect. When a config
// image was inserted on the seed slot, the host boots the freshly installed OS from it and its agent
// takes the identity from that image, which ends the install. Otherwise the running OS and its agent
// restart.
func (s *Server) bootCMXHost() {
	s.state.ApplyHostResetFirmware()
	s.state.ApplySecureBoot()
	if iso := s.state.TakePendingSeedISO(); iso != nil {
		target, enabled := s.state.BootOverride()
		klog.InfoS("Redfish ComputerSystem.Reset: booting installed OS from virtual media", "seedISOBytes", len(iso), "bootSourceOverrideTarget", target, "bootSourceOverrideEnabled", enabled)
		s.sup.Boot(s.ctx, iso)
		return
	}
	s.sup.Reboot(s.ctx, provisioningv1.DPURebooting)
}

// handleSOCForceReset implements the hostless SOC.ForceReset action. CMX has no BlueField SoC.
func (s *Server) handleSOCForceReset(w http.ResponseWriter, r *http.Request) {
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	klog.InfoS("Redfish SOC.ForceReset")
	s.state.ApplySecureBoot()
	s.sup.Reboot(s.ctx, provisioningv1.DPURebooting)
	writeSuccess(w)
}

// handleChassisReset implements the BF4 NvidiaChassis.Reset action. ArmShutdown powers the Arm
// off. ArmReset either boots the freshly installed OS (a seed.iso was downloaded and not yet
// booted: the end of the BF4 install sequence) or reboots the running one.
func (s *Server) handleChassisReset(w http.ResponseWriter, r *http.Request) {
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	var body struct {
		ResetType string `json:"ResetType"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	switch body.ResetType {
	case "ArmShutdown":
		klog.InfoS("Redfish NvidiaChassis.Reset ArmShutdown")
		s.sup.PowerOff()
	case "ArmReset":
		s.state.ApplySecureBoot()
		if iso := s.state.TakePendingSeedISO(); iso != nil {
			klog.InfoS("Redfish NvidiaChassis.Reset ArmReset: booting installed OS", "seedISOBytes", len(iso))
			s.sup.Boot(s.ctx, iso)
		} else {
			klog.InfoS("Redfish NvidiaChassis.Reset ArmReset: rebooting")
			s.sup.Reboot(s.ctx, provisioningv1.DPUPerformArmForceRestart)
		}
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "Unsupported ResetType "+body.ResetType)
		return
	}
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}

// handleAuxPowerReset implements NvidiaChassis.AuxPowerReset on a CMX BMC chassis. AuxPowerCycle
// and AuxPowerCycleForce are the Node AC cycle that activates every staged firmware image.
func (s *Server) handleAuxPowerReset(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	if !p.IsCMX() || r.PathValue("chassis") != p.BMCChassisID {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	var body struct {
		ResetType string `json:"ResetType"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	switch body.ResetType {
	case "AuxPowerCycle", "AuxPowerCycleForce":
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "Unsupported ResetType "+body.ResetType)
		return
	}
	klog.InfoS("Redfish NvidiaChassis.AuxPowerReset", "resetType", body.ResetType)
	s.state.ApplyNodeACFirmware()
	s.state.ApplySecureBoot()
	s.sup.Reboot(s.ctx, provisioningv1.DPURebooting)
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}

// handleHostRshimSet implements HostRshim.Set. CMX has no BlueField rshim, so it answers 404 there.
func (s *Server) handleHostRshimSet(w http.ResponseWriter, r *http.Request) {
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeSuccess(w)
}

// handleBMCRShimOem serves the BF3 BmcRShim enable/poll resource. The enable takes effect at once.
func (s *Server) handleBMCRShimOem(w http.ResponseWriter, r *http.Request) {
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	if r.Method == http.MethodPatch {
		var body struct {
			BmcRShim struct {
				BmcRShimEnabled *bool `json:"BmcRShimEnabled"`
			} `json:"BmcRShim"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
			return
		}
		if body.BmcRShim.BmcRShimEnabled != nil {
			s.state.SetBMCRShimEnabled(*body.BmcRShim.BmcRShimEnabled)
		}
		writeSuccess(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id": r.URL.Path,
		"BmcRShim":  map[string]interface{}{"BmcRShimEnabled": s.state.BMCRShimEnabled()},
	})
}

func (s *Server) handleHostPrivilegeConfig(w http.ResponseWriter, r *http.Request) {
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	var body struct {
		PrivilegeMode string `json:"PrivilegeMode"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	if body.PrivilegeMode != "" {
		s.state.SetHostPrivilegeMode(body.PrivilegeMode)
	}
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}

// handleVirtualMediaCollection lists VirtualMedia members. BlueField accepts any slot id and does
// not publish a fixed collection.
func (s *Server) handleVirtualMediaCollection(w http.ResponseWriter, r *http.Request) {
	ids := s.state.Personality().virtualMediaIDs()
	members := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		members = append(members, odata(r.URL.Path+"/"+id))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           r.URL.Path,
		"@odata.type":         "#VirtualMediaCollection.VirtualMediaCollection",
		"Name":                "Virtual Media Services",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

func (s *Server) handleVirtualMediaGet(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	media := r.PathValue("media")
	if !p.virtualMediaExists(media) {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	actions := map[string]interface{}{
		"#VirtualMedia.EjectMedia": map[string]interface{}{"target": r.URL.Path + "/Actions/VirtualMedia.EjectMedia"},
	}
	if p.allowsVirtualMedia(media) {
		actions["#VirtualMedia.InsertMedia"] = map[string]interface{}{"target": r.URL.Path + "/Actions/VirtualMedia.InsertMedia"}
	}
	image := interface{}("")
	inserted := s.state.VirtualMediaInserted(media)
	connectedVia := "NotConnected"
	if p.IsCMX() && !inserted {
		image = nil
	} else if p.IsCMX() {
		image = s.state.VirtualMediaImage(media)
		connectedVia = "URI"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":      r.URL.Path,
		"@odata.type":    "#VirtualMedia.v1_4_0.VirtualMedia",
		"Id":             media,
		"Name":           "Virtual Removable Media",
		"Inserted":       inserted,
		"Image":          image,
		"MediaTypes":     []string{"CD", "USBStick"},
		"TransferMethod": "Stream",
		"WriteProtected": true,
		"ConnectedVia":   connectedVia,
		"Actions":        actions,
	})
}

// insertCMXMedia handles the body of InsertMedia on a CMX slot: the Image URI is required and
// recorded. The seed slot has its image downloaded and kept as the pending seed ISO, which the next
// boot of the host consumes the way a BF4 ArmReset consumes the SimpleUpdate seed. The OS ISO is
// not read, a real BMC only streams it to the host while it boots. It writes the error response and
// returns false when the insert must fail.
func (s *Server) insertCMXMedia(w http.ResponseWriter, r *http.Request, media string) bool {
	var body struct {
		Image string `json:"Image"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return false
	}
	if body.Image == "" {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyMissing", "The property Image is a required property and must be included in the request.")
		return false
	}
	if media == s.state.Personality().SeedMedia {
		buf := artifact.NewLimitedBuffer(artifact.MaxSeedISOSize)
		n, err := artifact.Download(r.Context(), body.Image, buf, nil)
		if err != nil {
			klog.ErrorS(err, "Redfish InsertMedia failed to read the config image", "media", media, "image", body.Image)
			writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterValueFormatError", "cannot read "+body.Image+": "+err.Error())
			return false
		}
		s.state.SetPendingSeedISO(buf.Bytes())
		klog.InfoS("Redfish InsertMedia: config image kept for the next boot", "media", media, "image", body.Image, "bytes", n)
	}
	s.state.SetVirtualMediaImage(media, body.Image)
	return true
}

func (s *Server) handleVirtualMediaAction(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	media := r.PathValue("media")
	if !p.virtualMediaExists(media) {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	switch r.PathValue("action") {
	case "VirtualMedia.InsertMedia":
		if !p.allowsVirtualMedia(media) {
			writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
			return
		}
		if p.IsCMX() && !s.insertCMXMedia(w, r, media) {
			return
		}
		s.state.SetVirtualMediaInserted(media, true)
	case "VirtualMedia.EjectMedia":
		s.state.SetVirtualMediaInserted(media, false)
		s.state.SetVirtualMediaImage(media, "")
		if media == p.SeedMedia {
			s.state.ClearPendingSeedISO()
		}
	default:
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}
