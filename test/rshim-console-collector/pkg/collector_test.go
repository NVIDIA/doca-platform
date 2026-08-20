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

package collector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testConsolePath = "/devices/rshim0/console"

var _ = Describe("Device metadata", func() {
	DescribeTable("parses exactly one DEV_NAME",
		func(metadata string, expected string, errorSubstring string) {
			deviceName, err := parseDeviceName([]byte(metadata))
			if errorSubstring != "" {
				Expect(err).To(MatchError(ContainSubstring(errorSubstring)))
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(deviceName).To(Equal(expected))
		},
		Entry("valid metadata", "DISPLAY_LEVEL 0\nDEV_NAME pcie-0000:03:00.0\nBOOT_MODE 1\n", "pcie-0000:03:00.0", ""),
		Entry("missing value", "DEV_NAME\n", "", "exactly one value"),
		Entry("extra value", "DEV_NAME pcie-0000:03:00.0 unexpected\n", "", "exactly one value"),
		Entry("missing key", "DISPLAY_LEVEL 0\n", "", "missing DEV_NAME"),
		Entry("duplicate key", "DEV_NAME first\nDEV_NAME second\n", "", "multiple DEV_NAME"),
		Entry("control character", "DEV_NAME bad\x00name\n", "", "control characters"),
	)

	It("bounds reads of the misc file", func() {
		fs := newFakeFileSystem()
		miscPath := "/devices/rshim0/misc"
		fs.setMisc(miscPath, []byte("DEV_NAME device0\n"))
		collector := newTestCollector(fs, &safeBuffer{})

		Expect(collector.readDeviceName(context.Background(), testConsolePath)).To(Equal("device0"))
		Expect(fs.lastReadLimit()).To(Equal(maxMiscBytes))
	})
})

var _ = Describe("Discovery", func() {
	It("finds, sorts, and de-duplicates rshim console paths", func() {
		fs := newFakeFileSystem()
		fs.setPaths(
			"/devices/rshim2/console",
			"/devices/rshim0/console",
			"/devices/rshim2/console",
			"/devices/rshim-invalid/console",
		)

		paths, err := discoverConsolePaths(fs, "/devices")
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(Equal([]string{
			"/devices/rshim0/console",
			"/devices/rshim2/console",
		}))
		Expect(fs.lastGlobPattern()).To(Equal(filepath.Join("/devices", "rshim*", "console")))
	})
})

var _ = Describe("Collection", func() {
	It("keeps simultaneous devices associated with their own console source", func() {
		fs := newFakeFileSystem()
		output := &safeBuffer{}
		collector := newTestCollector(fs, output)
		collector.wait = waitForCancellation

		consoleA := testConsolePath
		consoleB := "/devices/rshim1/console"
		fs.setPaths(consoleB, consoleA)
		fs.setMisc(filepath.Join(filepath.Dir(consoleA), "misc"), []byte("DEV_NAME device-a\n"))
		fs.setMisc(filepath.Join(filepath.Dir(consoleB), "misc"), []byte("DEV_NAME device-b\n"))
		fs.setOpener(consoleA, func() io.ReadCloser {
			return io.NopCloser(strings.NewReader("source-a\n"))
		})
		fs.setOpener(consoleB, func() io.ReadCloser {
			return io.NopCloser(strings.NewReader("source-b\n"))
		})

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(func() {
			cancel()
			collector.stopAllWorkers()
		})
		collector.reconcile(ctx)

		Eventually(output.String).Should(ContainSubstring("device=device-a source-a\n"))
		Eventually(output.String).Should(ContainSubstring("device=device-b source-b\n"))
		Expect(output.String()).NotTo(ContainSubstring("device=device-a source-b"))
		Expect(output.String()).NotTo(ContainSubstring("device=device-b source-a"))
	})

	It("reads DEV_NAME after opening the console", func() {
		fs := newFakeFileSystem()
		output := &safeBuffer{}
		collector := newTestCollector(fs, output)
		consolePath := testConsolePath
		miscPath := filepath.Join(filepath.Dir(consolePath), "misc")
		fs.setOpener(consolePath, func() io.ReadCloser {
			fs.setMisc(miscPath, []byte("DEV_NAME device-after-open\n"))
			return io.NopCloser(strings.NewReader("payload\n"))
		})

		madeProgress, err := collector.collectOnce(context.Background(), consolePath)

		Expect(madeProgress).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("EOF")))
		Expect(output.String()).To(ContainSubstring("device=device-after-open payload\n"))
	})

	It("uses the rshim directory when DEV_NAME is unavailable", func() {
		fs := newFakeFileSystem()
		output := &safeBuffer{}
		collector := newTestCollector(fs, output)
		fs.setOpener(testConsolePath, func() io.ReadCloser {
			return io.NopCloser(strings.NewReader("fallback-payload\n"))
		})

		madeProgress, err := collector.collectOnce(context.Background(), testConsolePath)

		Expect(madeProgress).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("EOF")))
		Expect(output.String()).To(ContainSubstring("device=rshim0 fallback-payload\n"))
	})

	It("retries after EOF and refreshes DEV_NAME before reopening", func() {
		fs := newFakeFileSystem()
		output := &safeBuffer{}
		collector := newTestCollector(fs, output)
		consolePath := testConsolePath
		miscPath := filepath.Join(filepath.Dir(consolePath), "misc")
		fs.setMisc(miscPath, []byte("DEV_NAME old-device\n"))
		fs.setOpener(consolePath, func() io.ReadCloser {
			return io.NopCloser(strings.NewReader("payload\n"))
		})

		waiting := make(chan time.Duration, 4)
		continueRetry := make(chan struct{}, 4)
		collector.wait = func(ctx context.Context, duration time.Duration) bool {
			waiting <- duration
			select {
			case <-ctx.Done():
				return false
			case <-continueRetry:
				return true
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			collector.runWorker(ctx, consolePath)
		}()
		DeferCleanup(func() {
			cancel()
			Eventually(done).Should(BeClosed())
		})

		Eventually(output.String).Should(ContainSubstring("device=old-device payload\n"))
		Expect(<-waiting).To(Equal(time.Millisecond))

		fs.setMisc(miscPath, []byte("DEV_NAME new-device\n"))
		continueRetry <- struct{}{}

		Eventually(output.String).Should(ContainSubstring("device=new-device payload\n"))
		Expect(<-waiting).To(Equal(time.Millisecond))
		Expect(fs.openCount(consolePath)).To(BeNumerically(">=", 2))
	})

	It("cancels a disappeared device and starts it again when it reappears", func() {
		fs := newFakeFileSystem()
		output := &safeBuffer{}
		collector := newTestCollector(fs, output)
		collector.wait = waitForCancellation
		consolePath := testConsolePath
		miscPath := filepath.Join(filepath.Dir(consolePath), "misc")
		firstConsole := newBlockingReadCloser()

		fs.setPaths(consolePath)
		fs.setMisc(miscPath, []byte("DEV_NAME first-device\n"))
		fs.setOpener(consolePath, func() io.ReadCloser {
			return firstConsole
		})

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(func() {
			cancel()
			collector.stopAllWorkers()
		})
		collector.reconcile(ctx)
		Eventually(firstConsole.readStarted).Should(BeClosed())

		fs.setPaths()
		collector.reconcile(ctx)
		Expect(firstConsole.closed).To(BeClosed())

		fs.setMisc(miscPath, []byte("DEV_NAME second-device\n"))
		fs.setOpener(consolePath, func() io.ReadCloser {
			return io.NopCloser(strings.NewReader("after-reappearance\n"))
		})
		fs.setPaths(consolePath)
		collector.reconcile(ctx)

		Eventually(output.String).Should(ContainSubstring("device=second-device after-reappearance\n"))
	})

	It("actively closes a blocked console when collection is canceled", func() {
		fs := newFakeFileSystem()
		collector := newTestCollector(fs, &safeBuffer{})
		consolePath := testConsolePath
		miscPath := filepath.Join(filepath.Dir(consolePath), "misc")
		blockedConsole := newBlockingReadCloser()
		manualScanTicker := newManualTicker()

		fs.setPaths(consolePath)
		fs.setMisc(miscPath, []byte("DEV_NAME device0\n"))
		fs.setOpener(consolePath, func() io.ReadCloser {
			return blockedConsole
		})
		collector.newTicker = func(time.Duration) ticker {
			return manualScanTicker
		}

		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() {
			runDone <- collector.Run(ctx)
		}()

		Eventually(blockedConsole.readStarted).Should(BeClosed())
		cancel()

		Eventually(blockedConsole.closed).Should(BeClosed())
		Eventually(runDone).Should(Receive(BeNil()))
		Expect(manualScanTicker.stopped).To(BeClosed())
	})

	It("uses capped exponential retry delays", func() {
		fs := newFakeFileSystem()
		collector := newTestCollector(fs, &safeBuffer{})
		collector.retryMin = time.Second
		collector.retryMax = 4 * time.Second
		consolePath := testConsolePath

		waiting := make(chan time.Duration, 5)
		continueRetry := make(chan struct{}, 5)
		collector.wait = func(ctx context.Context, duration time.Duration) bool {
			waiting <- duration
			select {
			case <-ctx.Done():
				return false
			case <-continueRetry:
				return true
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			collector.runWorker(ctx, consolePath)
		}()
		DeferCleanup(func() {
			cancel()
			Eventually(done).Should(BeClosed())
		})

		for _, expected := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second} {
			Expect(<-waiting).To(Equal(expected))
			continueRetry <- struct{}{}
		}
	})

	It("resets retry delay after console output is captured", func() {
		fs := newFakeFileSystem()
		output := &safeBuffer{}
		collector := newTestCollector(fs, output)
		collector.retryMin = time.Second
		collector.retryMax = 4 * time.Second
		consolePath := testConsolePath
		fs.setMisc(filepath.Join(filepath.Dir(consolePath), "misc"), []byte("DEV_NAME device0\n"))

		waiting := make(chan time.Duration, 3)
		continueRetry := make(chan struct{}, 3)
		collector.wait = func(ctx context.Context, duration time.Duration) bool {
			waiting <- duration
			select {
			case <-ctx.Done():
				return false
			case <-continueRetry:
				return true
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			collector.runWorker(ctx, consolePath)
		}()
		DeferCleanup(func() {
			cancel()
			Eventually(done).Should(BeClosed())
		})

		Expect(<-waiting).To(Equal(time.Second))
		continueRetry <- struct{}{}
		Expect(<-waiting).To(Equal(2 * time.Second))

		fs.setOpener(consolePath, func() io.ReadCloser {
			return io.NopCloser(strings.NewReader("payload\n"))
		})
		continueRetry <- struct{}{}

		Eventually(output.String).Should(ContainSubstring("device=device0 payload\n"))
		Expect(<-waiting).To(Equal(time.Second))
	})

	It("writes the exact timestamped line format", func() {
		output := &safeBuffer{}
		collector := newTestCollector(newFakeFileSystem(), output)
		collector.nodeName = "node-a"
		collector.now = func() time.Time {
			return time.Date(2026, time.August, 13, 4, 5, 6, 123456789, time.FixedZone("offset", 8*60*60))
		}

		madeProgress, err := collector.readConsole(strings.NewReader("raw console payload\n"), "pcie-0000:03:00.0")
		Expect(madeProgress).To(BeTrue())
		Expect(err).To(MatchError(io.EOF))
		Expect(output.String()).To(Equal(
			"2026-08-12T20:05:06.123456789Z node=node-a device=pcie-0000:03:00.0 raw console payload\n",
		))
	})

	It("records progress when reading succeeds but writing fails", func() {
		collector := newTestCollector(newFakeFileSystem(), failingWriter{})

		madeProgress, err := collector.readConsole(strings.NewReader("payload\n"), "device0")

		Expect(madeProgress).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("write console output")))
	})

	It("rejects a line larger than the configured limit", func() {
		collector := newTestCollector(newFakeFileSystem(), &safeBuffer{})
		collector.maxLineBytes = 4

		madeProgress, err := collector.readConsole(strings.NewReader("12345\n"), "device0")
		Expect(madeProgress).To(BeFalse())
		Expect(err).To(MatchError(errLineTooLong))
	})
})

