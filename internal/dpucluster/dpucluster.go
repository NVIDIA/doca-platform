/*
Copyright 2024 NVIDIA

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

package controlplane

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config hold the cluster configuration.
// It provides methods to get the client, clientset, and rest config for the cluster.
type Config struct {
	// Cluster is the DPU cluster for which the config is being fetched.
	Cluster *provisioningv1.DPUCluster
	// hostClient is the client for the host cluster. It is used to fetch the kubeconfig secret.
	hostClient      client.Client
	kubeconfig      *clientcmdv1.Config
	kubeconfigBytes []byte
	clientConfig    clientcmd.OverridingClientConfig
}

// NewConfig returns a new Config.
func NewConfig(c client.Client, cluster *provisioningv1.DPUCluster) *Config {
	return &Config{
		Cluster:    cluster,
		hostClient: c,
	}
}

func (cc *Config) ClusterNamespaceName() string {
	return fmt.Sprintf("%s-%s", cc.Cluster.Namespace, cc.Cluster.Name)
}

// Kubeconfig returns the kubeconfig for the cluster.
func (cc *Config) Kubeconfig(ctx context.Context) (*clientcmdv1.Config, error) {
	if cc.kubeconfig != nil {
		return cc.kubeconfig, nil
	}
	if cc.clientConfig == nil {
		if err := cc.getClientConfig(ctx); err != nil {
			return nil, err
		}
	}

	adminConfig, err := cc.clientConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("error while getting raw config: %w", err)
	}

	if len(adminConfig.AuthInfos) != 1 {
		return nil, fmt.Errorf("secret not in the expected format: auth info list should have one member")
	}
	if len(adminConfig.Clusters) != 1 {
		return nil, fmt.Errorf("secret not in the expected format: cluster list should have one member")
	}

	cc.kubeconfig = &adminConfig
	return cc.kubeconfig, nil
}

func (cc *Config) getClientConfig(ctx context.Context) error {
	// Get the control plane secret using the cluster's `spec.kubeconfig` field.
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: cc.Cluster.Namespace, Name: cc.Cluster.Spec.Kubeconfig}
	if err := cc.hostClient.Get(ctx, key, secret); err != nil {
		return err
	}
	kubeconfigBytes, ok := secret.Data["admin.conf"]
	if !ok {
		return fmt.Errorf("secret %v/%v not in the expected format: data.admin.conf not found", secret.Namespace, secret.Name)
	}
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("error while getting client config: %w", err)
	}
	cc.clientConfig = clientConfig
	cc.kubeconfigBytes = kubeconfigBytes
	return nil
}

// Client returns a new client for the cluster.
func (cc *Config) Client(ctx context.Context) (client.Client, error) {
	_, err := cc.Kubeconfig(ctx)
	if err != nil {
		return nil, err
	}

	restConfig, err := cc.clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	newClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, err
	}
	return newClient, nil
}

// Clientset returns a new clientset for the cluster.
func (cc *Config) Clientset(ctx context.Context) (*kubernetes.Clientset, []byte, error) {
	_, err := cc.Kubeconfig(ctx)
	if err != nil {
		return nil, nil, err
	}

	restConfig, err := cc.clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	newClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}
	return newClient, cc.kubeconfigBytes, nil
}

// GetConfigs returns a list of Configs for all DPU clusters.
func GetConfigs(ctx context.Context, c client.Client) ([]*Config, error) {
	dpuClusters := &provisioningv1.DPUClusterList{}
	err := c.List(ctx, dpuClusters)
	if err != nil {
		return nil, err
	}

	configs := make([]*Config, 0, len(dpuClusters.Items))
	for _, cluster := range dpuClusters.Items {
		config := NewConfig(c, &cluster)
		configs = append(configs, config)
	}
	return configs, nil
}
