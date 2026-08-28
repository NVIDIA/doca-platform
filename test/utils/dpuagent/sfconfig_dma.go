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

// Package dpuagent holds e2e validations for the DPU Agent operations that run
// inside the DPU (internal/provisioning/dpuagent/operations/...). The
// validations observe the resulting DPU OS state from a pod on the DPU cluster.
package dpuagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultDMASFNum is the sfnum the DPU Agent uses for the SNAP DMA SF when
	// the DPUFlavor leaves scalableFunctions has no dma entry with sfNumStart set. It mirrors
	// snapDMASFNum in internal/provisioning/dpuagent/operations/sfconfig.
	DefaultDMASFNum = 8000

	// dmaSFPodPrefix is the name prefix of the per-DPU-node netutils pods used to
	// inspect the DMA SF from inside the DPU.
	dmaSFPodPrefix = "netutils-dma-sf"

	dmaSFPodReadyTimeout = 2 * time.Minute
	dmaSFCheckTimeout    = 5 * time.Minute
	dmaSFPollInterval    = 1 * time.Second
)

// DMAScalableFunctionInput carries everything ValidateDMAScalableFunction needs
// from the e2e suite: the flavor under test and access to the DPU cluster the
// provisioned DPUs joined.
type DMAScalableFunctionInput struct {
	// DPUFlavor is the flavor the DPUs were provisioned with; its
	// spec.scalableFunctions' dma entry decides whether the validation applies at all.
	DPUFlavor *provisioningv1.DPUFlavor

	// ClusterClient targets the DPU cluster via a refreshable wrapper, so it
	// stays valid across tunnel restarts on its own.
	ClusterClient client.Client

	// RESTClient and RESTConfig target the DPU cluster over the tunneled REST
	// config. They are **rest.RESTClient/**rest.Config (pointers to the
	// suite's dpuClusterRestClient[i]/dpuClusterRestConfig[i] slice elements),
	// not plain pointers, because a background health-check can replace those
	// elements with a new client/port at any time if the tunnel drops. Code
	// that retries across time must dereference *RESTClient/*RESTConfig fresh
	// on every attempt instead of snapshotting them once - see
	// verifyDMASFRepresentorUp and the sysfs check in
	// ValidateDMAScalableFunction.
	RESTClient **rest.RESTClient
	RESTConfig **rest.Config

	// Namespace is the DPU cluster namespace the netutils pods are created in.
	Namespace string

	// NetutilsImage is the netutils image repository (no tag) and Tag the tag to
	// pull; ImagePullSecret is the pull secret available in Namespace.
	NetutilsImage   string
	Tag             string
	ImagePullSecret string

	// PodLabels are set on the netutils pods so suite-level cleanup can find them.
	PodLabels map[string]string

	// SkipCleanup leaves the netutils pods behind for post-mortem debugging.
	SkipCleanup bool
}

// dmaSFInspectScriptFmt walks the mlx5 auxiliary devices in the DPU's sysfs and
// prints one line per SF whose sfnum matches the expected DMA sfnum:
//
//	aux=<aux device> parent=<ECPF BDF> rdma=[<ibdevs>] netdev=[<netdevs>] parentrdma=[<ibdevs>]
//
// sysfs is host-wide inside the pod (hostNetwork is not what makes it visible,
// the /sys hostPath mount is), so this observes the real DPU OS state.
const dmaSFInspectScriptFmt = `
for d in /sys/bus/auxiliary/devices/mlx5_core.sf.*; do
  [ -r "$d/sfnum" ] || continue
  [ "$(cat "$d/sfnum")" = "%d" ] || continue
  rdma=$(ls "$d/infiniband" 2>/dev/null | tr '\n' ' ')
  netdev=$(ls "$d/net" 2>/dev/null | tr '\n' ' ')
  parent=$(basename "$(readlink -f "$d/..")")
  parentrdma=$(ls "$d/../infiniband" 2>/dev/null | tr '\n' ' ')
  echo "aux=$(basename "$d") parent=$parent rdma=[$rdma] netdev=[$netdev] parentrdma=[$parentrdma]"
done
`

// dmaSFObservation is one parsed line of dmaSFInspectScriptFmt output.
type dmaSFObservation struct {
	auxDev     string
	parentBDF  string
	rdmaDevs   []string
	netdevs    []string
	parentRDMA []string
}

