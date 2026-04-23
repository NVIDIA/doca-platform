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
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CreateArchive creates a .tar.gz archive of the given directory.
// The archive is written next to the directory (e.g., sosreport-xxx.tar.gz).
// Returns the path to the created archive.
func CreateArchive(dir string) (string, error) {
	archivePath := dir + ".tar.gz"

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	if err := addDirToArchive(tw, dir); err != nil {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("create archive: %w", err)
	}

	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("finalize tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", fmt.Errorf("finalize gzip: %w", err)
	}

	return archivePath, nil
}

// addDirToArchive walks the directory tree and writes each entry to the tar writer.
func addDirToArchive(tw *tar.Writer, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := writeEntryToArchive(tw, dir, path, info); err != nil {
			return err
		}
		return nil
	})
}

// writeEntryToArchive writes a single file or directory entry to the tar writer.
func writeEntryToArchive(tw *tar.Writer, baseDir, path string, info os.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("create tar header for %s: %w", path, err)
	}

	rel, err := filepath.Rel(filepath.Dir(baseDir), path)
	if err != nil {
		return err
	}
	// Ensure forward slashes in tar headers for Windows compatibility.
	header.Name = filepath.ToSlash(rel)

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %s: %w", path, err)
	}

	if info.IsDir() {
		return nil
	}

	return writeFileContent(tw, path)
}

// writeFileContent copies the contents of a file into the tar writer.
func writeFileContent(tw *tar.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("write %s to archive: %w", path, err)
	}
	return nil
}
