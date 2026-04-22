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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (st *blueFieldSoftwareExtractingState) Handle(ctx context.Context, _ client.Client) error {
	if isDeleting(st.bfs) {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}

	packagePath := st.resolvePackagePath()
	if packagePath == "" {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
		conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondReady)
		return nil
	}

	outDir := st.extractOutputDir()
	// Idempotent reconcile: skip unpack if output already exists (e.g. prior run
	// extracted successfully but status did not patch to Ready yet).
	alreadyExtracted, err := isExtractOutputPresent(outDir)
	if err != nil {
		msg := fmt.Sprintf("Check extract output directory (%s/%s): %v", st.bfs.Namespace, st.bfs.Name, err)
		st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedExtractBFBReason, msg)
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareError
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady, conditions.ReasonFailure, conditions.ConditionMessage(msg))
		return err
	}
	if !alreadyExtracted {
		if err := callPldmUnpackService(ctx, packagePath, outDir); err != nil {
			msg := fmt.Sprintf("Extract PLDM firmware bundle (%s/%s) failed: %v", st.bfs.Namespace, st.bfs.Name, err)
			st.recorder.Eventf(st.bfs, corev1.EventTypeWarning, events.EventFailedExtractBFBReason, msg)
			st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareError
			conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondReady, conditions.ReasonFailure, conditions.ConditionMessage(msg))
			return err
		}
	}

	st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareReady
	st.recorder.Eventf(st.bfs, corev1.EventTypeNormal, events.EventSuccessfulExtractBFBReason,
		"Extract PLDM firmware bundle successful")
	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondReady)
	return nil
}

func (st *blueFieldSoftwareExtractingState) resolvePackagePath() string {
	packageRef := st.bfs.Status.DownloadedComponents.PldmFwBundle
	if packageRef == "" {
		packageRef = st.bfs.Spec.PldmFwBundle
	}
	if packageRef == "" {
		return ""
	}
	if isURL(packageRef) {
		fileName := butil.DefaultComponentFilename(st.bfs, butil.ComponentTypeFwBundle)
		return generateComponentFilePath(fileName)
	}
	return packageRef
}

func (st *blueFieldSoftwareExtractingState) extractOutputDir() string {
	return extractOutputDirForBFS(st.bfs)
}

// extractOutputDirForBFS returns the on-disk directory where PLDM unpack output
// is written for this BlueFieldSoftware (shared with deleting cleanup).
func extractOutputDirForBFS(bfs *provisioningv1.BlueFieldSoftware) string {
	if bfs == nil {
		return ""
	}
	return filepath.Join(string(os.PathSeparator), cutil.BFBBaseDir, "components",
		fmt.Sprintf("%s-%s-fwbundle-extracted", bfs.Namespace, bfs.Name))
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

func callPldmUnpackService(ctx context.Context, packagePath, outDir string) error {
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
		return fmt.Errorf("marshal unpack request: %w", err)
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
		return fmt.Errorf("build unpack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call unpack service: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read unpack response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unpack service returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed unpackResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("parse unpack response: %w", err)
	}
	if !parsed.Success {
		return fmt.Errorf("unpack failed (exitCode=%d): %s %s %s", parsed.ExitCode, parsed.Error, parsed.Stdout, parsed.Stderr)
	}
	return nil
}
