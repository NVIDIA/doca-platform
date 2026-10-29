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
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IDlocker tests", func() {
	It("Can lock different IDs at the same time", func() {
		locker := NewIDLocker()
		ok1, unlock1 := locker.TryLock("foo1")
		Expect(ok1).To(BeTrue())
		Expect(unlock1).NotTo(BeNil())
		ok2, unlock2 := locker.TryLock("foo2")
		Expect(ok2).To(BeTrue())
		Expect(unlock2).NotTo(BeNil())
		unlock1()
		unlock2()
	})
	It("Can't lock same ID twice", func() {
		locker := NewIDLocker()
		ok1, unlock1 := locker.TryLock("foo1")
		Expect(ok1).To(BeTrue())
		Expect(unlock1).NotTo(BeNil())
		ok2, unlock2 := locker.TryLock("foo1")
		Expect(ok2).To(BeFalse())
		Expect(unlock2).To(BeNil())
		unlock1()
	})
	It("Can lock again after unlock", func() {
		locker := NewIDLocker()
		ok1, unlock1 := locker.TryLock("foo1")
		Expect(ok1).To(BeTrue())
		Expect(unlock1).NotTo(BeNil())
		ok2, unlock2 := locker.TryLock("foo1")
		Expect(ok2).To(BeFalse())
		Expect(unlock2).To(BeNil())
		unlock1()
		ok3, unlock3 := locker.TryLock("foo1")
		Expect(ok3).To(BeTrue())
		Expect(unlock3).NotTo(BeNil())
		unlock3()
	})
	It("Concurrent access", func() {
		commonValue := "foo"
		commonLockCount := int32(0)
		locker := NewIDLocker()
		wg := sync.WaitGroup{}
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, _ := locker.TryLock(commonValue)
				if ok {
					atomic.AddInt32(&commonLockCount, 1)
				}
			}()
		}
		wg.Wait()
		Expect(commonLockCount).To(BeEquivalentTo(1))
	})
})
