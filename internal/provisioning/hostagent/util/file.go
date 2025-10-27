/*
Copyright 2025 NVIDIA

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

package util

import (
	"os"
	"path/filepath"
)

func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name()) //nolint: errcheck
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	// Ensure file data and metadata are flushed to stable storage before rename
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempFile.Name(), path); err != nil {
		return err
	}
	if err := os.Chmod(path, perm); err != nil {
		return err
	}
	// Fsync the directory to persist the rename operation
	dirFile, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dirFile.Close() //nolint: errcheck
	if err := dirFile.Sync(); err != nil {
		return err
	}
	return nil
}
