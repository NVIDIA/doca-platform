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

	"github.com/kelseyhightower/envconfig"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Config holds OVN connection settings.
// EndPoint is the OVN northbound database endpoint, defaults to "tcp:127.0.0.1:6641".
// OVNNBReconnectTime is the reconnection delay in seconds, defaults to "5 seconds".
type Config struct {
	EndPoint           string `envconfig:"OVNNB_ENDPOINT" default:"tcp:127.0.0.1:6641"`
	OVNNBReconnectTime int    `envconfig:"OVN_RECONNECT_TIME" default:"5"` //in seconds
}

// FromEnv populates the Config from environment variables.
// It uses the envconfig package to parse variables and set defaults if variables were not set.
// Returns an error if parsing fails.
func (config *Config) FromEnv() error {
	if err := envconfig.Process("", config); err != nil {
		return fmt.Errorf("failed to parse environment variables: %v", err)
	}
	return nil
}

// OVNWrapper defines the interface for interacting with OVN (Open Virtual Network).
// It provides methods for managing logical network components such as switches, routers, ports, and policies.
type OVNWrapper interface {
	// CreateLogicalSwitch creates a new logical switch.
	CreateLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitch) (*nbdb.LogicalSwitch, error)
	// DeleteLogicalSwitch removes an existing logical switch.
	DeleteLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitchDeleteParams) error
	// GetLogicalSwitch retrieves information about a specific logical switch.
	// The params.Name or params.UUID must be set in the LogicalSwitchGetParams.
	GetLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitchGetParams) (*nbdb.LogicalSwitch, error)
	// ListLogicalSwitch returns a list of all logical switches.
	ListLogicalSwitch(ctx context.Context, params *nbdb.LogicalSwitchListParams) ([]*nbdb.LogicalSwitch, error)

	// CreateDhcpOptions creates new DHCP options.
	CreateDhcpOptions(ctx context.Context, params *nbdb.DHCPOptions) (*nbdb.DHCPOptions, error)
	// DeleteDhcpOptions removes existing DHCP options.
	DeleteDhcpOptions(ctx context.Context, params *nbdb.DHCPOptionsDeleteParams) error
	// GetDhcpOptions retrieves information about specific DHCP options.
	// The params.Cidr or params.UUID must be set in the DHCPOptionsGetParams.
	GetDhcpOptions(ctx context.Context, params *nbdb.DHCPOptionsGetParams) (*nbdb.DHCPOptions, error)
	// ListDhcpOptions returns a list of all DHCP options.
	ListDhcpOptions(ctx context.Context, params *nbdb.DHCPOptionsListParams) ([]*nbdb.DHCPOptions, error)

	// CreateLogicalRouter creates a new logical router.
	CreateLogicalRouter(ctx context.Context, params *nbdb.LogicalRouter) (*nbdb.LogicalRouter, error)
	// DeleteLogicalRouter removes an existing logical router.
	DeleteLogicalRouter(ctx context.Context, params *nbdb.LogicalRouterDeleteParams) error
	// GetLogicalRouter retrieves information about a specific logical router.
	// The params.Name or params.UUID must be set in the LogicalRouterGetParams.
	GetLogicalRouter(ctx context.Context, params *nbdb.LogicalRouterGetParams) (*nbdb.LogicalRouter, error)
	// ListLogicalRouter returns a list of all logical routers.
	ListLogicalRouter(ctx context.Context, params *nbdb.LogicalRouterListParams) ([]*nbdb.LogicalRouter, error)
	// UpdateLogicalRouterOptions updates the options of a logical router.
	// If a specific option value is set to "null", it will be deleted from the logical router options.
	// If options is nil, no update will be performed.
	// Returns the updated logical router.
	// The params.Name or params.UUID must be set in the LogicalRouterGetParams.
	UpdateLogicalRouterOptions(ctx context.Context, params *nbdb.LogicalRouterGetParams, options map[string]string) (*nbdb.LogicalRouter, error)

	// CreateLogicalRouterPort creates a new port on a logical router.
	CreateLogicalRouterPort(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, params *nbdb.LogicalRouterPort) (*nbdb.LogicalRouterPort, error)
	// DeleteLogicalRouterPort removes a port from a logical router.
	DeleteLogicalRouterPort(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, params *nbdb.LogicalRouterPortDeleteParams) error
	// GetLogicalRouterPort retrieves information about a specific logical router port.
	// The params.Name or params.UUID must be set in the LogicalRouterPortGetParams.
	GetLogicalRouterPort(ctx context.Context, params *nbdb.LogicalRouterPortGetParams) (*nbdb.LogicalRouterPort, error)
	// ListLogicalRouterPort returns a list of all ports on a logical router.
	ListLogicalRouterPort(ctx context.Context, params *nbdb.LogicalRouterPortListParams) ([]*nbdb.LogicalRouterPort, error)

	// CreateLogicalSwitchPort creates a new port on a logical switch.
	CreateLogicalSwitchPort(ctx context.Context, lsParams *nbdb.LogicalSwitchGetParams, params *nbdb.LogicalSwitchPort) (*nbdb.LogicalSwitchPort, error)
	// DeleteLogicalSwitchPort removes a port from a logical switch.
	DeleteLogicalSwitchPort(ctx context.Context, lsParams *nbdb.LogicalSwitchGetParams, params *nbdb.LogicalSwitchPortDeleteParams) error
	// GetLogicalSwitchPort retrieves information about a specific logical switch port.
	// The params.Name or params.UUID must be set in the LogicalSwitchPortGetParams.
	GetLogicalSwitchPort(ctx context.Context, params *nbdb.LogicalSwitchPortGetParams) (*nbdb.LogicalSwitchPort, error)
	// ListLogicalSwitchPort returns a list of all ports on a logical switch.
	ListLogicalSwitchPort(ctx context.Context, params *nbdb.LogicalSwitchPortListParams) ([]*nbdb.LogicalSwitchPort, error)

	// CreateLogicalRouterStaticRoute creates a new static route on a logical router.
	CreateLogicalRouterStaticRoute(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, params *nbdb.LogicalRouterStaticRoute) (*nbdb.LogicalRouterStaticRoute, error)
	// DeleteLogicalRouterStaticRoute removes a static route from a logical router.
	DeleteLogicalRouterStaticRoute(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, params *nbdb.LogicalRouterStaticRouteDeleteParams) error
	// GetLogicalRouterStaticRoute retrieves information about a specific static route on a logical router.
	// The params.UUD or params.IPPrefix and params.Nexthop must be set in the LogicalRouterStaticRouteGetParams.
	GetLogicalRouterStaticRoute(ctx context.Context, params *nbdb.LogicalRouterStaticRouteGetParams) (*nbdb.LogicalRouterStaticRoute, error)
	// ListLogicalRouterStaticRoute returns a list of all static routes on a logical router.
	ListLogicalRouterStaticRoute(ctx context.Context, params *nbdb.LogicalRouterStaticRouteListParams) ([]*nbdb.LogicalRouterStaticRoute, error)

	// CreateLogicalRouterPolicy creates a new policy on a logical router.
	CreateLogicalRouterPolicy(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, policyParams *nbdb.LogicalRouterPolicy) (*nbdb.LogicalRouterPolicy, error)
	// DeleteLogicalRouterPolicy removes a policy from a logical router.
	DeleteLogicalRouterPolicy(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, policyParams *nbdb.LogicalRouterPolicyDeleteParams) error
	// GetLogicalRouterPolicy retrieves information about a specific policy on a logical router
	// The params.UUD or params.Match and params.Priority must be set in the LogicalRouterPolicyGetParams.
	GetLogicalRouterPolicy(ctx context.Context, params *nbdb.LogicalRouterPolicyGetParams) (*nbdb.LogicalRouterPolicy, error)
	// ListLogicalRouterPolicy returns a list of all policies on a logical router.
	ListLogicalRouterPolicy(ctx context.Context, params *nbdb.LogicalRouterPolicyListParams) ([]*nbdb.LogicalRouterPolicy, error)

	// CreateLogicalRouterNat creates a new NAT rule on a logical router.
	CreateLogicalRouterNat(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, natParams *nbdb.NAT) (*nbdb.NAT, error)
	// DeleteLogicalRouterNat removes a NAT rule from a logical router.
	DeleteLogicalRouterNat(ctx context.Context, lrParams *nbdb.LogicalRouterGetParams, natParams *nbdb.NatDeleteParams) error
	// GetLogicalRouterNat retrieves information about a specific NAT rule on a logical router.
	// The params.UUID or params.LogicalIP and params.Type and optionally params.GatewayPort must be set in NatGetParams.
	GetLogicalRouterNat(ctx context.Context, params *nbdb.NatGetParams) (*nbdb.NAT, error)
	// ListLogicalRouterNat returns a list of all NAT rules on a logical router.
	ListLogicalRouterNat(ctx context.Context, params *nbdb.NatListParams) ([]*nbdb.NAT, error)

	// ClearAll removes all OVN configurations. Used for testing purposes.
	ClearAll(ctx context.Context) error

	// Embed the client.Client interface
	client.Client
}