// ValidateDMAScalableFunction verifies the SNAP DMA SF that the DPU Agent creates
// when the DPUFlavor sets a dma entry (BlueField-4
// socket-direct). It spins up a privileged hostNetwork netutils pod on every DPU
// cluster node, execs into it and asserts, from the DPU's own sysfs, that the SF
// with the expected sfnum exists, exposes an RDMA device and has no netdev of its
// own — the same "consumable by SNAP" contract verifyDMASFConsumable enforces
// agent-side, checked here end-to-end on real hardware.
//
// It skips when the flavor does not enable the DMA SF. It does not skip when the
// SF is missing: a flavor that enables the feature on a DPU that is not
// socket-direct is a setup error the suite should surface, not hide.
func ValidateDMAScalableFunction(ctx context.Context, input DMAScalableFunctionInput) {
	Expect(input.ClusterClient).NotTo(BeNil(), "DPUCluster client is not initialized")
	Expect(input.RESTClient).NotTo(BeNil(), "DPUCluster REST client is not initialized")
	Expect(*input.RESTClient).NotTo(BeNil(), "DPUCluster REST client is not initialized")

	sfNum, enabled := dmaSFNumFromFlavor(input.DPUFlavor)
	if !enabled {
		Skip("Skip DMA SF test as the DPUFlavor does not have a scalableFunctions dma entry")
	}

	dpuNodes := &corev1.NodeList{}
	Expect(input.ClusterClient.List(ctx, dpuNodes)).To(Succeed(), "listing DPU cluster nodes")
	Expect(dpuNodes.Items).NotTo(BeEmpty(), "expected at least one node in the DPU cluster")

	pods := make([]*corev1.Pod, len(dpuNodes.Items))
	for i := range dpuNodes.Items {
		pods[i] = createNetutilsSysfsPod(ctx, input,
			fmt.Sprintf("%s-%d-%s", dmaSFPodPrefix, i, utilrand.String(5)),
			dpuNodes.Items[i].Name)
	}

	for i := range dpuNodes.Items {
		nodeName := dpuNodes.Items[i].Name
		pod := pods[i]

		By(fmt.Sprintf("Verifying the DMA SF (sfnum %d) on DPU node %s", sfNum, nodeName))
		Eventually(func(g Gomega) {
			// Dereference RESTClient/RESTConfig fresh on every attempt: a tunnel restart
			// mid-Eventually replaces *input.RESTClient/*input.RESTConfig with a client
			// bound to a new local port, and a stale snapshot would keep dialing the dead one.
			out, err := netshoot.ExecInPodOnce(*input.RESTClient, *input.RESTConfig, pod.Namespace, pod.Name,
				[]string{"sh", "-c", fmt.Sprintf(dmaSFInspectScriptFmt, sfNum)})
			g.Expect(err).NotTo(HaveOccurred(), "inspecting sysfs in pod %s/%s failed: %s", pod.Namespace, pod.Name, out)

			observations := parseDMASFObservations(out)
			g.Expect(observations).To(HaveLen(1),
				"expected exactly one SF with sfnum %d on DPU node %s (the DMA SF is created on a single ECPF; "+
					"none means the flavor's scalableFunctions dma entry did not take effect or the DPU is not socket-direct), got: %s",
				sfNum, nodeName, out)

			sf := observations[0]
			g.Expect(sf.rdmaDevs).NotTo(BeEmpty(),
				"DMA SF %s on DPU node %s exposes no RDMA device; SNAP discovery would fail", sf.auxDev, nodeName)
			g.Expect(sf.netdevs).To(BeEmpty(),
				"DMA SF %s on DPU node %s still has netdev(s) %v; enable_eth=false did not take effect",
				sf.auxDev, nodeName, sf.netdevs)
			g.Expect(sf.parentRDMA).To(BeEmpty(),
				"DMA SF %s on DPU node %s sits on ECPF %s which exposes RDMA device(s) %v; it must be created on the "+
					"silenced, ibdev-less second-link ECPF", sf.auxDev, nodeName, sf.parentBDF, sf.parentRDMA)
		}).WithTimeout(dmaSFCheckTimeout).WithPolling(dmaSFPollInterval).Should(Succeed())

		verifyDMASFRepresentorUp(input, pod, nodeName, sfNum)
	}
}

// dmaSFNumFromFlavor returns the DMA sfnum the DPU Agent is expected to use for
// flavor, and whether the flavor enables the DMA SF at all.
func dmaSFNumFromFlavor(flavor *provisioningv1.DPUFlavor) (int, bool) {
	if flavor == nil {
		return 0, false
	}
	for _, sf := range flavor.Spec.ScalableFunctions {
		if sf.Type == provisioningv1.ScalableFunctionTypeDMA {
			return int(ptr.Deref(sf.SFNumStart, int32(DefaultDMASFNum))), true
		}
	}
	return 0, false
}

