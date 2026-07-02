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

package dpu

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const adoptNS = "adopt-ns"

func adoptScheme(g *WithT) *runtime.Scheme {
	s := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(s)).To(Succeed())
	return s
}

func adoptReconciler(s *runtime.Scheme, objs ...client.Object) *DPUReconciler {
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	return &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: c, Scheme: s}}
}

func templateModeDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dpu-0", Namespace: adoptNS, UID: "dpu-uid",
			Labels: map[string]string{cutil.DPUFlavorTemplateNameLabel: "tmpl"},
		},
		Spec: provisioningv1.DPUSpec{DPUFlavor: "dpu-0"},
	}
}

func generatedFlavor() *provisioningv1.DPUFlavor {
	return &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dpu-0", Namespace: adoptNS,
			Labels: map[string]string{cutil.GeneratedByLabel: cutil.GeneratedByDPUFlavorTemplate},
		},
	}
}

func getFlavor(g *WithT, r *DPUReconciler) *provisioningv1.DPUFlavor {
	got := &provisioningv1.DPUFlavor{}
	g.Expect(r.ctrlCtx.Client.Get(context.Background(), types.NamespacedName{Namespace: adoptNS, Name: "dpu-0"}, got)).To(Succeed())
	return got
}

func TestAdoptGeneratedFlavor(t *testing.T) {
	ctx := context.Background()

	t.Run("sets ownerRef and finalizer on the generated flavor", func(t *testing.T) {
		g := NewWithT(t)
		dpu := templateModeDPU()
		r := adoptReconciler(adoptScheme(g), dpu, generatedFlavor())

		g.Expect(r.adoptGeneratedFlavor(ctx, dpu)).To(Succeed())

		got := getFlavor(g, r)
		g.Expect(controllerutil.ContainsFinalizer(got, cutil.GeneratedDPUFlavorFinalizer)).To(BeTrue())
		g.Expect(metav1.IsControlledBy(got, dpu)).To(BeTrue())
	})

	t.Run("is a no-op for a static (non-template) DPU", func(t *testing.T) {
		g := NewWithT(t)
		dpu := templateModeDPU()
		dpu.Labels = nil // static-mode DPU: no template-name label
		// A static DPU's dpu.Spec.DPUFlavor points at a shared user flavor that must not be touched.
		userFlavor := generatedFlavor()
		userFlavor.Labels = nil
		r := adoptReconciler(adoptScheme(g), dpu, userFlavor)

		g.Expect(r.adoptGeneratedFlavor(ctx, dpu)).To(Succeed())

		got := getFlavor(g, r)
		g.Expect(got.Finalizers).To(BeEmpty())
		g.Expect(got.OwnerReferences).To(BeEmpty())
	})

	t.Run("does not adopt a flavor lacking the generated-by label", func(t *testing.T) {
		g := NewWithT(t)
		dpu := templateModeDPU()
		userFlavor := generatedFlavor()
		userFlavor.Labels = nil // user-authored flavor that happens to share the name
		r := adoptReconciler(adoptScheme(g), dpu, userFlavor)

		g.Expect(r.adoptGeneratedFlavor(ctx, dpu)).To(Succeed())

		got := getFlavor(g, r)
		g.Expect(got.Finalizers).To(BeEmpty())
		g.Expect(got.OwnerReferences).To(BeEmpty())
	})

	t.Run("tolerates a missing flavor (render-failed/in-flight reprovision)", func(t *testing.T) {
		g := NewWithT(t)
		dpu := templateModeDPU()
		r := adoptReconciler(adoptScheme(g), dpu) // no flavor object

		g.Expect(r.adoptGeneratedFlavor(ctx, dpu)).To(Succeed())
	})

	t.Run("is idempotent when ownerRef and finalizer are already present", func(t *testing.T) {
		g := NewWithT(t)
		dpu := templateModeDPU()
		r := adoptReconciler(adoptScheme(g), dpu, generatedFlavor())

		g.Expect(r.adoptGeneratedFlavor(ctx, dpu)).To(Succeed())
		g.Expect(r.adoptGeneratedFlavor(ctx, dpu)).To(Succeed())

		got := getFlavor(g, r)
		g.Expect(got.OwnerReferences).To(HaveLen(1))
		g.Expect(got.Finalizers).To(ConsistOf(cutil.GeneratedDPUFlavorFinalizer))
	})
}
