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
	"time"

	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	ibWriteBWPort           = 18515
	ibWriteBWClientJSONPath = "/tmp/ib_write_bw_client.json"
)

// Ping pings Destination, optionally inside VRF.
func (h Host) Ping(cfg Net) {
	Eventually(func(g Gomega) {
		output, err := h.execInNet(cfg, cfg.Family.pingArgs("4", "2", cfg.Destination)...)
		g.Expect(err).ToNot(HaveOccurred(),
			"failed to ping %s from container %s on %s:\n%s",
			cfg.Destination, h.ContainerName, h.Addr, output)
	}, DefaultTimeout).Should(Succeed())
}

// AssertPingFailure asserts ping to Destination consistently fails.
func (h Host) AssertPingFailure(cfg Net) {
	Consistently(func(g Gomega) {
		output, err := h.execInNet(cfg, cfg.Family.pingArgs("2", "2", cfg.Destination)...)
		g.Expect(err).To(HaveOccurred(),
			"ping %s from container %s on %s should fail:\n%s",
			cfg.Destination, h.ContainerName, h.Addr, output)
	}, 30*time.Second, 5*time.Second).Should(Succeed())
}

// RunTrafficTest runs bidirectional iperf3 using cfg Destination and optional ClientBindIP.
func RunTrafficTest(client, server Host, cfg Net) {
	server.startIperf3Server(cfg)
	defer server.stopIperf3Server()

	forwardResult := netshoot.ParseIperfResult(client.runIperf3Client(cfg, false))
	netshoot.AnalyzeIperfResults(forwardResult, false)

	reverseResult := netshoot.ParseIperfResult(client.runIperf3Client(cfg, true))
	netshoot.AnalyzeIperfResults(reverseResult, true)
}

// RunIBWriteBW runs ib_write_bw and asserts average BW above minAvgBWGbit.
func RunIBWriteBW(
	server, client Host,
	cfg Net,
	duration time.Duration,
	minAvgBWGbit float32,
	mtu int,
	extraArgs ...string,
) {
	extra := strings.Join(extraArgs, " ")
	durationSec := int(duration / time.Second)

	By(fmt.Sprintf("Starting ib_write_bw server on %s (dev=%s, -D=%s, extra=%v)",
		server.Addr, cfg.RDMADev, duration, extraArgs))
	serverInner := fmt.Sprintf(
		"pkill -9 ib_write_bw 2>/dev/null; "+
			"nohup ib_write_bw -d %s -D %d -q 1 -m %d --report_gbit %s >/dev/null 2>&1 < /dev/null &",
		cfg.RDMADev, durationSec, mtu, extra)
	out, err := server.execShellInNet(cfg, serverInner)
	Expect(err).ToNot(HaveOccurred(),
		"starting ib_write_bw server on %s failed: %s", server.Addr, out)
	defer func() {
		if killOut, killErr := server.execShell("pkill -9 ib_write_bw 2>/dev/null || true"); killErr != nil {
			GinkgoWriter.Printf("best-effort pkill ib_write_bw on %s failed: %v, output: %s\n",
				server.Addr, killErr, killOut)
		}
	}()

	By(fmt.Sprintf("Waiting for ib_write_bw to listen on :%d on %s",
		ibWriteBWPort, server.Addr))
	listenInner := cfg.Family.ssListenCmd(ibWriteBWPort)
	Eventually(func(g Gomega) {
		listenOut, listenErr := server.execShellInNet(cfg, listenInner)
		g.Expect(listenErr).ToNot(HaveOccurred(),
			"ib_write_bw not listening on :%d on %s: %s", ibWriteBWPort, server.Addr, listenOut)
	}, 30*time.Second).Should(Succeed())

	By(fmt.Sprintf("Running ib_write_bw client on %s -> %s (dev=%s, -D=%s, extra=%v)",
		client.Addr, cfg.Destination, cfg.RDMADev, duration, extraArgs))
	clientInner := fmt.Sprintf(
		"rm -f %s; ib_write_bw -d %s -D %d -q 1 -m %d --report_gbit --out_json --out_json_file=%s %s %s",
		ibWriteBWClientJSONPath, cfg.RDMADev, durationSec, mtu,
		ibWriteBWClientJSONPath, extra, cfg.Destination)
	cliOut, cliErr := client.execShellInNet(cfg, clientInner)
	Expect(cliErr).ToNot(HaveOccurred(),
		"ib_write_bw client on %s failed: %s", client.Addr, cliOut)

	jsonOut, jsonErr := client.exec("cat", ibWriteBWClientJSONPath)
	Expect(jsonErr).ToNot(HaveOccurred(),
		"reading ib_write_bw client JSON on %s failed: %s", client.Addr, jsonOut)

	By(fmt.Sprintf("Checking ib_write_bw average BW against threshold %.2f Gbit/sec", minAvgBWGbit))
	netshoot.AnalyzeIBWriteBWResult(jsonOut, minAvgBWGbit)
}

// startIperf3Server starts a daemon iperf3 server, optionally inside VRF.
func (h Host) startIperf3Server(cfg Net) {
	args := withVRF(cfg.VRF, "iperf3", "-s", "-D")
	if flag := cfg.Family.iperfIPFlag(); flag != "" {
		args = append(args, flag)
	}
	h.execEventually(DefaultTimeout, args...)
}

// stopIperf3Server best-effort kills iperf3.
func (h Host) stopIperf3Server() {
	if output, err := h.exec("pkill", "iperf3"); err != nil {
		GinkgoWriter.Printf("best-effort iperf3 stop on %s failed: %v, output: %s\n", h.Addr, err, output)
	}
}

// runIperf3Client runs iperf3 -c against Destination, reverse is -R.
func (h Host) runIperf3Client(cfg Net, reverse bool) string {
	args := withVRF(cfg.VRF, "iperf3", "-c", cfg.Destination, "-J")
	if flag := cfg.Family.iperfIPFlag(); flag != "" {
		args = append(args, flag)
	}
	if cfg.ClientBindIP != "" {
		args = append(args, "-B", cfg.ClientBindIP)
	}
	if reverse {
		args = append(args, "-R")
	}
	var output string
	Eventually(func(g Gomega) {
		var err error
		output, err = h.exec(args...)
		if err != nil {
			if parsedErr := netshoot.IperfErrorParser(output, ""); parsedErr != "" {
				output = parsedErr
			}
		}
		g.Expect(err).ToNot(HaveOccurred(), "iperf3 client failed on %s: %s", h.Addr, output)
	}, DefaultTimeout).Should(Succeed())
	return output
}
