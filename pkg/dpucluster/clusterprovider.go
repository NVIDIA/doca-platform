/*
Copyright 2025 NVIDIA

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

package dpucluster

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterClientProvider is the interface that is used by the DPUClusterHelper to get clients for DPUClusters.
type ClusterClientProvider interface {
	// GetClient returns a client for the specified DPUCluster
	GetClient(clusterKey client.ObjectKey) (client.Client, error)
	// ListClients returns a list of clients for all DPUClusters known
	// to the provider.
	ListClients() ([]client.Client, error)
}

// check that RemoteCache struct is aligned with the ClusterClientProvider interface
var _ ClusterClientProvider = &RemoteCache{}

// NewStaticClusterClientProvider creates a new ClusterClientProvider that returns statically configured clients
func NewStaticClusterClientProvider(clients map[client.ObjectKey]client.Client) ClusterClientProvider {
	return &staticClusterClientProvider{
		clients: clients,
	}
}

type staticClusterClientProvider struct {
	clients map[client.ObjectKey]client.Client
}

// GetClient returns a client for the specified DPUCluster.
// If a client is not found for a cluster key, ErrDPUClusterNotConnected is returned.
func (p *staticClusterClientProvider) GetClient(clusterKey client.ObjectKey) (client.Client, error) {
	client, ok := p.clients[clusterKey]
	if !ok {
		return nil, ErrDPUClusterNotConnected
	}
	return client, nil
}

// ListClients returns a list of clients for all DPUClusters known to the provider.
func (p *staticClusterClientProvider) ListClients() ([]client.Client, error) {
	clients := make([]client.Client, 0, len(p.clients))
	for _, client := range p.clients {
		clients = append(clients, client)
	}
	return clients, nil
}
