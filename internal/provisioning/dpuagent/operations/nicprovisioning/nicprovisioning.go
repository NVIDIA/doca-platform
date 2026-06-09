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
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	dpuagentutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	utils "github.com/nvidia/doca-platform/internal/utils"

	nicconfigurationv1alpha1 "github.com/Mellanox/nic-configuration-operator/api/v1alpha1"
	nicconfiguration "github.com/Mellanox/nic-configuration-operator/pkg/configuration"
	nicconsts "github.com/Mellanox/nic-configuration-operator/pkg/consts"
	nicdevicediscovery "github.com/Mellanox/nic-configuration-operator/pkg/devicediscovery"
	nicdms "github.com/Mellanox/nic-configuration-operator/pkg/dms"
	nicfirmware "github.com/Mellanox/nic-configuration-operator/pkg/firmware"
	nicnvconfig "github.com/Mellanox/nic-configuration-operator/pkg/nvconfig"
	nicspectrumx "github.com/Mellanox/nic-configuration-operator/pkg/spectrumx"
	nictypes "github.com/Mellanox/nic-configuration-operator/pkg/types"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var nicFirmwareDir = string(os.PathSeparator) + "nic-firmware"

const (
	cx9NICDeviceType          = "1025"
	nicFirmwareInstallTimeout = 15 * time.Minute
	nicNVConfigApplyTimeout   = 5 * time.Minute
	nicRuntimeApplyTimeout    = 5 * time.Minute
	// TODO: The NIC Operator team will remove this constraint in the near future, and we will need to update the code.
	spectrumXConfigDir = "/bindata/spectrum-x"

	embeddedSpectrumXConfigDir = "bindata/spectrum-x"
)

//go:embed bindata/spectrum-x/*.yaml
var embeddedSpectrumXConfigs embed.FS

