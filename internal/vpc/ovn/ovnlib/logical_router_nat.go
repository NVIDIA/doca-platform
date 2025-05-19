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

func validateLogicalRouterNatCreateParams(nat *nbdb.NAT) error {
	if nat.Type == "" || nat.ExternalIP == "" || nat.LogicalIP == "" {
		return NewOvnError(ErrInvalidArgument, "create operation requires at least Type, ExternalIP and LogicalIP arguments")
	}
	return nil
}

// CreateLogicalRouterNat creates a new NAT entry for a logical router
func (ovnClient *OVNClient) CreateLogicalRouterNat(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, nat *nbdb.NAT) (*nbdb.NAT, error) {
	if err := validateLogicalRouterNatCreateParams(nat); err != nil {
		return nil, err
	}

	err := ovnClient.createLogicalRouterEntity(ctx, lrParams, nat, "Nat")
	if err != nil {
		return nil, err
	}

	return nat, nil
}

// DeleteLogicalRouterNat deletes a NAT entry from a logical router
func (ovnClient *OVNClient) DeleteLogicalRouterNat(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, natParams *nbdb.NatDeleteParams) error {
	logicalRouter, err := ovnClient.GetLogicalRouter(ctx, lrParams)
	if err != nil {
		return err
	}

	logicalRouterNat, err := ovnClient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{
		UUID:        natParams.UUID,
		Type:        natParams.Type,
		LogicalIP:   natParams.LogicalIP,
		GatewayPort: natParams.GatewayPort,
	})
	if err != nil {
		return err
	}

	ops := []ovsdb.Operation{}

	mutateOp, err := ovnClient.Client.Where(logicalRouter).Mutate(logicalRouter, model.Mutation{
		Field:   &logicalRouter.Nat,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{logicalRouterNat.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create mutation operation for logical router: %v", err)
	}
	ops = append(ops, mutateOp...)

	deleteOp, err := ovnClient.Client.Where(logicalRouterNat).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for NAT entry: %v", err)
	}
	ops = append(ops, deleteOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("transaction failed to delete NAT entry %s: %v", logicalRouterNat.UUID, err)
	}

	return nil
}

// validateNatGetParams validates the parameters for getting a logical router nat
func validateNatGetParams(lrp *nbdb.NatGetParams) error {
	if lrp.UUID == "" && (lrp.Type == "" || lrp.LogicalIP == "") {
		return NewOvnError(ErrInvalidArgument, "operation requires UUID or Type and LogicalIP and optionally GatewayPort")
	}
	return nil
}

func (ovnClient *OVNClient) GetLogicalRouterNat(ctx context.Context, params *nbdb.NatGetParams) (*nbdb.NAT, error) {
	if err := validateNatGetParams(params); err != nil {
		return nil, err
	}

	natList, err := ovnClient.ListLogicalRouterNat(ctx, &nbdb.NatListParams{
		UUID:        params.UUID,
		Type:        params.Type,
		LogicalIP:   params.LogicalIP,
		GatewayPort: params.GatewayPort,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router nat: %v", err)
	}

	switch len(natList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical router nat not found: %+v", *params)
	case 1:
		return natList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical router nat found: %+v", *params)
	}
}

func (ovnClient *OVNClient) ListLogicalRouterNat(ctx context.Context, params *nbdb.NatListParams) ([]*nbdb.NAT, error) {
	var natList []*nbdb.NAT

	err := ovnClient.Client.WhereCache(func(nat *nbdb.NAT) bool {
		// Check basic fields
		if (params.UUID != "" && nat.UUID != params.UUID) ||
			(params.Type != "" && nat.Type != params.Type) ||
			(params.LogicalIP != "" && nat.LogicalIP != params.LogicalIP) ||
			(params.ExternalIP != "" && nat.ExternalIP != params.ExternalIP) ||
			(params.ExternalMAC != nil && *nat.ExternalMAC != *params.ExternalMAC) ||
			(params.LogicalPort != nil && *nat.LogicalPort != *params.LogicalPort) ||
			(params.GatewayPort != nil && *nat.GatewayPort != *params.GatewayPort) {
			return false
		}
		return isSubMap(nat.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &natList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical router nats: %v", err)
	}
	return natList, nil
}
