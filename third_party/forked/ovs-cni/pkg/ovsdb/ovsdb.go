// Modifications copyright (C) 2017 Che Wei, Lin
// Copyright 2014 Cisco Systems Inc. All rights reserved.
// Copyright 2019 Red Hat Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ovsdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"os/exec"
	"strings"

	current "github.com/containernetworking/cni/pkg/types/100"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

const ovsPortOwner = "ovs-cni.network.kubevirt.io"
const SfcBridge = "br-sfc"
const HbnBridge = "br-hbn"
const OvnBridge = "br-ovn"
const (
	bridgeTable = "Bridge"
	ovsTable    = "Open_vSwitch"
)

// OpenvSwitch limits the port numbers that it automatically assigns to
// the range 1 through 32,767, inclusive.  Controllers therefore have
// free use of ports 32,768 and up. Maximum valid ofport_request is 65279.
// Hence valid `ofport_request` range is from 32768 to 65279 (inclusive)
// See: https://github.com/openvswitch/ovs/blob/a8b0e4cab94f57bc414b20b4af43c7c5a800cf6c/vswitchd/vswitch.xml#L2738-L2760
const (
	minOFPort   = uint(32768)
	maxOFPort   = uint(65279)
	oFPortCount = maxOFPort - minOFPort + 1
)

var (
	errObjectNotFound = errors.New("object not found")
)

// Bridge defines an object in Bridge table
type Bridge struct {
	UUID string `ovsdb:"_uuid"`
}

// OpenvSwitch defines an object in Open_vSwitch table
type OpenvSwitch struct {
	UUID string `ovsdb:"_uuid"`
}

// OvsDriver OVS driver state
type OvsDriver struct {
	// OVS client
	OvsClient client.Client
}

// OvsBridgeDriver OVS bridge driver state
type OvsBridgeDriver struct {
	OvsDriver

	// Name of the OVS bridge
	OvsBridgeName string
}

// hashToOFPort maps interface name to an OpenFlow port number in [minOFPort, maxOFPort]
// It takes hash + modulo of oFPortCount, giving a number in [0, oFPortCount-1], then
// shifts that 0‑based result into the desired port range [minOFPort, maxOFPort].
// So the final returned value is always between 32768 and 65279 (inclusive).
func hashToOFPort(s string) uint {
	h := fnv.New32a()
	h.Write([]byte(s))
	return minOFPort + uint(h.Sum32()%uint32(oFPortCount))
}

// resolveOFPort picks a collision-free ofport_request for intfName on a bridge.
// It starts from the hash-derived candidate and linear-probes within
// [minOFPort, maxOFPort] until an unused slot is found.
//
// Basic steps are:
// a) check if the candidate is used, if not used, return as a valid candidate
// b) if it is in use, probe = minOFPort + (candidate - minOFPort + i) % oFPortCount
// c) if the probe is used, increment i and repeat
// i is a step offset (1, 2, 3, …),
//
// Example (minOFPort=10, oFPortCount=5 → range [10,14]):
//
//	candidate = 12, ports 12 and 13 already used
//	  i=1 → 10 + (2+1)%5 = 13  (used, skip)
//	  i=2 → 10 + (2+2)%5 = 14  (free, return 14)

func resolveOFPort(intfName string, usedPorts map[uint]bool) uint {
	candidate := hashToOFPort(intfName)
	if !usedPorts[candidate] {
		return candidate
	}
	for i := uint(1); i < oFPortCount; i++ { // i is uint to match oFPortCount and the arithmetic below
		probe := minOFPort + (candidate-minOFPort+i)%oFPortCount
		if !usedPorts[probe] {
			return probe
		}
	}
	// All slots (ofPortCount) are occupied; return 0 so that createInterfaceOperation
	// omits ofport_request entirely and lets OVS assign a port number itself.
	// This should not happen in practice.
	return 0
}

