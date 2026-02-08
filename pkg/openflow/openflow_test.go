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

const (
	testMeterStr = "meter=1,kbps,burst,stats,bands=type=drop,rate=1000"
	testFlowStr  = "cookie=0, table=0, priority=1, actions=normal"
)

//nolint:goconst
var _ = Describe("AddFlows", func() {
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

	DescribeTable("should handle empty parameters gracefully",
		func(flowStr, bridge string) {
			Expect(flows.AddFlows(context.Background(), flowStr, bridge)).To(Succeed())
		},
		Entry("when flows is empty", "", bridgeName),
		Entry("when bridgeName is empty", "some flow", ""),
	)

	It("should succeed when flows and bridgeName are not empty", func() {
		flowsStr := "learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2"
		execMock.EXPECT().LookPath("ovs-ofctl").Return("ovs-ofctl", nil)
		execMock.EXPECT().
			CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "--bundle", "add-flows", bridgeName, gomock.Any()).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(nil)
		Expect(flows.AddFlows(context.Background(), flowsStr, bridgeName)).To(Succeed())
	})

	DescribeTable("should handle errors appropriately",
		func(setupMock func(), expectedError string) {
			flowsStr := "learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,learn(cookie=0,idle_timeout=10,table=0,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2"
			setupMock()
			Expect(flows.AddFlows(context.Background(), flowsStr, bridgeName)).To(MatchError(ContainSubstring(expectedError)))
		},
		Entry("when ovs-ofctl path not found",
			func() {
				execMock.EXPECT().LookPath("ovs-ofctl").Return("ovs-ofctl", fmt.Errorf("ovs-ofctl not found"))
			},
			"ovs-ofctl not found",
		),
		Entry("when ovs-ofctl command fails",
			func() {
				execMock.EXPECT().LookPath("ovs-ofctl").Return("ovs-ofctl", nil)
				execMock.EXPECT().
					CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "--bundle", "add-flows", bridgeName, gomock.Any()).
					Return(cmdMock)
				cmdMock.EXPECT().SetStderr(gomock.Any())
				cmdMock.EXPECT().Run().Return(fmt.Errorf("ovs-ofctl command failed"))
			},
			"ovs-ofctl command failed",
		),
	)
})

var _ = Describe("AddMeter", func() {
	var (
		mockCtrl   *gomock.Controller
		flows      *OpenFlow
		execMock   *MockInterface
		cmdMock    *MockCmd
		bridgeName = "br-sfc"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		execMock = NewMockInterface(mockCtrl)
		cmdMock = NewMockCmd(mockCtrl)
		flows = &OpenFlow{
			Exec:         execMock,
			OVSOfctlPath: "ovs-ofctl",
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	DescribeTable("should handle empty parameters gracefully",
		func(meterStr, bridge string) {
			Expect(flows.AddMeter(context.Background(), meterStr, bridge)).To(Succeed())
		},
		Entry("when meter is empty", "", bridgeName),
		Entry("when bridgeName is empty", testMeterStr, ""),
	)

	It("should succeed when meter and bridgeName are not empty", func() {
		execMock.EXPECT().
			CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "-O", "OpenFlow13", "add-meter", bridgeName, testMeterStr).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(nil)
		Expect(flows.AddMeter(context.Background(), testMeterStr, bridgeName)).To(Succeed())
	})

	It("should succeed when meter already exists", func() {
		execMock.EXPECT().
			CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "-O", "OpenFlow13", "add-meter", bridgeName, testMeterStr).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any()).Do(func(stderr interface{}) {
			// Write the OFPMMFC_METER_EXISTS error to stderr
			_, _ = stderr.(interface{ Write([]byte) (int, error) }).Write([]byte("ovs-ofctl: OFPMMFC_METER_EXISTS\n"))
		})
		cmdMock.EXPECT().Run().Return(fmt.Errorf("exit status 1"))
		Expect(flows.AddMeter(context.Background(), testMeterStr, bridgeName)).To(Succeed())
	})

	DescribeTable("should handle errors appropriately",
		func(needsUninitializedFlow bool, setupMock func(), expectedError string) {
			testFlow := flows
			if needsUninitializedFlow {
				testFlow = &OpenFlow{Exec: execMock}
			}
			setupMock()
			Expect(testFlow.AddMeter(context.Background(), testMeterStr, bridgeName)).To(MatchError(ContainSubstring(expectedError)))
		},
		Entry("when ovs-ofctl path not found",
			true,
			func() {
				execMock.EXPECT().LookPath("ovs-ofctl").Return("", fmt.Errorf("ovs-ofctl not found"))
			},
			"ovs-ofctl not found",
		),
		Entry("when ovs-ofctl command fails with non-METER_EXISTS error",
			false,
			func() {
				execMock.EXPECT().
					CommandContext(gomock.Any(), "ovs-ofctl", "-t", "5", "-O", "OpenFlow13", "add-meter", bridgeName, testMeterStr).
					Return(cmdMock)
				cmdMock.EXPECT().SetStderr(gomock.Any()).Do(func(stderr interface{}) {
					// Write a different error to stderr (not METER_EXISTS)
					_, _ = stderr.(interface{ Write([]byte) (int, error) }).Write([]byte("ovs-ofctl: some other error\n"))
				})
				cmdMock.EXPECT().Run().Return(fmt.Errorf("ovs-ofctl command failed"))
			},
			"ovs-ofctl command failed",
		),
	)
})

