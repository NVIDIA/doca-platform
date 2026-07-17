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

package client

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// RunDPUWatch triggers onReconcile once immediately, then whenever a watch event may
// affect the owning DPU (matched by name in namespace).
func RunDPUWatch(ctx context.Context, c crclient.WithWatch, namespace, dpuName string, onReconcile func()) error {
watchLoop:
	for {
		// Reconcile once before each watch session so changes that happened
		// between watch reconnects are observed.
		onReconcile()
		w, err := c.Watch(
			ctx,
			&provisioningv1.DPUList{},
			crclient.InNamespace(namespace),
			crclient.MatchingFields{"metadata.name": dpuName},
		)
		if err != nil {
			return fmt.Errorf("watch DPUs in namespace %s: %w", namespace, err)
		}
		for {
			select {
			case <-ctx.Done():
				w.Stop()
				return ctx.Err()
			case event, ok := <-w.ResultChan():
				if !ok {
					// Watch streams can close during normal API-server watch expiration.
					// Re-open and continue watching instead of exiting.
					w.Stop()
					continue watchLoop
				}
				_, ok = event.Object.(*provisioningv1.DPU)
				if !ok {
					continue
				}
				onReconcile()
			}
		}
	}
}
