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

	"github.com/kelseyhightower/envconfig"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// SBConfig holds OVN connection settings.
// EndPoint is the OVN southbound database endpoint, defaults to "tcp:127.0.0.1:6642".
// OVNSBReconnectTime is the reconnection delay in seconds, defaults to "5 seconds".
type SBConfig struct {
	EndPoint           string `envconfig:"OVNSB_ENDPOINT" default:"tcp:127.0.0.1:6642"`
	OVNSBReconnectTime int    `envconfig:"OVN_RECONNECT_TIME" default:"5"` //in seconds
}

// FromEnv populates the Config from environment variables.
// It uses the envconfig package to parse variables and set defaults if variables were not set.
// Returns an error if parsing fails.
func (config *SBConfig) FromEnv() error {
	if err := envconfig.Process("", config); err != nil {
		return fmt.Errorf("failed to parse environment variables: %v", err)
	}
	return nil
}

// OVNSBWrapper defines the interface for interacting with OVN Southbound database (Open Virtual Network).
// It provides methods for managing OVN Southbound database components.
type OVNSBWrapper interface {
	// CreateChassis creates a new chassis.
	CreateChassis(ctx context.Context, encap *sbdb.Encap, params *sbdb.Chassis) (*sbdb.Chassis, error)
	// DeleteChassis removes an existing chassis.
	DeleteChassis(ctx context.Context, params *sbdb.ChassisDeleteParams) error
	// GetChassis retrieves information about a specific chassis.
	// The params.Name or params.UUID must be set in the ChassisGetParams.
	GetChassis(ctx context.Context, params *sbdb.ChassisGetParams) (*sbdb.Chassis, error)
	// ListChassis returns a list of all chassis.
	ListChassis(ctx context.Context, params *sbdb.ChassisListParams) ([]*sbdb.Chassis, error)
	// ClearAll removes all entries from the OVN Southbound database.
	ClearAll(ctx context.Context) error
	// Embed the client.Client interface
	client.Client
}

// OVNSBClient implements the OVNSBWrapper interface.
// It also contains client.Client object which interacts with OVN database.
type OVNSBClient struct {
	client.Client
}

// GetOvnSBClient creates and returns a new OVNSBClient for the OVN Southbound database.
// It takes a context, OVN configuration and TLS options as parameters.
// Returns the client and any error encountered during creation.
func GetOvnSBClient(ctx context.Context, ovnSBConfig *SBConfig, tlsOption []client.Option) (*OVNSBClient, error) {
	dbModelReq, err := sbdb.FullDatabaseModel()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OVN SB models: %v", err)
	}

	ovnSBClient, err := getOvnClientAux(ctx, ovnSBConfig.EndPoint, ovnSBConfig.OVNSBReconnectTime, dbModelReq, "SB", tlsOption)
	if err != nil {
		return nil, err
	}
	return &OVNSBClient{Client: ovnSBClient}, nil
}

// ClearAll removes all entries from the OVN Southbound database.
// It returns an error if the operation fails.
func (ovnSBClient *OVNSBClient) ClearAll(ctx context.Context) error {
	var errs []error
	var ops []ovsdb.Operation
	chassisList := []*sbdb.Chassis{}
	err := ovnSBClient.Client.List(ctx, &chassisList)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, chassis := range chassisList {
			deleteOp, err := ovnSBClient.Client.Where(chassis).Delete()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			ops = append(ops, deleteOp...)
		}
	}

	_, err = ovnSBClient.Client.Transact(ctx, ops...)
	if err != nil {
		errs = append(errs, err)
	}
	return kerrors.NewAggregate(errs)
}
