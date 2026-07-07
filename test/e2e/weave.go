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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/expfmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Host-side PF interface names. These are the host PF netdevs that the flow controller creates PF
	// attachments for in NIC Cloud.
	weaveHostPFInterfaceP0 = "enp8s0f0np0"
	weaveHostPFInterfaceP1 = "enp8s0f1np1"

	// DPU-side port names used by the flow-controller for PF lookups.
	weaveDPUPortP0 = "p0"
	weaveDPUPortP1 = "p1"

	// weaveDPUServiceLabelKey is propagated from spec.serviceDaemonSet.labels in our DPUService
	// manifests to every pod. We use it (rather than svc.dpu.nvidia.com/service, which the
	// controller hashes via generateServiceID) because the value is human-readable and stable.
	weaveDPUServiceLabelKey = "dpuservice"

	// DPUService label values and pod-name substrings for the Weave workloads on the DPU cluster.
	weaveFlowControllerName = "weave-flow-controller"
	weaveDHCPAgentName      = "weave-dhcp-agent"

	// Host DHCP NetworkAttachmentDefinition names for Weave PF interfaces.
	weaveDHCPNADP0 = "weave-dhcp-p0"
	weaveDHCPNADP1 = "weave-dhcp-p1"

	// DPU-side PCI addresses (underscored form used in OVS bridge names) for the two NIC ports.
	// Pinned by the dpf-bootstrap deployment, see DPUService-weave-flow-controller.yml.
	// If the underlay config in the DPUService changes, these need to follow.
	weaveDPUPortP0PCIUnderscored = "0000_03_00_0"
	weaveDPUPortP1PCIUnderscored = "0000_03_00_1"

	// Host-side RDMA device name (ibv) for the p0 PF netdev. Pinned by hardware/driver enumeration
	// on the worker.
	weaveHostPFRDMADeviceP0 = "mlx5_0"

	weavePFMTU = 9000

	// weaveVNetSubnet is the /8 overlay IPv4 subnet used for all Weave virtual networks.
	weaveVNetSubnet = "10.0.0.0/8"

	// weaveRDMAMinAvgBWGbit gates that HW offload is engaged: with offload, BF2/BF3 sustain >60 Gbit/sec;
	// without it, the SW path tops at a much lower average.
	weaveRDMAMinAvgBWGbit = 60.0

	// weaveIBWriteBWDuration is the per-run duration for the host-side ib_write_bw test (-D).
	weaveIBWriteBWDuration = 5 * time.Second

	// weaveIBWriteBWPort is the default TCP port ib_write_bw listens on (used to wait for
	// the server to be ready before launching the client).
	weaveIBWriteBWPort = 18515

	// weaveIBWriteBWClientJSONPath is where ib_write_bw client writes its JSON result inside the pod.
	weaveIBWriteBWClientJSONPath = "/tmp/ib_write_bw_client.json"

	// weaveRDMAFlushCmdFmt is best-effort cleanup shared by releaseDHCPLeaseInPod and the pod preStop hook.
	// Steps are independent (`;`, not `&&`): on a fresh pod dhcpcd -k has nothing to kill and exits non-zero,
	// so chaining with `&&` would skip the addr flush and lease wipe. Trailing `true` keeps exec exit 0.
	weaveRDMAFlushCmdFmt = "dhcpcd -k %[1]s 2>/dev/null; ip -4 addr flush dev %[1]s 2>/dev/null; rm -f /var/lib/dhcpcd/%[1]s.lease*; true"

	// weaveDPUTunnelCleanupTimeout is for vpcctl deletes over the tunneled DPU REST client. When the tunnel
	// drops, getDPUClusterClient's inner Eventually can take up to 3m (system_setup.go); a shorter timeout
	// causes flaky failures (e.g. "connection reset by peer" on pod exec) attributed to the last It block.
	weaveDPUTunnelCleanupTimeout = 4 * time.Minute

	// weaveOperationTimeout is the default ceiling for vpcctl and DPU pod exec Eventually loops (create,
	// wait, verify). Tunnel and API variance affect these the same way; one value avoids brittle 30s/60s/2m splits.
	weaveOperationTimeout = 2 * time.Minute

	weaveEventuallyPollInterval = 1 * time.Second

	// weaveMetricBurstCount is the ICMP echo count per ping-based metric-delta burst.
	weaveMetricBurstCount = 30

	// weaveTxAccountingSlack is the acceptable gap, in packets, between host_tx and (tx_sent+tx_dropped). The gap
	// exists because broadcast/ARP/DHCP are counted in host_tx but not in tx_sent or tx_dropped.
	weaveTxAccountingSlack = 2

	// weaveCrossNodePacketDriftTolerance is the maximum allowed drift, in packets, between matching sender/receiver
	// this is just a small margin for a rare in-flight/lost packet across the tunnel.
	weaveCrossNodePacketDriftTolerance = 2

	// Names of the OVS flow metrics we use in the tests.
	weaveMetricHostTx        = "weave_host_tx"
	weaveMetricHostRx        = "weave_host_rx"
	weaveMetricTxSent        = "weave_tx_sent"
	weaveMetricTxDropped     = "weave_tx_dropped"
	weaveMetricRxDecap       = "weave_rx_decap"
	weaveMetricRxDropped     = "weave_rx_dropped"
	weaveMetricRxVNIMismatch = "weave_rx_vni_mismatch"
)

