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

package remotehost

import (
	"fmt"
	"strings"
)

// IPFamily is IPv4 or IPv6 for DHCP and traffic helpers, zero value is IPv4, no dual-stack until a caller needs it.
type IPFamily int

const (
	// IPFamilyV4 is IPv4.
	IPFamilyV4 IPFamily = iota
	// IPFamilyV6 is IPv6.
	IPFamilyV6
)

// String returns ipv4 or ipv6.
func (f IPFamily) String() string {
	if f == IPFamilyV6 {
		return "ipv6"
	}
	return "ipv4"
}

// IPVersion returns 4 or 6 for ip -4/-6.
func (f IPFamily) IPVersion() string {
	if f == IPFamilyV6 {
		return "6"
	}
	return "4"
}

// Bits returns 32 for IPv4 or 128 for IPv6.
func (f IPFamily) Bits() int {
	if f == IPFamilyV6 {
		return 128
	}
	return 32
}

// wantsV4 reports whether IPv4 DHCP or traffic is enabled.
func (f IPFamily) wantsV4() bool {
	return f != IPFamilyV6
}

// wantsV6 reports whether IPv6 DHCP or traffic is enabled.
func (f IPFamily) wantsV6() bool {
	return f == IPFamilyV6
}

// pingArgs is ping, or ping -6 when Family is IPv6.
func (f IPFamily) pingArgs(count, wait, dest string) []string {
	args := []string{"ping"}
	if f == IPFamilyV6 {
		args = append(args, "-6")
	}
	return append(args, "-c", count, "-W", wait, dest)
}

// iperfIPFlag is iperf3 -6 for IPv6, -4 for IPv4.
func (f IPFamily) iperfIPFlag() string {
	if f == IPFamilyV6 {
		return "-6"
	}
	return "-4"
}

// ssListenCmd checks that TCP port is LISTEN for this family.
func (f IPFamily) ssListenCmd(port int) string {
	if f == IPFamilyV6 {
		return fmt.Sprintf("ss -lnt6 'sport = :%d' | grep -q LISTEN", port)
	}
	return fmt.Sprintf("ss -lnt4 'sport = :%d' | grep -q LISTEN", port)
}

// dhcpSections returns [DHCPv4] and/or [DHCPv6] unit text.
func (f IPFamily) dhcpSections() string {
	s := ""
	if f.wantsV4() {
		s += "\n[DHCPv4]\nUseRoutes=yes\nUseGateway=yes\n"
	}
	if f.wantsV6() {
		s += "\n[DHCPv6]\nUseAddress=yes\n"
	}
	return s
}

// flushScript flushes addresses and the subnet route for this family.
func (f IPFamily) flushScript(iface, subnet string) string {
	var parts []string
	if f.wantsV4() {
		parts = append(parts, fmt.Sprintf("$SUDO ip -4 addr flush dev %s 2>/dev/null", shellQuote(iface)))
		if subnet != "" {
			parts = append(parts, fmt.Sprintf("$SUDO ip -4 route del %s dev %s 2>/dev/null", shellQuote(subnet), shellQuote(iface)))
		}
	}
	if f.wantsV6() {
		parts = append(parts, fmt.Sprintf("$SUDO ip -6 addr flush dev %s 2>/dev/null", shellQuote(iface)))
		if subnet != "" {
			parts = append(parts, fmt.Sprintf("$SUDO ip -6 route del %s dev %s 2>/dev/null", shellQuote(subnet), shellQuote(iface)))
		}
	}
	parts = append(parts, "true")
	return strings.Join(parts, "; ")
}
