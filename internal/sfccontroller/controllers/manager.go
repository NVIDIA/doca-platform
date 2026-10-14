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
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetMgrCache(nodeName string) cache.Options {
	// spec.node is a selectable field on ServiceChain, ServiceInterface, and NodeServiceInterfaces.
	// The API server applies it, so each DaemonSet pod stores only its own objects.
	nodeSelector := fields.OneTermEqualSelector("spec.node", nodeName)
	return cache.Options{
		// To reduce cache memory consumption, only objects for this node are added to the cache:
		// 1) List calls using the controller Manager client return only those objects.
		// 2) Get calls for objects outside the cache never find them.
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}:                    {Field: fields.SelectorFromSet(map[string]string{"spec.nodeName": nodeName})},
			&dpuservicev1.ServiceChain{}:     {Field: nodeSelector},
			&dpuservicev1.ServiceInterface{}: {Field: nodeSelector},
			&dpuservicev1.NodeServiceInterfaces{}: {
				Namespaces: map[string]cache.Config{
					utils.NSIObjectsNamespace: {},
				},
				Field: nodeSelector,
			},
		},
		// SyncPeriod is left unset so controller-runtime's default periodic resync (10h) applies as a slow
		// safety net for out-of-band OVS/legacy-ServiceInterface drift not covered by watches.
	}
}
