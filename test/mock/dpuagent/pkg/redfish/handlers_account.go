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

// Authentication is never checked and no password state is kept: every basic-auth login succeeds,
// so the controller's "factory reset landed" (factory password works) and "password initialized"
// (new password works) probes both pass at once.

func (s *Server) handleAccountPatch(w http.ResponseWriter, r *http.Request) {
	klog.V(2).InfoS("Redfish account password change accepted", "account", r.PathValue("user"))
	writeJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) handleEnableMTLS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) handleFactoryReset(w http.ResponseWriter, _ *http.Request) {
	klog.InfoS("Redfish Manager.ResetToDefaults accepted (no offline window is simulated)")
	writeJSON(w, http.StatusOK, map[string]interface{}{})
}

// handleResetBMC implements Manager.Reset; on BF3 it activates an uploaded BMC firmware.
func (s *Server) handleResetBMC(w http.ResponseWriter, r *http.Request) {
	klog.InfoS("Redfish Manager.Reset accepted")
	s.state.ResetBMC()
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}
