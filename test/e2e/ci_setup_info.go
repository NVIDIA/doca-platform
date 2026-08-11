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
	"os"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// setupInfoEntry is one serial's lab metadata from $E2E_CI_SETUP_INFO_PATH.
type setupInfoEntry struct {
	// HostBMCIP is the host BMC IP address of the DPU.
	HostBMCIP string `json:"host-bmc-ip"`
	// DPUDeviceValues is the map of DPUDevice values to be applied to the DPUDevice.
	DPUDeviceValues map[string]any `json:"dpu-device-values,omitempty"`
}

// ciSetupInfo is the loaded lab setup-info document keyed by normalized DPU serial.
type ciSetupInfo struct {
	// path is the filesystem path to the lab DPU-serial -> CI setup-info YAML.
	path string
	// bySerial is the map of normalized DPU serial to setup-info entry.
	bySerial map[string]setupInfoEntry
}

// normalizeSetupInfoSerial lower-cases and trims a DPU serial so setup-info keys.
func normalizeSetupInfoSerial(serial string) string {
	return strings.ToLower(strings.TrimSpace(serial))
}

// loadCISetupInfo reads and normalizes the lab setup-info YAML keyed by
// DPU serial.
func loadCISetupInfo(path string) *ciSetupInfo {
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(),
		"reading CI setup info at %s (set $E2E_CI_SETUP_INFO_PATH to a valid path)",
		path)

	parsed := map[string]setupInfoEntry{}
	Expect(yaml.Unmarshal(raw, &parsed)).To(Succeed(),
		"parsing %s", path)

	bySerial := make(map[string]setupInfoEntry, len(parsed))
	for serial, entry := range parsed {
		key := normalizeSetupInfoSerial(serial)
		Expect(key).NotTo(BeEmpty(), "setup-info serial key must not be empty in %s", path)
		Expect(bySerial).NotTo(HaveKey(key),
			"duplicate setup-info serial %q after case normalization in %s", serial, path)
		entry.HostBMCIP = strings.TrimSpace(entry.HostBMCIP)
		Expect(entry.HostBMCIP).NotTo(BeEmpty(),
			"serial %q missing required host-bmc-ip in %s", serial, path)
		bySerial[key] = entry
	}
	return &ciSetupInfo{path: path, bySerial: bySerial}
}

// GetDPUDeviceValuesForDPUDevice looks up dpu-device-values by the device's
// Spec.SerialNumber. The values live in setup-info, not on the DPUDevice object.
func (s *ciSetupInfo) GetDPUDeviceValuesForDPUDevice(dpuDevice *provisioningv1.DPUDevice) map[string]any {
	serial := normalizeSetupInfoSerial(dpuDevice.Spec.SerialNumber)
	entry, ok := s.bySerial[serial]
	Expect(ok).To(BeTrue(),
		"DPUDevice %q serial %q has no entry in %s; add the DPU serial there",
		dpuDevice.Name, serial, s.path)
	return entry.DPUDeviceValues
}

// GetHostBMCIPForDPUNode looks up the host BMC IP by resolving the first
// DPUDevice in node.Spec.DPUs and using its Status.SerialNumber. The IP lives
// in setup-info, not on the DPUNode.
func (s *ciSetupInfo) GetHostBMCIPForDPUNode(ctx context.Context, c client.Client, node *provisioningv1.DPUNode) string {
	Expect(node.Spec.DPUs).NotTo(BeEmpty(),
		"DPUNode %q has no Spec.DPUs entries to resolve a DPUDevice", node.Name)

	deviceName := node.Spec.DPUs[0].Name
	device := &provisioningv1.DPUDevice{}
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: deviceName}, device)).To(Succeed(),
			"getting DPUDevice %q for DPUNode %q", deviceName, node.Name)
		g.Expect(device.Status.SerialNumber).NotTo(BeNil(),
			"DPUDevice %q Status.SerialNumber not yet set", deviceName)
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	serial := normalizeSetupInfoSerial(*device.Status.SerialNumber)
	entry, ok := s.bySerial[serial]
	Expect(ok).To(BeTrue(),
		"DPUNode %q (DPUDevice %q status serial %q) has no entry in %s; add the DPU serial there",
		node.Name, deviceName, serial, s.path)
	return entry.HostBMCIP
}
