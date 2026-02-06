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

package controllers

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetClientOptions returns the controller-runtime client options used by this
// controller.
//
// Keeping controller-runtime option in this package helps ensure
// production code and controller tests use the same client/cache behavior.
func GetClientOptions() client.Options {
	clientOptions := client.Options{
		Cache: &client.CacheOptions{
			DisableFor: []client.Object{&corev1.Secret{}},
		},
	}

	return clientOptions
}

// GetCacheOptions returns the controller-runtime cache options used by this
// controller.
//
// Keeping controller-runtime option in this package helps ensure
// production code and controller tests use the same client/cache behavior.
func GetCacheOptions(namespace string, cacheSyncPeriod time.Duration, ownerConfigMapName string) cache.Options {
	cacheOptions := cache.Options{
		SyncPeriod: &cacheSyncPeriod,
		// Watch namespaced objects only in the specified namespace.
		DefaultNamespaces: map[string]cache.Config{
			namespace: {},
		},
		// Watch only pods managed by this controller.
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				Label: labels.SelectorFromSet(labels.Set{
					ManagedByLabelKey: ManagedByLabelValue,
				}),
			},
		},
	}
	// If an owner ConfigMap name is specified, watch only that ConfigMap.
	if ownerConfigMapName != "" {
		cacheOptions.ByObject[&corev1.ConfigMap{}] = cache.ByObject{
			Field: fields.SelectorFromSet(fields.Set{
				"metadata.name": ownerConfigMapName,
			}),
		}
	}
	return cacheOptions
}
