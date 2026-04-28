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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils/tunnel"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	hostScheme = func() *runtime.Scheme {
		s := runtime.NewScheme()
		_ = provisioningv1.AddToScheme(s)
		_ = corev1.AddToScheme(s)
		_ = batchv1.AddToScheme(s)
		return s
	}()
	dpuScheme = func() *runtime.Scheme {
		s := runtime.NewScheme()
		_ = corev1.AddToScheme(s)
		_ = batchv1.AddToScheme(s)
		return s
	}()
)

// ClusterTarget represents a cluster to run SOS reports on.
type ClusterTarget struct {
	Name       string
	Client     client.Client
	RestConfig *rest.Config

	// Fields for tunnel reconnection (DPU clusters only).
	tunnel         *tunnel.Tunnel
	hostClient     client.Client
	hostRESTConfig *rest.Config
	dpuCluster     *provisioningv1.DPUCluster
}

// Close cleans up tunnel resources.
func (ct *ClusterTarget) Close() {
	if ct.tunnel != nil {
		ct.tunnel.Close()
	}
}

type ClusterTargets []ClusterTarget

func (t ClusterTargets) Close() {
	for i := range t {
		t[i].Close()
	}
}

// EnsureTunnel checks if the tunnel is healthy and re-establishes it if needed.
// For host clusters (no tunnel), this is a no-op.
// Before reconnecting, it verifies that the DPUCluster is still in Ready phase
// to avoid pointless reconnection attempts against an unreachable cluster.
func (ct *ClusterTarget) EnsureTunnel(ctx context.Context) error {
	if ct.tunnel == nil {
		return nil
	}

	if ct.tunnel.IsHealthy() {
		return nil
	}

	// Re-fetch the DPUCluster to check if it's still ready before reconnecting.
	if ct.hostClient != nil && ct.dpuCluster != nil {
		fresh := &provisioningv1.DPUCluster{}
		if err := ct.hostClient.Get(ctx, client.ObjectKeyFromObject(ct.dpuCluster), fresh); err != nil {
			return fmt.Errorf("check DPUCluster %s readiness: %w", ct.Name, err)
		}
		if fresh.Status.Phase != provisioningv1.PhaseReady {
			return fmt.Errorf("DPUCluster %s is not ready (phase: %s), skipping tunnel reconnect", ct.Name, fresh.Status.Phase)
		}
	}

	Warn("Tunnel to %s is unhealthy, reconnecting...", ct.Name)
	ct.tunnel.Close()

	restConfig, tun, err := tunnel.NewTunneledRestConfig(ctx, ct.hostClient, ct.hostRESTConfig, ct.dpuCluster)
	if err != nil {
		return fmt.Errorf("re-establish tunnel to %s: %w", ct.Name, err)
	}

	dpuClient, err := client.New(restConfig, client.Options{Scheme: dpuScheme})
	if err != nil {
		tun.Close()
		return fmt.Errorf("create client after tunnel reconnect: %w", err)
	}

	ct.tunnel = tun
	ct.RestConfig = restConfig
	ct.Client = dpuClient
	return nil
}

const (
	// clusterFilterHost targets only the host cluster.
	clusterFilterHost = "host"
	// clusterFilterDPU targets only DPU clusters.
	clusterFilterDPU = "dpu"
	// clusterFilterAll targets both host and DPU clusters.
	clusterFilterAll = "all"
)

// GetClusterTargets resolves a cluster filter into a list of cluster targets.
func GetClusterTargets(ctx context.Context, clusterFilter, dpuClusterName string) (ClusterTargets, error) {
	hostRESTConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get host kubeconfig: %w", err)
	}

	hostClient, err := client.New(hostRESTConfig, client.Options{Scheme: hostScheme})
	if err != nil {
		return nil, fmt.Errorf("create host client: %w", err)
	}

	var targets ClusterTargets

	if clusterFilter == clusterFilterHost || clusterFilter == clusterFilterAll {
		targets = append(targets, ClusterTarget{
			Name:       clusterFilterHost,
			Client:     hostClient,
			RestConfig: hostRESTConfig,
		})
	}

	if clusterFilter == clusterFilterDPU || clusterFilter == clusterFilterAll {
		dpuTargets, err := getDPUClusterTargets(ctx, hostClient, hostRESTConfig, dpuClusterName)
		if err != nil {
			if clusterFilter == clusterFilterDPU {
				return nil, err
			}
			Warn("failed to get DPU cluster targets: %v", err)
		}
		targets = append(targets, dpuTargets...)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no cluster targets found")
	}

	return targets, nil
}

