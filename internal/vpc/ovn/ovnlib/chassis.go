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

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/sbdb"

	"github.com/google/uuid"
	"github.com/ovn-org/libovsdb/ovsdb"
)

func validateEncap(encap *sbdb.Encap) error {
	if encap == nil {
		return NewOvnError(ErrInvalidArgument, "encap is required")
	}
	if encap.ChassisName == "" || encap.IP == "" || encap.Type == "" {
		return NewOvnError(ErrInvalidArgument, "encap chassis name, ip and type are required")
	}
	return nil
}

func validateChassis(chassis *sbdb.Chassis) error {
	if chassis == nil {
		return NewOvnError(ErrInvalidArgument, "chassis is required")
	}
	if chassis.Name == "" {
		return NewOvnError(ErrInvalidArgument, "chassis name is required")
	}
	return nil
}

// CreateChassis creates a new chassis in the OVN SB database.
func (ovnSBClient *OVNSBClient) CreateChassis(ctx context.Context, encap *sbdb.Encap, chassis *sbdb.Chassis) (*sbdb.Chassis, error) {
	if err := validateEncap(encap); err != nil {
		return nil, err
	}
	if err := validateChassis(chassis); err != nil {
		return nil, err
	}
	if encap.UUID == "" {
		encap.UUID = uuid.New().String()
	}

	encapCreateOp, err := ovnSBClient.Client.Create(encap)
	if err != nil {
		return nil, fmt.Errorf("failed to create OVN SB encap: %v", err)
	}
	ops := []ovsdb.Operation{}
	ops = append(ops, encapCreateOp...)
	chassis.Encaps = []string{encap.UUID}

	chassis.UUID = uuid.New().String()
	chassisCreateOp, err := ovnSBClient.Client.Create(chassis)
	if err != nil {
		return nil, fmt.Errorf("failed to create OVN SB chassis: %v", err)
	}
	ops = append(ops, chassisCreateOp...)

	transactRes, err := ovnSBClient.Client.Transact(ctx, ops...)
	if err != nil || len(transactRes) == 0 {
		return nil, fmt.Errorf("OVN SB chassis transaction failed: %v", err)
	}
	if err := checkTransactionResults(transactRes); err != nil {
		return nil, err
	}
	return chassis, nil
}

// DeleteChassis deletes a chassis in the OVN SB database.
func (ovnSBClient *OVNSBClient) DeleteChassis(ctx context.Context, params *sbdb.ChassisDeleteParams) error {
	chassis, err := ovnSBClient.GetChassis(ctx, &sbdb.ChassisGetParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return err
	}
	ovsdbOperation, err := ovnSBClient.Client.Where(chassis).Delete()
	if err != nil {
		return fmt.Errorf("failed to create delete operation for chassis %s: %v", params.Name, err)
	}

	_, err = ovnSBClient.Client.Transact(ctx, ovsdbOperation...)
	if err != nil {
		return fmt.Errorf("chassis deletion transaction failed: %v", err)
	}
	return nil
}

// ListChassis lists chassis based on given parameters
func (ovnSBClient *OVNSBClient) ListChassis(ctx context.Context, params *sbdb.ChassisListParams) ([]*sbdb.Chassis, error) {
	var chassisList []*sbdb.Chassis

	err := ovnSBClient.Client.WhereCache(func(chassis *sbdb.Chassis) bool {
		// Check basic fields
		if (params.UUID != "" && chassis.UUID != params.UUID) ||
			(params.Name != "" && chassis.Name != params.Name) {
			return false
		}
		return true
	}).List(ctx, &chassisList)

	if err != nil {
		return nil, fmt.Errorf("failed to list chassis: %v", err)
	}
	return chassisList, nil
}

// validateChassisGetParams validates the parameters for getting a chassis
func validateChassisGetParams(chassis *sbdb.ChassisGetParams) error {
	if chassis.Name == "" && chassis.UUID == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires at least one parameter - UUID or Name")
	}
	return nil
}

// GetChassis retrieves a chassis by UUID or Name
func (ovnSBClient *OVNSBClient) GetChassis(ctx context.Context, params *sbdb.ChassisGetParams) (*sbdb.Chassis, error) {
	if err := validateChassisGetParams(params); err != nil {
		return nil, err
	}

	chassisList, err := ovnSBClient.ListChassis(ctx, &sbdb.ChassisListParams{
		UUID: params.UUID,
		Name: params.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get chassis: %v", err)
	}

	switch len(chassisList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "chassis not found: %+v", *params)
	case 1:
		return chassisList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple chassis found: %+v", *params)
	}
}
