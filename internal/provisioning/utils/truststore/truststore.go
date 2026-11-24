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

package truststore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	// BundleFileName is the local trust anchor filename on the DPU OS.
	BundleFileName = "dpf-ca.crt"
)

var (
	// debianAnchorDir and rhelAnchorDir are vars so unit tests can redirect installs into a temp dir.
	debianAnchorDir = "/usr/local/share/ca-certificates"
	rhelAnchorDir   = "/etc/pki/ca-trust/source/anchors"
)

// Backend identifies how the DPU OS refreshes its system trust store.
type Backend string

const (
	BackendDebian Backend = "debian"
	BackendRHEL   Backend = "rhel"
)

// lookPath finds an executable on PATH. Overridable in tests.
var lookPath = exec.LookPath

// runCommand runs a shell command. Overridable in tests.
var runCommand = bash.Run

// DetectBackend selects the trust store backend from commands present on the DPU OS.
func DetectBackend() (Backend, error) {
	if _, err := lookPath("update-ca-trust"); err == nil {
		return BackendRHEL, nil
	}
	if _, err := lookPath("update-ca-certificates"); err == nil {
		return BackendDebian, nil
	}
	return "", fmt.Errorf("no supported CA trust store command found (checked update-ca-trust and update-ca-certificates)")
}

// AnchorPath returns where the DPF CA bundle file is installed for the given backend.
func AnchorPath(backend Backend) string {
	switch backend {
	case BackendRHEL:
		return filepath.Join(rhelAnchorDir, BundleFileName)
	default:
		return filepath.Join(debianAnchorDir, BundleFileName)
	}
}

// RefreshCommand returns the command that reapplies the OS trust store after the anchor file changes.
func RefreshCommand(backend Backend) string {
	switch backend {
	case BackendRHEL:
		return "update-ca-trust extract"
	default:
		return "update-ca-certificates"
	}
}

// Apply writes bundlePEM to the OS-specific anchor path and refreshes the system trust store.
func Apply(bundlePEM string) error {
	backend, err := DetectBackend()
	if err != nil {
		return err
	}

	path := AnchorPath(backend)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create trust bundle directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(bundlePEM), 0o644); err != nil {
		return fmt.Errorf("write trust bundle file %s: %w", path, err)
	}
	klog.Infof("CA trust bundle: wrote %s (backend=%s)", path, backend)

	cmd := RefreshCommand(backend)
	stdout, stderr, err := runCommand(cmd)
	if err != nil {
		return fmt.Errorf("run %s: %w, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	}
	return nil
}