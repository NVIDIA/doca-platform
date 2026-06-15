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

package nodelabels

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionType        = "NodeLabelsReported"
	defaultScriptTimeout = 30 * time.Second
	nodeLabelPrefix      = "scripts.dpu.nvidia.com/"
)

type ReportNodeLabels struct {
	scriptsDir            string
	kubeletKubeconfigPath string
	newNodeClientFunc     func() (crclient.Client, error)
	scriptTimeout         time.Duration
}

func (r *ReportNodeLabels) Name() string {
	return "Report Node Labels"
}

func (r *ReportNodeLabels) ConditionType() string {
	return conditionType
}

func (r *ReportNodeLabels) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipNodeLabeling
}

func (r *ReportNodeLabels) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return true
}

func (r *ReportNodeLabels) Execute(execCtx context.Context, optCtx *operations.Context) error {
	labels, err := r.collectLabels(execCtx, optCtx)
	if err != nil {
		return err
	}
	if optCtx.Options.DPUName == "" {
		return fmt.Errorf("dpu name is required to update DPU cluster node labels")
	}
	nodeClient, err := r.newNodeClient()
	if err != nil {
		return err
	}
	if err := r.applyLabels(execCtx, nodeClient, optCtx.Options.DPUName, labels); err != nil {
		return err
	}
	klog.Infof("Reported %d node label(s) from DPU ARM scripts", len(labels))
	return nil
}

func (r *ReportNodeLabels) collectLabels(ctx context.Context, optCtx *operations.Context) (map[string]string, error) {
	scriptsDir := r.scriptsDir
	if scriptsDir == "" {
		scriptsDir = optCtx.Options.NodeLabelScriptsDir
	}

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		// If the directory does not exist, return an empty map.
		// No scripts to run, so no labels to report.
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to read node label scripts directory %s: %w", scriptsDir, err)
	}

	scripts := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to stat node label script %s: %w", filepath.Join(scriptsDir, entry.Name()), err)
		}
		// Skip non-regular files (directories, symlinks, etc.) and non-executable files.
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		scripts = append(scripts, entry.Name())
	}
	sort.Strings(scripts)

	labels := make(map[string]string, len(scripts))
	var errs []error
	for _, script := range scripts {
		key := nodeLabelPrefix + script

		if validationErrs := validation.IsQualifiedName(key); len(validationErrs) > 0 {
			errs = append(errs, fmt.Errorf("invalid node label script name %q: %s", script, strings.Join(validationErrs, "; ")))
			continue
		}

		value, err := r.runScript(ctx, filepath.Join(scriptsDir, script))
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to run node label script %q: %w", script, err))
			continue
		}
		if validationErrs := validation.IsValidLabelValue(value); len(validationErrs) > 0 {
			errs = append(errs, fmt.Errorf("invalid node label value from script %q: %s", script, strings.Join(validationErrs, "; ")))
			continue
		}
		labels[key] = value
	}

	return labels, kerrors.NewAggregate(errs)
}

func (r *ReportNodeLabels) applyLabels(ctx context.Context, nodeClient crclient.Client, nodeName string, labels map[string]string) error {
	if nodeName == "" {
		return fmt.Errorf("dpu name is required to update DPU cluster node labels")
	}

	node := &corev1.Node{}
	if err := nodeClient.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return fmt.Errorf("failed to get DPU cluster Node %s: %w", nodeName, err)
	}
	patchBase := node.DeepCopy()
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}

	for key := range node.Labels {
		if !strings.HasPrefix(key, nodeLabelPrefix) {
			continue
		}
		// Keep current script labels, they will get updated below.
		if _, ok := labels[key]; ok {
			continue
		}

		// Remove stale script labels.
		delete(node.Labels, key)
	}

	for key, value := range labels {
		node.Labels[key] = value
	}

	if err := nodeClient.Patch(ctx, node, crclient.MergeFrom(patchBase)); err != nil {
		return fmt.Errorf("failed to patch DPU cluster Node %s labels: %w", nodeName, err)
	}
	return nil
}

func (r *ReportNodeLabels) newNodeClient() (crclient.Client, error) {
	if r.newNodeClientFunc != nil {
		return r.newNodeClientFunc()
	}
	kubeconfigPath := r.kubeletKubeconfigPath
	if kubeconfigPath == "" {
		kubeconfigPath = operations.DefaultKubeletKubeconfigPath
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build DPU cluster client config from %s: %w", kubeconfigPath, err)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	nodeClient, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create DPU cluster client: %w", err)
	}
	return nodeClient, nil
}

func (r *ReportNodeLabels) runScript(ctx context.Context, path string) (string, error) {
	timeout := r.scriptTimeout
	if timeout == 0 {
		timeout = defaultScriptTimeout
	}
	scriptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout, stderr, err := bash.RunScriptWithContext(scriptCtx, path)
	if err != nil {
		if scriptCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("node label script %s timed out after %s", path, timeout)
		}
		return "", fmt.Errorf("failed to run node label script %s: stdout: %q, stderr: %q, err: %w", path, stdout.String(), stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
