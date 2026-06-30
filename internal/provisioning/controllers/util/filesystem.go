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
	"errors"
	"os"
	"syscall"
)

// IsIgnorableRemoveErr reports whether err from os.Remove or os.RemoveAll can be
// ignored during best-effort cleanup. Missing paths and stale NFS file handles
// are treated as success because the desired end state (path gone) is already
// satisfied or unreachable from this client.
func IsIgnorableRemoveErr(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	if errors.Is(err, syscall.ESTALE) {
		return true
	}
	return false
}

// RemoveFileEx removes path.
func RemoveFileEx(path string) error {
	if err := os.Remove(path); err != nil && !IsIgnorableRemoveErr(err) {
		return err
	}
	return nil
}

// RemoveAllEx removes path and its children.
func RemoveAllEx(path string) error {
	if err := os.RemoveAll(path); err != nil && !IsIgnorableRemoveErr(err) {
		return err
	}
	return nil
}
