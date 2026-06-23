// Modifications copyright (C) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
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

// DPF-OVS-CNI helpers around ovsutils.API: ofport allocation, external_ids
// schema, patch-port wiring, and raw-libovsdb queries ovsutils doesn't expose.

package plugin

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"

	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"
)

const (
	ovsPortOwner = "ovs-cni.network.kubevirt.io"
	sfcBridge    = "br-sfc"
	hbnBridge    = "br-hbn"
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

var errObjectNotFound = errors.New("object not found")

// ensureBridge returns an error if bridgeName is missing in OVS.
func ensureBridge(ctx context.Context, api ovsutils.API, bridgeName string) error {
	err := api.Get(ctx, &ovsmodel.Bridge{Name: bridgeName})
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return fmt.Errorf("failed to find bridge %s", bridgeName)
		}
		return fmt.Errorf("get bridge %s: %w", bridgeName, err)
	}
	return nil
}

// hashToOFPort maps an interface name to an ofport in [minOFPort, maxOFPort].
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
	// All slots occupied: return 0 so the caller omits ofport_request and
	// lets OVS auto-assign. Should not happen in practice.
	return 0
}

// getUsedOFPorts returns ofports in use across ALL bridges in one OVSDB read.
// Superset across bridges is fine, resolveOFPort just skips a few extra slots.
func getUsedOFPorts(ctx context.Context, api ovsutils.API) (map[uint]bool, error) {
	selectAll := ovsdb.Operation{
		Op:      "select",
		Table:   "Interface",
		Where:   []ovsdb.Condition{},
		Columns: []string{"ofport", "ofport_request"},
	}
	results, err := ovsdbTransact(ctx, api, []ovsdb.Operation{selectAll})
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

// createPort creates an OVS port on bridgeName.
// Composes ovsutils.AddPort with DPF-specific external_ids schema and
// collision-free ofport_request selection.
func createPort(ctx context.Context, api ovsutils.API, bridgeName, intfName, contNetnsPath, ovnPortName, dpfId string, contIface *current.Interface, ofportRequest uint, vlanTag uint, trunks []uint, portType string, mtu int, intfType string, contPodUid string) error {
	if ofportRequest == 0 {
		usedPorts, err := getUsedOFPorts(ctx, api)
		if err != nil {
			return fmt.Errorf("query used ofports: %w", err)
		}
		ofportRequest = resolveOFPort(intfName, usedPorts)
	}
	log.Printf("createPort: ofportRequest=%d", ofportRequest)

	var ifaceExtIDs map[string]string
	if ovnPortName != "" || dpfId != "" {
		ifaceExtIDs = map[string]string{
			"iface-id":        ovnPortName,
			ovsutils.DPFIDKey: dpfId,
			"iface-mac":       contIface.Mac,
		}
	}

	portExtIDs := map[string]string{
		"contPodUid": contPodUid,
		"contNetns":  contNetnsPath,
		"contIface":  contIface.Name,
		"owner":      ovsPortOwner,
	}

	// DPF CRDs enforce MTU in [1280, 9216] (IPv6 floor), anything below is treated as unset.
	var mtuPtr *int
	if mtu >= 1280 {
		m := mtu
		mtuPtr = &m
	}

	var ofportReqPtr *int
	if ofportRequest != 0 {
		o := int(ofportRequest)
		ofportReqPtr = &o
	}

	vlanMode := ovsmodel.PortVLANMode(portType)
	var tagPtr *int
	var trunksInts []int
	if portType == "access" {
		t := int(vlanTag)
		tagPtr = &t
	} else if len(trunks) > 0 {
		trunksInts = make([]int, 0, len(trunks))
		for _, t := range trunks {
			trunksInts = append(trunksInts, int(t))
		}
	}

	return api.AddPort(ctx, ovsutils.PortConfig{
		Name:                 intfName,
		BridgeName:           bridgeName,
		InterfaceType:        intfType,
		MTU:                  mtuPtr,
		InterfaceExternalIDs: ifaceExtIDs,
		PortExternalIDs:      portExtIDs,
		OFPortRequest:        ofportReqPtr,
		VLANMode:             &vlanMode,
		Tag:                  tagPtr,
		Trunks:               trunksInts,
		WaitForOFPortFree:    ofportReqPtr != nil,
	})
}

// cleanupStaleHbn removes ports tagged hbn_netdev=contIfaceName from br-hbn.
// br-sfc cleanup belongs to cleanupStaleSfc (upstream RM #4690834).
func cleanupStaleHbn(ctx context.Context, api ovsutils.API, contIfaceName string) error {
	results, err := arrayByCondition(
		ctx, api,
		"Interface",
		ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, mustOvsMap(map[string]string{"hbn_netdev": contIfaceName})),
		[]string{"name", "external_ids"},
	)
	if err != nil {
		return err
	}

	for _, row := range results {
		if err := api.DelPort(ctx, hbnBridge, row["name"].(string)); err != nil {
			return err
		}
	}
	return nil
}