var _ = Describe("ensureOVSOfctlPath", func() {
	var (
		mockCtrl   *gomock.Controller
		flows      *OpenFlow
		execMock   *MockInterface
		cmdMock    *MockCmd
		bridgeName = "br-sfc"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		execMock = NewMockInterface(mockCtrl)
		cmdMock = NewMockCmd(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should successfully initialize OVSOfctlPath on first call", func() {
		// Create OpenFlow without OVSOfctlPath set
		flows = &OpenFlow{
			Exec: execMock,
		}

		// Expect LookPath to be called once
		execMock.EXPECT().LookPath("ovs-ofctl").Return("/usr/bin/ovs-ofctl", nil)

		// Mock the actual command execution
		execMock.EXPECT().
			CommandContext(gomock.Any(), "/usr/bin/ovs-ofctl", "-t", "5", "--bundle", "add-flows", bridgeName, gomock.Any()).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(nil)

		// Call should succeed and initialize OVSOfctlPath
		Expect(flows.AddFlows(context.Background(), testFlowStr, bridgeName)).To(Succeed())
		Expect(flows.OVSOfctlPath).To(Equal("/usr/bin/ovs-ofctl"))
	})

	It("should not call LookPath if OVSOfctlPath is already set", func() {
		// Create OpenFlow with OVSOfctlPath already set
		flows = &OpenFlow{
			Exec:         execMock,
			OVSOfctlPath: "/usr/bin/ovs-ofctl",
		}

		// LookPath should NOT be called
		// execMock.EXPECT().LookPath("ovs-ofctl") - deliberately not set

		// Mock the actual command execution
		execMock.EXPECT().
			CommandContext(gomock.Any(), "/usr/bin/ovs-ofctl", "-t", "5", "--bundle", "add-flows", bridgeName, gomock.Any()).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(nil)

		// Call should succeed without calling LookPath
		Expect(flows.AddFlows(context.Background(), testFlowStr, bridgeName)).To(Succeed())
	})

	It("should fail if ovs-ofctl is not found in PATH", func() {
		// Create OpenFlow without OVSOfctlPath set
		flows = &OpenFlow{
			Exec: execMock,
		}

		// LookPath returns error
		execMock.EXPECT().LookPath("ovs-ofctl").Return("", fmt.Errorf("executable file not found in $PATH"))

		// Call should fail with LookPath error
		err := flows.AddFlows(context.Background(), testFlowStr, bridgeName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("executable file not found in $PATH"))
	})

	It("should work with AddMeter when path is not initialized", func() {
		// Create OpenFlow without OVSOfctlPath set
		flows = &OpenFlow{
			Exec: execMock,
		}

		// Expect LookPath to be called
		execMock.EXPECT().LookPath("ovs-ofctl").Return("/usr/bin/ovs-ofctl", nil)

		// Mock the actual command execution
		execMock.EXPECT().
			CommandContext(gomock.Any(), "/usr/bin/ovs-ofctl", "-t", "5", "-O", "OpenFlow13", "add-meter", bridgeName, testMeterStr).
			Return(cmdMock)
		cmdMock.EXPECT().SetStderr(gomock.Any())
		cmdMock.EXPECT().Run().Return(nil)

		// Call should succeed and initialize OVSOfctlPath
		Expect(flows.AddMeter(context.Background(), testMeterStr, bridgeName)).To(Succeed())
		Expect(flows.OVSOfctlPath).To(Equal("/usr/bin/ovs-ofctl"))
	})
})
