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

func validateLogicalRouterPolicyCreateParams(lrp *nbdb.LogicalRouterPolicy) error {
	if lrp.Priority == 0 || lrp.Match == "" || lrp.Action == "" {
		return NewOvnError(ErrInvalidArgument, "create operation requires at least Priority, Match and Action arguments")
	}
	return nil
}

// CreateLogicalRouterPolicy creates a new logical router policy
func (ovnClient *OVNClient) CreateLogicalRouterPolicy(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, logicalRouterPolicy *nbdb.LogicalRouterPolicy) (*nbdb.LogicalRouterPolicy, error) {
	if err := validateLogicalRouterPolicyCreateParams(logicalRouterPolicy); err != nil {
		return nil, err
	}

	err := ovnClient.createLogicalRouterEntity(ctx, lrParams, logicalRouterPolicy, "Policies")
	if err != nil {
		return nil, err
	}

	return logicalRouterPolicy, nil
}

// DeleteLogicalRouterPolicy deletes a logical router policy
func (ovnClient *OVNClient) DeleteLogicalRouterPolicy(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, policyParams *nbdb.LogicalRouterPolicyDeleteParams) error {
	logicalRouter, err := ovnClient.GetLogicalRouter(ctx, lrParams)
	if err != nil {
		return err
	}

	logicalRouterPolicy, err := ovnClient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{
		UUID:     policyParams.UUID,
		Priority: policyParams.Priority,
		Match:    policyParams.Match,
	})
	if err != nil {
		return err
	}

	ops := []ovsdb.Operation{}

	mutateOp, err := ovnClient.Client.Where(logicalRouter).Mutate(logicalRouter, model.Mutation{
		Field:   &logicalRouter.Policies,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{logicalRouterPolicy.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create mutation operation for logical router: %v", err)
	}
	ops = append(ops, mutateOp...)

	deleteOp, err := ovnClient.Client.Where(logicalRouterPolicy).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for logical router policy: %v", err)
	}
	ops = append(ops, deleteOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("transaction failed to delete logical router policy %s: %v", logicalRouterPolicy.UUID, err)
	}

	return nil
}

// validateLogicalRouterPolicyGetParams validates the parameters for getting a logical router policy
func validateLogicalRouterPolicyGetParams(lrp *nbdb.LogicalRouterPolicyGetParams) error {
	if lrp.UUID == "" && (lrp.Priority == 0 || lrp.Match == "") {
		return NewOvnError(ErrInvalidArgument, "operation requires UUID or Priority, Match")
	}
	return nil
}

func (ovnClient *OVNClient) GetLogicalRouterPolicy(ctx context.Context, params *nbdb.LogicalRouterPolicyGetParams) (*nbdb.LogicalRouterPolicy, error) {
	if err := validateLogicalRouterPolicyGetParams(params); err != nil {
		return nil, err
	}

	lrpList, err := ovnClient.ListLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyListParams{
		UUID:     params.UUID,
		Priority: params.Priority,
		Match:    params.Match,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router policy: %v", err)
	}

	switch len(lrpList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "logical router policy not found: %+v", *params)
	case 1:
		return lrpList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple logical router policy found: %+v", *params)
	}
}

func (ovnClient *OVNClient) ListLogicalRouterPolicy(ctx context.Context, params *nbdb.LogicalRouterPolicyListParams) ([]*nbdb.LogicalRouterPolicy, error) {
	var lrpList []*nbdb.LogicalRouterPolicy

	err := ovnClient.Client.WhereCache(func(lrp *nbdb.LogicalRouterPolicy) bool {
		// Check basic fields
		if (params.UUID != "" && lrp.UUID != params.UUID) ||
			(params.Priority != 0 && lrp.Priority != params.Priority) ||
			(params.Match != "" && lrp.Match != params.Match) ||
			(params.Action != "" && lrp.Action != params.Action) {
			return false
		}
		return isSubMap(lrp.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &lrpList)

	if err != nil {
		return nil, fmt.Errorf("failed to list logical router policies: %v", err)
	}
	return lrpList, nil
}
