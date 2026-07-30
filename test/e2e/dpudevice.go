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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func waitForDPUDevicesWithPCIAddress(
	ctx context.Context,
	c client.Client,
	namespace string,
	nodeName string,
) []provisioningv1.DPUDevice {
	// Nodes are waited for one after the other, so name the node to make a stalled
	// node obvious in the test output.
	By(fmt.Sprintf("Waiting for the DPUDevice inventory of DPU node %s", nodeName))

	var discovered []provisioningv1.DPUDevice
	Eventually(func(g Gomega) {
		dpuNode := &provisioningv1.DPUNode{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: nodeName}, dpuNode)).To(Succeed())
		g.Expect(dpuNode.DeletionTimestamp.IsZero()).To(BeTrue(), "DPUNode %s is being deleted", nodeName)
		g.Expect(dpuNode.Spec.DPUs).NotTo(BeEmpty(), "waiting for DPUNode %s device inventory", nodeName)

		dpuDevices := &provisioningv1.DPUDeviceList{}
		g.Expect(c.List(ctx, dpuDevices,
			client.InNamespace(namespace),
			client.MatchingLabels{provisioningv1.DPUNodeNameLabel: nodeName},
		)).To(Succeed())

		// Comparing PCI addresses across DPU nodes is only meaningful once every
		// DPUDevice attached to the node is discovered and labeled.
		g.Expect(dpuDevices.Items).To(HaveLen(len(dpuNode.Spec.DPUs)),
			"waiting for every DPUDevice of DPUNode %s", nodeName)
		for _, dpuDevice := range dpuDevices.Items {
			g.Expect(dpuDevice.Labels).To(HaveKeyWithValue(util.DPUDevicePCIAddressLabel, Not(BeEmpty())),
				"waiting for the PCI address label on DPUDevice %s", dpuDevice.Name)
		}

		discovered = dpuDevices.Items
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())

	return discovered
}

