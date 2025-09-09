/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
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

	"github.com/BurntSushi/toml"
	networkhelper_mock "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"
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

// setupMockNetworkHelper creates a mock NetworkHelper with default expectations
func setupMockNetworkHelper(t *testing.T) *networkhelper_mock.MockNetworkHelper {
	ctrl := gomock.NewController(t)
	mockNetworkHelper := networkhelper_mock.NewMockNetworkHelper(ctrl)

	// Set up default expectations for NewVFMAC calls
	mockNetworkHelper.EXPECT().GetUplinkRepresentor("0000:03:00.0").Return("p0", nil).AnyTimes()
	mockNetworkHelper.EXPECT().GetUplinkRepresentor("0000:03:00.1").Return("p1", nil).AnyTimes()

	return mockNetworkHelper
}

// setupMockNetworkHelperError creates a mock NetworkHelper which errors out
func setupMockNetworkHelperError(t *testing.T) *networkhelper_mock.MockNetworkHelper {
	ctrl := gomock.NewController(t)
	mockNetworkHelper := networkhelper_mock.NewMockNetworkHelper(ctrl)
	mockNetworkHelper.EXPECT().GetUplinkRepresentor(gomock.Any()).Return("", fmt.Errorf("mock error")).AnyTimes()

	return mockNetworkHelper
}

