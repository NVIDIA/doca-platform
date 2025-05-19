/*
Copyright 2025 NVIDIA

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

package manager

import (
	"context"
	"testing"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/controller/clusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Manager Suite")
}

type dummyGRPCHandler struct {
	csi.ControllerServer
	csi.NodeServer
	csi.IdentityServer
}

func (d *dummyGRPCHandler) ControllerGetCapabilities(
	ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				}},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				}},
			},
		},
	}, nil
}

func (d *dummyGRPCHandler) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: "test",
	}, nil
}

var _ clusterhelper.Helper = &TestClusterHelper{}

type TestClusterHelper struct {
	*runner.FakeRunnable
}

func (ch *TestClusterHelper) GetHostClusterClient(_ context.Context) (client.Client, error) {
	return nil, nil
}

func (ch *TestClusterHelper) GetDPUClusterClient(_ context.Context) (client.Client, error) {
	return nil, nil
}
