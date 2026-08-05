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

// cniNames are the CNIs the image stages, the DPF owned ones plus the standard containernetworking
// ones. This is a golden list, deliberately spelled out instead of read from the installer, so that
// dropping a CNI from the cnis list in cniinstaller.go fails the tests instead of silently changing
// what the DPU gets. Changing it means changing cniinstaller.go and Dockerfile.cni-installer too.
var cniNames = []string{"rdma", "ovs", "host-device", "loopback", "dhcp", "static", "vrf"}

// cniEntries returns one DescribeTable entry per shipped CNI, so dropping a COPY from the
// Dockerfile is caught.
func cniEntries() []TableEntry {
	entries := []TableEntry{}
	for _, name := range cniNames {
		entries = append(entries, Entry(nil, name))
	}
	return entries
}

// cniContent returns the fixture content of a CNI binary
func cniContent(name string) []byte {
	return []byte("#!/usr/bin/env bash\necho '" + name + " CNI binary'")
}

var _ = Describe("CNI Installer", func() {
	Context("When installing CNIs", func() {
		var tmpDir string
		var sourceCniDir string
		var destCniDir string
		var installer *cniinstaller.CNIInstaller

		BeforeEach(func() {
			var err error
			tmpDir, err = os.MkdirTemp("", "cniinstaller-test")
			Expect(err).NotTo(HaveOccurred())
			installer = cniinstaller.New()
			installer.FileSystemRoot = tmpDir
			sourceCniDir = filepath.Join(tmpDir, "/opt/cnis")
			destCniDir = filepath.Join(tmpDir, "/opt/cni/bin")
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tmpDir)).ToNot(HaveOccurred())
		})

		// writeSourceCNIs creates a fixture binary for every CNI the image stages
		writeSourceCNIs := func() {
			Expect(os.MkdirAll(sourceCniDir, 0755)).To(Succeed())
			for _, name := range cniNames {
				Expect(os.WriteFile(filepath.Join(sourceCniDir, name), cniContent(name), 0755)).To(Succeed())
			}
		}

		It("should copy every CNI found in the source directory", func() {
			writeSourceCNIs()
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			Expect(installer.Install()).To(Succeed())

			for _, name := range cniNames {
				destPath := filepath.Join(destCniDir, name)
				Expect(destPath).To(BeAnExistingFile())

				destContent, err := os.ReadFile(destPath)
				Expect(err).ToNot(HaveOccurred())
				Expect(destContent).To(Equal(cniContent(name)))

				destInfo, err := os.Stat(destPath)
				Expect(err).ToNot(HaveOccurred())
				Expect(destInfo.Mode() & 0777).To(Equal(os.FileMode(0755)))
			}
		})

		It("should copy only relevant CNIs when source contains extra CNIs", func() {
			writeSourceCNIs()
			// Extra CNI and subdirectory, neither is relevant to this installer
			Expect(os.WriteFile(filepath.Join(sourceCniDir, "extra"), cniContent("extra"), 0755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(sourceCniDir, "somedir"), 0755)).To(Succeed())
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			Expect(installer.Install()).To(Succeed())

			destFiles, err := os.ReadDir(destCniDir)
			Expect(err).ToNot(HaveOccurred())
			destNames := []string{}
			for _, destFile := range destFiles {
				destNames = append(destNames, destFile.Name())
			}
			Expect(destNames).To(ConsistOf(cniNames))
		})

		It("should fail when the source directory doesn't exist", func() {
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())
			// Note: source directory is not created

			err := installer.Install()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to validate CNI presence"))
			Expect(err.Error()).To(ContainSubstring("failed to stat"))
		})

		DescribeTable("should fail when a CNI is missing from source",
			func(missing string) {
				writeSourceCNIs()
				Expect(os.Remove(filepath.Join(sourceCniDir, missing))).To(Succeed())
				Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

				err := installer.Install()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to validate CNI presence"))
				Expect(err.Error()).To(ContainSubstring("failed to stat"))
				Expect(err.Error()).To(ContainSubstring(missing))
			},
			EntryDescription("without %s"),
			cniEntries(),
		)

		It("should fail when destination directory doesn't exist", func() {
			writeSourceCNIs()
			// Note: Destination directory is not created

			err := installer.Install()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to validate destination directory"))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("should not copy binary if destination already exists with same checksum", func() {
			writeSourceCNIs()
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// First install - should copy the file
			Expect(installer.Install()).To(Succeed())

			rdmaDestPath := filepath.Join(destCniDir, "rdma")
			Expect(rdmaDestPath).To(BeAnExistingFile())

			// Get the modification time of the destination file
			destInfo, err := os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			originalModTime := destInfo.ModTime()

			// Second install - should not copy since file already exists with same content
			Expect(installer.Install()).To(Succeed())

			// Verify file still exists and modification time hasn't changed
			destInfo, err = os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destInfo.ModTime()).To(Equal(originalModTime))
		})

		It("should make the binary executable if destination already exists with same checksum", func() {
			writeSourceCNIs()
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Destination has the expected content, but is not executable. This is what a
			// binary put there by something other than this installer can look like.
			rdmaDestPath := filepath.Join(destCniDir, "rdma")
			Expect(os.WriteFile(rdmaDestPath, cniContent("rdma"), 0644)).To(Succeed())
			destInfo, err := os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			originalModTime := destInfo.ModTime()

			Expect(installer.Install()).To(Succeed())

			// The content matches, so the file is not rewritten, but the mode is corrected.
			destInfo, err = os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destInfo.ModTime()).To(Equal(originalModTime))
			Expect(destInfo.Mode() & 0777).To(Equal(os.FileMode(0755)))
		})

		It("should copy binary if destination exists but has different checksum", func() {
			writeSourceCNIs()
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Create destination file with different content and permissions
			rdmaDestPath := filepath.Join(destCniDir, "rdma")
			Expect(os.WriteFile(rdmaDestPath, []byte("#!/usr/bin/env bash\necho 'Different RDMA CNI binary'"), 0644)).To(Succeed())

			// Install - should copy since content is different
			Expect(installer.Install()).To(Succeed())

			// Verify content was updated to match source
			destContent, err := os.ReadFile(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destContent).To(Equal(cniContent("rdma")))

			// Verify permissions were set to 755
			destInfo, err := os.Stat(rdmaDestPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(destInfo.Mode() & 0777).To(Equal(os.FileMode(0755)))
		})

		It("should not leave temporary files after successful copy", func() {
			writeSourceCNIs()
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			Expect(installer.Install()).To(Succeed())

			// Should only contain the expected CNI binaries, no temporary files
			destFiles, err := os.ReadDir(destCniDir)
			Expect(err).ToNot(HaveOccurred())
			destNames := []string{}
			for _, destFile := range destFiles {
				destNames = append(destNames, destFile.Name())
			}
			Expect(destNames).To(ConsistOf(cniNames))
		})

		It("should not leave temporary files after failed copy", func() {
			writeSourceCNIs()
			Expect(os.MkdirAll(destCniDir, 0755)).To(Succeed())

			// Occupy the destination of the first copied CNI with a directory. Renaming a file
			// onto a directory fails, so the copy fails after the temporary file was created,
			// which is the only case where the deferred cleanup has anything to remove. rdma is
			// copied first, so the install fails before any other CNI reaches the destination.
			Expect(os.MkdirAll(filepath.Join(destCniDir, "rdma"), 0755)).To(Succeed())

			err := installer.Install()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to move temporary file"))

			destFiles, err := os.ReadDir(destCniDir)
			Expect(err).ToNot(HaveOccurred())

			// Should only contain the pre-existing entry, no temporary files
			Expect(destFiles).To(HaveLen(1))
			Expect(destFiles[0].Name()).To(Equal("rdma"))
		})
	})
})
