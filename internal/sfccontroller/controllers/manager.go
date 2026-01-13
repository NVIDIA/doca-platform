/*
Copyright 2025 NVIDIA

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package controller

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetMgrCache(nodeName string, syncPeriod time.Duration) cache.Options {
	return cache.Options{
		// To reduce cache memory consumption, only pods on on the same node as the reconciler will be added to the cache:
		// 1) List calls using the controller Manager client will only return Pods with `spec.nodeName` set to nodeName
		// 2) Get calls for pods not in the cache will never find the pod.
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}: {Field: fields.SelectorFromSet(map[string]string{"spec.nodeName": nodeName})},
		},
		SyncPeriod: &syncPeriod,
	}
}