// GetHostClient returns the host client from the targets, or creates one if not present.
func GetHostClient(targets ClusterTargets) (client.Client, error) {
	for _, t := range targets {
		if t.Name == clusterFilterHost {
			return t.Client, nil
		}
	}
	hostRESTConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get host kubeconfig: %w", err)
	}
	c, err := client.New(hostRESTConfig, client.Options{Scheme: hostScheme})
	if err != nil {
		return nil, fmt.Errorf("create host client: %w", err)
	}
	return c, nil
}

// GetKubeconfigData returns kubeconfig data for the given cluster target.
func GetKubeconfigData(ctx context.Context, target ClusterTarget, hostClient client.Client) ([]byte, error) {
	if target.Name == clusterFilterHost {
		return CreateKubeconfigDataFromConfig(target.RestConfig)
	}

	if target.dpuCluster == nil {
		return nil, fmt.Errorf("DPU cluster target %s has no DPUCluster reference", target.Name)
	}

	secret := &corev1.Secret{}
	if err := hostClient.Get(ctx, client.ObjectKey{Name: target.dpuCluster.Spec.Kubeconfig, Namespace: target.dpuCluster.Namespace}, secret); err != nil {
		return nil, fmt.Errorf("get kubeconfig secret for DPUCluster %s/%s: %w", target.dpuCluster.Namespace, target.dpuCluster.Name, err)
	}
	if data, ok := secret.Data["super-admin.conf"]; ok {
		return data, nil
	}

	return nil, fmt.Errorf("kubeconfig secret %s/%s does not contain super-admin.conf", target.dpuCluster.Namespace, target.dpuCluster.Spec.Kubeconfig)
}

func getDPUClusterTargets(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuClusterName string) (ClusterTargets, error) {
	dpuClusters := &provisioningv1.DPUClusterList{}
	if err := hostClient.List(ctx, dpuClusters); err != nil {
		return nil, fmt.Errorf("list DPUClusters: %w", err)
	}

	if len(dpuClusters.Items) == 0 {
		return nil, fmt.Errorf("no DPUClusters found")
	}

	targets := make(ClusterTargets, 0, len(dpuClusters.Items))
	for i := range dpuClusters.Items {
		dc := &dpuClusters.Items[i]

		if dpuClusterName != "" && dc.Name != dpuClusterName {
			continue
		}

		if dc.Status.Phase != provisioningv1.PhaseReady {
			Warn("DPUCluster %s/%s is not ready (phase: %s), skipping", dc.Namespace, dc.Name, dc.Status.Phase)
			continue
		}

		restConfig, tun, err := tunnel.NewTunneledRestConfig(ctx, hostClient, hostRESTConfig, dc)
		if err != nil {
			Warn("failed to tunnel to DPUCluster %s/%s: %v", dc.Namespace, dc.Name, err)
			continue
		}

		dpuClient, err := client.New(restConfig, client.Options{Scheme: dpuScheme})
		if err != nil {
			tun.Close()
			Warn("failed to create client for DPUCluster %s/%s: %v", dc.Namespace, dc.Name, err)
			continue
		}

		clusterName := fmt.Sprintf("dpu-%s", dc.Name)
		targets = append(targets, ClusterTarget{
			Name:           clusterName,
			Client:         dpuClient,
			RestConfig:     restConfig,
			tunnel:         tun,
			hostClient:     hostClient,
			hostRESTConfig: hostRESTConfig,
			dpuCluster:     dc,
		})
	}

	return targets, nil
}
