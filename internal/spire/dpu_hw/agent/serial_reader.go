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

package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-hclog"
)

// defaultSerialPath and fallbackSerialPath are DMI sysfs nodes that expose the
// DPU serial on the Arm cores. board_serial is primary; product_serial is the
// fallback when the primary node is missing or empty.
const (
	defaultSerialPath  = "/sys/class/dmi/id/board_serial"
	fallbackSerialPath = "/sys/class/dmi/id/product_serial"
)

// SerialReader reads the raw DPU hardware serial from the local system.
type SerialReader interface {
	ReadSerial(ctx context.Context) (string, error)
}

type loggerAwareSerialReader interface {
	SetLogger(logger hclog.Logger)
}

// FileSerialReader reads the serial from a file on the local filesystem, such
// as a DMI/SMBIOS sysfs node. Paths are configurable for tests.
type FileSerialReader struct {
	// Path is the primary file to read. When empty, defaultSerialPath is used.
	Path string
	// FallbackPath is read when Path is missing or empty.
	FallbackPath string

	// logger is set once during plugin initialization, before any attestation
	// RPCs are served.
	logger hclog.Logger
}

// SetLogger wires the SPIRE-provided logger into fallback diagnostics.
func (r *FileSerialReader) SetLogger(logger hclog.Logger) {
	r.logger = logger
}

// NewFileSerialReader returns a FileSerialReader bound to the primary DMI board
// serial node with the product serial node as a fallback.
func NewFileSerialReader() *FileSerialReader {
	return &FileSerialReader{Path: defaultSerialPath, FallbackPath: fallbackSerialPath}
}

// ReadSerial reads the serial from the primary path, falling back to
// FallbackPath when configured if the primary node is missing or empty.
func (r *FileSerialReader) ReadSerial(_ context.Context) (string, error) {
	primary := r.Path
	if primary == "" {
		primary = defaultSerialPath
	}

	serial, primErr := readTrimmedSerial(primary)
	if primErr == nil {
		return serial, nil
	}
	if r.FallbackPath == "" {
		return "", primErr
	}

	serial, fbErr := readTrimmedSerial(r.FallbackPath)
	if fbErr == nil {
		if r.logger != nil {
			r.logger.Debug("primary DPU serial read failed, using fallback",
				"primary_path", primary,
				"fallback_path", r.FallbackPath,
				"primary_err", primErr,
			)
		}
		return serial, nil
	}
	return "", fmt.Errorf("primary serial source failed (%v); fallback %w", primErr, fbErr)
}

// readTrimmedSerial reads path and returns the whitespace-trimmed contents,
// treating a missing file or empty/whitespace-only contents as an error.
func readTrimmedSerial(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading DPU serial from %q: %w", path, err)
	}
	serial := strings.TrimSpace(string(b))
	if serial == "" {
		return "", fmt.Errorf("DPU serial at %q is empty", path)
	}
	return serial, nil
}
