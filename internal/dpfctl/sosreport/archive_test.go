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

package sosreport

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func TestCreateArchive(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()
	sosDir := filepath.Join(dir, "sosreport-test")
	g.Expect(os.Mkdir(sosDir, 0o755)).To(Succeed())

	// Create test files.
	for _, name := range []string{"report-host-worker1.tar.gz", "report-host-worker2.tar.gz"} {
		g.Expect(os.WriteFile(filepath.Join(sosDir, name), []byte("test-content-"+name), 0o644)).To(Succeed())
	}

	archivePath, err := CreateArchive(sosDir)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(archivePath).To(Equal(sosDir + ".tar.gz"))

	// Verify the archive is a valid tar.gz with the expected files.
	f, err := os.Open(archivePath)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	files := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		g.Expect(err).NotTo(HaveOccurred())
		files[hdr.Name] = true
	}

	g.Expect(files).To(HaveKey("sosreport-test/report-host-worker1.tar.gz"))
	g.Expect(files).To(HaveKey("sosreport-test/report-host-worker2.tar.gz"))
}

func TestCreateArchive_EmptyDir(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()
	sosDir := filepath.Join(dir, "sosreport-empty")
	g.Expect(os.Mkdir(sosDir, 0o755)).To(Succeed())

	archivePath, err := CreateArchive(sosDir)
	g.Expect(err).NotTo(HaveOccurred())

	info, err := os.Stat(archivePath)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(info.Size()).NotTo(BeZero())
}
