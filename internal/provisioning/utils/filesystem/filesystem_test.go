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

package filesystem

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FilesystemHelper", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "util-file-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("AtomicWrite", Label("AtomicWrite"), func() {
		It("should write data to a new file", func() {
			path := filepath.Join(tempDir, "test-file.txt")
			data := []byte("test content")

			err := AtomicWrite(path, data, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(data))
		})

		It("should overwrite an existing file", func() {
			path := filepath.Join(tempDir, "existing-file.txt")

			err := os.WriteFile(path, []byte("initial content"), 0644)
			Expect(err).NotTo(HaveOccurred())

			newData := []byte("new content")
			err = AtomicWrite(path, newData, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(newData))
		})

		It("should set correct file permissions", func() {
			path := filepath.Join(tempDir, "permissions-test.txt")
			data := []byte("test content")

			err := AtomicWrite(path, data, 0600)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		It("should write empty data", func() {
			path := filepath.Join(tempDir, "empty-file.txt")
			data := []byte{}

			err := AtomicWrite(path, data, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(BeEmpty())
		})

		It("should write binary data", func() {
			path := filepath.Join(tempDir, "binary-file.bin")
			data := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}

			err := AtomicWrite(path, data, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(data))
		})

		It("should fail when directory does not exist", func() {
			path := filepath.Join(tempDir, "nonexistent", "subdir", "file.txt")
			data := []byte("test content")

			err := AtomicWrite(path, data, 0644)
			Expect(err).To(HaveOccurred())
		})

		It("should not leave temp files on success", func() {
			path := filepath.Join(tempDir, "clean-test.txt")
			data := []byte("test content")

			err := AtomicWrite(path, data, 0644)
			Expect(err).NotTo(HaveOccurred())

			entries, err := os.ReadDir(tempDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Name()).To(Equal("clean-test.txt"))
		})

		It("should write large data", func() {
			path := filepath.Join(tempDir, "large-file.txt")
			data := make([]byte, 1024*1024)
			for i := range data {
				data[i] = byte(i % 256)
			}

			err := AtomicWrite(path, data, 0644)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(data))
		})

		It("should remove a file", func() {
			path := filepath.Join(tempDir, "test-file.txt")
			data := []byte("test content")

			err := WriteFile(path, data, 0644)
			Expect(err).NotTo(HaveOccurred())

			err = Remove(path)
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(path)
			Expect(err).To(HaveOccurred())
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})
