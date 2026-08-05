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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestFileSerialReader(t *testing.T) {
	dir := t.TempDir()

	t.Run("reads and trims trailing newline", func(t *testing.T) {
		path := filepath.Join(dir, "board_serial")
		require.NoError(t, os.WriteFile(path, []byte("MT2152X00ABC\n"), 0o600))

		r := &FileSerialReader{Path: path}
		got, err := r.ReadSerial(context.Background())
		require.NoError(t, err)
		require.Equal(t, "MT2152X00ABC", got)
	})

	t.Run("missing file errors", func(t *testing.T) {
		r := &FileSerialReader{Path: filepath.Join(dir, "does-not-exist")}
		_, err := r.ReadSerial(context.Background())
		require.Error(t, err)
	})

	t.Run("empty file errors", func(t *testing.T) {
		path := filepath.Join(dir, "empty_serial")
		require.NoError(t, os.WriteFile(path, []byte("  \n"), 0o600))

		r := &FileSerialReader{Path: path}
		_, err := r.ReadSerial(context.Background())
		require.Error(t, err)
	})

	t.Run("default path constant is the DMI board serial node", func(t *testing.T) {
		require.Equal(t, "/sys/class/dmi/id/board_serial", defaultSerialPath)
	})

	t.Run("fallback path constant is the DMI product serial node", func(t *testing.T) {
		require.Equal(t, "/sys/class/dmi/id/product_serial", fallbackSerialPath)
	})
}

func TestFileSerialReaderFallback(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "board_serial")
	fallback := filepath.Join(dir, "product_serial")
	require.NoError(t, os.WriteFile(fallback, []byte("MT2440600YYW\n"), 0o600))

	t.Run("uses fallback when primary is missing", func(t *testing.T) {
		r := &FileSerialReader{Path: primary, FallbackPath: fallback}
		got, err := r.ReadSerial(context.Background())
		require.NoError(t, err)
		require.Equal(t, "MT2440600YYW", got)
	})

	t.Run("uses fallback when primary is empty", func(t *testing.T) {
		require.NoError(t, os.WriteFile(primary, []byte("  \n"), 0o600))
		r := &FileSerialReader{Path: primary, FallbackPath: fallback}
		got, err := r.ReadSerial(context.Background())
		require.NoError(t, err)
		require.Equal(t, "MT2440600YYW", got)
	})

	t.Run("uses fallback when board serial is unspecified", func(t *testing.T) {
		require.NoError(t, os.WriteFile(primary, []byte("Unspecified Base Board Serial Number\n"), 0o600))
		r := &FileSerialReader{Path: primary, FallbackPath: fallback}
		got, err := r.ReadSerial(context.Background())
		require.NoError(t, err)
		require.Equal(t, "MT2440600YYW", got)
	})

	t.Run("logs primary failure when fallback succeeds", func(t *testing.T) {
		var buf bytes.Buffer
		logger := hclog.New(&hclog.LoggerOptions{
			Level:  hclog.Debug,
			Output: &buf,
		})
		r := &FileSerialReader{Path: primary, FallbackPath: fallback, logger: logger}
		got, err := r.ReadSerial(context.Background())
		require.NoError(t, err)
		require.Equal(t, "MT2440600YYW", got)
		require.Contains(t, buf.String(), "using fallback")
		require.Contains(t, buf.String(), "primary_path")
		require.Contains(t, buf.String(), "fallback_path")
		require.Contains(t, buf.String(), "primary_err")
	})

	t.Run("errors mention both sources when primary and fallback fail", func(t *testing.T) {
		noPrimary := filepath.Join(dir, "no-primary")
		noFallback := filepath.Join(dir, "no-fallback")
		r := &FileSerialReader{
			Path:         noPrimary,
			FallbackPath: noFallback,
		}
		_, err := r.ReadSerial(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "primary serial source failed")
		require.Contains(t, err.Error(), noPrimary)
		require.Contains(t, err.Error(), "fallback")
		require.Contains(t, err.Error(), noFallback)
	})

	t.Run("errors when product serial is unspecified", func(t *testing.T) {
		require.NoError(t, os.WriteFile(fallback, []byte("Unspecified System Serial Number\n"), 0o600))
		r := &FileSerialReader{Path: primary, FallbackPath: fallback}
		_, err := r.ReadSerial(context.Background())
		require.ErrorContains(t, err, "is unspecified")
	})
}

func TestSetLoggerWiresFileSerialReaderFallbackLogging(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "board_serial")
	fallback := filepath.Join(dir, "product_serial")
	require.NoError(t, os.WriteFile(fallback, []byte("MT2440600YYW\n"), 0o600))

	var buf bytes.Buffer
	logger := hclog.New(&hclog.LoggerOptions{
		Level:  hclog.Debug,
		Output: &buf,
	})
	plugin := New()
	plugin.SetLogger(logger)

	reader, ok := plugin.reader.(*FileSerialReader)
	require.True(t, ok)
	reader.Path = primary
	reader.FallbackPath = fallback

	got, err := reader.ReadSerial(context.Background())
	require.NoError(t, err)
	require.Equal(t, "MT2440600YYW", got)
	require.Contains(t, buf.String(), "using fallback")
}

func TestNewFileSerialReaderWiresBothDMINodes(t *testing.T) {
	r := NewFileSerialReader()
	require.Equal(t, "/sys/class/dmi/id/board_serial", r.Path)
	require.Equal(t, "/sys/class/dmi/id/product_serial", r.FallbackPath)
}
