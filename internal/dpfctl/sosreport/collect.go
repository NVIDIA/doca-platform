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

package sosreport

import (
	"context"
	"fmt"
	"time"
)

// CollectOptions contains the configuration for the full collect workflow.
type CollectOptions struct {
	StartOptions

	// OutputDir is the local directory for downloaded reports.
	OutputDir string
}

// DownloadOptions contains the configuration for the download workflow.
type DownloadOptions struct {
	OutputDir      string
	CaseID         string
	Namespace      string
	Archive        bool
	ArchiveOnly    bool
	ShowStatusHint bool
}

// Collect orchestrates the full SOS report workflow: start Jobs, wait for
// completion, download reports, and clean up. It is the programmatic equivalent
// of `dpfctl sosreport collect`.
func Collect(ctx context.Context, opts CollectOptions) error {
	if err := ValidateStartOptions(&opts.StartOptions); err != nil {
		return err
	}

	targets, err := GetClusterTargets(ctx, opts.Cluster, opts.DPUCluster)
	if err != nil {
		return fmt.Errorf("get cluster targets: %w", err)
	}
	defer targets.Close()

	// Step 1: Start
	hostClient, err := GetHostClient(targets)
	if err != nil {
		return fmt.Errorf("get host client: %w", err)
	}
	startedTargets, err := Start(ctx, targets, hostClient, opts.StartOptions)
	if err != nil {
		if ctx.Err() != nil {
			Warn("Interrupted during start")
			return fmt.Errorf("interrupted")
		}
		return fmt.Errorf("start failed: %w", err)
	}

	// Step 2: Wait
	Step("Waiting for SOS report Jobs to complete")
	if err := WaitForAll(ctx, startedTargets, opts.Namespace, opts.CaseID, opts.Timeout); err != nil {
		if ctx.Err() != nil {
			Warn("Interrupted")
		} else {
			Warn("%v", err)
		}
	}

	// Step 3: For NFS mode, just clean up. For local mode, download then clean up.
	if opts.Output == OutputNFS {
		if opts.NFSSubDir != "" {
			if opts.ArchiveOnly {
				Result("Archive created on NFS: %s/%s.tar.gz", opts.NFSPath, opts.NFSSubDir)
			} else {
				Result("Reports written to NFS: %s/%s", opts.NFSPath, opts.NFSSubDir)
				if opts.Archive {
					Result("Archive created on NFS: %s/%s.tar.gz", opts.NFSPath, opts.NFSSubDir)
				}
			}
		} else {
			Result("Reports written to NFS: %s", opts.NFSPath)
		}
		Step("Cleaning up")
		Cleanup(context.Background(), startedTargets, opts.Namespace, opts.CaseID)
		return nil
	}

	// Download — collect always cleans up after itself.
	// Use a fresh context — the original context may already be canceled
	// if the caller interrupted during the wait phase. We still want to
	// attempt downloading whatever reports are ready.
	if ctx.Err() != nil {
		Warn("Wait was interrupted. Proceeding to download all available reports; results may be incomplete")
	}
	Step("Downloading SOS reports")
	dlCtx, dlCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer dlCancel()

	dlOpts := DownloadOptions{
		OutputDir:   opts.OutputDir,
		CaseID:      opts.CaseID,
		Namespace:   opts.Namespace,
		Archive:     opts.Archive,
		ArchiveOnly: opts.ArchiveOnly,
	}
	if err := DownloadAndArchive(dlCtx, startedTargets, dlOpts); err != nil {
		Warn("Download failed: %v", err)
		Warn("Skipping cleanup — resources preserved for retry via 'dpfctl sosreport download'")
		return fmt.Errorf("download failed: %w", err)
	}

	// Always clean up after collect — reuse existing startedTargets.
	Step("Cleaning up")
	Cleanup(context.Background(), startedTargets, opts.Namespace, opts.CaseID)
	return nil
}
