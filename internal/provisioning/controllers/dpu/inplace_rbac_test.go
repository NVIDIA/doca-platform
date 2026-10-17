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

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureInPlaceAgentRole(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(rbacv1.AddToScheme(scheme)).To(Succeed())

	dpu := inPlaceRBACDPU()
	flavor := &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough",
			Namespace: dpu.Namespace,
		},
		Spec: provisioningv1.DPUFlavorSpec{
			ConfigFiles: []provisioningv1.ConfigFile{
				agentAppliedMarkerFile(),
			},
		},
	}
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpu.Spec.DPUDeviceName,
			Namespace: dpu.Namespace,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu, flavor, device).Build()
	r := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: c, Scheme: scheme}}

	g.Expect(r.ensureInPlaceAgentRole(ctx, dpu)).To(Succeed())
	g.Expect(configMapNames(ctx, g, c, dpu)).To(ContainElement("dpf-reconfig-marker"))

	idle := dpu.DeepCopy()
	idle.Status.DPUReconfig.InProgress = ptr.To(false)
	idleClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idle, flavor.DeepCopy(), device.DeepCopy()).Build()
	idleReconciler := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: idleClient, Scheme: scheme}}
	g.Expect(idleReconciler.ensureInPlaceAgentRole(ctx, idle)).To(Succeed())
	err := idleClient.Get(ctx, types.NamespacedName{Name: "da-worker1", Namespace: dpu.Namespace}, &rbacv1.Role{})
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
}

func TestEnsureInPlaceAgentRoleAddsAConfigMapFromTheUpdatedFlavor(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(rbacv1.AddToScheme(scheme)).To(Succeed())

	dpu := inPlaceRBACDPU()
	flavor := &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough",
			Namespace: dpu.Namespace,
		},
	}
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpu.Spec.DPUDeviceName,
			Namespace: dpu.Namespace,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu, flavor, device).Build()
	r := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: c, Scheme: scheme}}
	g.Expect(r.ensureInPlaceAgentRole(ctx, dpu)).To(Succeed())
	g.Expect(configMapNames(ctx, g, c, dpu)).NotTo(ContainElement("dpf-reconfig-marker"))

	stored := &provisioningv1.DPUFlavor{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: flavor.Name, Namespace: flavor.Namespace}, stored)).To(Succeed())
	stored.Spec.ConfigFiles = []provisioningv1.ConfigFile{
		agentAppliedMarkerFile(),
	}
	g.Expect(c.Update(ctx, stored)).To(Succeed())
	g.Expect(r.ensureInPlaceAgentRole(ctx, dpu)).To(Succeed())
	g.Expect(configMapNames(ctx, g, c, dpu)).To(ContainElement("dpf-reconfig-marker"))
}

// inPlaceRBACDPU returns a DPU with an in-place reconfiguration round in
// progress so the in-place Role refresh path runs.
func inPlaceRBACDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker1",
			Namespace: "dpf-operator-system",
			UID:       "dpu-uid",
		},
		Spec: provisioningv1.DPUSpec{
			DPUFlavor:     "passthrough",
			DPUDeviceName: "worker1-device",
		},
		Status: provisioningv1.DPUStatus{
			DPUReconfig: &provisioningv1.DPUReconfigStatus{
				InProgress: ptr.To(true),
				Round:      1,
			},
		},
	}
}

// agentAppliedMarkerFile returns an agent-applied config file sourced from the
// dpf-reconfig-marker ConfigMap.
func agentAppliedMarkerFile() provisioningv1.ConfigFile {
	return provisioningv1.ConfigFile{
		Type: ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
		Path: "/var/tmp/dpf-reconfig-marker",
		ContentFrom: &provisioningv1.ConfigFileContentSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "dpf-reconfig-marker",
				},
				Key: "marker",
			},
		},
	}
}

// configMapNames returns the ConfigMap resourceNames granted to the per-DPU
// agent Role.
func configMapNames(ctx context.Context, g *WithT, c client.Client, dpu *provisioningv1.DPU) []string {
	role := &rbacv1.Role{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "da-" + dpu.Name, Namespace: dpu.Namespace}, role)).To(Succeed())
	var names []string
	for _, rule := range role.Rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == "configmaps" {
			names = append(names, rule.ResourceNames...)
		}
	}
	return names
}
