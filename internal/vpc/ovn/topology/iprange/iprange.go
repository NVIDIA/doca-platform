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

package iprange

import (
	"fmt"
	"net"
	"strings"

	iputils "gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ip"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
)

// IPRange represents a range of IP addresses
// An IPRange can be a single IP address, a range of IP addresses consisting of all IPs between Start and End, or both.
type IPRange struct {
	// IP is a single IP in the range
	IP net.IP
	// Start is the first IP in the range
	Start net.IP
	// End is the last IP in the range
	End net.IP
}

// String generates a string representation of the IPRange in a format that
// can be provided to ovn.
func (r *IPRange) String() string {
	var out []string
	if r.IP != nil {
		out = append(out, r.IP.String())
	}

	if r.Start != nil && r.End != nil {
		if r.Start.String() == r.End.String() {
			out = append(out, r.Start.String())
		} else {
			out = append(out, fmt.Sprintf("%s..%s", r.Start.String(), r.End.String()))
		}
	}
	return strings.Join(out, " ")
}

// IPRangesString generates a string representation of a list of IPRange
func IPRangesString(ranges []IPRange) string {
	out := make([]string, 0, len(ranges))

	for _, ipr := range ranges {
		if ipr.String() == "" {
			continue
		}
		out = append(out, ipr.String())
	}

	return strings.Join(out, " ")
}

// IPRangeFromExcludeIPsSpec creates a list of ipRange from a list of ExcludeIPsEntry.
// each ip or range in the ExcludeIPsEntry is checked to be part of the subnet.
// returns error if occurred.
func IPRangeFromExcludeIPsSpec(excludeIPs []vpcv1.ExcludeIPsEntry, subnet *net.IPNet) ([]IPRange, error) {
	if len(excludeIPs) == 0 {
		return nil, nil
	}

	if subnet == nil {
		return nil, fmt.Errorf("subnet is nil")
	}

	ipranges := make([]IPRange, 0, len(excludeIPs))

	for _, excludeIP := range excludeIPs {
		ipr := IPRange{}

		if excludeIP.IP != nil {
			ip := net.ParseIP(*excludeIP.IP)
			if ip == nil {
				return nil, fmt.Errorf("failed to parse IP address: %s", *excludeIP.IP)
			}

			if err := validateIP(ip, subnet); err != nil {
				return nil, fmt.Errorf("failed to validate IP address. %w", err)
			}

			ipr.IP = ip
		}

		if excludeIP.Range != nil {
			start := net.ParseIP(excludeIP.Range.Start)
			end := net.ParseIP(excludeIP.Range.End)
			if start == nil || end == nil {
				return nil, fmt.Errorf("failed to parse IP adresses of given range: %s-%s", excludeIP.Range.Start, excludeIP.Range.End)
			}

			if err := validateIP(start, subnet); err != nil {
				return nil, fmt.Errorf("failed to validate start IP address. %w", err)
			}

			if err := validateIP(end, subnet); err != nil {
				return nil, fmt.Errorf("failed to validate end IP address. %w", err)
			}

			cmpRes := iputils.Cmp(start, end)
			if cmpRes == -2 {
				return nil, fmt.Errorf("start IP address %s and end IP address %s are incomparable", start, end)
			}
			if cmpRes > 0 {
				return nil, fmt.Errorf("start IP address %s is greater than end IP address %s", start, end)
			}

			ipr.Start = start
			ipr.End = end
		}

		ipranges = append(ipranges, ipr)
	}

	return ipranges, nil
}

// IPRangeFromIP creates an ipRange from provided IP address.
func IPRangeFromIP(ip net.IP) IPRange {
	return IPRange{
		IP: ip,
	}
}

// validateIP performs validation on the provided IP address.
func validateIP(ip net.IP, subnet *net.IPNet) error {
	if ip.To4() == nil {
		return fmt.Errorf("invalid IP address: %s. not ipv4", ip)
	}

	if !subnet.Contains(ip) {
		return fmt.Errorf("IP address %s is not part of the subnet %s", ip, subnet)
	}

	return nil
}
