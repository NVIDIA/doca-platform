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

package controller

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WatchVSwitchdRestarts", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		pidFile    string
		restartsMu sync.Mutex
		restarts   int
	)

	countRestart := func(context.Context) {
		restartsMu.Lock()
		defer restartsMu.Unlock()
		restarts++
	}

	restartCount := func() int {
		restartsMu.Lock()
		defer restartsMu.Unlock()
		return restarts
	}

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		restarts = 0
		pidFile = filepath.Join(GinkgoT().TempDir(), "ovs-vswitchd.pid")
	})

	AfterEach(func() {
		cancel()
	})

	It("does not call onRestart when the pidfile is created for the first time", func() {
		Expect(WatchVSwitchdRestarts(ctx, pidFile, countRestart)).To(Succeed())

		Expect(os.WriteFile(pidFile, []byte("100\n"), 0o644)).To(Succeed())

		Consistently(restartCount).WithTimeout(1 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(0))
	})

	It("calls onRestart when the pidfile is removed, as ovs-vswitchd does on shutdown", func() {
		Expect(os.WriteFile(pidFile, []byte("100\n"), 0o644)).To(Succeed())
		Expect(WatchVSwitchdRestarts(ctx, pidFile, countRestart)).To(Succeed())

		Expect(os.Remove(pidFile)).To(Succeed())

		Eventually(restartCount).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(1))
	})

	It("calls onRestart once for a full remove-then-recreate restart cycle", func() {
		Expect(os.WriteFile(pidFile, []byte("100\n"), 0o644)).To(Succeed())
		Expect(WatchVSwitchdRestarts(ctx, pidFile, countRestart)).To(Succeed())

		By("simulating the old ovs-vswitchd unlinking its pidfile on exit")
		Expect(os.Remove(pidFile)).To(Succeed())
		Eventually(restartCount).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(1))

		By("simulating the new ovs-vswitchd creating a fresh pidfile via write-to-temp + rename")
		tmp := pidFile + ".tmp"
		Expect(os.WriteFile(tmp, []byte("200\n"), 0o644)).To(Succeed())
		Expect(os.Rename(tmp, pidFile)).To(Succeed())

		Consistently(restartCount).WithTimeout(1 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(1))
	})

	It("ignores unrelated files in the same directory", func() {
		Expect(WatchVSwitchdRestarts(ctx, pidFile, countRestart)).To(Succeed())

		unrelated := filepath.Join(filepath.Dir(pidFile), "unrelated")
		Expect(os.WriteFile(unrelated, []byte("100\n"), 0o644)).To(Succeed())
		Expect(os.Remove(unrelated)).To(Succeed())

		Consistently(restartCount).WithTimeout(1 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(0))
	})

	It("stops watching once the context is canceled", func() {
		Expect(os.WriteFile(pidFile, []byte("100\n"), 0o644)).To(Succeed())
		Expect(WatchVSwitchdRestarts(ctx, pidFile, countRestart)).To(Succeed())
		cancel()

		// Give the watcher goroutine a chance to observe cancellation before this removal.
		time.Sleep(100 * time.Millisecond)
		Expect(os.Remove(pidFile)).To(Succeed())

		Consistently(restartCount).WithTimeout(1 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(0))
	})

	It("returns an error if the pidfile's directory doesn't exist", func() {
		err := WatchVSwitchdRestarts(ctx, filepath.Join(GinkgoT().TempDir(), "missing-dir", "ovs-vswitchd.pid"), countRestart)
		Expect(err).To(HaveOccurred())
	})

	It("recovers from a panic in onRestart and keeps watching for further restarts", func() {
		panicOnce := func(context.Context) {
			restartsMu.Lock()
			defer restartsMu.Unlock()
			restarts++
			if restarts == 1 {
				panic("boom")
			}
		}

		Expect(os.WriteFile(pidFile, []byte("100\n"), 0o644)).To(Succeed())
		Expect(WatchVSwitchdRestarts(ctx, pidFile, panicOnce)).To(Succeed())

		Expect(os.Remove(pidFile)).To(Succeed())
		Eventually(restartCount).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(1))

		By("a later restart is still observed despite the earlier panic")
		Expect(os.WriteFile(pidFile, []byte("200\n"), 0o644)).To(Succeed())
		// Gap avoids a macOS/kqueue quirk that coalesces a same-path write+remove into a no-op.
		time.Sleep(200 * time.Millisecond)
		Expect(os.Remove(pidFile)).To(Succeed())
		Eventually(restartCount).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(2))
	})
})
