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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
)

var _ = Describe("TransformPodForCache", func() {
	It("keeps only the Pod fields the controllers read", func() {
		now := metav1.Now()
		pod := &corev1.Pod{
			TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:              "p",
				Namespace:         "ns",
				UID:               "uid",
				ResourceVersion:   "42",
				Labels:            map[string]string{"l": "v"},
				Annotations:       map[string]string{NetworkAttachmentAnnot: "[]", NetworkDigestAnnotation: "d"},
				DeletionTimestamp: &now,
				OwnerReferences:   []metav1.OwnerReference{{Name: "rs"}},
				ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "m"}},
			},
			Spec: corev1.PodSpec{
				NodeName:   "node-1",
				Containers: []corev1.Container{{Name: "c", Image: "i", Env: []corev1.EnvVar{{Name: "E", Value: "V"}}}},
				Volumes:    []corev1.Volume{{Name: "v"}},
			},
			Status: corev1.PodStatus{
				Phase:             corev1.PodPending,
				PodIP:             "10.0.0.1",
				Conditions:        []corev1.PodCondition{{Type: corev1.PodReady}},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "c"}},
			},
		}

		out, err := TransformPodForCache()(pod)
		Expect(err).NotTo(HaveOccurred())

		Expect(out).To(Equal(&corev1.Pod{
			TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:              "p",
				Namespace:         "ns",
				UID:               "uid",
				ResourceVersion:   "42",
				Labels:            map[string]string{"l": "v"},
				Annotations:       map[string]string{NetworkAttachmentAnnot: "[]", NetworkDigestAnnotation: "d"},
				DeletionTimestamp: &now,
				OwnerReferences:   []metav1.OwnerReference{{Name: "rs"}},
			},
			Spec:   corev1.PodSpec{NodeName: "node-1"},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		}))
	})

	It("passes non-Pod objects through unchanged", func() {
		tombstone := toolscache.DeletedFinalStateUnknown{Key: "ns/p", Obj: &corev1.Pod{}}

		out, err := TransformPodForCache()(tombstone)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal(tombstone))
	})
})
