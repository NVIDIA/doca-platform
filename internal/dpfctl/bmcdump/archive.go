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

package bmcdump

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// archivesKeptPerSerial is how many runs stay on disk for one serial.
	archivesKeptPerSerial = 3
	// minArchiveFreeBytes is the default free space below which nothing is archived. By default the
	// archive volume also holds the OS images that provisioning serves, and those must keep fitting.
	minArchiveFreeBytes = 2 << 30
	// archiveRunTimeFormat starts the name of every run directory.
	archiveRunTimeFormat = "20060102T150405Z"
)

// freeBytes is a variable so tests can simulate a full volume.
var freeBytes = func(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	// Bsize is int64 on Linux and uint32 on macOS, and never negative.
	return stat.Bavail * uint64(stat.Bsize), nil
}

// newArchiveRunDir returns the directory for this run, <ArchiveDir>/<Serial>/<timestamp>[-<DPUUID>].
// The timestamp comes first so that names sort by age. The directory is created here and must be
// new: a run never writes into the directory of another.
func newArchiveRunDir(opts CollectOptions) (string, error) {
	// Created first: the free space of a directory that does not exist cannot be read.
	if err := os.MkdirAll(opts.ArchiveDir, 0700); err != nil {
		return "", err
	}
	free, err := freeBytes(opts.ArchiveDir)
	if err != nil {
		return "", fmt.Errorf("checking free space in bmc archive directory %s: %w", opts.ArchiveDir, err)
	}
	minFree := opts.ArchiveMinFreeBytes
	if minFree == 0 {
		minFree = minArchiveFreeBytes
	}
	// validate rejects a negative minimum, so the conversion cannot wrap.
	if free < uint64(minFree) {
		return "", fmt.Errorf("bmc dump collection skipped: %d bytes free in %s, below the %d byte minimum",
			free, opts.ArchiveDir, minFree)
	}
	name := time.Now().UTC().Format(archiveRunTimeFormat)
	if opts.DPUUID != "" {
		name += "-" + opts.DPUUID
	}
	runDir := filepath.Join(opts.ArchiveDir, opts.Serial, name)
	if err := os.MkdirAll(filepath.Dir(runDir), 0700); err != nil {
		return "", err
	}
	return runDir, os.Mkdir(runDir, 0700)
}

// A run that has no dump is another collection in progress, unless it is older than any collection
// can run.
const abandonedRunAge = 2 * time.Hour

// hasDump reports whether a run directory holds at least one downloaded dump.
func hasDump(runDir string) bool {
	units, _ := os.ReadDir(runDir)
	for _, unit := range units {
		if _, err := os.Stat(filepath.Join(runDir, unit.Name(), dumpArchiveName)); err == nil {
			return true
		}
	}
	return false
}

// finishArchive removes a run that stored no dump, so it cannot push a real archive out, and then
// keeps the newest archivesKeptPerSerial runs of the serial. Only directories named as runs are
// counted or removed.
func finishArchive(runDir string) error {
	var errs []error
	if !hasDump(runDir) {
		errs = append(errs, os.RemoveAll(runDir))
	}

	serialDir := filepath.Dir(runDir)
	entries, err := os.ReadDir(serialDir) // sorted by name, so oldest first
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	var runs []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || len(name) < len(archiveRunTimeFormat) {
			continue
		}
		started, err := time.Parse(archiveRunTimeFormat, name[:len(archiveRunTimeFormat)])
		switch {
		case err != nil:
		case hasDump(filepath.Join(serialDir, name)):
			runs = append(runs, name)
		case time.Since(started) > abandonedRunAge:
			errs = append(errs, os.RemoveAll(filepath.Join(serialDir, name)))
		}
	}
	for i := 0; i < len(runs)-archivesKeptPerSerial; i++ {
		// The dumps of this run are still to be printed, however many runs finished after it started.
		if runs[i] != filepath.Base(runDir) {
			errs = append(errs, os.RemoveAll(filepath.Join(serialDir, runs[i])))
		}
	}
	return errors.Join(errs...)
}
