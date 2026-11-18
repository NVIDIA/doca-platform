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

// Package statestore keeps the DPU agent's own state on the DPU, so the agent does not read it
// back from DPU.status after a restart. Each owner keeps one JSON file under a per-DPU directory.
//
// There is no locking: every file has a single owner that writes it from one goroutine, and
// writes are atomic (temporary file plus rename), so a reader never sees a partial file.
package statestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"
)

const (
	envelopeVersion = 1
	dirPermission   = 0o700
	filePermission  = 0o600
)

// envelope wraps every file. Version describes the envelope only; each owner evolves its own data.
type envelope struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
}

// Store is the state of one DPU object, kept in the directory <root>/<dpuUID>/. A DPU that is
// provisioned again gets a new UID and so starts from an empty directory.
type Store struct {
	dir string
}

// New returns the Store of the DPU with the given UID. It does not touch the file system.
func New(root, dpuUID string) *Store {
	return &Store{dir: filepath.Join(root, dpuUID)}
}

// Save atomically writes value as JSON to <dir>/<name>.json with mode 0600, creating the
// directory when needed. When the write fails, the previous file is left unchanged.
func (s *Store) Save(name string, value any) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal state %s: %w", name, err)
	}
	content, err := json.Marshal(envelope{Version: envelopeVersion, Data: data})
	if err != nil {
		return fmt.Errorf("marshal state %s: %w", name, err)
	}
	if err := os.MkdirAll(s.dir, dirPermission); err != nil {
		return fmt.Errorf("create state directory %s: %w", s.dir, err)
	}
	if err := filesystem.AtomicWrite(path, content, filePermission); err != nil {
		return fmt.Errorf("write state %s: %w", path, err)
	}
	return nil
}

// Load decodes <dir>/<name>.json into value, which must be a pointer. When the file does not
// exist, errors.Is(err, os.ErrNotExist) is true and value is left unchanged. A file that cannot be
// decoded, or whose envelope version is unknown, is an error: a corrupt file is not repaired.
func (s *Store) Load(name string, value any) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read state %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(content, &env); err != nil {
		return fmt.Errorf("decode state %s: %w", path, err)
	}
	if env.Version != envelopeVersion {
		return fmt.Errorf("decode state %s: unsupported version %d", path, env.Version)
	}
	if err := json.Unmarshal(env.Data, value); err != nil {
		return fmt.Errorf("decode state %s: %w", path, err)
	}
	return nil
}

// Reset removes <dir>/<name>.json. A missing file is not an error.
func (s *Store) Reset(name string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove state %s: %w", path, err)
	}
	return nil
}

func (s *Store) path(name string) (string, error) {
	if name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("invalid state name %q", name)
	}
	return filepath.Join(s.dir, name+".json"), nil
}
