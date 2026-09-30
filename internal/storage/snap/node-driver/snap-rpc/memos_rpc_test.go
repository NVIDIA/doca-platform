/*
Copyright 2026 NVIDIA

  Licensed under the Apache License, Version 2.0 (the License);
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an AS IS BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
*/

package rpcclient

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// testMemosPCIBDF is the PCI address the mock assigns to a powered-on hotplug
// port, and the one a static emulation function reports.
const testMemosPCIBDF = "26:00.2"

// MockMemosClient is a JSONRPCClient that serves the NVMe front end of the CMX
// path - the PCI switch and doca_nvme_* RPCs - from in-memory state, and records
// every call so ordering and parameters can be asserted.
type MockMemosClient struct {
	switches        []PCISwitch
	nvmeManagers    []DocaNvmeManager
	nvmeFunctions   []DocaNvmeEmulationFunction
	nvmeSubsystems  []DocaNvmeSubsystem
	nvmeNamespaces  []DocaNvmeNamespace
	nvmeControllers []DocaNvmeController

	// failMethod makes that one method return an error.
	failMethod string

	// failStatusMethod makes that one method fail the way SNAP reports a refusal:
	// a successful reply whose envelope status names the error.
	failStatusMethod string

	calls  []string
	params []map[string]interface{}
}

// Send records nothing and returns a fixed request id, satisfying the memos
// client interface without performing any transport work.
func (m *MockMemosClient) Send(method string, params map[string]interface{}) (int, error) {
	return 1, nil
}

// Recv returns a canned successful reply, satisfying the memos client interface
// without performing any transport work.
func (m *MockMemosClient) Recv() (map[string]interface{}, error) {
	return map[string]interface{}{"result": "success"}, nil
}

// Close is a no-op that satisfies the memos client interface.
func (m *MockMemosClient) Close() error { return nil }

// methodsCalled returns the recorded call sequence.
func (m *MockMemosClient) methodsCalled() []string { return m.calls }

// paramsFor returns the parameters of the first call to method.
func (m *MockMemosClient) paramsFor(method string) map[string]interface{} {
	for i, called := range m.calls {
		if called == method {
			return m.params[i]
		}
	}
	return nil
}

// countOf returns how many times method was called.
func (m *MockMemosClient) countOf(method string) int {
	count := 0
	for _, called := range m.calls {
		if called == method {
			count++
		}
	}
	return count
}

// Call answers the way the SNAP RPC server does: the payload a method produces,
// nested inside the {"status", "result"} envelope that every reply carries. A
// method returning no data answers with the status alone, and failStatusMethod
// produces the failure SNAP reports through that status rather than through a
// JSON-RPC error.
func (m *MockMemosClient) Call(method string, params map[string]interface{}) (interface{}, error) {
	payload, err := m.payloadFor(method, params)
	if err != nil {
		return nil, err
	}

	if m.failStatusMethod == method {
		return map[string]interface{}{
			"status":  "SNAP_ERROR_NOT_FOUND",
			"message": fmt.Sprintf("injected status failure for %s", method),
		}, nil
	}

	envelope := map[string]interface{}{"status": snapStatusSuccess}
	if payload != nil {
		envelope["result"] = payload
	}
	return envelope, nil
}

