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
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	utils "github.com/nvidia/doca-platform/internal/utils"

	"k8s.io/klog/v2"
)

var nicFirmwareDir = string(os.PathSeparator) + "nic-firmware"

// NICProvisioning performs NIC-related provisioning steps before the rest of the
// DPU agent pipeline (modules, netplan, etc.).
type NICProvisioning struct{}

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

func (n *NICProvisioning) Execute(execCtx context.Context, optCtx *operations.Context) error {
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
		//TODO: remove this once we have a way to set correct path for blueFieldSoftware.Status.DownloadedComponents.AstraNicFw
		return nil
		//return fmt.Errorf("blueFieldSoftware %s/%s has empty status.downloadedComponents.astraNicFw", optCtx.Options.DPUNamespace, blueFieldSoftwareName)
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
