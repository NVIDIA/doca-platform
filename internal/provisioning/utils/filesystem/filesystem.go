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
)

var (
	DefaultFileSystem = OSFileSystem{}
)

// FileSystem abstracts file and OS operations for testability.
type FileSystem interface {
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	AtomicWrite(name string, data []byte, perm os.FileMode) error
	Open(name string) (*os.File, error)
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(name string) ([]os.DirEntry, error)
	Remove(name string) error
}

func Stat(name string) (os.FileInfo, error) { return DefaultFileSystem.Stat(name) }
func ReadFile(name string) ([]byte, error)  { return DefaultFileSystem.ReadFile(name) }
func WriteFile(name string, data []byte, perm os.FileMode) error {
	return DefaultFileSystem.WriteFile(name, data, perm)
}
func Open(name string) (*os.File, error)           { return DefaultFileSystem.Open(name) }
func MkdirAll(path string, perm os.FileMode) error { return DefaultFileSystem.MkdirAll(path, perm) }
func ReadDir(name string) ([]os.DirEntry, error)   { return DefaultFileSystem.ReadDir(name) }
func Remove(name string) error                     { return DefaultFileSystem.Remove(name) }
func AtomicWrite(name string, data []byte, perm os.FileMode) error {
	return DefaultFileSystem.AtomicWrite(name, data, perm)
}

// OSFileSystem is the default implementation of FileSystem using the os package.
type OSFileSystem struct{}

func (OSFileSystem) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }
func (OSFileSystem) ReadFile(name string) ([]byte, error)  { return os.ReadFile(name) }
func (OSFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (OSFileSystem) Open(name string) (*os.File, error)           { return os.Open(name) }
func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFileSystem) ReadDir(name string) ([]os.DirEntry, error)   { return os.ReadDir(name) }
func (OSFileSystem) Remove(name string) error                     { return os.Remove(name) }

func (OSFileSystem) AtomicWrite(name string, data []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+"-*.tmp")
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
	if err := os.Rename(tempFile.Name(), name); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	// Fsync the directory to persist the rename operation
	dirFile, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer dirFile.Close() //nolint: errcheck
	if err := dirFile.Sync(); err != nil {
		return err
	}
	return nil
}
