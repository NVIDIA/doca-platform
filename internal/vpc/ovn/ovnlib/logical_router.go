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

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
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

// UpdateLogicalRouterOptions updates the options of a logical router.
// If a specific option value is set to "null", it will be deleted from the logical router options.
// If options is nil, no update will be performed.
// Returns the updated logical router.
// The params.Name or params.UUID must be set in the LogicalRouterGetParams.
func (ovnClient *OVNClient) UpdateLogicalRouterOptions(ctx context.Context, params *nbdb.LogicalRouterGetParams, options map[string]string) (*nbdb.LogicalRouter, error) {
	logicalRouter, err := ovnClient.GetLogicalRouter(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router: %w", err)
	}

	if options == nil {
		return logicalRouter, nil
	}

	// calculate the options that need to be added
	// 1. options that are present in options but not in the logical router
	// 2. options that are present in options but with a different value in the logical router

	optionsToAdd := make(map[string]string)
	for k, v := range options {
		if v == "null" {
			continue
		}

		if _, ok := logicalRouter.Options[k]; !ok {
			optionsToAdd[k] = v
		} else if logicalRouter.Options[k] != v {
			optionsToAdd[k] = v
		}
	}

	// calculate the options that need to be deleted
	// 1. options that are present in the logical router but appear as "null" in options
	// 2. options that are present in the logical router but with a different value in options

	optionsToDel := make(map[string]string)
	for k, v := range options {
		if _, ok := logicalRouter.Options[k]; !ok {
			continue
		}

		if v == "null" || logicalRouter.Options[k] != v {
			optionsToDel[k] = logicalRouter.Options[k]
		}
	}

	if len(optionsToAdd) == 0 && len(optionsToDel) == 0 {
		// nothing to update
		return logicalRouter, nil
	}

	ops := []ovsdb.Operation{}
	mutateOp, err := ovnClient.Client.Where(logicalRouter).Mutate(logicalRouter,
		model.Mutation{
			Field:   &logicalRouter.Options,
			Mutator: ovsdb.MutateOperationDelete,
			Value:   optionsToDel,
		},
		model.Mutation{
			Field:   &logicalRouter.Options,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   optionsToAdd,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create mutation operation: %v", err)
	}
	ops = append(ops, mutateOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return nil, fmt.Errorf("logical router update options transaction failed: %v", err)
	}

	logicalRouter, err = ovnClient.GetLogicalRouter(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router after update: %w", err)
	}

	return logicalRouter, nil
}
