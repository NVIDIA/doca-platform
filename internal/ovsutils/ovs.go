/*
COPYRIGHT 2024 NVIDIA

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

	"github.com/nvidia/doca-platform/internal/ovsmodel"

	ovsclient "github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs_conditional_api.go github.com/ovn-org/libovsdb/client ConditionalAPI
//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt --build_flags=--mod=mod -package ovsutils -destination mock_ovs.go . API

type API interface {
	ovsclient.Client
	AddPort(ctx context.Context, bridgeName, portName, ifaceType string) error
	DelPort(ctx context.Context, bridgeName, portName string) error
	SetIfaceExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error
	SetIfaceOptions(ctx context.Context, name string, options map[string]string) error
	IsIfaceInBr(ctx context.Context, bridgeName, portName string) (bool, error)
	SetPortExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error
}

var _ API = (*Client)(nil)

type Client struct {
	ovsclient.Client
}

// AddPort performing 3 operations
// Adding interface, adding port, attaching port to a bridge
func (c *Client) AddPort(ctx context.Context, bridgeName, portName, ifaceType string) error {
	port := &ovsmodel.Port{
		Name: portName,
		UUID: portName,
	}

	err := c.Get(ctx, port)
	if err != nil && !errors.Is(err, ovsclient.ErrNotFound) {
		return err
	}
	// port already exist
	if err == nil {
		return nil
	}

	ifaceUUI := "iface" + portName
	iface := &ovsmodel.Interface{
		Name: portName,
		UUID: ifaceUUI,
		Type: ifaceType,
	}

	iIns, err := c.Create(iface)
	if err != nil {
		return fmt.Errorf("failed to create interface for port %s: %v", portName, err)
	}

	operations := iIns

	port.Interfaces = []string{ifaceUUI}

	pIns, err := c.Create(port)
	if err != nil {
		return fmt.Errorf("faield to create port %s: %v", portName, err)
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

func (c *Client) DelPort(ctx context.Context, bridgeName, portName string) error {
	// get port
	port := &ovsmodel.Port{
		Name: portName,
	}
	err := c.Get(ctx, port)
	if err != nil {
		return fmt.Errorf("faield to get port %s: %v", portName, err)
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
		return fmt.Errorf("faield to mutate: %v", err)
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

func (c *Client) SetIfaceExternalIDs(ctx context.Context, name string, externalIDs map[string]string) error {
	iface := &ovsmodel.Interface{
		Name: name,
	}

	return c.mutate(ctx, iface, model.Mutation{
		Field:   &iface.ExternalIDs,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   externalIDs,
	})
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
	bridge := &ovsmodel.Bridge{
		Name: bridgeName,
	}
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
		return true, nil
	}

	return false, nil
}
