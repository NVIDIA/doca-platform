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

package vfmac

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	networkhelper_mock "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"

	"github.com/BurntSushi/toml"
	"github.com/go-logr/logr"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"go.uber.org/mock/gomock"
)

// Test Strategy Documentation
/*
This test suite follows these principles:
1. Unit Tests: Each function is tested in isolation using mocks
2. Table-Driven Tests: Complex functions use table-driven tests for better coverage
3. Error Cases: Each function tests both success and failure paths
4. Mocking: External dependencies (filesystem) are mocked
5. Cleanup: All mocks are properly cleaned up after tests

Test Categories:
- Basic Validation Tests: Simple input validation (MAC addresses, etc.)
- Filesystem Tests: File operations with various error conditions
- Integration Tests: End-to-end functionality testing
*/

// TestFileSystem interface defines the filesystem operations needed for testing
type TestFileSystem interface {
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Open(name string) (*os.File, error)
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(name string) ([]os.DirEntry, error)
}

// mockFS implements TestFileSystem interface for testing
type mockFS struct {
	files map[string][]byte
	dirs  map[string]bool
	// Add error injection capabilities
	statErr  error
	readErr  error
	writeErr error
	openErr  error
	mkdirErr error
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() os.FileMode          { return 0755 }
func (m *mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func (m *mockFS) Stat(name string) (os.FileInfo, error) {
	if m.statErr != nil {
		return nil, m.statErr
	}
	if _, ok := m.files[name]; ok {
		return &mockFileInfo{name: name, isDir: false}, nil
	}
	if _, ok := m.dirs[name]; ok {
		return &mockFileInfo{name: name, isDir: true}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFS) ReadFile(name string) ([]byte, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	if data, ok := m.files[name]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.files[name] = data
	return nil
}

func (m *mockFS) Open(name string) (*os.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	if _, ok := m.dirs[name]; ok {
		return &os.File{}, nil
	}
	if _, ok := m.files[name]; ok {
		return &os.File{}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFS) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirErr != nil {
		return m.mkdirErr
	}
	m.dirs[path] = true
	return nil
}

func (m *mockFS) ReadDir(name string) ([]os.DirEntry, error) {
	if _, ok := m.dirs[name]; !ok {
		return nil, os.ErrNotExist
	}

	var entries []os.DirEntry
	// Check for VF directories
	for dir := range m.dirs {
		if strings.HasPrefix(dir, name+"/vf") {
			entries = append(entries, &mockDirEntry{
				name:  filepath.Base(dir),
				isDir: true,
			})
		}
	}
	return entries, nil
}

type mockFileInfo struct {
	name  string
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return 0644 }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

const (
	testMAC = "fa:b0:b2:04:9f:b1"
)

// newTestVFMAC creates a VFMAC instance directly with the given ECPFs, bypassing
// discoverECPFs which requires real sysfs access.
func newTestVFMAC(fs FileSystem, ecpfs []string, configDir, configFile string) *VFMAC {
	if configDir == "" {
		configDir = "/etc/mellanox"
	}
	if configFile == "" {
		configFile = "dpf-vf-mac-mapping.toml"
	}
	return &VFMAC{
		fs:         fs,
		configDir:  configDir,
		configFile: configFile,
		ecpfs:      ecpfs,
		log:        logr.Discard(),
	}
}

func TestNewVFMAC(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) *networkhelper_mock.MockNetworkHelper
		wantErr bool
	}{
		{
			name: "success - no ECPFs discovered",
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return(nil, nil).AnyTimes()
				return mock
			},
			wantErr: false,
		},
		{
			name: "error - DevlinkPortList fails",
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return(nil, fmt.Errorf("mock error")).AnyTimes()
				return mock
			},
			wantErr: true,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			mockNetworkHelper := tcase.setup(t)
			mfs := &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}
			_, err := NewVFMAC(mfs, mockNetworkHelper, logr.Discard(), "", "")
			if (err != nil) != tcase.wantErr {
				t.Errorf("NewVFMAC() error = %v, wantErr %v", err, tcase.wantErr)
			}
		})
	}
}

