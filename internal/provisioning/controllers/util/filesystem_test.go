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
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("filesystem helpers", func() {
	Describe("IsIgnorableRemoveErr", func() {
		It("returns false for nil", func() {
			Expect(IsIgnorableRemoveErr(nil)).To(BeFalse())
		})

		It("returns true for not exist", func() {
			Expect(IsIgnorableRemoveErr(os.ErrNotExist)).To(BeTrue())
		})

		It("returns true for ESTALE", func() {
			err := fmt.Errorf("remove failed: %w", syscall.ESTALE)
			Expect(IsIgnorableRemoveErr(err)).To(BeTrue())
		})

		It("returns true for ESTALE wrapped in PathError", func() {
			err := &os.PathError{Op: "remove", Path: "/bfb/foo.cfg", Err: syscall.ESTALE}
			Expect(IsIgnorableRemoveErr(err)).To(BeTrue())
		})

		It("returns false for other errors", func() {
			Expect(IsIgnorableRemoveErr(os.ErrPermission)).To(BeFalse())
		})
	})

	Describe("RemoveFileEx", func() {
		It("ignores missing files", func() {
			Expect(RemoveFileEx(filepath.Join(GinkgoT().TempDir(), "missing.cfg"))).To(Succeed())
		})

		It("removes existing files", func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "delete-me.cfg")
			Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
			Expect(RemoveFileEx(path)).To(Succeed())
			_, err := os.Stat(path)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})

	Describe("RemoveAllEx", func() {
		It("ignores missing directories", func() {
			Expect(RemoveAllEx(filepath.Join(GinkgoT().TempDir(), "missing-dir"))).To(Succeed())
		})

		It("removes existing directories", func() {
			dir := filepath.Join(GinkgoT().TempDir(), "delete-me")
			Expect(os.MkdirAll(filepath.Join(dir, "nested"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "nested", "file.cfg"), []byte("x"), 0o644)).To(Succeed())
			Expect(RemoveAllEx(dir)).To(Succeed())
			_, err := os.Stat(dir)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})