// payloadFor records the call, honors any injected failure, and returns the
// data payload the given method produces for the fake SNAP state.
//
//nolint:gocyclo
func (m *MockMemosClient) payloadFor(method string, params map[string]interface{}) (interface{}, error) {
	m.calls = append(m.calls, method)
	m.params = append(m.params, params)

	if m.failMethod == method {
		return nil, fmt.Errorf("injected failure for %s", method)
	}

	switch method {
	case "pci_switch_show_info":
		return map[string]interface{}{"switches": m.switches}, nil
	case "pci_switch_port_create":
		vuid := fmt.Sprintf("MT2333X00000HP01%05d", m.countOf("pci_switch_port_create"))
		for i := range m.switches {
			if m.switches[i].Name == params["switch"].(string) {
				m.switches[i].Ports = append(m.switches[i].Ports, PCISwitchPort{
					VUID:  vuid,
					Type:  params["type"].(string),
					Power: PCIPortStatePowerOff,
				})
			}
		}
		return map[string]interface{}{"vuid": vuid}, nil
	case "pci_switch_port_set_power":
		m.setSwitchPortPowerByVUID(params["vuid"].(string), params["power"].(string) == PCIPowerOn)
		return nil, nil
	case "pci_switch_port_destroy":
		for i := range m.switches {
			kept := m.switches[i].Ports[:0]
			for _, port := range m.switches[i].Ports {
				if port.VUID != params["vuid"].(string) {
					kept = append(kept, port)
				}
			}
			m.switches[i].Ports = kept
		}
		return nil, nil

	case "doca_nvme_get_supported_managers":
		return m.nvmeManagers, nil
	case "doca_nvme_get_emulation_functions":
		return map[string]interface{}{"nvme_functions": m.nvmeFunctions}, nil
	case "doca_nvme_set_config":
		return nil, nil
	case "doca_nvme_get_subsystems":
		return m.nvmeSubsystems, nil
	case "doca_nvme_get_namespaces":
		return m.nvmeNamespaces, nil
	case "doca_nvme_get_controllers":
		return m.nvmeControllers, nil
	case "doca_nvme_subsystem_create":
		m.nvmeSubsystems = append(m.nvmeSubsystems, DocaNvmeSubsystem{
			SubsystemName: params["subsystem_name"].(string),
			NQN:           params["nqn"].(string),
		})
		return nil, nil
	case "doca_nvme_subsystem_destroy":
		kept := m.nvmeSubsystems[:0]
		for _, subsystem := range m.nvmeSubsystems {
			if subsystem.SubsystemName != params["subsystem_name"].(string) {
				kept = append(kept, subsystem)
			}
		}
		m.nvmeSubsystems = kept
		return nil, nil
	case "doca_nvme_subsystem_ns_create":
		m.nvmeNamespaces = append(m.nvmeNamespaces, DocaNvmeNamespace{
			NSID:          params["ns_id"].(int),
			CSI:           params["csi"].(string),
			Backend:       params["backend_name"].(string),
			SubsystemName: params["subsystem_name"].(string),
		})
		return nil, nil
	case "doca_nvme_subsystem_ns_destroy":
		kept := m.nvmeNamespaces[:0]
		for _, ns := range m.nvmeNamespaces {
			if ns.SubsystemName != params["subsystem_name"].(string) || ns.NSID != params["ns_id"].(int) {
				kept = append(kept, ns)
			}
		}
		m.nvmeNamespaces = kept
		return nil, nil
	case "doca_nvme_subsystem_controller_create":
		m.nvmeControllers = append(m.nvmeControllers, DocaNvmeController{
			CntlID:        params["cntl_id"].(int),
			SubsystemName: params["subsystem_name"].(string),
			VUID:          params["vuid"].(string),
		})
		return nil, nil
	case "doca_nvme_subsystem_controller_destroy":
		kept := m.nvmeControllers[:0]
		for _, ctrl := range m.nvmeControllers {
			if ctrl.SubsystemName != params["subsystem_name"].(string) || ctrl.CntlID != params["cntl_id"].(int) {
				kept = append(kept, ctrl)
			}
		}
		m.nvmeControllers = kept
		return nil, nil
	case "doca_nvme_subsystem_controller_hotplug":
		m.setSwitchPortPowerForController(params, true)
		return nil, nil
	case "doca_nvme_subsystem_controller_hotunplug":
		m.setSwitchPortPowerForController(params, false)
		return nil, nil
	}

	return nil, fmt.Errorf("unexpected method %s", method)
}

// setSwitchPortPowerForController powers the PCI switch port bound to the
// controller named by subsystem_name and cntl_id. Hotplug is what assigns the
// BDF the host sees.
func (m *MockMemosClient) setSwitchPortPowerForController(params map[string]interface{}, on bool) {
	subsystem, _ := params["subsystem_name"].(string)
	cntlID, _ := params["cntl_id"].(int)
	for _, ctrl := range m.nvmeControllers {
		if ctrl.SubsystemName == subsystem && ctrl.CntlID == cntlID {
			m.setSwitchPortPowerByVUID(ctrl.VUID, on)
			return
		}
	}
}

