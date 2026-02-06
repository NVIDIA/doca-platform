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

package state

import (
	"context"
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

// Note: TestBFBState is the main test entry point in error_test.go
// This file contains additional specs that are included in the same suite

// cleanupDownloadTasks removes any pending download tasks from the map
func cleanupDownloadTasks(bfb *provisioningv1.BFB) {
	taskName := cutil.GenerateBFBTaskName(*bfb)
	butil.DownloadingTaskMap.Delete(taskName)
	butil.DownloadingTaskMap.Delete(taskName + "cancel")
}

var _ = Describe("BFB Downloading State Error Handling", func() {
	var (
		ctx      context.Context
		bfb      *provisioningv1.BFB
		recorder *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		recorder = record.NewFakeRecorder(10)
		bfb = &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bfb",
				Namespace: "default",
			},
			Spec: provisioningv1.BFBSpec{
				URL: "http://example.com/test.bfb",
			},
			Status: provisioningv1.BFBStatus{
				Phase:    provisioningv1.BFBDownloading,
				FileName: "test.bfb",
			},
		}
	})

	AfterEach(func() {
		// Clean up any download tasks
		cleanupDownloadTasks(bfb)
	})

	Describe("checkBFB error handling", func() {
		Context("when checkBFB returns a filesystem error", func() {
			It("should transition to Error phase with ReasonFailure", func() {
				st := &bfbDownloadingState{
					bfb:      bfb,
					recorder: recorder,
					checkBFB: func(fileName string) (bool, error) {
						return false, errors.New("permission denied: /bfb-cache/test.bfb")
					},
					versionBFB: versionBFB,
				}
				err := st.Handle(ctx, nil)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("permission denied"))
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBError),
					"should transition to Error phase")

				// Verify condition is set with ReasonFailure (terminal error)
				downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
				Expect(downloadedCond).NotTo(BeNil())
				Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonFailure)),
					"filesystem errors should use ReasonFailure (no retry)")
				Expect(downloadedCond.Message).To(ContainSubstring("permission denied"))
			})

			It("should emit a warning event", func() {
				st := &bfbDownloadingState{
					bfb:      bfb,
					recorder: recorder,
					checkBFB: func(fileName string) (bool, error) {
						return false, errors.New("disk I/O error")
					},
					versionBFB: versionBFB,
				}
				_ = st.Handle(ctx, nil)

				// Check that a warning event was recorded
				Eventually(recorder.Events).Should(Receive(ContainSubstring("disk I/O error")))
			})
		})

		Context("when checkBFB returns file not found (normal case)", func() {
			It("should NOT return an error - file not found is expected", func() {
				st := &bfbDownloadingState{
					bfb:      bfb,
					recorder: recorder,
					checkBFB: func(fileName string) (bool, error) {
						return false, nil
					},
					versionBFB: versionBFB,
				}
				err := st.Handle(ctx, nil)

				// No error should be returned - this is the normal download start path
				Expect(err).NotTo(HaveOccurred())
				// Phase should still be Downloading (not Error)
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBDownloading))
			})
		})
	})

	Describe("versionBFBFunc error handling", func() {
		Context("when versionBFB returns a parse error", func() {
			It("should transition to Error phase with ReasonFailure", func() {
				// Clean up any existing download tasks first
				cleanupDownloadTasks(bfb)

				st := &bfbDownloadingState{
					bfb:      bfb,
					recorder: recorder,
					checkBFB: func(fileName string) (bool, error) {
						return true, nil // File exists
					},
					versionBFB: func(filename string) (*provisioningv1.BFBVersions, error) {
						return nil, errors.New("failed to parse BFB version: invalid format")
					},
				}
				err := st.Handle(ctx, nil)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse BFB version"))
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBError),
					"should transition to Error phase")

				// Verify condition is set with ReasonFailure (terminal error)
				downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
				Expect(downloadedCond).NotTo(BeNil())
				Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonFailure)),
					"version parse errors should use ReasonFailure (no retry)")
				Expect(downloadedCond.Message).To(ContainSubstring("failed to parse BFB version"))
			})

			It("should emit a warning event for version parse error", func() {
				cleanupDownloadTasks(bfb)
				st := &bfbDownloadingState{
					bfb:      bfb,
					recorder: recorder,
					checkBFB: func(fileName string) (bool, error) {
						return true, nil
					},
					versionBFB: func(filename string) (*provisioningv1.BFBVersions, error) {
						return nil, errors.New("invalid BFB file structure")
					},
				}
				_ = st.Handle(ctx, nil)

				// Check that a warning event was recorded
				Eventually(recorder.Events).Should(Receive(ContainSubstring("invalid BFB file structure")))
			})
		})

		Context("when versionBFBFunc succeeds", func() {
			It("should transition to Ready phase", func() {
				cleanupDownloadTasks(bfb)
				st := &bfbDownloadingState{
					bfb:      bfb,
					recorder: recorder,
					checkBFB: func(fileName string) (bool, error) {
						return true, nil
					},
					versionBFB: func(filename string) (*provisioningv1.BFBVersions, error) {
						return &provisioningv1.BFBVersions{
							BSP:  "4.7.0",
							DOCA: "2.7.0",
							UEFI: "4.7.0-10",
							ATF:  "4.7.0-10",
						}, nil
					},
				}
				err := st.Handle(ctx, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady),
					"should transition to Ready phase")

				// Verify versions are set
				Expect(bfb.Status.Versions.BSP).To(Equal("4.7.0"))
				Expect(bfb.Status.Versions.DOCA).To(Equal("2.7.0"))

				// Verify condition is set with success
				downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
				Expect(downloadedCond).NotTo(BeNil())
				Expect(downloadedCond.Status).To(Equal(metav1.ConditionTrue))
			})
		})
	})

	Describe("Error classification for retry logic", func() {
		It("checkBFB errors should NOT be retried (ReasonFailure)", func() {
			st := &bfbDownloadingState{
				bfb:      bfb,
				recorder: recorder,
				checkBFB: func(fileName string) (bool, error) {
					return false, errors.New("no space left on device")
				},
				versionBFB: versionBFB,
			}
			_ = st.Handle(ctx, nil)

			downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
			Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonFailure)),
				"filesystem errors use ReasonFailure - not eligible for retry")
		})

		It("versionBFB errors should NOT be retried (ReasonFailure)", func() {
			cleanupDownloadTasks(bfb)
			st := &bfbDownloadingState{
				bfb:      bfb,
				recorder: recorder,
				checkBFB: func(fileName string) (bool, error) {
					return true, nil
				},
				versionBFB: func(filename string) (*provisioningv1.BFBVersions, error) {
					return nil, errors.New("corrupt BFB file")
				},
			}
			_ = st.Handle(ctx, nil)

			downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
			Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonFailure)),
				"version parse errors use ReasonFailure - not eligible for retry")
		})
	})
})

var _ = Describe("checkBFB function", func() {
	It("should return false, nil for non-existent file", func() {
		// Test the actual function implementation
		exists, err := checkBFB("nonexistent-file-that-does-not-exist.bfb")
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeFalse())
	})
})