// weavePodsToVerify is used by Weave tests to wait for Weave workloads on the DPU cluster (pod names contain these substrings).
var weavePodsToVerify = []string{
	weaveDHCPAgentName,
	weaveFlowControllerName,
}

var (
	// weaveContextScope manages cleanup for test-specific Weave resources within each test Context.
	weaveContextScope *cleanup.Scope

	// weavePrerequisiteScope manages cleanup for shared Weave prerequisites (e.g. the host DHCP CNI
	// daemon) that must outlive individual Contexts. It is cleaned up only in the top-level AfterAll.
	weavePrerequisiteScope *cleanup.Scope
)

var weaveInput = &weaveTestInput{}

// vpcctlVNetResponse is used to parse create-vnet / get-vnet JSON responses.
type vpcctlVNetResponse struct {
	VirtualNetwork struct {
		Spec struct {
			ID string `json:"id"`
		} `json:"spec"`
		Status struct {
			State struct {
				Phase string `json:"phase"`
			} `json:"state"`
		} `json:"status"`
	} `json:"virtualNetwork"`
}

// vpcctlAttachmentResponse is used to parse create-attachment / get-attachment JSON responses.
type vpcctlAttachmentResponse struct {
	VirtualNetworkAttachment struct {
		Spec struct {
			ID string `json:"id"`
		} `json:"spec"`
		Status struct {
			State struct {
				Phase string `json:"phase"`
			} `json:"state"`
			HostIPv4 string `json:"hostIpv4"`
		} `json:"status"`
	} `json:"virtualNetworkAttachment"`
}

// vpcctlListAttachmentResponse is used to parse list-attachment JSON responses.
type vpcctlListAttachmentResponse struct {
	VirtualNetworkAttachments []struct {
		Spec struct {
			ID string `json:"id"`
		} `json:"spec"`
	} `json:"virtualNetworkAttachments"`
}

// weaveTestInput holds objects loaded from config for Weave e2e (see applyWeaveConfig).
type weaveTestInput struct {
	dhcpDaemonSet *appsv1.DaemonSet
}

func (t *weaveTestInput) applyWeaveConfig(conf config) {
	t.dhcpDaemonSet = requiredObjectFromFile[appsv1.DaemonSet](Domain.Weave, "dhcpDaemonSet", conf.DHCPDaemonSetPath)
}

// WeaveBeforeSuite is called from the e2e BeforeSuite to load Weave test artifacts from config.
func WeaveBeforeSuite(c config) {
	By("Setting Weave configs for the test")
	weaveInput.applyWeaveConfig(c)
}

// getProvisionDPUClustersInputForWeave returns provision input for Weave tests.
func getProvisionDPUClustersInputForWeave(ctx context.Context, provisionInput ProvisionDPUClustersInput, cl client.Client) ProvisionDPUClustersInput {
	if dpuClusterName != "" && dpuClusterNamespace != "" {
		name, ns := dpuClusterName, dpuClusterNamespace
		dc := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
		if err := cl.Get(ctx, client.ObjectKeyFromObject(dc), dc); err == nil {
			provisionInput.dpuClusters = []*provisioningv1.DPUCluster{dc}
			return provisionInput
		}
	}
	if len(provisionInput.dpuClusters) > 0 {
		key := client.ObjectKeyFromObject(provisionInput.dpuClusters[0])
		if err := cl.Get(ctx, key, provisionInput.dpuClusters[0]); err == nil {
			return provisionInput
		}
	}
	// No readiness filter needed here — getDPUClusterClients waits for the
	// DPUCluster to be ready and have a kubeconfig before creating the client.
	list := &provisioningv1.DPUClusterList{}
	Expect(cl.List(ctx, list)).To(Succeed())
	if len(list.Items) > 0 {
		provisionInput.dpuClusters = []*provisioningv1.DPUCluster{&list.Items[0]}
	}
	return provisionInput
}

// verifyOVSResponsive runs `ovs-vsctl show` on the given flow-controller pod and asserts
// it returns non-empty output. Acts as an early sanity check that OVS is up.
func verifyOVSResponsive(pod *corev1.Pod) {
	By(fmt.Sprintf("Verifying OVS is responsive on pod %s (node %s)", pod.Name, pod.Spec.NodeName))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name,
			[]string{"ovs-vsctl", "show"})
		g.Expect(err).ToNot(HaveOccurred(), "ovs-vsctl show failed on pod %s: %s", pod.Name, out)
		g.Expect(strings.TrimSpace(out)).ToNot(BeEmpty(), "ovs-vsctl show returned empty output on pod %s", pod.Name)
	}).WithTimeout(30 * time.Second).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
}

