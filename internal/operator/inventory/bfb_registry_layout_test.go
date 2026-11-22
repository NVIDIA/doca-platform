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

package inventory

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestClassifyBFBRegistryLayout(t *testing.T) {
	pvcName := "bfb-pvc"
	rwx := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	rwo := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	rwxRox := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany, corev1.ReadOnlyMany},
		},
	}

	tests := []struct {
		name       string
		pvcName    *string
		pvc        *corev1.PersistentVolumeClaim
		wantLayout BFBRegistryLayout
		wantClaim  string
	}{
		{name: "unset name", pvcName: nil, pvc: rwx, wantLayout: BFBRegistryLayoutProvisioningController, wantClaim: ""},
		{name: "empty name", pvcName: ptr.To(""), pvc: rwx, wantLayout: BFBRegistryLayoutProvisioningController, wantClaim: ""},
		{name: "missing pvc", pvcName: &pvcName, pvc: nil, wantLayout: BFBRegistryLayoutProvisioningController, wantClaim: ""},
		{name: "RWO", pvcName: &pvcName, pvc: rwo, wantLayout: BFBRegistryLayoutProvisioningController, wantClaim: ""},
		{name: "RWX pending", pvcName: &pvcName, pvc: rwx, wantLayout: BFBRegistryLayoutSpread, wantClaim: pvcName},
		{name: "RWX+ROX mix", pvcName: &pvcName, pvc: rwxRox, wantLayout: BFBRegistryLayoutSpread, wantClaim: pvcName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			layout, claim := ClassifyBFBRegistryLayout(tt.pvcName, tt.pvc)
			g.Expect(layout).To(Equal(tt.wantLayout))
			g.Expect(claim).To(Equal(tt.wantClaim))
		})
	}
}

func TestBFBRegistryReplicasConfigured(t *testing.T) {
	g := NewWithT(t)
	g.Expect(BFBRegistryReplicasConfigured(Variables{})).To(Equal(int32(0)))
	g.Expect(BFBRegistryReplicasConfigured(Variables{
		DPFProvisioningController: DPFProvisioningVariables{
			Registry: &operatorv1.RegistryConfiguration{},
		},
	})).To(Equal(int32(0)))
	g.Expect(BFBRegistryReplicasConfigured(Variables{
		DPFProvisioningController: DPFProvisioningVariables{
			Registry: &operatorv1.RegistryConfiguration{Replicas: 3},
		},
	})).To(Equal(int32(3)))
}

func TestBFBRegistryReplicas(t *testing.T) {
	g := NewWithT(t)
	g.Expect(bfbRegistryReplicas(Variables{})).To(Equal(int32(1)))
	g.Expect(bfbRegistryReplicas(Variables{
		DPFProvisioningController: DPFProvisioningVariables{
			Registry: &operatorv1.RegistryConfiguration{Replicas: 2},
		},
	})).To(Equal(int32(2)))
}

func TestBFBRegistryDisabled(t *testing.T) {
	g := NewWithT(t)
	g.Expect(BFBRegistryDisabled(Variables{})).To(BeFalse())
	g.Expect(BFBRegistryDisabled(Variables{
		DisableSystemComponents: map[operatorv1.ComponentName]bool{
			operatorv1.BFBRegistryName: true,
		},
	})).To(BeTrue())
	g.Expect(BFBRegistryDisabled(Variables{
		DPFProvisioningController: DPFProvisioningVariables{
			InstallInterface: &operatorv1.ProvisioningInstallInterface{
				InstallViaRedfish: &operatorv1.InstallViaRedfish{
					BFBRegistry: &operatorv1.BFBRegistryConfiguration{Disable: ptr.To(true)},
				},
			},
		},
	})).To(BeTrue())
}

func TestBFBRegistryManagedByOperator(t *testing.T) {
	g := NewWithT(t)
	spread := Variables{DPFProvisioningController: DPFProvisioningVariables{BFBRegistryLayout: BFBRegistryLayoutSpread}}
	g.Expect(BFBRegistryManagedByOperator(spread)).To(BeTrue())
	g.Expect(BFBRegistryManagedByOperator(Variables{})).To(BeFalse())

	disabled := spread
	disabled.DisableSystemComponents = map[operatorv1.ComponentName]bool{operatorv1.BFBRegistryName: true}
	g.Expect(BFBRegistryManagedByOperator(disabled)).To(BeFalse())
}
