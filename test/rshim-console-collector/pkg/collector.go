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

// Package collector provides E2E-only collection of locally attached DPU
// consoles. It discovers multiple rshim devices, identifies each DPU by its
// DEV_NAME metadata, streams console output, and reconnects when devices are
// recreated during DPU resets.
package collector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"k8s.io/klog/v2"
)

const maxMiscBytes int64 = 64 * 1024

var errLineTooLong = errors.New("console line exceeds configured maximum")

// FileSystem abstracts the rshim filesystem operations used by Collector.
type FileSystem interface {
	Glob(pattern string) ([]string, error)
	ReadFileBounded(path string, maxBytes int64) ([]byte, error)
	Open(path string) (io.ReadCloser, error)
}

// Config configures a Collector.
type Config struct {
	NodeName     string
	DevRoot      string
	ScanInterval time.Duration
	RetryMin     time.Duration
	RetryMax     time.Duration
	MaxLineBytes int
	Output       io.Writer
	FileSystem   FileSystem
	Now          func() time.Time
}

// Collector discovers rshim devices and maintains one console reader per
// device.
type Collector struct {
	nodeName     string
	devRoot      string
	scanInterval time.Duration
	retryMin     time.Duration
	retryMax     time.Duration
	maxLineBytes int
	fs           FileSystem
	output       *synchronizedOutput
	now          func() time.Time

	newTicker func(time.Duration) ticker
	wait      func(context.Context, time.Duration) bool
	workers   map[string]*worker
}

type worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	*time.Ticker
}

func (t realTicker) Chan() <-chan time.Time {
	return t.C
}

type osFileSystem struct{}

func (osFileSystem) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func (osFileSystem) ReadFileBounded(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maxBytes)
	}
	return data, nil
}

