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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
)

// CMX Redfish resource ids from the v0.2 nvidia-cmx-baseboard mockup. One node is one BMC.
// Resource ids are the same on Node_0 and Node_1; the node is Location, not the URI.
const (
	CMXSystemID         = "System_0"
	CMXIdentityChassis  = "Chassis_0"
	CMXBMCChassisID     = "BMC_0"
	CMXERoTChassisID    = "ERoT_BMC_0"
	CMXFirmwareBMC      = "FW_BMC_0"
	CMXFirmwareERoT     = "FW_ERoT_BMC_0"
	CMXFirmwareSBIOS    = "SBIOS_FW_0"
	CMXFirmwareSBIOSFMC = "SBIOS_FMC_0"
	CMXFirmwareCX0      = "FW_CX_0"
	CMXFirmwareCX1      = "FW_CX_1"
	CMXVirtualMediaOS   = "USB1"
	CMXVirtualMediaSeed = "USB2"

	// CMXResetAuxPowerCycle is the Node AC cycle on Chassis/BMC_0.
	CMXResetAuxPowerCycle = "AuxPowerCycle"
)

// CMXNIC is one ConnectX chassis on a CMX node, with the firmware member that RelatedItem points at it.
type CMXNIC struct {
	ID           string
	ServiceLabel string
	Ordinal      int
	Model        string
	SerialNumber string
	PartNumber   string
	FirmwareID   string
	Version      string
}

// ListCMXNICs returns every CX chassis. The count is not fixed: the chassis collection is walked
// and any member whose id starts with "CX_" is read.
func (c *Client) ListCMXNICs(ctx context.Context) ([]CMXNIC, error) {
	_, collection, err := read[collectionBody](ctx, c, RedfishAPIs.Path("APIGetChassisCollection"))
	if err != nil {
		return nil, err
	}
	firmware, err := c.ListFirmwareInventory(ctx)
	if err != nil {
		return nil, err
	}
	var nics []CMXNIC
	for _, ref := range collection.Members {
		id := pathID(ref.ODataID)
		if !strings.HasPrefix(id, "CX_") {
			continue
		}
		_, chassis, err := read[ChassisInfo](ctx, c, RedfishAPIs.Path("APIGetChassis"), map[string]string{"CHASSIS_ID": id})
		if err != nil {
			return nil, fmt.Errorf("chassis %s: %w", id, err)
		}
		nic := CMXNIC{
			ID:           id,
			Model:        chassis.Model,
			SerialNumber: chassis.SerialNumber,
			PartNumber:   chassis.PartNumber,
		}
		if chassis.Location != nil {
			nic.ServiceLabel = chassis.Location.PartLocation.ServiceLabel
			nic.Ordinal = chassis.Location.PartLocation.LocationOrdinalValue
		}
		for _, member := range firmware {
			if firmwareRelatedChassis(member, id) {
				nic.FirmwareID = member.ID
				nic.Version = member.Version
				break
			}
		}
		nics = append(nics, nic)
	}
	return nics, nil
}

// UpdateCMXFirmware pushes a PLDM package with multipart update. targets are firmware inventory
// ids. An empty list omits Targets, which installs every component in the package. There is no
// UpdateService.Activate on this BMC.
func (c *Client) UpdateCMXFirmware(ctx context.Context, fwFile *os.File, force bool, targets []string) (*resty.Response, *TaskInfo, error) {
	params := map[string]interface{}{}
	if force {
		params["ForceUpdate"] = true
	}
	if len(targets) > 0 {
		uris := make([]string, 0, len(targets))
		for _, id := range targets {
			uris = append(uris, "/redfish/v1/UpdateService/FirmwareInventory/"+id)
		}
		params["Targets"] = uris
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, nil, err
	}
	return do[TaskInfo](func() (*resty.Response, error) {
		return c.Client.R().SetContext(ctx).
			SetFileReader("UpdateFile", fwFile.Name(), fwFile).
			SetMultipartField("UpdateParameters", "", "application/json", strings.NewReader(string(raw))).
			Post(RedfishAPIs.Path("APIUpdateBluefieldFWMultipart"))
	})
}