// cleanupStaleSfc removes ports tagged dpf-id=contIfaceName from br-sfc.
func cleanupStaleSfc(ctx context.Context, api ovsutils.API, contIfaceName string) error {
	results, err := arrayByCondition(
		ctx, api,
		"Interface",
		ovsdb.NewCondition("external_ids", ovsdb.ConditionIncludes, mustOvsMap(map[string]string{ovsutils.DPFIDKey: contIfaceName})),
		[]string{"name", "external_ids"},
	)
	if err != nil {
		return err
	}

	for _, row := range results {
		if err := api.DelPort(ctx, sfcBridge, row["name"].(string)); err != nil {
			return err
		}
	}
	return nil
}

// createPatch creates a pair of patch ports linking brA and brB.
// The br-hbn-side patch carries nl2doca DAL metadata in interface external_ids.
// Each port is recreated rather than left as-is, so external_ids stay in sync
// when the same host iface is reused by a new container.
func createPatch(ctx context.Context, api ovsutils.API, intfName, contIfaceName, brA, brB string) error {
	portOnBrA := patchPortName(intfName, brA)
	portOnBrB := patchPortName(intfName, brB)

	if err := addPatchPort(ctx, api, brA, portOnBrA, portOnBrB, intfName, contIfaceName); err != nil {
		return err
	}
	return addPatchPort(ctx, api, brB, portOnBrB, portOnBrA, intfName, contIfaceName)
}

func patchPortName(intfName, bridge string) string {
	return fmt.Sprintf("p%s%s", intfName, strings.ReplaceAll(bridge, "-", ""))
}

func addPatchPort(ctx context.Context, api ovsutils.API, bridgeName, portName, peerName, hostIfaceName, contIfaceName string) error {
	if err := api.DelPort(ctx, bridgeName, portName); err != nil {
		return fmt.Errorf("delete stale patch port %s: %w", portName, err)
	}

	usedPorts, err := getUsedOFPorts(ctx, api)
	if err != nil {
		return fmt.Errorf("query used ofports: %w", err)
	}
	var ofportPtr *int
	if ofport := resolveOFPort(portName, usedPorts); ofport != 0 {
		v := int(ofport)
		ofportPtr = &v
	}

	ifaceExtIDs := map[string]string{"iface-id": hostIfaceName, ovsutils.DPFIDKey: contIfaceName, "iface-mac": ""}
	if strings.HasSuffix(portName, "brhbn") {
		// Required by the nl2doca DAL, only on the br-hbn-side patch port.
		ifaceExtIDs = map[string]string{"hbn_rep_ofport": hostIfaceName, "hbn_netdev": contIfaceName}
	}

	vlanMode := ovsmodel.PortVLANMode("trunk")
	return api.AddPort(ctx, ovsutils.PortConfig{
		Name:                 portName,
		BridgeName:           bridgeName,
		InterfaceType:        "patch",
		InterfaceOptions:     map[string]string{"peer": peerName},
		InterfaceExternalIDs: ifaceExtIDs,
		PortExternalIDs:      map[string]string{"owner": ovsPortOwner},
		OFPortRequest:        ofportPtr,
		VLANMode:             &vlanMode,
		WaitForOFPortFree:    ofportPtr != nil,
	})
}

// deletePort removes intfName from bridgeName.
// Errors if the port is missing, not owned by ovs-cni, or on a different bridge.
func deletePort(ctx context.Context, api ovsutils.API, bridgeName, intfName string) error {
	port := &ovsmodel.Port{Name: intfName}
	if err := api.Get(ctx, port); err != nil {
		return fmt.Errorf("get port %s: %w", intfName, err)
	}

	if port.ExternalIDs["owner"] != ovsPortOwner {
		return fmt.Errorf("port not created by ovs-cni")
	}

	inBridge, err := api.IsIfaceInBr(ctx, bridgeName, intfName)
	if err != nil {
		return fmt.Errorf("check port %s on bridge %s: %w", intfName, bridgeName, err)
	}
	if !inBridge {
		return fmt.Errorf("port %s is not on bridge %s", intfName, bridgeName)
	}

	return api.DelPort(ctx, bridgeName, intfName)
}

