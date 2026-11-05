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
	"os"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/util"

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
func Collect(ctx context.Context, c client.Client, opts CollectOptions) error {
	opts = opts.withDefaults()
	if !opts.Quiet {
		util.Step("Collecting BMC diagnostic dumps")
	}

	// Resolve targets before touching the filesystem so we do not create an
	// output directory or write artifacts when there is nothing to collect.
	targets, discoveryErr := getLogTargets(ctx, c, opts)
	if err := requireTargets(targets, discoveryErr, opts.Quiet); err != nil {
		return err
	}

	// Resolved once, before any artifact is written, so a run that cannot verify leaves nothing behind.
	if err := prepareTLSVerification(ctx, c, &opts); err != nil {
		return err
	}

	if err := os.MkdirAll(opts.OutputDir, 0700); err != nil {
		return fmt.Errorf("creating bmc artifact directory %s: %w", opts.OutputDir, err)
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
