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

package future

import (
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TaskManager Test", func() {
	const (
		maxRun = 3
	)
	var (
		tm *TaskManager
	)
	BeforeEach(func() {
		tm = NewTaskManager(3)
	})

	Context("RunTask", func() {
		It("should run the task if it does not exist", func() {
			runCnt := 0
			task, maxReached := tm.RunTask("test", func() (any, error) {
				runCnt++
				return nil, nil
			}, nil)
			Expect(task).NotTo(BeNil())
			_, err := task.GetResult()
			Expect(err).NotTo(HaveOccurred())
			Expect(maxReached).To(BeFalse())
			Expect(runCnt).To(Equal(1))
		})
		It("should retry until max run is reached", func() {
			runCnt := 0
			testFunc := func() (any, error) {
				runCnt++
				return nil, fmt.Errorf("test error")
			}
			for i := 0; i < maxRun; i++ {
				task, maxReached := tm.RunTask("test", testFunc, nil)
				Expect(task).NotTo(BeNil())
				_, err := task.GetResult()
				Expect(err).To(HaveOccurred())
				if i == maxRun-1 {
					Expect(maxReached).To(BeTrue())
				} else {
					Expect(maxReached).To(BeFalse())
				}
				Expect(runCnt).To(Equal(i + 1))
			}
		})
		It("should not re-run if the task succeeded", func() {
			runCnt := 0
			succFunc := func() (any, error) {
				runCnt++
				return nil, nil
			}
			failFunc := func() (any, error) {
				runCnt++
				return nil, fmt.Errorf("test error")
			}
			task, maxReached := tm.RunTask("test", failFunc, nil)
			Expect(task).NotTo(BeNil())
			_, err := task.GetResult()
			Expect(err).To(HaveOccurred())
			Expect(maxReached).To(BeFalse())
			Expect(runCnt).To(Equal(1))

			task, maxReached = tm.RunTask("test", succFunc, nil)
			Expect(task).NotTo(BeNil())
			_, err = task.GetResult()
			Expect(err).NotTo(HaveOccurred())
			Expect(maxReached).To(BeFalse())
			Expect(runCnt).To(Equal(2))
			rc := runCnt

			task, maxReached = tm.RunTask("test", succFunc, nil)
			Expect(task).NotTo(BeNil())
			_, err = task.GetResult()
			Expect(err).NotTo(HaveOccurred())
			Expect(maxReached).To(BeFalse())
			Expect(runCnt).To(Equal(rc))
		})
		It("should cleanup the task if the cleanup function returns true", func() {
			runCnt := 0
			runCntLock := sync.Mutex{}
			taskFunc := func() (any, error) {
				runCntLock.Lock()
				defer runCntLock.Unlock()
				runCnt++
				return nil, nil
			}
			cleanupFunc := func() bool {
				runCntLock.Lock()
				defer runCntLock.Unlock()
				return runCnt > 0
			}
			_, _ = tm.RunTask("test", taskFunc, cleanupFunc)
			Eventually(func(g Gomega) {
				runCntLock.Lock()
				defer runCntLock.Unlock()
				g.Expect(runCnt).To(Equal(1))
			}).WithTimeout(time.Minute).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(tm.Len()).To(BeZero())
			}).WithTimeout(time.Minute).Should(Succeed())
		})
	})
})

func TestFuture(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Future Suite")
}