// ConnectToOvsDb connect to ovsdb
func ConnectToOvsDb(ovsSocket string) (client.Client, error) {
	dbmodel, err := model.NewClientDBModel("Open_vSwitch",
		map[string]model.Model{bridgeTable: &Bridge{}, ovsTable: &OpenvSwitch{}})
	if err != nil {
		return nil, fmt.Errorf("unable to create DB model error: %v", err)
	}

	ovsDB, err := client.NewOVSDBClient(dbmodel, client.WithEndpoint(ovsSocket))
	if err != nil {
		return nil, fmt.Errorf("unable to create DB client error: %v", err)
	}
	err = ovsDB.Connect(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ovsdb error: %v", err)
	}

	return ovsDB, nil
}

// NewOvsDriver Create a new OVS driver with Unix socket
func NewOvsDriver(ovsSocket string) (*OvsDriver, error) {
	ovsDriver := new(OvsDriver)

	if ovsSocket == "" {
		ovsSocket = "unix:/var/run/openvswitch/db.sock"
	}

	ovsDB, err := ConnectToOvsDb(ovsSocket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ovsdb error: %v", err)
	}

	ovsDriver.OvsClient = ovsDB

	return ovsDriver, nil
}

// NewOvsBridgeDriver Create a new OVS driver for a bridge with Unix socket
func NewOvsBridgeDriver(bridgeName, socketFile string) (*OvsBridgeDriver, error) {
	ovsDriver := new(OvsBridgeDriver)

	if socketFile == "" {
		socketFile = "unix:/var/run/openvswitch/db.sock"
	}

	ovsDB, err := ConnectToOvsDb(socketFile)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ovsdb socket %s: error: %v", socketFile, err)
	}

	// Setup state
	ovsDriver.OvsClient = ovsDB
	ovsDriver.OvsBridgeName = bridgeName

	bridgeExist, err := ovsDriver.IsBridgePresent(bridgeName)
	if err != nil {
		return nil, err
	}

	if !bridgeExist {
		return nil, fmt.Errorf("failed to find bridge %s", bridgeName)
	}

	// Return the new OVS driver
	return ovsDriver, nil
}

// Wrapper for ovsDB transaction
func (ovsd *OvsDriver) ovsdbTransact(ops []ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	// Perform OVSDB transaction
	reply, _ := ovsd.OvsClient.Transact(context.Background(), ops...)

	if len(reply) < len(ops) {
		return nil, errors.New("OVS transaction failed. Less replies than operations")
	}

	// Parse reply and look for errors
	for _, o := range reply {
		if o.Error != "" {
			return nil, errors.New("OVS Transaction failed err " + o.Error + " Details: " + o.Details)
		}
	}

	// Return success
	return reply, nil
}

// getUsedOFPorts returns the set of OpenFlow port numbers currently in use
// across ALL bridges. It selects every Interface row in a single atomic
// OVSDB read (1 round-trip). The result is a superset when there are
// multiple bridges, but this is safer, resolveOFPort better skips a few extra
// slots rather than risking a collision. Also most of the other ports will be
// in the lower range i.e below 32768.
func (ovsd *OvsBridgeDriver) getUsedOFPorts() (map[uint]bool, error) {
	selectAll := ovsdb.Operation{
		Op:      "select",
		Table:   "Interface",
		Where:   []ovsdb.Condition{},
		Columns: []string{"ofport", "ofport_request"},
	}
	results, err := ovsd.ovsdbTransact([]ovsdb.Operation{selectAll})
	if err != nil {
		return nil, fmt.Errorf("select all interfaces: %w", err)
	}

	used := make(map[uint]bool)
	for _, row := range results[0].Rows {
		// Prefer the actual ofport assigned by OVS. If it is -1 the
		// interface is still being configured, so fall back to
		// ofport_request to reserve the slot it has already claimed.
		if v, ok := row["ofport"]; ok {
			if p, ok := v.(float64); ok && p > 0 {
				used[uint(p)] = true
				continue
			}
		}
		if v, ok := row["ofport_request"]; ok {
			if p, ok := v.(float64); ok && p > 0 {
				used[uint(p)] = true
			}
		}
	}
	log.Printf("getUsedOFPorts: %d ports in use", len(used))
	return used, nil
}