// getPFMACFromFlowControllerByPort reads the PF MAC for the given DPU-side port (e.g. "p0" or "p1")
// from the smart_nic sysfs config inside the flow-controller pod.
// NOTE: This will not work on BF4 ASTRA setup since ECPFs have different names.
func getPFMACFromFlowControllerByPort(pod *corev1.Pod, port string) string {
	cmd := []string{"sh", "-c", fmt.Sprintf(`grep -i '^MAC' /sys/class/net/%s/smart_nic/pf/config | head -1 | sed 's/^[^:]*:[[:space:]]*//'`, port)}
	var mac string
	Eventually(func(g Gomega) {
		output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, cmd)
		g.Expect(err).ToNot(HaveOccurred(), "failed to read PF MAC (%s) from pod %s: %s", port, pod.Name, output)
		mac = strings.TrimSpace(output)
		g.Expect(mac).ToNot(BeEmpty(), "empty PF MAC (%s) from pod %s", port, pod.Name)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"could not read PF MAC (%s) from flow-controller pod %s", port, pod.Name)
	By(fmt.Sprintf("Got PF MAC %s for port %s from pod %s (node %s)", mac, port, pod.Name, pod.Spec.NodeName))
	return mac
}

func assertVPCtlVNetPhaseReady(g Gomega, pod *corev1.Pod, vnetID string) {
	out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name,
		[]string{"/vpcctl", "get-vnet", "--id", vnetID})
	g.Expect(err).ToNot(HaveOccurred(), "vpcctl get-vnet %q on pod %s: %s", vnetID, pod.Name, out)
	var resp vpcctlVNetResponse
	g.Expect(json.Unmarshal([]byte(out), &resp)).To(Succeed(), "failed to parse get-vnet %q on pod %s: %s", vnetID, pod.Name, out)
	g.Expect(resp.VirtualNetwork.Status.State.Phase).To(Equal("PHASE_READY"),
		"virtual network %q on pod %s not PHASE_READY: %s", vnetID, pod.Name, out)
}

func assertVPCtlAttachmentPhaseReady(g Gomega, pod *corev1.Pod, attachmentID string) {
	out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name,
		[]string{"/vpcctl", "get-attachment", "--id", attachmentID})
	g.Expect(err).ToNot(HaveOccurred(), "vpcctl get-attachment %q on pod %s: %s", attachmentID, pod.Name, out)
	var resp vpcctlAttachmentResponse
	g.Expect(json.Unmarshal([]byte(out), &resp)).To(Succeed(), "failed to parse get-attachment %q on pod %s: %s", attachmentID, pod.Name, out)
	g.Expect(resp.VirtualNetworkAttachment.Status.State.Phase).To(Equal("PHASE_READY"),
		"attachment %q on pod %s not PHASE_READY: %s", attachmentID, pod.Name, out)
}

// createVNetOnPod creates a virtual network on a flow-controller pod via vpcctl and asserts it reaches PHASE_READY.
// The same vnetID and vni must be used on both flow-controller pods so that cross-node VXLAN traffic uses matching VNIs.
func createVNetOnPod(pod *corev1.Pod, vnetID string, vni uint32, subnet string) {
	By(fmt.Sprintf("Creating virtual network %q (vni=%d, subnet=%s) on pod %s", vnetID, vni, subnet, pod.Name))
	cmd := []string{
		"/vpcctl", "create-vnet",
		"--id", vnetID,
		"--vni", fmt.Sprintf("%d", vni),
		"--subnet-v4", subnet,
	}
	Eventually(func(g Gomega) {
		output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, cmd)
		if err != nil && strings.Contains(output, "AlreadyExists") {
			assertVPCtlVNetPhaseReady(g, pod, vnetID)
			return
		}
		g.Expect(err).ToNot(HaveOccurred(), "vpcctl create-vnet failed on pod %s: %s", pod.Name, output)
		var resp vpcctlVNetResponse
		g.Expect(json.Unmarshal([]byte(output), &resp)).To(Succeed(), "failed to parse create-vnet response from pod %s: %s", pod.Name, output)
		g.Expect(resp.VirtualNetwork.Status.State.Phase).To(Equal("PHASE_READY"),
			"virtual network %q on pod %s not PHASE_READY: %s", vnetID, pod.Name, output)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"failed to create virtual network %q on pod %s", vnetID, pod.Name)
}

// listAttachmentIDs runs vpcctl list-attachment with the given filter flags (e.g. --nic-id, --vnet-id)
// and returns the attachment IDs from the JSON response. Used to discover stale attachments that
// block create/delete operations without relying on parsing gRPC error strings.
func listAttachmentIDs(g Gomega, pod *corev1.Pod, filterFlags ...string) []string {
	args := append([]string{"/vpcctl", "list-attachment"}, filterFlags...)
	out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, args)
	g.Expect(err).ToNot(HaveOccurred(), "vpcctl list-attachment %v failed on pod %s: %s", filterFlags, pod.Name, out)
	var resp vpcctlListAttachmentResponse
	g.Expect(json.Unmarshal([]byte(out), &resp)).To(Succeed(),
		"failed to parse list-attachment response on pod %s: %s", pod.Name, out)
	ids := make([]string, 0, len(resp.VirtualNetworkAttachments))
	for _, a := range resp.VirtualNetworkAttachments {
		ids = append(ids, a.Spec.ID)
	}
	return ids
}

