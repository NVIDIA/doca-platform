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

// Package bmcdump collects BlueField BMC diagnostic dumps for DPUDevices.
package bmcdump

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/util"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CollectOptions contains BMC diagnostic dump collection settings.
type CollectOptions struct {
	// Namespace is the namespace that contains DPUDevice objects and their BMC credential Secrets.
	Namespace string
	// OutputDir is the local directory where dump artifacts are written.
	OutputDir string
	// Devices limits collection to the named DPUDevices. Empty means all DPUDevices with BMC IPs.
	Devices []string
	// Username overrides the Redfish user for the BlueField BMC. Empty means the
	// user is derived from the BMC generation reported by the root service.
	Username string
	// RequestTimeout is the per-request Redfish timeout.
	RequestTimeout time.Duration
	// TaskTimeout is the maximum time to wait for a BMC dump task to complete.
	TaskTimeout time.Duration
	// ClearExisting controls whether existing BMC dump entries are deleted before creating a new dump.
	ClearExisting bool
	// CACertFile is a PEM file holding the CA that signed the BMC server certificates. Empty means
	// the CA is read from the trust bundle the DPF operator publishes, in the DPFOperatorConfig namespace (Namespace if it cannot be listed).
	CACertFile string
	// BMCAddress collects from this one BMC instead of discovering DPUDevices. No Kubernetes client
	// is used: the password comes from PasswordFile and the CA from CACertFile.
	BMCAddress string
	// PasswordFile holds the BMC password for BMCAddress.
	PasswordFile string
	// StopAt, when set, ends the collection at that time. Until then, failed read requests are
	// repeated, so a BMC that drops off the network for a while is still collected from.
	StopAt time.Time
	// ArchiveDir stores the run under <ArchiveDir>/<Serial>/<timestamp>-<DPUUID>/ and then keeps
	// only the newest archives of that serial. It replaces OutputDir and requires BMCAddress.
	ArchiveDir string
	// Serial and DPUUID name the archive directory. Both must be valid label values, because they
	// are used as path segments.
	Serial string
	DPUUID string
	// ArchiveMinFreeBytes is the free space in ArchiveDir below which nothing is collected. Zero
	// means minArchiveFreeBytes, which suits a volume shared with the OS images.
	ArchiveMinFreeBytes int64
	// PrintLogs, when set, receives the collection as JSON lines: a header, the selected logs of
	// the dumps, and a result. Nothing else is printed then. It requires BMCAddress.
	PrintLogs io.Writer
	// Header holds extra fields for the header line, such as the DPU's name and why it failed.
	Header map[string]string
	// Quiet suppresses CLI progress output.
	Quiet bool
	// TLSConfig, when set, is used as is for every BMC connection: Collect then resolves no CA and
	// verifies nothing itself. It exists so the E2E suite can still collect logs from a BMC that has
	// not been issued its DPF certificate yet (factory reset, early provisioning). dpfctl never sets it.
	TLSConfig *tls.Config

	// caPool is resolved by Collect from CACertFile or the published trust bundle; unexported so
	// callers cannot substitute their own.
	caPool *x509.CertPool
}

// Collect resolves DPUDevice BMC endpoints, creates diagnostic dumps, waits for
// completion, and downloads the dump archive and supporting JSON artifacts.
func Collect(ctx context.Context, c client.Client, opts CollectOptions) (err error) {
	if err := opts.validate(); err != nil {
		return err
	}
	opts = opts.withDefaults()
	if !opts.StopAt.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, opts.StopAt)
		defer cancel()
	}

	// What has to happen on every way out, in this order: the logs are printed from the dumps
	// before anything removes them.
	var out *logOutput
	var cleanup []func()
	var archiveRun string
	defer func() {
		if archiveRun != "" {
			pruneErr := finishArchive(archiveRun)
			switch {
			case out != nil:
				out.pruned(pruneErr)
			case pruneErr != nil && !opts.Quiet:
				util.Warn("Pruning BMC dump archives: %v", pruneErr)
			}
		}
		if out != nil {
			if readErr := out.finish(err); err == nil {
				err = readErr
			}
		}
		for _, clean := range cleanup {
			clean()
		}
	}()
	if opts.PrintLogs != nil {
		// The output is a stream of JSON lines for a log pipeline, so nothing else may be printed.
		opts.Quiet = true
		out = newLogOutput(opts)
	}
	if !opts.Quiet {
		util.Step("Collecting BMC diagnostic dumps")
	}

	// Resolve targets before touching the filesystem so we do not create an
	// output directory or write artifacts when there is nothing to collect.
	targets, discoveryErr := resolveTargets(ctx, c, opts)
	if err := requireTargets(targets, discoveryErr, opts.Quiet); err != nil {
		return err
	}

	// Resolved once, before any artifact is written, so a run that cannot verify leaves nothing behind.
	if err := prepareTLSVerification(ctx, c, &opts); err != nil {
		return err
	}

	if opts.ArchiveDir != "" {
		runDir, archiveErr := newArchiveRunDir(opts)
		switch {
		case archiveErr == nil:
			opts.OutputDir, archiveRun = runDir, runDir
			if out != nil {
				out.archive = "stored"
			}
		case out == nil:
			return archiveErr
		default:
			// The logs are still worth printing, so the dumps go to a directory that is removed
			// once they are printed.
			tempDir, err := os.MkdirTemp("", "bmcdump-")
			if err != nil {
				return errors.Join(archiveErr, err)
			}
			opts.OutputDir = tempDir
			cleanup = append(cleanup, func() { _ = os.RemoveAll(tempDir) })
			out.archive = archiveErr.Error()
		}
	}
	if err := os.MkdirAll(opts.OutputDir, 0700); err != nil {
		return fmt.Errorf("creating bmc artifact directory %s: %w", opts.OutputDir, err)
	}
	if out != nil {
		out.dir = opts.OutputDir
		// An output directory used before holds that run's dumps, and only this run's are printed.
		for _, unit := range []string{managerUnitName, systemUnitName} {
			_ = os.Remove(filepath.Join(opts.OutputDir, unit, dumpArchiveName))
		}
	}
	if discoveryErr != nil && !opts.Quiet {
		util.Warn("Some DPUDevices were skipped: %v", discoveryErr)
	}
	if !opts.Quiet {
		util.Info("Found %d BMC target(s)", len(targets))
	}

	var errs []error
	for _, target := range targets {
		if err := collectDumpWithOutput(ctx, target, opts.OutputDir, opts); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if !opts.Quiet {
		util.Result("BMC dumps written to %s", opts.OutputDir)
	}
	return nil
}

