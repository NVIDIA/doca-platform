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

// Package rshimconsole provides E2E helpers for deploying the rshim console
// collector and exporting its logs.
package rshimconsole

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nvidia/doca-platform/test/e2e/cleanup"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AppLabelKey identifies rshim console collector resources.
	AppLabelKey = "app.kubernetes.io/name"
	// AppLabelValue is the label value used by rshim console collector resources.
	AppLabelValue = "rshim-console-collector"

	collectorContainerName = "rshim-console-collector"
	daemonSetReadyTimeout  = 5 * time.Minute
)

// Deploy creates or refreshes the collector DaemonSet with the image and pull
// secrets produced by the E2E release.
func Deploy(
	ctx context.Context,
	kubeClient client.Client,
	manifest *appsv1.DaemonSet,
	image string,
	pullSecretNames []string,
	cleanupLabels map[string]string,
) error {
	if manifest == nil {
		return errors.New("collector DaemonSet manifest must not be nil")
	}
	if image == "" {
		return errors.New("collector image must not be empty")
	}

	desired := manifest.DeepCopy()
	desired.Labels = cleanup.MergeMaps(desired.Labels, cleanupLabels)
	desired.Spec.Template.Labels = cleanup.MergeMaps(desired.Spec.Template.Labels, cleanupLabels)

	container := findCollectorContainer(desired)
	if container == nil {
		return fmt.Errorf("collector DaemonSet must contain container %q", collectorContainerName)
	}
	container.Image = image

	desired.Spec.Template.Spec.ImagePullSecrets = make([]corev1.LocalObjectReference, 0, len(pullSecretNames))
	for _, name := range pullSecretNames {
		desired.Spec.Template.Spec.ImagePullSecrets = append(
			desired.Spec.Template.Spec.ImagePullSecrets,
			corev1.LocalObjectReference{Name: name},
		)
	}

	if err := kubeClient.Create(ctx, desired); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create collector DaemonSet: %w", err)
		}

		existing := &appsv1.DaemonSet{}
		key := client.ObjectKeyFromObject(desired)
		if err := kubeClient.Get(ctx, key, existing); err != nil {
			return fmt.Errorf("get existing collector DaemonSet: %w", err)
		}
		existing.Labels = desired.Labels
		existing.Spec = desired.Spec
		if err := kubeClient.Update(ctx, existing); err != nil {
			return fmt.Errorf("update collector DaemonSet: %w", err)
		}
		desired = existing
	}

	if err := waitReady(ctx, kubeClient, client.ObjectKeyFromObject(desired)); err != nil {
		return err
	}
	return nil
}

func findCollectorContainer(ds *appsv1.DaemonSet) *corev1.Container {
	for i := range ds.Spec.Template.Spec.Containers {
		if ds.Spec.Template.Spec.Containers[i].Name == collectorContainerName {
			return &ds.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

func waitReady(ctx context.Context, kubeClient client.Client, key types.NamespacedName) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, daemonSetReadyTimeout, true, func(ctx context.Context) (bool, error) {
		current := &appsv1.DaemonSet{}
		if err := kubeClient.Get(ctx, key, current); err != nil {
			return false, nil
		}
		return current.Status.ObservedGeneration == current.Generation &&
			current.Status.DesiredNumberScheduled > 0 &&
			current.Status.NumberReady == current.Status.DesiredNumberScheduled, nil
	})
	if err != nil {
		return fmt.Errorf("wait for collector DaemonSet %s to become ready: %w", key, err)
	}
	return nil
}
