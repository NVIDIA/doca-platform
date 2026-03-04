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

package cleanup

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	testClient client.Client
	ctx        context.Context
)

const testNamespace = "cleanup-test"

// noOpCleanupFunc is a cleanup function that does nothing (for unit tests)
func noOpCleanupFunc(_ context.Context, _ client.Client, _ labels.Selector, _ ...client.ObjectList) error {
	return nil
}

func TestCleanupTracker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cleanup Tracker Suite")
}

var _ = BeforeSuite(func() {
	ctx = context.Background()
	testClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
})

var _ = Describe("Cleanup Tracker", func() {
	Context("MergeMaps", func() {
		It("should merge two maps", func() {
			a := map[string]string{"key1": "val1"}
			b := map[string]string{"key2": "val2"}
			result := MergeMaps(a, b)

			Expect(result).To(HaveKeyWithValue("key1", "val1"))
			Expect(result).To(HaveKeyWithValue("key2", "val2"))
		})

		It("should override with second map values", func() {
			a := map[string]string{"key": "original"}
			b := map[string]string{"key": "override"}
			result := MergeMaps(a, b)

			Expect(result).To(HaveKeyWithValue("key", "override"))
		})

		It("should handle nil maps", func() {
			result := MergeMaps(nil, map[string]string{"key": "val"})
			Expect(result).To(HaveKeyWithValue("key", "val"))
		})

		It("should return empty map when both nil", func() {
			result := MergeMaps(nil, nil)
			Expect(result).To(BeEmpty())
		})
	})

	Context("CleanupLabels selectors", func() {
		It("should have It labels", func() {
			Expect(CleanupLabels.It).NotTo(BeEmpty())
		})

		It("should have Suite labels", func() {
			Expect(CleanupLabels.Suite).NotTo(BeEmpty())
		})

		It("should have different It and Suite labels", func() {
			Expect(CleanupLabels.It).NotTo(Equal(CleanupLabels.Suite))
		})
	})

	Context("Scope Registration", func() {
		var tracker *Tracker

		BeforeEach(func() {
			// Use Init() directly instead of NewCleanupFlagsFromCLI() to avoid global flag registration
			tracker = NewTracker(noOpCleanupFunc, (&CleanupFlags{}).Init(), ctx, testClient, []client.ObjectList{&corev1.ConfigMapList{}})
		})

		It("should register a scope and return it", func() {
			scope := tracker.RegisterScope(NamedScopeManual("test-scope"))

			Expect(scope).NotTo(BeNil())
			Expect(scope.Name).To(Equal("test-scope"))
		})

		It("should return same scope when registering twice", func() {
			scope1 := tracker.RegisterScope(NamedScopeManual("same-scope"))
			scope2 := tracker.RegisterScope(NamedScopeManual("same-scope"))

			// BeIdenticalTo checks pointer identity - both variables point to the same object
			Expect(scope1).To(BeIdenticalTo(scope2))
		})

		It("should generate cleanup labels for scope", func() {
			scope := tracker.RegisterScope(NamedScopeManual("labeled-scope"))

			// Should have exactly 2 labels: scope-specific + global e2e cleanup label
			expectedLabels := MergeMaps(cleanupScopes.ScopeSelector("labeled-scope"), globalE2ETestCleanupLabel)
			Expect(scope.CleanupLabels).To(HaveLen(2))
			Expect(scope.CleanupLabels).To(Equal(expectedLabels))
		})
	})

	Context("ResourcesExist and CountResources", func() {
		var (
			tracker *Tracker
			scope   *Scope
		)

		BeforeEach(func() {
			// Create fresh client for each test to avoid state leakage
			testClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

			tracker = NewTracker(noOpCleanupFunc, (&CleanupFlags{}).Init(), ctx, testClient, []client.ObjectList{&corev1.ConfigMapList{}})
			scope = tracker.RegisterScope(NamedScopeManual("resource-test-" + utilrand.String(5)))
		})

		It("should report empty for scope without resources", func() {
			Expect(scope.ResourcesExist()).To(BeFalse())
			Expect(scope.CountResources()).To(Equal(0))
		})

		It("should find resource labeled with scope.CleanupLabels", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configMap",
					Namespace: testNamespace,
					Labels:    scope.CleanupLabels,
				},
			}
			Expect(testClient.Create(ctx, configMap)).To(Succeed())

			Expect(scope.ResourcesExist()).To(BeTrue())
			Expect(scope.CountResources()).To(Equal(1))
		})

		It("should ignore resources with different labels", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-configMap",
					Namespace: testNamespace,
					Labels:    map[string]string{"other": "label"},
				},
			}
			Expect(testClient.Create(ctx, configMap)).To(Succeed())

			Expect(scope.ResourcesExist()).To(BeFalse())
			Expect(scope.CountResources()).To(Equal(0))
		})

		It("should count multiple resources", func() {
			for i := 0; i < 3; i++ {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("configMap-%d", i),
						Namespace: testNamespace,
						Labels:    scope.CleanupLabels,
					},
				}
				Expect(testClient.Create(ctx, configMap)).To(Succeed())
			}

			Expect(scope.CountResources()).To(Equal(3))
		})

		It("should detect resources with merged labels (scope + custom)", func() {
			customLabels := map[string]string{"custom": "value"}
			merged := MergeMaps(scope.CleanupLabels, customLabels)

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "merged-configMap",
					Namespace: testNamespace,
					Labels:    merged,
				},
			}
			Expect(testClient.Create(ctx, configMap)).To(Succeed())

			// Should be found by scope (has all scope labels)
			Expect(scope.ResourcesExist()).To(BeTrue())
		})
	})

	Context("CleanupFlags", func() {
		It("should initialize with defaults", func() {
			flags := &CleanupFlags{}

			Expect(flags.SkipCleanup).To(BeFalse())
			Expect(flags.SkipCleanupOnFailure).To(BeFalse())
		})

		It("should parse scope filter correctly", func() {
			flags := &CleanupFlags{
				SkipNamedScopesBefore: "scope-a,scope-b",
			}
			flags.Init()

			// Filter should match scope-a
			Expect(flags.shouldSkipNamedScopeBefore([]string{"scope-a"})).To(BeTrue())
			// Filter should not match scope-c
			Expect(flags.shouldSkipNamedScopeBefore([]string{"scope-c"})).To(BeFalse())
		})

		It("should skip only scopes matching filter expression", func() {
			flags := &CleanupFlags{
				SkipNamedScopesAfter: "my-scope",
			}
			flags.Init()

			// Should match the specific scope
			Expect(flags.shouldSkipNamedScopeAfter([]string{"my-scope"})).To(BeTrue())
			// Should not match other scopes
			Expect(flags.shouldSkipNamedScopeAfter([]string{"other-scope"})).To(BeFalse())
		})

		It("should not skip any scope when filter is empty", func() {
			flags := (&CleanupFlags{}).Init()

			// Empty filter should not skip anything
			Expect(flags.shouldSkipNamedScopeBefore([]string{"any-scope"})).To(BeFalse())
			Expect(flags.shouldSkipNamedScopeAfter([]string{"any-scope"})).To(BeFalse())
		})
	})

	Context("NamedScopeManual", func() {
		It("should create namedScope with Manual mode", func() {
			ns := NamedScopeManual("my-scope")

			Expect(ns.name).To(Equal("my-scope"))
			Expect(ns.mode).To(Equal(Manual))
		})
	})
})