// createPFAttachmentAndWaitForHostIP creates a PF attachment on a flow-controller pod, waits until it is PHASE_READY,
// and returns the attachment ID together with the assigned host overlay IP (hostIpv4).
// If the NIC already has a stale attachment from a previous test run or context, it is deleted and the create is retried.
func createPFAttachmentAndWaitForHostIP(pod *corev1.Pod, vnetID, pfMAC string) (attID, hostIP string) {
	// Deterministic ID so the create is idempotent: if the tunnel drops after the server
	// processes the request, a retry returns AlreadyExists rather than FailedPrecondition
	// (the server enforces one attachment per NIC).
	macHex := strings.ReplaceAll(strings.ToLower(pfMAC), ":", "")
	if len(macHex) > 4 {
		macHex = macHex[len(macHex)-4:]
	}
	attID = fmt.Sprintf("e2e-%s-%s", vnetID, macHex)

	By(fmt.Sprintf("Creating PF attachment for MAC %s on pod %s", pfMAC, pod.Name))
	cmd := []string{
		"/vpcctl", "create-attachment",
		"--id", attID,
		"--vnet-id", vnetID,
		"--nic-id", pfMAC,
		"--type", "pf",
		"--pf", pfMAC,
	}
	Eventually(func(g Gomega) {
		output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, cmd)
		if err != nil && strings.Contains(output, "AlreadyExists") {
			assertVPCtlAttachmentPhaseReady(g, pod, attID)
			return
		}
		if err != nil && strings.Contains(output, "already attached") {
			staleIDs := listAttachmentIDs(g, pod, "--nic-id", pfMAC)
			for _, staleID := range staleIDs {
				By(fmt.Sprintf("NIC %s has stale attachment %s — deleting before retry", pfMAC, staleID))
				delOut, delErr := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name,
					[]string{"/vpcctl", "delete-attachment", "--id", staleID})
				if delErr != nil && !strings.Contains(delOut, "NotFound") {
					g.Expect(delErr).ToNot(HaveOccurred(), "failed to delete stale attachment %s on pod %s: %s", staleID, pod.Name, delOut)
				}
			}
			g.Expect(false).To(BeTrue(), "retry vpcctl create-attachment for nic %s after clearing %d stale attachment(s)", pfMAC, len(staleIDs))
		}
		g.Expect(err).ToNot(HaveOccurred(), "vpcctl create-attachment failed on pod %s: %s", pod.Name, output)
		var createResp vpcctlAttachmentResponse
		g.Expect(json.Unmarshal([]byte(output), &createResp)).To(Succeed(), "failed to parse create-attachment response from pod %s: %s", pod.Name, output)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"failed to create PF attachment for MAC %s on pod %s", pfMAC, pod.Name)

	By(fmt.Sprintf("Waiting for attachment %s on pod %s to reach PHASE_READY", attID, pod.Name))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"/vpcctl", "get-attachment", "--id", attID})
		g.Expect(err).ToNot(HaveOccurred())
		var resp vpcctlAttachmentResponse
		g.Expect(json.Unmarshal([]byte(out), &resp)).To(Succeed())
		g.Expect(resp.VirtualNetworkAttachment.Status.State.Phase).To(Equal("PHASE_READY"))
		hostIP = resp.VirtualNetworkAttachment.Status.HostIPv4
		g.Expect(hostIP).ToNot(BeEmpty())
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"attachment %s on pod %s did not reach PHASE_READY", attID, pod.Name)

	By(fmt.Sprintf("Attachment %s ready on pod %s — host overlay IP: %s", attID, pod.Name, hostIP))
	return attID, hostIP
}

var dpuPortToPCIUnderscored = map[string]string{
	weaveDPUPortP0: weaveDPUPortP0PCIUnderscored,
	weaveDPUPortP1: weaveDPUPortP1PCIUnderscored,
}

var dpuPortToDropNIC = map[string]string{
	weaveDPUPortP0: "n0",
	weaveDPUPortP1: "n1",
}

// isolationBridgeName returns the OVS isolation bridge name for a VNI on a DPU port.
func isolationBridgeName(vni uint32, dpuPort string) string {
	pci, ok := dpuPortToPCIUnderscored[dpuPort]
	Expect(ok).To(BeTrue(), "unknown DPU port %q", dpuPort)
	return fmt.Sprintf("br-isol-%d-%s", vni, pci)
}

// verifyIsolationBridgeExists asserts that the OVS isolation bridge for the given VNI
// on the given DPU port (br-isol-<vni>-<pci_underscored>) is present on the flow-controller pod.
func verifyIsolationBridgeExists(pod *corev1.Pod, vni uint32, dpuPort string) {
	bridge := isolationBridgeName(vni, dpuPort)
	By(fmt.Sprintf("Verifying bridge %s exists on pod %s", bridge, pod.Name))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name,
			[]string{"ovs-vsctl", "list", "bridge", bridge})
		g.Expect(err).ToNot(HaveOccurred(), "bridge %s not found on pod %s: %s", bridge, pod.Name, out)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
}

// weaveMetricFamily is the Prometheus metric family emitted by `ovs-appctl metrics/show` carrying per-flow packet
// counters. Each sample is labeled with the bridge and the weave_* counter name.
const weaveMetricFamily = "ovs_vswitchd_flow_packets_total"