func TestIsValidMAC(t *testing.T) {
	tests := []struct {
		name     string
		mac      string
		expected bool
	}{
		{
			name:     "valid MAC with colons",
			mac:      "fa:b0:b2:04:9f:b1",
			expected: true,
		},
		{
			name:     "valid MAC with hyphens",
			mac:      "fa-b0-b2-04-9f-b1",
			expected: true,
		},
		{
			name:     "invalid MAC format",
			mac:      "notamac",
			expected: false,
		},
		{
			name:     "empty MAC",
			mac:      "",
			expected: false,
		},
		{
			name:     "MAC with wrong length",
			mac:      "fa:b0:b2:04:9f",
			expected: false,
		},
		{
			name:     "MAC with invalid characters",
			mac:      "fa:b0:b2:04:9f:g1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidMAC(tt.mac); got != tt.expected {
				t.Errorf("isValidMAC(%q) = %v, want %v", tt.mac, got, tt.expected)
			}
		})
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	tests := []struct {
		name           string
		mapping        VFMapping
		mockFS         *mockFS
		wantErr        bool
		verifyContents bool
	}{
		{
			name: "basic config save and load",
			mapping: VFMapping{
				"p0": ECPFConfig{"vf0": VFConfig{MAC: testMAC}},
				"p1": ECPFConfig{"vf0": VFConfig{MAC: "da:f2:ea:53:cf:40"}},
			},
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs:  make(map[string]bool),
			},
			wantErr:        false,
			verifyContents: true,
		},
		{
			name: "empty config",
			mapping: VFMapping{
				"p0": ECPFConfig{},
				"p1": ECPFConfig{},
			},
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs:  make(map[string]bool),
			},
			wantErr:        false,
			verifyContents: true,
		},
		{
			name: "multiple VFs per ECPF",
			mapping: VFMapping{
				"p0": ECPFConfig{
					"vf0": VFConfig{MAC: testMAC},
					"vf1": VFConfig{MAC: "da:f2:ea:53:cf:40"},
				},
				"p1": ECPFConfig{
					"vf0": VFConfig{MAC: "aa:bb:cc:dd:ee:ff"},
					"vf1": VFConfig{MAC: "11:22:33:44:55:66"},
				},
			},
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs:  make(map[string]bool),
			},
			wantErr:        false,
			verifyContents: true,
		},
		{
			name: "save error - permission denied",
			mapping: VFMapping{
				"p0": ECPFConfig{"vf0": VFConfig{MAC: testMAC}},
				"p1": ECPFConfig{"vf0": VFConfig{MAC: "da:f2:ea:53:cf:40"}},
			},
			mockFS: &mockFS{
				files:    make(map[string][]byte),
				dirs:     make(map[string]bool),
				writeErr: os.ErrPermission,
			},
			wantErr:        true,
			verifyContents: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vfmac := newTestVFMAC(tt.mockFS, []string{"p0", "p1"}, "/test/config/dir", "test-config.toml")

			// Test save
			err := vfmac.saveConfig(tt.mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("saveConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Test load
			loaded, err := vfmac.loadConfig()
			if err != nil {
				t.Errorf("LoadConfig() error = %v", err)
				return
			}

			if tt.verifyContents {
				for ecpf, ecpfConfig := range tt.mapping {
					for vf, config := range ecpfConfig {
						if loaded[ecpf][vf].MAC != config.MAC {
							t.Errorf("%s[%s].MAC = %v, want %v", ecpf, vf, loaded[ecpf][vf].MAC, config.MAC)
						}
					}
				}
			}
		})
	}
}

