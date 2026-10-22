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

package controller

import (
	corev1 "k8s.io/api/core/v1"
	toolscache "k8s.io/client-go/tools/cache"
)

// TransformPodForCache returns a cache transform that keeps only the Pod fields read by the
// controllers in this package: object metadata (without managedFields), spec.nodeName and
// status.phase. Cached Pods are therefore incomplete and must never be used as a write body.
func TransformPodForCache() toolscache.TransformFunc {
	return func(in any) (any, error) {
		pod, ok := in.(*corev1.Pod)
		if !ok {
			return in, nil
		}
		pod.ManagedFields = nil
		pod.Spec = corev1.PodSpec{NodeName: pod.Spec.NodeName}
		pod.Status = corev1.PodStatus{Phase: pod.Status.Phase}
		return pod, nil
	}
}
