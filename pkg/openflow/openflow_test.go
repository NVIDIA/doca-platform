/*
Copyright 2026 NVIDIA

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

package openflow

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

//nolint:goconst
var _ = Describe("flows", func() {
	var (
		mockCtrl   *gomock.Controller
		flows      OpenFlowAPI
		execMock   *MockInterface
		cmdMock    *MockCmd
		bridgeName = "br-sfc"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		execMock = NewMockInterface(mockCtrl)
		cmdMock = NewMockCmd(mockCtrl)
		flows = &OpenFlow{Exec: execMock}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should succeed when flows is empty", func() {
		Expect(flows.Add(context.Background(), "", bridgeName)).To(Succeed())
	})

	It("should succeed when bridgeName is empty", func() {
		Expect(flows.Add(context.Background(), "some flow", "")).To(Succeed())
	})

	It("should succeed when flows and bridgeName are not empty", func() {
		flowsStr := "learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2"
		execMock.EXPECT().LookPath("ovs-ofctl").Return("ovs-ofctl", nil)
		execMock.EXPECT().
			CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "--bundle", "add-flows", bridgeName, gomock.Any()).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(nil)
		Expect(flows.Add(context.Background(), flowsStr, bridgeName)).To(Succeed())
	})

	It("should fail if ovs-vsctl path not found", func() {
		flowsStr := "learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2"
		execMock.EXPECT().LookPath("ovs-ofctl").Return("ovs-ofctl", fmt.Errorf("ovs-ofctl not found"))
		Expect(flows.Add(context.Background(), flowsStr, bridgeName)).To(MatchError(ContainSubstring("ovs-ofctl not found")))
	})

	It("should fail if ovs-ofctl command fails", func() {
		flowsStr := "learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2"
		execMock.EXPECT().LookPath("ovs-ofctl").Return("ovs-ofctl", nil)
		execMock.EXPECT().
			CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "--bundle", "add-flows", bridgeName, gomock.Any()).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(fmt.Errorf("ovs-ofctl command failed"))
		Expect(flows.Add(context.Background(), flowsStr, bridgeName)).To(MatchError(ContainSubstring("ovs-ofctl command failed")))
	})
})
