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
	"reflect"
	"slices"

	"github.com/nvidia/doca-platform/internal/vpc/ovn/nbdb"

	"github.com/google/uuid"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

func isSubSlice(slice, subslice []string) bool {
	for _, element := range subslice {
		if !slices.Contains(slice, element) {
			return false
		}
	}
	return true
}

func isSubMap(bigMap, subMap map[string]string) bool {
	for key, value := range subMap {
		bigMapValue, exists := bigMap[key]
		if !exists || bigMapValue != value {
			return false
		}
	}
	return true
}

// createLogicalRouterEntity is a helper function to create and associate an entity with a logical router
func (ovnClient *OVNClient) createLogicalRouterEntity(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, entity interface{}, fieldName string) error {
	logicalRouter, err := ovnClient.GetLogicalRouter(ctx, lrParams)
	if err != nil {
		return err
	}

	entityUUID := uuid.New().String()
	reflect.ValueOf(entity).Elem().FieldByName("UUID").SetString(entityUUID)

	ops := []ovsdb.Operation{}

	createOp, err := ovnClient.Client.Create(entity)
	if err != nil {
		return fmt.Errorf("failed to create operation: %v", err)
	}
	ops = append(ops, createOp...)

	mutateOp, err := ovnClient.Client.Where(logicalRouter).Mutate(logicalRouter, model.Mutation{
		Field:   reflect.ValueOf(logicalRouter).Elem().FieldByName(fieldName).Addr().Interface(),
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{entityUUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create mutation operation: %v", err)
	}
	ops = append(ops, mutateOp...)

	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("creation transaction failed: %v", err)
	}

	return nil
}