// weaveMetrics holds weave packet counters from one scrape of a flow-controller pod,
// keyed by bridge then counter name (e.g. metrics["br-isol-1001-..."]["weave_host_tx"]).
type weaveMetrics map[string]map[string]uint64

// scrapeWeaveMetrics performs a single weave-metrics scrape of a flow-controller pod, keyed [bridge][name].
func scrapeWeaveMetrics(g Gomega, pod *corev1.Pod) weaveMetrics {
	cmd := []string{"sh", "-c", `exec ovs-appctl -t /var/run/openvswitch/ovs-vswitchd.$(cat /var/run/openvswitch/ovs-vswitchd.pid).ctl metrics/show`}

	out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, cmd)
	g.Expect(err).ToNot(HaveOccurred(), "ovs-appctl metrics/show failed on pod %s: %s", pod.Name, out)

	families, perr := (&expfmt.TextParser{}).TextToMetricFamilies(strings.NewReader(out))
	if perr != nil && len(families) == 0 {
		g.Expect(perr).ToNot(HaveOccurred(), "fatal parse error in metrics/show output on pod %s: %s", pod.Name, out)
	}

	metrics := weaveMetrics{}
	for _, m := range families[weaveMetricFamily].GetMetric() {
		var bridge, name string
		for _, l := range m.GetLabel() {
			switch l.GetName() {
			case "bridge":
				bridge = l.GetValue()
			case "name":
				name = l.GetValue()
			}
		}
		if bridge == "" || !strings.HasPrefix(name, "weave_") {
			continue
		}
		if metrics[bridge] == nil {
			metrics[bridge] = map[string]uint64{}
		}
		// The family is counter-typed, so the value lives in Counter.
		metrics[bridge][name] = uint64(m.GetCounter().GetValue())
	}
	g.Expect(metrics).ToNot(BeEmpty(), "no weave_* metrics found on pod %s: %s", pod.Name, out)
	return metrics
}

