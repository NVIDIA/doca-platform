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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/phasedelay"

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
	body := map[string]interface{}{
		"@odata.id":            "/redfish/v1/UpdateService",
		"@odata.type":          "#UpdateService.v1_10_0.UpdateService",
		"Id":                   "UpdateService",
		"Name":                 "Update Service",
		"HttpPushUri":          "/redfish/v1/UpdateService",
		"MultipartHttpPushUri": "/redfish/v1/UpdateService/update-multipart",
		"FirmwareInventory":    odata("/redfish/v1/UpdateService/FirmwareInventory"),
	}
	if s.state.Personality().IsCMX() {
		body["@odata.type"] = "#UpdateService.v1_11_1.UpdateService"
		body["MultipartHttpPushUri@Redfish.OperationApplyTimeSupport"] = map[string]interface{}{
			"@odata.type":     "#Settings.v1_3_3.OperationApplyTimeSupport",
			"SupportedValues": []string{"OnReset", "Immediate"},
		}
		body["Actions"] = map[string]interface{}{
			"Oem": map[string]interface{}{
				"#NvidiaUpdateService.CommitImage": map[string]interface{}{
					"target": "/redfish/v1/UpdateService/Actions/Oem/NvidiaUpdateService.CommitImage",
				},
			},
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// handleCommitImage implements NvidiaUpdateService.CommitImage. The action takes no parameters and
// does not change the running version. It marks a staged inactive slot as pending activation.
func (s *Server) handleCommitImage(w http.ResponseWriter, r *http.Request) {
	if !s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	s.state.CommitStagedImage()
	klog.InfoS("Redfish NvidiaUpdateService.CommitImage")
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
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

// handleFirmwareInventoryCollection lists firmware inventory members.
func (s *Server) handleFirmwareInventoryCollection(w http.ResponseWriter, _ *http.Request) {
	ids := s.state.FirmwareIDs()
	members := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		members = append(members, odata("/redfish/v1/UpdateService/FirmwareInventory/"+id))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           "/redfish/v1/UpdateService/FirmwareInventory",
		"@odata.type":         "#SoftwareInventoryCollection.SoftwareInventoryCollection",
		"Name":                "Software Inventory Collection",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

func (s *Server) handleFirmwareInventory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	version, ok := s.state.FirmwareVersion(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	body := map[string]interface{}{
		"@odata.id":   r.URL.Path,
		"@odata.type": "#SoftwareInventory.v1_4_0.SoftwareInventory",
		"Id":          id,
		"Name":        id,
		"Version":     version,
		"Updateable":  true,
		"Status":      map[string]interface{}{"State": "Enabled", "Health": "OK"},
	}
	p := s.state.Personality()
	if member, ok := p.firmwareMember(id); ok && member.ReportRelated {
		items := make([]map[string]interface{}, 0, len(member.Related))
		for _, related := range member.Related {
			items = append(items, odata(related))
		}
		body["RelatedItem"] = items
	} else if nic, ok := p.nicSlot(id); ok {
		body["RelatedItem"] = []map[string]interface{}{odata("/redfish/v1/Chassis/" + nic.ChassisID)}
	}
	if member, ok := p.firmwareMember(id); ok && member.DualSlot {
		inactiveVersion, inactiveState, staged := s.state.InactiveSlot(id)
		if !staged {
			inactiveVersion = ""
			inactiveState = "Unknown"
		}
		body["Oem"] = map[string]interface{}{
			"Nvidia": map[string]interface{}{
				"@odata.type": "#NvidiaSoftwareInventory.v1_2_0.NvidiaSoftwareInventory",
				"ActiveFirmwareSlot": map[string]interface{}{
					"SlotId":                   0,
					"Version":                  version,
					"FirmwareComparisonNumber": 0,
					"BuildType":                "Development",
					"FirmwareState":            "Activated",
				},
				"InactiveFirmwareSlot": map[string]interface{}{
					"SlotId":                   1,
					"Version":                  inactiveVersion,
					"FirmwareComparisonNumber": 0,
					"BuildType":                "Development",
					"FirmwareState":            inactiveState,
				},
			},
		}
	}
	writeJSON(w, http.StatusOK, body)
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

// taskPace keeps a task Running for at least hold after start.
type taskPace struct {
	start time.Time
	hold  time.Duration
}

// percent is the task progress for a downloaded percentage: never ahead of the elapsed share of
// the hold, and below 100 until the task completes.
func (p taskPace) percent(downloaded int) int {
	pct := downloaded
	if p.hold > 0 {
		pct = min(pct, int(time.Since(p.start)*100/p.hold))
	}
	return min(pct, 99)
}

// holdTask keeps a task Running until its hold has elapsed, advancing PercentComplete with time.
// It returns ctx.Err() when ctx ends first.
func (s *Server) holdTask(ctx context.Context, taskID string, p taskPace) error {
	for {
		remaining := p.hold - time.Since(p.start)
		if remaining <= 0 {
			return nil
		}
		s.state.SetTaskProgress(taskID, p.percent(100))
		if err := phasedelay.Sleep(ctx, min(remaining, time.Second)); err != nil {
			return err
		}
	}
}

// transferStep is the OS Installing step of a SimpleUpdate target.
func transferStep(p Personality, target string) phasedelay.Step {
	switch target {
	case p.InvOSImage:
		return phasedelay.StepTransferISO
	case p.InvOSConfig:
		return phasedelay.StepTransferSeed
	default:
		return phasedelay.StepTransfer
	}
}

func (s *Server) runSimpleUpdate(ctx context.Context, taskID, target, imageURI string) {
	p := s.state.Personality()
	pace := taskPace{start: time.Now(), hold: s.delays.Draw(provisioningv1.DPUOSInstalling, transferStep(p, target), 1)}
	progress := func(done, total int64) {
		if total > 0 {
			s.state.SetTaskProgress(taskID, pace.percent(int(done*100/total)))
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
		if err := s.holdTask(ctx, taskID, pace); err != nil {
			return
		}
		s.state.SetInstalledBFBVersions(versions)
		s.state.CompleteTask(taskID)
		klog.InfoS("BFB installed", "bytes", n, "bfbBytes", splitter.BFBSize(), "images", splitter.Images(),
			"bfcfgBytes", len(bfcfg), "uefi", versions.UEFI, "bsp", versions.BSP, "doca", versions.DOCA)
		s.sup.Boot(ctx, bfcfg)
	case p.InvOSImage: // BF4: OS ISO, content irrelevant
		n, err := artifact.Download(ctx, imageURI, io.Discard, progress)
		if err != nil {
			fail(err)
			return
		}
		if err := s.holdTask(ctx, taskID, pace); err != nil {
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
		if err := s.holdTask(ctx, taskID, pace); err != nil {
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
		targets     []string
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
				ForceUpdate bool     `json:"ForceUpdate"`
				Targets     []string `json:"Targets"`
			}
			if err := json.NewDecoder(part).Decode(&params); err != nil {
				writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", "invalid UpdateParameters: "+err.Error())
				return
			}
			forceUpdate = params.ForceUpdate
			targets = params.Targets
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
	if s.state.Personality().IsCMX() {
		only, err := s.cmxUpdateTargets(targets, pkg.Versions)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyValueNotInList", err.Error())
			return
		}
		s.state.SetPendingBundleTargets(pkg.Versions, only)
	} else {
		s.state.SetPendingBundle(pkg.Versions)
	}
	task := s.state.NewTask("PLDM firmware update")
	if hold := s.delays.Draw(provisioningv1.DPUUpdateFirmware, phasedelay.StepFirmwareTask, 1); hold > 0 {
		go func() {
			if err := s.holdTask(s.ctx, task.ID, taskPace{start: time.Now(), hold: hold}); err == nil {
				s.state.CompleteTask(task.ID)
			}
		}()
	} else {
		s.state.CompleteTask(task.ID)
	}
	klog.InfoS("Redfish PLDM bundle uploaded", "package", pkg.PackageVersion, "forceUpdate", forceUpdate,
		"payloadBytes", payloadSize, "bmc", pkg.Versions.BMC, "erot", pkg.Versions.ERoT, "sbios", pkg.Versions.SBIOS, "nic", pkg.Versions.NIC)
	writeJSON(w, http.StatusAccepted, taskResponse(mustTask(s.state, task.ID)))
}

// cmxUpdateTargets resolves the Targets of a multipart update on a CMX BMC. Each target must be
// the full FirmwareInventory member URI, and the bundle must carry a version for that member. No
// Targets means every component of the bundle. It returns the inventory ids to stage, or an error
// naming a target that is not a member or has no version in the bundle.
func (s *Server) cmxUpdateTargets(targets []string, v artifact.PLDMVersions) (map[string]struct{}, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	const prefix = "/redfish/v1/UpdateService/FirmwareInventory/"
	only := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		id := strings.TrimPrefix(target, prefix)
		if id == target || id == "" || strings.Contains(id, "/") {
			return nil, fmt.Errorf("target %s is not a firmware inventory member", target)
		}
		if _, ok := s.state.FirmwareVersion(id); !ok {
			return nil, fmt.Errorf("target %s is not a firmware inventory member", target)
		}
		if !s.state.Personality().bundleCarries(v, id) {
			return nil, fmt.Errorf("target %s has no version in the PLDM bundle", target)
		}
		only[id] = struct{}{}
	}
	return only, nil
}

func (s *Server) handleActivatePendingBundle(w http.ResponseWriter, r *http.Request) {
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	if !s.state.ActivatePendingBundle() {
		writeError(w, http.StatusBadRequest, "Update.1.0.ActivateFailed", "no pending bundle to activate")
		return
	}
	klog.InfoS("Redfish pending bundle activated; versions switch on the next host reboot")
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}