// ofportWaitOp returns an OVSDB "wait" operation that asserts no Interface
// row currently has the given ofport_request value (RFC 7047 §5.2.7).
//
// Ideally we would use Until:"==" with empty Rows (assert result is the
// empty set), but libovsdb tags the Rows field with omitempty, so the JSON
// encoder drops it — ovsdb-server then rejects the op with "Required
// 'rows' member is missing."
//
// Instead we use Until:"!=" with a single row matching the query. When no
// row has ofport_request == N the result is empty, which differs from the
// provided row, so the wait succeeds. When exactly one row has the value
// the result equals Rows, so "!=" is false and the transaction aborts with
// "timed out", signalling the caller (k8s CNI) to retry.
// Timeout 0 makes it an instant assertion.
func ofportWaitOp(ofportRequest uint) ovsdb.Operation {
	timeout := 0
	return ovsdb.Operation{
		Op:      "wait",
		Table:   "Interface",
		Where:   []ovsdb.Condition{ovsdb.NewCondition("ofport_request", ovsdb.ConditionEqual, ofportRequest)},
		Columns: []string{"ofport_request"},
		Until:   "!=",
		Rows:    []ovsdb.Row{{"ofport_request": ofportRequest}},
		Timeout: &timeout,
	}
}

// **************** OVS driver API ********************

// CreatePort Create an internal port in OVS
func (ovsd *OvsBridgeDriver) CreatePort(intfName, contNetnsPath, contIfaceName, ovnPortName, dpfId string, contIface *current.Interface, ofportRequest uint, vlanTag uint, trunks []uint, portType string, mtu int, intfType string, contPodUid string) error {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, intfName)
	row, _ := ovsd.findByCondition("Port", condition, nil)
	if row != nil { // Port is already created so skip the creation
		// TODO check if it is on the right bridge and create by the CNI
		return nil
	}

	if ofportRequest == 0 {
		usedPorts, err := ovsd.getUsedOFPorts()
		if err != nil {
			return fmt.Errorf("query used ofports: %w", err)
		}
		ofportRequest = resolveOFPort(intfName, usedPorts)
	}
	log.Printf("CreatePort: ofportRequest=%d", ofportRequest)

	intfUUID, intfOp, err := createInterfaceOperation(intfName, ofportRequest, ovnPortName, dpfId, contIface.Mac, intfType, mtu, "")
	if err != nil {
		return err
	}
	portUUID, portOp, err := createPortOperation(intfName, contNetnsPath, contIface.Name, vlanTag, trunks, portType, intfUUID, contPodUid)
	if err != nil {
		return err
	}
	mutateOp := attachPortOperation(portUUID, ovsd.OvsBridgeName)

	var operations []ovsdb.Operation
	if ofportRequest != 0 {
		operations = []ovsdb.Operation{ofportWaitOp(ofportRequest), *intfOp, *portOp, *mutateOp}
	} else {
		operations = []ovsdb.Operation{*intfOp, *portOp, *mutateOp}
	}
	_, err = ovsd.ovsdbTransact(operations)
	return err
}

func runOVSVsctl(args ...string) error {
	ovsVsctlPath, err := exec.LookPath("ovs-vsctl")
	if err != nil {
		return err
	}
	cmd := exec.Command(ovsVsctlPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("error running ovs-vsctl command with args %v failed: err=%w stderr=%s", args, err, stderr.String())
	}
	return nil
}

func (ovsd *OvsBridgeDriver) DelPort(portName string) error {
	return runOVSVsctl("-t", "7", "--if-exist", "del-port", portName)
}

func (ovsd *OvsBridgeDriver) CleanupStaleHbn(contIfaceName string) error {
	searchMap := map[string]string{
		"hbn_netdev": contIfaceName,
	}
	ovsmap, err := ovsdb.NewOvsMap(searchMap)
	if err != nil {
		return err
	}

	condition := ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, ovsmap)
	colums := []string{"name", "external_ids"}
	results, err := ovsd.arrayByCondition("Interface", condition, colums)
	if err != nil {
		return err
	}

	for i := 0; i < len(results); i++ {
		element := results[i]
		if err = ovsd.DelPort(element["name"].(string)); err != nil {
			return err
		}

		// RM: ##4690834, we are skipping deleting sf reps from br-sfc and expecting
		// CleanupStaleSfc to do it.
		//externalIDs, err := getExternalIDs(element)
		//if err != nil {
		//	return fmt.Errorf("get external ids: %v", err)
		//}
		//if v, ok := externalIDs["hbn_rep_ofport"]; ok {
		//	if err = ovsd.DelPort(v); err != nil {
		//		return err
		//	}
		//}
	}

	return nil
}

