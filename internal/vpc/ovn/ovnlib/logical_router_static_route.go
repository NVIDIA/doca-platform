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

func validateLogicalRouterStaticRouteCreateParams(lrsr *nbdb.LogicalRouterStaticRoute) error {
	if lrsr.IPPrefix == "" || lrsr.Nexthop == "" {
		return NewOvnError(ErrInvalidArgument, "create operation requires at least IPPrefix and Nexthop")
	}
	return nil
}

// CreateLogicalRouterStaticRoute creates a new static route for a logical router
func (ovnClient *OVNClient) CreateLogicalRouterStaticRoute(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, staticRouteParams *nbdb.LogicalRouterStaticRoute) (*nbdb.LogicalRouterStaticRoute, error) {
	if err := validateLogicalRouterStaticRouteCreateParams(staticRouteParams); err != nil {
		return nil, err
	}

	err := ovnClient.createLogicalRouterEntity(ctx, lrParams, staticRouteParams, "StaticRoutes")
	if err != nil {
		return nil, err
	}

	return staticRouteParams, nil
}

// DeleteLogicalRouterStaticRoute deletes a static route from a logical router
func (ovnClient *OVNClient) DeleteLogicalRouterStaticRoute(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, staticRouteParams *nbdb.LogicalRouterStaticRouteDeleteParams) error {
	logicalRouter, err := ovnClient.GetLogicalRouter(ctx, lrParams)
	if err != nil {
		return err
	}

	staticRoute, err := ovnClient.GetLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteGetParams{
		UUID:     staticRouteParams.UUID,
		IPPrefix: staticRouteParams.IPPrefix,
		Nexthop:  staticRouteParams.Nexthop,
	})
	if err != nil {
		return err
	}

	ops := []ovsdb.Operation{}

	mutateOp, err := ovnClient.Client.Where(logicalRouter).Mutate(logicalRouter, model.Mutation{
		Field:   &logicalRouter.StaticRoutes,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{staticRoute.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create mutation operation for logical router: %v", err)
	}
	ops = append(ops, mutateOp...)

	deleteOp, err := ovnClient.Client.Where(staticRoute).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for static route: %v", err)
	}
	ops = append(ops, deleteOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("transaction failed to delete static route %+v: %v",
			staticRouteParams, err)
	}

	return nil
}

// validateLogicalRouterStaticRouteGetParams validates the parameters for getting a logical router static route
func validateLogicalRouterStaticRouteGetParams(lrsr *nbdb.LogicalRouterStaticRouteGetParams) error {
	if lrsr.UUID == "" && (lrsr.IPPrefix == "" || lrsr.Nexthop == "") {
		return NewOvnError(ErrInvalidArgument, "operation requires UUID or IPPrefix and Nexthop")
	}
	return nil
}

func (ovnClient *OVNClient) GetLogicalRouterStaticRoute(ctx context.Context, params *nbdb.LogicalRouterStaticRouteGetParams) (*nbdb.LogicalRouterStaticRoute, error) {
	if err := validateLogicalRouterStaticRouteGetParams(params); err != nil {
		return nil, err
	}

	lrsrList, err := ovnClient.ListLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteListParams{
		UUID:     params.UUID,
		IPPrefix: params.IPPrefix,
		Nexthop:  params.Nexthop,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router static route: %v", err)
	}

	switch len(lrsrList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical router static route not found: %+v", *params)
	case 1:
		return lrsrList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical router static route found: %+v", *params)
	}
}

func (ovnClient *OVNClient) ListLogicalRouterStaticRoute(ctx context.Context, params *nbdb.LogicalRouterStaticRouteListParams) ([]*nbdb.LogicalRouterStaticRoute, error) {
	var lrsrList []*nbdb.LogicalRouterStaticRoute

	err := ovnClient.Client.WhereCache(func(lrsr *nbdb.LogicalRouterStaticRoute) bool {
		// Check basic fields
		if (params.UUID != "" && lrsr.UUID != params.UUID) ||
			(params.IPPrefix != "" && lrsr.IPPrefix != params.IPPrefix) ||
			(params.Nexthop != "" && lrsr.Nexthop != params.Nexthop) {
			return false
		}
		return isSubMap(lrsr.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &lrsrList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical router static routes: %v", err)
	}
	return lrsrList, nil
}
