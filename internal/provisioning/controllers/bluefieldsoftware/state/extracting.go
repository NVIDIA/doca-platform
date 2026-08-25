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

package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultPldmUnpackSocketPath = "/var/run/dpf/pldm-unpack.sock"
	defaultPldmUnpackEndpoint   = "/v1/unpack"
)

type blueFieldSoftwareExtractingState struct {
	bfs      *provisioningv1.BlueFieldSoftware
	recorder record.EventRecorder
}

type unpackRequest struct {
	PackagePath string `json:"packagePath"`
	OutDir      string `json:"outDir,omitempty"`
}

type unpackResponse struct {
	Success  bool   `json:"success"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Error    string `json:"error"`
}

type unpackedComponent struct {
	ComponentVersionString string `json:"componentVersionString"`
	FWImage                string `json:"fwImage"`
}

type unpackStdout struct {
	FirmwareDeviceRecords []unpackFirmwareDeviceRecord `json:"FirmwareDeviceRecords"`
}

type unpackFirmwareDeviceRecord struct {
	Components []unpackComponentPayload `json:"Components"`
}

type unpackComponentPayload struct {
	ComponentVersionString string `json:"ComponentVersionString"`
	FWImage                string `json:"FWImage"`
}

func (st *blueFieldSoftwareExtractingState) Handle(ctx context.Context, _ client.Client) error {
	if isDeleting(st.bfs) {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}

	targets := st.resolveExtractTargets()
	if len(targets) == 0 {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
		conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondReady)
		return nil
	}

	for _, target := range targets {
		if err := st.unpackTargetIfNeeded(ctx, target); err != nil {
			return err
		}
	}

	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulExtractBFBReason,
		"Extract PLDM firmware bundle successful")
	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondReady)
	return nil
}

// unpackTargetIfNeeded unpacks target unless status already carries the versions
// derived from that unpack.
//
// Completion is tracked in status rather than by the presence of the output
// directory. That directory lives on a /bfb hostPath that outlives the object
// (and a DPF reinstall), so a leftover from an earlier BlueFieldSoftware with the
// same name would otherwise skip the unpack and leave this object Ready with no
// versions.
func (st *blueFieldSoftwareExtractingState) unpackTargetIfNeeded(ctx context.Context, target extractTarget) error {
	if extractedVersionsRecorded(st.bfs, target.componentType, target.key) {
		return nil
	}

	outDir := st.extractOutputDir(target.componentType, target.key)
	// Drop any leftover output so this bundle is not unpacked alongside files
	// from a different bundle previously extracted to the same path.
	if err := cutil.RemoveAllEx(outDir); err != nil {
		return st.failExtract(err, "Clear extract output directory for %s (%s/%s): %v",
			target.componentType, st.bfs.Namespace, st.bfs.Name, err)
	}

	components, err := callPldmUnpackService(ctx, target.packagePath, outDir)
	if err != nil {
		return st.failExtract(err, "Extract PLDM firmware bundle for %s (%s/%s) failed: %v",
			target.componentType, st.bfs.Namespace, st.bfs.Name, err)
	}
	if err := applyUnpackedComponentsToDownloaded(st.bfs, target.componentType, target.key, components); err != nil {
		return st.failExtract(err, "Apply unpacked components for %s (%s/%s) failed: %v",
			target.componentType, st.bfs.Namespace, st.bfs.Name, err)
	}
	return nil
}

// failExtract moves the object to Error with the formatted message and returns err
// unchanged so callers can propagate it.
func (st *blueFieldSoftwareExtractingState) failExtract(err error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedExtractBFBReason, msg)
	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareError
	conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady,
		conditions.ReasonFailure, conditions.ConditionMessage(msg))
	return err
}

type extractTarget struct {
	componentType butil.ComponentType
	// key is the PSID for the DPU PLDM bundle; "" for single-valued bundles.
	key         string
	packagePath string
}

func (st *blueFieldSoftwareExtractingState) resolveExtractTargets() []extractTarget {
	var targets []extractTarget
	for _, unit := range extractionUnits(st.bfs) {
		if packagePath := st.resolvePackagePath(unit); packagePath != "" {
			targets = append(targets, extractTarget{
				componentType: unit.ComponentType,
				key:           unit.Key,
				packagePath:   packagePath,
			})
		}
	}
	return targets
}

func (st *blueFieldSoftwareExtractingState) resolvePackagePath(unit componentInfo) string {
	packageRef := downloadedComponentPath(st.bfs, unit.ComponentType, unit.Key)
	if packageRef == "" {
		packageRef = unit.URL
	}
	if packageRef == "" {
		return ""
	}
	if isURL(packageRef) {
		return componentDestinationPath(unit.ComponentType, componentFileName(st.bfs, unit))
	}
	return packageRef
}

func (st *blueFieldSoftwareExtractingState) extractOutputDir(componentType butil.ComponentType, key string) string {
	return extractOutputDirForBFS(st.bfs, componentType, key)
}

// extractOutputDirForBFS returns the on-disk directory where PLDM unpack output
// is written for this BlueFieldSoftware and source bundle (per PSID for DPU bundles).
func extractOutputDirForBFS(bfs *provisioningv1.BlueFieldSoftware, componentType butil.ComponentType, key string) string {
	if bfs == nil {
		return ""
	}
	name := fmt.Sprintf("%s-%s-%s", bfs.Namespace, bfs.Name, componentType)
	if key != "" {
		name += "-" + key
	}
	return filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components", name+"-extracted")
}

// extractedVersionsRecorded reports whether this object's status already carries the
// firmware versions produced by unpacking a bundle, so a re-reconcile can skip
// redundant unpack work. The fields checked per component type must be exactly those
// applyUnpackedComponentsToDownloaded requires for it: a partial record must not count
// as done, or checkFirmwareVersions fails forever with no path back into Extracting.
func extractedVersionsRecorded(bfs *provisioningv1.BlueFieldSoftware, componentType butil.ComponentType, key string) bool {
	if bfs.Status.Versions == nil {
		return false
	}
	switch componentType {
	case butil.ComponentTypeFwBundle:
		return deviceVersionsComplete(bfs.Status.Versions.BluefieldSoftwareVersions[key])
	case butil.ComponentTypePlatformFwBundle:
		return bfs.Status.Versions.EWNicFwVersion != ""
	}
	return false
}

// deviceVersionsComplete is true when every field checkFirmwareVersions requires is set.
func deviceVersionsComplete(v provisioningv1.BluefieldDeviceVersions) bool {
	return v.BMCVersion != "" && v.BMCErotVersion != "" && v.SBIOSVersion != "" && v.BFNicFwVersion != ""
}

func callPldmUnpackService(ctx context.Context, packagePath, outDir string) ([]unpackedComponent, error) {
	logger := log.FromContext(ctx)
	socketPath := os.Getenv("PLDM_UNPACK_SOCKET_PATH")
	if socketPath == "" {
		socketPath = defaultPldmUnpackSocketPath
	}
	endpoint := os.Getenv("PLDM_UNPACK_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultPldmUnpackEndpoint
	}

	reqBody, err := json.Marshal(unpackRequest{
		PackagePath: packagePath,
		OutDir:      outDir,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal unpack request: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(innerCtx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(innerCtx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build unpack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call unpack service: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read unpack response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unpack service returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed unpackResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse unpack response: %w", err)
	}
	if !parsed.Success {
		return nil, fmt.Errorf("unpack failed (exitCode=%d): %s %s %s", parsed.ExitCode, parsed.Error, parsed.Stdout, parsed.Stderr)
	}
	components, err := extractUnpackedComponents(parsed.Stdout)
	if err != nil {
		return nil, fmt.Errorf("extract components from unpack stdout: %w", err)
	}
	logger.Info("unpack service returned success",
		"exitCode", parsed.ExitCode,
		"stdout", parsed.Stdout,
		"stderr", parsed.Stderr,
		"components", components)
	return components, nil
}

func extractUnpackedComponents(stdout string) ([]unpackedComponent, error) {
	if stdout == "" {
		return nil, nil
	}

	var parsed unpackStdout
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return nil, fmt.Errorf("parse stdout json: %w", err)
	}

	components := make([]unpackedComponent, 0)
	seen := make(map[unpackedComponent]struct{})
	for _, record := range parsed.FirmwareDeviceRecords {
		for _, comp := range record.Components {
			if comp.ComponentVersionString == "" && comp.FWImage == "" {
				continue
			}
			component := unpackedComponent(comp)
			if _, ok := seen[component]; ok {
				continue
			}
			seen[component] = struct{}{}
			components = append(components, component)
		}
	}
	return components, nil
}

func applyUnpackedComponentsToDownloaded(
	bfs *provisioningv1.BlueFieldSoftware,
	sourceComponentType butil.ComponentType,
	key string,
	components []unpackedComponent,
) error {
	if bfs == nil {
		return nil
	}
	if bfs.Status.Versions == nil {
		bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{}
	}
	switch sourceComponentType {
	case butil.ComponentTypeFwBundle:
		return applyDeviceVersions(bfs, key, components)
	case butil.ComponentTypePlatformFwBundle:
		applyPlatformNicFw(bfs, components)
	}
	return nil
}

// applyDeviceVersions records the firmware versions shipped in one PSID's DPU PLDM
// bundle, which the firmware update flow compares against the device before updating.
// All four component types are required; writing a partial record would make
// extractedVersionsRecorded skip re-unpack while checkFirmwareVersions still fails.
func applyDeviceVersions(bfs *provisioningv1.BlueFieldSoftware, psid string, components []unpackedComponent) error {
	versions := bfs.Status.Versions.BluefieldSoftwareVersions[psid]
	for _, component := range components {
		imageName := strings.ToUpper(filepath.Base(component.FWImage))
		switch {
		case strings.Contains(imageName, "CX9"):
			if err := ensureCX9ImagePSID(imageName, psid); err != nil {
				return err
			}
			versions.BFNicFwVersion = component.ComponentVersionString
		case strings.Contains(imageName, "BMC_BF4"):
			versions.BMCVersion = component.ComponentVersionString
		case strings.Contains(imageName, "EROT"):
			versions.BMCErotVersion = component.ComponentVersionString
		case strings.Contains(imageName, "SBIOS"):
			versions.SBIOSVersion = component.ComponentVersionString
		}
	}
	if !deviceVersionsComplete(versions) {
		return fmt.Errorf("DPU PLDM bundle for PSID %s missing required component versions (need BMC, BMC ERoT, SBIOS, and BF NIC)", psid)
	}
	if bfs.Status.Versions.BluefieldSoftwareVersions == nil {
		bfs.Status.Versions.BluefieldSoftwareVersions = make(map[string]provisioningv1.BluefieldDeviceVersions)
	}
	bfs.Status.Versions.BluefieldSoftwareVersions[psid] = versions
	return nil
}

// applyPlatformNicFw records the E/W NIC firmware image unpacked from the platform
// bundle. That image is what the DPU agent flashes, so its path is kept in status
// next to the version.
func applyPlatformNicFw(bfs *provisioningv1.BlueFieldSoftware, components []unpackedComponent) {
	for _, component := range components {
		if !strings.Contains(strings.ToUpper(filepath.Base(component.FWImage)), "CX9") {
			continue
		}
		bfs.Status.DownloadedComponents.NicFw = component.FWImage
		bfs.Status.Versions.EWNicFwVersion = component.ComponentVersionString
	}
}

// psidFromCX9ImageName extracts the PSID from a CX9 firmware image basename such as
// "CX9_MT_0000001774_82.48.1680_4fdd89de_image.bin" -> "MT_0000001774".
// imageName should already be uppercased.
func psidFromCX9ImageName(imageName string) (string, error) {
	const marker = "CX9_"
	idx := strings.Index(imageName, marker)
	if idx < 0 {
		return "", fmt.Errorf("CX9 image name %q does not contain %q prefix", imageName, marker)
	}
	rest := imageName[idx+len(marker):]
	parts := strings.Split(rest, "_")
	// Expected: MT, <digits>, <version...>, ...
	if len(parts) < 2 || parts[0] != "MT" || parts[1] == "" {
		return "", fmt.Errorf("CX9 image name %q does not contain a PSID after %q", imageName, marker)
	}
	return parts[0] + "_" + parts[1], nil
}

func ensureCX9ImagePSID(imageName, key string) error {
	psid, err := psidFromCX9ImageName(imageName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(psid, key) {
		return fmt.Errorf("CX9 image PSID %q does not match expected PSID %q", psid, key)
	}
	return nil
}