func (ovsd *OvsBridgeDriver) CleanupStaleSfc(contIfaceName string) error {
	searchMap := map[string]string{
		"dpf-id": contIfaceName,
	}
	ovsmap, err := ovsdb.NewOvsMap(searchMap)
	if err != nil {
		return err
	}

	condition := ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, ovsmap)
	colums := []string{"name", "external_ids"}
	results, err := ovsd.arrayByCondition("Interface", condition, colums)
	if err != nil {
		return err
	}

	for i := 0; i < len(results); i++ {
		element := results[i]
		if err = ovsd.DelPort(element["name"].(string)); err != nil {
			return err
		}
	}

	return nil
}

func (ovsd *OvsBridgeDriver) createPeer(portOnBrA, portOnBrB, intfName, contIfaceName, bridge string) error {
	found, err := ovsd.IsPortPresent(portOnBrA)
	if err != nil {
		return err
	}

	if found {
		log.Printf("CmdAdd port already managed by OVS trying to remove it: %v", portOnBrA)
		if err := ovsd.DelPort(portOnBrA); err != nil {
			return err
		}
	}

	usedPorts, err := ovsd.getUsedOFPorts()
	if err != nil {
		return fmt.Errorf("query used ofports: %w", err)
	}
	ofportRequest := resolveOFPort(portOnBrA, usedPorts)
	log.Printf("createPeer: ofportRequest=%d", ofportRequest)

	intfUUID, intfOp, err := createInterfaceOperation(portOnBrA, ofportRequest, intfName, contIfaceName, "", "patch", 0, portOnBrB)
	if err != nil {
		return err
	}
	portUUID, portOp, err := createPortOperation(portOnBrA, "", "", 0, make([]uint, 0), "trunk", intfUUID, "")
	if err != nil {
		return err
	}
	mutateOp := attachPortOperation(portUUID, bridge)

	var operations []ovsdb.Operation
	if ofportRequest != 0 {
		operations = []ovsdb.Operation{ofportWaitOp(ofportRequest), *intfOp, *portOp, *mutateOp}
	} else {
		operations = []ovsdb.Operation{*intfOp, *portOp, *mutateOp}
	}
	_, err = ovsd.ovsdbTransact(operations)
	return err
}

// CreatePatch Create an patch between two bridges based on port name port in OVS
func (ovsd *OvsBridgeDriver) CreatePatch(intfName, contIfaceName, brA, brB string) error {
	portOnBrA := fmt.Sprintf("p%s%s", intfName, strings.ReplaceAll(brA, "-", ""))
	portOnBrB := fmt.Sprintf("p%s%s", intfName, strings.ReplaceAll(brB, "-", ""))

	err := ovsd.createPeer(portOnBrA, portOnBrB, intfName, contIfaceName, brA)
	if err != nil {
		return err
	}

	err = ovsd.createPeer(portOnBrB, portOnBrA, intfName, contIfaceName, brB)
	if err != nil {
		return err
	}

	return nil
}

// DeletePort Delete a port from OVS
func (ovsd *OvsBridgeDriver) DeletePort(intfName string) error {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, intfName)
	row, err := ovsd.findByCondition("Port", condition, nil)
	if err != nil {
		return err
	}

	externalIDs, err := getExternalIDs(row)
	if err != nil {
		return fmt.Errorf("get external ids: %v", err)
	}
	if externalIDs["owner"] != ovsPortOwner {
		return fmt.Errorf("port not created by ovs-cni")
	}

	// We make a select transaction using the interface name
	// Then get the Port UUID from it
	portUUID := row["_uuid"].(ovsdb.UUID)

	intfOp := deleteInterfaceOperation(intfName)

	portOp := deletePortOperation(intfName)

	mutateOp := detachPortOperation(portUUID, ovsd.OvsBridgeName)

	// Perform OVS transaction
	operations := []ovsdb.Operation{*intfOp, *portOp, *mutateOp}

	_, err = ovsd.ovsdbTransact(operations)
	return err
}