// resolveTargets returns the one BMC named by address, or discovers the DPUDevices' BMCs.
func resolveTargets(ctx context.Context, c client.Client, opts CollectOptions) ([]logTarget, error) {
	if opts.BMCAddress == "" {
		return getLogTargets(ctx, c, opts)
	}
	password, err := os.ReadFile(opts.PasswordFile)
	if err != nil {
		return nil, fmt.Errorf("reading bmc password file: %w", err)
	}
	// Kept apart, as for a discovered BMC, so the saved metadata names the port that was used.
	host, port := opts.BMCAddress, uint64(defaultPort)
	if h, p, err := net.SplitHostPort(opts.BMCAddress); err == nil {
		if n, err := strconv.ParseUint(p, 10, 16); err == nil {
			host, port = h, n
		}
	}
	return []logTarget{{
		IP:   host,
		Port: uint32(port),
		// A file written by hand ends with a newline; a mounted Secret does not.
		Password: strings.TrimRight(string(password), "\r\n"),
	}}, nil
}

// validate rejects option combinations before anything is read or written.
func (o CollectOptions) validate() error {
	if o.BMCAddress != "" {
		if o.PasswordFile == "" {
			return errors.New("a bmc address requires a password file")
		}
		if o.CACertFile == "" && o.TLSConfig == nil {
			return errors.New("a bmc address requires a CA certificate file")
		}
		if len(o.Devices) > 0 {
			return errors.New("a bmc address and DPUDevice names are mutually exclusive")
		}
	}
	if o.PrintLogs != nil && o.BMCAddress == "" {
		return errors.New("printing the logs requires a bmc address")
	}
	// An option that would be ignored is a mistake in the command line, not a default.
	switch {
	case o.PasswordFile != "" && o.BMCAddress == "":
		return errors.New("a password file requires a bmc address")
	case o.Serial != "" && o.BMCAddress == "":
		return errors.New("a serial requires a bmc address")
	case len(o.Header) > 0 && o.PrintLogs == nil:
		return errors.New("header fields require printing the logs")
	case o.DPUUID != "" && o.ArchiveDir == "":
		return errors.New("a DPU UID requires an archive directory")
	}
	if o.ArchiveMinFreeBytes < 0 {
		return errors.New("the minimum free space cannot be negative")
	}
	if o.ArchiveDir == "" {
		return nil
	}
	if o.BMCAddress == "" {
		return errors.New("an archive directory requires a bmc address")
	}
	if o.OutputDir != "" {
		return errors.New("an archive directory and an output directory are mutually exclusive")
	}
	if o.Serial == "" {
		return errors.New("an archive directory requires a serial")
	}
	for name, value := range map[string]string{"serial": o.Serial, "DPU UID": o.DPUUID} {
		if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
			return fmt.Errorf("%s %q cannot be used in an archive path: %s", name, value, strings.Join(errs, "; "))
		}
	}
	return nil
}

// requireTargets fails when discovery found nothing to collect, reporting the discovery error when
// there was one.
func requireTargets(targets []logTarget, discoveryErr error, quiet bool) error {
	if len(targets) > 0 {
		return nil
	}
	if discoveryErr != nil {
		if !quiet {
			util.ResultFail("No DPUDevice BMC target found: %v", discoveryErr)
		}
		return discoveryErr
	}
	if !quiet {
		util.ResultFail("No DPUDevice BMC target found")
	}
	return fmt.Errorf("bmc dump collection skipped: no DPUDevice BMC target found")
}

// prepareTLSVerification loads the CA that BMC server certificates are verified against into opts,
// unless the caller supplied its own TLSConfig.
func prepareTLSVerification(ctx context.Context, c client.Client, opts *CollectOptions) error {
	if opts.TLSConfig != nil {
		return nil
	}
	caPool, err := resolveCAPool(ctx, c, *opts)
	if err != nil {
		if !opts.Quiet {
			util.ResultFail("Cannot verify BMC server certificates: %v", err)
		}
		return err
	}
	opts.caPool = caPool
	return nil
}

func (o CollectOptions) withDefaults() CollectOptions {
	if o.Namespace == "" {
		o.Namespace = DefaultNamespace
	}
	if o.OutputDir == "" {
		o.OutputDir = fmt.Sprintf("bmcdump-%s", time.Now().Format("20060102-150405"))
	}
	if o.RequestTimeout == 0 {
		o.RequestTimeout = defaultRequestTimeout
	}
	if o.TaskTimeout == 0 {
		o.TaskTimeout = defaultTaskTimeout
	}
	return o
}
