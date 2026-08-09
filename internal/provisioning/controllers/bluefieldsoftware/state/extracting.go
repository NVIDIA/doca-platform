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

// unpackTargetIfNeeded unpacks target unless its output directory and the status
// versions derived from that output are both already present.
func (st *blueFieldSoftwareExtractingState) unpackTargetIfNeeded(ctx context.Context, target extractTarget) error {
	outDir := st.extractOutputDir(target.componentType)
	alreadyExtracted, err := isExtractOutputPresent(outDir)
	if err != nil {
		return st.failExtract(err, "Check extract output directory for %s (%s/%s): %v",
			target.componentType, st.bfs.Namespace, st.bfs.Name, err)
	}

	if alreadyExtracted && statusHasVersionsForComponent(st.bfs, target.componentType) {
		return nil
	}

	// A leftover output directory does not mean status kept the versions.
	// Clear it before re-unpacking: unpacked images are written read-only.
	if alreadyExtracted {
		if err := cutil.RemoveAllEx(outDir); err != nil {
			return st.failExtract(err, "Clear stale extract output directory for %s (%s/%s) failed: %v",
				target.componentType, st.bfs.Namespace, st.bfs.Name, err)
		}
	}

	components, err := callPldmUnpackService(ctx, target.packagePath, outDir)
	if err != nil {
		return st.failExtract(err, "Extract PLDM firmware bundle for %s (%s/%s) failed: %v",
			target.componentType, st.bfs.Namespace, st.bfs.Name, err)
	}
	applyUnpackedComponentsToDownloaded(st.bfs, target.componentType, components)
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
	packagePath   string
}

// extractableComponentTypes are the component types unpacked by this state. Every
// entry must have a case in statusHasVersionsForComponent.
var extractableComponentTypes = []butil.ComponentType{
	butil.ComponentTypeFwBundle,
	butil.ComponentTypePlatformFwBundle,
}

func (st *blueFieldSoftwareExtractingState) resolveExtractTargets() []extractTarget {
	var targets []extractTarget
	for _, componentType := range extractableComponentTypes {
		if packagePath := st.resolvePackagePath(componentType); packagePath != "" {
			targets = append(targets, extractTarget{
				componentType: componentType,
				packagePath:   packagePath,
			})
		}
	}
	return targets
}

func (st *blueFieldSoftwareExtractingState) resolvePackagePath(componentType butil.ComponentType) string {
	packageRef := st.statusPathForComponent(componentType)
	if packageRef == "" {
		packageRef = butil.SpecURLForComponent(st.bfs, componentType)
	}
	if packageRef == "" {
		return ""
	}
	if isURL(packageRef) {
		fileName := butil.ComponentDownloadFilename(st.bfs, componentType, packageRef)
		return componentDestinationPath(componentType, fileName)
	}
	return packageRef
}

func (st *blueFieldSoftwareExtractingState) statusPathForComponent(componentType butil.ComponentType) string {
	switch componentType {
	case butil.ComponentTypeFwBundle:
		return st.bfs.Status.DownloadedComponents.PldmFwBundle
	case butil.ComponentTypePlatformFwBundle:
		return st.bfs.Status.DownloadedComponents.PlatformPldmFwBundle
	default:
		return ""
	}
}

func (st *blueFieldSoftwareExtractingState) extractOutputDir(componentType butil.ComponentType) string {
	return extractOutputDirForBFS(st.bfs, componentType)
}

// extractOutputDirForBFS returns the on-disk directory where PLDM unpack output
// is written for this BlueFieldSoftware and source bundle.
func extractOutputDirForBFS(bfs *provisioningv1.BlueFieldSoftware, componentType butil.ComponentType) string {
	if bfs == nil {
		return ""
	}
	return filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components",
		fmt.Sprintf("%s-%s-%s-extracted", bfs.Namespace, bfs.Name, componentType))
}

// isExtractOutputPresent reports whether outDir exists as a directory and already
// contains at least one entry (so re-reconcile can skip redundant unpack work).
// An empty directory is treated as incomplete extraction.
func isExtractOutputPresent(outDir string) (bool, error) {
	st, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !st.IsDir() {
		return false, fmt.Errorf("extract output path %q exists but is not a directory", outDir)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
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

// statusHasVersionsForComponent reports whether status already carries version data
// derived from componentType's bundle, and is the signal used to decide that its
// output does not need unpacking again.
//
// The fields listed per component type below must be exactly those that
// applyUnpackedComponentsToDownloaded can write for that component type - the two
// functions are coupled and have to be updated together. If they drift, or a
// component type reaches here without a case, the bundle is treated as never
// extracted and is unpacked again each time the object enters this state.
// TestStatusHasVersionsForComponent_CoversExtractableTypes guards the missing-case
// half of that invariant.
//
// One populated field is enough: a bundle need not ship every component, and
// requiring all of them would re-unpack bundles that are perfectly fine.
func statusHasVersionsForComponent(bfs *provisioningv1.BlueFieldSoftware, componentType butil.ComponentType) bool {
	if bfs == nil || bfs.Status.Versions == nil {
		return false
	}
	versions := bfs.Status.Versions
	switch componentType {
	case butil.ComponentTypeFwBundle:
		return versions.BMCVersion != "" || versions.BMCErotVersion != "" ||
			versions.SBIOSVersion != "" || versions.BFNicFwVersion != ""
	case butil.ComponentTypePlatformFwBundle:
		return versions.EWNicFwVersion != ""
	default:
		return false
	}
}

func applyUnpackedComponentsToDownloaded(
	bfs *provisioningv1.BlueFieldSoftware,
	sourceComponentType butil.ComponentType,
	components []unpackedComponent,
) {
	if bfs == nil {
		return
	}
	for _, component := range components {
		imageName := strings.ToUpper(filepath.Base(component.FWImage))
		switch sourceComponentType {
		case butil.ComponentTypePlatformFwBundle:
			if !strings.Contains(imageName, "CX9") {
				continue
			}
			bfs.Status.DownloadedComponents.NicFw = component.FWImage
			if bfs.Status.Versions == nil {
				bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{}
			}
			bfs.Status.Versions.EWNicFwVersion = component.ComponentVersionString
		case butil.ComponentTypeFwBundle:
			if bfs.Status.Versions == nil {
				bfs.Status.Versions = &provisioningv1.BluefieldSoftwareVersions{}
			}
			switch {
			case strings.Contains(imageName, "CX9"):
				bfs.Status.Versions.BFNicFwVersion = component.ComponentVersionString
			case strings.Contains(imageName, "BMC_BF4"):
				bfs.Status.Versions.BMCVersion = component.ComponentVersionString
			case strings.Contains(imageName, "EROT"):
				bfs.Status.Versions.BMCErotVersion = component.ComponentVersionString
			case strings.Contains(imageName, "SBIOS"):
				bfs.Status.Versions.SBIOSVersion = component.ComponentVersionString
			}
		}
	}
}