func TestLoadIfaceMACAddressMapping(t *testing.T) {
	tests := []struct {
		name           string
		ecpfs          []string
		mockFS         *mockFS
		wantErr        bool
		verifyContents bool
	}{
		{
			name:  "basic config load from smart_nic",
			ecpfs: []string{"p0", "p1"},
			mockFS: &mockFS{
				files: map[string][]byte{
					"/sys/class/net/p0/smart_nic/vf0/config": []byte("MAC: fa:b0:b2:04:9f:b1\n"),
					"/sys/class/net/p0/smart_nic/vf1/config": []byte("MAC: fa:b0:b2:04:9f:b2\n"),
					"/sys/class/net/p0/smart_nic/pf/config":  []byte("MAC: fa:b0:b2:04:9f:b3\n"),
					"/sys/class/net/p1/smart_nic/vf0/config": []byte("MAC: fa:b0:b2:04:9f:b4\n"),
					"/sys/class/net/p1/smart_nic/vf1/config": []byte("MAC: fa:b0:b2:04:9f:b5\n"),
					"/sys/class/net/p1/smart_nic/pf/config":  []byte("MAC: fa:b0:b2:04:9f:b6\n"),
				},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":     true,
					"/sys/class/net/p0/smart_nic/vf0": true,
					"/sys/class/net/p0/smart_nic/vf1": true,
					"/sys/class/net/p0/smart_nic/pf":  true,
					"/sys/class/net/p1/smart_nic":     true,
					"/sys/class/net/p1/smart_nic/vf0": true,
					"/sys/class/net/p1/smart_nic/vf1": true,
					"/sys/class/net/p1/smart_nic/pf":  true,
				},
			},
			wantErr:        false,
			verifyContents: true,
		},
		{
			name:  "empty config",
			ecpfs: []string{"p0", "p1"},
			mockFS: &mockFS{
				files: map[string][]byte{},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic": true,
					"/sys/class/net/p1/smart_nic": true,
				},
			},
			wantErr:        false,
			verifyContents: true,
		},
		{
			name:  "invalid MAC",
			ecpfs: []string{"p0"},
			mockFS: &mockFS{
				files: map[string][]byte{
					"/sys/class/net/p0/smart_nic/pf/config": []byte("MAC: invalid:mac:format\n"),
				},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic": true,
				},
			},
			wantErr:        true,
			verifyContents: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vfmac := newTestVFMAC(tt.mockFS, tt.ecpfs, "/test/config/dir", "test-config.toml")

			vfmapping, err := vfmac.LoadIfaceMACAddressMapping()
			if err != nil && !tt.wantErr {
				t.Errorf("LoadIfaceMACAddressMapping() error = %v", err)
				return
			}

			if tt.verifyContents {
				for ecpf, ecpfConfig := range vfmapping {
					for iface, config := range ecpfConfig {
						if vfmapping[ecpf][iface].MAC != config.MAC {
							t.Errorf("%s[%s].MAC = %v, want %v", ecpf, iface, vfmapping[ecpf][iface].MAC, config.MAC)
						}
					}
				}
			}
		})
	}
}