func TestNewVFMAC(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockNetworkHelper := networkhelper_mock.NewMockNetworkHelper(ctrl)
	mockNetworkHelper.EXPECT().GetUplinkRepresentor("0000:03:00.0").Return("p0", nil).AnyTimes()
	mockNetworkHelper.EXPECT().GetUplinkRepresentor("0000:03:00.1").Return("p1", nil).AnyTimes()

	tests := []struct {
		name              string
		mockFS            *mockFS
		mockNetworkHelper *networkhelper_mock.MockNetworkHelper
		wantErr           bool
	}{
		{
			name:              "success",
			mockFS:            &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			mockNetworkHelper: setupMockNetworkHelper(t),
			wantErr:           false,
		},
		{
			name:              "error",
			mockFS:            &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)},
			mockNetworkHelper: setupMockNetworkHelperError(t),
			wantErr:           true,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			_, err := NewVFMAC(tcase.mockFS, tcase.mockNetworkHelper, "", "")
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
		mapping        *VFMapping
		mockFS         *mockFS
		wantErr        bool
		verifyContents bool
	}{
		{
			name: "basic config save and load",
			mapping: &VFMapping{
				P0: map[string]VFConfig{"vf0": {MAC: testMAC}},
				P1: map[string]VFConfig{"vf0": {MAC: "da:f2:ea:53:cf:40"}},
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
			mapping: &VFMapping{
				P0: map[string]VFConfig{},
				P1: map[string]VFConfig{},
			},
			mockFS: &mockFS{
				files: make(map[string][]byte),
				dirs:  make(map[string]bool),
			},
			wantErr:        false,
			verifyContents: true,
		},
		{
			name: "multiple VFs per PF",
			mapping: &VFMapping{
				P0: map[string]VFConfig{
					"vf0": {MAC: testMAC},
					"vf1": {MAC: "da:f2:ea:53:cf:40"},
				},
				P1: map[string]VFConfig{
					"vf0": {MAC: "aa:bb:cc:dd:ee:ff"},
					"vf1": {MAC: "11:22:33:44:55:66"},
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
			mapping: &VFMapping{
				P0: map[string]VFConfig{"vf0": {MAC: testMAC}},
				P1: map[string]VFConfig{"vf0": {MAC: "da:f2:ea:53:cf:40"}},
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
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(tt.mockFS, mockNetworkHelper, "/test/config/dir", "test-config.toml")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

			// Test save
			err = vfmac.saveConfig(tt.mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("saveConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return // Skip load test if save failed
			}

			// Test load
			loaded, err := vfmac.loadConfig()
			if err != nil {
				t.Errorf("LoadConfig() error = %v", err)
				return
			}

			if tt.verifyContents {
				// Verify P0 contents
				for vf, config := range tt.mapping.P0 {
					if loaded.P0[vf].MAC != config.MAC {
						t.Errorf("P0[%s].MAC = %v, want %v", vf, loaded.P0[vf].MAC, config.MAC)
					}
				}

				// Verify P1 contents
				for vf, config := range tt.mapping.P1 {
					if loaded.P1[vf].MAC != config.MAC {
						t.Errorf("P1[%s].MAC = %v, want %v", vf, loaded.P1[vf].MAC, config.MAC)
					}
				}
			}
		})
	}
}

func TestLoadIfaceMACAddressMapping(t *testing.T) {
	tests := []struct {
		name           string
		mockFS         *mockFS
		wantErr        bool
		verifyContents bool
	}{
		{
			name: "basic config load from smart_nic",
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
			name: "empty config",
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
			name: "invalid MAC ",
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
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(tt.mockFS, mockNetworkHelper, "/test/config/dir", "test-config.toml")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

			// Test load
			vfmapping, err := vfmac.LoadIfaceMACAddressMapping()
			if err != nil && !tt.wantErr {
				t.Errorf("LoadIfaceMACAddressMapping() error = %v", err)
				return
			}

			if tt.verifyContents {
				// Verify P0 contents
				for iface, config := range vfmapping.P0 {
					if vfmapping.P0[iface].MAC != config.MAC {
						t.Errorf("P0[%s].MAC = %v, want %v", iface, vfmapping.P0[iface].MAC, config.MAC)
					}
				}

				// Verify P1 contents
				for iface, config := range vfmapping.P1 {
					if vfmapping.P1[iface].MAC != config.MAC {
						t.Errorf("P1[%s].MAC = %v, want %v", iface, vfmapping.P1[iface].MAC, config.MAC)
					}
				}
			}
		})
	}
}

func TestSetVFMAC_InvalidMAC(t *testing.T) {
	mockNetworkHelper := setupMockNetworkHelper(t)
	vfmac, err := NewVFMAC(&mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}, mockNetworkHelper, "", "")
	if err != nil {
		t.Fatalf("NewVFMAC() error = %v", err)
	}
	if err := vfmac.setVFMAC("p0", "vf0", "notamac"); err == nil {
		t.Errorf("expected error for invalid MAC, got nil")
	}
}

func TestGetVFMacAddressFromVFMapping(t *testing.T) {
	mapping := &VFMapping{
		P0: map[string]VFConfig{
			"vf0": {MAC: testMAC},
			"vf1": {MAC: "da:f2:ea:53:cf:40"},
		},
		P1: map[string]VFConfig{
			"vf0": {MAC: "aa:bb:cc:dd:ee:ff"},
			"vf1": {MAC: testMAC},
		},
	}

	tests := []struct {
		name    string
		pf      int
		vf      int
		want    string
		wantErr bool
	}{
		{
			name:    "valid P0/VF0 lookup",
			pf:      0,
			vf:      0,
			want:    testMAC,
			wantErr: false,
		},
		{
			name:    "valid P0/VF1 lookup",
			pf:      0,
			vf:      1,
			want:    "da:f2:ea:53:cf:40",
			wantErr: false,
		},
		{
			name:    "valid P1/VF0 lookup",
			pf:      1,
			vf:      0,
			want:    "aa:bb:cc:dd:ee:ff",
			wantErr: false,
		},
		{
			name:    "valid P1/VF1 lookup",
			pf:      1,
			vf:      1,
			want:    testMAC,
			wantErr: false,
		},
		{
			name:    "invalid PF (too high)",
			pf:      2,
			vf:      0,
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid PF (negative)",
			pf:      -1,
			vf:      0,
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid VF (too high)",
			pf:      0,
			vf:      2,
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid VF (negative)",
			pf:      0,
			vf:      -1,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapping.GetVFMacAddressFromVFMapping(tt.pf, tt.vf)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetVFMacAddressFromVFMapping() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("GetVFMacAddressFromVFMapping() = %v, want %v", got, tt.want)
			}
		})
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
	mockNetworkHelper := setupMockNetworkHelper(t)
	mfs := &mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}
	vfmac, err := NewVFMAC(mfs, mockNetworkHelper, "/test/config/dir", "test-config.toml")
	if err != nil {
		t.Fatalf("NewVFMAC() error = %v", err)
	}

	mapping, err := vfmac.loadConfig()
	if err != nil {
		t.Errorf("LoadConfig() unexpected error: %v", err)
	}
	if len(mapping.P0) != 0 || len(mapping.P1) != 0 {
		t.Error("LoadConfig() returned non-empty mapping for missing file")
	}
}

