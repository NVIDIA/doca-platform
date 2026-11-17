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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
)

// TestCMXBMCUser checks that a CMX root service selects the admin account, the same account the
// client certificate is issued as.
func TestCMXBMCUser(t *testing.T) {
	g := NewWithT(t)
	g.Expect(familyCMX.bmcUser()).To(Equal(BF4BMCUser))
	g.Expect(familyCMX.certDirSuffix()).To(Equal(familyBF4.certDirSuffix()))
	g.Expect(bmcUserCandidates(&RootServiceInfo{Product: "CMX"})).To(Equal([]string{BF4BMCUser}))
	g.Expect(bmcUserCandidates(&RootServiceInfo{Product: " cmx "})).To(Equal([]string{BF4BMCUser}))
	g.Expect(bmcUserCandidates(&RootServiceInfo{Product: "BlueField-4"})).To(Equal([]string{BF4BMCUser}))
	g.Expect(bmcUserCandidates(&RootServiceInfo{Product: "BlueField-3"})).To(Equal([]string{BF3BMCUser}))
	g.Expect(bmcUserCandidates(nil)).To(Equal([]string{BF3BMCUser, BF4BMCUser}))
	g.Expect(bmcUserCandidates(&RootServiceInfo{})).To(Equal([]string{BF3BMCUser, BF4BMCUser}))
}