func (osFileSystem) Open(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

type synchronizedOutput struct {
	mu     sync.Mutex
	writer io.Writer
}

func (o *synchronizedOutput) write(timestamp time.Time, nodeName, deviceName string, raw []byte) error {
	line := fmt.Sprintf(
		"%s node=%s device=%s %s\n",
		timestamp.UTC().Format(time.RFC3339Nano),
		nodeName,
		deviceName,
		raw,
	)

	o.mu.Lock()
	defer o.mu.Unlock()
	_, err := io.WriteString(o.writer, line)
	return err
}

// New creates a Collector and validates its configuration.
func New(config Config) (*Collector, error) {
	if strings.TrimSpace(config.NodeName) == "" {
		return nil, errors.New("node name must not be empty")
	}
	if config.DevRoot == "" {
		return nil, errors.New("device root must not be empty")
	}
	if config.ScanInterval <= 0 {
		return nil, errors.New("scan interval must be positive")
	}
	if config.RetryMin <= 0 {
		return nil, errors.New("minimum retry interval must be positive")
	}
	if config.RetryMax < config.RetryMin {
		return nil, errors.New("maximum retry interval must not be less than minimum retry interval")
	}
	if config.MaxLineBytes <= 0 {
		return nil, errors.New("maximum line size must be positive")
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.FileSystem == nil {
		config.FileSystem = osFileSystem{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	return &Collector{
		nodeName:     config.NodeName,
		devRoot:      config.DevRoot,
		scanInterval: config.ScanInterval,
		retryMin:     config.RetryMin,
		retryMax:     config.RetryMax,
		maxLineBytes: config.MaxLineBytes,
		fs:           config.FileSystem,
		output:       &synchronizedOutput{writer: config.Output},
		now:          config.Now,
		newTicker: func(interval time.Duration) ticker {
			return realTicker{Ticker: time.NewTicker(interval)}
		},
		wait:    waitForDuration,
		workers: map[string]*worker{},
	}, nil
}

// Run reconciles devices until ctx is canceled.
func (c *Collector) Run(ctx context.Context) error {
	c.reconcile(ctx)

	scanTicker := c.newTicker(c.scanInterval)
	defer scanTicker.Stop()
	defer c.stopAllWorkers()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-scanTicker.Chan():
			c.reconcile(ctx)
		}
	}
}

func (c *Collector) reconcile(ctx context.Context) {
	consolePaths, err := discoverConsolePaths(c.fs, c.devRoot)
	if err != nil {
		klog.FromContext(ctx).Error(err, "Failed to discover rshim consoles")
		return
	}

	discovered := make(map[string]struct{}, len(consolePaths))
	for _, consolePath := range consolePaths {
		discovered[consolePath] = struct{}{}
	}

	for consolePath, currentWorker := range c.workers {
		if _, found := discovered[consolePath]; found {
			continue
		}
		klog.FromContext(ctx).Info("Rshim console disappeared", "console", consolePath)
		currentWorker.cancel()
		<-currentWorker.done
		delete(c.workers, consolePath)
	}

	for _, consolePath := range consolePaths {
		if _, found := c.workers[consolePath]; found {
			continue
		}
		workerContext, cancel := context.WithCancel(ctx)
		newWorker := &worker{
			cancel: cancel,
			done:   make(chan struct{}),
		}
		c.workers[consolePath] = newWorker
		klog.FromContext(ctx).Info("Discovered rshim console", "console", consolePath)
		go func() {
			defer close(newWorker.done)
			c.runWorker(workerContext, consolePath)
		}()
	}
}

func (c *Collector) stopAllWorkers() {
	for _, currentWorker := range c.workers {
		currentWorker.cancel()
	}
	for _, currentWorker := range c.workers {
		<-currentWorker.done
	}
	clear(c.workers)
}

func (c *Collector) runWorker(ctx context.Context, consolePath string) {
	logger := klog.FromContext(ctx)
	retryDelay := c.retryMin

	for {
		if ctx.Err() != nil {
			return
		}

		madeProgress, err := c.collectOnce(ctx, consolePath)
		if madeProgress {
			retryDelay = c.retryMin
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Error(err, "Rshim console collection interrupted", "console", consolePath, "retryAfter", retryDelay)
		}
		if !c.wait(ctx, retryDelay) {
			return
		}
		retryDelay = nextBackoff(retryDelay, c.retryMax)
	}
}

func (c *Collector) readDeviceName(ctx context.Context, consolePath string) string {
	miscPath := filepath.Join(filepath.Dir(consolePath), "misc")
	data, err := c.fs.ReadFileBounded(miscPath, maxMiscBytes)
	if err == nil {
		var deviceName string
		deviceName, err = parseDeviceName(data)
		if err == nil {
			return deviceName
		}
	}

	deviceName := filepath.Base(filepath.Dir(consolePath))
	klog.FromContext(ctx).Info(
		"Failed to read rshim DEV_NAME; using device directory as fallback",
		"error", err,
		"console", consolePath,
		"device", deviceName,
	)
	return deviceName
}

func (c *Collector) collectOnce(ctx context.Context, consolePath string) (bool, error) {
	console, err := c.fs.Open(consolePath)
	if err != nil {
		return false, fmt.Errorf("open rshim console %s: %w", consolePath, err)
	}

	logger := klog.FromContext(ctx)
	deviceName := c.readDeviceName(ctx, consolePath)
	logger.Info("Connected to rshim console", "console", consolePath, "device", deviceName)

	closeOnce := sync.Once{}
	closeConsole := func() {
		closeOnce.Do(func() {
			if err := console.Close(); err != nil {
				klog.FromContext(ctx).Error(err, "Failed to close rshim console", "console", consolePath)
			}
		})
	}
	stopCloseWatcher := make(chan struct{})
	closeWatcherDone := make(chan struct{})
	go func() {
		defer close(closeWatcherDone)
		select {
		case <-ctx.Done():
			closeConsole()
		case <-stopCloseWatcher:
		}
	}()

	madeProgress, err := c.readConsole(console, deviceName)
	close(stopCloseWatcher)
	<-closeWatcherDone
	closeConsole()

	if err != nil {
		return madeProgress, fmt.Errorf("read rshim console %s: %w", consolePath, err)
	}
	return madeProgress, nil
}

func (c *Collector) readConsole(reader io.Reader, deviceName string) (bool, error) {
	buffered := bufio.NewReader(reader)
	madeProgress := false
	for {
		line, err := readBoundedLine(buffered, c.maxLineBytes)
		if len(line) > 0 || err == nil {
			madeProgress = true
			if writeErr := c.output.write(c.now(), c.nodeName, deviceName, line); writeErr != nil {
				return madeProgress, fmt.Errorf("write console output: %w", writeErr)
			}
		}
		if err != nil {
			return madeProgress, err
		}
	}
}

func discoverConsolePaths(fs FileSystem, devRoot string) ([]string, error) {
	paths, err := fs.Glob(filepath.Join(devRoot, "rshim*", "console"))
	if err != nil {
		return nil, fmt.Errorf("glob rshim consoles: %w", err)
	}
	paths = slices.DeleteFunc(paths, func(path string) bool {
		return !isRshimDeviceDirectory(filepath.Base(filepath.Dir(path)))
	})
	sort.Strings(paths)
	return compact(paths), nil
}

func isRshimDeviceDirectory(name string) bool {
	suffix := strings.TrimPrefix(name, "rshim")
	if suffix == "" || suffix == name {
		return false
	}
	for _, character := range suffix {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func compact(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	compacted := paths[:1]
	for _, path := range paths[1:] {
		if path != compacted[len(compacted)-1] {
			compacted = append(compacted, path)
		}
	}
	return compacted
}

func parseDeviceName(data []byte) (string, error) {
	var deviceName string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] != "DEV_NAME" {
			continue
		}
		if len(fields) != 2 {
			return "", fmt.Errorf("DEV_NAME must contain exactly one value")
		}
		if deviceName != "" {
			return "", errors.New("multiple DEV_NAME entries")
		}
		if strings.IndexFunc(fields[1], func(character rune) bool {
			return unicode.IsSpace(character) || unicode.IsControl(character)
		}) >= 0 {
			return "", errors.New("DEV_NAME contains whitespace or control characters")
		}
		deviceName = fields[1]
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan DEV_NAME: %w", err)
	}
	if deviceName == "" {
		return "", errors.New("missing DEV_NAME entry")
	}
	return deviceName, nil
}

func readBoundedLine(reader *bufio.Reader, maxLineBytes int) ([]byte, error) {
	line := make([]byte, 0, min(maxLineBytes, 4096))
	for {
		fragment, err := reader.ReadSlice('\n')
		hasNewline := len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		if hasNewline {
			fragment = fragment[:len(fragment)-1]
		}
		if len(line)+len(fragment) > maxLineBytes {
			return nil, errLineTooLong
		}
		line = append(line, fragment...)

		switch {
		case hasNewline:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return line, nil
		case err != nil:
			return nil, err
		default:
			return line, nil
		}
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum-current {
		return maximum
	}
	return current * 2
}

func waitForDuration(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
