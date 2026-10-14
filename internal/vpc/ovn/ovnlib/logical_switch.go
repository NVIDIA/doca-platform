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
)

// CreateLogicalSwitch creates a new logical switch
func (ovnClient *OVNClient) CreateLogicalSwitch(ctx context.Context, logicalSwitch *nbdb.LogicalSwitch) (*nbdb.LogicalSwitch, error) {
	op, err := ovnClient.Client.Create(logicalSwitch)
	if err != nil {
		return nil, fmt.Errorf("failed to create logical switch operation: %v", err)
	}

	transactRes, err := ovnClient.Client.Transact(ctx, op...)
	if err != nil || len(transactRes) == 0 {
		return nil, fmt.Errorf("logical switch creation transaction failed: %v", err)
	}
	logicalSwitch.UUID = transactRes[0].UUID.GoUUID
	return logicalSwitch, nil
}

// DeleteLogicalSwitch deletes a logical switch
func (ovnClient *OVNClient) DeleteLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitchDeleteParams) error {
	ls, err := ovnClient.GetLogicalSwitch(ctx, &nbdb.LogicalSwitchGetParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return err
	}

	ovsdbOperation, err := ovnClient.Client.Where(ls).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for logical switch %s: %v", params.Name, err)
	}

	_, err = ovnClient.Client.Transact(ctx, ovsdbOperation...)
	if err != nil {
		return fmt.Errorf("logical switch deletion transaction failed: %v", err)
	}
	return nil
}

// ListLogicalSwitch lists logical switches based on given parameters
func (ovnClient *OVNClient) ListLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitchListParams) ([]*nbdb.LogicalSwitch, error) {
	var lsList []*nbdb.LogicalSwitch

	err := ovnClient.Client.WhereCache(func(ls *nbdb.LogicalSwitch) bool {
		// Check basic fields
		if (params.UUID != "" && ls.UUID != params.UUID) ||
			(params.Name != "" && ls.Name != params.Name) {
			return false
		}

		// Check OtherConfig and ExternalIDs for partial matching
		return isSubMap(ls.OtherConfig, params.OtherConfig) && isSubMap(ls.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &lsList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical switches: %v", err)
	}
	return lsList, nil
}

// GetLogicalSwitch retrieves a logical switch by UUID or Name
func (ovnClient *OVNClient) GetLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitchGetParams) (*nbdb.LogicalSwitch, error) {
	if err := validateSwitchGetParams(params); err != nil {
		return nil, err
	}

	lsList, err := ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return nil, NewOvnError(ErrInternal, "failed to get logical switch: %v", err)
	}

	switch len(lsList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical switch not found: %+v", *params)
	case 1:
		return lsList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical switches found: %+v", *params)
	}
}

// validateSwitchGetParams validates the parameters for getting a logical switch
func validateSwitchGetParams(ls *nbdb.LogicalSwitchGetParams) error {
	if ls.Name == "" && ls.UUID == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires at least one parameter - UUID or Name")
	}
	return nil
}