func TestSetVFMAC_InvalidMAC(t *testing.T) {
	vfmac := newTestVFMAC(&mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}, []string{"p0"}, "", "")
	if err := vfmac.setVFMAC("p0", "vf0", "notamac"); err == nil {
		t.Errorf("expected error for invalid MAC, got nil")
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		setValue     string
		want         string
	}{
		{
			name:         "environment variable set",
			key:          "VFMAC_TEST_ENV",
			defaultValue: "default",
			setValue:     "foo",
			want:         "foo",
		},
		{
			name:         "environment variable not set",
			key:          "VFMAC_TEST_ENV",
			defaultValue: "default",
			setValue:     "",
			want:         "default",
		},
		{
			name:         "empty default value",
			key:          "VFMAC_TEST_ENV",
			defaultValue: "",
			setValue:     "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment after test
			defer func() {
				if err := os.Unsetenv(tt.key); err != nil {
					t.Errorf("failed to unset environment variable: %v", err)
				}
			}()

			if tt.setValue != "" {
				if err := os.Setenv(tt.key, tt.setValue); err != nil {
					t.Fatalf("failed to set environment variable: %v", err)
				}
			}

			if got := getEnv(tt.key, tt.defaultValue); got != tt.want {
				t.Errorf("getEnv(%q, %q) = %v, want %v", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	mfs := &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}
	vfmac := newTestVFMAC(mfs, []string{"p0"}, "/test/config/dir", "test-config.toml")

	mapping, err := vfmac.loadConfig()
	if err != nil {
		t.Errorf("LoadConfig() unexpected error: %v", err)
	}
	if len(mapping) != 0 {
		t.Error("LoadConfig() returned non-empty mapping for missing file")
	}
}

func TestLoadConfig_InvalidTOML(t *testing.T) {
	mfs := &mockFS{
		files: map[string][]byte{"/test/config/dir/test-config.toml": []byte("not toml")},
		dirs:  make(map[string]bool),
	}
	vfmac := newTestVFMAC(mfs, []string{"p0"}, "/test/config/dir", "test-config.toml")
	_, err := vfmac.loadConfig()
	if err == nil {
		t.Errorf("expected error for invalid TOML")
	}
}

type failFS struct{ mockFS }

func (f *failFS) MkdirAll(path string, perm os.FileMode) error { return errors.New("fail mkdir") }

func TestSaveConfig_MkdirFail(t *testing.T) {
	mfs := &failFS{mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}}
	vfmac := newTestVFMAC(mfs, []string{"p0"}, "/test/config/dir", "test-config.toml")
	mapping := VFMapping{"p0": ECPFConfig{}, "p1": ECPFConfig{}}
	if err := vfmac.saveConfig(mapping); err == nil {
		t.Errorf("expected error for mkdir failure")
	}
}

func TestGetMaxVFs(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		err     error
		want    int
		wantErr bool
	}{
		{
			name:    "successful query",
			want:    4,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockedFs := &mockFS{
				files: map[string][]byte{},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":     true,
					"/sys/class/net/p0/smart_nic/vf0": true,
					"/sys/class/net/p0/smart_nic/vf1": true,
					"/sys/class/net/p0/smart_nic/vf2": true,
					"/sys/class/net/p0/smart_nic/vf3": true,
				},
			}

			vfmac := newTestVFMAC(mockedFs, []string{"p0"}, "", "")

			got, err := vfmac.getMaxVFs("p0")
			if (err != nil) != tt.wantErr {
				t.Errorf("getMaxVFs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getMaxVFs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetVFConfig(t *testing.T) {
	tests := []struct {
		name    string
		pf      string
		vf      string
		content string
		want    VFConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			pf:      "p0",
			vf:      "vf0",
			content: "MAC: fa:b0:b2:04:9f:b1\n",
			want:    VFConfig{MAC: "fa:b0:b2:04:9f:b1"},
			wantErr: false,
		},
		{
			name:    "missing MAC",
			pf:      "p0",
			vf:      "vf0",
			content: "OTHER: value\n",
			want:    VFConfig{},
			wantErr: true,
		},
		{
			name:    "empty MAC",
			pf:      "p0",
			vf:      "vf0",
			content: "MAC: \n",
			want:    VFConfig{},
			wantErr: true,
		},
		{
			name:    "invalid MAC format",
			pf:      "p0",
			vf:      "vf0",
			content: "MAC: invalid:mac:format\n",
			want:    VFConfig{},
			wantErr: true,
		},
		{
			name:    "file not found",
			pf:      "p0",
			vf:      "vf0",
			content: "",
			want:    VFConfig{},
			wantErr: true,
		},
		{
			name:    "empty file",
			pf:      "p0",
			vf:      "vf0",
			content: "",
			want:    VFConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := &mockFS{
				files: map[string][]byte{
					filepath.Join("/sys/class/net", tt.pf, "smart_nic", tt.vf, "config"): []byte(tt.content),
				},
				dirs: make(map[string]bool),
			}
			vfmac := newTestVFMAC(mfs, []string{"p0"}, "", "")

			got, err := vfmac.getVFConfig(tt.pf, tt.vf)
			if (err != nil) != tt.wantErr {
				t.Errorf("getVFConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getVFConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetVFMAC(t *testing.T) {
	tests := []struct {
		name    string
		pf      string
		vf      string
		mac     string
		mockFS  *mockFS
		wantErr bool
	}{
		{
			name: "valid MAC",
			pf:   "p0",
			vf:   "vf0",
			mac:  testMAC,
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs: map[string]bool{
					filepath.Join("/sys/class/net", "p0", "smart_nic", "vf0"): true,
				},
			},
			wantErr: false,
		},
		{
			name: "random MAC",
			pf:   "p0",
			vf:   "vf0",
			mac:  "Random",
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs: map[string]bool{
					filepath.Join("/sys/class/net", "p0", "smart_nic", "vf0"): true,
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid MAC format",
			pf:      "p0",
			vf:      "vf0",
			mac:     "notamac",
			mockFS:  &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			wantErr: true,
		},
		{
			name:    "empty MAC",
			pf:      "p0",
			vf:      "vf0",
			mac:     "",
			mockFS:  &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			wantErr: true,
		},
		{
			name:    "invalid VF name format",
			pf:      "p0",
			vf:      "invalid",
			mac:     testMAC,
			mockFS:  &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			wantErr: true,
		},
		{
			name: "write permission denied",
			pf:   "p0",
			vf:   "vf0",
			mac:  testMAC,
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs: map[string]bool{
					filepath.Join("/sys/class/net", "p0", "smart_nic", "vf0"): true,
				},
				writeErr: os.ErrPermission,
			},
			wantErr: true,
		},
		{
			name: "VF directory not found",
			pf:   "p0",
			vf:   "vf0",
			mac:  testMAC,
			mockFS: &mockFS{
				files:   make(map[string][]byte),
				dirs:    make(map[string]bool),
				statErr: os.ErrNotExist,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vfmac := newTestVFMAC(tt.mockFS, []string{"p0"}, "", "")

			err := vfmac.setVFMAC(tt.pf, tt.vf, tt.mac)
			if (err != nil) != tt.wantErr {
				t.Errorf("setVFMAC() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify the MAC was written correctly
				expectedPath := filepath.Join("/sys/class/net", tt.pf, "smart_nic", tt.vf, "mac")
				if _, ok := tt.mockFS.files[expectedPath]; !ok {
					t.Errorf("setVFMAC() did not write to expected path %s", expectedPath)
				}
			}
		})
	}
}

func TestLoadConfig_FileErrors(t *testing.T) {
	tests := []struct {
		name    string
		mockFS  *mockFS
		wantErr bool
	}{
		{
			name: "file not found",
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs:  make(map[string]bool),
			},
			wantErr: false,
		},
		{
			name: "invalid TOML",
			mockFS: &mockFS{
				files: map[string][]byte{
					"/test/config/dir/test-config.toml": []byte("not toml\n[p0]\nvf0 = { mac = \"invalid-mac\" }"),
				},
				dirs: make(map[string]bool),
			},
			wantErr: true,
		},
		{
			name: "permission denied",
			mockFS: &mockFS{
				files:   make(map[string][]byte),
				dirs:    make(map[string]bool),
				readErr: os.ErrPermission,
			},
			wantErr: true,
		},
		{
			name: "invalid MAC in config",
			mockFS: &mockFS{
				files: map[string][]byte{
					"/test/config/dir/test-config.toml": []byte("[p0]\n[p0.vf0]\nmac = \"invalid-mac\"\n"),
				},
				dirs: make(map[string]bool),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vfmac := newTestVFMAC(tt.mockFS, []string{"p0"}, "/test/config/dir", "test-config.toml")

			_, err := vfmac.loadConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveConfig_FileErrors(t *testing.T) {
	tests := []struct {
		name    string
		mockFS  *mockFS
		wantErr bool
	}{
		{
			name: "mkdir failure",
			mockFS: &mockFS{
				files:    make(map[string][]byte),
				dirs:     make(map[string]bool),
				mkdirErr: errors.New("mkdir failed"),
			},
			wantErr: true,
		},
		{
			name: "write permission denied",
			mockFS: &mockFS{
				files:    make(map[string][]byte),
				dirs:     make(map[string]bool),
				writeErr: os.ErrPermission,
			},
			wantErr: true,
		},
		{
			name: "disk full",
			mockFS: &mockFS{
				files:    make(map[string][]byte),
				dirs:     make(map[string]bool),
				writeErr: errors.New("no space left on device"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vfmac := newTestVFMAC(tt.mockFS, []string{"p0"}, "/test/config/dir", "test-config.toml")

			mapping := VFMapping{
				"p0": ECPFConfig{"vf0": VFConfig{MAC: testMAC}},
			}

			err := vfmac.saveConfig(mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("saveConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProcessVFs(t *testing.T) {
	tests := []struct {
		name           string
		maxVFs         int
		existingConfig VFMapping
		mockFS         TestFileSystem
		wantErr        bool
	}{
		{
			name:   "new config with random MACs",
			maxVFs: 2,
			mockFS: &mockFS{
				files: map[string][]byte{
					"/sys/class/net/p0/smart_nic/vf0/config": []byte("MAC: 00:00:00:00:00:01\n"),
					"/sys/class/net/p0/smart_nic/vf1/config": []byte("MAC: 00:00:00:00:00:02\n"),
				},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":     true,
					"/sys/class/net/p0/smart_nic/vf0": true,
					"/sys/class/net/p0/smart_nic/vf1": true,
				},
			},
			wantErr: false,
		},
		{
			name:   "existing config with MACs",
			maxVFs: 2,
			existingConfig: VFMapping{
				"p0": ECPFConfig{"vf0": VFConfig{MAC: testMAC}},
			},
			mockFS: &mockFS{
				files: map[string][]byte{
					"/sys/class/net/p0/smart_nic/vf0/config": []byte("MAC: fa:b0:b2:04:9f:b1\n"),
					"/sys/class/net/p0/smart_nic/vf1/config": []byte("MAC: fa:b0:b2:04:9f:b1\n"),
				},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":     true,
					"/sys/class/net/p0/smart_nic/vf0": true,
					"/sys/class/net/p0/smart_nic/vf1": true,
				},
			},
			wantErr: false,
		},
		{
			name:   "max VFs error",
			maxVFs: 0,
			mockFS: &mockFS{
				files: map[string][]byte{},
				dirs:  make(map[string]bool),
			},
			wantErr: true,
		},
		{
			name:   "invalid existing config",
			maxVFs: 2,
			existingConfig: VFMapping{
				"p0": ECPFConfig{"vf0": VFConfig{MAC: "invalid"}},
			},
			mockFS: &mockFS{
				files: map[string][]byte{
					"/sys/class/net/p0/smart_nic/vf0/config": []byte("MAC: fa:b0:b2:04:9f:b1\n"),
					"/sys/class/net/p0/smart_nic/vf1/config": []byte("MAC: fa:b0:b2:04:9f:b1\n"),
				},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":     true,
					"/sys/class/net/p0/smart_nic/vf0": true,
					"/sys/class/net/p0/smart_nic/vf1": true,
				},
			},
			wantErr: true,
		},
		{
			name:    "config save error",
			maxVFs:  2,
			mockFS:  &failFS{mockFS: mockFS{files: map[string][]byte{}, dirs: make(map[string]bool)}},
			wantErr: true,
		},
		{
			name:   "stale ECPF entries are removed from config",
			maxVFs: 1,
			existingConfig: VFMapping{
				"p0": ECPFConfig{"vf0": VFConfig{MAC: testMAC}},
				"p1": ECPFConfig{"vf0": VFConfig{MAC: "da:f2:ea:53:cf:40"}},
			},
			mockFS: &mockFS{
				files: map[string][]byte{
					"/sys/class/net/p0/smart_nic/vf0/config": []byte("MAC: fa:b0:b2:04:9f:b1\n"),
				},
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":     true,
					"/sys/class/net/p0/smart_nic/vf0": true,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vfmac := newTestVFMAC(tt.mockFS, []string{"p0"}, "/test/config/dir", "test-config.toml")

			// Set up existing config if provided
			if tt.existingConfig != nil {
				var buf strings.Builder
				encoder := toml.NewEncoder(&buf)
				if err := encoder.Encode(tt.existingConfig); err != nil {
					t.Fatalf("failed to encode test config: %v", err)
				}
				if mfs, ok := tt.mockFS.(*mockFS); ok {
					mfs.files[filepath.Join("/test/config/dir", "test-config.toml")] = []byte(buf.String())
				}
			}

			err := vfmac.ProcessVFs()
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessVFs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify config was saved
				if mfs, ok := tt.mockFS.(*mockFS); ok {
					configData, ok := mfs.files[filepath.Join("/test/config/dir", "test-config.toml")]
					if !ok {
						t.Error("ProcessVFs() did not save config file")
					}

					if tt.name == "stale ECPF entries are removed from config" {
						var savedMapping VFMapping
						if err := toml.Unmarshal(configData, &savedMapping); err != nil {
							t.Fatalf("failed to parse saved config: %v", err)
						}
						if _, exists := savedMapping["p1"]; exists {
							t.Error("stale ECPF 'p1' was not removed from saved config")
						}
						if _, exists := savedMapping["p0"]; !exists {
							t.Error("active ECPF 'p0' is missing from saved config")
						}
					}
				}
			}
		})
	}
}

func TestDiscoverECPFs(t *testing.T) {
	tests := []struct {
		name      string
		mockFS    *mockFS
		setup     func(t *testing.T) *networkhelper_mock.MockNetworkHelper
		wantECPFs []string
		wantErr   bool
	}{
		{
			name:   "DevlinkPortList error",
			mockFS: &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return(nil, fmt.Errorf("devlink error"))
				return mock
			},
			wantErr: true,
		},
		{
			name:   "empty port list",
			mockFS: &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return(nil, nil)
				return mock
			},
			wantECPFs: nil,
			wantErr:   false,
		},
		{
			name:   "no physical ports - all filtered",
			mockFS: &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
					{NetdeviceName: "cpu0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_CPU},
					{NetdeviceName: "pcisf0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PCI_SF},
					{NetdeviceName: "enp3s0vf0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_VIRTUAL},
				}, nil)
				return mock
			},
			wantECPFs: nil,
			wantErr:   false,
		},
		{
			name:   "physical ports without smart_nic dir are filtered",
			mockFS: &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
					{NetdeviceName: "en3f0s0np0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
					{NetdeviceName: "en3f1s0np1", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
				}, nil)
				return mock
			},
			wantECPFs: nil,
			wantErr:   false,
		},
		{
			name: "physical ports with smart_nic dir are discovered",
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic":        true,
					"/sys/class/net/p1/smart_nic":        true,
					"/sys/class/net/enp1s0np0/smart_nic": true,
				},
			},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
					{NetdeviceName: "p0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
					{NetdeviceName: "p1", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
					{NetdeviceName: "enp1s0np0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
				}, nil)
				return mock
			},
			wantECPFs: []string{"p0", "p1", "enp1s0np0"},
			wantErr:   false,
		},
		{
			name: "mixed ports - only physical with smart_nic survive",
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs: map[string]bool{
					"/sys/class/net/p0/smart_nic": true,
				},
			},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
					{NetdeviceName: "p0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
					{NetdeviceName: "enp1s0np0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
					{NetdeviceName: "cpu0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_CPU},
				}, nil)
				return mock
			},
			wantECPFs: []string{"p0"},
			wantErr:   false,
		},
		{
			name:   "stat error (non-NotExist) returns error",
			mockFS: &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool), statErr: os.ErrPermission},
			setup: func(t *testing.T) *networkhelper_mock.MockNetworkHelper {
				ctrl := gomock.NewController(t)
				mock := networkhelper_mock.NewMockNetworkHelper(ctrl)
				mock.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
					{NetdeviceName: "p0", PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL},
				}, nil)
				return mock
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockNH := tt.setup(t)
			got, err := discoverECPFs(mockNH, tt.mockFS)
			if (err != nil) != tt.wantErr {
				t.Errorf("discoverECPFs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.wantECPFs) {
				t.Errorf("discoverECPFs() got %v (len %d), want %v (len %d)", got, len(got), tt.wantECPFs, len(tt.wantECPFs))
				return
			}
			for i, ecpf := range got {
				if ecpf != tt.wantECPFs[i] {
					t.Errorf("discoverECPFs()[%d] = %v, want %v", i, ecpf, tt.wantECPFs[i])
				}
			}
		})
	}
}
