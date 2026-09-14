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
	"encoding/base64"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	networkdUnitPrefix = "05-e2e"
)

// VRFLink is a host iface enslaved to a VRF.
type VRFLink struct {
	// Iface is the host netdev.
	Iface string
	// VRF is the VRF device to enslave Iface to.
	VRF string
	// Table is the VRF routing table ID.
	Table int
	// Family is IPv4 or IPv6, zero value is IPv4.
	Family IPFamily
}

// ConfigureVRFDHCP writes transient networkd drop-ins and runs DHCP for each link Family.
func ConfigureVRFDHCP(mtu int, links []VRFLink, hosts []Host) {
	if len(links) == 0 {
		return
	}

	ifaces := make([]string, 0, len(links))
	writeSteps := make([]string, 0, len(links)*3)
	for _, link := range links {
		writeSteps = append(writeSteps, vrfUnitWrites(link, mtu)...)
		ifaces = append(ifaces, link.Iface)
	}

	for _, h := range hosts {
		steps := append([]string{"set -e", hostSudoPreamble, "$SUDO systemctl start systemd-networkd"}, writeSteps...)
		steps = append(steps,
			"$SUDO networkctl reload",
			fmt.Sprintf("$SUDO networkctl reconfigure %s", strings.Join(ifaces, " ")),
		)
		script := strings.Join(steps, "\n")

		By(fmt.Sprintf("Applying systemd-networkd DHCP config on %s for %s", h.Addr, strings.Join(ifaces, ", ")))
		Eventually(func(g Gomega) {
			out, err := h.SSH(script)
			g.Expect(err).ToNot(HaveOccurred(),
				"failed to apply networkd config on %s ifaces %s: %s", h.Addr, strings.Join(ifaces, ", "), out)
		}, LongTimeout).Should(Succeed())
	}
}

// RemoveVRFNetworkd removes transient networkd drop-ins (best-effort).
func RemoveVRFNetworkd(links []VRFLink, hosts []Host) {
	for _, h := range hosts {
		removeVRFNetworkd(h, links)
	}
}

// networkdUnitPaths returns transient netdev and network unit paths for a VRF link.
func networkdUnitPaths(iface, vrf string) (netdevPath, vrfNetworkPath, ifaceNetworkPath string) {
	base := "/run/systemd/network/" + networkdUnitPrefix
	return fmt.Sprintf("%s-%s.netdev", base, vrf),
		fmt.Sprintf("%s-%s.network", base, vrf),
		fmt.Sprintf("%s-%s.network", base, iface)
}

// writeHostFileScript returns a sudo tee of content to path via base64.
func writeHostFileScript(path, content string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	return fmt.Sprintf("printf '%%s' '%s' | base64 -d | $SUDO tee %s >/dev/null", encoded, path)
}

// vrfUnitWrites returns host scripts that write VRF and DHCP units for a link.
func vrfUnitWrites(link VRFLink, mtu int) []string {
	netdevPath, vrfNetworkPath, ifaceNetworkPath := networkdUnitPaths(link.Iface, link.VRF)
	netdev := fmt.Sprintf("[NetDev]\nName=%s\nKind=vrf\n\n[VRF]\nTable=%d\n", link.VRF, link.Table)
	vrfNetwork := fmt.Sprintf("[Match]\nName=%s\n\n[Network]\nConfigureWithoutCarrier=yes\n", link.VRF)
	ifaceNetwork := fmt.Sprintf(
		"[Match]\nName=%s\n\n[Link]\nMTUBytes=%d\n\n[Network]\nVRF=%s\nDHCP=%s\nIPv6AcceptRA=no\n%s",
		link.Iface, mtu, link.VRF, link.Family.String(), link.Family.dhcpSections())
	return []string{
		writeHostFileScript(netdevPath, netdev),
		writeHostFileScript(vrfNetworkPath, vrfNetwork),
		writeHostFileScript(ifaceNetworkPath, ifaceNetwork),
	}
}

// removeVRFNetworkd best-effort cleans networkd drop-ins and VRFs on one host.
func removeVRFNetworkd(h Host, links []VRFLink) {
	if len(links) == 0 {
		return
	}

	steps := []string{hostSudoPreamble}
	files := make([]string, 0, len(links)*3)
	ifaces := make([]string, 0, len(links))
	for _, link := range links {
		netdevPath, vrfNetworkPath, ifaceNetworkPath := networkdUnitPaths(link.Iface, link.VRF)
		files = append(files, netdevPath, vrfNetworkPath, ifaceNetworkPath)
		ifaces = append(ifaces, link.Iface)
	}
	steps = append(steps,
		fmt.Sprintf("$SUDO rm -f %s", strings.Join(files, " ")),
		"$SUDO networkctl reload 2>/dev/null || true",
		fmt.Sprintf("$SUDO networkctl reconfigure %s 2>/dev/null || true", strings.Join(ifaces, " ")),
	)
	for _, link := range links {
		if link.Family.wantsV4() {
			steps = append(steps, fmt.Sprintf("$SUDO ip -4 addr flush dev %s 2>/dev/null || true", link.Iface))
		}
		if link.Family.wantsV6() {
			steps = append(steps, fmt.Sprintf("$SUDO ip -6 addr flush dev %s 2>/dev/null || true", link.Iface))
		}
		steps = append(steps, fmt.Sprintf("$SUDO ip link del %s 2>/dev/null || true", link.VRF))
	}
	steps = append(steps, "true")

	if out, err := h.SSH(strings.Join(steps, "\n")); err != nil {
		GinkgoWriter.Printf("best-effort networkd cleanup on %s ifaces %s failed: %v, output: %s\n",
			h.Addr, strings.Join(ifaces, ", "), err, out)
	}
}