func getExternalIDs(row map[string]interface{}) (map[string]string, error) {
	rowVal, ok := row["external_ids"]
	if !ok {
		return nil, fmt.Errorf("row does not contain external_ids")
	}

	rowValOvsMap, ok := rowVal.(ovsdb.OvsMap)
	if !ok {
		return nil, fmt.Errorf("not a OvsMap: %T: %v", rowVal, rowVal)
	}

	extIDs := make(map[string]string, len(rowValOvsMap.GoMap))
	for key, value := range rowValOvsMap.GoMap {
		extIDs[key.(string)] = value.(string)
	}
	return extIDs, nil
}

// BridgeList returns available ovs bridge names
func (ovsd *OvsDriver) BridgeList() ([]string, error) {
	selectOp := []ovsdb.Operation{{
		Op:      "select",
		Table:   "Bridge",
		Columns: []string{"name"},
	}}

	transactionResult, err := ovsd.ovsdbTransact(selectOp)
	if err != nil {
		return nil, err
	}

	if len(transactionResult) != 1 {
		return nil, fmt.Errorf("unknow error")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return nil, fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	bridges := []string{}
	for _, bridge := range operationResult.Rows {
		bridges = append(bridges, fmt.Sprintf("%v", bridge["name"]))
	}

	return bridges, nil
}

// GetOFPortOpState retrieves link state of the OF port
func (ovsd *OvsDriver) GetOFPortOpState(portName string) (string, error) {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, portName)
	selectOp := []ovsdb.Operation{{
		Op:      "select",
		Table:   "Interface",
		Columns: []string{"link_state"},
		Where:   []ovsdb.Condition{condition},
	}}

	transactionResult, err := ovsd.ovsdbTransact(selectOp)
	if err != nil {
		return "", err
	}

	if len(transactionResult) != 1 {
		return "", fmt.Errorf("unknown error")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return "", fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	if len(operationResult.Rows) != 1 {
		return "", nil
	}

	return fmt.Sprintf("%v", operationResult.Rows[0]["link_state"]), nil
}

// GetOFPortVlanState retrieves port vlan state of the OF port
func (ovsd *OvsDriver) GetOFPortVlanState(portName string) (string, *uint, []uint, error) {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, portName)
	selectOp := []ovsdb.Operation{{
		Op:      "select",
		Table:   "Port",
		Columns: []string{"vlan_mode", "tag", "trunks"},
		Where:   []ovsdb.Condition{condition},
	}}
	var vlanMode = ""
	var tag *uint = nil
	var trunks []uint

	transactionResult, err := ovsd.ovsdbTransact(selectOp)
	if err != nil {
		return vlanMode, tag, trunks, err
	}

	if len(transactionResult) != 1 {
		return vlanMode, tag, trunks, fmt.Errorf("transactionResult length is not one")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return vlanMode, tag, trunks, fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	if len(operationResult.Rows) != 1 {
		return vlanMode, tag, trunks, fmt.Errorf("operationResult.Rows length is not one")
	}

	vlanModeCol := operationResult.Rows[0]["vlan_mode"]
	switch vlanModeCol.(type) {
	case string:
		vlanMode = operationResult.Rows[0]["vlan_mode"].(string)
	}

	tagCol := operationResult.Rows[0]["tag"]
	switch tagCol.(type) {
	case float64:
		tagValue := uint(operationResult.Rows[0]["tag"].(float64))
		tag = &tagValue
	}

	trunksCol := operationResult.Rows[0]["trunks"].(ovsdb.OvsSet).GoSet
	if len(trunksCol) > 0 {
		for i := range trunksCol {
			trunks = append(trunks, uint(trunksCol[i].(float64)))
		}
	}

	return vlanMode, tag, trunks, nil
}

// IsBridgePresent Checks if the bridge entry already exists
func (ovsd *OvsDriver) IsBridgePresent(bridgeName string) (bool, error) {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, bridgeName)
	selectOp := []ovsdb.Operation{{
		Op:      "select",
		Table:   "Bridge",
		Where:   []ovsdb.Condition{condition},
		Columns: []string{"name"},
	}}

	transactionResult, err := ovsd.ovsdbTransact(selectOp)
	if err != nil {
		return false, err
	}

	if len(transactionResult) != 1 {
		return false, fmt.Errorf("unknow error")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return false, fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	if len(operationResult.Rows) != 1 {
		return false, nil
	}

	return true, nil
}

// IsPortPresent Checks if the port entry already exists
func (ovsd *OvsDriver) IsPortPresent(port string) (bool, error) {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, port)
	selectOp := []ovsdb.Operation{{
		Op:      "select",
		Table:   "Port",
		Where:   []ovsdb.Condition{condition},
		Columns: []string{"name"},
	}}

	transactionResult, err := ovsd.ovsdbTransact(selectOp)
	if err != nil {
		return false, err
	}

	if len(transactionResult) != 1 {
		return false, fmt.Errorf("unknow error")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return false, fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	if len(operationResult.Rows) != 1 {
		return false, nil
	}

	return true, nil
}

// FindBridgeByInterface returns name of the bridge that contains provided interface
func (ovsd *OvsDriver) FindBridgeByInterface(ifaceName string) (string, error) {
	iface, err := ovsd.findByCondition("Interface",
		ovsdb.NewCondition("name", ovsdb.ConditionEqual, ifaceName),
		[]string{"name", "_uuid"})
	if err != nil {
		return "", fmt.Errorf("failed to find interface %s: %v", ifaceName, err)
	}
	port, err := ovsd.findByCondition("Port",
		ovsdb.NewCondition("interfaces", ovsdb.ConditionIncludes, iface["_uuid"]),
		[]string{"name", "_uuid"})
	if err != nil {
		return "", fmt.Errorf("failed to find port %s: %v", ifaceName, err)
	}
	bridge, err := ovsd.findByCondition("Bridge",
		ovsdb.NewCondition("ports", ovsdb.ConditionIncludes, port["_uuid"]),
		[]string{"name"})
	if err != nil {
		return "", fmt.Errorf("failed to find bridge for %s: %v", ifaceName, err)
	}
	return fmt.Sprintf("%v", bridge["name"]), nil
}

// GetOvsPortForContIface Return ovs port name for an container interface
func (ovsd *OvsDriver) GetOvsPortForContIface(contIface, contNetnsPath string) (string, bool, error) {
	searchMap := map[string]string{
		"contNetns": contNetnsPath,
		"contIface": contIface,
		"owner":     ovsPortOwner,
	}
	ovsmap, err := ovsdb.NewOvsMap(searchMap)
	if err != nil {
		return "", false, err
	}

	condition := ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, ovsmap)
	colums := []string{"name", "external_ids"}
	port, err := ovsd.findByCondition("Port", condition, colums)
	if err != nil {
		if errors.Is(err, errObjectNotFound) {
			return "", false, nil
		}
		return "", false, err
	}

	return fmt.Sprintf("%v", port["name"]), true, nil
}

