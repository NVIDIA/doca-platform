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

package controllers

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchRegisterer is an interface that registers watches in remote cache for a given cluster
type WatchRegisterer interface {
	RegisterWatchesForCluster(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error
}

// RequestWithCluster is a request with a cluster key to identify the cluster that originated the request
type RequestWithCluster struct {
	ctrl.Request
	cluster client.ObjectKey
}
