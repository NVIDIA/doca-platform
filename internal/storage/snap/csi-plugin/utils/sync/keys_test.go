/*
Copyright 2024 NVIDIA

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

package sync

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Keys", func() {
	It("Same prefix, same values", func() {
		prefix := "test"
		Expect(LockKey(prefix, "val1")).To(BeEquivalentTo(LockKey(prefix, "val1")))
	})
	It("Same prefix, different values", func() {
		prefix := "test"
		Expect(LockKey(prefix, "val1")).NotTo(BeEquivalentTo(LockKey(prefix, "val2")))
	})
	It("Different prefixes, same values", func() {
		value := "val1"
		Expect(LockKey("prefix1", value)).NotTo(BeEquivalentTo(LockKey("prefix2", value)))
	})
})