// readWeaveMetrics scrapes weave packet counters with retry, for standalone use outside an
// Eventually. Inside an outer Eventually, call scrapeWeaveMetrics instead.
func readWeaveMetrics(pod *corev1.Pod) weaveMetrics {
	var metrics weaveMetrics
	Eventually(func(g Gomega) {
		metrics = scrapeWeaveMetrics(g, pod)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"failed to read weave metrics from pod %s", pod.Name)
	return metrics
}

// metricDelta returns after-before for a counter on a bridge, asserting it did not go backwards
func metricDelta(g Gomega, before, after weaveMetrics, bridge, name string) uint64 {
	b, bOK := before[bridge][name]
	a, aOK := after[bridge][name]
	g.Expect(bOK).To(BeTrue(), "weave metric %s missing on bridge %s in before scrape", name, bridge)
	g.Expect(aOK).To(BeTrue(), "weave metric %s missing on bridge %s in after scrape", name, bridge)
	g.Expect(a).To(BeNumerically(">=", b), "weave metric %s on bridge %s went backwards (%d -> %d): OVS restart?", name, bridge, b, a)
	return a - b
}

// metricDeltaExpect describes how a set of weave counters should move between two scrapes.
type metricDeltaExpect struct {
	// mustRiseBy maps a counter name to the minimum delta it must have gained.
	mustRiseBy map[string]uint64
	// mustStayFlat lists counters whose delta must be exactly zero.
	mustStayFlat []string
}

// assertMetricDeltas checks, on the given bridge, that every counter in expect.mustRiseBy advanced
// by at least its minimum and every counter in expect.mustStayFlat did not move.
func assertMetricDeltas(g Gomega, before, after weaveMetrics, bridge string, expect metricDeltaExpect) {
	for name, minDelta := range expect.mustRiseBy {
		delta := metricDelta(g, before, after, bridge, name)
		g.Expect(delta).To(BeNumerically(">=", minDelta), "weave metric %s on bridge %s: delta %d < expected %d", name, bridge, delta, minDelta)
	}
	for _, name := range expect.mustStayFlat {
		delta := metricDelta(g, before, after, bridge, name)
		g.Expect(delta).To(BeZero(), "weave metric %s on bridge %s: expected no change, got delta %d", name, bridge, delta)
	}
}

// assertTxPacketsAccountedFor asserts every host_tx packet is accounted for as tx_sent or tx_dropped,
// leaving only a small remainder (DHCP/ARP) under slack — i.e. no TX packets silently vanish.
func assertTxPacketsAccountedFor(g Gomega, before, after weaveMetrics, bridge string) {
	hostTx := metricDelta(g, before, after, bridge, weaveMetricHostTx)
	txSent := metricDelta(g, before, after, bridge, weaveMetricTxSent)
	txDropped := metricDelta(g, before, after, bridge, weaveMetricTxDropped)
	accounted := txSent + txDropped
	g.Expect(hostTx).To(BeNumerically(">=", accounted),
		"tx accounting on %s: tx_sent(%d)+tx_dropped(%d)=%d exceeds host_tx delta %d", bridge, txSent, txDropped, accounted, hostTx)
	g.Expect(hostTx).To(BeNumerically("<", accounted+weaveTxAccountingSlack),
		"tx accounting on %s: host_tx delta %d exceeds tx_sent(%d)+tx_dropped(%d)=%d by >= slack %d", bridge, hostTx, txSent, txDropped, accounted, weaveTxAccountingSlack)
}

// metricRef identifies one weave counter sampled before and after traffic: a counter name on a
// specific bridge, paired with its two scrapes.
type metricRef struct {
	before, after weaveMetrics
	bridge, name  string
}

// assertMetricDeltasMatch asserts the sender and receiver counters advanced by the same amount
// within tolerance — e.g. tx_sent on the sender DPU vs rx_decap on the receiver DPU track the same overlay
// packets.
func assertMetricDeltasMatch(g Gomega, sender, receiver metricRef) {
	src := metricDelta(g, sender.before, sender.after, sender.bridge, sender.name)
	dst := metricDelta(g, receiver.before, receiver.after, receiver.bridge, receiver.name)
	// Absolute difference between the two counters.
	diff := max(src, dst) - min(src, dst)
	g.Expect(diff).To(BeNumerically("<=", weaveCrossNodePacketDriftTolerance),
		"%s on %s delta %d vs %s on %s delta %d differ by %d packets (> tolerance %d)",
		sender.name, sender.bridge, src, receiver.name, receiver.bridge, dst, diff, weaveCrossNodePacketDriftTolerance)
}

// ensureOverlayRoute ensures the route for subnet on a netshoot pod uses the DHCP
// gateway rather than being on-link. The CNI DHCP plugin sometimes fails to apply
// option 121 classless static routes correctly. overlayIP is the pod's known overlay
// address (from createPFAttachmentAndWaitForHostIP); for a /31 the gateway is the peer.
//
// restClient and restCfg must be for the host (management) cluster where netshoot pods run.
func ensureOverlayRoute(restClient *rest.RESTClient, restCfg *rest.Config, namespace, podName, overlayIP, subnet string) {
	const overlayIface = "net1"
	ip := net.ParseIP(overlayIP).To4()
	Expect(ip).ToNot(BeNil(), "invalid overlay IP %s for pod %s", overlayIP, podName)
	gateway := net.IPv4(ip[0], ip[1], ip[2], ip[3]^1).String()

	Eventually(func(g Gomega) {
		linkOut, linkErr := netshoot.ExecInPodOnce(restClient, restCfg, namespace, podName,
			[]string{"ip", "link", "set", overlayIface, "up"})
		g.Expect(linkErr).ToNot(HaveOccurred(), "failed to bring up %s on pod %s: %s", overlayIface, podName, linkOut)
		output, err := netshoot.ExecInPodOnce(restClient, restCfg, namespace, podName,
			[]string{"ip", "route", "replace", subnet, "via", gateway, "dev", overlayIface})
		g.Expect(err).ToNot(HaveOccurred(), "failed to set overlay route on pod %s: %s", podName, output)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"overlay route on pod %s not set after timeout", podName)
	By(fmt.Sprintf("Overlay route on pod %s: %s via %s", podName, subnet, gateway))
}

// addRouteOnPodBetweenOverlayAndSubnet installs a route on a netshoot pod for the given (foreign) subnet
// via the pod's /31 overlay peer. Used to force traffic destined for another VNet's subnet out the local
// overlay interface so the isolation enforcement on the DPU is exercised.
func addRouteOnPodBetweenOverlayAndSubnet(restClient *rest.RESTClient, restCfg *rest.Config, namespace, podName, overlayIP, subnet string) {
	const overlayIface = "net1"
	ip := net.ParseIP(overlayIP).To4()
	Expect(ip).ToNot(BeNil(), "invalid overlay IP %s for pod %s", overlayIP, podName)
	gatewayIP := net.IPv4(ip[0], ip[1], ip[2], ip[3]^1).String()
	Eventually(func(g Gomega) {
		output, err := netshoot.ExecInPodOnce(restClient, restCfg, namespace, podName,
			[]string{"ip", "route", "replace", subnet, "via", gatewayIP, "dev", overlayIface})
		g.Expect(err).ToNot(HaveOccurred(), "failed to set overlay route on pod %s: %s", podName, output)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"overlay route on pod %s not set after timeout", podName)
	By(fmt.Sprintf("Cross-subnet route on pod %s: %s via %s", podName, subnet, gatewayIP))
}

// deleteAttachmentOnPod deletes a virtual network attachment via vpcctl on the given flow-controller pod.
func deleteAttachmentOnPod(pod *corev1.Pod, attID string) {
	By(fmt.Sprintf("Deleting attachment %s on pod %s", attID, pod.Name))
	Eventually(func(g Gomega) {
		output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"/vpcctl", "delete-attachment", "--id", attID})
		if err != nil && strings.Contains(output, "NotFound") {
			return
		}
		g.Expect(err).ToNot(HaveOccurred(), "vpcctl delete-attachment %s failed on pod %s: %s", attID, pod.Name, output)
	}).WithTimeout(weaveDPUTunnelCleanupTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"failed to delete attachment %s on pod %s", attID, pod.Name)
}

// deleteVNetOnPod deletes a virtual network via vpcctl on the given flow-controller pod.
// If an attachment is still attached (FailedPrecondition), it deletes the blocking attachment first.
func deleteVNetOnPod(pod *corev1.Pod, vnetID string) {
	By(fmt.Sprintf("Deleting virtual network %s on pod %s", vnetID, pod.Name))
	Eventually(func(g Gomega) {
		output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"/vpcctl", "delete-vnet", "--id", vnetID})
		if err != nil && strings.Contains(output, "NotFound") {
			return
		}
		if err != nil && strings.Contains(output, "still attached") {
			staleIDs := listAttachmentIDs(g, pod, "--vnet-id", vnetID)
			for _, staleID := range staleIDs {
				By(fmt.Sprintf("VNet %s still has attachment %s — deleting before retry", vnetID, staleID))
				deleteAttachmentOnPod(pod, staleID)
			}
			g.Expect(fmt.Errorf("deleted %d blocking attachment(s) for vnet %s, retrying vnet delete", len(staleIDs), vnetID)).ToNot(HaveOccurred())
		}
		g.Expect(err).ToNot(HaveOccurred(), "vpcctl delete-vnet %s failed on pod %s: %s", vnetID, pod.Name, output)
	}).WithTimeout(weaveDPUTunnelCleanupTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"failed to delete virtual network %s on pod %s", vnetID, pod.Name)
}