func newTestCollector(fs FileSystem, output io.Writer) *Collector {
	collector, err := New(Config{
		NodeName:     "test-node",
		DevRoot:      "/devices",
		ScanInterval: time.Second,
		RetryMin:     time.Millisecond,
		RetryMax:     10 * time.Millisecond,
		MaxLineBytes: 1024,
		Output:       output,
		FileSystem:   fs,
		Now: func() time.Time {
			return time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return collector
}

func waitForCancellation(ctx context.Context, _ time.Duration) bool {
	<-ctx.Done()
	return false
}

type fakeFileSystem struct {
	mu          sync.Mutex
	paths       []string
	misc        map[string][]byte
	openers     map[string]func() io.ReadCloser
	openCounts  map[string]int
	globPattern string
	readLimit   int64
}

func newFakeFileSystem() *fakeFileSystem {
	return &fakeFileSystem{
		misc:       map[string][]byte{},
		openers:    map[string]func() io.ReadCloser{},
		openCounts: map[string]int{},
	}
}

func (f *fakeFileSystem) Glob(pattern string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.globPattern = pattern
	return append([]string(nil), f.paths...), nil
}

func (f *fakeFileSystem) ReadFileBounded(path string, maxBytes int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readLimit = maxBytes
	data, found := f.misc[path]
	if !found {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("file exceeds read limit")
	}
	return bytes.Clone(data), nil
}

func (f *fakeFileSystem) Open(path string) (io.ReadCloser, error) {
	f.mu.Lock()
	opener, found := f.openers[path]
	if found {
		f.openCounts[path]++
	}
	f.mu.Unlock()
	if !found {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	return opener(), nil
}

func (f *fakeFileSystem) setPaths(paths ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append([]string(nil), paths...)
}

func (f *fakeFileSystem) setMisc(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.misc[path] = bytes.Clone(data)
}

func (f *fakeFileSystem) setOpener(path string, opener func() io.ReadCloser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openers[path] = opener
}

func (f *fakeFileSystem) openCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openCounts[path]
}

func (f *fakeFileSystem) lastGlobPattern() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.globPattern
}

func (f *fakeFileSystem) lastReadLimit() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readLimit
}

type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *safeBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type blockingReadCloser struct {
	readStarted chan struct{}
	closed      chan struct{}
	startOnce   sync.Once
	closeOnce   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	r.startOnce.Do(func() {
		close(r.readStarted)
	})
	<-r.closed
	return 0, os.ErrClosed
}

func (r *blockingReadCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
	})
	return nil
}

type manualTicker struct {
	channel chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newManualTicker() *manualTicker {
	return &manualTicker{
		channel: make(chan time.Time),
		stopped: make(chan struct{}),
	}
}

func (t *manualTicker) Chan() <-chan time.Time {
	return t.channel
}

func (t *manualTicker) Stop() {
	t.once.Do(func() {
		close(t.stopped)
	})
}
