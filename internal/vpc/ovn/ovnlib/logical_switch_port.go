/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package ovnlib

import (
	"context"
	"fmt"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"

	"github.com/google/uuid"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

// CreateLogicalSwitchPort creates a new logical switch port
func (ovnClient *OVNClient) CreateLogicalSwitchPort(ctx context.Context, lsParams *nbdb.LogicalSwitchGetParams, logicalSwitchPort *nbdb.LogicalSwitchPort) (*nbdb.LogicalSwitchPort, error) {
	logicalSwitch, err := ovnClient.GetLogicalSwitch(ctx, lsParams)
	if err != nil {
		return nil, err
	}

	logicalSwitchPort.UUID = uuid.New().String()

	ops := []ovsdb.Operation{}

	createOp, err := ovnClient.Client.Create(logicalSwitchPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create logical switch port operation: %v", err)
	}
	ops = append(ops, createOp...)

	mutateOp, err := ovnClient.Client.Where(logicalSwitch).Mutate(logicalSwitch, model.Mutation{
		Field:   &logicalSwitch.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{logicalSwitchPort.UUID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create mutation operation: %v", err)
	}
	ops = append(ops, mutateOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return nil, fmt.Errorf("logical switch port creation transaction failed: %v", err)
	}

	return logicalSwitchPort, nil
}

// DeleteLogicalSwitchPort deletes a logical switch port
func (ovnClient *OVNClient) DeleteLogicalSwitchPort(ctx context.Context, lsParams *nbdb.LogicalSwitchGetParams, lspParamas *nbdb.LogicalSwitchPortDeleteParams) error {
	logicalSwitch, err := ovnClient.GetLogicalSwitch(ctx, lsParams)
	if err != nil {
		return err
	}

	logicalSwitchPort, err := ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{
		UUID: lspParamas.UUID,
		Name: lspParamas.Name,
	})
	if err != nil {
		return err
	}

	ops := []ovsdb.Operation{}

	mutateOp, err := ovnClient.Client.Where(logicalSwitch).Mutate(logicalSwitch, model.Mutation{
		Field:   &logicalSwitch.Ports,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{logicalSwitchPort.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create mutation operation for logical switch: %v", err)
	}
	ops = append(ops, mutateOp...)

	deleteOp, err := ovnClient.Client.Where(logicalSwitchPort).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for logical switch port: %v", err)
	}
	ops = append(ops, deleteOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("transaction failed to delete logical switch port %s: %v", logicalSwitchPort.Name, err)
	}

	return nil
}

// validateLogicalSwitchPortGetParams validates the parameters for getting a logical switch port
func validateLogicalSwitchPortGetParams(lsp *nbdb.LogicalSwitchPortGetParams) error {
	if lsp.UUID == "" && lsp.Name == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires at least one parameter - UUID or Name")
	}
	return nil
}

func (ovnClient *OVNClient) GetLogicalSwitchPort(ctx context.Context, params *nbdb.LogicalSwitchPortGetParams) (*nbdb.LogicalSwitchPort, error) {
	if err := validateLogicalSwitchPortGetParams(params); err != nil {
		return nil, err
	}

	lspList, err := ovnClient.ListLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortListParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical switch port: %v", err)
	}

	switch len(lspList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical switch port not found: %+v", *params)
	case 1:
		return lspList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical switch ports found: %+v", *params)
	}
}

func (ovnClient *OVNClient) ListLogicalSwitchPort(ctx context.Context, params *nbdb.LogicalSwitchPortListParams) ([]*nbdb.LogicalSwitchPort, error) {
	var lspList []*nbdb.LogicalSwitchPort

	err := ovnClient.Client.WhereCache(func(lsp *nbdb.LogicalSwitchPort) bool {
		// Check basic fields
		if (params.UUID != "" && lsp.UUID != params.UUID) ||
			(params.Name != "" && lsp.Name != params.Name) {
			return false
		}

		return isSubSlice(lsp.Addresses, params.Addresses) &&
			isSubMap(lsp.Options, params.Options) &&
			isSubMap(lsp.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &lspList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical switch ports: %v", err)
	}
	return lspList, nil
}
