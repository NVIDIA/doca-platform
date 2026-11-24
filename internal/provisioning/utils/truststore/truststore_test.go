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

package truststore

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
)

func TestDetectBackend(t *testing.T) {
	t.Cleanup(func() { lookPath = exec.LookPath })

	tests := []struct {
		name    string
		look    func(string) (string, error)
		want    Backend
		wantErr bool
	}{
		{
			name: "prefers rhel when update-ca-trust is present",
			look: func(file string) (string, error) {
				if file == "update-ca-trust" {
					return "/usr/bin/update-ca-trust", nil
				}
				return "", errors.New("not found")
			},
			want: BackendRHEL,
		},
		{
			name: "debian when only update-ca-certificates is present",
			look: func(file string) (string, error) {
				if file == "update-ca-certificates" {
					return "/usr/sbin/update-ca-certificates", nil
				}
				return "", errors.New("not found")
			},
			want: BackendDebian,
		},
		{
			name: "error when neither is present",
			look: func(string) (string, error) {
				return "", errors.New("not found")
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookPath = tc.look
			got, err := DetectBackend()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectBackend: %v", err)
			}
			if got != tc.want {
				t.Fatalf("backend=%q want %q", got, tc.want)
			}
		})
	}
}

func TestApply(t *testing.T) {
	t.Cleanup(func() {
		lookPath = exec.LookPath
		runCommand = bash.Run
		debianAnchorDir = "/usr/local/share/ca-certificates"
		rhelAnchorDir = "/etc/pki/ca-trust/source/anchors"
	})

	dir := t.TempDir()
	debianAnchorDir = filepath.Join(dir, "debian")
	rhelAnchorDir = filepath.Join(dir, "rhel")

	pem := "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----\n"

	t.Run("rhel", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			if file == "update-ca-trust" {
				return "/usr/bin/update-ca-trust", nil
			}
			return "", errors.New("not found")
		}
		var ran string
		runCommand = func(cmdStr string) (stdout, stderr bytes.Buffer, err error) {
			ran = cmdStr
			return stdout, stderr, nil
		}

		if err := Apply(pem); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if ran != "update-ca-trust extract" {
			t.Fatalf("refresh command=%q", ran)
		}
		wantPath := filepath.Join(rhelAnchorDir, BundleFileName)
		data, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("read anchor: %v", err)
		}
		if string(data) != pem {
			t.Fatalf("anchor content mismatch")
		}
	})

	t.Run("debian", func(t *testing.T) {
		lookPath = func(file string) (string, error) {
			if file == "update-ca-certificates" {
				return "/usr/sbin/update-ca-certificates", nil
			}
			return "", errors.New("not found")
		}
		var ran string
		runCommand = func(cmdStr string) (stdout, stderr bytes.Buffer, err error) {
			ran = cmdStr
			return stdout, stderr, nil
		}

		if err := Apply(pem); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if ran != "update-ca-certificates" {
			t.Fatalf("refresh command=%q", ran)
		}
		wantPath := filepath.Join(debianAnchorDir, BundleFileName)
		data, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("read anchor: %v", err)
		}
		if string(data) != pem {
			t.Fatalf("anchor content mismatch")
		}
	})
}