func (m *MockMemosClient) setSwitchPortPowerByVUID(vuid string, on bool) {
	for i := range m.switches {
		for j := range m.switches[i].Ports {
			if m.switches[i].Ports[j].VUID != vuid {
				continue
			}
			if on {
				m.switches[i].Ports[j].Power = PCIPortStatePowerOn
				// The BDF only exists once the function is visible to the host.
				m.switches[i].Ports[j].BDF = testMemosPCIBDF
			} else {
				m.switches[i].Ports[j].Power = PCIPortStatePowerOff
				m.switches[i].Ports[j].BDF = ""
			}
		}
	}
}

// assertCallOrder checks that want appears in called in that relative order.
// Extra calls in between are allowed, since the listings a step reads first are
// not part of the ordering being asserted.
func assertCallOrder(t *testing.T, called, want []string) {
	t.Helper()

	next := 0
	for _, method := range called {
		if next < len(want) && method == want[next] {
			next++
		}
	}

	if next != len(want) {
		t.Errorf("expected calls in the order %v, got %v (stopped at %s)", want, called, want[next])
	}
}

func TestDocaNvmeSetConfig(t *testing.T) {
	t.Run("omits an empty network device list", func(t *testing.T) {
		client := &MockMemosClient{}

		if err := DocaNvmeSetConfig(NewMemosJSONRPCClient(client), "mlx5_0", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		params := client.paramsFor("doca_nvme_set_config")
		if params["managers_dev_list"] != "mlx5_0" {
			t.Errorf("managers_dev_list = %v, want mlx5_0", params["managers_dev_list"])
		}
		if _, ok := params["network_dev_list"]; ok {
			t.Errorf("network_dev_list should be omitted when empty")
		}
	})

	t.Run("rejects an empty manager list", func(t *testing.T) {
		client := &MockMemosClient{}

		if err := DocaNvmeSetConfig(NewMemosJSONRPCClient(client), "", "mlx5_2"); err == nil {
			t.Fatalf("expected an error for an empty manager list")
		}
		if client.countOf("doca_nvme_set_config") != 0 {
			t.Errorf("an invalid manager list must not reach the RPC")
		}
	})
}

func TestDocaNvmeNextControllerID(t *testing.T) {
	// Controller IDs start at 0, unlike namespace IDs.
	controllers := []DocaNvmeController{{SubsystemName: "subsys0", CntlID: 0}}

	if got := DocaNvmeNextControllerID(controllers, "subsys0"); got != 1 {
		t.Errorf("next controller ID = %d, want 1", got)
	}
	if got := DocaNvmeNextControllerID(nil, "subsys0"); got != 0 {
		t.Errorf("first controller ID = %d, want 0", got)
	}
}

func TestDocaNvmeFindNamespaceByBackend(t *testing.T) {
	// The MEMOS volume is what identifies the namespace of an attachment, since
	// the namespace ID alone is only unique within a subsystem.
	namespaces := []DocaNvmeNamespace{
		{SubsystemName: "subsys0", NSID: 1, Backend: "volume_1", CSI: CSIKV},
		{SubsystemName: "subsys0", NSID: 2, Backend: "volume_2", CSI: CSIKV},
	}

	ns, found := DocaNvmeFindNamespaceByBackend(namespaces, "volume_2")
	if !found || ns.NSID != 2 {
		t.Errorf("got (%+v, %v), want NSID 2", ns, found)
	}
	if _, found := DocaNvmeFindNamespaceByBackend(namespaces, "volume_3"); found {
		t.Errorf("an unknown backend should not match a namespace")
	}
}

func TestDocaNvmeFindFreeStaticFunction(t *testing.T) {
	// A function already carrying a controller is in use, and a hotplug function
	// is not a static one, so neither may be claimed.
	functions := []DocaNvmeEmulationFunction{
		{VUID: "hotplug0", Kind: FunctionKindHotplug, BDF: "26:00.2"},
		{VUID: "static0", Kind: FunctionKindStatic, BDF: "25:00.2", Controller: "NvmeCtrl0"},
		{VUID: "static1", Kind: FunctionKindStatic, BDF: "25:00.3"},
	}

	function, found := DocaNvmeFindFreeStaticFunction(functions)
	if !found || function.VUID != "static1" {
		t.Errorf("got (%+v, %v), want static1", function, found)
	}

	if _, found := DocaNvmeFindFreeStaticFunction(functions[:2]); found {
		t.Errorf("no function should be claimable when every static one is in use")
	}
}

func TestDocaNvmeFindFunctionByVUID(t *testing.T) {
	// SR-IOV functions are nested under their PF, so the search has to recurse.
	functions := []DocaNvmeEmulationFunction{{
		VUID: "pf0",
		Kind: FunctionKindStatic,
		VFs:  []DocaNvmeEmulationFunction{{VUID: "vf0", BDF: "25:00.4"}},
	}}

	function, found := DocaNvmeFindFunctionByVUID(functions, "vf0")
	if !found || function.BDF != "25:00.4" {
		t.Errorf("got (%+v, %v), want the nested VF", function, found)
	}
}

func TestDocaNvmeFindFunctionByBDF(t *testing.T) {
	functions := []DocaNvmeEmulationFunction{{
		VUID: "MT2547601P6QNVMES0D0F2",
		Kind: FunctionKindStatic,
		BDF:  "5E:00.2",
		VFs:  []DocaNvmeEmulationFunction{{VUID: "vf0", BDF: "25:00.4"}},
	}}

	function, found := DocaNvmeFindFunctionByBDF(functions, "5E:00.2")
	if !found || function.VUID != "MT2547601P6QNVMES0D0F2" {
		t.Errorf("got (%+v, %v), want the static PF", function, found)
	}
	function, found = DocaNvmeFindFunctionByBDF(functions, "25:00.4")
	if !found || function.VUID != "vf0" {
		t.Errorf("got (%+v, %v), want the nested VF", function, found)
	}
	if _, found := DocaNvmeFindFunctionByBDF(functions, "5F:00.0"); found {
		t.Errorf("an unknown BDF should not match a function")
	}
}

func TestPCISwitchForType(t *testing.T) {
	switches := []PCISwitch{
		{Name: "mlx5_1", Types: []string{"virtio_fs"}, MaxPorts: 31},
		{Name: "mlx5_0", Types: []string{PCIPortTypeNVMe}, MaxPorts: 31},
	}

	name, err := PCISwitchForType(switches, PCIPortTypeNVMe)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "mlx5_0" {
		t.Errorf("switch = %q, want mlx5_0", name)
	}

	if _, err := PCISwitchForType(switches[:1], PCIPortTypeNVMe); err == nil {
		t.Errorf("expected an error when no switch advertises the port type")
	}

	// A switch that advertises the type but has no hotplug slots (max_ports 0)
	// is skipped, falling through to one that still has room.
	mixed := []PCISwitch{
		{Name: "mlx5_full", Types: []string{PCIPortTypeNVMe}, MaxPorts: 0},
		{Name: "mlx5_free", Types: []string{PCIPortTypeNVMe}, MaxPorts: 31},
	}
	name, err = PCISwitchForType(mixed, PCIPortTypeNVMe)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "mlx5_free" {
		t.Errorf("switch = %q, want mlx5_free", name)
	}

	// A switch whose ports are all in use (len(Ports) >= MaxPorts) is skipped.
	full := []PCISwitch{
		{
			Name:     "mlx5_0",
			Types:    []string{PCIPortTypeNVMe},
			MaxPorts: 1,
			Ports:    []PCISwitchPort{{VUID: "vuid0", Type: PCIPortTypeNVMe}},
		},
	}
	if _, err := PCISwitchForType(full, PCIPortTypeNVMe); err == nil {
		t.Errorf("expected an error when the only matching switch is full")
	}
}

func TestDocaNvmeSubsystemControllerHotplug(t *testing.T) {
	t.Run("sends subsystem name and controller ID", func(t *testing.T) {
		client := &MockMemosClient{}

		if err := DocaNvmeSubsystemControllerHotplug(NewMemosJSONRPCClient(client), "cmx0", 1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		params := client.paramsFor("doca_nvme_subsystem_controller_hotplug")
		if params["subsystem_name"] != "cmx0" {
			t.Errorf("subsystem_name = %v, want cmx0", params["subsystem_name"])
		}
		if params["cntl_id"] != 1 {
			t.Errorf("cntl_id = %v, want 1", params["cntl_id"])
		}
	})

	t.Run("rejects an empty subsystem name", func(t *testing.T) {
		client := &MockMemosClient{}

		if err := DocaNvmeSubsystemControllerHotplug(NewMemosJSONRPCClient(client), "", 1); err == nil {
			t.Fatalf("expected an error for an empty subsystem name")
		}
		if client.countOf("doca_nvme_subsystem_controller_hotplug") != 0 {
			t.Errorf("an invalid subsystem must not reach the RPC")
		}
	})

	t.Run("rejects a controller ID outside 0-0xFFEF", func(t *testing.T) {
		client := &MockMemosClient{}

		if err := DocaNvmeSubsystemControllerHotunplug(NewMemosJSONRPCClient(client), "cmx0", maxNvmeControllerID+1); err == nil {
			t.Fatalf("expected an error for a controller ID above %#x", maxNvmeControllerID)
		}
		if client.countOf("doca_nvme_subsystem_controller_hotunplug") != 0 {
			t.Errorf("an invalid controller ID must not reach the RPC")
		}
	})
}

func TestPCISwitchPortSetPowerRejectsAnInvalidState(t *testing.T) {
	// The states PCISwitchShowInfo reports are not the ones this RPC accepts, so
	// passing a reported value straight back through has to be refused.
	client := &MockMemosClient{}

	if err := PCISwitchPortSetPower(NewMemosJSONRPCClient(client), "mlx5_0", "vuid0", PCIPortStatePowerOn); err == nil {
		t.Fatalf("expected an error for the reported state %q", PCIPortStatePowerOn)
	}
	if client.countOf("pci_switch_port_set_power") != 0 {
		t.Errorf("an invalid power state must not reach the RPC")
	}
}

func TestPCISwitchPortCreateRequiresAVUIDInTheResponse(t *testing.T) {
	// The VUID is the only handle to the new port, so a response without one is
	// an error rather than an empty success.
	client := &MockMemosClient{failMethod: "pci_switch_port_create"}

	if _, err := PCISwitchPortCreate(NewMemosJSONRPCClient(client), "mlx5_0", PCIPortTypeNVMe); err == nil {
		t.Fatalf("expected the injected failure to surface")
	}
}

func TestFindPCISwitchPort(t *testing.T) {
	switches := []PCISwitch{
		{Name: "mlx5_0", Ports: []PCISwitchPort{{VUID: "vuid0", BDF: "26:00.2", Power: PCIPortStatePowerOn}}},
		{Name: "mlx5_1", Ports: []PCISwitchPort{{VUID: "vuid1"}}},
	}

	port, owningSwitch, found := FindPCISwitchPort(switches, "vuid0")
	if !found || owningSwitch != "mlx5_0" || port.BDF != "26:00.2" {
		t.Errorf("got (%+v, %q, %v), want the powered port on mlx5_0", port, owningSwitch, found)
	}

	if _, _, found := FindPCISwitchPort(switches, "vuid2"); found {
		t.Errorf("an unknown VUID should not match a port")
	}
}

// TestDecodeCapturedResponses decodes responses captured from a live SNAP
// service, so the structs stay pinned to what the service actually sends.
// stubJSONRPCClient returns one canned value as the JSON-RPC result member,
// standing in for whatever the server replied.
type stubJSONRPCClient struct {
	result interface{}
}

func (s stubJSONRPCClient) Call(string, map[string]interface{}) (interface{}, error) {
	return s.result, nil
}

func (s stubJSONRPCClient) Send(string, map[string]interface{}) (int, error) { return 1, nil }

func (s stubJSONRPCClient) Recv() (map[string]interface{}, error) { return nil, nil }

func (s stubJSONRPCClient) Close() error { return nil }

// TestMemosJSONRPCClientCall covers the reply shape that separates the SNAP this
// path talks to from the one the block path talks to.
func TestMemosJSONRPCClientCall(t *testing.T) {
	// callWith feeds raw JSON through the client the way Recv does: as the value
	// of the JSON-RPC result member, already decoded into interface{}.
	callWith := func(t *testing.T, resultMember string) (interface{}, error) {
		t.Helper()

		var decoded interface{}
		if err := json.Unmarshal([]byte(resultMember), &decoded); err != nil {
			t.Fatalf("bad test input: %v", err)
		}

		return NewMemosJSONRPCClient(stubJSONRPCClient{result: decoded}).Call("doca_nvme_get_subsystems", nil)
	}

	t.Run("returns the payload nested inside the envelope", func(t *testing.T) {
		// Decoding the result member itself yields the envelope object, not the
		// listing, which is the failure this unwrapping exists to prevent.
		payload, err := callWith(t, `{"status":"SNAP_SUCCESS","result":[{"subsystem_name":"cmx0"}]}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		listing, ok := payload.([]interface{})
		if !ok || len(listing) != 1 {
			t.Fatalf("payload = %#v, want a one-entry array", payload)
		}
	})

	t.Run("accepts a reply that carries only a status", func(t *testing.T) {
		// Every RPC that returns no data answers this way, so an absent result is
		// a success rather than a malformed reply.
		payload, err := callWith(t, `{"status":"SNAP_SUCCESS"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if payload != nil {
			t.Errorf("payload = %#v, want nil", payload)
		}
	})

	t.Run("reports a failure the envelope status carries", func(t *testing.T) {
		// A refused call still arrives as a well-formed JSON-RPC success, so the
		// status is the only thing that makes it an error.
		_, err := callWith(t, `{"status":"SNAP_ERROR_NOT_FOUND","message":"subsystem 'cmx0' not found"}`)
		if err == nil {
			t.Fatal("expected the envelope status to surface as an error")
		}
		if !strings.Contains(err.Error(), "SNAP_ERROR_NOT_FOUND") ||
			!strings.Contains(err.Error(), "subsystem 'cmx0' not found") {
			t.Errorf("error = %v, want it to name the status and the message", err)
		}
	})

	t.Run("refuses a reply with no status", func(t *testing.T) {
		// A flat array belongs to the SNAP the block path talks to. Reading it here
		// would mean the path is pointed at the wrong service, which is worth an
		// error rather than a silent empty listing.
		if _, err := callWith(t, `[{"subsystem_name":"cmx0"}]`); err == nil {
			t.Fatal("expected an error for a reply without an envelope")
		}
	})

	t.Run("returns a listing that has no status envelope", func(t *testing.T) {
		// doca_nvme_get_emulation_functions answers with {nvme_functions: [...]}
		// and no SNAP_SUCCESS wrapper.
		payload, err := callWith(t, `{"nvme_functions":[{"vuid":"MT0","kind":"static"}]}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		listing, ok := payload.(map[string]interface{})
		if !ok {
			t.Fatalf("payload = %#v, want an object", payload)
		}
		if _, ok := listing["nvme_functions"]; !ok {
			t.Fatalf("payload = %#v, want nvme_functions", payload)
		}
	})
}

func TestDecodeCapturedResponses(t *testing.T) {
	t.Run("pci_switch_show_info", func(t *testing.T) {
		// The switch array is nested under a key, unlike the doca_nvme listings,
		// and a switch with no hotplug slots reports max_ports 0.
		const payload = `{"switches":[
		  {"name":"mlx5_0","types":["nvme","virtio_blk"],"max_ports":31,
		   "ports":[{"vuid":"MT2333X00000HP0100000","type":"nvme","power":"power_on","bdf":"26:00.2"}]},
		  {"name":"mlx5_1","types":["nvme"],"max_ports":0}
		]}`

		var response pciSwitchShowInfoResponse
		if err := json.Unmarshal([]byte(payload), &response); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(response.Switches) != 2 {
			t.Fatalf("got %d switches, want 2", len(response.Switches))
		}
		if !slices.Contains(response.Switches[0].Types, PCIPortTypeNVMe) {
			t.Errorf("mlx5_0 types = %v, want to contain %q", response.Switches[0].Types, PCIPortTypeNVMe)
		}
		// A reported power state is its own vocabulary, distinct from PCIPowerOn.
		if response.Switches[0].Ports[0].Power != PCIPortStatePowerOn {
			t.Errorf("port power = %q, want %q", response.Switches[0].Ports[0].Power, PCIPortStatePowerOn)
		}
		if response.Switches[1].MaxPorts != 0 || len(response.Switches[1].Ports) != 0 {
			t.Errorf("unexpected mlx5_1: %+v", response.Switches[1])
		}
	})

	t.Run("doca_nvme_get_emulation_functions", func(t *testing.T) {
		// The function array is nested under nvme_functions, a free function has
		// no controller, and VFs are nested under their PF.
		const payload = `{"nvme_functions":[
		  {"vuid":"MT2333XZ0NVMES1D0F0","kind":"static","bdf":"25:00.2","emu_mgr":"mlx5_0",
		   "controller":"NvmeCtrl0","num_vfs":1,
		   "vfs":[{"vuid":"MT2333XZ0NVMEV1D0F0","kind":"static","bdf":"25:00.4","emu_mgr":"mlx5_0"}]},
		  {"vuid":"MT2333XZ0NVMES1D0F1","kind":"static","bdf":"25:00.3","emu_mgr":"mlx5_0","num_vfs":0}
		]}`

		var response docaNvmeEmulationFunctionsResponse
		if err := json.Unmarshal([]byte(payload), &response); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(response.NvmeFunctions) != 2 {
			t.Fatalf("got %d functions, want 2", len(response.NvmeFunctions))
		}
		function, found := DocaNvmeFindFreeStaticFunction(response.NvmeFunctions)
		if !found || function.BDF != "25:00.3" {
			t.Errorf("free static function = (%+v, %v), want the one at 25:00.3", function, found)
		}
		if _, found := DocaNvmeFindFunctionByVUID(response.NvmeFunctions, "MT2333XZ0NVMEV1D0F0"); !found {
			t.Errorf("the nested VF should be findable by VUID")
		}
	})

	t.Run("doca_nvme_get_namespaces", func(t *testing.T) {
		// A KV namespace reports its MEMOS volume as the backend, which is how an
		// attachment finds the namespace it already created.
		const payload = `[
		  {"ns_id":1,"csi":"kv","backend":"memos_vol_1","subsystem_name":"cmx0",
		   "nqn":"nqn.2026-01.io.spdk:cmx0","inflight":0}
		]`

		var namespaces []DocaNvmeNamespace
		if err := json.Unmarshal([]byte(payload), &namespaces); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(namespaces) != 1 || namespaces[0].CSI != CSIKV {
			t.Fatalf("unexpected namespaces: %+v", namespaces)
		}
		if ns, found := DocaNvmeFindNamespaceByBackend(namespaces, "memos_vol_1"); !found || ns.NSID != 1 {
			t.Errorf("got (%+v, %v), want NSID 1", ns, found)
		}
	})

	t.Run("doca_nvme_get_supported_managers", func(t *testing.T) {
		const payload = `[
		  {"manager_name":"mlx5_0","in_use":true,"max_io_queue_size":256},
		  {"manager_name":"mlx5_1","in_use":false,"max_io_queue_size":256}
		]`

		var managers []DocaNvmeManager
		if err := json.Unmarshal([]byte(payload), &managers); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		names := DocaNvmeManagerNames(managers)
		if len(names) != 2 || names[0] != "mlx5_0" || names[1] != "mlx5_1" {
			t.Errorf("manager names = %v, want [mlx5_0 mlx5_1]", names)
		}
	})
}
