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

//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs_conditional_api.go github.com/ovn-org/libovsdb/client ConditionalAPI
//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs.go . API

type API interface {
	ovsclient.Client
	AddPort(ctx context.Context, bridgeName, portName, ifaceType string, mtu *int) error
	DelPort(ctx context.Context, bridgeName, portName string) error
	SetIfaceExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error
	GetIfaceWithExternalIDs(ctx context.Context, externalIDs map[string]string) (*ovsmodel.Interface, error)
	SetIfaceOptions(ctx context.Context, name string, options map[string]string) error
	IsIfaceInBr(ctx context.Context, bridgeName, portName string) (bool, error)
	SetPortExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error
	SetOpenVSwitchExternalIDs(ctx context.Context, externalIDs map[string]string) error
	GetOpenVSwitchExternalIDs(ctx context.Context) (map[string]string, error)
	AddBridge(ctx context.Context, bridgeName, bridgeDatapathType string) error
	GetIfaceWithName(ctx context.Context, name string) (*ovsmodel.Interface, error)
}

var _ API = (*Client)(nil)

type Client struct {
	ovsclient.Client
}

const InterfaceTypeInternal = "internal"

// AddPort performing 3 operations
// Adding interface, adding port, attaching port to a bridge
// The port will not be added if it already exists on a different bridge
func (c *Client) AddPort(ctx context.Context, bridgeName, portName, ifaceType string, mtu *int) error {
	port := &ovsmodel.Port{
		Name: portName,
		UUID: portName,
	}

	err := c.Get(ctx, port)
	if err != nil && !errors.Is(err, ovsclient.ErrNotFound) {
		return err
	}

	// Port already exists
	if err == nil {
		isPortInBridge, err := c.IsIfaceInBr(ctx, bridgeName, portName)
		if err != nil {
			return fmt.Errorf("failed to check if port %s is in bridge %s: %v", portName, bridgeName, err)
		}
		// Does not add port because it already exists on this bridge
		if isPortInBridge {
			return nil
		}
		// Should not add port because it already exists on different bridge
		return fmt.Errorf("port %s already exists on a bridge other than %s", portName, bridgeName)
	}

	// maxMtuSize is the maximum MTU size that a DOCA enabled interface can take
	maxMtuSize := 9216
	ifaceUUI := "iface" + portName
	iface := &ovsmodel.Interface{
		Name:       portName,
		UUID:       ifaceUUI,
		Type:       ifaceType,
		MTURequest: &maxMtuSize,
	}

	if mtu != nil {
		iface.MTURequest = mtu
	}

	iIns, err := c.Create(iface)
	if err != nil {
		return fmt.Errorf("failed to create interface for port %s: %v", portName, err)
	}

	operations := iIns

	port.Interfaces = []string{ifaceUUI}

	pIns, err := c.Create(port)
	if err != nil {
		return fmt.Errorf("failed to create port %s: %v", portName, err)
	}

	operations = append(operations, pIns...)

	bridge := &ovsmodel.Bridge{
		Name: bridgeName,
	}

	bIns, err := c.Where(bridge).Mutate(
		bridge,
		model.Mutation{
			Field:   &bridge.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{port.Name},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to add port %s to %s bridge: %v", portName, bridgeName, err)
	}

	operations = append(operations, bIns...)

	reply, err := c.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf("failed to create port %s: %v", portName, err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		return fmt.Errorf("port %s creation failed: %v", portName, err)

	}

	return nil
}

// DelPort performing 4 operations
// Deleting interface, deleting port, deleting port from a bridge, deleting port transaction
// The port will not be deleted if it exists on a different bridge
func (c *Client) DelPort(ctx context.Context, bridgeName, portName string) error {
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

func (c *Client) mutate(ctx context.Context, obj model.Model, mutation model.Mutation) error {
	ifaceMutations := []model.Mutation{mutation}
	operations, err := c.Where(obj).Mutate(obj, ifaceMutations...)
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

// SetIfaceExternalIDs sets the external IDs for an interface
// if the external ID already exists, it will be updated with the new value
// if the external ID does not exist, it will be added
func (c *Client) SetIfaceExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error {
	iface := &ovsmodel.Interface{
		Name: name,
	}

	err := c.Get(ctx, iface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %v", name, err)
	}

	// Get previous external IDs
	deleteExternalIDs := map[string]string{}
	for key := range externalIDs {
		if prevExternalIDVal, ok := iface.ExternalIDs[key]; ok {
			if prevExternalIDVal != externalIDs[key] {
				// external ID has changed, delete the previous one
				deleteExternalIDs[key] = prevExternalIDVal
			}
		}
	}
	// Delete previous external IDs
	if err := c.mutate(ctx, iface, model.Mutation{
		Field:   &iface.ExternalIDs,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   deleteExternalIDs,
	}); err != nil {
		return fmt.Errorf("failed to delete previous external IDs: %v", err)
	}

	// Create mutation with existing UUID as condition
	return c.mutate(ctx, iface, model.Mutation{
		Field:   &iface.ExternalIDs,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   externalIDs,
	})
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
		return nil, fmt.Errorf("failed to find matching interface with external_ids: %v", iface.ExternalIDs)
	}

	if len(ifaces) > 1 {
		return nil, fmt.Errorf("found multiple interfaces with external_ids: %v", iface.ExternalIDs)
	}

	return &ifaces[0], nil
}

func (c *Client) SetIfaceOptions(ctx context.Context, name string, options map[string]string) error {
	iface := &ovsmodel.Interface{
		Name: name,
	}

	return c.mutate(ctx, iface, model.Mutation{
		Field:   &iface.Options,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   options,
	})
}

func (c *Client) SetPortExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error {
	port := &ovsmodel.Port{
		Name: name,
	}

	return c.mutate(ctx, port, model.Mutation{
		Field:   &port.ExternalIDs,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   externalIDs,
	})
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
	//Get previous external IDs
	deleteExternalIDs := map[string]string{}
	for key := range externalIDs {
		if prevExternalIDVal, ok := ovsRow.ExternalIDs[key]; ok {
			if prevExternalIDVal != externalIDs[key] {
				// external ID has changed, delete the previous one
				deleteExternalIDs[key] = prevExternalIDVal
			}
		}
	}
	// Delete previous external IDs
	if err := c.mutate(ctx, ovsRow, model.Mutation{
		Field:   &ovsRow.ExternalIDs,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   deleteExternalIDs,
	}); err != nil {
		return fmt.Errorf("failed to delete previous external IDs: %v", err)
	}
	// Create mutation with existing UUID as condition
	return c.mutate(ctx, ovsRow, model.Mutation{
		Field:   &ovsRow.ExternalIDs,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   externalIDs,
	})
}

func (c *Client) GetOpenVSwitchExternalIDs(ctx context.Context) (map[string]string, error) {
	ovsRow, err := c.GetOpenVSwitch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Open_vSwitch row: %v", err)
	}
	return ovsRow.ExternalIDs, nil
}

func (c *Client) AddBridge(ctx context.Context, bridgeName, bridgeDatapathType string) error {
	// Check if bridge already exists
	err := c.Get(ctx, &ovsmodel.Bridge{Name: bridgeName})
	if err != nil && !errors.Is(err, ovsclient.ErrNotFound) {
		return fmt.Errorf("failed to get bridge %s: %v", bridgeName, err)
	}
	// bridge already exists
	if err == nil {
		return nil
	}

	bridgeUUID := uuid.New().String()
	portUUID := uuid.New().String()
	interfaceUUID := uuid.New().String()

	operations := []ovsdb.Operation{}
	// Create Bridge
	bridge := &ovsmodel.Bridge{
		Name:         bridgeName,
		UUID:         bridgeUUID,
		Ports:        []string{portUUID},
		DatapathType: bridgeDatapathType,
	}
	bridgeCreateOp, err := c.Create(bridge)
	if err != nil {
		return fmt.Errorf("failed to create bridge %s: %v", bridgeName, err)
	}
	operations = append(operations, bridgeCreateOp...)

	// Create Port
	port := &ovsmodel.Port{
		Name:       bridgeName,
		UUID:       portUUID,
		Interfaces: []string{interfaceUUID},
	}
	portCreateOp, err := c.Create(port)
	if err != nil {
		return fmt.Errorf("failed to create port %s: %v", bridgeName, err)
	}
	operations = append(operations, portCreateOp...)

	// Create Interface
	interfaceInsert := &ovsmodel.Interface{
		Name: bridgeName,
		UUID: interfaceUUID,
		Type: InterfaceTypeInternal,
	}
	interfaceCreateOp, err := c.Create(interfaceInsert)
	if err != nil {
		return fmt.Errorf("failed to create interface %s: %v", bridgeName, err)
	}
	operations = append(operations, interfaceCreateOp...)

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
		return fmt.Errorf("failed to create bridge %s: %v", bridgeName, err)
	}

	if _, err := ovsdb.CheckOperationResults(reply, operations); err != nil {
		return fmt.Errorf("failed to create bridge %s: %v", bridgeName, err)
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
