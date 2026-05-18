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
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StartOptions contains the configuration for starting SOS report collection.
type StartOptions struct {
	Namespace    string
	Image        string
	CaseID       string
	Output       OutputMode
	NFSServer    string
	NFSPath      string
	NFSSubDir    string
	NFSNoSub     bool
	NFSUID       int64
	Archive      bool
	ArchiveOnly  bool
	Timeout      time.Duration
	Nodes        []string
	NodeSelector string
	Cluster      string
	DPUCluster   string
}

// ValidateStartOptions validates the start options.
func ValidateStartOptions(opts *StartOptions) error {
	if opts.Output == OutputNFS && opts.NFSPath == "" {
		return fmt.Errorf("--nfs-path is required when using NFS output")
	}

	if opts.Cluster != "host" && opts.Cluster != "dpu" && opts.Cluster != "all" {
		return fmt.Errorf("invalid target value %q: must be 'host', 'dpu', or 'all'", opts.Cluster)
	}

	now := time.Now().Format("20060102-150405")
	if opts.CaseID == "" {
		opts.CaseID = fmt.Sprintf("dpf-%s", now)
	}

	if err := ValidateCaseID(opts.CaseID); err != nil {
		return err
	}

	if opts.Output == OutputNFS && !opts.NFSNoSub {
		opts.NFSSubDir = fmt.Sprintf("sosreport-%s", now)
	}

	if opts.ArchiveOnly {
		opts.Archive = true
	}

	return nil
}

// ValidateCaseID validates a user-provided case ID used in labels and paths.
func ValidateCaseID(caseID string) error {
	if caseID == "" {
		return nil
	}
	if strings.ContainsAny(caseID, "/\\") || strings.Contains(caseID, "..") {
		return fmt.Errorf("invalid case-id %q: must not contain path separators or '..'", caseID)
	}
	if errs := validation.IsValidLabelValue(caseID); len(errs) > 0 {
		return fmt.Errorf("invalid case-id %q: must be a valid Kubernetes label value: %s", caseID, strings.Join(errs, "; "))
	}
	return nil
}

// Start creates SOS report Jobs on the given targets.
// Returns the subset of targets where jobs were successfully created, and an
// error if all clusters fail. Callers should use the returned targets for
// subsequent wait/download/cleanup phases to avoid operating on clusters
// where no jobs exist.
func Start(ctx context.Context, targets ClusterTargets, hostClient client.Client, opts StartOptions) (ClusterTargets, error) {
	if opts.Output == OutputNFS && opts.NFSSubDir != "" {
		Step("Starting SOS report collection (case-id: %s, nfs: %s/%s)", opts.CaseID, opts.NFSPath, opts.NFSSubDir)
	} else {
		Step("Starting SOS report collection (case-id: %s)", opts.CaseID)
	}

	var started ClusterTargets
	for _, target := range targets {
		if err := startOnCluster(ctx, target, hostClient, opts); err != nil {
			Failure("cluster %s: %v", target.Name, err)
			continue
		}
		started = append(started, target)
	}

	if len(started) == 0 {
		return nil, fmt.Errorf("failed to start SOS reports on all %d cluster(s)", len(targets))
	}

	return started, nil
}

func startOnCluster(ctx context.Context, target ClusterTarget, hostClient client.Client, opts StartOptions) error {
	nodes, err := getTargetNodes(ctx, target, opts.Nodes, opts.NodeSelector)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil // node not found in this cluster, skip
	}

	kubeconfigData, err := GetKubeconfigData(ctx, target, hostClient)
	if err != nil {
		return fmt.Errorf("get kubeconfig data for %s: %w", target.Name, err)
	}

	if err := CreateKubeconfigSecret(ctx, target.Client, opts.Namespace, target.Name, opts.CaseID, kubeconfigData); err != nil {
		return fmt.Errorf("create kubeconfig secret for %s: %w", target.Name, err)
	}

	for _, nodeName := range nodes {
		jobOpts := JobOptions{
			Namespace:   opts.Namespace,
			NodeName:    nodeName,
			CaseID:      opts.CaseID,
			Image:       opts.Image,
			ClusterName: target.Name,
			Timeout:     opts.Timeout,
			Output:      opts.Output,
			NFSServer:   opts.NFSServer,
			NFSPath:     opts.NFSPath,
			NFSSubDir:   opts.NFSSubDir,
			NFSUID:      opts.NFSUID,
			Archive:     opts.Archive,
			ArchiveOnly: opts.ArchiveOnly,
		}

		job, err := CreateJob(ctx, target.Client, jobOpts)
		if err != nil {
			Failure("%s/%s: %v", target.Name, nodeName, err)
			continue
		}
		Success("%s/%s (job: %s)", target.Name, job.Spec.Template.Spec.NodeName, job.Name)
	}

	return nil
}

func getTargetNodes(ctx context.Context, target ClusterTarget, filterNodes []string, nodeSelector string) ([]string, error) {
	nodes, err := ListNodes(ctx, target.Client, nodeSelector)
	if err != nil {
		return nil, fmt.Errorf("list nodes on %s: %w", target.Name, err)
	}

	if len(filterNodes) > 0 {
		// Only target nodes that exist in this cluster.
		var matched []string
		for _, fn := range filterNodes {
			if slices.Contains(nodes, fn) {
				matched = append(matched, fn)
			}
		}
		if len(matched) == 0 {
			Warn("Cluster %s: none of the specified nodes %v were found", target.Name, filterNodes)
		}
		return matched, nil
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes found on cluster %s", target.Name)
	}

	Info("Cluster %s: %d node(s)", target.Name, len(nodes))
	return nodes, nil
}
