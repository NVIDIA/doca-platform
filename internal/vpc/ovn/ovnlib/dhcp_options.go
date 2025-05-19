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
)

// CreateDhcpOptions creates a new DHCP options entry in OVN
func (ovnClient *OVNClient) CreateDhcpOptions(ctx context.Context, dhcpOptions *nbdb.DHCPOptions) (*nbdb.DHCPOptions, error) {
	op, err := ovnClient.Client.Create(dhcpOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create OVN DHCP options: %v", err)
	}

	transactRes, err := ovnClient.Client.Transact(ctx, op...)
	if err != nil || len(transactRes) == 0 {
		return nil, fmt.Errorf("OVN DHCP options transaction failed: %v", err)
	}
	dhcpOptions.UUID = transactRes[0].UUID.GoUUID
	return dhcpOptions, nil
}

// DeleteDhcpOptions deletes DHCP options from OVN
func (ovnClient *OVNClient) DeleteDhcpOptions(ctx context.Context, params *nbdb.DHCPOptionsDeleteParams) error {
	dhcpOptions, err := ovnClient.GetDhcpOptions(ctx, &nbdb.DHCPOptionsGetParams{
		UUID: params.UUID,
		Cidr: params.Cidr,
	})
	if err != nil {
		return err
	}

	ovsdbOperation, err := ovnClient.Client.Where(dhcpOptions).Delete()

	if err != nil {
		return fmt.Errorf("failed to create delete operation for OVN DHCP options: %v", err)
	}

	_, err = ovnClient.Client.Transact(ctx, ovsdbOperation...)
	if err != nil {
		return fmt.Errorf("OVN DHCP options delete transaction failed: %v", err)
	}
	return nil
}

// validateDHCPGetParams validates the parameters for getting a dhcp options
func validateDHCPGetParams(ls *nbdb.DHCPOptionsGetParams) error {
	if ls.UUID == "" && ls.Cidr == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires at least one parameter - UUID or CIDR")
	}
	return nil
}

// GetDhcpOptions retrieves a dhcp option by UUID or CIDR
func (ovnClient *OVNClient) GetDhcpOptions(ctx context.Context, params *nbdb.DHCPOptionsGetParams) (*nbdb.DHCPOptions, error) {
	if err := validateDHCPGetParams(params); err != nil {
		return nil, err
	}

	dhcpOptionsList, err := ovnClient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{
		UUID: params.UUID,
		Cidr: params.Cidr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get dhcp options: %v", err)
	}

	switch len(dhcpOptionsList) {
	case 0:
		return nil, NewOvnError(ErrNotFound, "dhcp options not found: %+v", *params)
	case 1:
		return dhcpOptionsList[0], nil
	default:
		return nil, NewOvnError(ErrInternal, "multiple dhcp options found: %+v", *params)
	}
}

func (ovnClient *OVNClient) ListDhcpOptions(ctx context.Context, params *nbdb.DHCPOptionsListParams) ([]*nbdb.DHCPOptions, error) {
	var dhcpOptionsList []*nbdb.DHCPOptions

	err := ovnClient.Client.WhereCache(func(dhcpOpt *nbdb.DHCPOptions) bool {
		// Check basic fields
		if (params.UUID != "" && dhcpOpt.UUID != params.UUID) ||
			(params.Cidr != "" && dhcpOpt.Cidr != params.Cidr) {
			return false
		}

		// Check Options and ExternalIDs for partial matching
		return isSubMap(dhcpOpt.Options, params.Options) &&
			isSubMap(dhcpOpt.ExternalIDs, params.ExternalIDs)
	}).List(ctx, &dhcpOptionsList)

	if err != nil {
		return nil, fmt.Errorf("failed to list dhcp options: %v", err)
	}
	return dhcpOptionsList, nil
}
