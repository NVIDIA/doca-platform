/*
  COPYRIGHT 2026 NVIDIA
  Licensed under the Apache License, Version 2.0 (the License);
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an AS IS BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/pkg/openflow"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

//nolint:goconst
var _ = Describe("servicechain GenerateAndApplyOpenFlows", func() {
	var (
		mockCtrl     *gomock.Controller
		ctx          = context.Background()
		sc           *ServiceChain
		ports        [][]string
		openflowMock *openflow.MockOpenFlowAPI
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		openflowMock = openflow.NewMockOpenFlowAPI(mockCtrl)
		sc = &ServiceChain{OPFlow: openflowMock}
		ports = nil
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should succeed when ports is empty", func() {
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has one port", func() {
		ports = [][]string{{"1"}}
		// no flows should be added
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has two ports", func() {
		ports = [][]string{{"1", "2"}}
		expectedFlows := `cookie=0, table=0, priority=20, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2
cookie=0, table=0, priority=20, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1`
		openflowMock.EXPECT().Add(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has 3 ports", func() {
		ports = [][]string{{"1", "2", "3"}}
		expectedFlows := `cookie=0, table=0, priority=20, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2,output:3
cookie=0, table=0, priority=20, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,output:3
cookie=0, table=0, priority=20, in_port=3 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,output:2`
		openflowMock.EXPECT().Add(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has 2 groups ports", func() {
		ports = [][]string{{"1", "2"}, {"3", "4"}}
		expectedFlows := `cookie=0, table=0, priority=20, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2
cookie=0, table=0, priority=20, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1`
		openflowMock.EXPECT().Add(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		expectedFlows = `cookie=0, table=0, priority=20, in_port=3 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=4,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:4
cookie=0, table=0, priority=20, in_port=4 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:3`
		openflowMock.EXPECT().Add(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should contionue even if one of the flows fails", func() {
		ports = [][]string{{"1", "2"}, {"3", "4"}}
		openflowMock.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("add flows failed"))
		openflowMock.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(MatchError(ContainSubstring("add flows failed")))
	})

	It("should fail when adding flows fails", func() {
		ports = [][]string{{"1", "2"}}
		openflowMock.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("add flows failed"))
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(MatchError(ContainSubstring("add flows failed")))
	})
})
