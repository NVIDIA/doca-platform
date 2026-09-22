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

package vpc

import (
	"fmt"
	"net"
	"regexp"

	"github.com/nvidia/doca-platform/test/utils/remotehost"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// WorkerOverlayIPs are expected overlay addresses and subnet for a host.
type WorkerOverlayIPs struct {
	// Worker is the remote host.
	Worker remotehost.Host
	// IPs are expected CIDRs, one per VRF link (e.g. 10.0.0.1/31).
	IPs []string
	// Subnet is the overlay subnet used for route flush.
	Subnet string
	// Family is IPv4 or IPv6, zero value is IPv4.
	Family remotehost.IPFamily
}

// cidrToken matches cidr as a whole whitespace-delimited token in ip command output.
func cidrToken(cidr string) string {
	return `(?m)(^|\s)` + regexp.QuoteMeta(cidr) + `(\s|$)`
}

// VerifyAttachmentsDHCPAddresses asserts each host got the expected CIDR per VRF link.
func VerifyAttachmentsDHCPAddresses(links []remotehost.VRFLink, overlays []WorkerOverlayIPs) {
	for _, overlay := range overlays {
		Expect(overlay.IPs).To(HaveLen(len(links)),
			"overlay IPs for %s must match links length %d", overlay.Worker.Addr, len(links))
		ipVer := overlay.Family.IPVersion()
		for i, link := range links {
			expectedCIDR := overlay.IPs[i]
			By(fmt.Sprintf("Verifying DHCP %s on %s VRF %s dev %s", expectedCIDR, overlay.Worker.Addr, link.VRF, link.Iface))
			Eventually(func(g Gomega) {
				out, err := overlay.Worker.SSH(fmt.Sprintf("ip -%s -o addr show dev %s", ipVer, link.Iface))
				g.Expect(err).ToNot(HaveOccurred(), "ip addr show failed on %s: %s", overlay.Worker.Addr, out)
				g.Expect(out).To(MatchRegexp(cidrToken(expectedCIDR)),
					"expected DHCP address %s on %s VRF %s dev %s, got: %s",
					expectedCIDR, overlay.Worker.Addr, link.VRF, link.Iface, out)
			}, LongTimeout).Should(Succeed())
		}
	}
}

// CleanupHosts flushes PF overlays using each host's Subnet and removes containers.
func CleanupHosts(overlays []WorkerOverlayIPs, pfIfaces []string) {
	for _, overlay := range overlays {
		for _, iface := range pfIfaces {
			overlay.Worker.FlushIface(iface, overlay.Subnet, overlay.Family)
		}
		overlay.Worker.Remove()
	}
}

// UnderlayPrefixBits is overlay, software-plane, and rail prefix lengths from underlayConfigMapData.
type UnderlayPrefixBits struct {
	// Overlay is overlayNetworkPrefixLength.
	Overlay int
	// SoftwarePlane is softwarePlaneIDBitLength.
	SoftwarePlane int
	// Rail is railIDBitLength.
	Rail int
}

// softwarePlanePrefix is overlayNetworkPrefixLength + softwarePlaneIDBitLength.
func (b UnderlayPrefixBits) softwarePlanePrefix() int {
	return b.Overlay + b.SoftwarePlane
}

// railPrefix is overlayNetworkPrefixLength + softwarePlaneIDBitLength + railIDBitLength.
func (b UnderlayPrefixBits) railPrefix() int {
	return b.softwarePlanePrefix() + b.Rail
}

// prefixCIDR masks cidr's address to bits for family and returns network/bits.
func prefixCIDR(cidr string, bits int, family remotehost.IPFamily) string {
	ip, _, err := net.ParseCIDR(cidr)
	Expect(err).NotTo(HaveOccurred(), "parsing overlay CIDR %s", cidr)
	width := family.Bits()
	if family == remotehost.IPFamilyV6 {
		Expect(ip.To4()).To(BeNil(), "overlay CIDR %s is IPv4, family is IPv6", cidr)
		ip = ip.To16()
	} else {
		ip = ip.To4()
		Expect(ip).NotTo(BeNil(), "overlay CIDR %s is not IPv4", cidr)
	}
	Expect(bits).To(BeNumerically(">=", 0), "prefix length %d must be >= 0", bits)
	Expect(bits).To(BeNumerically("<=", width),
		"prefix length %d exceeds %s width %d", bits, family, width)
	return fmt.Sprintf("%s/%d", ip.Mask(net.CIDRMask(bits, width)), bits)
}

// VerifyAttachmentsDHCPRoutes asserts DHCP software-plane and rail prefixes in each VRF table.
func VerifyAttachmentsDHCPRoutes(links []remotehost.VRFLink, overlays []WorkerOverlayIPs, bits UnderlayPrefixBits) {
	Expect(links).NotTo(BeEmpty(), "DHCP overlay route check requires VRF links")
	swBits := bits.softwarePlanePrefix()
	railBits := bits.railPrefix()

	for _, overlay := range overlays {
		Expect(overlay.IPs).To(HaveLen(len(links)),
			"overlay IPs for %s must match links length %d", overlay.Worker.Addr, len(links))
		ipVer := overlay.Family.IPVersion()
		for i, link := range links {
			swCIDR := prefixCIDR(overlay.IPs[i], swBits, overlay.Family)
			railCIDR := prefixCIDR(overlay.IPs[i], railBits, overlay.Family)
			By(fmt.Sprintf("Verifying DHCP overlay routes %s and %s in VRF table %d on %s",
				swCIDR, railCIDR, link.Table, overlay.Worker.Addr))
			Eventually(func(g Gomega) {
				out, err := overlay.Worker.SSH(fmt.Sprintf("ip -%s route show table %d", ipVer, link.Table))
				g.Expect(err).ToNot(HaveOccurred(), "ip route show failed on %s: %s", overlay.Worker.Addr, out)
				g.Expect(out).To(MatchRegexp(cidrToken(swCIDR)),
					"expected DHCP overlay route %s in VRF table %d on %s, got: %s",
					swCIDR, link.Table, overlay.Worker.Addr, out)
				g.Expect(out).To(MatchRegexp(cidrToken(railCIDR)),
					"expected DHCP overlay route %s in VRF table %d on %s, got: %s",
					railCIDR, link.Table, overlay.Worker.Addr, out)
			}, LongTimeout).Should(Succeed())
		}
	}
}
