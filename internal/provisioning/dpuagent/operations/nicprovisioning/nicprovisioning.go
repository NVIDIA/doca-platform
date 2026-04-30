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

package nicprovisioning

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	utils "github.com/nvidia/doca-platform/internal/utils"

	nicconfigurationv1alpha1 "github.com/Mellanox/nic-configuration-operator/api/v1alpha1"
	nicdevicediscovery "github.com/Mellanox/nic-configuration-operator/pkg/devicediscovery"
	nicdms "github.com/Mellanox/nic-configuration-operator/pkg/dms"
	nicnvconfig "github.com/Mellanox/nic-configuration-operator/pkg/nvconfig"
	"k8s.io/klog/v2"
)

var nicFirmwareDir = string(os.PathSeparator) + "nic-firmware"

// NICProvisioning performs NIC-related provisioning steps before the rest of the
// DPU agent pipeline (modules, netplan, etc.).
type NICProvisioning struct {
	dmsServer               nicdms.DMSServer
	runBash                 func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	prepareLocalDMSServerFn func(optCtx *operations.Context) error
}

func (n *NICProvisioning) Name() string {
	return "NIC provisioning"
}

func (n *NICProvisioning) ConditionType() string {
	return "NICProvisioning"
}

func (n *NICProvisioning) ShouldSkip(optCtx *operations.Context) bool {
	if optCtx.LatestDPU == nil {
		klog.Error("Latest DPU not retrieved, will return error during execution. (this should never happen)")
		return false
	}
	return !optCtx.Options.AstraEnabled || optCtx.Options.SkipAstra
}

func (n *NICProvisioning) ShouldUpdateStatusBeforeContinue(_ *operations.Context) bool {
	return false
}

func (n *NICProvisioning) Execute(execCtx context.Context, optCtx *operations.Context) (err error) {
	isBlueField4 := optCtx.LatestDPU != nil && optCtx.LatestDPU.Status.DPUType == provisioningv1.DPUTypeBlueField4
	if !isBlueField4 {
		return fmt.Errorf("DPU type is not BlueField4")
	}
	if optCtx.Options.BFBRegistryURL == "" {
		return fmt.Errorf("BFB registry URL is not set")
	}
	klog.InfoS("NIC provisioning", "dpu", optCtx.Options.DPUName, "namespace", optCtx.Options.DPUNamespace,
		"bfbRegistryURL", optCtx.Options.BFBRegistryURL)
	// 1. Download Astra NIC firmware from bfb-registry
	if err := n.downloadNICFirmware(execCtx, optCtx); err != nil {
		return err
	}
	// 2. Stop host dmsd service (if present) and start local DMS server from NCO library.
	prepareDMSServer := n.prepareLocalDMSServer
	if n.prepareLocalDMSServerFn != nil {
		prepareDMSServer = n.prepareLocalDMSServerFn
	}
	if err := prepareDMSServer(optCtx); err != nil {
		return err
	}
	if n.dmsServer != nil && n.dmsServer.IsRunning() {
		defer func() {
			stopErr := n.stopLocalDMSServer()
			if stopErr == nil {
				return
			}
			if err == nil {
				err = stopErr
				return
			}
			klog.ErrorS(stopErr, "NIC provisioning: failed to stop local DMS server while unwinding execute")
		}()
	}
	return nil
}

// downloadNICFirmware resolves and downloads Astra NIC firmware from bfb-registry
// to the local nic-firmware directory if it is not already cached.
func (n *NICProvisioning) downloadNICFirmware(execCtx context.Context, optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil {
		return fmt.Errorf("latest DPU object is required for NIC provisioning")
	}
	if optCtx.Client == nil {
		return fmt.Errorf("client is required for NIC provisioning")
	}

	blueFieldSoftwareName := strings.TrimSpace(optCtx.LatestDPU.Spec.BlueFieldSoftware)
	if blueFieldSoftwareName == "" {
		return fmt.Errorf("dpu %s/%s does not reference a BlueFieldSoftware", optCtx.Options.DPUNamespace, optCtx.Options.DPUName)
	}

	blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
	if err := optCtx.Client.GetObject(execCtx, optCtx.Options.DPUNamespace, blueFieldSoftwareName, blueFieldSoftware); err != nil {
		return fmt.Errorf("failed to get BlueFieldSoftware %s/%s: %w", optCtx.Options.DPUNamespace, blueFieldSoftwareName, err)
	}

	nicFWLocation := strings.TrimSpace(blueFieldSoftware.Status.DownloadedComponents.AstraNicFw)
	if nicFWLocation == "" {
		return fmt.Errorf("blueFieldSoftware %s/%s has empty status.downloadedComponents.astraNicFw", optCtx.Options.DPUNamespace, blueFieldSoftwareName)
	}

	nicFWFileName := filepath.Base(strings.TrimSpace(extractPathForFileName(nicFWLocation)))
	if nicFWFileName == "." || nicFWFileName == string(os.PathSeparator) || nicFWFileName == "" {
		return fmt.Errorf("invalid NIC firmware location %q", nicFWLocation)
	}
	localNICFWPath := filepath.Join(nicFirmwareDir, nicFWFileName)

	if _, err := os.Stat(localNICFWPath); err == nil {
		klog.InfoS("NIC provisioning: firmware already exists, skip download", "path", localNICFWPath)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat local NIC firmware %s: %w", localNICFWPath, err)
	}

	downloadURL, err := resolveNICFirmwareDownloadURL(optCtx.Options.BFBRegistryURL, nicFWLocation)
	if err != nil {
		return err
	}

	if err := utils.DownloadFile(execCtx, downloadURL, localNICFWPath, 0600); err != nil {
		return fmt.Errorf("failed to download NIC firmware from %s to %s: %w", downloadURL, localNICFWPath, err)
	}
	klog.InfoS("NIC provisioning: downloaded firmware", "url", downloadURL, "path", localNICFWPath)
	return nil
}

