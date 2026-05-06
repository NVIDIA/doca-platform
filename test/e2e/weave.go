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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
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

	// DPU-side PCI addresses (underscored form used in OVS bridge names) for the two NIC ports.
	// Pinned by the dpf-bootstrap deployment, see DPUService-weave-flow-controller.yml.
	// If the underlay config in the DPUService changes, these need to follow.
	weaveDPUPortP0PCIUnderscored = "0000_03_00_0"
	weaveDPUPortP1PCIUnderscored = "0000_03_00_1"

	weavePFMTU = 9000

	// weaveVNetSubnet is the /8 overlay IPv4 subnet used for all Weave virtual networks.
	weaveVNetSubnet = "10.0.0.0/8"

	// weaveDPUTunnelCleanupTimeout is for vpcctl deletes over the tunneled DPU REST client. When the tunnel
	// drops, getDPUClusterClient's inner Eventually can take up to 3m (system_setup.go); a shorter timeout
	// causes flaky failures (e.g. "connection reset by peer" on pod exec) attributed to the last It block.
	weaveDPUTunnelCleanupTimeout = 4 * time.Minute

	// weaveOperationTimeout is the default ceiling for vpcctl and DPU pod exec Eventually loops (create,
	// wait, verify). Tunnel and API variance affect these the same way; one value avoids brittle 30s/60s/2m splits.
	weaveOperationTimeout = 2 * time.Minute

	weaveEventuallyPollInterval = 1 * time.Second
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
	dhcpDaemonSet := &appsv1.DaemonSet{}
	dhcpObj := unstructuredFromFile(conf.DHCPDaemonSetPath)
	Expect(machineryruntime.DefaultUnstructuredConverter.FromUnstructured(dhcpObj.Object, dhcpDaemonSet)).To(Succeed())
	t.dhcpDaemonSet = dhcpDaemonSet
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
func createVNetOnPod(pod *corev1.Pod, vnetID string, vni uint32) {
	By(fmt.Sprintf("Creating virtual network %q (vni=%d, subnet=%s) on pod %s", vnetID, vni, weaveVNetSubnet, pod.Name))
	cmd := []string{
		"/vpcctl", "create-vnet",
		"--id", vnetID,
		"--vni", fmt.Sprintf("%d", vni),
		"--subnet-v4", weaveVNetSubnet,
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

// verifyIsolationBridgeExists asserts that the OVS isolation bridge for the given VNI
// on the given DPU port (br-isol-<vni>-<pci_underscored>) is present on the flow-controller pod.
func verifyIsolationBridgeExists(pod *corev1.Pod, vni uint32, dpuPort string) {
	pci, ok := dpuPortToPCIUnderscored[dpuPort]
	Expect(ok).To(BeTrue(), "unknown DPU port %q", dpuPort)
	bridge := fmt.Sprintf("br-isol-%d-%s", vni, pci)
	By(fmt.Sprintf("Verifying bridge %s exists on pod %s", bridge, pod.Name))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name,
			[]string{"ovs-vsctl", "list", "bridge", bridge})
		g.Expect(err).ToNot(HaveOccurred(), "bridge %s not found on pod %s: %s", bridge, pod.Name, out)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
}

// ensureOverlayRoute ensures the route for weaveVNetSubnet on a netshoot pod uses the DHCP
// gateway rather than being on-link. The CNI DHCP plugin sometimes fails to apply
// option 121 classless static routes correctly. overlayIP is the pod's known overlay
// address (from createPFAttachmentAndWaitForHostIP); for a /31 the gateway is the peer.
//
// restClient and restCfg must be for the host (management) cluster where netshoot pods run.
func ensureOverlayRoute(restClient *rest.RESTClient, restCfg *rest.Config, namespace, podName, overlayIP string) {
	const overlayIface = "net1"
	ip := net.ParseIP(overlayIP).To4()
	Expect(ip).ToNot(BeNil(), "invalid overlay IP %s for pod %s", overlayIP, podName)
	gateway := net.IPv4(ip[0], ip[1], ip[2], ip[3]^1).String()

	Eventually(func(g Gomega) {
		linkOut, linkErr := netshoot.ExecInPodOnce(restClient, restCfg, namespace, podName,
			[]string{"ip", "link", "set", overlayIface, "up"})
		g.Expect(linkErr).ToNot(HaveOccurred(), "failed to bring up %s on pod %s: %s", overlayIface, podName, linkOut)
		output, err := netshoot.ExecInPodOnce(restClient, restCfg, namespace, podName,
			[]string{"ip", "route", "replace", weaveVNetSubnet, "via", gateway, "dev", overlayIface})
		g.Expect(err).ToNot(HaveOccurred(), "failed to set overlay route on pod %s: %s", podName, output)
	}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed(),
		"overlay route on pod %s not set after timeout", podName)
	By(fmt.Sprintf("Overlay route on pod %s: %s via %s", podName, weaveVNetSubnet, gateway))
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
