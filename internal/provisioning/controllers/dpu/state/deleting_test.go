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

package state_test

import (
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("DPU: deleting", func() {
	const (
		dpuName       = "dpu-deleting-finalizer-test"
		dpuDeviceName = "dpu-device-deleting-finalizer-test"
	)

	It("deleting state should remove finalizers from DpuDevice", func() {
		By("prepare DPUDevice CR with finalizer (as added by DPU controller when DPU uses it)")
		dpuDevice := dpuDeviceObj(dpuDeviceName)
		controllerutil.AddFinalizer(dpuDevice, provisioningv1.DPUDeviceFinalizer)
		createObject(dpuDevice)

		By("prepare DPU CR in OsInstalling state")
		dpu := dpuObj(dpuName)
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}}
		dpu.Spec.Cluster.Name = "" // skip deleteNode in Deleting
		dpu.Status.Phase = provisioningv1.DPUOSInstalling
		dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
		createObject(dpu)

		By("delete DpuDevice")
		Expect(k8sClient.Delete(ctx, dpuDevice)).To(Succeed())

		By("verify DpuDevice still exists (finalizer blocks deletion)")
		gotDevice := &provisioningv1.DPUDevice{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: dpuDevice.Namespace, Name: dpuDevice.Name}, gotDevice)).To(Succeed())
		Expect(gotDevice.DeletionTimestamp).NotTo(BeNil())
		Expect(controllerutil.ContainsFinalizer(gotDevice, provisioningv1.DPUDeviceFinalizer)).To(BeTrue())

		By("move to Deleting state and run cleanup")
		ctrlCtx := &dutil.ControllerContext{
			Client:               k8sClient,
			DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(10),
			ClusterAllocator:     &noOpAllocator{},
		}
		_, err := state.Deleting(ctx, dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())

		By("verify DpuDevice is deleted after finalizer removal")
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: dpuDevice.Namespace, Name: dpuDevice.Name}, &provisioningv1.DPUDevice{})
			return apierrors.IsNotFound(err)
		}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
	})
})

// noOpAllocator implements allocator.Allocator for tests; ClusterAllocator must be non-nil in Deleting().
type noOpAllocator struct{}

func (n *noOpAllocator) Allocate(context.Context, *provisioningv1.DPU) (allocator.AllocateResult, error) {
	return types.NamespacedName{}, nil
}
func (n *noOpAllocator) SaveAssignedDPU(*provisioningv1.DPU)      {}
func (n *noOpAllocator) SaveCluster(*provisioningv1.DPUCluster)   {}
func (n *noOpAllocator) ReleaseDPU(*provisioningv1.DPU)           {}
func (n *noOpAllocator) RemoveCluster(*provisioningv1.DPUCluster) {}
func (n *noOpAllocator) GetDPUsCount(*provisioningv1.DPUCluster) int {
	return 0
}
