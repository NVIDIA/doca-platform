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

// CreateLogicalRouter creates a new logical router
func (ovnClient *OVNClient) CreateLogicalRouter(ctx context.Context, logicalRouter *nbdb.LogicalRouter) (*nbdb.LogicalRouter, error) {
	op, err := ovnClient.Client.Create(logicalRouter)
	if err != nil {
		return nil, fmt.Errorf("failed to create logical router operation: %v", err)
	}

	transactRes, err := ovnClient.Client.Transact(ctx, op...)
	if err != nil || len(transactRes) == 0 {
		return nil, fmt.Errorf("logical router creation transaction failed: %v", err)
	}
	logicalRouter.UUID = transactRes[0].UUID.GoUUID
	return logicalRouter, nil
}

// DeleteLogicalRouter deletes a logical router
func (ovnClient *OVNClient) DeleteLogicalRouter(ctx context.Context, params *nbdb.LogicalRouterDeleteParams) error {
	lr, err := ovnClient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return err
	}
	ovsdbOperation, err := ovnClient.Client.Where(lr).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for logical router %s: %v", params.Name, err)
	}

	_, err = ovnClient.Client.Transact(ctx, ovsdbOperation...)
	if err != nil {
		return fmt.Errorf("logical router deletion transaction failed: %v", err)
	}
	return nil
}

// ListLogicalRouter lists logical routers based on given parameters
func (ovnClient *OVNClient) ListLogicalRouter(ctx context.Context, params *nbdb.LogicalRouterListParams) ([]*nbdb.LogicalRouter, error) {
	var lrList []*nbdb.LogicalRouter

	err := ovnClient.Client.WhereCache(func(lr *nbdb.LogicalRouter) bool {
		// Check basic fields
		if (params.UUID != "" && lr.UUID != params.UUID) ||
			(params.Name != "" && lr.Name != params.Name) {
			return false
		}

		// Check ExternalIDs for partial matching
		return isSubMap(lr.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &lrList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical routers: %v", err)
	}
	return lrList, nil
}

// validateRouterGetParams validates the parameters for getting a logical router
func validateRouterGetParams(lr *nbdb.LogicalRouterGetParams) error {
	if lr.Name == "" && lr.UUID == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires at least one parameter - UUID or Name")
	}
	return nil
}

// GetLogicalRouter retrieves a logical router by UUID or Name
func (ovnClient *OVNClient) GetLogicalRouter(ctx context.Context, params *nbdb.LogicalRouterGetParams) (*nbdb.LogicalRouter, error) {
	if err := validateRouterGetParams(params); err != nil {
		return nil, err
	}

	lrList, err := ovnClient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router: %v", err)
	}

	switch len(lrList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical router not found: %+v", *params)
	case 1:
		return lrList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical routers found: %+v", *params)
	}
}
