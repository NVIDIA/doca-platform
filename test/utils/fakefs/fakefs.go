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

package fakefs

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// DirEntry describes dir in the fake fs
type DirEntry struct {
	// Path defines absolute path to the dir
	Path string
}

// FileEntry describes file in the fake fs
type FileEntry struct {
	// Path defines absolute path to the file
	// note: parent dir for the file should be defined as DirEntry
	Path string
	// Data defines content of the file
	Data []byte
}

// SymlinkEntry describes symlink in the fake fs
type SymlinkEntry struct {
	// OldPath defines source of the link
	// note: this file/dir should be defined with FileEntry or DirEntry
	OldPath string
	// OldPath defines link
	NewPath string
}

// Config contains configuration for the fake fs
type Config struct {
	// Dirs to create in fake fs
	Dirs []DirEntry
	// Files to create in fake fs.
	Files []FileEntry
	// Symlinks to create in fake fs
	Symlinks []SymlinkEntry
}

// FakeFs store info about fake fs
type FakeFs struct {
	rootDir   string
	cleanFunc func() error
}

// GetRootDir returns Root Dir of the fake fs
func (f *FakeFs) GetRootDir() string {
	return f.rootDir
}

// Clean removes the fake fs
func (f *FakeFs) Clean() error {
	return f.cleanFunc()
}

// GetRealPath returns the real path for the file/dir
func (f *FakeFs) GetRealPath(path string) string {
	return filepath.Join(f.rootDir, path)
}

// Create function creates fake file system hierarchy within temporary dir
// Example usage:
// ```
// fakeFSRoot, clean, err := fakefs.Create(fakefs.Config{})
// if err != nil { ... }
// defer clean()
// ````
func Create(cfg Config) (*FakeFs, error) {
	// create the new fake fs root dir in /tmp/sriov...
	rootDir, err := os.MkdirTemp("", "fakefs*")
	if err != nil {
		return nil, fmt.Errorf("error creating fake root dir: %w", err)
	}
	if err := create(rootDir, &cfg); err != nil {
		_ = os.RemoveAll(rootDir)
		return nil, err
	}
	return &FakeFs{
		rootDir: rootDir,
		cleanFunc: func() error {
			if err := os.RemoveAll(rootDir); err != nil {
				return fmt.Errorf("error removing fake filesystem: %w", err)
			}
			return nil
		}}, nil
}

func create(rootDir string, cfg *Config) error {
	for _, d := range cfg.Dirs {
		if err := os.MkdirAll(filepath.Join(rootDir, d.Path), 0755); err != nil {
			return fmt.Errorf("error creating fake directory: %w", err)
		}
	}
	for _, f := range cfg.Files {
		if err := os.WriteFile(filepath.Join(rootDir, f.Path), f.Data, 0644); err != nil {
			return fmt.Errorf("error creating fake file: %w", err)
		}
	}
	for _, s := range cfg.Symlinks {
		oldPath := s.OldPath
		if filepath.IsAbs(oldPath) {
			oldPath = filepath.Join(rootDir, oldPath)
		}
		if err := os.Symlink(oldPath, filepath.Join(rootDir, s.NewPath)); err != nil {
			return fmt.Errorf("error creating fake symlink: %w", err)
		}
	}
	return nil
}

// GinkgoConfigureFakeFS configure fake filesystem and register Ginkgo DeferCleanup.
// set the rootFsVar to the root of the fakeFS
func GinkgoConfigureFakeFS(rootFsVar *string, cfg Config) *FakeFs {
	f, err := Create(cfg)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, f).NotTo(BeNil())
	if rootFsVar != nil {
		origRootFs := *rootFsVar
		*rootFsVar = f.GetRootDir()
		DeferCleanup(func() {
			*rootFsVar = origRootFs
		})
	}
	DeferCleanup(func() {
		ExpectWithOffset(1, f.Clean()).NotTo(HaveOccurred())
	})
	return f
}

// GinkgoFakeFsFileContent helper function to check content of the file
// in the fakeFs.
func GinkgoFakeFsFileContent(fs *FakeFs, path string) Assertion {
	realFilePath := fs.GetRealPath(path)
	data, err := os.ReadFile(realFilePath)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return Expect(string(data))
}
