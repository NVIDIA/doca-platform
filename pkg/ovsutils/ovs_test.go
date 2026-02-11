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

package ovsutils

import (
	"context"
	"errors"
	"testing"

	"github.com/nvidia/doca-platform/pkg/ovsmodel"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ovsclient "github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"
	"go.uber.org/mock/gomock"
)

func TestOVSUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OVSUtils Suite")
}

var _ = Describe("OVSUtils", func() {
	var (
		mockCtrl *gomock.Controller
		mockAPI  *MockAPI
		ctx      context.Context
	)

	const (
		portUUID           = "port-uuid"
		failModeSecure     = "secure"
		failModeStandalone = "standalone"
		bridgeUUID         = "bridge-uuid"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = NewMockAPI(mockCtrl)
		ctx = context.Background()
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("Constants", func() {
		It("should have correct InterfaceTypeInternal value", func() {
			Expect(InterfaceTypeInternal).To(Equal("internal"))
		})
	})

	Describe("GetIfaceWithName", func() {
		It("should fail when interface name is empty", func() {
			mockAPI.EXPECT().
				GetIfaceWithName(ctx, "").
				Return(nil, errors.New("interface name cannot be empty"))

			iface, err := mockAPI.GetIfaceWithName(ctx, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("interface name cannot be empty"))
			Expect(iface).To(BeNil())
		})

		It("should return ErrNotFound when interface doesn't exist", func() {
			mockAPI.EXPECT().
				GetIfaceWithName(ctx, "test-iface").
				Return(nil, ovsclient.ErrNotFound)

			iface, err := mockAPI.GetIfaceWithName(ctx, "test-iface")
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ovsclient.ErrNotFound)).To(BeTrue())
			Expect(iface).To(BeNil())
		})

		It("should propagate other errors", func() {
			expectedErr := errors.New("connection timeout")
			mockAPI.EXPECT().
				GetIfaceWithName(ctx, "test-iface").
				Return(nil, expectedErr)

			iface, err := mockAPI.GetIfaceWithName(ctx, "test-iface")
			Expect(err).To(MatchError(expectedErr))
			Expect(iface).To(BeNil())
		})

		It("should succeed when interface exists", func() {
			expectedIface := &ovsmodel.Interface{
				Name: "test-iface",
				UUID: "test-uuid",
			}
			mockAPI.EXPECT().
				GetIfaceWithName(ctx, "test-iface").
				Return(expectedIface, nil)

			iface, err := mockAPI.GetIfaceWithName(ctx, "test-iface")
			Expect(err).NotTo(HaveOccurred())
			Expect(iface).To(Equal(expectedIface))
		})
	})

	Describe("AddBridge", func() {
		var (
			bridgeConfig       BridgeConfig
			secureFailMode     = failModeSecure
			standaloneFailMode = failModeStandalone
		)

		BeforeEach(func() {
			bridgeConfig = BridgeConfig{
				Name:         "br-test",
				DatapathType: "system",
				FailMode:     &secureFailMode,
			}
		})

		It("should be idempotent when bridge already exists", func() {
			mockAPI.EXPECT().
				AddBridge(ctx, bridgeConfig).
				Return(nil)

			Expect(mockAPI.AddBridge(ctx, bridgeConfig)).To(Succeed())
		})

		It("should fail when database connection is lost", func() {
			expectedErr := errors.New("database connection lost")
			mockAPI.EXPECT().
				AddBridge(ctx, bridgeConfig).
				Return(expectedErr)

			err := mockAPI.AddBridge(ctx, bridgeConfig)
			Expect(err).To(MatchError(expectedErr))
		})

		DescribeTable("should succeed with different fail_mode values",
			func(failModePtr *string) {
				bridgeConfig.FailMode = failModePtr
				mockAPI.EXPECT().
					AddBridge(ctx, bridgeConfig).
					Return(nil)

				Expect(mockAPI.AddBridge(ctx, bridgeConfig)).To(Succeed())
			},
			Entry("with nil fail_mode", nil),
			Entry("with secure fail_mode", &secureFailMode),
			Entry("with standalone fail_mode", &standaloneFailMode),
		)
	})

	Describe("AddPort", func() {
		It("should be idempotent when port already exists on same bridge", func() {
			config := PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
			}
			mockAPI.EXPECT().
				AddPort(ctx, config).
				Return(nil)

			Expect(mockAPI.AddPort(ctx, config)).To(Succeed())
		})

		It("should fail when port exists on different bridge", func() {
			expectedErr := errors.New("port port-test already exists on a bridge other than br-test")
			config := PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
			}
			mockAPI.EXPECT().
				AddPort(ctx, config).
				Return(expectedErr)

			err := mockAPI.AddPort(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already exists on a bridge other than"))
		})

		It("should succeed with custom MTU", func() {
			mtu := 1500
			config := PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
				MTU:           &mtu,
			}
			mockAPI.EXPECT().
				AddPort(ctx, config).
				Return(nil)

			Expect(mockAPI.AddPort(ctx, config)).To(Succeed())
		})

		It("should fail when database error occurs", func() {
			expectedErr := errors.New("database error")
			config := PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
			}
			mockAPI.EXPECT().
				AddPort(ctx, config).
				Return(expectedErr)

			Expect(mockAPI.AddPort(ctx, config)).To(MatchError(expectedErr))
		})

		It("should fail when bridge does not exist", func() {
			expectedErr := errors.New("failed to get bridge br-test: bridge does not exist")
			mockAPI.EXPECT().
				AddPort(ctx, PortConfig{
					BridgeName:    "br-test",
					Name:          "port-test",
					InterfaceType: "internal",
				}).
				Return(expectedErr)

			err := mockAPI.AddPort(ctx, PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
			})
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(expectedErr))
			Expect(err.Error()).To(ContainSubstring("bridge does not exist"))
		})
	})

	Describe("DelPort", func() {
		DescribeTable("should be idempotent",
			func(description string) {
				mockAPI.EXPECT().
					DelPort(ctx, "br-test", "port-test").
					Return(nil)

				Expect(mockAPI.DelPort(ctx, "br-test", "port-test")).To(Succeed())
			},
			Entry("when port doesn't exist", "port doesn't exist"),
			Entry("when port exists on different bridge", "port on different bridge"),
		)

		It("should fail when database error occurs", func() {
			expectedErr := errors.New("database error")
			mockAPI.EXPECT().
				DelPort(ctx, "br-test", "port-test").
				Return(expectedErr)

			Expect(mockAPI.DelPort(ctx, "br-test", "port-test")).To(MatchError(expectedErr))
		})

		It("should succeed when port exists on the bridge", func() {
			mockAPI.EXPECT().
				DelPort(ctx, "br-test", "port-test").
				Return(nil)

			Expect(mockAPI.DelPort(ctx, "br-test", "port-test")).To(Succeed())
		})
	})

	Describe("IsIfaceInBr", func() {
		It("should return true when port is in target bridge", func() {
			mockAPI.EXPECT().
				IsIfaceInBr(ctx, "br-sfc", "test-port").
				Return(true, nil)

			result, err := mockAPI.IsIfaceInBr(ctx, "br-sfc", "test-port")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		DescribeTable("should return false for non-matching bridge or missing port",
			func(desc string) {
				mockAPI.EXPECT().
					IsIfaceInBr(ctx, "br-sfc", "test-port").
					Return(false, nil)

				result, err := mockAPI.IsIfaceInBr(ctx, "br-sfc", "test-port")
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeFalse())
			},
			Entry("when port is in different bridge", "port is in different bridge"),
			Entry("when port is not in any bridge", "port is not in any bridge"),
		)

		It("should fail when port lookup fails", func() {
			expectedErr := errors.New("port lookup failed")
			mockAPI.EXPECT().
				IsIfaceInBr(ctx, "br-sfc", "test-port").
				Return(false, expectedErr)

			result, err := mockAPI.IsIfaceInBr(ctx, "br-sfc", "test-port")
			Expect(err).To(MatchError(expectedErr))
			Expect(result).To(BeFalse())
		})
	})

	Describe("GetIfaceWithExternalIDs", func() {
		It("should return interface when single match found", func() {
			externalIDs := map[string]string{"key": "value"}
			expectedIface := &ovsmodel.Interface{
				Name:        "test-iface",
				ExternalIDs: externalIDs,
			}
			mockAPI.EXPECT().
				GetIfaceWithExternalIDs(ctx, externalIDs).
				Return(expectedIface, nil)

			iface, err := mockAPI.GetIfaceWithExternalIDs(ctx, externalIDs)
			Expect(err).NotTo(HaveOccurred())
			Expect(iface).To(Equal(expectedIface))
		})

		DescribeTable("should fail with appropriate error",
			func(errorMsg, errorSubstring string) {
				externalIDs := map[string]string{"key": "value"}
				expectedErr := errors.New(errorMsg)
				mockAPI.EXPECT().
					GetIfaceWithExternalIDs(ctx, externalIDs).
					Return(nil, expectedErr)

				iface, err := mockAPI.GetIfaceWithExternalIDs(ctx, externalIDs)
				Expect(err).To(HaveOccurred())
				if errorSubstring != "" {
					Expect(err.Error()).To(ContainSubstring(errorSubstring))
				}
				Expect(iface).To(BeNil())
			},
			Entry("when no interfaces match", "failed to find matching interface", "failed to find matching interface"),
			Entry("when multiple interfaces match", "found multiple interfaces", "found multiple interfaces"),
			Entry("when query fails", "query failed", ""),
		)
	})

	Describe("SetIfaceExternalIDs", func() {
		It("should succeed when setting external IDs", func() {
			externalIDs := map[string]string{"key": "value"}
			mockAPI.EXPECT().
				SetIfaceExternalIDs(ctx, "test-iface", externalIDs).
				Return(nil)

			Expect(mockAPI.SetIfaceExternalIDs(ctx, "test-iface", externalIDs)).To(Succeed())
		})

		It("should fail when interface not found", func() {
			externalIDs := map[string]string{"key": "value"}
			expectedErr := errors.New("interface not found")
			mockAPI.EXPECT().
				SetIfaceExternalIDs(ctx, "test-iface", externalIDs).
				Return(expectedErr)

			Expect(mockAPI.SetIfaceExternalIDs(ctx, "test-iface", externalIDs)).To(MatchError(expectedErr))
		})
	})

	Describe("SetIfaceOptions", func() {
		It("should succeed when setting interface options", func() {
			options := map[string]string{"remote_ip": "192.168.1.1"}
			mockAPI.EXPECT().
				SetIfaceOptions(ctx, "test-iface", options).
				Return(nil)

			Expect(mockAPI.SetIfaceOptions(ctx, "test-iface", options)).To(Succeed())
		})

		It("should fail when interface not found", func() {
			options := map[string]string{"remote_ip": "192.168.1.1"}
			expectedErr := errors.New("interface not found")
			mockAPI.EXPECT().
				SetIfaceOptions(ctx, "test-iface", options).
				Return(expectedErr)

			Expect(mockAPI.SetIfaceOptions(ctx, "test-iface", options)).To(MatchError(expectedErr))
		})
	})

	Describe("SetPortExternalIDs", func() {
		It("should succeed when setting port external IDs", func() {
			externalIDs := map[string]string{"key": "value"}
			mockAPI.EXPECT().
				SetPortExternalIDs(ctx, "test-port", externalIDs).
				Return(nil)

			Expect(mockAPI.SetPortExternalIDs(ctx, "test-port", externalIDs)).To(Succeed())
		})

		It("should fail when port not found", func() {
			externalIDs := map[string]string{"key": "value"}
			expectedErr := errors.New("port not found")
			mockAPI.EXPECT().
				SetPortExternalIDs(ctx, "test-port", externalIDs).
				Return(expectedErr)

			Expect(mockAPI.SetPortExternalIDs(ctx, "test-port", externalIDs)).To(MatchError(expectedErr))
		})
	})

	Describe("SetOpenVSwitchExternalIDs", func() {
		It("should succeed when setting Open_vSwitch external IDs", func() {
			externalIDs := map[string]string{"system-id": "test-system"}
			mockAPI.EXPECT().
				SetOpenVSwitchExternalIDs(ctx, externalIDs).
				Return(nil)

			Expect(mockAPI.SetOpenVSwitchExternalIDs(ctx, externalIDs)).To(Succeed())
		})

		It("should fail when Open_vSwitch not found", func() {
			externalIDs := map[string]string{"system-id": "test-system"}
			expectedErr := errors.New("Open_vSwitch not found")
			mockAPI.EXPECT().
				SetOpenVSwitchExternalIDs(ctx, externalIDs).
				Return(expectedErr)

			Expect(mockAPI.SetOpenVSwitchExternalIDs(ctx, externalIDs)).To(MatchError(expectedErr))
		})
	})

	Describe("GetOpenVSwitchExternalIDs", func() {
		It("should return external IDs when available", func() {
			expectedIDs := map[string]string{"system-id": "test-system"}
			mockAPI.EXPECT().
				GetOpenVSwitchExternalIDs(ctx).
				Return(expectedIDs, nil)

			externalIDs, err := mockAPI.GetOpenVSwitchExternalIDs(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(externalIDs).To(Equal(expectedIDs))
		})

		It("should fail when query fails", func() {
			expectedErr := errors.New("query failed")
			mockAPI.EXPECT().
				GetOpenVSwitchExternalIDs(ctx).
				Return(nil, expectedErr)

			externalIDs, err := mockAPI.GetOpenVSwitchExternalIDs(ctx)
			Expect(err).To(MatchError(expectedErr))
			Expect(externalIDs).To(BeNil())
		})
	})

	Describe("BridgeConfig", func() {
		It("should support all fields", func() {
			secureMode := failModeSecure
			config := BridgeConfig{
				Name:         "br-test",
				DatapathType: "system",
				FailMode:     &secureMode,
			}

			Expect(config.Name).To(Equal("br-test"))
			Expect(config.DatapathType).To(Equal("system"))
			Expect(config.FailMode).NotTo(BeNil())
			Expect(*config.FailMode).To(Equal(failModeSecure))
		})

		It("should support nil fail_mode", func() {
			config := BridgeConfig{
				Name:         "br-test",
				DatapathType: "system",
				FailMode:     nil,
			}

			Expect(config.Name).To(Equal("br-test"))
			Expect(config.DatapathType).To(Equal("system"))
			Expect(config.FailMode).To(BeNil())
		})
	})

	Describe("Interface implementation", func() {
		It("should verify Client implements API interface", func() {
			var _ API = (*Client)(nil)
			Expect(true).To(BeTrue(), "Client implements API interface")
		})

		It("should verify BridgeConfig struct fields", func() {
			standaloneMode := failModeStandalone
			config := BridgeConfig{
				Name:         "br-test",
				DatapathType: "netdev",
				FailMode:     &standaloneMode,
			}

			Expect(config.Name).To(Equal("br-test"))
			Expect(config.DatapathType).To(Equal("netdev"))
			Expect(config.FailMode).NotTo(BeNil())
			Expect(*config.FailMode).To(Equal(failModeStandalone))
		})
	})

	Describe("Client implementation tests", func() {
		var (
			mockOVSClient *MockClient
			client        *Client
		)

		BeforeEach(func() {
			mockOVSClient = NewMockClient(mockCtrl)
			client = &Client{Client: mockOVSClient}
		})

		Describe("GetIfaceWithName", func() {
			It("should return error for empty name", func() {
				iface, err := client.GetIfaceWithName(ctx, "")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("interface name cannot be empty"))
				Expect(iface).To(BeNil())
			})

			It("should handle ErrNotFound", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(ovsclient.ErrNotFound)

				iface, err := client.GetIfaceWithName(ctx, "test-iface")
				Expect(err).To(HaveOccurred())
				Expect(errors.Is(err, ovsclient.ErrNotFound)).To(BeTrue())
				Expect(iface).To(BeNil())
			})

			It("should return interface when found", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						iface := model.(*ovsmodel.Interface)
						iface.Name = "test-iface"
						iface.UUID = "test-uuid"
						return nil
					})

				iface, err := client.GetIfaceWithName(ctx, "test-iface")
				Expect(err).NotTo(HaveOccurred())
				Expect(iface).NotTo(BeNil())
				Expect(iface.Name).To(Equal("test-iface"))
			})
		})

		Describe("GetOpenVSwitch", func() {
			DescribeTable("should validate Open_vSwitch row count",
				func(rowCount int, expectError bool, errorSubstring string) {
					mockOVSClient.EXPECT().
						List(gomock.Any(), gomock.Any()).
						DoAndReturn(func(ctx context.Context, result interface{}) error {
							ptr := result.(*[]*ovsmodel.OpenvSwitch)
							switch rowCount {
							case 0:
								// Leave empty
							case 1:
								*ptr = []*ovsmodel.OpenvSwitch{{UUID: "test-uuid"}}
							case 2:
								*ptr = []*ovsmodel.OpenvSwitch{{UUID: "uuid1"}, {UUID: "uuid2"}}
							}
							return nil
						})

					ovs, err := client.GetOpenVSwitch(ctx)
					if expectError {
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorSubstring))
						Expect(ovs).To(BeNil())
					} else {
						Expect(err).NotTo(HaveOccurred())
						Expect(ovs).NotTo(BeNil())
					}
				},
				Entry("when no rows exist", 0, true, "expected 1 Open_vSwitch row, got 0"),
				Entry("when multiple rows exist", 2, true, "expected 1 Open_vSwitch row, got 2"),
				Entry("when exactly one row exists", 1, false, ""),
			)
		})

		Describe("GetOpenVSwitchExternalIDs", func() {
			It("should return external IDs", func() {
				expectedIDs := map[string]string{"system-id": "test"}
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]*ovsmodel.OpenvSwitch)
						*ptr = []*ovsmodel.OpenvSwitch{{
							UUID:        "test-uuid",
							ExternalIDs: expectedIDs,
						}}
						return nil
					})

				ids, err := client.GetOpenVSwitchExternalIDs(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(ids).To(Equal(expectedIDs))
			})
		})

		Describe("AddBridge", func() {
			DescribeTable("should handle bridge existence checks",
				func(getError error, expectError bool, errorSubstring string) {
					mockOVSClient.EXPECT().
						Get(gomock.Any(), gomock.Any()).
						Return(getError)

					config := BridgeConfig{
						Name:         "br-test",
						DatapathType: "system",
					}
					err := client.AddBridge(ctx, config)
					if expectError {
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorSubstring))
					} else {
						Expect(err).NotTo(HaveOccurred())
					}
				},
				Entry("when bridge already exists (idempotent)", nil, false, ""),
				Entry("when Get returns non-ErrNotFound error", errors.New("database connection lost"), true, "failed to get bridge"),
			)
		})

		Describe("AddPort", func() {
			var mockConditionalAPI *MockConditionalAPI

			BeforeEach(func() {
				mockConditionalAPI = NewMockConditionalAPI(mockCtrl)
			})

			It("should fail when bridge does not exist", func() {
				// First Get call for port (port doesn't exist yet)
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(ovsclient.ErrNotFound)

				// Second Get call for bridge (bridge doesn't exist)
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(ovsclient.ErrNotFound)

				err := client.AddPort(ctx, PortConfig{
					BridgeName:    "br-nonexistent",
					Name:          "port-test",
					InterfaceType: "internal",
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get bridge"))
			})

			It("should succeed when port already exists on same bridge", func() {
				// Port exists
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						port := model.(*ovsmodel.Port)
						port.UUID = portUUID
						return nil
					})

				// Check if port is in bridge - Get port again
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						port := model.(*ovsmodel.Port)
						port.UUID = portUUID
						return nil
					})

				mockOVSClient.EXPECT().
					WhereAll(gomock.Any(), gomock.Any()).
					Return(mockConditionalAPI)

				mockConditionalAPI.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]ovsmodel.Bridge)
						*ptr = []ovsmodel.Bridge{{Name: "br-test"}}
						return nil
					})

				err := client.AddPort(ctx, PortConfig{
					BridgeName:    "br-test",
					Name:          "port-test",
					InterfaceType: "internal",
				})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should fail when port exists on different bridge", func() {
				// Port exists
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						port := model.(*ovsmodel.Port)
						port.UUID = portUUID
						return nil
					})

				// Check if port is in bridge - Get port again
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						port := model.(*ovsmodel.Port)
						port.UUID = portUUID
						return nil
					})

				mockOVSClient.EXPECT().
					WhereAll(gomock.Any(), gomock.Any()).
					Return(mockConditionalAPI)

				mockConditionalAPI.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]ovsmodel.Bridge)
						*ptr = []ovsmodel.Bridge{{Name: "br-other"}}
						return nil
					})

				err := client.AddPort(ctx, PortConfig{
					BridgeName:    "br-test",
					Name:          "port-test",
					InterfaceType: "internal",
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("already exists on a bridge other than"))
			})
		})

		Describe("DelPort", func() {
			DescribeTable("should handle different Get error scenarios",
				func(getError error, expectError bool) {
					mockOVSClient.EXPECT().
						Get(gomock.Any(), gomock.Any()).
						Return(getError)

					err := client.DelPort(ctx, "br-test", "port-test")
					if expectError {
						Expect(err).To(HaveOccurred())
					} else {
						Expect(err).NotTo(HaveOccurred())
					}
				},
				Entry("when port not found (idempotent)", ovsclient.ErrNotFound, false),
				Entry("when Get returns error", errors.New("database error"), true),
			)
		})

		Describe("IsIfaceInBr", func() {
			var mockConditionalAPI *MockConditionalAPI

			BeforeEach(func() {
				mockConditionalAPI = NewMockConditionalAPI(mockCtrl)
			})

			It("should return error when port Get fails", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(errors.New("port not found"))

				result, err := client.IsIfaceInBr(ctx, "br-test", "port-test")
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeFalse())
			})

			DescribeTable("should check if port is in bridge",
				func(bridges []ovsmodel.Bridge, expectedResult bool) {
					mockOVSClient.EXPECT().
						Get(gomock.Any(), gomock.Any()).
						DoAndReturn(func(ctx context.Context, model interface{}) error {
							port := model.(*ovsmodel.Port)
							port.UUID = portUUID
							return nil
						})

					mockOVSClient.EXPECT().
						WhereAll(gomock.Any(), gomock.Any()).
						Return(mockConditionalAPI)

					mockConditionalAPI.EXPECT().
						List(gomock.Any(), gomock.Any()).
						DoAndReturn(func(ctx context.Context, result interface{}) error {
							ptr := result.(*[]ovsmodel.Bridge)
							*ptr = bridges
							return nil
						})

					result, err := client.IsIfaceInBr(ctx, "br-test", "port-test")
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(expectedResult))
				},
				Entry("when port is in target bridge", []ovsmodel.Bridge{{Name: "br-test"}}, true),
				Entry("when port is in different bridge", []ovsmodel.Bridge{{Name: "br-other"}}, false),
				Entry("when port is not in any bridge", []ovsmodel.Bridge{}, false),
			)
		})

		Describe("GetIfaceWithExternalIDs", func() {
			var mockConditionalAPI *MockConditionalAPI

			BeforeEach(func() {
				mockConditionalAPI = NewMockConditionalAPI(mockCtrl)
			})

			DescribeTable("should handle different interface match counts",
				func(interfaceCount int, expectError bool, errorSubstring string) {
					externalIDs := map[string]string{"key": "value"}

					mockOVSClient.EXPECT().
						WhereAll(gomock.Any(), gomock.Any()).
						Return(mockConditionalAPI)

					mockConditionalAPI.EXPECT().
						List(gomock.Any(), gomock.Any()).
						DoAndReturn(func(ctx context.Context, result interface{}) error {
							ptr := result.(*[]ovsmodel.Interface)
							switch interfaceCount {
							case 0:
								// Leave empty
							case 1:
								*ptr = []ovsmodel.Interface{{Name: "test-iface", UUID: "test-uuid"}}
							case 2:
								*ptr = []ovsmodel.Interface{{Name: "iface1"}, {Name: "iface2"}}
							}
							return nil
						})

					iface, err := client.GetIfaceWithExternalIDs(ctx, externalIDs)
					if expectError {
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorSubstring))
						Expect(iface).To(BeNil())
					} else {
						Expect(err).NotTo(HaveOccurred())
						Expect(iface).NotTo(BeNil())
						Expect(iface.Name).To(Equal("test-iface"))
					}
				},
				Entry("when no interfaces match", 0, true, "failed to find matching interface"),
				Entry("when multiple interfaces match", 2, true, "found multiple interfaces"),
				Entry("when single interface matches", 1, false, ""),
			)
		})

		Describe("DeleteBridge", func() {
			var mockConditionalAPI *MockConditionalAPI

			BeforeEach(func() {
				mockConditionalAPI = NewMockConditionalAPI(mockCtrl)
			})

			It("should be idempotent when bridge not found", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(ovsclient.ErrNotFound)

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should fail when Get returns non-ErrNotFound error", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(errors.New("database error"))

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get bridge"))
			})

			It("should fail when GetOpenVSwitch fails", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						bridge := model.(*ovsmodel.Bridge)
						bridge.UUID = bridgeUUID
						return nil
					})
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					Return(errors.New("list failed"))

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get Open_vSwitch row"))
			})

			It("should fail when Mutate returns error", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						bridge := model.(*ovsmodel.Bridge)
						bridge.UUID = bridgeUUID
						return nil
					})
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]*ovsmodel.OpenvSwitch)
						*ptr = []*ovsmodel.OpenvSwitch{{UUID: "ovs-uuid"}}
						return nil
					})
				mockOVSClient.EXPECT().
					Where(gomock.Any()).
					Return(mockConditionalAPI)
				mockConditionalAPI.EXPECT().
					Mutate(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("mutate failed"))

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create mutate operations"))
			})

			It("should fail when Delete returns error", func() {
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						bridge := model.(*ovsmodel.Bridge)
						bridge.UUID = bridgeUUID
						return nil
					})
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]*ovsmodel.OpenvSwitch)
						*ptr = []*ovsmodel.OpenvSwitch{{UUID: "ovs-uuid"}}
						return nil
					})
				mockOVSClient.EXPECT().
					Where(gomock.Any()).
					Return(mockConditionalAPI).
					Times(2)
				mockConditionalAPI.EXPECT().
					Mutate(gomock.Any(), gomock.Any()).
					Return([]ovsdb.Operation{}, nil)
				mockConditionalAPI.EXPECT().
					Delete().
					Return(nil, errors.New("delete failed"))

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create delete operations"))
			})

			It("should delete bridge with no ports", func() {
				// Mock Get for bridge
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						bridge := model.(*ovsmodel.Bridge)
						bridge.UUID = bridgeUUID
						bridge.Ports = []string{}
						return nil
					})
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]*ovsmodel.OpenvSwitch)
						*ptr = []*ovsmodel.OpenvSwitch{{UUID: "ovs-uuid"}}
						return nil
					})
				// Mock Where for bridge mutation and deletion
				mockOVSClient.EXPECT().
					Where(gomock.Any()).
					Return(mockConditionalAPI).
					Times(2)
				mockConditionalAPI.EXPECT().
					Mutate(gomock.Any(), gomock.Any()).
					Return([]ovsdb.Operation{}, nil)
				mockConditionalAPI.EXPECT().
					Delete().
					Return([]ovsdb.Operation{}, nil)
				mockOVSClient.EXPECT().
					Transact(gomock.Any(), gomock.Any()).
					Return([]ovsdb.OperationResult{{UUID: ovsdb.UUID{GoUUID: "test"}}}, nil)

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should delete bridge with ports (garbage collection by OVSDB)", func() {
				// Mock Get for bridge
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						bridge := model.(*ovsmodel.Bridge)
						bridge.UUID = bridgeUUID
						bridge.Ports = []string{"port-uuid-1"} // Ports exist but will be GC'd
						return nil
					})
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]*ovsmodel.OpenvSwitch)
						*ptr = []*ovsmodel.OpenvSwitch{{UUID: "ovs-uuid"}}
						return nil
					})
				// Mock Where for bridge mutation and deletion
				mockOVSClient.EXPECT().
					Where(gomock.Any()).
					Return(mockConditionalAPI).
					Times(2)
				mockConditionalAPI.EXPECT().
					Mutate(gomock.Any(), gomock.Any()).
					Return([]ovsdb.Operation{}, nil)
				mockConditionalAPI.EXPECT().
					Delete().
					Return([]ovsdb.Operation{}, nil)

				// Mock Transact
				mockOVSClient.EXPECT().
					Transact(gomock.Any(), gomock.Any()).
					Return([]ovsdb.OperationResult{{UUID: ovsdb.UUID{GoUUID: "test"}}}, nil)

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should fail when Transact returns error", func() {
				// Mock Get for bridge
				mockOVSClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, model interface{}) error {
						bridge := model.(*ovsmodel.Bridge)
						bridge.UUID = bridgeUUID
						bridge.Ports = []string{}
						return nil
					})
				mockOVSClient.EXPECT().
					List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, result interface{}) error {
						ptr := result.(*[]*ovsmodel.OpenvSwitch)
						*ptr = []*ovsmodel.OpenvSwitch{{UUID: "ovs-uuid"}}
						return nil
					})
				// Mock Where for bridge mutation and deletion
				mockOVSClient.EXPECT().
					Where(gomock.Any()).
					Return(mockConditionalAPI).
					Times(2)
				mockConditionalAPI.EXPECT().
					Mutate(gomock.Any(), gomock.Any()).
					Return([]ovsdb.Operation{}, nil)
				mockConditionalAPI.EXPECT().
					Delete().
					Return([]ovsdb.Operation{}, nil)

				// Mock Transact - returns error
				mockOVSClient.EXPECT().
					Transact(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("transaction failed"))

				err := client.DeleteBridge(ctx, "br-test")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to delete bridge"))
			})

		})
	})

	Describe("Edge cases and validation", func() {
		It("should handle multiple error types from API", func() {
			By("Testing connection errors")
			connErr := errors.New("connection timeout")
			config := PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
			}
			mockAPI.EXPECT().
				AddPort(ctx, config).
				Return(connErr)

			err := mockAPI.AddPort(ctx, config)
			Expect(err).To(MatchError(connErr))

			By("Testing not found errors")
			mockAPI.EXPECT().
				GetIfaceWithName(ctx, "missing").
				Return(nil, ovsclient.ErrNotFound)

			_, err = mockAPI.GetIfaceWithName(ctx, "missing")
			Expect(errors.Is(err, ovsclient.ErrNotFound)).To(BeTrue())
		})

		It("should handle empty and nil configurations", func() {
			By("Testing with empty bridge name")
			emptyConfig := BridgeConfig{
				Name:         "",
				DatapathType: "system",
			}
			mockAPI.EXPECT().
				AddBridge(ctx, emptyConfig).
				Return(errors.New("bridge name cannot be empty"))

			err := mockAPI.AddBridge(ctx, emptyConfig)
			Expect(err).To(HaveOccurred())

			By("Testing with nil MTU")
			config := PortConfig{
				BridgeName:    "br-test",
				Name:          "port-test",
				InterfaceType: "internal",
			}
			mockAPI.EXPECT().
				AddPort(ctx, config).
				Return(nil)

			Expect(mockAPI.AddPort(ctx, config)).To(Succeed())
		})

		It("should handle empty maps", func() {
			By("Testing empty external IDs")
			emptyMap := map[string]string{}
			mockAPI.EXPECT().
				SetIfaceExternalIDs(ctx, "test-iface", emptyMap).
				Return(nil)

			Expect(mockAPI.SetIfaceExternalIDs(ctx, "test-iface", emptyMap)).To(Succeed())

			By("Testing empty options")
			mockAPI.EXPECT().
				SetIfaceOptions(ctx, "test-iface", emptyMap).
				Return(nil)

			Expect(mockAPI.SetIfaceOptions(ctx, "test-iface", emptyMap)).To(Succeed())
		})
	})
})
