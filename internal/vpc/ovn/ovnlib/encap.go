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
	"strings"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/sbdb"
)

// validateEncapGetParams validates the parameters for getting an encap entry
func validateEncapGetParams(encap *sbdb.EncapGetParams) error {
	if encap.UUID == "" {
		return NewOvnError(ErrInvalidArgument, "operation requires UUID")
	}
	return nil
}

// GetEncap retrieves information about a specific encap.
func (ovnSBClient *OVNSBClient) GetEncap(ctx context.Context, params *sbdb.EncapGetParams) (*sbdb.Encap, error) {
	if err := validateEncapGetParams(params); err != nil {
		return nil, err
	}

	encap := &sbdb.Encap{UUID: params.UUID}
	err := ovnSBClient.Client.Get(ctx, encap)
	if err != nil {
		if strings.Contains(err.Error(), "object not found") {
			return nil, NewOvnError(ErrNotFound, "encap not found: %+v", *params)
		}
		return nil, fmt.Errorf("failed to get encap: %w", err)
	}

	return encap, nil
}
