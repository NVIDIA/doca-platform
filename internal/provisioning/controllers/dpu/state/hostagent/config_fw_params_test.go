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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ConfigFWParameters", func() {
	It("re-creates the per-DPU agent RBAC while waiting for the agent to apply NVConfig", func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(provisioningv1.AddToScheme(scheme))

		device := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{Name: "device-01", Namespace: "default"},
		}
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-01", Namespace: "default", UID: types.UID("dpu-01-uid")},
			Spec: provisioningv1.DPUSpec{
				DPUDeviceName: device.Name,
				DPUFlavor:     "dpu-flavor",
			},
			Status: provisioningv1.DPUStatus{
				Phase: provisioningv1.DPUConfigFWParameters,
				AgentStatus: &provisioningv1.AgentStatus{
					PreInstall: &provisioningv1.AgentPreInstallStatus{AgentReported: ptr.To(metav1.Now())},
				},
			},
		}
		ctrlCtx := &dutil.ControllerContext{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(device).Build(),
			Scheme: scheme,
		}

		// The DPUSet generation that owned the previous RBAC was deleted, so garbage
		// collection took the Role and RoleBinding with it.
		rbacKey := client.ObjectKey{Name: "da-" + dpu.Name, Namespace: dpu.Namespace}
		Expect(apierrors.IsNotFound(ctrlCtx.Get(context.Background(), rbacKey, &rbacv1.Role{}))).To(BeTrue())

		status, err := ConfigFWParameters(context.Background(), dpu, ctrlCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
		Expect(ctrlCtx.Get(context.Background(), rbacKey, &rbacv1.Role{})).To(Succeed())
		Expect(ctrlCtx.Get(context.Background(), rbacKey, &rbacv1.RoleBinding{})).To(Succeed())
	})
})
