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
	"fmt"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// DefaultOVSVswitchdPidFile is the standard location ovs-vswitchd writes its PID to.
const DefaultOVSVswitchdPidFile = "/var/run/openvswitch/ovs-vswitchd.pid"

// WatchVSwitchdRestarts calls onRestart whenever pidFile is deleted (ovs-vswitchd unlinks it on every restart), until ctx is canceled.
func WatchVSwitchdRestarts(ctx context.Context, pidFile string, onRestart func(ctx context.Context)) error {
	log := ctrllog.FromContext(ctx).WithValues("pidFile", pidFile)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the directory, not the file: a watch on the file itself wouldn't survive removal.
	dir := filepath.Dir(pidFile)
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		return err
	}

	restartCh := make(chan struct{}, 1)

	go watchPidFile(ctx, log, watcher, pidFile, restartCh)
	go runOnRestartWorker(ctx, log, restartCh, onRestart)

	return nil
}

// watchPidFile signals restartCh (non-blocking) whenever pidFile is removed, until ctx is canceled or watcher is closed.
func watchPidFile(ctx context.Context, log logr.Logger, watcher *fsnotify.Watcher, pidFile string, restartCh chan<- struct{}) {
	defer func() { _ = watcher.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) != filepath.Clean(pidFile) || ev.Op&fsnotify.Remove == 0 {
				continue
			}
			log.Info("detected ovs-vswitchd pidfile removal, signaling restart")
			select {
			case restartCh <- struct{}{}:
			default:
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error(err, "error watching ovs-vswitchd pidfile")
		}
	}
}

// runOnRestartWorker calls onRestart, via callOnRestart, once per restartCh signal, until ctx is canceled.
func runOnRestartWorker(ctx context.Context, log logr.Logger, restartCh <-chan struct{}, onRestart func(ctx context.Context)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-restartCh:
			callOnRestart(ctx, log, onRestart)
		}
	}
}

// callOnRestart invokes onRestart, recovering from any panic so it can't take down the whole process.
func callOnRestart(ctx context.Context, log logr.Logger, onRestart func(ctx context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Errorf("%v", r), "recovered from panic in ovs-vswitchd restart callback")
		}
	}()
	onRestart(ctx)
}
