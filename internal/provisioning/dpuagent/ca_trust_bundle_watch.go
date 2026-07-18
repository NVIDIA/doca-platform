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

package dpuagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/containerd"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultCATrustBundlePollInterval = 30 * time.Second
	dpuAgentCABundlePath             = "/usr/local/share/ca-certificates/dpf-ca.crt"
)

func (d *DPUAgent) startCATrustBundleWatcher(ctx context.Context) {
	go func() {
		klog.Info("Starting CA trust bundle watcher")
		d.reconcileCATrustBundle(ctx)

		ticker := time.NewTicker(defaultCATrustBundlePollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				klog.Info("Stopping CA trust bundle watcher")
				return
			case <-ticker.C:
				d.reconcileCATrustBundle(ctx)
			}
		}
	}()
}

func (d *DPUAgent) reconcileCATrustBundle(ctx context.Context) {
	ns := d.optCtx.Options.DPUNamespace
	cm := &corev1.ConfigMap{}
	if err := d.optCtx.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm); err != nil {
		klog.Warningf("CA trust bundle watcher: failed to get ConfigMap %s/%s: %v", ns, operatorv1.DefaultCATrustBundleConfigMapName, err)
		return
	}

	bundlePEM := cm.Data[operatorv1.CATrustBundleKey]
	if strings.TrimSpace(bundlePEM) == "" {
		klog.Warningf("CA trust bundle watcher: ConfigMap %s/%s missing %q", ns, operatorv1.DefaultCATrustBundleConfigMapName, operatorv1.CATrustBundleKey)
		return
	}
	bundleHash := cm.Data[operatorv1.CATrustBundleHashKey]
	if strings.TrimSpace(bundleHash) == "" {
		klog.Warningf("CA trust bundle watcher: ConfigMap %s/%s missing %q", ns, operatorv1.DefaultCATrustBundleConfigMapName, operatorv1.CATrustBundleHashKey)
		return
	}

	observed := d.currentTrustBundleHash(ctx)
	if observed == bundleHash {
		klog.V(2).Infof("CA trust bundle watcher: bundle hash already applied, skip (hash=%s)", bundleHash)
		return
	}
	klog.Infof("CA trust bundle watcher: detected bundle hash drift (observed=%q desired=%q), applying trust bundle update", observed, bundleHash)

	if err := applyTrustBundleToDPU(bundlePEM); err != nil {
		klog.Errorf("CA trust bundle watcher: failed to apply bundle to DPU OS: %v", err)
		return
	}

	klog.Infof("CA trust bundle watcher: evaluating containerd registry CA update for hash %s", bundleHash)
	containerdUpdated, err := d.updateContainerdRegistryCA(bundleHash)
	if err != nil {
		klog.Errorf("CA trust bundle watcher: failed to update containerd CA file for hash %s: %v", bundleHash, err)
		return
	}
	if containerdUpdated {
		klog.Infof("CA trust bundle watcher: containerd registry CA update completed for hash %s", bundleHash)
	}

	now := metav1.Now()
	d.optCtx.Status.TrustBundleHash = &bundleHash
	d.optCtx.Status.TrustBundleLastUpdateTime = &now
	if err := d.updateStatusUntilSuccess(ctx); err != nil {
		klog.Errorf("CA trust bundle watcher: failed to update applied hash %s to DPU status: %v", bundleHash, err)
		return
	}
	klog.Infof("CA trust bundle watcher: applied bundle hash %s", bundleHash)
}

func (d *DPUAgent) currentTrustBundleHash(ctx context.Context) string {
	if d.optCtx.Status.TrustBundleHash != nil {
		return *d.optCtx.Status.TrustBundleHash
	}

	latestDPU := &provisioningv1.DPU{}
	key := client.ObjectKey{Namespace: d.optCtx.Options.DPUNamespace, Name: d.optCtx.Options.DPUName}
	if err := d.optCtx.Client.Get(ctx, key, latestDPU); err != nil {
		klog.Warningf("CA trust bundle watcher: failed to get latest DPU for observed hash: %v", err)
		return ""
	}
	if latestDPU.Status.AgentStatus != nil && latestDPU.Status.AgentStatus.TrustBundleHash != nil {
		d.optCtx.Status.TrustBundleHash = latestDPU.Status.AgentStatus.TrustBundleHash
		return *latestDPU.Status.AgentStatus.TrustBundleHash
	}
	return ""
}

func applyTrustBundleToDPU(bundlePEM string) error {
	if err := os.MkdirAll(filepath.Dir(dpuAgentCABundlePath), 0755); err != nil {
		return fmt.Errorf("create trust bundle directory: %w", err)
	}
	if err := os.WriteFile(dpuAgentCABundlePath, []byte(bundlePEM), 0644); err != nil {
		return fmt.Errorf("write trust bundle file %s: %w", dpuAgentCABundlePath, err)
	}
	klog.Infof("CA trust bundle watcher: wrote trust bundle file %s", dpuAgentCABundlePath)
	stdout, stderr, err := bash.Run("update-ca-certificates")
	if err != nil {
		return fmt.Errorf("run update-ca-certificates: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

func (d *DPUAgent) updateContainerdRegistryCA(generation string) (bool, error) {
	if d.optCtx.Options.SkipContainerdConfigration {
		klog.Infof("CA trust bundle watcher: skip containerd update for generation %s (--skip-containerd-config)", generation)
		return false, nil
	}
	endpoint := strings.TrimSpace(d.optCtx.DPUFlavor.Spec.ContainerdConfig.RegistryEndpoint)
	if endpoint == "" {
		klog.Infof("CA trust bundle watcher: skip containerd update for generation %s (no registry endpoint)", generation)
		return false, nil
	}

	configurer := &containerd.ConfigureContainerd{}
	if err := configurer.ConfigureRegistryMirrorCAFile(endpoint, dpuAgentCABundlePath); err != nil {
		return false, err
	}
	return true, nil
}
