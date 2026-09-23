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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"

	"k8s.io/klog/v2"
)

func taskResponse(t Task) map[string]interface{} {
	body := map[string]interface{}{
		"@odata.context":  "/redfish/v1/$metadata#Task.Task",
		"@odata.id":       "/redfish/v1/TaskService/Tasks/" + t.ID,
		"@odata.type":     "#Task.v1_10_0.Task",
		"Id":              t.ID,
		"Name":            t.Name,
		"TaskState":       t.State,
		"TaskStatus":      "OK",
		"PercentComplete": t.Percent,
	}
	if t.State == TaskStateException {
		body["TaskStatus"] = "Critical"
		body["Messages"] = t.Messages
	}
	return body
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.state.Task(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, taskResponse(t))
}

func (s *Server) handleUpdateServiceGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":            "/redfish/v1/UpdateService",
		"@odata.type":          "#UpdateService.v1_10_0.UpdateService",
		"Id":                   "UpdateService",
		"Name":                 "Update Service",
		"HttpPushUri":          "/redfish/v1/UpdateService",
		"MultipartHttpPushUri": "/redfish/v1/UpdateService/update-multipart",
		"FirmwareInventory":    odata("/redfish/v1/UpdateService/FirmwareInventory"),
	})
}

// handleBMCFirmwarePush is the BF3 BMC firmware HttpPushUri upload. The controller only requires the
// version to be at least the minimum after Manager.Reset, so the payload is drained and dropped.
func (s *Server) handleBMCFirmwarePush(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", "failed to read firmware payload: "+err.Error())
		return
	}
	task := s.state.NewTask("BMC firmware update")
	s.state.CompleteTask(task.ID)
	s.state.MarkBMCFirmwareUploaded()
	klog.InfoS("Redfish BMC firmware upload accepted", "bytes", n, "task", task.ID)
	writeJSON(w, http.StatusAccepted, taskResponse(mustTask(s.state, task.ID)))
}

func mustTask(st *State, id string) Task {
	t, _ := st.Task(id)
	return t
}

func (s *Server) handleFirmwareInventory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	version, ok := s.state.FirmwareVersion(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":   r.URL.Path,
		"@odata.type": "#SoftwareInventory.v1_4_0.SoftwareInventory",
		"Id":          id,
		"Name":        id,
		"Version":     version,
		"Updateable":  true,
		"Status":      map[string]interface{}{"State": "Enabled", "Health": "OK"},
	})
}