// NICProvisioning performs NIC-related provisioning steps before the rest of the
// DPU agent pipeline (modules, netplan, etc.).
type NICProvisioning struct {
	dmsServer                 nicdms.DMSServer
	discoveredNICDevices      []nicconfigurationv1alpha1.NicDevice
	runBash                   func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	prepareLocalDMSServerFn   func(optCtx *operations.Context) error
	installNICFirmwareFn      func(execCtx context.Context, optCtx *operations.Context, localNICFWPath string) error
	prepareSpectrumXConfigsFn func() error
	applyNVConfigFn           func(execCtx context.Context, optCtx *operations.Context) error
	applyRuntimeConfigFn      func(execCtx context.Context, optCtx *operations.Context) error
	// configureRestrictedModeFn overrides the restricted mode step (tests only).
	configureRestrictedModeFn func(execCtx context.Context, optCtx *operations.Context) error
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
	if !dpuagentutil.IsBlueField4(optCtx.LatestDPU) {
		return fmt.Errorf("DPU type is not BlueField4")
	}
	if optCtx.Options.BFBRegistryURL == "" {
		return fmt.Errorf("BFB registry URL is not set")
	}
	klog.InfoS("NIC provisioning", "dpu", optCtx.Options.DPUName, "namespace", optCtx.Options.DPUNamespace,
		"bfbRegistryURL", optCtx.Options.BFBRegistryURL)

	blueFieldSoftware, err := getReferencedBlueFieldSoftware(execCtx, optCtx)
	if err != nil {
		return err
	}
	skipNICFirmware := !isPldmFwBundleConfigured(blueFieldSoftware)
	if skipNICFirmware {
		klog.InfoS("NIC provisioning: PldmFwBundle not configured, skipping NIC firmware download and installation",
			"dpu", optCtx.Options.DPUName, "namespace", optCtx.Options.DPUNamespace,
			"blueFieldSoftware", blueFieldSoftware.Name)
	}

	var localNICFWPath string
	if !skipNICFirmware {
		// 1. Download Astra NIC firmware from bfb-registry
		localNICFWPath, err = n.downloadNICFirmware(execCtx, optCtx, blueFieldSoftware)
		if err != nil {
			return err
		}
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
	// 3. Install NIC firmware (skipped when BlueFieldSoftware has no PldmFwBundle)
	if !skipNICFirmware {
		if err := n.installNICFirmwareAndUpdateStatus(execCtx, optCtx, localNICFWPath); err != nil {
			return err
		}
	}
	if optCtx.NICFirmwareRebootRequired {
		klog.InfoS("NIC provisioning: reboot required after NIC firmware installation, skipping remaining NIC provisioning steps")
		return nil
	}

	// 4. Set each E/W NIC to restricted (zero-trust) mode.
	if err := n.configureRestrictedModeWithOverride(execCtx, optCtx); err != nil {
		return err
	}

	// Temporarily stage Spectrum-X configs for the NIC Operator library.
	// TODO: Remove this once the NIC Operator library and DMS integration no longer require DPF to maintain these files.
	if err := n.prepareSpectrumXConfigFiles(); err != nil {
		return err
	}

	// 5. Apply NVConfig for E/W NIC devices.
	if err := n.applyNVConfigAndUpdateStatus(execCtx, optCtx); err != nil {
		return err
	}
	if optCtx.NICFirmwareRebootRequired {
		klog.InfoS("NIC provisioning: reboot required after NIC NV config apply, skipping runtime configuration")
		return nil
	}

	// 6. Apply runtime configuration for E/W NIC devices.
	if err := n.applyRuntimeConfigAndUpdateStatus(execCtx, optCtx); err != nil {
		return err
	}

	return nil
}

func (n *NICProvisioning) installNICFirmwareAndUpdateStatus(execCtx context.Context, optCtx *operations.Context, localNICFWPath string) error {
	installNICFirmware := n.installNICFirmware
	if n.installNICFirmwareFn != nil {
		installNICFirmware = n.installNICFirmwareFn
	}
	if err := installNICFirmware(execCtx, optCtx, localNICFWPath); err != nil {
		setAgentCondition(optCtx, cutil.AgentCondEWNicFirmwareInstalled, metav1.ConditionFalse, "InstallFailed", err.Error())
		updateStatusUntilSuccess(execCtx, optCtx)
		return err
	}
	setAgentCondition(optCtx, cutil.AgentCondEWNicFirmwareInstalled, metav1.ConditionTrue, "InstallSucceeded", "E/W NIC firmware installation completed")
	updateStatusUntilSuccess(execCtx, optCtx)
	return nil
}

func (n *NICProvisioning) configureRestrictedModeWithOverride(execCtx context.Context, optCtx *operations.Context) error {
	configureRestrictedMode := n.configureRestrictedMode
	if n.configureRestrictedModeFn != nil {
		configureRestrictedMode = n.configureRestrictedModeFn
	}
	return configureRestrictedMode(execCtx, optCtx)
}

func (n *NICProvisioning) prepareSpectrumXConfigFiles() error {
	prepareSpectrumXConfigFiles := prepareSpectrumXConfigs
	if n.prepareSpectrumXConfigsFn != nil {
		prepareSpectrumXConfigFiles = n.prepareSpectrumXConfigsFn
	}
	return prepareSpectrumXConfigFiles()
}

func (n *NICProvisioning) applyNVConfigAndUpdateStatus(execCtx context.Context, optCtx *operations.Context) error {
	applyNVConfig := n.applyNVConfig
	if n.applyNVConfigFn != nil {
		applyNVConfig = n.applyNVConfigFn
	}
	if err := applyNVConfig(execCtx, optCtx); err != nil {
		updateStatusUntilSuccess(execCtx, optCtx)
		return err
	}
	return nil
}

func (n *NICProvisioning) applyRuntimeConfigAndUpdateStatus(execCtx context.Context, optCtx *operations.Context) error {
	applyRuntimeConfig := n.applyRuntimeConfig
	if n.applyRuntimeConfigFn != nil {
		applyRuntimeConfig = n.applyRuntimeConfigFn
	}
	if err := applyRuntimeConfig(execCtx, optCtx); err != nil {
		updateStatusUntilSuccess(execCtx, optCtx)
		return err
	}
	return nil
}

func updateStatusUntilSuccess(execCtx context.Context, optCtx *operations.Context) {
	if optCtx.UpdateStatusUntilSuccess != nil {
		optCtx.UpdateStatusUntilSuccess(execCtx)
	}
}

func getReferencedBlueFieldSoftware(execCtx context.Context, optCtx *operations.Context) (*provisioningv1.BlueFieldSoftware, error) {
	if optCtx.LatestDPU == nil {
		return nil, fmt.Errorf("latest DPU object is required for NIC provisioning")
	}
	if optCtx.Client == nil {
		return nil, fmt.Errorf("client is required for NIC provisioning")
	}

	blueFieldSoftwareName := strings.TrimSpace(optCtx.LatestDPU.Spec.BlueFieldSoftware)
	if blueFieldSoftwareName == "" {
		return nil, fmt.Errorf("dpu %s/%s does not reference a BlueFieldSoftware", optCtx.Options.DPUNamespace, optCtx.Options.DPUName)
	}

	blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
	if err := optCtx.Client.Get(execCtx, client.ObjectKey{Namespace: optCtx.Options.DPUNamespace, Name: blueFieldSoftwareName}, blueFieldSoftware); err != nil {
		return nil, fmt.Errorf("failed to get BlueFieldSoftware %s/%s: %w", optCtx.Options.DPUNamespace, blueFieldSoftwareName, err)
	}
	return blueFieldSoftware, nil
}

func isPldmFwBundleConfigured(bfs *provisioningv1.BlueFieldSoftware) bool {
	if bfs == nil {
		return false
	}
	return strings.TrimSpace(bfs.Spec.PldmFwBundle) != ""
}

// downloadNICFirmware resolves and downloads Astra NIC firmware from bfb-registry
// to the local nic-firmware directory if it is not already cached.
func (n *NICProvisioning) downloadNICFirmware(execCtx context.Context, optCtx *operations.Context, blueFieldSoftware *provisioningv1.BlueFieldSoftware) (string, error) {
	if blueFieldSoftware == nil {
		return "", fmt.Errorf("blueFieldSoftware is required for NIC firmware download")
	}

	nicFWLocation := strings.TrimSpace(blueFieldSoftware.Status.DownloadedComponents.AstraNicFw)
	if nicFWLocation == "" {
		return "", fmt.Errorf("blueFieldSoftware %s/%s has empty status.downloadedComponents.astraNicFw",
			blueFieldSoftware.Namespace, blueFieldSoftware.Name)
	}

	nicFWFileName := filepath.Base(strings.TrimSpace(extractPathForFileName(nicFWLocation)))
	if nicFWFileName == "." || nicFWFileName == string(os.PathSeparator) || nicFWFileName == "" {
		return "", fmt.Errorf("invalid NIC firmware location %q", nicFWLocation)
	}
	localNICFWPath := filepath.Join(nicFirmwareDir, nicFWFileName)

	if _, err := os.Stat(localNICFWPath); err == nil {
		klog.InfoS("NIC provisioning: firmware already exists, skip download", "path", localNICFWPath)
		return localNICFWPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to stat local NIC firmware %s: %w", localNICFWPath, err)
	}

	downloadURL, err := resolveNICFirmwareDownloadURL(optCtx.Options.BFBRegistryURL, nicFWLocation)
	if err != nil {
		return "", err
	}

	if err := utils.DownloadFile(execCtx, downloadURL, localNICFWPath, 0600); err != nil {
		return "", fmt.Errorf("failed to download NIC firmware from %s to %s: %w", downloadURL, localNICFWPath, err)
	}
	klog.InfoS("NIC provisioning: downloaded firmware", "url", downloadURL, "path", localNICFWPath)
	return localNICFWPath, nil
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
	devices := filterCX9Devices(discoveredDevices)
	if len(devices) == 0 {
		return fmt.Errorf("no CX9 NIC devices discovered for local DMS server startup")
	}
	if len(devices) != optCtx.Options.NICDeviceCount {
		return fmt.Errorf("discovered NIC device count mismatch: expected %d, discovered %d",
			optCtx.Options.NICDeviceCount, len(devices))
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
	n.discoveredNICDevices = devices
	klog.InfoS("NIC provisioning: local DMS server started", "deviceCount", len(devices))
	return nil
}

func filterCX9Devices(discoveredDevices map[string]nicconfigurationv1alpha1.NicDevice) []nicconfigurationv1alpha1.NicDevice {
	cx9Devices := make([]nicconfigurationv1alpha1.NicDevice, 0, len(discoveredDevices))
	for _, device := range discoveredDevices {
		if strings.TrimSpace(device.Status.Type) == cx9NICDeviceType {
			cx9Devices = append(cx9Devices, device)
			continue
		}
		klog.InfoS("NIC provisioning: skipping non-CX9 NIC device",
			"serialNumber", device.Status.SerialNumber,
			"type", device.Status.Type,
			"modelName", device.Status.ModelName)
	}
	return cx9Devices
}

func (n *NICProvisioning) installNICFirmware(execCtx context.Context, optCtx *operations.Context, localNICFWPath string) error {
	if n.dmsServer == nil || !n.dmsServer.IsRunning() {
		return fmt.Errorf("local DMS server is not running for NIC firmware installation")
	}
	if len(n.discoveredNICDevices) == 0 {
		return fmt.Errorf("no discovered NIC devices available for firmware installation")
	}

	fwMgr := nicfirmware.NewFirmwareManager(nil, n.dmsServer, "")
	installOptions := &nictypes.FirmwareInstallOptions{
		FwFilePath: localNICFWPath,
		SkipReset:  true,
	}
	installCtx, cancel := context.WithTimeout(execCtx, nicFirmwareInstallTimeout)
	defer cancel()

	errCh := make(chan error, len(n.discoveredNICDevices))
	rebootRequiredCh := make(chan bool, len(n.discoveredNICDevices))
	var wg sync.WaitGroup
	for _, discoveredDevice := range n.discoveredNICDevices {
		device := discoveredDevice
		wg.Add(1)
		go func() {
			defer wg.Done()
			device.Spec.Firmware = &nicconfigurationv1alpha1.FirmwareTemplateSpec{
				UpdatePolicy: nicconsts.FirmwareUpdatePolicyUpdate,
			}
			device.Spec.Configuration = &nicconfigurationv1alpha1.NicDeviceConfigurationSpec{
				Template: buildEWNicConfigurationTemplate(optCtx.DPUFlavor.Spec.FirstEWNicConfiguration()),
			}
			rebootRequired, err := fwMgr.InstallFirmware(installCtx, &device, installOptions)
			if err != nil {
				errCh <- fmt.Errorf("failed to install firmware on NIC %q (type %q): %w",
					device.Status.SerialNumber, device.Status.Type, err)
				return
			}
			if rebootRequired {
				rebootRequiredCh <- true
			}
			klog.InfoS("NIC provisioning: firmware installed successfully",
				"serialNumber", device.Status.SerialNumber,
				"type", device.Status.Type,
				"rebootRequired", rebootRequired)
		}()
	}

	wg.Wait()
	close(errCh)
	close(rebootRequiredCh)

	installErrs := make([]string, 0, len(n.discoveredNICDevices))
	for installErr := range errCh {
		installErrs = append(installErrs, installErr.Error())
	}
	if len(installErrs) > 0 {
		return fmt.Errorf("NIC firmware installation failed: %s", strings.Join(installErrs, "; "))
	}
	for rebootRequired := range rebootRequiredCh {
		if rebootRequired {
			optCtx.NICFirmwareRebootRequired = true
			break
		}
	}
	if err := installCtx.Err(); err != nil && err != context.Canceled {
		return fmt.Errorf("NIC firmware installation timed out or canceled: %w", err)
	}
	return nil
}

func (n *NICProvisioning) applyNVConfig(execCtx context.Context, optCtx *operations.Context) error {
	if n.dmsServer == nil || !n.dmsServer.IsRunning() {
		return fmt.Errorf("local DMS server is not running for NIC NV config apply")
	}
	if len(n.discoveredNICDevices) == 0 {
		return fmt.Errorf("no discovered NIC devices available for NV config apply")
	}

	spectrumXConfigs, err := loadSpectrumXConfigs(spectrumXConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load Spectrum-X configs: %w", err)
	}
	nvUtils := nicnvconfig.NewNVConfigUtils()
	spectrumXMgr := nicspectrumx.NewSpectrumXConfigManager(n.dmsServer, spectrumXConfigs)
	cfgMgr := nicconfiguration.NewConfigurationManager(nil, n.dmsServer, nvUtils, spectrumXMgr)
	applyOptions := &nictypes.ConfigurationOptions{
		SkipReset:   true,
		WithDefault: true,
	}
	applyCtx, cancel := context.WithTimeout(execCtx, nicNVConfigApplyTimeout)
	defer cancel()

	errCh := make(chan error, len(n.discoveredNICDevices))
	partialAppliedCh := make(chan bool, len(n.discoveredNICDevices))
	var wg sync.WaitGroup
	for _, discoveredDevice := range n.discoveredNICDevices {
		device := discoveredDevice
		wg.Add(1)
		go func() {
			defer wg.Done()
			device.Spec.Configuration = &nicconfigurationv1alpha1.NicDeviceConfigurationSpec{
				Template: buildEWNicConfigurationTemplate(optCtx.DPUFlavor.Spec.FirstEWNicConfiguration()),
			}
			result, applyErr := cfgMgr.ApplyNVConfiguration(applyCtx, &device, applyOptions)
			if applyErr != nil {
				errCh <- fmt.Errorf("failed to apply NV config on NIC %q (type %q): %w",
					device.Status.SerialNumber, device.Status.Type, applyErr)
				return
			}
			if result.Status == nictypes.ApplyStatusPartiallyApplied {
				partialAppliedCh <- true
			}
			klog.InfoS("NIC provisioning: NV config applied",
				"serialNumber", device.Status.SerialNumber,
				"type", device.Status.Type,
				"status", result.Status,
				"rebootRequired", result.RebootRequired)
		}()
	}

	wg.Wait()
	close(errCh)
	close(partialAppliedCh)

	applyErrs := make([]string, 0, len(n.discoveredNICDevices))
	for applyErr := range errCh {
		applyErrs = append(applyErrs, applyErr.Error())
	}
	if len(applyErrs) > 0 {
		return fmt.Errorf("NIC NV config apply failed: %s", strings.Join(applyErrs, "; "))
	}
	for partialApplied := range partialAppliedCh {
		if partialApplied {
			optCtx.NICFirmwareRebootRequired = true
			break
		}
	}
	if err := applyCtx.Err(); err != nil && err != context.Canceled {
		return fmt.Errorf("NIC NV config apply timed out or canceled: %w", err)
	}
	return nil
}

func (n *NICProvisioning) applyRuntimeConfig(execCtx context.Context, optCtx *operations.Context) error {
	if n.dmsServer == nil || !n.dmsServer.IsRunning() {
		return fmt.Errorf("local DMS server is not running for NIC runtime config apply")
	}
	if len(n.discoveredNICDevices) == 0 {
		return fmt.Errorf("no discovered NIC devices available for runtime config apply")
	}

	spectrumXConfigs, err := loadSpectrumXConfigs(spectrumXConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load Spectrum-X configs: %w", err)
	}
	nvUtils := nicnvconfig.NewNVConfigUtils()
	spectrumXMgr := nicspectrumx.NewSpectrumXConfigManager(n.dmsServer, spectrumXConfigs)
	cfgMgr := nicconfiguration.NewConfigurationManager(nil, n.dmsServer, nvUtils, spectrumXMgr)
	applyCtx, cancel := context.WithTimeout(execCtx, nicRuntimeApplyTimeout)
	defer cancel()

	errCh := make(chan error, len(n.discoveredNICDevices))
	var wg sync.WaitGroup
	for _, discoveredDevice := range n.discoveredNICDevices {
		device := discoveredDevice
		wg.Add(1)
		go func() {
			defer wg.Done()
			device.Spec.Configuration = &nicconfigurationv1alpha1.NicDeviceConfigurationSpec{
				Template: buildEWNicConfigurationTemplate(optCtx.DPUFlavor.Spec.FirstEWNicConfiguration()),
			}
			result, applyErr := cfgMgr.ApplyRuntimeConfiguration(applyCtx, &device)
			if applyErr != nil {
				errCh <- fmt.Errorf("failed to apply runtime config on NIC %q (type %q): %w",
					device.Status.SerialNumber, device.Status.Type, applyErr)
				return
			}
			klog.InfoS("NIC provisioning: runtime config applied",
				"serialNumber", device.Status.SerialNumber,
				"type", device.Status.Type,
				"status", result.Status)
		}()
	}

	wg.Wait()
	close(errCh)

	applyErrs := make([]string, 0, len(n.discoveredNICDevices))
	for applyErr := range errCh {
		applyErrs = append(applyErrs, applyErr.Error())
	}
	if len(applyErrs) > 0 {
		return fmt.Errorf("NIC runtime config apply failed: %s", strings.Join(applyErrs, "; "))
	}
	if err := applyCtx.Err(); err != nil && err != context.Canceled {
		return fmt.Errorf("NIC runtime config apply timed out or canceled: %w", err)
	}
	return nil
}

func prepareSpectrumXConfigs() error {
	entries, err := fs.ReadDir(embeddedSpectrumXConfigs, embeddedSpectrumXConfigDir)
	if err != nil {
		return fmt.Errorf("failed to read embedded Spectrum-X configs: %w", err)
	}
	if err := os.MkdirAll(spectrumXConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create Spectrum-X config directory %s: %w", spectrumXConfigDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := embeddedSpectrumXConfigs.ReadFile(filepath.Join(embeddedSpectrumXConfigDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read embedded Spectrum-X config %s: %w", entry.Name(), err)
		}
		configPath := filepath.Join(spectrumXConfigDir, entry.Name())
		if err := os.WriteFile(configPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write Spectrum-X config %s: %w", configPath, err)
		}
		klog.InfoS("NIC provisioning: staged Spectrum-X config", "path", configPath)
	}
	return nil
}

