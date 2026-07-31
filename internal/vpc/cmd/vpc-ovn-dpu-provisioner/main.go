//go:build linux

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

package main

import (
	"context"
	"os"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/nodeutils"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/provisioner"

	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"
	"k8s.io/klog/v2"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	klog.Info("Starting VPC DPU Provisioner")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config, err := config.GetConfig()
	if err != nil {
		klog.Error(err, "failed to get config")
		os.Exit(1)
	}

	// Create a new client.Client
	k8sClient, err := k8sclient.New(config, k8sclient.Options{})
	if err != nil {
		klog.Error(err, "failed to create k8s client")
		os.Exit(1)
	}

	ovsClient, err := nodeutils.InitializeOVSClient(ctx)
	if err != nil {
		klog.Error(err, "failed to initialize OVS client")
		os.Exit(1)
	}

	dpuVPCProvisionerConfig := provisioner.Config{}
	// load and validate configuration from environment variables
	if err := dpuVPCProvisionerConfig.FromEnv(); err != nil {
		klog.Error(err, "failed to parse environment variables")
		os.Exit(1)
	}

	dpuVPCProvisioner := provisioner.NewVPCOVNDPUProvisioner(ctx, &dpuVPCProvisionerConfig, networkhelper.New(), k8sClient, ovsClient)
	if err := dpuVPCProvisioner.Provision(); err != nil {
		klog.Error(err, "failed to provision DPU VPC Provisioner")
		os.Exit(1)
	}
	klog.Info("VPC DPU Provisioner is ready")
}