// handleSimpleUpdate starts a download of ImageURI for the single target the controller names and
// tracks it as a Redfish task. The content is fetched completely, like a real BMC, so the registry
// carries real traffic; what happens with the bytes depends on the target.
func (s *Server) handleSimpleUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ImageURI string   `json:"ImageURI"`
		Targets  []string `json:"Targets"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	if body.ImageURI == "" || len(body.Targets) != 1 {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyMissing", "ImageURI and exactly one target are required")
		return
	}
	target := path.Base(strings.TrimSuffix(body.Targets[0], "/"))
	p := s.state.Personality()
	switch target {
	case p.InvOS, p.InvOSImage, p.InvOSConfig:
		if target == "" {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "unsupported target "+body.Targets[0])
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "Base.1.18.1.ActionParameterNotSupported", "unsupported target "+body.Targets[0])
		return
	}
	task := s.state.NewTask("SimpleUpdate " + target)
	klog.InfoS("Redfish SimpleUpdate accepted", "target", target, "imageURI", body.ImageURI, "task", task.ID)
	go s.runSimpleUpdate(s.ctx, task.ID, target, body.ImageURI)
	writeJSON(w, http.StatusAccepted, taskResponse(mustTask(s.state, task.ID)))
}

func (s *Server) runSimpleUpdate(ctx context.Context, taskID, target, imageURI string) {
	p := s.state.Personality()
	progress := func(done, total int64) {
		if total > 0 {
			pct := int(done * 100 / total)
			if pct > 99 {
				pct = 99
			}
			s.state.SetTaskProgress(taskID, pct)
		}
	}
	fail := func(err error) {
		klog.ErrorS(err, "Redfish SimpleUpdate failed", "target", target, "task", taskID)
		s.state.FailTask(taskID, err)
	}
	switch target {
	case p.InvOS: // BF3: BFB + bf.cfg concat stream
		splitter := artifact.NewBFBSplitter()
		n, err := artifact.Download(ctx, imageURI, splitter, progress)
		if err != nil {
			fail(err)
			return
		}
		if err := splitter.Finish(); err != nil {
			fail(err)
			return
		}
		versions, err := splitter.Inventory().Versions()
		if err != nil {
			fail(fmt.Errorf("BFB software inventory: %w", err))
			return
		}
		bfcfg := splitter.BFCFG()
		if len(bfcfg) == 0 {
			fail(errors.New("no bf.cfg followed the BFB in the ImageURI stream"))
			return
		}
		s.state.SetInstalledBFBVersions(versions)
		s.state.CompleteTask(taskID)
		klog.InfoS("BFB installed", "bytes", n, "bfbBytes", splitter.BFBSize(), "images", splitter.Images(),
			"bfcfgBytes", len(bfcfg), "uefi", versions.UEFI, "bsp", versions.BSP, "doca", versions.DOCA)
		go s.sup.Boot(ctx, bfcfg)
	case p.InvOSImage: // BF4: OS ISO, content irrelevant
		n, err := artifact.Download(ctx, imageURI, io.Discard, progress)
		if err != nil {
			fail(err)
			return
		}
		s.state.SetFirmwareVersion(p.InvOSImage, path.Base(imageURI))
		s.state.CompleteTask(taskID)
		klog.InfoS("BF4 OS image transferred", "bytes", n)
	case p.InvOSConfig: // BF4: cidata seed.iso, kept until ArmReset
		buf := artifact.NewLimitedBuffer(artifact.MaxSeedISOSize)
		n, err := artifact.Download(ctx, imageURI, buf, progress)
		if err != nil {
			fail(err)
			return
		}
		s.state.SetPendingSeedISO(buf.Bytes())
		s.state.SetFirmwareVersion(p.InvOSConfig, path.Base(imageURI))
		s.state.CompleteTask(taskID)
		klog.InfoS("BF4 OS config image transferred", "bytes", n)
	}
}

// handleUpdateMultipart is the BF4 PLDM bundle upload. The header is parsed for the component
// versions while the payload streams past; the versions stay pending until Activate + reboot.
func (s *Server) handleUpdateMultipart(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", "expected multipart body: "+err.Error())
		return
	}
	var (
		pkg         *artifact.PLDMPackage
		forceUpdate bool
		payloadSize int64
	)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", "read multipart: "+err.Error())
			return
		}
		switch part.FormName() {
		case "UpdateFile":
			pkg, err = artifact.ParsePLDM(part)
			if err != nil {
				_, _ = io.Copy(io.Discard, part)
				writeError(w, http.StatusBadRequest, "Update.1.0.InvalidPackage", "invalid PLDM package: "+err.Error())
				return
			}
			payloadSize, _ = io.Copy(io.Discard, part)
		case "UpdateParameters":
			var params struct {
				ForceUpdate bool `json:"ForceUpdate"`
			}
			_ = json.NewDecoder(part).Decode(&params)
			forceUpdate = params.ForceUpdate
		default:
			_, _ = io.Copy(io.Discard, part)
		}
	}
	if pkg == nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyMissing", "UpdateFile part is required")
		return
	}
	if !strings.EqualFold(pkg.Versions.NICPSID, s.state.PSID()) {
		klog.InfoS("PLDM bundle CX9 PSID differs from the configured board PSID; the controller will report the mismatch",
			"bundlePSID", pkg.Versions.NICPSID, "boardPSID", s.state.PSID())
	}
	s.state.SetPendingBundle(pkg.Versions)
	task := s.state.NewTask("PLDM firmware update")
	s.state.CompleteTask(task.ID)
	klog.InfoS("Redfish PLDM bundle uploaded", "package", pkg.PackageVersion, "forceUpdate", forceUpdate,
		"payloadBytes", payloadSize, "bmc", pkg.Versions.BMC, "erot", pkg.Versions.ERoT, "sbios", pkg.Versions.SBIOS, "nic", pkg.Versions.NIC)
	writeJSON(w, http.StatusAccepted, taskResponse(mustTask(s.state, task.ID)))
}

func (s *Server) handleActivatePendingBundle(w http.ResponseWriter, r *http.Request) {
	if !s.state.ActivatePendingBundle() {
		writeError(w, http.StatusBadRequest, "Update.1.0.ActivateFailed", "no pending bundle to activate")
		return
	}
	klog.InfoS("Redfish pending bundle activated; versions switch on the next host reboot")
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}
