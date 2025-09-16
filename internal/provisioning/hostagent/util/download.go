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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// DownloadFile downloads a file from a URL to a destination file.
// todo: make this an util and substitute the one in dpu-controllers
func DownloadFile(ctx context.Context, url string, dst string, fileMode os.FileMode) error {
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

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get: %s status: %d", url, resp.StatusCode)
	}
	defer resp.Body.Close() //nolint: errcheck

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
		}
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
