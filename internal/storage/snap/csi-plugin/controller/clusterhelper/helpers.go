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

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/runner"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

func newClusterWrapper(c cluster.Cluster) runner.Runnable {
	return &clusterWrapper{c: c, started: make(chan struct{})}
}

// wraps cluster from controller-runtime package to the runner.Runnable interface
type clusterWrapper struct {
	c       cluster.Cluster
	started chan struct{}
}

// Run is the runner.Runnable implementation for the wrapped cluster.Cluster
func (cw *clusterWrapper) Run(ctx context.Context) error {
	close(cw.started)
	err := cw.c.Start(ctx)
	if err != nil {
		return err
	}
	return nil
}

// Wait is the runner.Runnable implementation for the wrapped cluster.Cluster
func (cw *clusterWrapper) Wait(ctx context.Context) error {
	if err := cw.waitStarted(ctx); err != nil {
		return err
	}
	if !cw.c.GetCache().WaitForCacheSync(ctx) {
		return fmt.Errorf("failed to sync cache")
	}
	return nil
}

// waitStarted blocks until the started chan is close or ctx is canceled
func (cw *clusterWrapper) waitStarted(ctx context.Context) error {
	select {
	case <-cw.started:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