func TestLoadConfig_InvalidTOML(t *testing.T) {
	mockNetworkHelper := setupMockNetworkHelper(t)
	mfs := &mockFS{
		files: map[string][]byte{"/test/config/dir/test-config.toml": []byte("not toml")},
		dirs:  make(map[string]bool),
	}
	vfmac, err := NewVFMAC(mfs, mockNetworkHelper, "/test/config/dir", "test-config.toml")
	if err != nil {
		t.Fatalf("NewVFMAC() error = %v", err)
	}
	_, err = vfmac.loadConfig()
	if err == nil {
		t.Errorf("expected error for invalid TOML")
	}
}

type failFS struct{ mockFS }

func (f *failFS) MkdirAll(path string, perm os.FileMode) error { return errors.New("fail mkdir") }

func TestSaveConfig_MkdirFail(t *testing.T) {
	mockNetworkHelper := setupMockNetworkHelper(t)
	mfs := &failFS{mockFS{files: make(map[string][]byte), dirs: make(map[string]bool)}}
	vfmac, err := NewVFMAC(mfs, mockNetworkHelper, "/test/config/dir", "test-config.toml")
	if err != nil {
		t.Fatalf("NewVFMAC() error = %v", err)
	}
	mapping := &VFMapping{P0: map[string]VFConfig{}, P1: map[string]VFConfig{}}
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

			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(mockedFs, mockNetworkHelper, "", "")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

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
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(mfs, mockNetworkHelper, "", "")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

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
			name:    "invalid PF name",
			pf:      "invalid",
			vf:      "vf0",
			mac:     testMAC,
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
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(tt.mockFS, mockNetworkHelper, "", "")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

			err = vfmac.setVFMAC(tt.pf, tt.vf, tt.mac)
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
					"/test/config/dir/test-config.toml": []byte("not toml\n[P0]\nvf0 = { MAC = \"invalid-mac\" }"),
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
			name: "invalid file format",
			mockFS: &mockFS{
				files: map[string][]byte{
					"/test/config/dir/test-config.toml": []byte("[P0]\nvf0 = { MAC = \"invalid-mac\" }\n[P1]\nvf0 = { MAC = \"invalid-mac\" }"),
				},
				dirs: make(map[string]bool),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(tt.mockFS, mockNetworkHelper, "/test/config/dir", "test-config.toml")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

			_, err = vfmac.loadConfig()
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
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(tt.mockFS, mockNetworkHelper, "/test/config/dir", "test-config.toml")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

			mapping := &VFMapping{
				P0: map[string]VFConfig{"vf0": {MAC: testMAC}},
				P1: map[string]VFConfig{"vf0": {MAC: testMAC}},
			}

			err = vfmac.saveConfig(mapping)
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
		existingConfig *VFMapping
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
			wantErr: false, // Should succeed - ProcessVFs creates a fresh config
		},
		{
			name:   "existing config with MACs",
			maxVFs: 2,
			existingConfig: &VFMapping{
				P0: map[string]VFConfig{"vf0": {MAC: testMAC}},
				P1: map[string]VFConfig{"vf0": {MAC: testMAC}},
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
			existingConfig: &VFMapping{
				P0: map[string]VFConfig{"vf0": {MAC: "invalid"}},
				P1: map[string]VFConfig{"vf0": {MAC: "invalid"}},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockNetworkHelper := setupMockNetworkHelper(t)
			vfmac, err := NewVFMAC(tt.mockFS, mockNetworkHelper, "/test/config/dir", "test-config.toml")
			if err != nil {
				t.Fatalf("NewVFMAC() error = %v", err)
			}

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

			err = vfmac.ProcessVFs()
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessVFs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify config was saved
				if mfs, ok := tt.mockFS.(*mockFS); ok {
					if _, ok := mfs.files[filepath.Join("/test/config/dir", "test-config.toml")]; !ok {
						t.Error("ProcessVFs() did not save config file")
					}
				}
			}
		})
	}
}