// OVNClient implements the OVNWrapper interface.
// It also contains client.Client object which interacts with OVN database.
type OVNClient struct {
	client.Client
}

// GetOvnNBClient creates and returns a new OVNClient for the OVN Northbound database.
// It takes a context, OVN configuration and TLS options as parameters.
// Returns the client and any error encountered during creation.
func GetOvnNBClient(ctx context.Context, ovnNBConfig *Config, tlsOption []client.Option) (*OVNClient, error) {
	dbModelReq, err := nbdb.FullDatabaseModel()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OVN NB models: %v", err)
	}

	ovnNBClient, err := getOvnClientAux(ctx, ovnNBConfig.EndPoint, ovnNBConfig.OVNNBReconnectTime, dbModelReq, "NB", tlsOption)
	if err != nil {
		return nil, err
	}
	return &OVNClient{Client: ovnNBClient}, nil
}

// ClearAll removes all entries from the OVN database.
// It returns an error if the operation fails.
func (ovnClient *OVNClient) ClearAll(ctx context.Context) error {
	var errs []error
	var ops []ovsdb.Operation
	lrList := []*nbdb.LogicalRouter{}
	err := ovnClient.Client.List(ctx, &lrList)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, lr := range lrList {
			deleteOp, err := ovnClient.Client.Where(lr).Delete()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			ops = append(ops, deleteOp...)
		}
	}

	lsList := []*nbdb.LogicalSwitch{}
	err = ovnClient.Client.List(ctx, &lsList)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, ls := range lsList {
			deleteOp, err := ovnClient.Client.Where(ls).Delete()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			ops = append(ops, deleteOp...)
		}
	}

	dhcpList := []*nbdb.DHCPOptions{}
	err = ovnClient.Client.List(ctx, &dhcpList)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, dhcpOption := range dhcpList {
			deleteOp, err := ovnClient.Client.Where(dhcpOption).Delete()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			ops = append(ops, deleteOp...)
		}
	}
	_, err = ovnClient.Client.Transact(ctx, ops...)
	if err != nil {
		errs = append(errs, err)
	}
	return kerrors.NewAggregate(errs)
}
