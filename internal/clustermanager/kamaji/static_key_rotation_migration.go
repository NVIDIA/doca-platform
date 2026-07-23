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

package nvidia

import (
	"context"
	"fmt"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"

	storagemigrationv1 "k8s.io/api/storagemigration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const staticKeyStorageMigrationTimeout = 10 * time.Second

// reconcileStaticKeyStorageMigration ensures all encrypted resource types are migrated for the given key transition.
func (cm *clusterHandler) reconcileStaticKeyStorageMigration(ctx context.Context, dc *provisioningv1.DPUCluster, rotationID string) (bool, bool, string, error) {
	logger := log.FromContext(ctx).WithValues(
		"dpuCluster", client.ObjectKeyFromObject(dc),
		"rotationID", rotationID)
	ctx = log.IntoContext(ctx, logger)
	if dc.Spec.Kubeconfig == "" {
		logger.V(2).Info("waiting to start staticKey storage migration", "reason", "DPUClusterKubeconfigUnavailable")
		return false, false, "waiting for DPUCluster kubeconfig before creating StorageVersionMigration objects", nil
	}
	if cm.tenantClient == nil {
		return false, false, "", fmt.Errorf("tenant client provider is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, staticKeyStorageMigrationTimeout)
	defer cancel()
	tenantClient, err := cm.tenantClient.Client(ctx, dc)
	if err != nil {
		return false, false, "", fmt.Errorf("create tenant client: %w", err)
	}
	logger.V(2).Info("reconciling staticKey storage migration")

	var blockedMessages, pendingMessages []string
	resources := encryptionconfig.EncryptedResources()
	for _, resource := range resources {
		done, blocked, message, err := reconcileStorageVersionMigration(ctx, tenantClient, storageVersionMigrationName(resource, rotationID), resource)
		if err != nil {
			return false, false, "", err
		}
		if blocked {
			blockedMessages = append(blockedMessages, message)
			continue
		}
		if !done {
			pendingMessages = append(pendingMessages, message)
		}
	}
	if len(blockedMessages) > 0 {
		return false, true, strings.Join(blockedMessages, "; "), nil
	}
	if len(pendingMessages) > 0 {
		return false, false, strings.Join(pendingMessages, "; "), nil
	}
	cleanupStaticKeyStorageMigrations(ctx, tenantClient, rotationID)
	logger.V(2).Info("completed staticKey storage migration and requested StorageVersionMigration cleanup")
	return true, false, fmt.Sprintf("StorageVersionMigration completed for %s", strings.Join(resources, ", ")), nil
}

// storageVersionMigrationName returns a deterministic migration name for one encrypted resource and key transition.
func storageVersionMigrationName(resource, rotationID string) string {
	return fmt.Sprintf("dpf-ear-%s-%s", resource, rotationID)
}

// reconcileStorageVersionMigration creates or observes one StorageVersionMigration object.
func reconcileStorageVersionMigration(ctx context.Context, c client.Client, name, resource string) (bool, bool, string, error) {
	logger := log.FromContext(ctx).WithValues(
		"storageVersionMigration", name,
		"resource", resource)
	svm := &storagemigrationv1.StorageVersionMigration{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, svm); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, false, "", fmt.Errorf("get StorageVersionMigration %s: %w", name, err)
		}
		svm = storageVersionMigrationObject(name, resource)
		if err := c.Create(ctx, svm); err != nil {
			return false, false, "", fmt.Errorf("create StorageVersionMigration %s: %w", name, err)
		}
		logger.V(2).Info("created StorageVersionMigration")
		return false, false, fmt.Sprintf("created StorageVersionMigration %s for %s", name, resource), nil
	}

	if !svm.DeletionTimestamp.IsZero() {
		logger.V(2).Info("waiting for StorageVersionMigration deletion before re-creating it")
		return false, false, fmt.Sprintf("waiting for StorageVersionMigration %s deletion before re-creating it", name), nil
	}

	for _, condition := range svm.Status.Conditions {
		switch {
		case condition.Type == string(storagemigrationv1.MigrationSucceeded) && condition.Status == metav1.ConditionTrue:
			logger.V(2).Info("StorageVersionMigration succeeded")
			return true, false, fmt.Sprintf("StorageVersionMigration %s succeeded", name), nil
		case condition.Type == string(storagemigrationv1.MigrationFailed) && condition.Status == metav1.ConditionTrue:
			message := condition.Message
			if message == "" {
				message = condition.Reason
			}
			logger.V(2).Info("StorageVersionMigration failed", "message", message)
			return false, true, fmt.Sprintf("StorageVersionMigration %s failed: %s", name, message), nil
		}
	}
	logger.V(2).Info("waiting for StorageVersionMigration to complete")
	return false, false, fmt.Sprintf("waiting for StorageVersionMigration %s to complete", name), nil
}

// cleanupStaticKeyStorageMigrations best-effort deletes completed migration objects.
func cleanupStaticKeyStorageMigrations(ctx context.Context, c client.Client, rotationID string) {
	logger := log.FromContext(ctx)
	for _, resource := range encryptionconfig.EncryptedResources() {
		name := storageVersionMigrationName(resource, rotationID)
		svm := &storagemigrationv1.StorageVersionMigration{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		}
		if err := client.IgnoreNotFound(c.Delete(ctx, svm)); err != nil {
			logger.Error(err, "failed to delete completed StorageVersionMigration", "storageVersionMigration", name)
			continue
		}
		logger.V(2).Info("deleted completed StorageVersionMigration", "storageVersionMigration", name)
	}
}

// storageVersionMigrationObject builds a StorageVersionMigration object for a core API resource.
func storageVersionMigrationObject(name, resource string) *storagemigrationv1.StorageVersionMigration {
	return &storagemigrationv1.StorageVersionMigration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: storagemigrationv1.SchemeGroupVersion.String(),
			Kind:       "StorageVersionMigration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: storagemigrationv1.StorageVersionMigrationSpec{
			Resource: metav1.GroupResource{
				Group:    "",
				Resource: resource,
			},
		},
	}
}