func selectDPUDeviceWithPCIAddress(dpuDevices []provisioningv1.DPUDevice) (provisioningv1.DPUDevice, error) {
	candidates := make([]provisioningv1.DPUDevice, 0, len(dpuDevices))
	for _, dpuDevice := range dpuDevices {
		if dpuDevice.Labels[util.DPUDevicePCIAddressLabel] != "" {
			candidates = append(candidates, dpuDevice)
		}
	}
	if len(candidates) == 0 {
		return provisioningv1.DPUDevice{}, fmt.Errorf("no DPUDevice has a PCI address label")
	}

	slices.SortFunc(candidates, func(a, b provisioningv1.DPUDevice) int {
		if byPCIAddress := cmp.Compare(
			strings.ToLower(a.Labels[util.DPUDevicePCIAddressLabel]),
			strings.ToLower(b.Labels[util.DPUDevicePCIAddressLabel]),
		); byPCIAddress != 0 {
			return byPCIAddress
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return candidates[0], nil
}

// sharedDPUDevicePCIAddress returns the lowest PCI address exposed by a DPUDevice
// on every DPU node, so a single DPUSet selector selects one DPUDevice per node.
func sharedDPUDevicePCIAddress(dpuDevicesByNode map[string][]provisioningv1.DPUDevice) (string, error) {
	if len(dpuDevicesByNode) == 0 {
		return "", fmt.Errorf("no DPU nodes provided")
	}

	nodeNames := make([]string, 0, len(dpuDevicesByNode))
	for nodeName := range dpuDevicesByNode {
		nodeNames = append(nodeNames, nodeName)
	}
	slices.Sort(nodeNames)

	// Addresses are matched byte for byte on purpose. The result becomes a label
	// selector that has to match on every node, and one selector cannot cover two
	// spellings of the same address, so nodes labeled with different casing have to
	// fail here with both inventories reported rather than select a subset of nodes.
	shared := pciAddresses(dpuDevicesByNode[nodeNames[0]])
	for _, nodeName := range nodeNames[1:] {
		addresses := pciAddresses(dpuDevicesByNode[nodeName])
		shared = slices.DeleteFunc(shared, func(address string) bool {
			return !slices.Contains(addresses, address)
		})
	}
	if len(shared) == 0 {
		inventories := make([]string, 0, len(nodeNames))
		for _, nodeName := range nodeNames {
			inventories = append(inventories,
				fmt.Sprintf("%s=[%s]", nodeName, strings.Join(pciAddresses(dpuDevicesByNode[nodeName]), ", ")))
		}
		return "", fmt.Errorf("no DPUDevice PCI address shared by DPU nodes: %s",
			strings.Join(inventories, ", "))
	}
	return shared[0], nil
}

// pciAddresses returns the PCI addresses labeled on the DPUDevices, ordered
// case-insensitively like selectDPUDeviceWithPCIAddress so that the lowest address
// is picked the same way in both flows. The values themselves stay verbatim because
// callers turn them into a label selector.
func pciAddresses(dpuDevices []provisioningv1.DPUDevice) []string {
	addresses := make([]string, 0, len(dpuDevices))
	for _, dpuDevice := range dpuDevices {
		if address := dpuDevice.Labels[util.DPUDevicePCIAddressLabel]; address != "" {
			addresses = append(addresses, address)
		}
	}
	slices.SortFunc(addresses, func(a, b string) int {
		if byPCIAddress := cmp.Compare(strings.ToLower(a), strings.ToLower(b)); byPCIAddress != 0 {
			return byPCIAddress
		}
		return cmp.Compare(a, b)
	})
	return slices.Compact(addresses)
}

func resolveDPUSetDPUDevicePCISelector(
	ctx context.Context,
	c client.Client,
	dpuSet *provisioningv1.DPUSet,
	expectedDPUNodeCount int,
	expectedDPUsPerNode int,
) {
	Expect(expectedDPUsPerNode).To(Equal(1),
		"dynamic DPUDevice selection currently supports one DPU per node")

	pciAddress := discoverSharedDPUDevicePCIAddress(
		ctx,
		c,
		dpuSet.Namespace,
		dpuSet.Spec.DPUNodeSelector,
		expectedDPUNodeCount,
	)
	dpuSet.Spec.DPUDeviceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{
		util.DPUDevicePCIAddressLabel: pciAddress,
	}}
	//nolint:staticcheck // Clear the deprecated field to honor the selector XOR contract.
	dpuSet.Spec.DPUSelector = nil
}

func resolveDPUDeploymentDPUDevicePCISelectors(
	ctx context.Context,
	c client.Client,
	dpuDeployment *dpuservicev1.DPUDeployment,
	dpuNodeSelector *metav1.LabelSelector,
	expectedDPUNodeCount int,
	expectedDPUsPerNode int,
) {
	Expect(expectedDPUsPerNode).To(Equal(1),
		"dynamic DPUDevice selection currently supports one DPU per node")
	Expect(dpuDeployment.Spec.DPUs.DPUSets).NotTo(BeEmpty(),
		"dynamic DPUDevice selection requires at least one DPUSet")

	pciAddress := discoverSharedDPUDevicePCIAddress(
		ctx,
		c,
		dpuDeployment.Namespace,
		dpuNodeSelector,
		expectedDPUNodeCount,
	)
	for i := range dpuDeployment.Spec.DPUs.DPUSets {
		dpuSet := &dpuDeployment.Spec.DPUs.DPUSets[i]
		dpuSet.DPUDeviceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{
			util.DPUDevicePCIAddressLabel: pciAddress,
		}}
		//nolint:staticcheck // Clear the deprecated field to honor the selector XOR contract.
		dpuSet.DPUSelector = nil
	}
}

// discoverSharedDPUDevicePCIAddress returns a PCI address that a DPUDevice exposes on
// every selected DPU node, so one DPUSet selector selects one DPUDevice per node.
//
// The PCI address is only a valid key while the DPUFlavor leaves the bus alone. A flavor
// that sets PCI_SWITCH_EMULATION_ENABLE renumbers the bus during provisioning, DPF
// relabels the DPUDevice with the new address, and the DPUSet then deletes the DPU it is
// provisioning because the selector stops matching. A lane running such a flavor has to
// select on an identity that does not move, the way the SNAP lane labels the DPUDevice it
// picks.
func discoverSharedDPUDevicePCIAddress(
	ctx context.Context,
	c client.Client,
	namespace string,
	dpuNodeSelector *metav1.LabelSelector,
	expectedDPUNodeCount int,
) string {
	nodeSelector, err := utils.LabelSelectorAsSelector(dpuNodeSelector)
	Expect(err).NotTo(HaveOccurred())

	var dpuNodeNames []string
	Eventually(func(g Gomega) {
		dpuNodes := &provisioningv1.DPUNodeList{}
		g.Expect(c.List(ctx, dpuNodes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: nodeSelector},
		)).To(Succeed())

		names := make([]string, 0, len(dpuNodes.Items))
		for _, dpuNode := range dpuNodes.Items {
			if !dpuNode.DeletionTimestamp.IsZero() {
				continue
			}
			names = append(names, dpuNode.Name)
		}
		g.Expect(names).To(HaveLen(expectedDPUNodeCount))
		dpuNodeNames = names
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
	slices.Sort(dpuNodeNames)

	dpuDevicesByNode := make(map[string][]provisioningv1.DPUDevice, len(dpuNodeNames))
	for _, nodeName := range dpuNodeNames {
		dpuDevicesByNode[nodeName] = waitForDPUDevicesWithPCIAddress(ctx, c, namespace, nodeName)
	}

	pciAddress, err := sharedDPUDevicePCIAddress(dpuDevicesByNode)
	Expect(err).NotTo(HaveOccurred())
	By(fmt.Sprintf("Selecting DPUDevice PCI address %s shared by DPU nodes %s",
		pciAddress, strings.Join(dpuNodeNames, ", ")))
	return pciAddress
}
