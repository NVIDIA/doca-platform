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
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("BFB Ready State", func() {
	var (
		ctx         context.Context
		bfb         *provisioningv1.BFB
		origBaseDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		origBaseDir = cutil.BFBBaseDir
		cutil.BFBBaseDir = GinkgoT().TempDir()
		bfb = &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-bfb",
				Namespace:  "default",
				Generation: 2,
			},
			Status: provisioningv1.BFBStatus{
				Phase:    provisioningv1.BFBReady,
				FileName: "test.bfb",
			},
		}
		conditions.AddTrue(bfb, provisioningv1.BFBCondInitialized)
		conditions.AddTrue(bfb, provisioningv1.BFBCondDownloaded)
	})

	AfterEach(func() {
		cutil.BFBBaseDir = origBaseDir
	})

	It("keeps the Downloaded milestone True while the cached file exists and never writes Ready", func() {
		Expect(os.WriteFile(cutil.GenerateBFBFilePath(bfb.Status.FileName), []byte("bfb"), 0o644)).To(Succeed())

		st := &bfbReadyState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(10),
		}
		Expect(st.Handle(ctx, nil)).To(Succeed())

		Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady))
		Expect(conditions.IsTrue(bfb, provisioningv1.BFBCondInitialized)).To(BeTrue())
		Expect(conditions.IsTrue(bfb, provisioningv1.BFBCondDownloaded)).To(BeTrue())
		Expect(conditions.Get(bfb, provisioningv1.BFBCondReady)).To(BeNil(),
			"Ready is computed by the controller's summary, not by the phase handler")
	})

	It("restores a lost Initialized milestone while the cached file exists", func() {
		Expect(os.WriteFile(cutil.GenerateBFBFilePath(bfb.Status.FileName), []byte("bfb"), 0o644)).To(Succeed())
		// A conditions patch lost to a conflict leaves the milestone re-seeded as Unknown.
		conditions.AddUnknown(bfb, provisioningv1.BFBCondInitialized, conditions.ReasonPending, "")
		st := &bfbReadyState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(1),
		}

		Expect(st.Handle(ctx, nil)).To(Succeed())

		Expect(conditions.IsTrue(bfb, provisioningv1.BFBCondInitialized)).To(BeTrue())
	})

	It("restores a lost Downloaded milestone while the cached file exists", func() {
		Expect(os.WriteFile(cutil.GenerateBFBFilePath(bfb.Status.FileName), []byte("bfb"), 0o644)).To(Succeed())
		// A conditions patch lost to a conflict leaves the milestone re-seeded as Unknown.
		conditions.AddUnknown(bfb, provisioningv1.BFBCondDownloaded, conditions.ReasonPending, "")
		st := &bfbReadyState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(1),
		}

		Expect(st.Handle(ctx, nil)).To(Succeed())

		Expect(conditions.IsTrue(bfb, provisioningv1.BFBCondDownloaded)).To(BeTrue())
	})

	It("marks Downloaded False/Error and re-downloads when the cached file is missing", func() {
		recorder := record.NewFakeRecorder(10)
		st := &bfbReadyState{
			bfb:      bfb,
			recorder: recorder,
		}
		Expect(st.Handle(ctx, nil)).To(HaveOccurred())

		Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBDownloading))
		downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
		Expect(downloadedCond).NotTo(BeNil())
		Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonError)))
		Expect(downloadedCond.Message).To(ContainSubstring("not found"))
		Expect(downloadedCond.ObservedGeneration).To(Equal(bfb.Generation))
		Expect(recorder.Events).To(Receive(ContainSubstring("not found")))
	})

	It("marks Downloaded AwaitingDeletion and moves to Deleting when the BFB is deleted", func() {
		bfb.DeletionTimestamp = &metav1.Time{Time: time.Now()}

		st := &bfbReadyState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(10),
		}
		Expect(st.Handle(ctx, nil)).To(Succeed())

		Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBDeleting))
		downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
		Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonAwaitingDeletion)))
	})
})
