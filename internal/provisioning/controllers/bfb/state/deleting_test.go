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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("BFB Deleting State", func() {
	It("cancels and cleans up in-flight download task", func() {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		bfb := &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bfb",
				Namespace: "default",
				UID:       types.UID("uid-delete-1"),
			},
			Status: provisioningv1.BFBStatus{
				FileName: "non-existing.bfb",
			},
		}

		taskName := cutil.GenerateBFBTaskName(*bfb)
		cancelCalled := false
		butil.DownloadingTaskMap.Store(taskName, "dummy-future")
		butil.DownloadingTaskMap.Store(taskName+"cancel", context.CancelFunc(func() {
			cancelCalled = true
		}))
		defer func() {
			butil.DownloadingTaskMap.Delete(taskName)
			butil.DownloadingTaskMap.Delete(taskName + "cancel")
		}()

		st := &bfbDeletingState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(1),
		}
		err := st.Handle(context.Background(), cl)
		Expect(err).NotTo(HaveOccurred())
		Expect(cancelCalled).To(BeTrue())

		_, taskExists := butil.DownloadingTaskMap.Load(taskName)
		_, cancelExists := butil.DownloadingTaskMap.Load(taskName + "cancel")
		Expect(taskExists).To(BeFalse())
		Expect(cancelExists).To(BeFalse())
	})

	It("completes deletion when status.fileName is empty (deletion before initialization)", func() {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		now := metav1.Now()
		bfb := &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-bfb-stuck",
				Namespace:         "default",
				UID:               types.UID("uid-delete-stuck"),
				DeletionTimestamp: &now,
				Finalizers:        []string{provisioningv1.BFBFinalizer},
			},
			Status: provisioningv1.BFBStatus{
				// Race: deleted before bfbInitializingState populated FileName.
				FileName: "",
			},
		}

		st := &bfbDeletingState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(1),
		}
		Expect(st.Handle(context.Background(), cl)).To(Succeed())

		Expect(bfb.Finalizers).NotTo(ContainElement(provisioningv1.BFBFinalizer))

		deleted := meta.FindStatusCondition(bfb.Status.Conditions, string(provisioningv1.BFBCondDeleted))
		Expect(deleted).NotTo(BeNil())
		Expect(deleted.Status).To(Equal(metav1.ConditionTrue))
	})

	It("re-asserts AwaitingDeletion on the Downloaded milestone while the deletion is blocked", func() {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

		now := metav1.Now()
		bfb := &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-bfb-blocked",
				Namespace:         "default",
				UID:               types.UID("uid-delete-blocked"),
				DeletionTimestamp: &now,
				Finalizers:        []string{provisioningv1.BFBFinalizer},
			},
			Status: provisioningv1.BFBStatus{
				Phase:    provisioningv1.BFBDeleting,
				FileName: "test.bfb",
			},
		}
		// A conditions patch lost on the way into Deleting leaves the milestone True.
		conditions.AddTrue(bfb, provisioningv1.BFBCondDownloaded)
		dpuSet := &provisioningv1.DPUSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "referencing-dpuset",
				Namespace: "default",
			},
			Spec: provisioningv1.DPUSetSpec{
				DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{
						BFB: &provisioningv1.BFBReference{
							Name: bfb.Name,
						},
					},
				},
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpuSet).Build()

		st := &bfbDeletingState{
			bfb:      bfb,
			recorder: record.NewFakeRecorder(1),
		}
		Expect(st.Handle(context.Background(), cl)).To(HaveOccurred())

		Expect(bfb.Finalizers).To(ContainElement(provisioningv1.BFBFinalizer),
			"the deletion is still blocked by the DPUSet")
		downloaded := meta.FindStatusCondition(bfb.Status.Conditions, string(provisioningv1.BFBCondDownloaded))
		Expect(downloaded).NotTo(BeNil())
		Expect(downloaded.Status).To(Equal(metav1.ConditionFalse))
		Expect(downloaded.Reason).To(Equal(string(conditions.ReasonAwaitingDeletion)),
			"so the Ready summary reports AwaitingDeletion instead of staying True")
	})
})
