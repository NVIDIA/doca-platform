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

// Package sriovconfig creates the SR-IOV functions a DPUFlavor declares: the
// Scalable Functions of spec.scalableFunctions (plus the DMA SF of spec.dma) and the
// Virtual Functions of spec.virtualFunctions. Each kind is a separate operation —
// ReconcileSF and ReconcileVF — so that a failure names the kind that failed on the
// DPU condition. Both follow the same three stages:
//
//	plan      resolve the flavor's groups against the discovered ports; no side effects
//	reconcile create the functions and apply their settings; the only stage that mutates
//	verify    read the applied state back; must not configure anything
//
// The two operations share the SR-IOV device-plugin configuration, which is a single
// file listing the pools of both kinds. Each operation saves its resolved plan on the
// operations context and rewrites the whole file, so the file is correct after either
// operation and in every skip combination.
//
// ReconcileSF must be executed before ReconcileVF: it sets the eSwitch to switchdev on
// hostless DPUs, which SF creation requires, and leaves the port in the mode VF
// creation expects.
package sriovconfig

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/utils/ptr"
)

// defaultRootFS is the filesystem root the agent reads sysfs and writes config under.
const defaultRootFS = "/"

// runBashFunc runs a shell command, returning its stdout and stderr.
type runBashFunc func(cmd string) (bytes.Buffer, bytes.Buffer, error)

// sriovState holds the resolved execution plans shared by ReconcileSF and ReconcileVF.
// These plans are used to generate the SR-IOV device-plugin configuration.
// If a plan is nil, its operation did not run (because it was skipped or failed early)
// and therefore contributes no resource pools.
type sriovState struct {
	sf *sfPlan
	vf *vfPlan
}

// sharedState returns the state both SR-IOV operations contribute to, creating it on
// first use. It lives on the operations context.
func sharedState(optCtx *operations.Context) *sriovState {
	state, ok := optCtx.SRIOVState.(*sriovState)
	if !ok {
		state = &sriovState{}
		optCtx.SRIOVState = state
	}
	return state
}

// selectPorts matches a group's device against discovered ports. Unset or "*" is
// every port; "pN" is a netdev; anything else is a PCI address.
func selectPorts(ports []pciutil.NICPort, device *string) []pciutil.NICPort {
	selector := strings.TrimSpace(ptr.Deref(device, "*"))
	if selector == "" || selector == "*" {
		return ports
	}
	var out []pciutil.NICPort
	for _, port := range ports {
		if strings.EqualFold(port.Netdev, selector) ||
			pciutil.NormalizeAddress(port.PCIAddress) == pciutil.NormalizeAddress(selector) {
			out = append(out, port)
		}
	}
	return out
}

// selectGroupPorts filters the discovered ports for a specific group using selectPorts.
// It only returns an error if the group actually requests functions (count > 0) but its
// selector matches absolutely nothing. If the group's count is 0, matching nothing is
// fine and is not treated as an error.
func selectGroupPorts(ports []pciutil.NICPort, device *string, count int32, field string, index int) ([]pciutil.NICPort, error) {
	selected := selectPorts(ports, device)
	if count > 0 && len(selected) == 0 {
		return nil, fmt.Errorf("%s[%d]: device %q matches no discovered port", field, index, ptr.Deref(device, "*"))
	}
	return selected, nil
}

// deviceCursor assigns function numbers per device from 0, skipping numbers reserved
// up front. Used for both sfnum and VF index.
type deviceCursor struct {
	next     map[string]int
	reserved map[string]map[int]bool
}

func newDeviceCursor() *deviceCursor {
	return &deviceCursor{next: map[string]int{}, reserved: map[string]map[int]bool{}}
}

// reserve claims num on a device, reporting false when another group already pinned it.
func (c *deviceCursor) reserve(device string, num int) bool {
	if c.reserved[device] == nil {
		c.reserved[device] = map[int]bool{}
	}
	if c.reserved[device][num] {
		return false
	}
	c.reserved[device][num] = true
	return true
}

// take returns the next n free numbers on a device.
func (c *deviceCursor) take(device string, n int) []int {
	nums := make([]int, 0, n)
	next := c.next[device]
	for range n {
		for c.reserved[device][next] {
			next++
		}
		nums = append(nums, next)
		next++
	}
	c.next[device] = next
	return nums
}

// canonicalMAC returns a colon-separated 48-bit Ethernet MAC. net.ParseMAC also
// accepts EUI-64 and other separators, which mlnx-sf and `ip link set vf mac` do not.
func canonicalMAC(value string) (string, error) {
	hw, err := net.ParseMAC(value)
	if err != nil {
		return "", fmt.Errorf("invalid MAC address %q: %w", value, err)
	}
	if len(hw) != 6 {
		return "", fmt.Errorf("invalid MAC address %q: must be a 48-bit Ethernet MAC", value)
	}
	return hw.String(), nil
}

func readIntFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}