func (ovsd *OvsDriver) DoesContIfaceWithDpfIdExists(dpfId string) (bool, error) {
	searchMap := map[string]string{
		"dpf-id": dpfId,
	}
	ovsmap, err := ovsdb.NewOvsMap(searchMap)
	if err != nil {
		if errors.Is(err, errObjectNotFound) {
			return false, nil
		}
		return false, err
	}

	condition := ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, ovsmap)
	colums := []string{"name", "external_ids"}
	_, err = ovsd.findByCondition("Interface", condition, colums)
	if err != nil {
		return false, err
	}
	return true, nil
}

// FindInterfacesWithError returns the interfaces which are in error state
func (ovsd *OvsDriver) FindInterfacesWithError() ([]string, error) {
	selectOp := ovsdb.Operation{
		Op:      "select",
		Columns: []string{"name", "error"},
		Table:   "Interface",
	}
	transactionResult, err := ovsd.ovsdbTransact([]ovsdb.Operation{selectOp})
	if err != nil {
		return nil, err
	}
	if len(transactionResult) != 1 {
		return nil, fmt.Errorf("no transaction result")
	}
	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return nil, errors.New(operationResult.Error)
	}

	var names []string
	for _, row := range operationResult.Rows {
		if !hasError(row) {
			continue
		}
		names = append(names, fmt.Sprintf("%v", row["name"]))
	}
	if len(names) > 0 {
		log.Printf("found %d interfaces with error", len(names))
	}
	return names, nil
}

