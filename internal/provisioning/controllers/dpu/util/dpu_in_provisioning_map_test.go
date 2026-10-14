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
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DPUInProvisioningMap:", func() {
	var dpuMap *DPUInProvisioningMap
	const maxDPUs = 5

	BeforeEach(func() {
		dpuMap = NewDPUInProvisioningMap(maxDPUs)
	})

	Describe("Basic Operations", func() {
		It("should initialize with correct max value", func() {
			Expect(dpuMap.GetMax()).To(Equal(int32(maxDPUs)))
		})

		It("should handle basic insert and remove operations", func() {
			dpuID := DPUID("test-dpu-1")
			Expect(dpuMap.CanProceed(dpuID)).To(BeTrue())
			dpuMap.Remove(dpuID)
			Expect(dpuMap.CanProceed(dpuID)).To(BeTrue())
		})

		It("should handle multiple removes of same DPU", func() {
			dpuID := DPUID("test-dpu-1")
			dpuMap.Remove(dpuID)
			dpuMap.Remove(dpuID)
			dpuMap.Remove(dpuID)
			Expect(dpuMap.CanProceed(dpuID)).To(BeTrue())
		})

		It("should handle inserting same DPU multiple times", func() {
			dpuID := DPUID("test-dpu-1")
			Expect(dpuMap.CanProceed(dpuID)).To(BeTrue())
			Expect(dpuMap.CanProceed(dpuID)).To(BeTrue())
			Expect(dpuMap.CanProceed(dpuID)).To(BeTrue())
			Expect(dpuMap.CanProceed(DPUID("test-dpu-2"))).To(BeTrue())
		})
	})

	Describe("Concurrent Operations", func() {
		It("should handle parallel provisioning", func() {
			var wg sync.WaitGroup
			concurrentOps := 10
			results := make(chan bool, concurrentOps)

			for i := 0; i < concurrentOps; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					result := dpuMap.CanProceed(DPUID(fmt.Sprintf("test-dpu-%d", id)))
					results <- result
				}(i)
			}

			wg.Wait()
			close(results)

			// Count successful operations
			successCount := 0
			for result := range results {
				if result {
					successCount++
				}
			}

			// Verify exactly maxDPUs succeeded
			Expect(successCount).To(Equal(maxDPUs))
			Expect(dpuMap.CanProceed(DPUID("test-dpu-extra"))).To(BeFalse())
		})

		It("should handle concurrent removes", func() {
			// First insert to max
			for i := 0; i < maxDPUs; i++ {
				Expect(dpuMap.CanProceed(DPUID(fmt.Sprintf("test-dpu-%d", i)))).To(BeTrue())
			}

			var wg sync.WaitGroup
			concurrentOps := 10

			for i := 0; i < concurrentOps; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					dpuMap.Remove(DPUID(fmt.Sprintf("test-dpu-%d", id)))
				}(i)
			}

			wg.Wait()
			Expect(dpuMap.CanProceed(DPUID("test-dpu-new"))).To(BeTrue())
		})

		It("should handle mixed concurrent operations", func() {
			var wg sync.WaitGroup
			concurrentOps := 10

			for i := 0; i < concurrentOps; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					dpuID := DPUID(fmt.Sprintf("test-dpu-%d", id))
					if id%2 == 0 {
						dpuMap.CanProceed(dpuID)
					} else {
						dpuMap.Remove(dpuID)
					}
				}(i)
			}

			wg.Wait()
		})
	})

	Describe("Edge Cases", func() {
		It("should handle max limit correctly", func() {
			// Fill to limit
			for i := 0; i < maxDPUs; i++ {
				Expect(dpuMap.CanProceed(DPUID(fmt.Sprintf("test-dpu-%d", i)))).To(BeTrue())
			}
			Expect(dpuMap.CanProceed(DPUID("test-dpu-extra"))).To(BeFalse())

			// Remove one and verify can proceed
			dpuMap.Remove(DPUID("test-dpu-0"))
			Expect(dpuMap.CanProceed(DPUID("test-dpu-extra"))).To(BeTrue())
		})
	})
})
