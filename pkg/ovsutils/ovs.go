/*
Copyright 2024 NVIDIA

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

package ovsutils

import (
	"context"
	"errors"
	"fmt"

	"github.com/nvidia/doca-platform/pkg/ovsmodel"

	"github.com/google/uuid"
	ovsclient "github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

var (
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = ovsclient.ErrNotFound
)

//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs_conditional_api.go github.com/ovn-org/libovsdb/client ConditionalAPI
//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs.go . API
//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs_client.go github.com/ovn-org/libovsdb/client Client

type API interface {
	ovsclient.Client

	// Open vSwitch Commands
	SetOpenVSwitchExternalIDs(ctx context.Context, externalIDs map[string]string) error
	GetOpenVSwitchExternalIDs(ctx context.Context) (map[string]string, error)

	// Bridge Commands
	ValidateBridgeExists(ctx context.Context, bridgeName string) error
	GetBridgeNameByInterface(ctx context.Context, name string) (string, error)
	AddBridge(ctx context.Context, bridgeConfig BridgeConfig) error
	SetBridgeExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error
	ListBridgesWithExternalIDs(ctx context.Context, externalIDs map[string]string) ([]ovsmodel.Bridge, error)
	GetBridgeWithName(ctx context.Context, name string) (*ovsmodel.Bridge, error)
	DeleteBridge(ctx context.Context, name string) error

	// Port Commands
	AddPort(ctx context.Context, portConfig PortConfig) error
	DelPort(ctx context.Context, bridgeName, portName string, opt *DelPortOpt) error
	GetPortVLANState(ctx context.Context, name string) (PortVLANState, error)
	SetPortExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error

	// Interface Commands
	GetInterfaceLinkState(ctx context.Context, name string) (ovsmodel.InterfaceLinkState, error)
	ListInterfacesWithError(ctx context.Context) ([]string, error)
	SetIfaceExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error
	GetIfaceWithExternalIDs(ctx context.Context, externalIDs map[string]string) (*ovsmodel.Interface, error)
	SetIfaceOptions(ctx context.Context, name string, options map[string]string) error
	IsIfaceInBr(ctx context.Context, bridgeName, portName string) (bool, error)
	GetIfaceWithName(ctx context.Context, name string) (*ovsmodel.Interface, error)
}

var _ API = (*Client)(nil)

type Client struct {
	ovsclient.Client
}

// ValidateBridgeExists returns an error if bridgeName is missing in OVS.
func (c *Client) ValidateBridgeExists(ctx context.Context, bridgeName string) error {
	err := c.Get(ctx, &ovsmodel.Bridge{Name: bridgeName})
	if err != nil {
		if errors.Is(err, ovsclient.ErrNotFound) {
			return fmt.Errorf("failed to find bridge %s", bridgeName)
		}
		return fmt.Errorf("get bridge %s: %w", bridgeName, err)
	}
	return nil
}

// GetInterfaceLinkState returns the OVS link_state for an interface.
func (c *Client) GetInterfaceLinkState(ctx context.Context, name string) (ovsmodel.InterfaceLinkState, error) {
	iface := &ovsmodel.Interface{Name: name}
	if err := c.Get(ctx, iface); err != nil {
		if errors.Is(err, ovsclient.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get interface %s: %w", name, err)
	}
	if iface.LinkState == nil {
		return "", nil
	}
	return *iface.LinkState, nil
}

// GetPortVLANState returns the VLAN configuration for an OVS port.
func (c *Client) GetPortVLANState(ctx context.Context, name string) (PortVLANState, error) {
	port := &ovsmodel.Port{Name: name}
	if err := c.Get(ctx, port); err != nil {
		return PortVLANState{}, fmt.Errorf("failed to get port %s: %w", name, err)
	}
	return PortVLANState{
		Mode:   port.VLANMode,
		Tag:    port.Tag,
		Trunks: port.Trunks,
	}, nil
}

// GetBridgeNameByInterface returns the bridge name that contains an interface.
func (c *Client) GetBridgeNameByInterface(ctx context.Context, name string) (string, error) {
	iface := &ovsmodel.Interface{Name: name}
	if err := c.Get(ctx, iface); err != nil {
		return "", fmt.Errorf("failed to find interface %s: %w", name, err)
	}

	var ports []ovsmodel.Port
	port := &ovsmodel.Port{}
	err := c.WhereAll(
		port,
		model.Condition{
			Field:    &port.Interfaces,
			Function: ovsdb.ConditionIncludes,
			Value:    []string{iface.UUID},
		},
	).List(ctx, &ports)
	if err != nil {
		return "", fmt.Errorf("failed to find port %s: %w", name, err)
	}
	if len(ports) != 1 {
		return "", fmt.Errorf("failed to find port %s: expected 1 port, got %d", name, len(ports))
	}

	var bridges []ovsmodel.Bridge
	bridge := &ovsmodel.Bridge{}
	err = c.WhereAll(
		bridge,
		model.Condition{
			Field:    &bridge.Ports,
			Function: ovsdb.ConditionIncludes,
			Value:    []string{ports[0].UUID},
		},
	).List(ctx, &bridges)
	if err != nil {
		return "", fmt.Errorf("failed to find bridge for %s: %w", name, err)
	}
	if len(bridges) != 1 {
		return "", fmt.Errorf("failed to find bridge for %s: expected 1 bridge, got %d", name, len(bridges))
	}

	return bridges[0].Name, nil
}

// ListInterfacesWithError returns interface names with a non-empty OVS error.
func (c *Client) ListInterfacesWithError(ctx context.Context) ([]string, error) {
	var ifaces []ovsmodel.Interface
	if err := c.List(ctx, &ifaces); err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}

	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Error == nil || *iface.Error == "" {
			continue
		}
		names = append(names, iface.Name)
	}
	return names, nil
}

// BridgeConfig holds the configuration for creating an OVS bridge.
type BridgeConfig struct {
	// Name is the OVS bridge name.
	Name string
	// DatapathType is the OVS datapath type (e.g. "system", "netdev").
	DatapathType string
	// SkipCreateInternalPort, when true, skips creating the default internal port and interface.
	// By default (false), an internal port is created using InternalInterfaceName if set, otherwise the bridge name.
	SkipCreateInternalPort bool
	// InternalInterfaceName overrides the name of the internal port/interface (must be <= 15 characters, Linux IFNAMSIZ).
	// When nil, the bridge name is used. Only relevant when SkipCreateInternalPort is false.
	InternalInterfaceName *string
	// FailMode controls the bridge's behavior when no OpenFlow controller is connected.
	// "secure" drops all traffic, "standalone" falls back to normal L2 learning. Nil leaves it unset.
	FailMode *string
	// ExternalIDs are the external IDs to set for the bridge.
	ExternalIDs map[string]string
}

type PortConfig struct {
	Name             string
	BridgeName       string
	InterfaceType    string
	MTU              *int
	InterfaceOptions map[string]string

	// Optional fields below are written in the same transaction as port creation.
	InterfaceExternalIDs map[string]string
	PortExternalIDs      map[string]string
	OFPortRequest        *int
	VLANMode             *ovsmodel.PortVLANMode
	Tag                  *int
	Trunks               []int
	// WaitForOFPortFree, when set with OFPortRequest, aborts the transaction
	// if another Interface already requested the same ofport.
	WaitForOFPortFree bool
}

// PortVLANState holds the VLAN configuration read from an OVS port.
type PortVLANState struct {
	Mode   *ovsmodel.PortVLANMode
	Tag    *int
	Trunks []int
}

// DelPortOpt configures optional DelPort guards.
type DelPortOpt struct {
	Owner string
}

const (
	InterfaceTypeInternal      = "internal"
	maxInterfaceNameLen        = 15         // Linux IFNAMSIZ for netdev/port names
	DPFIDKey                   = "dpf-id"   // OVS external_ids key for DPF identifier
	DPFTypeKey                 = "dpf-type" // OVS external_ids key for DPF type
	DPFTypePhysical            = "physical" // Physical port type value
	errMsgFailedToCreatePort   = "failed to create port %s: %v"
	errMsgFailedToCreateBridge = "failed to create bridge %s: %v"
)

// ErrIfaceNotFound is returned by GetIfaceWithExternalIDs when no Interface matches.
var ErrIfaceNotFound = errors.New("failed to find matching interface")

// ofportWaitOp aborts the transaction if any Interface already has the given
// ofport_request. Uses Until:"!=" with a matching Row because libovsdb drops
// empty Rows via omitempty, which ovsdb-server would reject.
func ofportWaitOp(ofportRequest int) ovsdb.Operation {
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

// AddPort creates an interface, port, and attaches it to a bridge in a single
// transaction. No-op if the port already exists on the same bridge, errors if
// it exists on a different one. Optional PortConfig fields are written in the
// same transaction.
func (c *Client) AddPort(ctx context.Context, portConfig PortConfig) error {
	port := &ovsmodel.Port{
		Name: portConfig.Name,
		UUID: portConfig.Name,
	}

	err := c.Get(ctx, port)
	if err != nil && !errors.Is(err, ovsclient.ErrNotFound) {
		return err
	}

	// Port already exists
	if err == nil {
		isPortInBridge, err := c.IsIfaceInBr(ctx, portConfig.BridgeName, portConfig.Name)
		if err != nil {
			return fmt.Errorf("failed to check if port %s is in bridge %s: %v", portConfig.Name, portConfig.BridgeName, err)
		}
		// Does not add port because it already exists on this bridge
		if isPortInBridge {
			return nil
		}
		// Should not add port because it already exists on different bridge
		return fmt.Errorf("port %s already exists on a bridge other than %s", portConfig.Name, portConfig.BridgeName)
	}

	bridge := &ovsmodel.Bridge{
		Name: portConfig.BridgeName,
	}
	// SDK will not fail if we add a port to a non-existent bridge
	if err := c.Get(ctx, bridge); err != nil {
		return fmt.Errorf("failed to get bridge %s: %v", portConfig.BridgeName, err)
	}

	// maxMtuSize is the maximum MTU size that a DOCA enabled interface can take
	maxMtuSize := 9216
	ifaceUUI := "iface" + portConfig.Name
	iface := &ovsmodel.Interface{
		Name:          portConfig.Name,
		UUID:          ifaceUUI,
		Type:          portConfig.InterfaceType,
		MTURequest:    &maxMtuSize,
		Options:       portConfig.InterfaceOptions,
		ExternalIDs:   portConfig.InterfaceExternalIDs,
		OfportRequest: portConfig.OFPortRequest,
	}

	if portConfig.MTU != nil {
		iface.MTURequest = portConfig.MTU
	}

	var operations []ovsdb.Operation
	if portConfig.WaitForOFPortFree && portConfig.OFPortRequest != nil {
		operations = append(operations, ofportWaitOp(*portConfig.OFPortRequest))
	}

	ifaceOps, err := c.Create(iface)
	if err != nil {
		return fmt.Errorf("failed to create interface for port %s: %v", portConfig.Name, err)
	}
	operations = append(operations, ifaceOps...)

	port.Interfaces = []string{ifaceUUI}
	port.ExternalIDs = portConfig.PortExternalIDs
	port.VLANMode = portConfig.VLANMode
	port.Tag = portConfig.Tag
	port.Trunks = portConfig.Trunks

	pIns, err := c.Create(port)
	if err != nil {
		return fmt.Errorf(errMsgFailedToCreatePort, portConfig.Name, err)
	}

	operations = append(operations, pIns...)

	bIns, err := c.Where(bridge).Mutate(
		bridge,
		model.Mutation{
			Field:   &bridge.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{port.Name},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to add port %s to %s bridge: %v", portConfig.Name, portConfig.BridgeName, err)
	}

	operations = append(operations, bIns...)

	reply, err := c.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf(errMsgFailedToCreatePort, portConfig.Name, err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		return fmt.Errorf("port %s creation failed: %v", portConfig.Name, err)

	}

	return nil
}

// DelPort deletes a port from a bridge.
// When opt.Owner is set, ports with a different owner are rejected.
func (c *Client) DelPort(ctx context.Context, bridgeName, portName string, opt *DelPortOpt) error {
	// get port
	port := &ovsmodel.Port{
		Name: portName,
	}
	err := c.Get(ctx, port)

	// Make delete operations non fatal if the port does not exist
	// ovs-vsctl --if-exist
	if errors.Is(err, ovsclient.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get port %s: %v", portName, err)
	}

	if opt != nil && opt.Owner != "" && port.ExternalIDs["owner"] != opt.Owner {
		return fmt.Errorf("port %s owner %q does not match required owner %q", portName, port.ExternalIDs["owner"], opt.Owner)
	}

	// Make delete operations non fatal if the port does not exist on the requested bridge
	isPortInBridge, err := c.IsIfaceInBr(ctx, bridgeName, portName)
	if err != nil {
		return fmt.Errorf("failed to check if port %s is in bridge %s: %v", portName, bridgeName, err)
	}
	// Does not delete port because it exists on a different bridge
	if !isPortInBridge {
		return nil
	}

	delInterfaceOps, err := c.Where(&ovsmodel.Interface{
		Name: portName,
	}).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete interface %s: %v", portName, err)
	}

	delPortOps, err := c.Where(port).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete port %s: %v", portName, err)
	}

	operations := append(delInterfaceOps, delPortOps...)

	bridge := &ovsmodel.Bridge{Name: bridgeName}
	mutateOps, err := c.Where(bridge).Mutate(bridge, model.Mutation{
		Field:   &bridge.Ports,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{port.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to delete port %s reference: %v", portName, err)
	}

	operations = append(operations, mutateOps...)

	reply, err := c.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf("failed to exec delete port transaction %s: %v", portName, err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		return fmt.Errorf("failed to delete port %s: %v", portName, err)
	}

	return nil
}

func (c *Client) mutate(ctx context.Context, obj model.Model, mutations ...model.Mutation) error {
	if len(mutations) == 0 {
		return nil
	}
	operations, err := c.Where(obj).Mutate(obj, mutations...)
	if err != nil {
		return fmt.Errorf("failed to mutate: %v", err)
	}

	reply, err := c.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf("failed to update %v", err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		return fmt.Errorf("failed to update %v", err)
	}

	return nil
}

// SetIfaceExternalIDs sets the external IDs for an interface.
// It is a no-op if externalIDs is empty or if the interface already has the requested keys with the same values.
func (c *Client) SetIfaceExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error {
	iface := &ovsmodel.Interface{Name: name}
	if err := c.Get(ctx, iface); err != nil {
		return fmt.Errorf("failed to get interface %s: %w", name, err)
	}
	return c.mutateOVSMapField(ctx, iface, &iface.ExternalIDs, externalIDs)
}

func (c *Client) GetIfaceWithExternalIDs(ctx context.Context, externalIDs map[string]string) (*ovsmodel.Interface, error) {
	// Get doesn't work for ExternalIDs field
	var ifaces []ovsmodel.Interface
	iface := &ovsmodel.Interface{
		ExternalIDs: externalIDs,
	}
	err := c.WhereAll(
		iface,
		model.Condition{
			Field:    &iface.ExternalIDs,
			Function: ovsdb.ConditionIncludes,
			Value:    iface.ExternalIDs,
		},
	).List(ctx, &ifaces)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface with external_ids: %v", err)
	}

	if len(ifaces) == 0 {
		return nil, fmt.Errorf("%w with external_ids: %v", ErrIfaceNotFound, iface.ExternalIDs)
	}

	if len(ifaces) > 1 {
		return nil, fmt.Errorf("found multiple interfaces with external_ids: %v", iface.ExternalIDs)
	}

	return &ifaces[0], nil
}

// ListBridgesWithExternalIDs returns all bridges whose external_ids contain the given key-value pairs.
// For example, externalIDs map[string]string{"weave-owner-ref": "global"} returns all bridges
// with that external_id. Returns an error if externalIDs is empty (to avoid accidentally listing all bridges).
func (c *Client) ListBridgesWithExternalIDs(ctx context.Context, externalIDs map[string]string) ([]ovsmodel.Bridge, error) {
	if len(externalIDs) == 0 {
		return nil, fmt.Errorf("externalIDs cannot be empty")
	}

	var bridges []ovsmodel.Bridge
	br := &ovsmodel.Bridge{
		ExternalIDs: externalIDs,
	}
	err := c.WhereAll(
		br,
		model.Condition{
			Field:    &br.ExternalIDs,
			Function: ovsdb.ConditionIncludes,
			Value:    br.ExternalIDs,
		},
	).List(ctx, &bridges)
	if err != nil {
		return nil, fmt.Errorf("failed to list bridges with external_ids: %w", err)
	}

	return bridges, nil
}

// SetIfaceOptions sets keys on the interface options map (existing keys get new values).
// No-op if options is empty or values already match.
func (c *Client) SetIfaceOptions(ctx context.Context, name string, options map[string]string) error {
	iface := &ovsmodel.Interface{Name: name}
	if err := c.Get(ctx, iface); err != nil {
		return fmt.Errorf("failed to get interface %s: %w", name, err)
	}
	return c.mutateOVSMapField(ctx, iface, &iface.Options, options)
}

// mutateOVSMapField updates a string map column on an already-fetched model (e.g. external_ids, options).
// Only keys in desired map are written, already existing keys on the row are unchanged. No-op if desired is empty or already matches.
// Changing a value uses OVSDB delete-then-insert, in one transaction when both steps apply.
func (c *Client) mutateOVSMapField(ctx context.Context, m model.Model, field *map[string]string, desired map[string]string) error {
	if len(desired) == 0 {
		return nil
	}

	toDelete := map[string]string{}
	toInsert := map[string]string{}
	for k, desiredVal := range desired {
		currentVal, exists := (*field)[k]
		if exists && currentVal == desiredVal {
			continue
		}
		toInsert[k] = desiredVal
		if exists {
			toDelete[k] = currentVal
		}
	}

	// OVSDB map mutate requires delete-then-insert for keys we're changing. Both in one transaction.
	var mutations []model.Mutation
	if len(toDelete) > 0 {
		mutations = append(mutations, model.Mutation{
			Field:   field,
			Mutator: ovsdb.MutateOperationDelete,
			Value:   toDelete,
		})
	}
	if len(toInsert) > 0 {
		mutations = append(mutations, model.Mutation{
			Field:   field,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   toInsert,
		})
	}
	if len(mutations) > 0 {
		if err := c.mutate(ctx, m, mutations...); err != nil {
			return fmt.Errorf("failed to update map field: %v", err)
		}
	}
	return nil
}

// SetPortExternalIDs sets the external IDs for a port.
// It is a no-op if externalIDs is empty or if the port already has the requested keys with the same values.
func (c *Client) SetPortExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error {
	port := &ovsmodel.Port{Name: name}
	if err := c.Get(ctx, port); err != nil {
		return fmt.Errorf("failed to get port %s: %w", name, err)
	}
	return c.mutateOVSMapField(ctx, port, &port.ExternalIDs, externalIDs)
}

// SetBridgeExternalIDs sets the external IDs for a bridge.
// It is a no-op if externalIDs is empty or if the bridge already has the requested keys with the same values.
func (c *Client) SetBridgeExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error {
	bridge := &ovsmodel.Bridge{Name: name}
	if err := c.Get(ctx, bridge); err != nil {
		return fmt.Errorf("failed to get bridge %s: %w", name, err)
	}
	return c.mutateOVSMapField(ctx, bridge, &bridge.ExternalIDs, externalIDs)
}

func (c *Client) IsIfaceInBr(ctx context.Context, bridgeName, portName string) (bool, error) {
	port := &ovsmodel.Port{
		Name: portName,
	}
	err := c.Get(ctx, port)
	if err != nil {
		return false, fmt.Errorf("failed to get port %s: %v", portName, err)
	}

	var bridges []ovsmodel.Bridge
	bridge := &ovsmodel.Bridge{}
	err = c.WhereAll(
		bridge,
		model.Condition{
			Field:    &bridge.Ports,
			Function: ovsdb.ConditionIncludes,
			Value:    []string{port.UUID}},
	).List(ctx, &bridges)
	if err != nil {
		return false, fmt.Errorf("failed to list bridges: %v", err)
	}

	if len(bridges) == 1 {
		if bridges[0].Name == bridgeName {
			return true, nil
		}
		return false, nil
	}

	return false, nil
}

func (c *Client) GetOpenVSwitch(ctx context.Context) (*ovsmodel.OpenvSwitch, error) {
	// Get existing Open_vSwitch row (singleton), will not be right for multi controllers
	ovsRowList := []*ovsmodel.OpenvSwitch{}
	if err := c.List(ctx, &ovsRowList); err != nil {
		return nil, fmt.Errorf("failed to list Open_vSwitch rows: %v", err)
	}
	if len(ovsRowList) != 1 {
		return nil, fmt.Errorf("expected 1 Open_vSwitch row, got %d", len(ovsRowList))
	}
	return ovsRowList[0], nil
}

func (c *Client) SetOpenVSwitchExternalIDs(ctx context.Context, externalIDs map[string]string) error {
	ovsRow, err := c.GetOpenVSwitch(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Open_vSwitch row: %v", err)
	}
	return c.mutateOVSMapField(ctx, ovsRow, &ovsRow.ExternalIDs, externalIDs)
}

func (c *Client) GetOpenVSwitchExternalIDs(ctx context.Context) (map[string]string, error) {
	ovsRow, err := c.GetOpenVSwitch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Open_vSwitch row: %v", err)
	}
	return ovsRow.ExternalIDs, nil
}

func (c *Client) AddBridge(ctx context.Context, bridgeConfig BridgeConfig) error {
	// Check if bridge already exists
	err := c.Get(ctx, &ovsmodel.Bridge{Name: bridgeConfig.Name})
	if err != nil && !errors.Is(err, ovsclient.ErrNotFound) {
		return fmt.Errorf("failed to get bridge %s: %v", bridgeConfig.Name, err)
	}
	// bridge already exists
	if err == nil {
		return nil
	}

	bridgeUUID := uuid.New().String()
	bridge := &ovsmodel.Bridge{
		Name:         bridgeConfig.Name,
		UUID:         bridgeUUID,
		DatapathType: bridgeConfig.DatapathType,
		FailMode:     bridgeConfig.FailMode,
		ExternalIDs:  bridgeConfig.ExternalIDs,
	}

	var portOps []ovsdb.Operation
	if !bridgeConfig.SkipCreateInternalPort {
		portAndIfaceName := bridgeConfig.Name
		if bridgeConfig.InternalInterfaceName != nil && *bridgeConfig.InternalInterfaceName != "" {
			portAndIfaceName = *bridgeConfig.InternalInterfaceName
		}
		if len(portAndIfaceName) > maxInterfaceNameLen {
			return fmt.Errorf("internal interface name %q exceeds maximum length of %d characters. Set InternalInterfaceName for a shorter netdev name",
				portAndIfaceName, maxInterfaceNameLen)
		}

		portUUID := uuid.New().String()
		interfaceUUID := uuid.New().String()
		bridge.Ports = []string{portUUID}

		// Create Port
		port := &ovsmodel.Port{
			Name:       portAndIfaceName,
			UUID:       portUUID,
			Interfaces: []string{interfaceUUID},
		}
		portCreateOp, err := c.Create(port)
		if err != nil {
			return fmt.Errorf(errMsgFailedToCreatePort, portAndIfaceName, err)
		}

		// Create Interface
		interfaceInsert := &ovsmodel.Interface{
			Name: portAndIfaceName,
			UUID: interfaceUUID,
			Type: InterfaceTypeInternal,
		}
		interfaceCreateOp, err := c.Create(interfaceInsert)
		if err != nil {
			return fmt.Errorf("failed to create interface %s: %v", portAndIfaceName, err)
		}

		portOps = append(portCreateOp, interfaceCreateOp...)
	}

	bridgeCreateOp, err := c.Create(bridge)
	if err != nil {
		return fmt.Errorf(errMsgFailedToCreateBridge, bridgeConfig.Name, err)
	}

	operations := append(bridgeCreateOp, portOps...)

	// Mutate Open_vSwitch table
	ovsRow, err := c.GetOpenVSwitch(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Open_vSwitch row: %v", err)
	}
	mutateOp, err := c.Where(ovsRow).Mutate(
		ovsRow,
		model.Mutation{
			Field:   &ovsRow.Bridges,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{bridgeUUID},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to mutate Open_vSwitch row: %v", err)
	}
	operations = append(operations, mutateOp...)

	// Perform the transaction
	reply, err := c.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf(errMsgFailedToCreateBridge, bridgeConfig.Name, err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		return fmt.Errorf(errMsgFailedToCreateBridge, bridgeConfig.Name, err)
	}

	return nil
}

// DeleteBridge deletes a bridge from OVSDB.
// OVSDB automatically garbage collects orphaned ports and interfaces when a bridge is deleted.
// The bridge reference should first be removed from Open_vSwitch.Bridges.
// This matches the behavior of `ovs-vsctl del-br`.
func (c *Client) DeleteBridge(ctx context.Context, name string) error {
	// Get the bridge
	bridge := &ovsmodel.Bridge{Name: name}
	err := c.Get(ctx, bridge)
	if err != nil {
		if errors.Is(err, ovsclient.ErrNotFound) {
			return nil // Idempotent: bridge already doesn't exist
		}
		return fmt.Errorf("failed to get bridge %s: %v", name, err)
	}

	// Open_vSwitch.bridges holds a set of Bridge UUIDs. The Bridge row cannot be deleted
	// while its UUID is still in that set. We must remove it first, then delete the bridge.
	ovsRow, err := c.GetOpenVSwitch(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Open_vSwitch row: %v", err)
	}
	mutateOps, err := c.Where(ovsRow).Mutate(
		ovsRow,
		model.Mutation{
			Field:   &ovsRow.Bridges,
			Mutator: ovsdb.MutateOperationDelete,
			Value:   []string{bridge.UUID},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create mutate operations for bridge %s: %v", name, err)
	}

	deleteBridgeOps, err := c.Where(bridge).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operations for bridge %s: %v", name, err)
	}

	operations := append(mutateOps, deleteBridgeOps...)
	return c.executeDeleteOperations(ctx, operations, name)
}

func (c *Client) executeDeleteOperations(ctx context.Context, operations []ovsdb.Operation, bridgeName string) error {
	reply, err := c.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf("failed to delete bridge %s: %v", bridgeName, err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		// Ignore "not found" errors - bridge already doesn't exist (race condition)
		if errors.Is(err, ovsclient.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("failed to delete bridge %s: %v", bridgeName, err)
	}

	return nil
}

func (c *Client) GetIfaceWithName(ctx context.Context, name string) (*ovsmodel.Interface, error) {
	if name == "" {
		return nil, fmt.Errorf("interface name cannot be empty")
	}

	iface := &ovsmodel.Interface{Name: name}
	err := c.Get(ctx, iface)
	if err != nil {
		if errors.Is(err, ovsclient.ErrNotFound) {
			return nil, fmt.Errorf("interface %q not found: %w", name, err)
		}
		return nil, fmt.Errorf("failed to query interface %q: %w", name, err)
	}
	return iface, nil
}

// GetBridgeWithName gets a bridge by name. Returns ErrNotFound if the bridge is not found.
func (c *Client) GetBridgeWithName(ctx context.Context, name string) (*ovsmodel.Bridge, error) {
	if name == "" {
		return nil, fmt.Errorf("bridge name cannot be empty")
	}

	bridge := &ovsmodel.Bridge{Name: name}
	err := c.Get(ctx, bridge)
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge %s: %w", name, err)
	}
	return bridge, nil
}