// createNetutilsHostPodOnNode creates a privileged hostNetwork netutils pod on nodeName and waits for Ready state.
func createNetutilsHostPodOnNode(ctx context.Context, c client.Client, namespace, podName, nodeName string) *corev1.Pod {
	By(fmt.Sprintf("Creating netutils host pod %s/%s on node %s", namespace, podName, nodeName))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    weaveContextScope.CleanupLabels,
		},
		Spec: corev1.PodSpec{
			NodeName:         nodeName,
			HostNetwork:      true,
			DNSPolicy:        corev1.DNSClusterFirstWithHostNet,
			RestartPolicy:    corev1.RestartPolicyNever,
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: dpfPullSecretName}},
			Containers: []corev1.Container{{
				Name:    "netutils",
				Image:   fmt.Sprintf("%s:%s", netutilsImage, tag),
				Command: []string{"/bin/sh", "-c", "sleep infinity"},
				SecurityContext: &corev1.SecurityContext{
					Privileged: ptr.To(true),
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "dev-infiniband", MountPath: "/dev/infiniband"},
					{Name: "sys", MountPath: "/sys", ReadOnly: true},
				},
				// Backstop flush: kubelet runs preStop on pod delete.
				Lifecycle: &corev1.Lifecycle{
					PreStop: &corev1.LifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"/bin/sh", "-c",
								fmt.Sprintf(weaveRDMAFlushCmdFmt, weaveHostPFInterfaceP0),
							},
						},
					},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "dev-infiniband", VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/dev/infiniband", Type: ptr.To(corev1.HostPathDirectory)},
				}},
				{Name: "sys", VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/sys"},
				}},
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, pod))).To(Succeed(), "creating pod %s/%s", namespace, podName)

	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
		g.Expect(netshoot.IsPodRunningAndReady(pod)).To(BeTrue(), "pod %s/%s not ready", namespace, podName)
	}).WithTimeout(2 * time.Minute).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
	return pod
}

// releaseDHCPLeaseInPod runs weaveRDMAFlushCmdFmt inside the pod. Best-effort; preStop is the backstop.
func releaseDHCPLeaseInPod(restClient *rest.RESTClient, restCfg *rest.Config, pod *corev1.Pod, iface string) {
	By(fmt.Sprintf("Resetting dhcpcd state in pod %s/%s on %s", pod.Namespace, pod.Name, iface))
	cmd := fmt.Sprintf(weaveRDMAFlushCmdFmt, iface)
	if out, err := netshoot.ExecInPodOnce(restClient, restCfg, pod.Namespace, pod.Name,
		[]string{"sh", "-c", cmd}); err != nil {
		By(fmt.Sprintf("Best-effort dhcpcd cleanup in pod %s/%s on %s failed: %v, output: %s", pod.Namespace, pod.Name, iface, err, out))
	}
}

// acquireDHCPLeaseInPod resets dhcpcd state, runs dhcpcd -L -1 -4, and verifies expectedIP is on iface.
func acquireDHCPLeaseInPod(restClient *rest.RESTClient, restCfg *rest.Config, pod *corev1.Pod, iface, expectedIP string) {
	releaseDHCPLeaseInPod(restClient, restCfg, pod, iface)

	By(fmt.Sprintf("Running dhcpcd -L -1 -4 %s in pod %s/%s", iface, pod.Namespace, pod.Name))
	out, err := netshoot.ExecInPodOnce(restClient, restCfg, pod.Namespace, pod.Name,
		[]string{"dhcpcd", "-L", "-1", "-4", iface})
	Expect(err).ToNot(HaveOccurred(), "dhcpcd in pod %s/%s on %s failed: %s", pod.Namespace, pod.Name, iface, out)

	By(fmt.Sprintf("Verifying %s holds expected IP %s in pod %s/%s", iface, expectedIP, pod.Namespace, pod.Name))
	addrOut, addrErr := netshoot.ExecInPodOnce(restClient, restCfg, pod.Namespace, pod.Name,
		[]string{"ip", "-4", "-o", "addr", "show", "dev", iface})
	Expect(addrErr).ToNot(HaveOccurred(), "ip addr show in pod %s/%s on %s failed: %s", pod.Namespace, pod.Name, iface, addrOut)
	Expect(addrOut).To(ContainSubstring(expectedIP),
		"expected IP %s not present in pod %s/%s on %s; got: %s", expectedIP, pod.Namespace, pod.Name, iface, addrOut)
}

