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

package common

import (
	"fmt"
	"net"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IPtoMAC converts IP address to MAC address
// this is needed since we derrive MAC address from IP address for OVN logical topology
func IPtoMAC(ip net.IP) net.HardwareAddr {
	if ip == nil {
		return nil
	}

	ip4 := ip.To4()
	if ip4 != nil {
		mac := []byte{0xfe, 0x0, ip4[0], ip4[1], ip4[2], ip4[3]}
		return net.HardwareAddr(mac)
	}

	// IPv6
	ip6 := ip.To16()
	if ip6 != nil {
		mac := []byte{0xfe, 0x0, ip6[12], ip6[13], ip6[14], ip6[15]}
		return net.HardwareAddr(mac)
	}
	return nil
}

// ObjectToLabelValue returns a string that represents the object namespaced name in the format namespace_name
// so that it can be set as label value.
func ObjectToLabelValue(o client.Object) string {
	return fmt.Sprintf("%s_%s", o.GetNamespace(), o.GetName())
}

// ObjectKeyFromLabelValue returns the object key from the string that represents the object namespaced name in the format <ns>_<name>.
// returns error if the given string is not in the expected format.
func ObjectKeyFromLabelValue(labelValue string) (client.ObjectKey, error) {
	parts := strings.Split(labelValue, "_")
	if len(parts) != 2 {
		return client.ObjectKey{}, fmt.Errorf("invalid label value %s", labelValue)
	}
	return client.ObjectKey{Namespace: parts[0], Name: parts[1]}, nil
}
