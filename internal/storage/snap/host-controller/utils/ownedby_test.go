/*
Copyright 2025 NVIDIA

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

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testAnnotationKey = "test.nvidia.com/owned-by"
	testNamespace     = "test-namespace"
	testName          = "test-name"
)

func getTestObject() client.Object {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-object",
			Namespace: "default",
		},
		Data: map[string]string{
			"test-key": "test-value",
		},
	}
}

func getTestObjectWithAnnotations(annotations map[string]string) client.Object {
	obj := getTestObject()
	obj.SetAnnotations(annotations)
	return obj
}

var _ = Describe("OwnedByHelper", func() {
	var (
		ownedByHelper OwnedByHelper
		testObj       client.Object
		ownerKey      client.ObjectKey
	)
	BeforeEach(func() {
		ownedByHelper = New(testAnnotationKey)
		testObj = getTestObject()
		ownerKey = client.ObjectKey{
			Namespace: testNamespace,
			Name:      testName,
		}
	})
	Context("New", func() {
		It("should create a new OwnedByHelper with the given annotation key", func() {
			helper := New("custom.annotation/key")
			Expect(helper).NotTo(BeNil())
		})
	})
	Context("SetOwnedBy", func() {
		It("should set owned by annotation on object with no existing annotations", func() {
			ownedByHelper.SetOwnedBy(testObj, ownerKey)

			annotations := testObj.GetAnnotations()
			Expect(annotations).NotTo(BeNil())
			Expect(annotations).To(HaveKeyWithValue(testAnnotationKey, "test-namespace/test-name"))
		})
		It("should set owned by annotation on object with existing annotations", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				"existing.annotation": "existing-value",
			})

			ownedByHelper.SetOwnedBy(testObj, ownerKey)

			annotations := testObj.GetAnnotations()
			Expect(annotations).NotTo(BeNil())
			Expect(annotations).To(HaveKeyWithValue("existing.annotation", "existing-value"))
			Expect(annotations).To(HaveKeyWithValue(testAnnotationKey, "test-namespace/test-name"))
		})
		It("should overwrite existing owned by annotation", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "old-namespace/old-name",
			})

			ownedByHelper.SetOwnedBy(testObj, ownerKey)

			annotations := testObj.GetAnnotations()
			Expect(annotations).NotTo(BeNil())
			Expect(annotations).To(HaveKeyWithValue(testAnnotationKey, "test-namespace/test-name"))
		})
		It("should handle owner key with cluster-scoped resource (empty namespace)", func() {
			clusterScopedOwner := client.ObjectKey{
				Namespace: "",
				Name:      "cluster-resource",
			}

			ownedByHelper.SetOwnedBy(testObj, clusterScopedOwner)

			annotations := testObj.GetAnnotations()
			Expect(annotations).NotTo(BeNil())
			Expect(annotations).To(HaveKeyWithValue(testAnnotationKey, "/cluster-resource"))
		})
	})
	Context("GetOwnedBy", func() {
		It("should successfully get owned by from valid annotation", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "test-namespace/test-name",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).NotTo(HaveOccurred())
			Expect(owner.Namespace).To(Equal("test-namespace"))
			Expect(owner.Name).To(Equal("test-name"))
		})
		It("should return error when object has no annotations", func() {
			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).To(MatchError(ContainSubstring("annotations not set on object")))
			Expect(owner).To(Equal(client.ObjectKey{}))
		})
		It("should return error when owned by annotation is not found", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				"other.annotation": "other-value",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).To(MatchError(ContainSubstring("owned by annotation not found")))
			Expect(owner).To(Equal(client.ObjectKey{}))
		})
		It("should return error when owned by annotation is empty", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).To(MatchError(ContainSubstring("owned by annotation not found")))
			Expect(owner).To(Equal(client.ObjectKey{}))
		})
		It("should return error when owned by annotation format is invalid - no slash", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "invalid-format",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).To(MatchError(ContainSubstring("invalid owned by annotation format")))
			Expect(owner).To(Equal(client.ObjectKey{}))
		})
		It("should return error when owned by annotation format is invalid - too many parts", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "namespace/name/extra",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).To(MatchError(ContainSubstring("invalid owned by annotation format")))
			Expect(owner).To(Equal(client.ObjectKey{}))
		})
		It("should successfully parse cluster-scoped resource with empty namespace for owned by annotation", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "/test-name",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).NotTo(HaveOccurred())
			Expect(owner.Namespace).To(Equal(""))
			Expect(owner.Name).To(Equal("test-name"))
		})
		It("should return error when name part is empty for owned by annotation", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "test-namespace/",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).To(MatchError(ContainSubstring("invalid owned by annotation format")))
			Expect(owner).To(Equal(client.ObjectKey{}))
		})
		It("should successfully parse cluster-scoped resource for owned by annotation", func() {
			testObj = getTestObjectWithAnnotations(map[string]string{
				testAnnotationKey: "/cluster-resource",
			})

			owner, err := ownedByHelper.GetOwnedBy(testObj)
			Expect(err).NotTo(HaveOccurred())
			Expect(owner.Namespace).To(Equal(""))
			Expect(owner.Name).To(Equal("cluster-resource"))
		})
	})
})
