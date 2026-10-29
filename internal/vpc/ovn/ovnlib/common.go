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
	"reflect"
	"slices"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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

// getOvnClientAux is a common function to create and initialize an OVN client
func getOvnClientAux(ctx context.Context, endpoint string, reconnectTime int, dbModelReq model.ClientDBModel, dbType string, tlsOption []client.Option) (client.Client, error) {
	discardLogger := logr.Discard()
	options := []client.Option{
		client.WithEndpoint(endpoint),
		client.WithReconnect(time.Duration(reconnectTime)*time.Second, nil),
		client.WithLogger(&discardLogger),
	}
	options = append(options, tlsOption...)

	log := ctrllog.FromContext(ctx)
	log.Info(fmt.Sprintf("Creating OVN %s Client", dbType),
		"endpoint", endpoint,
		"reconnectTime", reconnectTime)
	ovnClient, err := client.NewOVSDBClient(dbModelReq, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OVSDB client: %v", err)
	}

	log.Info(fmt.Sprintf("Connecting to OVN %s", dbType),
		"endpoint", ovnClient.CurrentEndpoint())
	err = ovnClient.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OVN %s: %v", dbType, err)
	}
	log.Info(fmt.Sprintf("OVN %s client is connected successfully", dbType))

	// get is automatically done from a cache, monitoring will make sure cache is synced
	_, err = ovnClient.MonitorAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to monitor OVN models: %v", err)
	}

	return ovnClient, nil
}

// checkTransactionResults checks if any transaction result contains an error
func checkTransactionResults(transactRes []ovsdb.OperationResult) error {
	for _, res := range transactRes {
		if res.Error != "" {
			return fmt.Errorf("OVN transaction failed: %v", res.Details)
		}
	}
	return nil
}
