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
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
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
	utilruntime.Must(storagev1.AddToScheme(scheme))
}

type Helper interface {
	runner.Runnable
	// GetClient returns kubernetes API client for the Host cluster
	GetClient(ctx context.Context) (client.Client, error)
}

func New(clientConfig *rest.Config, controllerConfig config.Controller) Helper {
	return &clusterManager{
		clientConfig:     clientConfig,
		controllerConfig: controllerConfig,
		runner:           runner.New(),
		started:          make(chan struct{}),
	}
}

type clusterManager struct {
	clientConfig     *rest.Config
	controllerConfig config.Controller
	client           client.Client
	started          chan struct{}
	runner           runner.Runner
}

// GetClient returns kubernetes API client for the host cluster
func (m *clusterManager) GetClient(ctx context.Context) (client.Client, error) {
	if err := m.waitStarted(ctx); err != nil {
		return nil, err
	}
	return m.client, nil
}

// Run blocks until context is canceled or one of the dependant cluster.Cluster process exits
func (m *clusterManager) Run(ctx context.Context) error {
	resyncPeriod := time.Minute * time.Duration(resyncPeriodMinutes)
	hostCluster, err := cluster.New(m.clientConfig, func(o *cluster.Options) {
		o.Scheme = scheme
		o.Cache = cache.Options{
			SyncPeriod: &resyncPeriod,
			ByObject: map[client.Object]cache.ByObject{
				&storagev1.DPUVolume{}:           {Namespaces: map[string]cache.Config{m.controllerConfig.Namespace: {}}},
				&storagev1.DPUVolumeAttachment{}: {Namespaces: map[string]cache.Config{m.controllerConfig.Namespace: {}}}}}
	})
	if err != nil {
		return fmt.Errorf("failed to initialize client for the Host cluster: %v", err)
	}

	m.client = hostCluster.GetClient()
	m.runner.AddService("hostCluster", newClusterWrapper(hostCluster))
	close(m.started)

	return m.runner.Run(ctx)
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
