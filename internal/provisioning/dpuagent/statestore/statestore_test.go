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

package statestore

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type testState struct {
	Count int    `json:"count"`
	Name  string `json:"name,omitempty"`
}

var _ = Describe("Store", func() {
	var (
		root  string
		store *Store
	)

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		store = New(root, "uid-1")
	})

	It("does not touch the file system on New", func() {
		_ = New(filepath.Join(root, "missing"), "uid")
		Expect(filepath.Join(root, "missing")).NotTo(BeADirectory())
	})

	It("round-trips a value in the per-UID directory with mode 0600", func() {
		Expect(store.Save("reboot", testState{Count: 3, Name: "a"})).To(Succeed())

		path := filepath.Join(root, "uid-1", "reboot.json")
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(MatchJSON(`{"version":1,"data":{"count":3,"name":"a"}}`))

		var got testState
		Expect(store.Load("reboot", &got)).To(Succeed())
		Expect(got).To(Equal(testState{Count: 3, Name: "a"}))
	})

	It("returns os.ErrNotExist and leaves the value unchanged when the file is missing", func() {
		got := testState{Count: 7}
		err := store.Load("reboot", &got)
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
		Expect(got).To(Equal(testState{Count: 7}))
	})

	It("fails on a corrupt file", func() {
		Expect(os.MkdirAll(filepath.Join(root, "uid-1"), 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "uid-1", "reboot.json"), []byte("{"), 0o600)).To(Succeed())

		var got testState
		err := store.Load("reboot", &got)
		Expect(err).To(MatchError(ContainSubstring("decode state")))
		Expect(errors.Is(err, os.ErrNotExist)).To(BeFalse())
	})

	It("fails on an unknown envelope version", func() {
		Expect(os.MkdirAll(filepath.Join(root, "uid-1"), 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "uid-1", "reboot.json"), []byte(`{"version":2,"data":{}}`), 0o600)).To(Succeed())

		var got testState
		Expect(store.Load("reboot", &got)).To(MatchError(ContainSubstring("unsupported version 2")))
	})

	It("keeps the previous file when a write fails", func() {
		Expect(store.Save("reboot", testState{Count: 1})).To(Succeed())
		Expect(store.Save("reboot", func() {})).To(HaveOccurred())

		var got testState
		Expect(store.Load("reboot", &got)).To(Succeed())
		Expect(got).To(Equal(testState{Count: 1}))
	})

	It("resets idempotently", func() {
		Expect(store.Save("reboot", testState{Count: 1})).To(Succeed())
		Expect(store.Reset("reboot")).To(Succeed())
		Expect(store.Reset("reboot")).To(Succeed())

		var got testState
		Expect(errors.Is(store.Load("reboot", &got), os.ErrNotExist)).To(BeTrue())
	})

	It("keeps different names and UIDs apart", func() {
		other := New(root, "uid-2")
		Expect(store.Save("reboot", testState{Count: 1})).To(Succeed())
		Expect(store.Save("dns", testState{Count: 2})).To(Succeed())
		Expect(other.Save("reboot", testState{Count: 3})).To(Succeed())

		var got testState
		Expect(store.Load("reboot", &got)).To(Succeed())
		Expect(got.Count).To(Equal(1))
		Expect(store.Load("dns", &got)).To(Succeed())
		Expect(got.Count).To(Equal(2))
		Expect(other.Load("reboot", &got)).To(Succeed())
		Expect(got.Count).To(Equal(3))
	})

	DescribeTable("rejects invalid names",
		func(name string) {
			Expect(store.Save(name, testState{})).To(MatchError(ContainSubstring("invalid state name")))
			Expect(store.Load(name, &testState{})).To(MatchError(ContainSubstring("invalid state name")))
			Expect(store.Reset(name)).To(MatchError(ContainSubstring("invalid state name")))
		},
		Entry("empty", ""),
		Entry("slash", "a/b"),
		Entry("parent directory", "../other/reboot"),
	)
})
