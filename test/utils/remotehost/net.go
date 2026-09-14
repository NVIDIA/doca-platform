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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Net is the network context for traffic helpers, empty VRF uses the default namespace.
type Net struct {
	// VRF, when set, wraps commands with ip vrf exec.
	VRF string
	// Destination is the ping, iperf, or RDMA peer IP.
	Destination string
	// ClientBindIP is the optional iperf -B address.
	ClientBindIP string
	// RDMADev is the RDMA device for ib_write_bw.
	RDMADev string
	// Family is IPv4 or IPv6, zero value is IPv4.
	Family IPFamily
}

// GetLinkMAC returns the link MAC for iface on the remote host.
func (h Host) GetLinkMAC(iface string) string {
	var mac string
	Eventually(func(g Gomega) {
		out, err := h.SSH(fmt.Sprintf("cat /sys/class/net/%s/address", shellQuote(iface)))
		g.Expect(err).ToNot(HaveOccurred(), "failed to read MAC for %s on %s: %s", iface, h.Addr, out)
		mac = strings.ToLower(lastNonEmptyLine(out))
		g.Expect(mac).ToNot(BeEmpty(), "empty MAC for %s on %s: %q", iface, h.Addr, out)
	}, DefaultTimeout).Should(Succeed())
	By(fmt.Sprintf("Got link MAC %s for %s on %s", mac, iface, h.Addr))
	return mac
}

// FlushIface best-effort flushes host addresses on iface and drops the subnet route.
func (h Host) FlushIface(iface, subnet string, family IPFamily) {
	script := strings.Join([]string{hostSudoPreamble, family.flushScript(iface, subnet)}, "\n")
	if output, err := h.SSH(script); err != nil {
		GinkgoWriter.Printf("best-effort flush on %s dev %s failed: %v, output: %s\n", h.Addr, iface, err, output)
	}
}

// withVRF prepends ip vrf exec when vrf is set.
func withVRF(vrf string, cmd ...string) []string {
	if vrf == "" {
		return cmd
	}
	return append([]string{"ip", "vrf", "exec", vrf}, cmd...)
}

// execInNet runs cmd in the container, optionally inside VRF.
func (h Host) execInNet(cfg Net, cmd ...string) (string, error) {
	return h.exec(withVRF(cfg.VRF, cmd...)...)
}

// execShellInNet runs a shell script in the container, optionally inside VRF.
func (h Host) execShellInNet(cfg Net, script string) (string, error) {
	if cfg.VRF != "" {
		script = fmt.Sprintf("ip vrf exec %s sh -c %s", shellQuote(cfg.VRF), shellQuote(script))
	}
	return h.execShell(script)
}
