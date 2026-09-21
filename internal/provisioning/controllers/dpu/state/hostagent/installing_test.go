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

package hostagent

import (
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/install"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Installing", func() {
	var (
		ctx     context.Context
		dpu     *provisioningv1.DPU
		ctrlCtx *dutil.ControllerContext
	)

	// bfbPreparedAt anchors the installation start, which is what the timeout is measured from.
	bfbPreparedAt := func(t time.Time) metav1.Condition {
		return metav1.Condition{
			Type:               string(provisioningv1.DPUCondBFBPrepared),
			Status:             metav1.ConditionTrue,
			Reason:             "Prepared",
			LastTransitionTime: metav1.Time{Time: t},
		}
	}

	installInProgress := metav1.Condition{
		Type:   string(provisioningv1.DPUCondOSInstalled),
		Status: metav1.ConditionFalse,
		Reason: install.InstallationInProgress,
	}

	BeforeEach(func() {
		ctx = context.Background()
		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: "default",
			},
			Status: provisioningv1.DPUStatus{
				Phase: provisioningv1.DPUOSInstalling,
			},
		}
		ctrlCtx = &dutil.ControllerContext{}
		ctrlCtx.Options.OSInstallTimeout = 45 * time.Minute
	})

	It("should transition to DPUError once the install exceeds OSInstallTimeout", func() {
		dpu.Status.Conditions = []metav1.Condition{bfbPreparedAt(time.Now().Add(-50 * time.Minute)), installInProgress}

		status, err := Installing(ctx, dpu, ctrlCtx)

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		_, cond := cutil.GetDPUCondition(&status, string(provisioningv1.DPUCondOSInstalled))
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("InstallationTimeout"))
	})

	// Regression guard for the timeout check ordering: the deletion branch below it returns
	// early, so a timeout placed after it would leave a deleting DPU pinned in DPUOSInstalling
	// with its finalizer held. DPUError is what lets state.Error hand it on to DPUDeleting.
	It("should transition a deleting DPU to DPUError once the install exceeds OSInstallTimeout", func() {
		now := metav1.Now()
		dpu.DeletionTimestamp = &now
		dpu.Status.Conditions = []metav1.Condition{bfbPreparedAt(time.Now().Add(-50 * time.Minute)), installInProgress}

		status, err := Installing(ctx, dpu, ctrlCtx)

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
	})

	It("should keep waiting for a deleting DPU while the install is still within OSInstallTimeout", func() {
		now := metav1.Now()
		dpu.DeletionTimestamp = &now
		dpu.Status.Conditions = []metav1.Condition{bfbPreparedAt(time.Now()), installInProgress}

		status, err := Installing(ctx, dpu, ctrlCtx)

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
	})

	It("should transition a deleting DPU to DPUDeleting once the install is terminated", func() {
		now := metav1.Now()
		dpu.DeletionTimestamp = &now
		dpu.Status.Conditions = []metav1.Condition{
			bfbPreparedAt(time.Now()),
			{
				Type:   string(provisioningv1.DPUCondOSInstalled),
				Status: metav1.ConditionFalse,
				Reason: install.InstallationTerminated,
			},
		}

		status, err := Installing(ctx, dpu, ctrlCtx)

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
	})

	It("should not time out when OSInstallTimeout is disabled", func() {
		ctrlCtx.Options.OSInstallTimeout = 0
		dpu.Status.Conditions = []metav1.Condition{bfbPreparedAt(time.Now().Add(-50 * time.Minute)), installInProgress}

		status, err := Installing(ctx, dpu, ctrlCtx)

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
	})
})