func resolveNICFirmwareDownloadURL(registryURL, nicFWLocation string) (string, error) {
	if isHTTPURL(nicFWLocation) {
		return nicFWLocation, nil
	}

	base := strings.TrimRight(strings.TrimSpace(registryURL), "/")
	if base != "" && !strings.Contains(base, "://") {
		base = "http://" + base
	}
	joinedURL, err := url.JoinPath(base, strings.TrimLeft(strings.TrimSpace(nicFWLocation), "/"))
	if err != nil {
		return "", fmt.Errorf("failed to build NIC firmware download URL from registry %q and location %q: %w", registryURL, nicFWLocation, err)
	}
	return joinedURL, nil
}

func extractPathForFileName(location string) string {
	if !isHTTPURL(location) {
		return location
	}
	parsedURL, err := url.Parse(location)
	if err != nil {
		return location
	}
	return parsedURL.Path
}

func isHTTPURL(value string) bool {
	parsedURL, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return parsedURL.Scheme == "http" || parsedURL.Scheme == "https"
}

func (n *NICProvisioning) prepareLocalDMSServer(optCtx *operations.Context) error {
	if err := n.stopSystemDMSDServiceIfExists(); err != nil {
		return err
	}

	if n.dmsServer != nil && n.dmsServer.IsRunning() {
		klog.Info("NIC provisioning: local DMS server already running, skip start")
		return nil
	}

	nvConfigUtils := nicnvconfig.NewNVConfigUtils()
	deviceDiscovery := nicdevicediscovery.NewDeviceDiscovery(optCtx.Options.DPUName, nvConfigUtils)

	discoveredDevices, err := deviceDiscovery.DiscoverNicDevices()
	if err != nil {
		return fmt.Errorf("failed to discover NIC devices for local DMS server: %w", err)
	}
	if len(discoveredDevices) == 0 {
		return fmt.Errorf("no NIC devices discovered for local DMS server startup")
	}
	if len(discoveredDevices) != optCtx.Options.NICDeviceCount {
		return fmt.Errorf("discovered NIC device count mismatch: expected %d, discovered %d",
			optCtx.Options.NICDeviceCount, len(discoveredDevices))
	}

	devices := make([]nicconfigurationv1alpha1.NicDevice, 0, len(discoveredDevices))
	for _, device := range discoveredDevices {
		devices = append(devices, device)
	}
	klog.InfoS("NIC provisioning: discovered NIC devices for local DMS server", "deviceCount", len(devices))
	for _, device := range devices {
		pciPorts := make([]string, 0, len(device.Status.Ports))
		for _, port := range device.Status.Ports {
			pciPorts = append(pciPorts, port.PCI)
		}
		klog.InfoS("NIC provisioning: discovered NIC device",
			"serialNumber", device.Status.SerialNumber,
			"type", device.Status.Type,
			"modelName", device.Status.ModelName,
			"portCount", len(device.Status.Ports),
			"pciPorts", strings.Join(pciPorts, ","))
	}

	dmsServer := nicdms.NewDMSServer()
	if err := dmsServer.StartDMSServer(devices); err != nil {
		return fmt.Errorf("failed to start local DMS server: %w", err)
	}
	n.dmsServer = dmsServer
	klog.InfoS("NIC provisioning: local DMS server started", "deviceCount", len(devices))
	return nil
}

func (n *NICProvisioning) stopLocalDMSServer() error {
	if n.dmsServer == nil {
		return nil
	}
	if !n.dmsServer.IsRunning() {
		klog.Info("NIC provisioning: local DMS server is not running, skip stop")
		return nil
	}
	if err := n.dmsServer.StopDMSServer(); err != nil {
		return fmt.Errorf("failed to stop local DMS server: %w", err)
	}
	klog.Info("NIC provisioning: local DMS server stopped")
	return nil
}

func (n *NICProvisioning) stopSystemDMSDServiceIfExists() error {
	if n.runBash == nil {
		n.runBash = bash.Run
	}

	stdout, stderr, err := n.runBash("systemctl show dmsd.service --property=LoadState --value")
	if err != nil {
		combinedOutput := stdout.String() + stderr.String()
		if strings.Contains(combinedOutput, "not-found") || strings.Contains(combinedOutput, "could not be found") {
			klog.Info("NIC provisioning: dmsd service does not exist, skip service stop/disable")
			return nil
		}
		return fmt.Errorf("failed to check dmsd service status: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}

	if strings.TrimSpace(stdout.String()) == "not-found" {
		klog.Info("NIC provisioning: dmsd service not found, skip service stop/disable")
		return nil
	}

	if _, stderr, err := n.runBash("systemctl disable --now dmsd.service"); err != nil {
		return fmt.Errorf("failed to disable and stop dmsd.service: %w, stderr: %s", err, stderr.String())
	}
	if _, stderr, err := n.runBash("systemctl mask dmsd.service"); err != nil {
		return fmt.Errorf("failed to mask dmsd.service: %w, stderr: %s", err, stderr.String())
	}

	klog.Info("NIC provisioning: permanently stopped dmsd service (disabled and masked)")
	return nil
}
