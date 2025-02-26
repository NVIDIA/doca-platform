/*
Copyright 2024 NVIDIA

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

package ovshelper_test

import (
	"errors"
	"time"

	"github.com/nvidia/doca-platform/internal/ovshelper"
	ovsclient "github.com/nvidia/doca-platform/internal/utils/ovsclient"
	ovsclientMock "github.com/nvidia/doca-platform/internal/utils/ovsclient/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	clock "k8s.io/utils/clock/testing"
)

var _ = Describe("OVS Helper", func() {
	Context("When it runs once", func() {
		var ovsClient *ovsclientMock.MockOVSClient
		var handler *ovshelper.OVSHelper
		BeforeEach(func() {
			testCtrl := gomock.NewController(GinkgoT())
			ovsClient = ovsclientMock.NewMockOVSClient(testCtrl)
			handler = ovshelper.New(clock.NewFakeClock(time.Now()), ovsClient)
		})
		It("should not do anything if interfaces exist and all have pmd rx queues", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
			}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
			}, nil)
			Expect(handler.RunOnce()).To(Succeed())
		})
		It("should not do anything if no interfaces exist", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(map[string]interface{}{}, nil)
			Expect(handler.RunOnce()).To(Succeed())
		})
		It("should not do anything if listing interfaces errors out", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(nil, errors.New(""))
			Expect(handler.RunOnce()).To(HaveOccurred())
		})
		It("should not do anything if getting pmd rx queues errors out", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(nil, errors.New(""))
			Expect(handler.RunOnce()).To(HaveOccurred())
		})
		It("should replug the interface if interface is pmd rx queue doesn't exist for that interface", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
				"intf3": struct{}{},
			}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
			}, nil)

			ovsClient.EXPECT().GetPortExternalIDs("intf3").Return(map[string]string{
				"port_external_id_key1": "port_external_id_value1",
				"port_external_id_key2": "port_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceExternalIDs("intf3").Return(map[string]string{
				"intf_external_id_key1": "intf_external_id_value1",
				"intf_external_id_key2": "intf_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceOfPort("intf3").Return(5, nil)
			ovsClient.EXPECT().InterfaceToBridge("intf3").Return("my-bridge", nil)

			ovsClient.EXPECT().DeletePort("intf3").Return(nil)
			ovsClient.EXPECT().AddPortWithMetadata(
				"my-bridge",
				"intf3",
				ovsclient.DPDK,
				map[string]string{
					"port_external_id_key1": "port_external_id_value1",
					"port_external_id_key2": "port_external_id_value2",
				},
				map[string]string{
					"intf_external_id_key1": "intf_external_id_value1",
					"intf_external_id_key2": "intf_external_id_value2",
				},
				5,
			).Return(nil)
			Expect(handler.RunOnce()).To(Succeed())
		})
		It("should not delete a faulty interface until all metadata are acquired", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
				"intf3": struct{}{},
			}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
			}, nil)

			ovsClient.EXPECT().GetPortExternalIDs("intf3").Return(map[string]string{
				"port_external_id_key1": "port_external_id_value1",
				"port_external_id_key2": "port_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceExternalIDs("intf3").Return(map[string]string{
				"intf_external_id_key1": "intf_external_id_value1",
				"intf_external_id_key2": "intf_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceOfPort("intf3").Return(5, nil)
			// Fail in the last get metadata call
			ovsClient.EXPECT().InterfaceToBridge("intf3").Return("", errors.New(""))

			Expect(handler.RunOnce()).To(HaveOccurred())
		})
		It("should not delete a faulty interface if ofport is negative", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
				"intf3": struct{}{},
			}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
			}, nil)

			ovsClient.EXPECT().GetPortExternalIDs("intf3").Return(map[string]string{
				"port_external_id_key1": "port_external_id_value1",
				"port_external_id_key2": "port_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceExternalIDs("intf3").Return(map[string]string{
				"intf_external_id_key1": "intf_external_id_value1",
				"intf_external_id_key2": "intf_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceOfPort("intf3").Return(-1, nil)

			Expect(handler.RunOnce()).To(HaveOccurred())
		})
		It("should retry to plug faulty interface if command failed", func() {
			ovsClient.EXPECT().ListInterfaces(ovsclient.DPDK).Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
				"intf3": struct{}{},
			}, nil)
			ovsClient.EXPECT().GetInterfacesWithPMDRXQueue().Return(map[string]interface{}{
				"intf1": struct{}{},
				"intf2": struct{}{},
			}, nil)

			ovsClient.EXPECT().GetPortExternalIDs("intf3").Return(map[string]string{
				"port_external_id_key1": "port_external_id_value1",
				"port_external_id_key2": "port_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceExternalIDs("intf3").Return(map[string]string{
				"intf_external_id_key1": "intf_external_id_value1",
				"intf_external_id_key2": "intf_external_id_value2",
			}, nil)
			ovsClient.EXPECT().GetInterfaceOfPort("intf3").Return(5, nil)
			ovsClient.EXPECT().InterfaceToBridge("intf3").Return("my-bridge", nil)

			ovsClient.EXPECT().DeletePort("intf3").Return(nil)

			// Fail this one
			ovsClient.EXPECT().AddPortWithMetadata(
				"my-bridge",
				"intf3",
				ovsclient.DPDK,
				map[string]string{
					"port_external_id_key1": "port_external_id_value1",
					"port_external_id_key2": "port_external_id_value2",
				},
				map[string]string{
					"intf_external_id_key1": "intf_external_id_value1",
					"intf_external_id_key2": "intf_external_id_value2",
				},
				5,
			).Return(errors.New(""))

			// Succeed this one
			ovsClient.EXPECT().AddPortWithMetadata(
				"my-bridge",
				"intf3",
				ovsclient.DPDK,
				map[string]string{
					"port_external_id_key1": "port_external_id_value1",
					"port_external_id_key2": "port_external_id_value2",
				},
				map[string]string{
					"intf_external_id_key1": "intf_external_id_value1",
					"intf_external_id_key2": "intf_external_id_value2",
				},
				5,
			).Return(nil)
			Expect(handler.RunOnce()).To(Succeed())
		})
	})
})