// TestCMXClient checks the Vera BMC calls a DPUDevice or DPU controller uses to provision one CMX node.
func TestCMXClient(t *testing.T) {
	g := NewWithT(t)
	bmc := newCMXBMC()
	server := httptest.NewTLSServer(bmc)
	t.Cleanup(server.Close)

	c, err := NewBasicAuthClient(context.Background(), server.URL, BF4BMCUser, "password")
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(c.CloseIdleConnections)
	g.Expect(c.IsCMX()).To(BeTrue())
	g.Expect(c.IsBF4()).To(BeFalse())

	_, chassis, err := c.GetChassis(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(chassis.Model).To(Equal("CMX"))
	g.Expect(chassis.GetBlueFieldVersion()).To(Equal(provisioningv1.DPUTypeBlueField4CMX))
	g.Expect(bmc.paths()).To(ContainElement("/redfish/v1/Chassis/Chassis_0"))

	_, version, err := c.CheckBMCFirmware(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(version.Version).To(Equal("bmc-1"))
	_, version, err = c.CheckDPUUEFI(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(version.Version).To(Equal("sbios-1"))
	_, version, err = c.CheckBMCEROTFW(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(version.Version).To(Equal("erot-1"))

	members, err := c.ListFirmwareInventory(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(members).To(HaveLen(5))
	var bmcMember FirmwareInventoryMember
	for _, member := range members {
		if member.ID == CMXFirmwareBMC {
			bmcMember = member
		}
		if member.ID == "FW_CX_0" {
			g.Expect(member.InactiveVersion()).To(BeEmpty())
		}
	}
	g.Expect(bmcMember.InactiveVersion()).To(Equal("bmc-staged"))
	g.Expect(bmcMember.Oem.Nvidia.InactiveFirmwareSlot.FirmwareState).To(Equal("Inactive"))

	nics, err := c.ListCMXNICs(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nics).To(ConsistOf(
		CMXNIC{ID: "CX_0", ServiceLabel: "NIC 0", Model: "ConnectX-9 800GE 2P NIC", SerialNumber: "sn-0", PartNumber: "pn", FirmwareID: "FW_CX_0", Version: "cx-1"},
		CMXNIC{ID: "CX_1", ServiceLabel: "NIC 1", Ordinal: 1, Model: "ConnectX-9 800GE 2P NIC", SerialNumber: "sn-1", PartNumber: "pn", FirmwareID: "FW_CX_1", Version: "cx-1"},
	))

	_, system, err := c.GetSystem(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(system.PowerState).To(Equal("On"))

	before := bmc.count()
	resp, _, err := c.DisableHostRshim(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resp.StatusCode()).To(Equal(http.StatusOK))
	_, pf0, err := c.GetNetworkDeviceFunction(context.Background(), "0")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pf0.Ethernet.MACAddress).To(BeEmpty())
	_, err = c.GetPSID(context.Background())
	g.Expect(err).To(MatchError(ErrPSIDNotReported))
	g.Expect(bmc.count()).To(Equal(before))

	file, err := os.CreateTemp(t.TempDir(), "bundle")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = file.WriteString("pldm")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = file.Seek(0, io.SeekStart)
	g.Expect(err).NotTo(HaveOccurred())
	_, task, err := c.UpdateCMXFirmware(context.Background(), file, true, []string{"FW_CX_1"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.ID).To(Equal("7"))
	g.Expect(bmc.updateBody).To(ContainSubstring("FW_CX_1"))
	g.Expect(bmc.updateBody).To(ContainSubstring("ForceUpdate"))

	_, err = c.CommitImage(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	_, err = c.ACPowerCycle(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(bmc.auxReset).To(Equal(CMXResetAuxPowerCycle))
	g.Expect(bmc.paths()).To(ContainElement("/redfish/v1/Chassis/BMC_0/Actions/Oem/NvidiaChassis.AuxPowerReset"))

	_, err = c.InsertRemoteVirtualMedia(context.Background(), CMXVirtualMediaOS, "https://images.example/os.iso")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(bmc.inserted[CMXVirtualMediaOS]).To(Equal("https://images.example/os.iso"))
	_, err = c.InsertRemoteVirtualMedia(context.Background(), "Slot_0", "https://images.example/os.iso")
	g.Expect(err).To(HaveOccurred())

	_, err = c.PowerOnSystem(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(bmc.resetType).To(Equal("On"))
}

// TestCMXFirmwareActivation checks which staged PLDM members a host reset promotes and which
// stay staged until the Node AC cycle. ACPowerCycle is a high-water mark: a later CX9-only
// retry does not lower it to HostReset.
func TestCMXFirmwareActivation(t *testing.T) {
	g := NewWithT(t)
	for _, id := range []string{CMXFirmwareSBIOS, CMXFirmwareSBIOSFMC, CMXFirmwareCX0, CMXFirmwareCX1} {
		g.Expect(CMXNeedsACPowerCycle(id)).To(BeFalse(), id)
	}
	for _, id := range []string{
		CMXFirmwareBMC, CMXFirmwareERoT, "FW_CPLD_0",
		"FW_MCU_0", "FW_MCU_1", "FW_MCU_HPM_0", "FW_MCU_OSFP_0",
		"FW_ProcessorModule_SMA_0", "FW_StorageBackplane_SMA_0",
		"FW_NVMe_M2_0", "FW_NVMe_E3S_SSD0",
	} {
		g.Expect(CMXNeedsACPowerCycle(id)).To(BeTrue(), id)
	}

	g.Expect(CMXRaiseFirmwareActivation("", nil)).To(Equal(CMXActivationACPowerCycle))
	g.Expect(CMXRaiseFirmwareActivation(CMXActivationHostReset, nil)).To(Equal(CMXActivationACPowerCycle))
	g.Expect(CMXRaiseFirmwareActivation("", []string{CMXFirmwareSBIOS, CMXFirmwareCX1})).To(Equal(CMXActivationHostReset))
	g.Expect(CMXRaiseFirmwareActivation("", []string{CMXFirmwareERoT, "FW_CPLD_0"})).To(Equal(CMXActivationACPowerCycle))
	g.Expect(CMXRaiseFirmwareActivation("", []string{CMXFirmwareSBIOS, CMXFirmwareBMC, CMXFirmwareCX0})).To(Equal(CMXActivationACPowerCycle))
	g.Expect(CMXRaiseFirmwareActivation(CMXActivationHostReset, []string{CMXFirmwareCX0})).To(Equal(CMXActivationHostReset))
	g.Expect(CMXRaiseFirmwareActivation(CMXActivationACPowerCycle, []string{CMXFirmwareCX0, CMXFirmwareCX1})).To(Equal(CMXActivationACPowerCycle))
	g.Expect(CMXRaiseFirmwareActivation(CMXActivationACPowerCycle, nil)).To(Equal(CMXActivationACPowerCycle))
}

// cmxBMC is the slice of the v0.2 CMX Redfish tree these client calls use.
type cmxBMC struct {
	mu         sync.Mutex
	hits       int
	seen       []string
	inserted   map[string]string
	updateBody string
	auxReset   string
	resetType  string
}

// newCMXBMC returns an empty CMX BMC handler.
func newCMXBMC() *cmxBMC {
	return &cmxBMC{inserted: map[string]string{}}
}

// ServeHTTP records the request path and serves the CMX resource it names.
func (b *cmxBMC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.hits++
	b.seen = append(b.seen, r.URL.Path)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/redfish/v1" || r.URL.Path == "/redfish/v1/":
		writeJSON(w, map[string]string{"Product": "CMX"})
	case r.URL.Path == "/redfish/v1/Systems":
		writeJSON(w, map[string]interface{}{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/System_0"}}})
	case r.URL.Path == "/redfish/v1/Systems/System_0":
		writeJSON(w, map[string]string{"Id": "System_0", "PowerState": "On", "AssetTag": "CMX-MOCKUP-AT"})
	case r.URL.Path == "/redfish/v1/Systems/System_0/Actions/ComputerSystem.Reset":
		var body struct {
			ResetType string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.resetType = body.ResetType
		b.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	case r.URL.Path == "/redfish/v1/Managers":
		writeJSON(w, map[string]interface{}{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/Managers/BMC_0"},
			{"@odata.id": "/redfish/v1/Managers/SMM_0"},
		}})
	case r.URL.Path == "/redfish/v1/Chassis":
		writeJSON(w, map[string]interface{}{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/Chassis/Chassis_0"},
			{"@odata.id": "/redfish/v1/Chassis/CX_0"},
			{"@odata.id": "/redfish/v1/Chassis/CX_1"},
			{"@odata.id": "/redfish/v1/Chassis/BMC_0"},
		}})
	case r.URL.Path == "/redfish/v1/Chassis/Chassis_0":
		writeJSON(w, map[string]string{"Model": "CMX", "SerialNumber": "node-sn", "PartNumber": "pn", "AssetTag": "CMX-MOCKUP-AT"})
	case r.URL.Path == "/redfish/v1/Chassis/CX_0" || r.URL.Path == "/redfish/v1/Chassis/CX_1":
		id := pathID(r.URL.Path)
		ordinal := 0
		label := "NIC 0"
		serial := "sn-0"
		if id == "CX_1" {
			ordinal = 1
			label = "NIC 1"
			serial = "sn-1"
		}
		writeJSON(w, map[string]interface{}{
			"Id": id, "Model": "ConnectX-9 800GE 2P NIC", "SerialNumber": serial, "PartNumber": "pn",
			"Location": map[string]interface{}{"PartLocation": map[string]interface{}{"ServiceLabel": label, "LocationOrdinalValue": ordinal}},
		})
	case r.URL.Path == "/redfish/v1/UpdateService/FirmwareInventory":
		writeJSON(w, map[string]interface{}{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/FW_BMC_0"},
			{"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/FW_CX_0"},
			{"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/FW_CX_1"},
			{"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/SBIOS_FW_0"},
			{"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/FW_ERoT_BMC_0"},
		}})
	case strings.HasPrefix(r.URL.Path, "/redfish/v1/UpdateService/FirmwareInventory/"):
		b.writeFirmware(w, pathID(r.URL.Path))
	case r.URL.Path == "/redfish/v1/UpdateService/update-multipart":
		body, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.updateBody = string(body)
		b.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"Id":"7","TaskState":"Running"}`))
	case r.URL.Path == "/redfish/v1/UpdateService/Actions/Oem/NvidiaUpdateService.CommitImage":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	case r.URL.Path == "/redfish/v1/Chassis/BMC_0/Actions/Oem/NvidiaChassis.AuxPowerReset":
		var body struct {
			ResetType string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.auxReset = body.ResetType
		b.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	case strings.Contains(r.URL.Path, "/VirtualMedia/"):
		b.virtualMedia(w, r)
	default:
		http.NotFound(w, r)
	}
}

// writeFirmware writes one inventory member. Dual-slot BMC firmware includes an inactive slot.
func (b *cmxBMC) writeFirmware(w http.ResponseWriter, id string) {
	body := map[string]interface{}{"Id": id, "Version": "cx-1", "Updateable": true}
	switch id {
	case CMXFirmwareBMC:
		body["Version"] = "bmc-1"
		body["Oem"] = map[string]interface{}{"Nvidia": map[string]interface{}{
			"ActiveFirmwareSlot":   map[string]interface{}{"SlotId": 0, "Version": "bmc-1", "FirmwareState": "Activated"},
			"InactiveFirmwareSlot": map[string]interface{}{"SlotId": 1, "Version": "bmc-staged", "FirmwareState": "Inactive"},
		}}
	case CMXFirmwareSBIOS:
		body["Version"] = "sbios-1"
	case CMXFirmwareERoT:
		body["Version"] = "erot-1"
	case "FW_CX_0":
		body["RelatedItem"] = []map[string]string{{"@odata.id": "/redfish/v1/Chassis/CX_0"}}
	case "FW_CX_1":
		body["RelatedItem"] = []map[string]string{{"@odata.id": "/redfish/v1/Chassis/CX_1"}}
	}
	writeJSON(w, body)
}

// virtualMedia serves InsertMedia and EjectMedia for USB1 and USB2.
func (b *cmxBMC) virtualMedia(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	media := parts[len(parts)-1]
	if strings.HasSuffix(r.URL.Path, "/VirtualMedia.EjectMedia") {
		media = parts[len(parts)-3]
		b.mu.Lock()
		delete(b.inserted, media)
		b.mu.Unlock()
		writeJSON(w, map[string]string{})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/VirtualMedia.InsertMedia") {
		media = parts[len(parts)-3]
		var body struct {
			Image string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.inserted[media] = body.Image
		b.mu.Unlock()
		writeJSON(w, map[string]string{})
		return
	}
	b.mu.Lock()
	image := b.inserted[media]
	b.mu.Unlock()
	writeJSON(w, map[string]interface{}{"Id": media, "Inserted": image != "", "Image": image})
}

// count returns how many requests the BMC has served.
func (b *cmxBMC) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hits
}

// paths returns a copy of the request paths in arrival order.
func (b *cmxBMC) paths() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.seen))
	copy(out, b.seen)
	return out
}

// writeJSON writes body as a JSON response.
func writeJSON(w http.ResponseWriter, body interface{}) {
	raw, _ := json.Marshal(body)
	_, _ = w.Write(raw)
}
