/*
Copyright 2025 NVIDIA.

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

package cniinstaller

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

const (
	// sourceCNIBinDir represents the path where the CNIInstaller expects the CNIs to exist. This path is used as a source
	// for copying the manifests to the destinationCNIBinDir.
	sourceCNIBinDir = "/opt/cnis"
	// destinationCNIBinDir represents the path where the CNIInstaller copies the enabled CNIs.
	destinationCNIBinDir = "/opt/cni/bin"
)

type CNIInstaller struct {
	// FileSystemRoot controls the file system root. It's used for enabling easier testing of the package. Defaults to
	// empty.
	FileSystemRoot string

	// cnis controls which CNIs should be installed
	cnis cnis
}

// cnis controls which CNIs should be installed
type cnis struct {
	// RDMA enables installation of RDMA CNI
	RDMA bool
}

// GetRDMABinaryName returns the binary name of the RDMA CNI
func (c *cnis) GetRDMABinaryName() string {
	return "rdma"
}

// New creates a CNIInstaller that can copy CNIs to the host
func New() *CNIInstaller {
	return &CNIInstaller{
		FileSystemRoot: "",
		cnis: cnis{
			RDMA: true,
		},
	}
}

// DisableRDMA disables the installation of the RDMA CNI
func (c *CNIInstaller) DisableRDMA() {
	c.cnis.RDMA = false
}

// Install copies the CNIs to the host
func (c *CNIInstaller) Install() error {
	// Validate that destination directory exists
	if err := c.validateDestinationDirectory(); err != nil {
		return fmt.Errorf("failed to validate destination directory: %w", err)
	}

	// Validate that all the enabled CNIs are present
	if err := c.validateCNIsPresence(); err != nil {
		return fmt.Errorf("failed to validate CNI presence: %w", err)
	}

	// Copy CNIs to the destination bin dir
	if err := c.copyCNIs(); err != nil {
		return fmt.Errorf("failed to copy CNIs to host: %w", err)
	}

	return nil
}

// validateDestinationDirectory validates that the destination directory exists
func (c *CNIInstaller) validateDestinationDirectory() error {
	destDir := filepath.Join(c.FileSystemRoot, destinationCNIBinDir)
	if _, err := os.Stat(destDir); err != nil {
		return fmt.Errorf("destination directory %s does not exist: %w", destDir, err)
	}
	return nil
}

// validateCNIsPresence validates that all the enabled CNIs are present in the path the installer expects them to be
func (c *CNIInstaller) validateCNIsPresence() error {
	if c.cnis.RDMA {
		rdmaCNIPath := filepath.Join(c.FileSystemRoot, sourceCNIBinDir, c.cnis.GetRDMABinaryName())
		if _, err := os.Stat(rdmaCNIPath); err != nil {
			return fmt.Errorf("failed to stat %s: %w", rdmaCNIPath, err)
		}
	}
	return nil
}

// copyCNIs copies the enabled CNIs to the path the host cni bin dir is mounted
func (c *CNIInstaller) copyCNIs() error {
	if c.cnis.RDMA {
		if err := c.copyCNIBinary(c.cnis.GetRDMABinaryName()); err != nil {
			return fmt.Errorf("failed to copy RDMA CNI: %w", err)
		}
	}

	return nil
}

// copyCNIBinary copies a CNI binary from source to destination
func (c *CNIInstaller) copyCNIBinary(binaryName string) error {
	sourcePath := filepath.Join(c.FileSystemRoot, sourceCNIBinDir, binaryName)
	destDirPath := filepath.Join(c.FileSystemRoot, destinationCNIBinDir)
	destPath := filepath.Join(destDirPath, binaryName)

	// Check if destination file already exists and has same checksum
	if filesHaveSameChecksum(sourcePath, destPath) {
		klog.Warningf("skipping copy of CNI %s because it already exists with the same checksum", binaryName)
		return nil
	}

	sourceContent, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source file %s: %w", sourcePath, err)
	}

	// Create temporary file with the content of the CNI in the destination folder to ensure atomic operation.
	cniTmpFile, err := os.CreateTemp(destDirPath, fmt.Sprintf("cni-installer-%s-*", binaryName))
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		if errz := os.Remove(cniTmpFile.Name()); errz != nil {
			err = kerrors.NewAggregate([]error{err, fmt.Errorf("failed to remove temporary file %s: %w", cniTmpFile.Name(), errz)})
		}
	}()

	if err := os.WriteFile(cniTmpFile.Name(), sourceContent, 0755); err != nil {
		return fmt.Errorf("failed to write to temporary file %s: %w", cniTmpFile.Name(), err)
	}

	if err := cniTmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file %s: %w", cniTmpFile.Name(), err)
	}

	// Move temporary file to destination
	if err := os.Rename(cniTmpFile.Name(), destPath); err != nil {
		return fmt.Errorf("failed to move temporary file %s to %s: %w", cniTmpFile.Name(), destPath, err)
	}

	// In case the file already exists, we need to set the permissions to 0755
	if err := os.Chmod(destPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions on existing file %s: %w", destPath, err)
	}

	return nil
}

// filesHaveSameChecksum checks if two files have the same SHA256 checksum
func filesHaveSameChecksum(sourcePath, destPath string) bool {
	// Check if destination file exists
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return false
	}

	// Calculate source file checksum
	sourceChecksum, err := calculateFileChecksum(sourcePath)
	if err != nil {
		return false
	}

	// Calculate destination file checksum
	destChecksum, err := calculateFileChecksum(destPath)
	if err != nil {
		return false
	}

	return sourceChecksum == destChecksum
}

// calculateFileChecksum calculates the SHA256 checksum of a file
func calculateFileChecksum(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash), nil
}
