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

package clusterhelper

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

var (
	scheme = runtime.NewScheme()
)

const (
	resyncPeriodMinutes = 30
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	utilruntime.Must(storagev1.AddToScheme(scheme))
}

type Helper interface {
	runner.Runnable
	// GetHostClusterClient returns kubernetes API client for the Host cluster
	GetHostClusterClient(ctx context.Context) (client.Client, error)
	// GetDPUClusterClient returns kubernetes API client for the DPU cluster
	GetDPUClusterClient(ctx context.Context) (client.Client, error)
}

func New(config config.Controller) Helper {
	return &clusterManager{
		config:  config,
		runner:  runner.New(),
		started: make(chan struct{}),
	}
}

type clusterManager struct {
	config            config.Controller
	dpuClusterClient  client.Client
	hostClusterClient client.Client
	started           chan struct{}
	runner            runner.Runner
}

// GetHostClusterClient returns kubernetes API client for the Host cluster
func (m *clusterManager) GetHostClusterClient(ctx context.Context) (client.Client, error) {
	if err := m.waitStarted(ctx); err != nil {
		return nil, err
	}
	return m.hostClusterClient, nil
}

// GetDPUClusterClient returns kubernetes API client for the DPU cluster
func (m *clusterManager) GetDPUClusterClient(ctx context.Context) (client.Client, error) {
	if err := m.waitStarted(ctx); err != nil {
		return nil, err
	}
	return m.dpuClusterClient, nil
}

// Run blocks until context is canceled or one of the dependant cluster.Cluster process exits
func (m *clusterManager) Run(ctx context.Context) error {
	dpuClusterConfig, err := m.getDPUClusterRestConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST configuration for DPU cluster: %v", err)
	}
	hostClusterConfig, err := m.getHostClusterRestConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST configuration for Host cluster: %v", err)
	}
	resyncPeriod := time.Minute * time.Duration(resyncPeriodMinutes)
	dpuCluster, err := cluster.New(dpuClusterConfig, func(o *cluster.Options) {
		o.Scheme = scheme
		o.Cache = cache.Options{
			SyncPeriod: &resyncPeriod,
			ByObject: map[client.Object]cache.ByObject{
				&storagev1.Volume{}:           {Namespaces: map[string]cache.Config{m.config.TargetNamespace: {}}},
				&storagev1.VolumeAttachment{}: {Namespaces: map[string]cache.Config{m.config.TargetNamespace: {}}}}}
	})
	if err != nil {
		return fmt.Errorf("failed to initialize client for the DPU cluster: %v", err)
	}
	hostCluster, err := cluster.New(hostClusterConfig, func(o *cluster.Options) {
		o.Scheme = scheme
		o.Cache = cache.Options{
			SyncPeriod: &resyncPeriod,
			ByObject: map[client.Object]cache.ByObject{
				&provisioningv1.DPU{}: {Namespaces: map[string]cache.Config{cache.AllNamespaces: {}}}}}
	})
	if err != nil {
		return fmt.Errorf("failed to initialize client for the Host cluster: %v", err)
	}

	if err := hostCluster.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPU{}, "spec.dpuNodeName", func(o client.Object) []string {
		d, ok := o.(*provisioningv1.DPU)
		if !ok {
			return nil
		}
		return []string{d.Spec.DPUNodeName}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPU CR: %v", err)
	}

	if err := dpuCluster.GetFieldIndexer().IndexField(ctx, &storagev1.Volume{}, "metadata.uid", func(o client.Object) []string {
		v, ok := o.(*storagev1.Volume)
		if !ok {
			return nil
		}
		return []string{string(v.GetUID())}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for Volume CR: %v", err)
	}

	m.dpuClusterClient = dpuCluster.GetClient()
	m.hostClusterClient = hostCluster.GetClient()

	m.runner.AddService("dpuCluster", newClusterWrapper(dpuCluster))
	m.runner.AddService("hostCluster", newClusterWrapper(hostCluster))

	close(m.started)

	return m.runner.Run(ctx)
}

// returns rest.Config that points to the DPU cluster
func (m *clusterManager) getDPUClusterRestConfig() (*rest.Config, error) {
	token, err := os.ReadFile(m.config.DPUClusterTokenFile)
	if err != nil {
		return nil, err
	}
	tlsClientConfig := rest.TLSClientConfig{}
	if _, err := certutil.NewPool(m.config.DPUClusterCAFile); err != nil {
		klog.ErrorS(err, "can't load root CA config to the certpool", "CA", m.config.DPUClusterCAFile)
	} else {
		tlsClientConfig.CAFile = m.config.DPUClusterCAFile
	}
	return &rest.Config{
		Host:            "https://" + net.JoinHostPort(m.config.DPUClusterAPIHost, m.config.DPUClusterAPIPort),
		TLSClientConfig: tlsClientConfig,
		BearerToken:     string(token),
		BearerTokenFile: m.config.DPUClusterTokenFile,
	}, nil
}

// returns rest.Config that points to the Host cluster
func (m *clusterManager) getHostClusterRestConfig() (*rest.Config, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get config for host cluster: %v", err)
	}
	return config, nil
}

// Wait blocks until context is canceled or service is ready
func (m *clusterManager) Wait(ctx context.Context) error {
	if err := m.waitStarted(ctx); err != nil {
		return err
	}
	if err := m.runner.Wait(ctx); err != nil {
		return err
	}
	return nil
}

// waitStarted blocks until the started chan is close or ctx is canceled
func (m *clusterManager) waitStarted(ctx context.Context) error {
	select {
	case <-m.started:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