// configureRestrictedMode sets each discovered E/W NIC to restricted (zero-trust) mode
// via mlxprivhost. The restricted mode is a device-level (per-ASIC) firmware setting, so it
// is applied once per NicDevice using the first port's PCI address (PF0): the ports of a
// dual-port card are functions of the same device and share a single restricted mode config.
func (n *NICProvisioning) configureRestrictedMode(_ context.Context, _ *operations.Context) error {
	if len(n.discoveredNICDevices) == 0 {
		return fmt.Errorf("no discovered NIC devices available for restricted mode configuration")
	}
	if n.runBash == nil {
		n.runBash = bash.Run
	}

	for _, device := range n.discoveredNICDevices {
		if len(device.Status.Ports) == 0 {
			return fmt.Errorf("NIC %q (type %q) has no ports for restricted mode configuration",
				device.Status.SerialNumber, device.Status.Type)
		}
		pciAddress := strings.TrimSpace(device.Status.Ports[0].PCI)
		if pciAddress == "" {
			return fmt.Errorf("NIC %q (type %q) has empty PCI address for restricted mode configuration",
				device.Status.SerialNumber, device.Status.Type)
		}

		cmd := fmt.Sprintf("mlxprivhost -d %s r --disable_rshim --disable_tracer --disable_counter_rd --disable_port_owner", pciAddress)
		stdout, stderr, err := n.runBash(cmd)
		if err != nil {
			return fmt.Errorf("failed to set restricted mode on NIC %q (type %q, model %q, pci %q): %w, stdout: %s, stderr: %s",
				device.Status.SerialNumber, device.Status.Type, device.Status.ModelName, pciAddress, err, stdout.String(), stderr.String())
		}
		klog.InfoS("NIC provisioning: set E/W NIC to restricted mode",
			"serialNumber", device.Status.SerialNumber,
			"type", device.Status.Type,
			"modelName", device.Status.ModelName,
			"pci", pciAddress)
	}
	return nil
}

