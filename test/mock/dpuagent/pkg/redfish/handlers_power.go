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

	"k8s.io/klog/v2"
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
	switch body.ResetType {
	case "ForceRestart", "GracefulRestart", "PowerCycle", "ForceOff", "On":
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "Unsupported ResetType "+body.ResetType)
		return
	}
	klog.InfoS("Redfish ComputerSystem.Reset", "resetType", body.ResetType)
	s.state.ApplySecureBoot()
	go s.sup.Reboot(s.ctx)
	writeSuccess(w)
}

// handleSOCForceReset implements the hostless SOC.ForceReset action.
func (s *Server) handleSOCForceReset(w http.ResponseWriter, _ *http.Request) {
	klog.InfoS("Redfish SOC.ForceReset")
	s.state.ApplySecureBoot()
	go s.sup.Reboot(s.ctx)
	writeSuccess(w)
}

// handleChassisReset implements the BF4 NvidiaChassis.Reset action. ArmShutdown powers the Arm
// off. ArmReset either boots the freshly installed OS (a seed.iso was downloaded and not yet
// booted: the end of the BF4 install sequence) or reboots the running one.
func (s *Server) handleChassisReset(w http.ResponseWriter, r *http.Request) {
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
		s.state.SetPowerOff()
	case "ArmReset":
		s.state.ApplySecureBoot()
		if iso := s.state.TakePendingSeedISO(); iso != nil {
			klog.InfoS("Redfish NvidiaChassis.Reset ArmReset: booting installed OS", "seedISOBytes", len(iso))
			go s.sup.Boot(s.ctx, iso)
		} else {
			klog.InfoS("Redfish NvidiaChassis.Reset ArmReset: rebooting")
			go s.sup.Reboot(s.ctx)
		}
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "Unsupported ResetType "+body.ResetType)
		return
	}
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}

func (s *Server) handleHostRshimSet(w http.ResponseWriter, _ *http.Request) {
	writeSuccess(w)
}

// handleBMCRShimOem serves the BF3 BmcRShim enable/poll resource. The enable takes effect at once.
func (s *Server) handleBMCRShimOem(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleVirtualMediaGet(w http.ResponseWriter, r *http.Request) {
	media := r.PathValue("media")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":  r.URL.Path,
		"Id":         media,
		"Inserted":   s.state.VirtualMediaInserted(media),
		"Image":      "",
		"MediaTypes": []string{"CD", "USBStick"},
	})
}

func (s *Server) handleVirtualMediaAction(w http.ResponseWriter, r *http.Request) {
	media := r.PathValue("media")
	switch r.PathValue("action") {
	case "VirtualMedia.InsertMedia":
		s.state.SetVirtualMediaInserted(media, true)
	case "VirtualMedia.EjectMedia":
		s.state.SetVirtualMediaInserted(media, false)
	default:
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}
