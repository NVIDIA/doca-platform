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

package controller

import (
	"fmt"
	"math"

	"antrea.io/antrea/pkg/ovs/openflow"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
)

var _ = Describe("SecureConnection disconnect handling", func() {
	var (
		mockCtrl       *gomock.Controller
		ofb            *MockBridge
		sc             *SecureConnection
		reconnectCount int
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		ofb = NewMockBridge(mockCtrl)
		reconnectCount = 0
		sc = &SecureConnection{
			OFBridge: ofb,
			OnReconnected: func() error {
				reconnectCount++
				return nil
			},
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("deletes all flows on disconnect", func() {
		ofb.EXPECT().DumpFlows(uint64(0), uint64(0)).Return(map[uint64]*openflow.FlowStates{100: {}}, nil).Times(1)
		ofb.EXPECT().DeleteFlowsByCookie(uint64(100), uint64(math.MaxUint64)).Return(nil).Times(1)

		Expect(sc.processDisconnectEvent()).To(Succeed())
		Expect(reconnectCount).To(BeZero())
	})

	It("returns an error when only some flow cookies are deleted", func() {
		// One cookie delete succeeds before the other fails, diverging OVS state from the cache.
		ofb.EXPECT().DumpFlows(uint64(0), uint64(0)).
			Return(map[uint64]*openflow.FlowStates{100: {}, 200: {}}, nil).Times(1)
		ofb.EXPECT().DeleteFlowsByCookie(gomock.Any(), uint64(math.MaxUint64)).Return(nil).Times(1)
		ofb.EXPECT().DeleteFlowsByCookie(gomock.Any(), uint64(math.MaxUint64)).Return(fmt.Errorf("ovs busy")).Times(1)

		Expect(sc.processDisconnectEvent()).ToNot(Succeed())
		Expect(reconnectCount).To(BeZero())
	})

	It("succeeds when there was nothing to delete", func() {
		ofb.EXPECT().DumpFlows(uint64(0), uint64(0)).Return(map[uint64]*openflow.FlowStates{}, nil).Times(1)

		Expect(sc.processDisconnectEvent()).To(Succeed())
		Expect(reconnectCount).To(BeZero())
	})

	It("propagates the error when flows can't be listed", func() {
		ofb.EXPECT().DumpFlows(uint64(0), uint64(0)).Return(nil, fmt.Errorf("ovs unreachable")).Times(1)

		Expect(sc.processDisconnectEvent()).ToNot(Succeed())
		Expect(reconnectCount).To(BeZero())
	})

	It("triggers resync only after a disconnected connection recovers", func() {
		ofb.EXPECT().DumpFlows(uint64(0), uint64(0)).Return(map[uint64]*openflow.FlowStates{}, nil).Times(1)

		disconnected := sc.handleConnectionCheck(fmt.Errorf("API unavailable"), false)
		Expect(disconnected).To(BeTrue())
		Expect(reconnectCount).To(BeZero())

		disconnected = sc.handleConnectionCheck(nil, disconnected)
		Expect(disconnected).To(BeFalse())
		Expect(reconnectCount).To(Equal(1))

		disconnected = sc.handleConnectionCheck(nil, disconnected)
		Expect(disconnected).To(BeFalse())
		Expect(reconnectCount).To(Equal(1))
	})

	It("tolerates a nil OnReconnected callback", func() {
		sc.OnReconnected = nil
		Expect(sc.handleConnectionCheck(nil, true)).To(BeFalse())
	})

	It("retries OnReconnected after a callback failure", func() {
		sc.OnReconnected = func() error {
			reconnectCount++
			if reconnectCount == 1 {
				return fmt.Errorf("resync failed")
			}
			return nil
		}

		disconnected := sc.handleConnectionCheck(nil, true)
		Expect(disconnected).To(BeTrue())
		disconnected = sc.handleConnectionCheck(nil, disconnected)
		Expect(disconnected).To(BeFalse())
		Expect(reconnectCount).To(Equal(2))
	})
})