func buildEWNicConfigurationTemplate(cfg *provisioningv1.NicConfiguration) *nicconfigurationv1alpha1.ConfigurationTemplateSpec {
	if cfg == nil {
		return nil
	}
	rawNvConfig := make([]nicconfigurationv1alpha1.NvConfigParam, 0, len(cfg.RawNvConfig))
	rawNvConfig = append(rawNvConfig, cfg.RawNvConfig...)
	return &nicconfigurationv1alpha1.ConfigurationTemplateSpec{
		NumVfs:             cfg.NumVfs,
		LinkType:           cfg.LinkType,
		SpectrumXOptimized: cfg.SpectrumXOptimized,
		RawNvConfig:        rawNvConfig,
	}
}

func loadSpectrumXConfigs(configDir string) (map[string]*nictypes.SpectrumXConfig, error) {
	configs := make(map[string]*nictypes.SpectrumXConfig)
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			klog.InfoS("NIC provisioning: Spectrum-X config directory does not exist, continue with empty configs", "path", configDir)
			return configs, nil
		}
		return nil, err
	}
	for _, file := range entries {
		if file.IsDir() {
			continue
		}
		config, err := nictypes.LoadSpectrumXConfig(filepath.Join(configDir, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("load Spectrum-X config file %q: %w", file.Name(), err)
		}
		configName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
		configs[configName] = config
	}
	return configs, nil
}

func setAgentCondition(optCtx *operations.Context, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&optCtx.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
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