// CommitImage posts NvidiaUpdateService.CommitImage. The action takes no parameters and does not
// change the running firmware version.
func (c *Client) CommitImage(ctx context.Context) (*resty.Response, error) {
	resp, err := c.Client.R().SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(`{}`).
		Post(RedfishAPIs.Path("APICommitImage"))
	if err != nil {
		return resp, fmt.Errorf("commit image: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusAccepted {
		return resp, fmt.Errorf("commit image: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

// ACPowerCycle posts NvidiaChassis.AuxPowerReset with ResetType AuxPowerCycle on Chassis/BMC_0.
// That Node AC cycle promotes every staged PLDM image. BMC, ERoT, CPLD, MCU, SMA and drive
// firmware stay staged across ComputerSystem.Reset, so an update that includes any of them
// finishes with this cycle. SBIOS and the CX9s activate on the host reset instead.
func (c *Client) ACPowerCycle(ctx context.Context) (*resty.Response, error) {
	return c.AuxPowerReset(ctx, CMXResetAuxPowerCycle)
}

// CMXFirmwareActivation is the value of DPU.status.firmwareActivation. Empty means nothing
// staged is waiting. ACPowerCycle is the high-water mark: a later update must not lower it
// to HostReset, because the Node AC cycle is what promotes the image that raised it.
type CMXFirmwareActivation string

const (
	// CMXActivationHostReset is ComputerSystem.Reset. It promotes SBIOS and the CX9s only.
	CMXActivationHostReset CMXFirmwareActivation = "HostReset"
	// CMXActivationACPowerCycle is NvidiaChassis.AuxPowerReset AuxPowerCycle. It promotes
	// every staged image, including SBIOS and the CX9s.
	CMXActivationACPowerCycle CMXFirmwareActivation = "ACPowerCycle"
)

// CMXNeedsACPowerCycle reports whether a staged inventory member stays inactive across
// ComputerSystem.Reset. SBIOS_FW_0, SBIOS_FMC_0, FW_CX_0, and FW_CX_1 activate on that host reset.
// Every other bundle image needs the Node AC cycle.
func CMXNeedsACPowerCycle(id string) bool {
	switch id {
	case CMXFirmwareSBIOS, CMXFirmwareSBIOSFMC, CMXFirmwareCX0, CMXFirmwareCX1:
		return false
	default:
		return true
	}
}

// CMXRaiseFirmwareActivation applies one multipart update's Targets to the activation still
// owed. An empty list is a full-package install: UpdateCMXFirmware omits Targets and the
// package can contain BMC or ERoT images, so the activation becomes ACPowerCycle. Any other id
// besides SBIOS_FW_0, SBIOS_FMC_0, FW_CX_0, and FW_CX_1 also raises ACPowerCycle. A list of only
// those ids sets HostReset when current is empty. ACPowerCycle is never lowered: a later retry
// that targets only the CX9s leaves it in place, because the earlier image was not activated.
func CMXRaiseFirmwareActivation(current CMXFirmwareActivation, ids []string) CMXFirmwareActivation {
	if current == CMXActivationACPowerCycle {
		return current
	}
	if len(ids) == 0 {
		return CMXActivationACPowerCycle
	}
	hostReset := false
	for _, id := range ids {
		if id == "" {
			continue
		}
		if CMXNeedsACPowerCycle(id) {
			return CMXActivationACPowerCycle
		}
		hostReset = true
	}
	if hostReset && current == "" {
		return CMXActivationHostReset
	}
	return current
}

// AuxPowerReset posts NvidiaChassis.AuxPowerReset on the CMX BMC chassis. resetType is
// AuxPowerCycle or AuxPowerCycleForce. This is the Node AC cycle.
func (c *Client) AuxPowerReset(ctx context.Context, resetType string) (*resty.Response, error) {
	chassisID := CMXBMCChassisID
	if !c.IsCMX() {
		return nil, fmt.Errorf("auxiliary power reset is only defined for a CMX BMC")
	}
	resp, err := c.Client.R().SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"ResetType": resetType}).
		SetPathParam("CHASSIS_ID", chassisID).
		Post(RedfishAPIs.Path("APIAuxPowerReset"))
	if err != nil {
		return resp, fmt.Errorf("auxiliary power reset: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusAccepted {
		return resp, fmt.Errorf("auxiliary power reset: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

// PowerOnSystem posts ComputerSystem.Reset On. CMX firmware targets that belong to the host
// appear only after the host has been powered on.
func (c *Client) PowerOnSystem(ctx context.Context) (*resty.Response, error) {
	return c.resetDPUArm(ctx, "On")
}

// InsertRemoteVirtualMedia inserts an image URI into a virtual media slot. CMX accepts an HTTPS
// URI on USB1 (OS ISO) and USB2 (per-DPU config ISO). Slot_0 has no InsertMedia action.
func (c *Client) InsertRemoteVirtualMedia(ctx context.Context, mediaID, imageURI string) (*resty.Response, error) {
	if c.IsCMX() && mediaID != CMXVirtualMediaOS && mediaID != CMXVirtualMediaSeed {
		return nil, fmt.Errorf("CMX virtual media slot %q does not accept InsertMedia", mediaID)
	}
	return insertVirtualMedia(ctx, c, map[string]interface{}{
		"Image":          imageURI,
		"TransferMethod": "Stream",
	}, mediaID)
}

// findSystemIDFor selects the system for this client. CMX serves System_0, which does not contain "bluefield".
func findSystemIDFor(c *Client, systems *Systems) (string, error) {
	if c.IsCMX() {
		return findCMXSystemID(systems)
	}
	return findSystemID(systems)
}

// findCMXSystemID returns System_0 from the systems collection. A collection with one other member
// is accepted so a BMC that renames the single system still resolves.
func findCMXSystemID(systems *Systems) (string, error) {
	if systems == nil || len(systems.Members) == 0 {
		return "", fmt.Errorf("no system found")
	}
	for _, system := range systems.Members {
		if id := pathID(system.ODataID); id == CMXSystemID {
			return id, nil
		}
	}
	if len(systems.Members) == 1 {
		return pathID(systems.Members[0].ODataID), nil
	}
	return "", fmt.Errorf("no system found")
}

// firmwareRelatedChassis reports whether a firmware member's RelatedItem points at chassisID.
func firmwareRelatedChassis(member FirmwareInventoryMember, chassisID string) bool {
	suffix := "/Chassis/" + chassisID
	for _, related := range member.RelatedItem {
		if related.ODataID == suffix || strings.HasSuffix(related.ODataID, suffix) {
			return true
		}
	}
	return false
}

// cmxSkippedAction returns success without calling the BMC. BlueField-only steps are not on a
// CMX BMC, and an error from those calls would stop the DPU controller.
func cmxSkippedAction(operation string) (*resty.Response, *ExtendedInfo, error) {
	header := make(http.Header)
	header.Set("X-DPF-Skipped", operation)
	resp := &resty.Response{RawResponse: &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}}
	return resp, &ExtendedInfo{}, nil
}
