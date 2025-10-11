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

package cniinstaller_test

import (
	"os"
	"path/filepath"

	cniinstaller "github.com/nvidia/doca-platform/internal/cniinstaller"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CNI Installer", func() {
	Context("When installing CNIs", func() {
		var tmpDir string
		var installer *cniinstaller.CNIInstaller

		BeforeEach(func() {
			var err error
			tmpDir, err = os.MkdirTemp("", "cniinstaller-test")
			Expect(err).NotTo(HaveOccurred())
			installer = cniinstaller.New()
			installer.FileSystemRoot = tmpDir
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tmpDir)).ToNot(HaveOccurred())
		})

		It("should copy all enabled CNIs when source contains all required CNIs", func() {
			// Create source CNI directory with RDMA CNI (enabled by default)
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			Expect(os.WriteFile(rdmaSourcePath, []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'"), 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Install CNIs
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			// Verify destination contains the enabled CNI
			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(rdmaDestPath).To(BeAnExistingFile())

			// Verify content was copied correctly
			destContent, err := os.ReadFile(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destContent).To(Equal([]byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'")))

			// Verify permissions were set to 755
			destInfo, err := os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			finalMode := destInfo.Mode()
			Expect(finalMode & 0777).To(Equal(os.FileMode(0755)))
		})

		It("should copy only relevant CNIs when source contains extra CNIs", func() {
			// Create source CNI directory with RDMA CNI (enabled) + extra CNI (not relevant)
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())

			// RDMA CNI (enabled by default)
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			Expect(os.WriteFile(rdmaSourcePath, []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'"), 0755)).To(Succeed())

			// Extra CNI (not relevant to this installer)
			extraSourcePath := filepath.Join(sourceCniDir, "extra")
			Expect(os.WriteFile(extraSourcePath, []byte("#!/usr/bin/env bash\necho 'Extra CNI binary'"), 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Install CNIs
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			// Verify destination contains only the enabled CNI
			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(rdmaDestPath).To(BeAnExistingFile())

			// Verify extra CNI was not copied
			extraDestPath := filepath.Join(tmpDir, "/opt/cni/bin/extra")
			Expect(extraDestPath).NotTo(BeAnExistingFile())
		})

		It("should fail when source is missing an enabled CNI", func() {
			// Create source CNI directory but without RDMA CNI (which is enabled by default)
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			// Note: No RDMA CNI binary created

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Install should fail
			err := installer.Install()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to validate CNI presence"))
			Expect(err.Error()).To(ContainSubstring("failed to stat"))
		})

		It("should not fail when source is missing a disabled CNI", func() {
			// Disable RDMA CNI
			installer.DisableRDMA()

			// Create source CNI directory without RDMA CNI (which is now disabled)
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			// Note: No RDMA CNI binary created

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Install should succeed
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(rdmaDestPath).NotTo(BeAnExistingFile())
		})

		It("should not copy disabled CNI even when source contains it", func() {
			// Disable RDMA CNI
			installer.DisableRDMA()

			// Create source CNI directory with RDMA CNI (but it's disabled)
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			Expect(os.WriteFile(rdmaSourcePath, []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'"), 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Install should succeed
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(rdmaDestPath).NotTo(BeAnExistingFile())
		})

		It("should fail when destination directory doesn't exist", func() {
			// Create source CNI directory with RDMA CNI
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			Expect(os.WriteFile(rdmaSourcePath, []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'"), 0755)).To(Succeed())

			// Note: Destination directory is not created

			// Install should fail
			err := installer.Install()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to validate destination directory"))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("should not copy binary if destination already exists with same checksum", func() {
			// Create source CNI directory with RDMA CNI
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			rdmaContent := []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'")
			Expect(os.WriteFile(rdmaSourcePath, rdmaContent, 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// First install - should copy the file
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			// Verify file was copied
			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(rdmaDestPath).To(BeAnExistingFile())

			// Get the modification time of the destination file
			destInfo, err := os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			originalModTime := destInfo.ModTime()

			// Second install - should not copy since file already exists with same content
			err = installer.Install()
			Expect(err).ToNot(HaveOccurred())

			// Verify file still exists and modification time hasn't changed
			destInfo, err = os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destInfo.ModTime()).To(Equal(originalModTime))
		})

		It("should copy binary if destination exists but has different checksum", func() {
			// Create source CNI directory with RDMA CNI
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			rdmaContent := []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'")
			Expect(os.WriteFile(rdmaSourcePath, rdmaContent, 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Create destination file with different content and permissions
			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			differentContent := []byte("#!/usr/bin/env bash\necho 'Different RDMA CNI binary'")
			Expect(os.WriteFile(rdmaDestPath, differentContent, 0644)).To(Succeed())

			// Install - should copy since content is different
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			// Verify content was updated to match source
			destContent, err := os.ReadFile(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destContent).To(Equal(rdmaContent))

			// Verify permissions were set to 755
			destInfo, err := os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			finalMode := destInfo.Mode()
			Expect(finalMode & 0777).To(Equal(os.FileMode(0755)))
		})

		It("should not leave temporary files after successful copy", func() {
			// Create source CNI directory with RDMA CNI
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			rdmaContent := []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'")
			Expect(os.WriteFile(rdmaSourcePath, rdmaContent, 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Install CNIs
			err := installer.Install()
			Expect(err).ToNot(HaveOccurred())

			// Verify destination contains the CNI
			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(rdmaDestPath).To(BeAnExistingFile())

			// Check that no temporary files exist in the destination directory
			destFiles, err := os.ReadDir(destCniDir)
			Expect(err).ToNot(HaveOccurred())

			// Should only contain the expected CNI binary, no temporary files
			Expect(destFiles).To(HaveLen(1))
			Expect(destFiles[0].Name()).To(Equal("rdma"))
		})

		It("should not leave temporary files after failed copy", func() {
			// Create source CNI directory with RDMA CNI
			sourceCniDir := filepath.Join(tmpDir, "/opt/cnis")
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			rdmaSourcePath := filepath.Join(sourceCniDir, "rdma")
			rdmaContent := []byte("#!/usr/bin/env bash\necho 'RDMA CNI binary'")
			Expect(os.WriteFile(rdmaSourcePath, rdmaContent, 0755)).To(Succeed())

			// Create destination directory
			destCniDir := filepath.Join(tmpDir, "/opt/cni/bin")
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Create a read-only destination file to simulate a failure scenario
			rdmaDestPath := filepath.Join(tmpDir, "/opt/cni/bin/rdma")
			Expect(os.WriteFile(rdmaDestPath, []byte("existing"), 0444)).To(Succeed())

			// Make the destination directory read-only to cause a failure during rename
			Expect(os.Chmod(destCniDir, 0444)).To(Succeed())

			// Install should fail due to read-only destination directory
			err := installer.Install()
			Expect(err).To(HaveOccurred())

			// Check that no temporary files exist in the destination directory
			// First restore permissions to be able to read the directory
			Expect(os.Chmod(destCniDir, 0755)).To(Succeed())

			destFiles, err := os.ReadDir(destCniDir)
			Expect(err).ToNot(HaveOccurred())

			// Should only contain the original file, no temporary files
			Expect(destFiles).To(HaveLen(1))
			Expect(destFiles[0].Name()).To(Equal("rdma"))
		})
	})
})
