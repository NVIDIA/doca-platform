/*
Copyright 2025 NVIDIA

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

package ovnlib

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/internal/vpc/ovn/nbdb"
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