// getOFPortOpState retrieves link state of the OF port.
func getOFPortOpState(ctx context.Context, api ovsutils.API, portName string) (string, error) {
	condition := ovsdb.NewCondition("name", ovsdb.ConditionEqual, portName)
	selectOp := []ovsdb.Operation{{
		Op:      "select",
		Table:   "Interface",
		Columns: []string{"link_state"},
		Where:   []ovsdb.Condition{condition},
	}}

	transactionResult, err := ovsdbTransact(ctx, api, selectOp)
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

// getOFPortVlanState retrieves port vlan state of the OF port.
func getOFPortVlanState(ctx context.Context, api ovsutils.API, portName string) (string, *uint, []uint, error) {
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

	transactionResult, err := ovsdbTransact(ctx, api, selectOp)
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

// findBridgeByInterface returns name of the bridge that contains provided interface.
func findBridgeByInterface(ctx context.Context, api ovsutils.API, ifaceName string) (string, error) {
	iface, err := findByCondition(ctx, api, "Interface",
		ovsdb.NewCondition("name", ovsdb.ConditionEqual, ifaceName),
		[]string{"name", "_uuid"})
	if err != nil {
		return "", fmt.Errorf("failed to find interface %s: %v", ifaceName, err)
	}
	port, err := findByCondition(ctx, api, "Port",
		ovsdb.NewCondition("interfaces", ovsdb.ConditionIncludes, iface["_uuid"]),
		[]string{"name", "_uuid"})
	if err != nil {
		return "", fmt.Errorf("failed to find port %s: %v", ifaceName, err)
	}
	bridge, err := findByCondition(ctx, api, "Bridge",
		ovsdb.NewCondition("ports", ovsdb.ConditionIncludes, port["_uuid"]),
		[]string{"name"})
	if err != nil {
		return "", fmt.Errorf("failed to find bridge for %s: %v", ifaceName, err)
	}
	return fmt.Sprintf("%v", bridge["name"]), nil
}

// getOvsPortForContIface returns the ovs port name for a container interface.
// Returns ("", false, nil) if no matching port exists.
func getOvsPortForContIface(ctx context.Context, api ovsutils.API, contIface, contNetnsPath string) (string, bool, error) {
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
	port, err := findByCondition(ctx, api, "Port", condition, []string{"name", "external_ids"})
	if err != nil {
		if errors.Is(err, errObjectNotFound) {
			return "", false, nil
		}
		return "", false, err
	}

	return fmt.Sprintf("%v", port["name"]), true, nil
}

// findInterfacesWithError returns the interfaces which are in error state.
func findInterfacesWithError(ctx context.Context, api ovsutils.API) ([]string, error) {
	selectOp := ovsdb.Operation{
		Op:      "select",
		Columns: []string{"name", "error"},
		Table:   "Interface",
	}
	transactionResult, err := ovsdbTransact(ctx, api, []ovsdb.Operation{selectOp})
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

// ************************ Internal helpers ********************

// ovsdbTransact runs ops and returns the per-operation results, surfacing
// per-op errors as a single combined error.
func ovsdbTransact(ctx context.Context, api ovsutils.API, ops []ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	reply, err := api.Transact(ctx, ops...)
	if err != nil {
		return nil, fmt.Errorf("OVS transact: %w", err)
	}

	if len(reply) < len(ops) {
		return nil, errors.New("OVS transaction failed. Less replies than operations")
	}

	for _, o := range reply {
		if o.Error != "" {
			return nil, errors.New("OVS Transaction failed err " + o.Error + " Details: " + o.Details)
		}
	}

	return reply, nil
}

func findByCondition(ctx context.Context, api ovsutils.API, table string, condition ovsdb.Condition, columns []string) (map[string]interface{}, error) {
	selectOp := ovsdb.Operation{
		Op:    "select",
		Table: table,
		Where: []ovsdb.Condition{condition},
	}

	if columns != nil {
		selectOp.Columns = columns
	}

	transactionResult, err := ovsdbTransact(ctx, api, []ovsdb.Operation{selectOp})
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

func arrayByCondition(ctx context.Context, api ovsutils.API, table string, condition ovsdb.Condition, columns []string) ([]ovsdb.Row, error) {
	selectOp := ovsdb.Operation{
		Op:    "select",
		Table: table,
		Where: []ovsdb.Condition{condition},
	}

	if columns != nil {
		selectOp.Columns = columns
	}

	transactionResult, err := ovsdbTransact(ctx, api, []ovsdb.Operation{selectOp})
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

// mustOvsMap panics on error, unreachable for the typed map[string]string callers.
func mustOvsMap(m map[string]string) ovsdb.OvsMap {
	res, err := ovsdb.NewOvsMap(m)
	if err != nil {
		panic(fmt.Errorf("ovsdb.NewOvsMap: %w", err))
	}
	return res
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
