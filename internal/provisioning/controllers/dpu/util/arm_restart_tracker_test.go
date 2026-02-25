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

package util

import (
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ArmRestartTracker", func() {
	Context("struct methods", func() {
		It("should track attempts and report completion correctly", func() {
			tracker := &ArmRestartTracker{MaxAttempts: 2}

			Expect(tracker.Attempt).To(Equal(0))
			Expect(tracker.AllRestartsDone()).To(BeFalse())

			tracker.IncrementAttempt()
			Expect(tracker.Attempt).To(Equal(1))
			Expect(tracker.AllRestartsDone()).To(BeFalse())
			Expect(tracker.LastRestartTime).NotTo(BeZero())

			tracker.IncrementAttempt()
			Expect(tracker.Attempt).To(Equal(2))
			Expect(tracker.AllRestartsDone()).To(BeTrue())
		})

		It("should detect stale trackers based on timeout", func() {
			tracker := &ArmRestartTracker{MaxAttempts: 2}

			By("not stale when LastRestartTime is zero (not started)")
			Expect(tracker.IsStale()).To(BeFalse())

			By("not stale when recently updated")
			tracker.LastRestartTime = time.Now()
			Expect(tracker.IsStale()).To(BeFalse())

			By("stale when older than StaleTrackerTimeout")
			tracker.LastRestartTime = time.Now().Add(-StaleTrackerTimeout - time.Second)
			Expect(tracker.IsStale()).To(BeTrue())
		})

		It("should handle AllRestartsDone when attempts exceed max", func() {
			tracker := &ArmRestartTracker{Attempt: 5, MaxAttempts: 2}
			Expect(tracker.AllRestartsDone()).To(BeTrue())
		})
	})

	Context("annotation persistence", func() {
		var dpu *provisioningv1.DPU

		BeforeEach(func() {
			dpu = &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "default",
				},
			}
		})

		It("should round-trip save and load correctly", func() {
			original := &ArmRestartTracker{
				Attempt:           1,
				MaxAttempts:       2,
				LastRestartTime:   time.Now().Truncate(time.Second),
				InitialGeneration: 42,
			}

			Expect(SaveArmRestartTracker(dpu, original)).To(Succeed())
			Expect(dpu.Annotations).To(HaveKey(provisioningv1.AnnotationArmRestartTracker))

			loaded, err := LoadArmRestartTracker(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded).NotTo(BeNil())
			Expect(loaded.Attempt).To(Equal(original.Attempt))
			Expect(loaded.MaxAttempts).To(Equal(original.MaxAttempts))
			Expect(loaded.InitialGeneration).To(Equal(original.InitialGeneration))
		})

		It("should return nil when no annotation exists", func() {
			loaded, err := LoadArmRestartTracker(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded).To(BeNil())
		})

		It("should return nil when annotations map is nil", func() {
			dpu.Annotations = nil
			loaded, err := LoadArmRestartTracker(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded).To(BeNil())
		})

		It("should return error for corrupted annotation data", func() {
			dpu.Annotations = map[string]string{
				provisioningv1.AnnotationArmRestartTracker: "not-valid-json",
			}
			loaded, err := LoadArmRestartTracker(dpu)
			Expect(err).To(HaveOccurred())
			Expect(loaded).To(BeNil())
		})

		It("should return error for invalid MaxAttempts (zero or negative)", func() {
			dpu.Annotations = map[string]string{
				provisioningv1.AnnotationArmRestartTracker: `{"attempt":0,"maxAttempts":0}`,
			}
			loaded, err := LoadArmRestartTracker(dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("MaxAttempts must be positive"))
			Expect(loaded).To(BeNil())

			dpu.Annotations[provisioningv1.AnnotationArmRestartTracker] = `{"attempt":0,"maxAttempts":-1}`
			loaded, err = LoadArmRestartTracker(dpu)
			Expect(err).To(HaveOccurred())
			Expect(loaded).To(BeNil())
		})

		It("should return error for negative Attempt count", func() {
			dpu.Annotations = map[string]string{
				provisioningv1.AnnotationArmRestartTracker: `{"attempt":-1,"maxAttempts":2}`,
			}
			loaded, err := LoadArmRestartTracker(dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Attempt cannot be negative"))
			Expect(loaded).To(BeNil())
		})

		It("should initialize annotations map when saving to DPU with nil annotations", func() {
			Expect(dpu.Annotations).To(BeNil())
			Expect(SaveArmRestartTracker(dpu, &ArmRestartTracker{MaxAttempts: 2})).To(Succeed())
			Expect(dpu.Annotations).NotTo(BeNil())
		})

		It("should remove annotation on clear and be safe on nil annotations", func() {
			By("saving then clearing")
			Expect(SaveArmRestartTracker(dpu, &ArmRestartTracker{MaxAttempts: 2})).To(Succeed())
			Expect(dpu.Annotations).To(HaveKey(provisioningv1.AnnotationArmRestartTracker))

			ClearArmRestartTracker(dpu)
			Expect(dpu.Annotations).NotTo(HaveKey(provisioningv1.AnnotationArmRestartTracker))

			By("clearing with nil annotations should not panic")
			dpu.Annotations = nil
			Expect(func() { ClearArmRestartTracker(dpu) }).NotTo(Panic())
		})
	})
})
