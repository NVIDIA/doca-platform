/*
COPYRIGHT 2025 NVIDIA

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

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

func validateLogicalRouterPortCreateParams(lrsr *nbdb.LogicalRouterPort) error {
	if lrsr.Name == "" {
		return NewOvnError(ErrInvalidArgument, "create operation requires at least Name")
	}
	return nil
}

// CreateLogicalRouterPort creates a new logical router port
func (ovnClient *OVNClient) CreateLogicalRouterPort(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, logicalRouterPort *nbdb.LogicalRouterPort) (*nbdb.LogicalRouterPort, error) {
	if err := validateLogicalRouterPortCreateParams(logicalRouterPort); err != nil {
		return nil, err
	}

	err := ovnClient.createLogicalRouterEntity(ctx, lrParams, logicalRouterPort, "Ports")
	if err != nil {
		return nil, err
	}

	return logicalRouterPort, nil
}

// DeleteLogicalRouterPort deletes a logical router port
func (ovnClient *OVNClient) DeleteLogicalRouterPort(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, lrpParams *nbdb.LogicalRouterPortDeleteParams) error {
	logicalRouter, err := ovnClient.GetLogicalRouter(ctx, lrParams)
	if err != nil {
		return err
	}

	logicalRouterPort, err := ovnClient.GetLogicalRouterPort(ctx, &nbdb.LogicalRouterPortGetParams{
		UUID: lrpParams.UUID,
		Name: lrpParams.Name,
	})
	if err != nil {
		return err
	}

	ops := []ovsdb.Operation{}

	mutateOp, err := ovnClient.Client.Where(logicalRouter).Mutate(logicalRouter, model.Mutation{
		Field:   &logicalRouter.Ports,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{logicalRouterPort.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create mutation operation for logical router: %v", err)
	}
	ops = append(ops, mutateOp...)

	deleteOp, err := ovnClient.Client.Where(logicalRouterPort).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for logical router port: %v", err)
	}
	ops = append(ops, deleteOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("transaction failed to delete logical router port %+v: %v", *lrpParams, err)
	}

	return nil
}

// validateLogicalRouterPortGetParams validates the parameters for getting a logical router port
func validateLogicalRouterPortGetParams(lrp *nbdb.LogicalRouterPortGetParams) error {
	if lrp.UUID == "" && lrp.Name == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires at least one parameter - UUID or Name")
	}
	return nil
}

// GetLogicalRouterPort retrieves a logical router port by UUID or Name
func (ovnClient *OVNClient) GetLogicalRouterPort(ctx context.Context, params *nbdb.LogicalRouterPortGetParams) (*nbdb.LogicalRouterPort, error) {
	if err := validateLogicalRouterPortGetParams(params); err != nil {
		return nil, err
	}

	lrpList, err := ovnClient.ListLogicalRouterPort(ctx, &nbdb.LogicalRouterPortListParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router port: %v", err)
	}

	switch len(lrpList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical router port not found: %+v", *params)
	case 1:
		return lrpList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical router ports found: %+v", *params)
	}
}

func (ovnClient *OVNClient) ListLogicalRouterPort(ctx context.Context, params *nbdb.LogicalRouterPortListParams) ([]*nbdb.LogicalRouterPort, error) {
	var lrpList []*nbdb.LogicalRouterPort

	err := ovnClient.Client.WhereCache(func(lrp *nbdb.LogicalRouterPort) bool {
		// Check basic fields
		if (params.UUID != "" && lrp.UUID != params.UUID) ||
			(params.Name != "" && lrp.Name != params.Name) ||
			(params.MAC != "" && lrp.MAC != params.MAC) {
			return false
		}

		// Check Networks and ExternalIDs for partial matching
		return isSubSlice(lrp.Networks, params.Networks) &&
			isSubMap(lrp.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &lrpList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical router ports: %v", err)
	}
	return lrpList, nil
}
