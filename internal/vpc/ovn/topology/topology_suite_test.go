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

package topology_test

import (
	"context"
	"testing"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/testutils"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ovn-org/libovsdb/client"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type Config struct {
	ImageName string `envconfig:"OVN_CENTRAL_IMAGE_NAME" default:"ovn-central:v24.03.0"`
}

var (
	ovnClient         ovnlib.OVNWrapper
	ctx, testCancelfn = context.WithCancel(ctrl.SetupSignalHandler())
)

var _ = BeforeSuite(func() {
	// Set up a logger
	ctrllog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx = ctrllog.IntoContext(context.Background(), ctrllog.Log)
	var err error

	// Parse env variables
	var config Config
	err = envconfig.Process("", &config)
	Expect(err).NotTo(HaveOccurred(), "failed to parse environment variables: %v", err)

	// Start OVN container
	stopFn, endpoint, _, err := testutils.StartOvnContainer(config.ImageName)
	Expect(err).NotTo(HaveOccurred(), "failed to start OVN container: %v", err)
	DeferCleanup(stopFn)

	// Wait for container to be ready
	time.Sleep(5 * time.Second)
	ovnNBConfig := &ovnlib.Config{
		EndPoint:           endpoint,
		OVNNBReconnectTime: 5,
	}
	ovnClient, err = ovnlib.GetOvnNBClient(ctx, ovnNBConfig, []client.Option{})
	Expect(err).NotTo(HaveOccurred(), "failed to create OVN client")
	DeferCleanup(ovnClient.Close)
})

var _ = AfterSuite(func() {
	testCancelfn()
})

func TestTopologyManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Topology Manager Suite")
}