// verifyDMASFRepresentorUp checks the DMA SF's representor netdev, which lives in
// the DPU host network namespace (hence the hostNetwork pod). The agent brings it
// up best-effort on every reconcile and never attaches it to a bridge; absence is
// tolerated (setups may not expose one), a present-but-down representor is not.
func verifyDMASFRepresentorUp(input DMAScalableFunctionInput, pod *corev1.Pod, nodeName string, sfNum int) {
	By(fmt.Sprintf("Verifying the DMA SF representor on DPU node %s is up if present", nodeName))
	// grep 'sf%d:' anchors on the colon ip-link always appends after the ifname,
	// preventing a false match when sfNum is a numeric prefix of a longer sfnum
	// (e.g. sfNum=800 must not match en3f1pf0sf8000:).
	Eventually(func(g Gomega) {
		// Dereference fresh on every attempt; see the comment in ValidateDMAScalableFunction.
		out, err := netshoot.ExecInPodOnce(*input.RESTClient, *input.RESTConfig, pod.Namespace, pod.Name,
			[]string{"sh", "-c", fmt.Sprintf("ip -o link show | grep 'sf%d:' || true", sfNum)})
		g.Expect(err).NotTo(HaveOccurred(), "listing links in pod %s/%s failed: %s", pod.Namespace, pod.Name, out)

		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			g.Expect(line).To(ContainSubstring("UP"),
				"DMA SF representor on DPU node %s is not up: %s", nodeName, line)
		}
	}).WithTimeout(dmaSFCheckTimeout).WithPolling(dmaSFPollInterval).Should(Succeed())
}

// parseDMASFObservations parses the output of dmaSFInspectScriptFmt.
func parseDMASFObservations(out string) []dmaSFObservation {
	trimmed := strings.TrimSpace(out)
	observations := make([]dmaSFObservation, 0, strings.Count(trimmed, "\n")+1)
	for line := range strings.SplitSeq(trimmed, "\n") {
		if obs, ok := parseDMASFObservationLine(strings.TrimSpace(line)); ok {
			observations = append(observations, obs)
		}
	}
	return observations
}

// parseDMASFObservationLine parses one line of dmaSFInspectScriptFmt output,
// e.g. `aux=mlx5_core.sf.9 parent=0001:03:00.0 rdma=[mlx5_2] netdev=[] parentrdma=[]`.
// The rdma/netdev/parentrdma values are themselves space-separated lists, so the
// fields are cut in order rather than split on a shared delimiter.
func parseDMASFObservationLine(line string) (dmaSFObservation, bool) {
	rest, ok := strings.CutPrefix(line, "aux=")
	if !ok {
		return dmaSFObservation{}, false
	}
	auxDev, rest, ok := strings.Cut(rest, " parent=")
	if !ok {
		return dmaSFObservation{}, false
	}
	parentBDF, rest, ok := strings.Cut(rest, " rdma=[")
	if !ok {
		return dmaSFObservation{}, false
	}
	rdma, rest, ok := strings.Cut(rest, "] netdev=[")
	if !ok {
		return dmaSFObservation{}, false
	}
	netdev, rest, ok := strings.Cut(rest, "] parentrdma=[")
	if !ok {
		return dmaSFObservation{}, false
	}
	parentRDMA, ok := strings.CutSuffix(rest, "]")
	if !ok {
		return dmaSFObservation{}, false
	}
	return dmaSFObservation{
		auxDev:     auxDev,
		parentBDF:  parentBDF,
		rdmaDevs:   strings.Fields(rdma),
		netdevs:    strings.Fields(netdev),
		parentRDMA: strings.Fields(parentRDMA),
	}, true
}

// createNetutilsSysfsPod creates a privileged hostNetwork netutils pod with the
// host sysfs mounted on nodeName, waits for it to be Ready and registers its
// deletion. Used to observe DPU-side device state from inside the DPU cluster.
func createNetutilsSysfsPod(ctx context.Context, input DMAScalableFunctionInput, podName, nodeName string) *corev1.Pod {
	By(fmt.Sprintf("Creating netutils pod %s/%s on DPU node %s", input.Namespace, podName, nodeName))
	pod := netshoot.NewNetutilsHostPod(podName, input.Namespace, nodeName, fmt.Sprintf("%s:%s", input.NetutilsImage, input.Tag)).
		WithLabels(input.PodLabels).
		WithImagePullSecret(input.ImagePullSecret).
		WithSysMount().
		WithTerminationGracePeriod(0).
		Build()
	pod = netshoot.CreateNetutilsHostPod(ctx, input.ClusterClient, pod)

	DeferCleanup(func(ctx context.Context) {
		if input.SkipCleanup {
			return
		}
		By(fmt.Sprintf("Deleting netutils pod %s/%s", input.Namespace, podName))
		err := input.ClusterClient.Delete(ctx, pod)
		Expect(client.IgnoreNotFound(err)).To(Succeed(), "deleting pod %s/%s", input.Namespace, podName)
	})

	netshoot.WaitForNetutilsPodReady(ctx, input.ClusterClient, pod, dmaSFPodReadyTimeout)

	return pod
}