func hasError(row map[string]interface{}) bool {
	v := row["error"]
	switch x := v.(type) {
	case string:
		return x != ""
	default:
		return false
	}
}

// ************************ Notification handler for OVS DB changes ****************

// Update yet to be implemented
func (ovsd *OvsDriver) Update(context interface{}, tableUpdates ovsdb.TableUpdates) {
}

// Disconnected yet to be implemented
func (ovsd *OvsDriver) Disconnected(ovsClient client.Client) {
}

// Locked yet to be implemented
func (ovsd *OvsDriver) Locked([]interface{}) {
}

// Stolen yet to be implemented
func (ovsd *OvsDriver) Stolen([]interface{}) {
}

// Echo yet to be implemented
func (ovsd *OvsDriver) Echo([]interface{}) {
}

// ************************ Helper functions ********************
func (ovsd *OvsDriver) findByCondition(table string, condition ovsdb.Condition, columns []string) (map[string]interface{}, error) {
	selectOp := ovsdb.Operation{
		Op:    "select",
		Table: table,
		Where: []ovsdb.Condition{condition},
	}

	if columns != nil {
		selectOp.Columns = columns
	}

	transactionResult, err := ovsd.ovsdbTransact([]ovsdb.Operation{selectOp})
	if err != nil {
		return nil, err
	}

	if len(transactionResult) != 1 {
		return nil, fmt.Errorf("unknown error")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return nil, fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	if len(operationResult.Rows) != 1 {
		return nil, fmt.Errorf("%w in the table %s", errObjectNotFound, table)
	}

	return operationResult.Rows[0], nil
}

func (ovsd *OvsDriver) arrayByCondition(table string, condition ovsdb.Condition, columns []string) ([]ovsdb.Row, error) {
	selectOp := ovsdb.Operation{
		Op:    "select",
		Table: table,
		Where: []ovsdb.Condition{condition},
	}

	if columns != nil {
		selectOp.Columns = columns
	}

	transactionResult, err := ovsd.ovsdbTransact([]ovsdb.Operation{selectOp})
	if err != nil {
		return nil, err
	}

	if len(transactionResult) != 1 {
		return nil, fmt.Errorf("unknown error")
	}

	operationResult := transactionResult[0]
	if operationResult.Error != "" {
		return nil, fmt.Errorf("%s - %s", operationResult.Error, operationResult.Details)
	}

	return operationResult.Rows, nil
}

func createInterfaceOperation(intfName string, ofportRequest uint, ovnPortName string, dpfId string, contIfaceMac string, intfType string, mtu int, patch string) (ovsdb.UUID, *ovsdb.Operation, error) {
	intfUUIDStr := fmt.Sprintf("Intf%s", intfName)
	intfUUID := ovsdb.UUID{GoUUID: intfUUIDStr}

	intf := make(map[string]interface{})
	intf["name"] = intfName

	// Configure interface type if not nil
	if intfType != "" {
		intf["type"] = intfType
	}
	if mtu > 100 {
		intf["mtu_request"] = mtu
	}

	// Configure interface ID for ovn
	if ovnPortName != "" || dpfId != "" || patch != "" {
		var err error
		var oMap ovsdb.OvsMap
		if patch != "" && strings.HasSuffix(intfName, "brhbn") {
			// This metadata is required by the DAL layer inside the nl2doca app
			// This should be set only on a single interface, on the patch port which resides on the br-hbn bridge
			oMap, err = ovsdb.NewOvsMap(map[string]string{"hbn_rep_ofport": ovnPortName, "hbn_netdev": dpfId})
		} else {
			oMap, err = ovsdb.NewOvsMap(map[string]string{"iface-id": ovnPortName, "dpf-id": dpfId, "iface-mac": contIfaceMac})
		}
		if err != nil {
			return ovsdb.UUID{}, nil, err
		}
		intf["external_ids"] = oMap
	}

	if patch != "" {
		oMap, err := ovsdb.NewOvsMap(map[string]string{"peer": patch})
		if err != nil {
			return ovsdb.UUID{}, nil, err
		}
		intf["options"] = oMap
	}

	// Callers are expected to always supply a resolved, collision-free
	// ofport_request (via resolveOFPort). In case we run out of all slots,
	// ofPortRequest is 0, so that createInterfaceOperation omits ofport_request entirely.
	if ofportRequest != 0 {
		intf["ofport_request"] = ofportRequest
	}

	// Add an entry in Interface table
	intfOp := ovsdb.Operation{
		Op:       "insert",
		Table:    "Interface",
		Row:      intf,
		UUIDName: intfUUIDStr,
	}

	return intfUUID, &intfOp, nil
}

func createPortOperation(intfName string, contNetnsPath string, contIfaceName string, vlanTag uint, trunks []uint, portType string, intfUUID ovsdb.UUID, contPodUid string) (ovsdb.UUID, *ovsdb.Operation, error) {
	portUUIDStr := intfName
	portUUID := ovsdb.UUID{GoUUID: portUUIDStr}

	port := make(map[string]interface{})
	port["name"] = intfName

	port["vlan_mode"] = portType
	var err error
	if portType == "access" {
		port["tag"] = vlanTag
	} else if len(trunks) > 0 {
		port["trunks"], err = ovsdb.NewOvsSet(trunks)
		if err != nil {
			return ovsdb.UUID{}, nil, err
		}
	}

	port["interfaces"], err = ovsdb.NewOvsSet(intfUUID)
	if err != nil {
		return ovsdb.UUID{}, nil, err
	}

	oMap, err := ovsdb.NewOvsMap(map[string]string{
		"contPodUid": contPodUid,
		"contNetns":  contNetnsPath,
		"contIface":  contIfaceName,
		"owner":      ovsPortOwner,
	})
	if err != nil {
		return ovsdb.UUID{}, nil, err
	}
	port["external_ids"] = oMap

	// Add an entry in Port table
	portOp := ovsdb.Operation{
		Op:       "insert",
		Table:    "Port",
		Row:      port,
		UUIDName: portUUIDStr,
	}

	return portUUID, &portOp, nil
}

func attachPortOperation(portUUID ovsdb.UUID, bridgeName string) *ovsdb.Operation {
	// mutate the Ports column of the row in the Bridge table
	mutateSet, _ := ovsdb.NewOvsSet(portUUID)
	mutation := ovsdb.NewMutation("ports", ovsdb.MutateOperationInsert, mutateSet)
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, bridgeName)
	mutateOp := ovsdb.Operation{
		Op:        "mutate",
		Table:     "Bridge",
		Mutations: []ovsdb.Mutation{*mutation},
		Where:     []ovsdb.Condition{condition},
	}

	return &mutateOp
}

func deleteInterfaceOperation(intfName string) *ovsdb.Operation {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, intfName)
	intfOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Interface",
		Where: []ovsdb.Condition{condition},
	}

	return &intfOp
}

func deletePortOperation(intfName string) *ovsdb.Operation {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, intfName)
	portOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Port",
		Where: []ovsdb.Condition{condition},
	}

	return &portOp
}

func detachPortOperation(portUUID ovsdb.UUID, bridgeName string) *ovsdb.Operation {
	// mutate the Ports column of the row in the Bridge table
	mutateSet, _ := ovsdb.NewOvsSet(portUUID)
	mutation := ovsdb.NewMutation("ports", ovsdb.MutateOperationDelete, mutateSet)
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, bridgeName)
	mutateOp := ovsdb.Operation{
		Op:        "mutate",
		Table:     "Bridge",
		Mutations: []ovsdb.Mutation{*mutation},
		Where:     []ovsdb.Condition{condition},
	}

	return &mutateOp
}

