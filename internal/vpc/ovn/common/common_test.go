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

package common_test

import (
	"encoding/json"
	"errors"
	"net"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Common", func() {
	Context("LRPAddressFromAnnotation", func() {
		It("should return nil if the annotation is nil", func() {
			lrpa, err := common.LRPAddressFromAnnotation(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(lrpa).To(BeNil())
		})

		It("should return nil if the annotation is not present", func() {
			annotations := map[string]string{}
			lrpa, err := common.LRPAddressFromAnnotation(annotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(lrpa).To(BeNil())
		})

		It("should return an error if the annotation is not valid", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: "invalid",
			}
			lrpa, err := common.LRPAddressFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
			Expect(lrpa).To(BeNil())
		})

		It("should return the LRPAddress if the annotation is valid", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: `{"ipv4":"192.168.1.1/24","ipv6":"2001:db8::1/64"}`,
			}
			lrpa, err := common.LRPAddressFromAnnotation(annotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(lrpa).To(Equal(&common.LRPAddress{
				IPV4: "192.168.1.1/24",
				IPV6: "2001:db8::1/64",
			}))
		})
	})

	Context("LRPAddressToAnnotation", func() {
		It("should return an error if the annotation is nil", func() {
			err := common.LRPAddressToAnnotation(common.LRPAddress{}, nil)
			Expect(err).To(HaveOccurred())
		})

		It("should set expected annotation", func() {
			annotations := map[string]string{}
			err := common.LRPAddressToAnnotation(common.LRPAddress{
				IPV4: "192.168.1.1/24",
				IPV6: "2001:db8::1/64",
			}, annotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				common.LRPAddressesAnnotationKey: `{"ipv4":"192.168.1.1/24","ipv6":"2001:db8::1/64"}`,
			}))
		})
	})

	Context("HasLRPAddressAnnotation", func() {
		It("should return false if the annotation is nil", func() {
			Expect(common.HasLRPAddressAnnotation(nil)).To(BeFalse())
		})

		It("should return false if the annotation is not present", func() {
			annotations := map[string]string{}
			Expect(common.HasLRPAddressAnnotation(annotations)).To(BeFalse())
		})

		It("should return true if the annotation is present", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: "dont-care",
			}
			Expect(common.HasLRPAddressAnnotation(annotations)).To(BeTrue())
		})
	})

	Context("NetworksFromLRPAddressAnnotation", func() {
		It("should return an error if the annotation is nil", func() {
			_, _, err := common.NetworksFromLRPAddressAnnotation(nil)
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the annotation is not present", func() {
			annotations := map[string]string{}
			_, _, err := common.NetworksFromLRPAddressAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the annotation is invalid", func() {
			annotations := map[string]string{}
			annotations[common.LRPAddressesAnnotationKey] = "invalid"
			_, _, err := common.NetworksFromLRPAddressAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the IPv4 address is not present", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: `{"ipv6":"2001:db8::1"}`,
			}
			_, _, err := common.NetworksFromLRPAddressAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return error if the IPv4 address is not valid", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: `{"ipv4":"invalid"}`,
			}
			_, _, err := common.NetworksFromLRPAddressAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return the IPv4 address and network", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: `{"ipv4":"192.168.1.1/24"}`,
			}
			ipn4, ipn6, err := common.NetworksFromLRPAddressAnnotation(annotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(ipn4).NotTo(BeNil())
			Expect(ipn6).To(BeNil())
			expectedIPv4, expIPv4Net, _ := net.ParseCIDR("192.168.1.1/24")
			expIPv4Net.IP = expectedIPv4
			Expect(ipn4).To(Equal(expIPv4Net))
			Expect(ipn6).To(BeNil())
		})

		It("Should return the IPv4 address and network and IPv6 address and network", func() {
			annotations := map[string]string{
				common.LRPAddressesAnnotationKey: `{"ipv4":"192.168.1.1/24","ipv6":"2001:db8::1/64"}`,
			}
			ipn4, ipn6, err := common.NetworksFromLRPAddressAnnotation(annotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(ipn4).NotTo(BeNil())
			Expect(ipn6).NotTo(BeNil())
			expectedIPv4, expIPv4Net, _ := net.ParseCIDR("192.168.1.1/24")
			expIPv4Net.IP = expectedIPv4
			expectedIPv6, expIPv6Net, _ := net.ParseCIDR("2001:db8::1/64")
			expIPv6Net.IP = expectedIPv6
			Expect(ipn4).To(Equal(expIPv4Net))
			Expect(ipn6).To(Equal(expIPv6Net))
		})
	})

	Context("LSPMACAddressFromAnnotation", func() {
		It("should return error if the annotation is nil", func() {
			_, err := common.LSPMACAddressFromAnnotation(nil)
			Expect(err).To(HaveOccurred())
		})

		It("should return error if the annotation is not present", func() {
			annotations := map[string]string{}
			_, err := common.LSPMACAddressFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return nil if the annotation is present with value unknown", func() {
			annotations := map[string]string{}
			annotations[common.LSPMACAddressAnnotationKey] = "unknown"
			mac, err := common.LSPMACAddressFromAnnotation(annotations)
			Expect(err).ToNot(HaveOccurred())
			Expect(mac).To(BeNil())
		})

		It("should return error if the annotation is present but empty", func() {
			annotations := map[string]string{}
			annotations[common.LSPMACAddressAnnotationKey] = ""
			_, err := common.LSPMACAddressFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the annotation is not valid", func() {
			annotations := map[string]string{
				common.LSPMACAddressAnnotationKey: "invalid",
			}
			_, err := common.LSPMACAddressFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
		})

		It("should return the MAC address", func() {
			annotations := map[string]string{
				common.LSPMACAddressAnnotationKey: "00:fe:00:00:de:ad",
			}
			mac, err := common.LSPMACAddressFromAnnotation(annotations)
			Expect(err).NotTo(HaveOccurred())
			expMAC, _ := net.ParseMAC("00:fe:00:00:de:ad")
			Expect(mac).To(Equal(&expMAC))
		})
	})

	Context("IPtoMAC", func() {
		It("should return nil if the IP is nil", func() {
			Expect(common.IPtoMAC(nil)).To(BeNil())
		})

		It("should return the MAC address for a valid IPv4 address", func() {
			ip := net.ParseIP("192.168.1.1")
			mac := common.IPtoMAC(ip)
			expMac, _ := net.ParseMAC("fe:00:c0:a8:01:01")
			Expect(mac).To(Equal(expMac))
		})

		It("should return the MAC address for a valid IPv6 address", func() {
			ip := net.ParseIP("2001:db8::0010:0001")
			mac := common.IPtoMAC(ip)
			expMac, _ := net.ParseMAC("fe:00:00:10:00:01")
			Expect(mac).To(Equal(expMac))
		})
	})

	Context("GatewayConfigFromAnnotation", func() {
		It("should return nil if the annotation is nil", func() {
			_, err := common.GatewayConfigFromAnnotation(nil)
			Expect(err).To(HaveOccurred())
		})

		It("should return error if the annotation is not present", func() {
			annotations := map[string]string{}
			_, err := common.GatewayConfigFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, common.ErrAnnotationNotFound)).To(BeTrue())
		})

		It("should return error if the annotation is present but empty", func() {
			annotations := map[string]string{}
			annotations[common.OVNGatewayConfigAnnotationKey] = ""
			_, err := common.GatewayConfigFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, common.ErrAnnotationNotFound)).To(BeTrue())
		})

		It("should return the gateway configuration", func() {
			expGatewayConfig := common.GatewayConfig{
				IP: common.IPNetConfiguration{
					IPv4: "192.168.1.2/24",
					IPv6: "2001:db8::2/64",
				},
				MAC: "00:fe:00:00:de:ad",
				NextHop: common.IPConfiguration{
					IPv4: "192.168.1.1",
					IPv6: "2001:db8::1",
				},
			}
			data, err := json.Marshal(expGatewayConfig)
			Expect(err).ToNot(HaveOccurred())

			annotations := map[string]string{
				common.OVNGatewayConfigAnnotationKey: string(data),
			}
			gatewayConfig, err := common.GatewayConfigFromAnnotation(annotations)
			Expect(err).NotTo(HaveOccurred())
			Expect(gatewayConfig).To(Equal(&expGatewayConfig))
		})

		It("should return error if the annotation contains malformed JSON", func() {
			annotations := map[string]string{
				common.OVNGatewayConfigAnnotationKey: `{gateway-ip:foo}`,
			}
			_, err := common.GatewayConfigFromAnnotation(annotations)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal gateway config"))
		})
	})

	Context("ObjectToLabelValue", func() {
		It("should create correct label value format", func() {
			obj := &vpcv1.DPUVPC{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vpc",
					Namespace: "default",
				},
			}
			result := common.ObjectToLabelValue(obj)
			Expect(result).To(Equal("default_test-vpc"))
		})
	})

	Context("ObjectKeyFromLabelValue", func() {
		It("should create correct object key from label value", func() {
			labelValue := "default_test-vpc"
			result, err := common.ObjectKeyFromLabelValue(labelValue)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Namespace).To(Equal("default"))
			Expect(result.Name).To(Equal("test-vpc"))
		})

		It("should return error for invalid label value format", func() {
			labelValue := "invalid_label_value"
			_, err := common.ObjectKeyFromLabelValue(labelValue)
			Expect(err).To(HaveOccurred())
		})

		It("should return error for empty label value", func() {
			labelValue := ""
			_, err := common.ObjectKeyFromLabelValue(labelValue)
			Expect(err).To(HaveOccurred())
		})

		It("ensure idempotence with ObjectToLabelValue", func() {
			obj := &vpcv1.DPUVPC{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vpc",
					Namespace: "default",
				},
			}
			result := common.ObjectToLabelValue(obj)
			key, err := common.ObjectKeyFromLabelValue(result)
			Expect(err).ToNot(HaveOccurred())
			Expect(key.Namespace).To(Equal(obj.Namespace))
			Expect(key.Name).To(Equal(obj.Name))
		})
	})

	Context("IPConfigurationFromAnnotation", func() {
		It("should return nil if the annotation is nil", func() {
			_, err := common.IPNetConfigurationFromAnnotation(nil, common.OVNVtepIPAnnotationKey)
			Expect(err).To(HaveOccurred())
		})

		It("should return error if the annotation is not present", func() {
			annotations := map[string]string{}
			_, err := common.IPNetConfigurationFromAnnotation(annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, common.ErrAnnotationNotFound)).To(BeTrue())
		})

		It("should return error if the annotation is present but empty", func() {
			annotations := map[string]string{}
			annotations[common.OVNVtepIPAnnotationKey] = ""
			_, err := common.IPNetConfigurationFromAnnotation(annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, common.ErrAnnotationNotFound)).To(BeTrue())
		})

		It("should return error if the annotation contains malformed JSON", func() {
			annotations := map[string]string{
				common.OVNVtepIPAnnotationKey: `{ipv4:foo}`,
			}
			_, err := common.IPNetConfigurationFromAnnotation(annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal IP configuration"))
		})

		It("should return the IP configuration", func() {
			annotations := map[string]string{
				common.OVNVtepIPAnnotationKey: `{"ipv4":"192.168.1.1/24","ipv6":"2001:db8::1/64"}`,
			}
			ipConfig, err := common.IPNetConfigurationFromAnnotation(annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).NotTo(HaveOccurred())
			expIPConfig := common.IPNetConfiguration{
				IPv4: "192.168.1.1/24",
				IPv6: "2001:db8::1/64",
			}
			Expect(ipConfig).To(Equal(&expIPConfig))
		})
	})

	Context("IPConfigurationToAnnotation", func() {
		It("should return an error if the annotation is nil", func() {
			err := common.IPNetConfigurationToAnnotation(common.IPNetConfiguration{}, nil, common.OVNVtepIPAnnotationKey)
			Expect(err).To(HaveOccurred())
		})

		It("should include both IPv4 and IPv6 addresses", func() {
			annotations := map[string]string{}
			expectedJSON := `{"ipv4":"192.168.1.1/24","ipv6":"2001:db8::1/64"}`
			ipConfig := common.IPNetConfiguration{
				IPv4: "192.168.1.1/24",
				IPv6: "2001:db8::1/64",
			}
			err := common.IPNetConfigurationToAnnotation(ipConfig, annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				common.OVNVtepIPAnnotationKey: expectedJSON,
			}))
		})

		It("should include only IPv4 address", func() {
			annotations := map[string]string{}
			expectedJSON := `{"ipv4":"192.168.1.1/24"}`
			ipConfig := common.IPNetConfiguration{
				IPv4: "192.168.1.1/24",
			}
			err := common.IPNetConfigurationToAnnotation(ipConfig, annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				common.OVNVtepIPAnnotationKey: expectedJSON,
			}))
		})

		It("should include only IPv6 address", func() {
			annotations := map[string]string{}
			expectedJSON := `{"ipv6":"2001:db8::1/64"}`
			ipConfig := common.IPNetConfiguration{
				IPv6: "2001:db8::1/64",
			}
			err := common.IPNetConfigurationToAnnotation(ipConfig, annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				common.OVNVtepIPAnnotationKey: expectedJSON,
			}))
		})

		It("should be empty if the IP configuration is empty", func() {
			annotations := map[string]string{}
			expectedJSON := `{}`
			ipConfig := common.IPNetConfiguration{}
			err := common.IPNetConfigurationToAnnotation(ipConfig, annotations, common.OVNVtepIPAnnotationKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				common.OVNVtepIPAnnotationKey: expectedJSON,
			}))
		})
	})

})
