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

package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	httputil "github.com/nvidia/doca-platform/internal/provisioning/utils/http"
)

// DownloadFile downloads a file from a URL to a destination file.
// If the file already exists, the download is skipped.
// The downloaded content is validated against the Content-Length header from the GET response.
func DownloadFile(ctx context.Context, url string, dst string, fileMode os.FileMode) error {
	// Check if file already exists - skip download if it does
	if _, err := os.Stat(dst); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name()) //nolint: errcheck

	// Retry protects initial connection from transient network failures.
	// Once connected, body reading has no retry.
	resp, err := httputil.DoRequestWithRetry(ctx, "GET", url, 3, time.Second)
	if err != nil {
		return err
	}
	defer httputil.CloseBody(resp)

	expectedSize := resp.ContentLength
	var totalWritten int64

	buf := make([]byte, 128*1024*1024)
copyLoop:
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("download canceled")
		default:
			n, err := resp.Body.Read(buf)
			if err != nil && err != io.EOF {
				if errors.Is(err, context.Canceled) {
					return ctx.Err()
				}
				return fmt.Errorf("failed to read from source file: %w", err)
			}
			if n == 0 {
				break copyLoop
			}
			if _, writeErr := tempFile.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			totalWritten += int64(n)
		}
	}

	// Validate downloaded file size against Content-Length header from this GET response.
	// This detects truncated downloads caused by network failures or connection drops.
	// Only validate when Content-Length is positive (known size).
	// Skip validation when Content-Length is -1 (not set) or 0 (empty) since we can't reliably verify.
	// If validation fails, the function returns an error and the deferred os.Remove() cleans up
	// the temporary file, ensuring no partial/corrupted file is left at the destination.
	if expectedSize > 0 && totalWritten != expectedSize {
		return fmt.Errorf("downloaded file size mismatch: expected %d bytes, got %d bytes", expectedSize, totalWritten)
	}

	// Close the temp file before renaming
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempFile.Name(), dst); err != nil {
		return err
	}
	if err := os.Chmod(dst, fileMode); err != nil {
		return err
	}
	return nil
}