// runIBWriteBWPodToPod runs ib_write_bw between two pods and asserts the BW threshold. extraArgs forwards to both sides.
func runIBWriteBWPodToPod(restClient *rest.RESTClient, restCfg *rest.Config, serverPod, clientPod *corev1.Pod, dev, serverIP string, extraArgs ...string) {
	// Joined as-is into the sh -c command. Safe only for plain shell flags (e.g. "--reversed").
	extra := strings.Join(extraArgs, " ")
	durationSec := int(weaveIBWriteBWDuration / time.Second)

	By(fmt.Sprintf("Starting ib_write_bw server in pod %s/%s (dev=%s, -D=%s, extra=%v)",
		serverPod.Namespace, serverPod.Name, dev, weaveIBWriteBWDuration, extraArgs))
	// Flow: pre-kill any stale ib_write_bw from a prior failed run (port :18515 would otherwise be busy), then
	// detach via nohup + bg + redirected stdio so the kubectl exec returns immediately while the server keeps
	// running for -D seconds. The defer below pkill's again as a belt-and-braces cleanup if the spec fails.
	serverCmd := fmt.Sprintf(
		"pkill -9 ib_write_bw 2>/dev/null; nohup ib_write_bw -d %s -D %d -q 1 -m 4096 --report_gbit %s >/dev/null 2>&1 < /dev/null &",
		dev, durationSec, extra)
	out, err := netshoot.ExecInPodOnce(restClient, restCfg, serverPod.Namespace, serverPod.Name,
		[]string{"sh", "-c", serverCmd})
	Expect(err).ToNot(HaveOccurred(), "starting ib_write_bw server in pod %s/%s failed: %s", serverPod.Namespace, serverPod.Name, out)
	// Wide pkill -9 is safe: the netutils pod is dedicated to this test and runs no other ib_write_bw process,
	// so we can't kill an unrelated workload. SIGKILL (not SIGTERM) is fine since ib_write_bw has no on-exit
	// state to clean up. `|| true` makes the no-process-found case a no-op (e.g. server already exited).
	defer func() {
		if killOut, killErr := netshoot.ExecInPodOnce(restClient, restCfg, serverPod.Namespace, serverPod.Name,
			[]string{"sh", "-c", "pkill -9 ib_write_bw 2>/dev/null || true"}); killErr != nil {
			By(fmt.Sprintf("Best-effort pkill ib_write_bw in pod %s/%s failed: %v, output: %s", serverPod.Namespace, serverPod.Name, killErr, killOut))
		}
	}()

	By(fmt.Sprintf("Waiting for ib_write_bw to listen on :%d in pod %s/%s", weaveIBWriteBWPort, serverPod.Namespace, serverPod.Name))
	listenCmd := fmt.Sprintf("ss -lnt 'sport = :%d' | grep -q LISTEN", weaveIBWriteBWPort)
	Eventually(func(g Gomega) {
		listenOut, listenErr := netshoot.ExecInPodOnce(restClient, restCfg, serverPod.Namespace, serverPod.Name,
			[]string{"sh", "-c", listenCmd})
		g.Expect(listenErr).ToNot(HaveOccurred(), "ib_write_bw not listening on :%d: %s", weaveIBWriteBWPort, listenOut)
	}).WithTimeout(30 * time.Second).WithPolling(weaveEventuallyPollInterval).Should(Succeed())

	By(fmt.Sprintf("Running ib_write_bw client in pod %s/%s -> %s (dev=%s, -D=%s, extra=%v)",
		clientPod.Namespace, clientPod.Name, serverIP, dev, weaveIBWriteBWDuration, extraArgs))
	clientCmd := fmt.Sprintf(
		"rm -f %s; ib_write_bw -d %s -D %d -q 1 -m 4096 --report_gbit --out_json --out_json_file=%s %s %s",
		weaveIBWriteBWClientJSONPath, dev, durationSec, weaveIBWriteBWClientJSONPath, extra, serverIP)
	cliOut, cliErr := netshoot.ExecInPodOnce(restClient, restCfg, clientPod.Namespace, clientPod.Name,
		[]string{"sh", "-c", clientCmd})
	Expect(cliErr).ToNot(HaveOccurred(), "ib_write_bw client in pod %s/%s failed: %s", clientPod.Namespace, clientPod.Name, cliOut)

	jsonOut, jsonErr := netshoot.ExecInPodOnce(restClient, restCfg, clientPod.Namespace, clientPod.Name,
		[]string{"cat", weaveIBWriteBWClientJSONPath})
	Expect(jsonErr).ToNot(HaveOccurred(), "reading ib_write_bw client JSON in pod %s/%s failed: %s", clientPod.Namespace, clientPod.Name, jsonOut)

	netshoot.AnalyzeIBWriteBWResult(jsonOut, weaveRDMAMinAvgBWGbit)
}
